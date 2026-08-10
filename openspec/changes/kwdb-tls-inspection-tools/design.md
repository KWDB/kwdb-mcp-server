## Context

`kwdb-mcp-server` 当前仅有 `query-metrics-history` 工具,通过 `X-Admin-Base-URL` header 与 `--admin-base-url` flag 调用 KaiwuDB `/ts/query` REST API,但不支持 Basic Auth 用户名密码认证,无法对接 TLS 模式部署的 KaiwuDB。

KGA 工程的 Python skill 实现了 `get_kwdb_ts_metrics.py` 与 `get_kwdb_statements.py`,但仅支持 insecure 部署(无认证)。本 change 整合二者,在 MCP server 层补齐 TLS + Basic Auth 能力,并移除旧的 `query-metrics-history`。

## Goals / Non-Goals

**Goals:**
- 新增 `query-metrics` 与 `query-slow-sql` 两个 MCP 工具,支持 TLS 部署与 insecure 部署
- 从 DB URL(`X-Database-URI` header 或默认连接池)派生 Basic Auth 凭据
- 智能 Admin URL 推导(三级降级)
- 智能 TLS 检测(基于 DB URL sslmode)
- KaiwuDB 响应体错误解析(HTTP 状态码不可靠)

**Non-Goals:**
- 不实现 mTLS 客户端证书认证
- 不引入新凭据存储
- 不替换 SQL 连接驱动(沿用 lib/pq)
- 不修改 `read-query` / `write-query`
- 不修改任何 prompt/resource

## Decisions

### Decision 1: DB URL 解析模块独立为 `pkg/db/dburl.go`

**Why**: 当前 `pkg/db/` 没有任何 DB URL 解析逻辑;DB URL 解析是 admin 工具的依赖,但不应与 SQL 连接池耦合。独立文件 + 公开函数,便于复用与测试。

**格式**: `postgresql://[user[:password]@]host[:port][/dbname][?sslmode=...&sslcert=...&sslkey=...&sslrootcert=...]`

**导出结构**:
```go
type DBURLInfo struct {
    User, Password, Host string
    Port                 int
    Database             string
    SSLMode              string  // raw value
    SSLCert, SSLKey, SSLRootCert string
    HasPassword          bool
}

func ParseDBURL(dbURL string) (DBURLInfo, error)
```

### Decision 2: Admin URL 三级降级

**优先级**: ① `X-Admin-Base-URL` header(任意 scheme) → ② `--admin-base-url` flag(任意 scheme) → ③ 从 DB URL 推导(`isTLS ? https:// : http://`,host + 端口 8080)

**Why**: 与现有架构兼容,显式覆盖优先;自动推导作为合理默认值。

**bugfix-2026-08-07**: 解析器在返回 URL 的同时,以第三个返回值暴露 `adminURLIsTLS`(URL scheme 是否为 https),独立于 `IsTLS(dbURL)`。调用方在做 Authorization 决策时把这两个信号 OR 起来(`effective_isTLS = adminURLIsTLS || IsTLS(dbURL)`),覆盖"secure 集群 + `sslmode=disable`"的合法场景——CockroachDB/KaiwuDB 允许 SQL 端用 insecure 连接,但 admin 端口仍要求 HTTPS + Basic Auth,否则会被 401 拒掉。

### Decision 3: TLS 检测算法基于 DB URL sslmode

**算法**:
```go
func isTLSForAdmin(sslmode string, sslcert, sslkey, sslrootcert string) bool {
    switch strings.ToLower(sslmode) {
    case "disable", "":
        return false  // INSECURE
    case "allow", "prefer":
        return false  // 默认按 insecure 兜底,见 Decision 4
    case "require", "verify-ca", "verify-full":
        return true
    default:
        return false
    }
}
```

**简化说明**: 鉴于用户指示"insecure 模式统一不带 Authorization 透传请求",`allow`/`prefer` 按 INSECURE 兜底,不再实现 TRY_TLS_FIRST 模式。

### Decision 4: Authorization header 决策

`effective_isTLS = adminURLIsTLS || isTLS(dbURL)`(bugfix-2026-08-07 引入的 OR 组合)

| effective_isTLS | HasPassword | 是否发送 Authorization |
|---|---|---|
| false | 任意 | ❌ 不发送 |
| true | true | ✅ Basic Auth |
| true | false | ❌ 立即报错 `TLS admin endpoint requires credentials, but DB URL has no password` |

**Why**: KaiwuDB admin REST API 只接受 Basic Auth;insecure 部署无用户概念,无需认证。bugfix-2026-08-07 把 admin URL 的 https-ness 也纳入判断,避免"secure 集群 + DB URL sslmode=disable"的合法配置被误判为 insecure、丢 Authorization 头、被 401 拒掉。

### Decision 5: HTTP 客户端配置

- **TLS 证书验证**: 硬编码跳过(`tls.Config{InsecureSkipVerify: true}`),匹配 Python `ssl._create_unverified_context()`
- **超时**: 30 秒
- **重试**: 不实现(由 LLM 客户端决定)

### Decision 6: 响应解析必须基于 body code 字段

**Why**: KaiwuDB 巡检 REST API 即使认证失败也可能返回 HTTP 200(待调试阶段验证),响应体 `code` 字段才是错误判定依据。

### Decision 7: query-slow-sql 走 SQL 系统表而非 admin REST endpoint(bugfix-2026-08-07)

`query-slow-sql` 不再调 `/_status/statements`,改为 SELECT `kwdb_internal.node_statement_statistics`。原因:

- KaiwuDB 企业版 3.3.0 移除了 `/_status/statements` admin endpoint
- 系统表 `kwdb_internal.node_statement_statistics` 由每个节点的语句统计 collector 写入,包含 key、count、`*_lat_avg`(秒)等字段
- SQL 端口的 DB URL 用户名密码已足够认证,不需要 admin URL

**SQL 形态**(参数化 + 排序白名单,防注入):

```sql
SELECT key, count, service_lat_avg, run_lat_avg, plan_lat_avg, database, user_name
FROM kwdb_internal.node_statement_statistics
WHERE service_lat_avg >= $1
ORDER BY <whitelisted_column> DESC
LIMIT $2
```

占位符用 `$1`/`$2` 而不是 `?`(KaiwuDB pgx 驱动要求 postgresql 协议风格)。排序字段 `service_lat_avg`/`run_lat_avg`/`plan_lat_avg`/`count` 走 `slowSQLStatementsSortColumn` 白名单,任何用户输入都不会直接拼进 SQL。

依赖注入点 `slowSQLExecuteQueryFn`:生产实现路由到 `db.GetMultiPoolManager().ExecuteWithURI`,测试用 canned rows stub 替换,避免依赖真实数据库。

**解析器**(在 `pkg/tools/admin.go`):
```go
type AdminResponse struct {
    Code int    `json:"code"`
    Desc string `json:"desc"`
    // 其他字段保留原始 RawMessage
}

func parseAdminResponse(body []byte) (*AdminResponse, error) {
    // 1. 尝试解析 JSON
    // 2. 提取 code / desc
    // 3. code != 0 → 返回 desc 作为 error
}
```

### Decision 7: METRICS_MAP 固定子集(~30 个指标)

**Why**: 与 Python 脚本对齐,LLM 只能从已知指标名中选取,降低误用风险。完整列表见 `pkg/tools/metrics.go` 的 `inspectionMetrics` 映射。

### Decision 8: 测试策略

- **单元测试**: DB URL 解析、TLS 检测、Auth 决策、METRICS_MAP 校验、过滤排序
- **集成测试**: `httptest.NewServer` mock KaiwuDB admin API,断言 Authorization header、请求 payload、响应解析
- **覆盖率**: 目标 ≥ 80%

## Risks / Trade-offs

[Risk 1: TLS 模式 + 无 password 的误用] → Mitigation: fail-fast,工具立即返回明确错误信息,不发送 HTTP 请求。

[Risk 2: HTTP 状态码在认证失败时的实际行为未确认] → Mitigation: 响应解析以 body `code` 字段为准,不依赖 HTTP 状态码;调试阶段验证后修正响应解析器。

[Risk 3: 移除 `query-metrics-history` 是破坏性变更] → Mitigation: 在 README 中标注 changelog,客户端升级时同步替换工具名。

[Risk 4: METRICS_MAP 固定列表可能不覆盖所有巡检需求] → Mitigation: 完整对齐 Python skill 的现有列表;未来若有新指标需求,通过新 change 扩展。

[Risk 5: `pkg/db/dburl.go` 与 lib/pq 驱动的 DB URL 解析可能存在边界差异] → Mitigation: 自行实现轻量解析器,只关注我们需要的字段,不依赖 lib/pq 内部。

## Migration Plan

1. **代码层面**: 一次性提交(删除 + 新增 + 修改)
2. **文档层面**: 同步更新 README
3. **客户端层面**: 升级到新版本后,使用 `query-metrics-history` 的客户端需替换为 `query-metrics`(语义不完全等价,见 design.md Decision 7)
4. **回滚策略**: `git revert` 即可;破坏性变更已通过版本号标注

## Open Questions

- ❓ KaiwuDB 巡检 REST API 认证失败时实际 HTTP 状态码(待调试验证)
- ❓ 业务错误的 `code` 字段具体取值集合(待调试验证)
- ❓ TLS 模式下 admin 端点的默认端口(KaiwuDB 文档未明确,当前假设 8080,与 SQL 端口同 host)
