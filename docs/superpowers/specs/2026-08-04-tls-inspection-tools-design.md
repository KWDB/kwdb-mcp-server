---
comet_change: kwdb-tls-inspection-tools
role: technical-design
canonical_spec: openspec
---

# TLS Inspection Tools — 技术设计文档

## Context

`kwdb-mcp-server` 当前提供 `query-metrics-history` 工具,通过 KaiwuDB admin 端口的 `/ts/query` REST API 查询历史时序指标,但**仅支持无认证调用**,无法对接 TLS 模式部署的 KaiwuDB。KGA 工程通过 Python skill (`get_kwdb_ts_metrics.py`、`get_kwdb_statements.py`) 实现了巡检能力,但同样不支持 Basic Auth 用户名密码认证。

本 change 完整移除 `query-metrics-history`,基于 KGA Python 脚本封装两个新的 LLM Agent Tools,通过 MCP server 已保存的 DB URL(在 `X-Database-URI` header 或默认连接池中)派生 Basic Auth 凭据,支持 TLS 部署与 insecure 部署两种形态的 KaiwuDB 巡检。

Open 阶段已完成多轮澄清,主要设计决策已锁定,见 [proposal.md](../../openspec/changes/kwdb-tls-inspection-tools/proposal.md) 与 [spec.md](../../openspec/changes/kwdb-tls-inspection-tools/specs/tls-inspection-tools/spec.md)。

## 术语表(Glossary)

| 术语 | 全称 | 说明 |
|---|---|---|
| **DB URL** | Database URL | 数据库连接字符串,描述连接到数据库所需的全部信息(host/port/credentials/dbname/options)。本设计文档中 DB URL 特指 `postgresql://` 协议 URI 形式,通过 `X-Database-URI` header 传入 MCP server。 |
| **KaiwuDB** | Kaiwu Database | 开务数据库,本 MCP server 服务的目标数据库。 |
| **TLS** | Transport Layer Security | 传输层安全协议,用于加密 HTTP 通信。本设计中等价于 HTTPS。 |
| **mTLS** | Mutual TLS | 双向 TLS 认证,客户端与服务端互相验证证书。KaiwuDB SQL 连接支持 mTLS,但 admin REST API 不支持,本设计不在 admin 客户端实现 mTLS。 |
| **Basic Auth** | HTTP Basic Authentication | HTTP 标准认证机制,以 `Authorization: Basic <base64(user:password)>` header 传递凭据。KaiwuDB admin REST API 唯一支持的认证方式。 |
| **Admin URL** | Admin Endpoint URL | KaiwuDB admin 端口的服务地址,如 `https://host:8081`。与 SQL 端口(默认 26257)不同。 |
| **Inspection** | 巡检 | 对运行中的数据库进行健康检查、性能指标采集、慢 SQL 分析等运维动作。 |
| **METRICS_MAP** | Metric Name → Query Parameter Map | 固定指标名到查询参数(downsampler/source_aggregator/derivative)的映射表,本设计包含 ~30 个巡检相关指标。 |
| **HTTP Status Code** | HTTP 响应状态码 | 如 200/401/500。本设计中 KaiwuDB admin API 的认证失败可能仍返回 200(待验证),因此不能仅依赖 HTTP 状态码。 |

## Goals / Non-Goals

**Goals**:
- 删除 `query-metrics-history` 工具及其相关文件
- 新增 `query-metrics`(封装 `/restapi/ts/query` REST API,bugfix-2026-08-07 路径修正)与 `query-slow-sql`(SQL 查 `kwdb_internal.node_statement_statistics` 系统表,bugfix-2026-08-07 从 `/_status/statements` admin REST 重写)
- 复用 MCP server 已保存的 DB URL,自动派生 Basic Auth 凭据
- 支持 TLS 部署与 insecure 部署两种形态
- 智能 Admin URL 推导(三级降级)
- 智能 TLS 检测(基于 DB URL `sslmode` 与 admin URL scheme OR 组合)
- 响应解析以 body `code` 字段为准(HTTP 状态码不可靠)
- 完整的单元 + 集成测试覆盖(目标 ≥ 80%)

**Non-Goals**:
- 不实现 mTLS 客户端证书认证(admin REST API 不支持)
- 不引入新凭据存储或凭据管理系统
- 不替换 SQL 连接驱动(沿用 `lib/pq`)
- 不修改 `read-query` / `write-query` 工具
- 不修改任何 prompt 或 resource

## Decisions

### Decision 1: DB URL 解析模块独立为 `pkg/db/dburl.go`

**Why**: 当前 `pkg/db/` 没有 DB URL 解析逻辑;DB URL 解析是 admin 工具的依赖,但不应与 SQL 连接池耦合。独立文件 + 公开函数,便于复用与单元测试。

**格式**: `postgresql://[user[:password]@]host[:port][/dbname][?sslmode=...&sslcert=...&sslkey=...&sslrootcert=...]`

**导出结构**:

```go
// pkg/db/dburl.go
type DBURLInfo struct {
    Scheme                          string  // "postgresql"
    User, Password                  string
    Host                            string
    Port                            int
    Database                        string
    SSLMode                         string  // 原始值
    SSLCert, SSLKey, SSLRootCert    string
    HasPassword                     bool
}

func ParseDBURL(dbURL string) (DBURLInfo, error)
func (d DBURLInfo) IsTLS() bool
```

**解析规则**:
- 必须以 `postgresql://` 开头,否则报错
- password 中的 URL-encoded 字符(`%40` → `@`)需正确解码(调用方负责 URL-encode)
- query 参数名**严格小写**:`sslmode` / `sslcert` / `sslkey` / `sslrootcert` 等;大写或混合大小写一律不识别
- 不接受空 dbname(未提供 dbname 段或为 `//` → 报错)
- 解析失败返回明确错误,不静默忽略

**边界场景与判定**:

| 场景 | 判定 |
|---|---|
| `postgresql://user:@host:port/db`(空 password) | `Password=""`, `HasPassword=false` |
| `postgresql://user@host:port/db`(无 password 段) | `Password=""`, `HasPassword=false` |
| `postgresql://host:port/db`(无 user) | 返回错误 `missing user` |
| `postgresql://user:pass@host/db`(无 port) | `Port=26257`(KaiwuDB SQL 约定) |
| `postgresql://user:pass@host:26257/`(空 dbname) | 返回错误 `empty database name` |
| `?sslmode=require` | `SSLMode="require"` |
| `?SSLMODE=require`(大写) | 不识别,等价于未设置 `sslmode`(`IsTLS=false`) |
| `?SslMode=require`(混合大小写) | 不识别,等价于未设置 `sslmode` |
| `?sslcert=...` | `SSLCert="..."`(同小写规则) |
| `?sslmode=require&sslmode=disable`(重复) | 取第一个值(Go `url.ParseQuery` 默认行为) |
| `user:Kaiwudb%40123@host:port/db` | `Password="Kaiwudb@123"`(自动解码) |
| `user:plain@pass@host:port/db`(password 含未编码 `@`) | 解析失败(URL 解析器无法确定边界) |

### Decision 2: Admin URL 三级降级

**优先级**: ① `X-Admin-Base-URL` header(任意 scheme) → ② `--admin-base-url` flag(任意 scheme) → ③ 从 DB URL 推导

**DB URL 推导规则**:
- 显式 URL 中已含 scheme → 使用其 scheme
- 从 DB URL 推导时,scheme 由 `IsTLS(dbURL)` 决定:`true` → `https://`;`false` → `http://`
- host + 固定端口 8080(KaiwuDB 约定 admin 端口)
- 缺省(无任何来源)→ 返回错误 `missing admin URL and database URI`

**bugfix-2026-08-07**: 解析器除了返回 URL,还以第三返回值方式暴露 `adminURLIsTLS`(URL scheme 是否为 https)。该信号独立于 `IsTLS(dbURL)`,让调用方在判断是否发送 Basic Auth 时能把 admin URL 的 https-ness 与 DB URL 的 sslmode 一起 OR 起来,覆盖"secure 集群但 DB URL 不开 TLS"的合法场景(CockroachDB/KaiwuDB SQL 端口允许 `sslmode=disable` 但 admin 端口仍要求 HTTPS+auth,HTTP→HTTPS 重定向后缺 auth 头会被 401 拒掉)。

### Decision 3: TLS 检测算法

**基于 KaiwuDB DB URL `sslmode` 语义**:

| `sslmode` | TLS 检测结果 |
|---|---|
| `""`(未设置)、`disable` | `false`(INSECURE) |
| `allow`、`prefer` | `false`(按 INSECURE 兜底) |
| `require`、`verify-ca`、`verify-full` | `true` |
| 其他未知值 | `false`(安全兜底) |

**简化说明**: 用户在 Open 阶段明确"insecure 模式统一不带 Authorization 透传请求",因此 `allow`/`prefer` 按 INSECURE 兜底,不实现 TRY_TLS_FIRST 模式。

**注意**: 此判断基于 KaiwuDB DB URL 规范(用户提供的官方文档截图)。其他数据库的 URL 参数不适用。

### Decision 4: Authorization Header 决策

`effective_isTLS = adminURLIsTLS || IsTLS(dbURL)`(bugfix-2026-08-07 引入的 OR 组合)

| `effective_isTLS` | `HasPassword` | 是否发送 Authorization |
|---|---|---|
| `false` | 任意 | ❌ 不发送 |
| `true` | `true` | ✅ 发送 `Basic <base64(user:password)>` |
| `true` | `false` | ❌ **立即报错** `TLS admin endpoint requires credentials, but DB URL has no password` |

**Why**:
- KaiwuDB admin REST API 只接受 Basic Auth;insecure 部署无用户概念,SQL 连接串中的 password 仅是驱动占位符,admin 端点既不识别也不需要。
- bugfix-2026-08-07: admin URL scheme 与 DB URL sslmode 独立判断。`secure 集群 + sslmode=disable` 的合法场景(CockroachDB/KaiwuDB SQL 允许 insecure、admin 仍要求 HTTPS+auth)会被错误地判定为 insecure 而丢失 Authorization 头,导致 307→HTTPS 之后被 401 拒掉。OR 组合保证 https admin URL 一定走认证路径。

### Decision 5: HTTP 客户端配置

- **超时**: 30 秒
- **TLS**: 硬编码跳过证书验证(`tls.Config{InsecureSkipVerify: true}`),匹配 Python `ssl._create_unverified_context()` 行为,适配自签名证书
- **重试**: 不实现(由 LLM 客户端决定)
- **方法**: query-metrics 用 POST,query-slow-sql 用 GET

### Decision 6: 响应解析以 body `code` 字段为准

**Why**: 用户提供的 KaiwuDB 内部设计文档片段显示,巡检 REST API 认证失败时可能返回 HTTP 200(实际行为待调试验证),响应体 `code: -1` 才是错误判定依据。

**错误翻译矩阵**:

| 来源 | 工具返回 |
|---|---|
| HTTP 5xx | `admin API server error: status=N body=<前200字节>` |
| HTTP 4xx | `admin API client error: status=N body=<前200字节>` |
| HTTP 2xx + `code=-1` | `auth failed: <desc>` |
| HTTP 2xx + `code=其他` | `<desc>` |
| HTTP 2xx + 非 JSON | `unexpected response format: <前200字节>` |
| 网络错误 | `admin request failed: <err>` |
| DB URL 解析失败 | `invalid X-Database-URI: <err>` |
| TLS + 无 password | `TLS admin endpoint requires credentials, but DB URL has no password` |
| 未知指标 | `unknown metric: <name>` |

**响应解析器**:

```go
// pkg/tools/admin.go
type AdminResponse struct {
    Code int    `json:"code"`
    Desc string `json:"desc"`
    Raw  json.RawMessage  // 保留完整 body
}

func parseAdminResponse(body []byte) (*AdminResponse, error) {
    // 1. 尝试解析 JSON
    // 2. 提取 code / desc
    // 3. code != 0 → 返回 desc 作为 error
}
```

### Decision 7: METRICS_MAP 固定子集(共 32 个指标)

**Why**: 与 Python 脚本 `get_kwdb_ts_metrics.py` 的 `METRICS_MAP` 对齐,LLM 只能从已知指标名中选取,降低误用风险。

**完整指标列表**(共 32 个):

| 类别 | 指标名 | downsampler / source_aggregator | derivative |
|---|---|---|---|
| 基础 | `cr.node.liveness.livenodes` | avg / avg | none |
| 基础 | `cr.node.sys.uptime` | avg / avg | none |
| 系统 | `cr.node.sys.cpu.user.percent` | avg / sum | none |
| 系统 | `cr.node.sys.cpu.sys.percent` | avg / sum | none |
| 系统 | `cr.node.sys.cpu.combined.percent-normalized` | avg / sum | none |
| 系统 | `cr.store.capacity` | avg / sum | none |
| 系统 | `cr.store.capacity.available` | avg / sum | none |
| 系统 | `cr.store.capacity.used` | avg / sum | none |
| 系统 | `cr.node.sys.rss` | avg / sum | none |
| 系统 | `cr.node.sys.go.allocbytes` | avg / sum | none |
| 系统 | `cr.node.sys.go.totalbytes` | avg / sum | none |
| 数据库 | `cr.node.sql.insert.count` | avg / sum | none |
| 数据库 | `cr.node.sql.update.count` | avg / sum | none |
| 数据库 | `cr.node.sql.delete.count` | avg / sum | none |
| 数据库 | `cr.node.sql.select.count` | avg / sum | none |
| 数据库 | `cr.node.sql.query.count` | avg / sum | none |
| 数据库 | `cr.store.rebalancing.writespersecond` | avg / sum | none |
| 数据库 | `cr.store.rebalancing.queriespersecond` | avg / sum | none |
| 数据库 | `cr.node.exec.latency-p99` | avg / avg | none |
| 数据库 | `cr.node.sql.service.latency-p99` | avg / avg | none |
| 数据库 | `cr.node.sql.distsql.exec.latency-p99` | avg / avg | none |
| 存储 | `cr.store.totalbytes` | avg / sum | none |
| 存储 | `cr.store.livebytes` | avg / sum | none |
| 集群 | `cr.store.replicas` | avg / sum | none |
| 集群 | `cr.store.replicas.leaders` | avg / sum | none |
| 集群 | `cr.store.replicas.leaseholders` | avg / sum | none |
| 集群 | `cr.store.ranges.unavailable` | avg / max | none |
| 集群 | `cr.store.ranges.underreplicated` | avg / max | none |
| 集群 | `cr.store.ranges.overreplicated` | avg / max | none |
| 集群 | `cr.store.raftlog.behind` | avg / max | none |
| 集群 | `cr.store.raft.replica.consistent.latency-p99` | avg / avg | none |
| 网络 | `cr.node.clock-offset.meannanos` | avg / max | none |

注: 衍生计算(`derivative`)全部为 `none`,保留未来扩展空间。

### Decision 8: 测试策略

- **单元测试**: DB URL 解析(含 URL-decode、各 query param、SSL 检测、TLS 检测)、Auth 决策、METRICS_MAP 校验、过滤排序、Admin URL 推导
- **集成测试**: `httptest.NewServer` mock KaiwuDB admin API,断言 Authorization header、请求 payload、响应解析
- **覆盖率目标**: ≥ 80%

## Risks / Trade-offs

[Risk 1: TLS 模式 + 无 password 的误用] → Mitigation: fail-fast,工具立即返回明确错误信息,不发送 HTTP 请求。

[Risk 2: HTTP 状态码在认证失败时的实际行为未确认] → Mitigation: 响应解析以 body `code` 字段为准,不依赖 HTTP 状态码;调试阶段验证后修正响应解析器。

[Risk 3: 移除 `query-metrics-history` 是破坏性变更] → Mitigation: 在 README 中标注 changelog,客户端升级时同步替换工具名。

[Risk 4: METRICS_MAP 固定列表可能不覆盖所有巡检需求] → Mitigation: 完整对齐 Python skill 的现有列表;未来若有新指标需求,通过新 change 扩展。

[Risk 5: `pkg/db/dburl.go` 与 lib/pq 驱动的 DB URL 解析可能存在边界差异] → Mitigation: 自行实现轻量解析器,只关注我们需要的字段,不依赖 lib/pq 内部。

[Risk 6: KaiwuDB admin 默认端口假设为 8080] → Mitigation: 文档明确说明;用户可通过 `X-Admin-Base-URL` 显式覆盖。

## Migration Plan

1. **代码层面**: 一次性提交(删除 + 新增 + 修改)
2. **文档层面**: 同步更新 README(`README.md`、`README_zh.md`)
3. **客户端层面**: 升级到新版本后,使用 `query-metrics-history` 的客户端需替换为 `query-metrics`(语义不完全等价,见 Decision 7)
4. **回滚策略**: `git revert` 即可;破坏性变更已通过版本号标注

## Open Questions

- ❓ KaiwuDB 巡检 REST API 认证失败时实际 HTTP 状态码(待调试验证)
- ❓ 业务错误的 `code` 字段具体取值集合(待调试验证)
- ❓ TLS 模式下 admin 端点的默认端口(KaiwuDB 文档未明确,当前假设 8080,与 SQL 端口同 host)