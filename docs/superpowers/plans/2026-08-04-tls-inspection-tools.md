---
change: kwdb-tls-inspection-tools
design-doc: docs/superpowers/specs/2026-08-04-tls-inspection-tools-design.md
base-ref: 68449695897022f6bdb3c593e47be78aa2f362cc
---

# TLS Inspection Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Each checkbox is an independently reviewable step.

**Goal:** 移除 `query-metrics-history`，新增支持 TLS/Basic Auth 的 `query-metrics` 与 `query-slow-sql` MCP 工具，并完成测试、注册、文档和验证。

**Architecture:** 将 PostgreSQL DB URL 解析独立放在 `pkg/db/dburl.go`；`pkg/tools/admin.go` 提供 Admin URL 推导、TLS/Auth 决策、HTTP 请求和统一响应解析；两个工具只负责输入校验、KaiwuDB API payload 和结果整理。工具通过 `X-Database-URI` 或默认连接池对应的连接串获得凭据，Admin URL 按 header → flag → DB URL 推导三级降级。

**Tech Stack:** Go 1.23、`net/url`、`net/http`、`crypto/tls`、`httptest`、mcp-go v0.40.0、lib/pq。

## 关联产物

- 技术设计：[docs/superpowers/specs/2026-08-04-tls-inspection-tools-design.md](../specs/2026-08-04-tls-inspection-tools-design.md)
- 需求场景：[openspec/changes/kwdb-tls-inspection-tools/specs/tls-inspection-tools/spec.md](../../../openspec/changes/kwdb-tls-inspection-tools/specs/tls-inspection-tools/spec.md)
- 任务边界：[openspec/changes/kwdb-tls-inspection-tools/tasks.md](../../../openspec/changes/kwdb-tls-inspection-tools/tasks.md)

## Global Constraints

- 仅接受 `postgresql://` DB URL；缺 user、空 database name、无法解析 URL 时必须返回明确错误。
- query 参数名严格区分大小写；只识别小写 `sslmode`、`sslcert`、`sslkey`、`sslrootcert`，重复参数取第一个。
- SQL 端口默认 26257，Admin 推导端口固定 8080；显式 Admin URL 可使用任意 scheme/端口。
- `sslmode=require|verify-ca|verify-full` 才判定 TLS；空值、`disable`、`allow`、`prefer` 和未知值判定 insecure。
- insecure 请求无论 password 是否存在都不发送 Authorization；TLS 且无 password 必须 fail-fast，不能发 HTTP 请求。
- Admin HTTP 客户端 timeout 为 30 秒，TLS 使用 `InsecureSkipVerify: true`，不实现重试。
- 错误判定优先依据响应 body 的 `code`；HTTP 4xx/5xx、非 JSON、网络错误按设计文档错误矩阵翻译。
- METRICS_MAP 固定包含设计文档列出的 30 个指标，所有 derivative 为 `none`。
- 每项实现先写失败测试，再写最小实现；每个任务完成后运行指定测试并单独提交。

## 文件地图

| 文件 | 责任 |
|---|---|
| `pkg/db/dburl.go` | `DBURLInfo`、DB URL 解析和 TLS 语义判断 |
| `pkg/db/dburl_test.go` | DB URL 正常、边界和错误场景 |
| `pkg/tools/admin.go` | Admin URL、TLS/Auth、HTTP transport、响应错误翻译 |
| `pkg/tools/admin_test.go` | 公共 Admin 模块单元及 HTTPS 自签名集成测试 |
| `pkg/tools/metrics.go` | 固定指标映射、query-metrics 输入、`/ts/query` POST |
| `pkg/tools/metrics_test.go` | 指标校验、payload、认证和响应集成测试 |
| `pkg/tools/slow_sql.go` | statements 输入、解析、过滤排序、GET 工具 |
| `pkg/tools/slow_sql_test.go` | 慢 SQL 默认值、过滤排序和 Admin 集成测试 |
| `pkg/tools/tools.go` | 新旧工具注册入口及配置收敛 |
| `pkg/server/server.go` | ServerConfig 清理和注册调用 |
| `cmd/kwdb-mcp-server/main.go` | CLI flag 和 server config 清理 |
| `README.md`, `README_zh.md` | 新工具说明、旧工具迁移说明 |
| `docs/design-tls-inspection-tools.md` | 面向仓库用户的设计与边界说明 |

## 实施顺序与依赖

严格按 `DSN → Admin → query-metrics/query-slow-sql → 注册 → 清理 → 文档 → 验证` 执行。任务 3.1 与 3.2 可在 Admin 完成后并行；任务 4.1 与 4.2 同理，但注册任务必须等待两个工具完成。

### Task 1: 定义 DBURLInfo

**依赖:** 无

**Files:** Create `pkg/db/dburl.go`；Test `pkg/db/dburl_test.go`

**目标代码片段:**
```go
type DBURLInfo struct {
    Scheme string; User, Password string; Host string; Port int; Database string
    SSLMode string; SSLCert, SSLKey, SSLRootCert string; HasPassword bool
}
func ParseDBURL(dbURL string) (DBURLInfo, error)
func (d DBURLInfo) IsTLS() bool
```

- [x] 写失败测试：标准 URL 断言所有字段和 `HasPassword=true`，`IsTLS()` 对 require 为 true。
- [x] 运行 `go test ./pkg/db -run TestParseDBURL -count=1`，预期因文件/函数不存在失败。
- [x] 实现仅使用 `url.Parse`/`url.ParseQuery`，校验 scheme、user、host、database；默认 port 26257；小写 query 取第一个值。
- [x] 运行同一命令，预期 PASS。
- [x] 验收：导出结构和函数签名与设计文档 Decision 1 完全一致。

### Task 2: 覆盖 DB URL 边界场景

**依赖:** Task 1

**Files:** Modify `pkg/db/dburl_test.go`

**目标测试代码片段:**
```go
cases := []struct{name, raw, wantErr, password, sslmode string; hasPassword bool}{
 {"empty password", "postgresql://user:@host/db", "", "", "", false},
 {"no password", "postgresql://user@host/db", "", "", "", false},
 {"missing user", "postgresql://host:26257/db", "missing user", "", "", false},
 {"empty db", "postgresql://user:pass@host:26257/", "empty database name", "", "", false},
 {"encoded password", "postgresql://user:Kaiwudb%40123@host/db", "", "Kaiwudb@123", "", true},
 {"uppercase query", "postgresql://user:pass@host/db?SSLMODE=require", "", "pass", "", true},
}
```

- [x] 为上述表格及 cert paths、重复 `sslmode`、未编码多 `@`、非 postgresql URL 添加表驱动测试。
- [x] 运行 `go test ./pkg/db -run 'TestParseDBURL|TestDBURL' -count=1`，预期新增边界用例先失败。
- [x] 补齐解析错误文本和 query 处理，不改变 Task 1 公共接口。
- [x] 运行测试并验收：覆盖空 password、缺 user、空 dbname、大写参数、URL decode；单元测试通过。

### Task 3: 实现 TLS 语义判断

**依赖:** Task 1

**Files:** Modify `pkg/db/dburl.go`；Test `pkg/db/dburl_test.go`

**目标测试代码片段:**
```go
for _, mode := range []string{"require", "verify-ca", "verify-full"} { got := infoWithSSLMode(mode).IsTLS(); if !got { t.Errorf("%s: want TLS", mode) } }
for _, mode := range []string{"", "disable", "allow", "prefer", "unknown"} { if infoWithSSLMode(mode).IsTLS() { t.Errorf("%s: want insecure", mode) } }
```

- [x] 写并运行 `go test ./pkg/db -run TestDBURLIsTLS -count=1`，预期缺少/错误实现时失败。
- [x] 实现 `IsTLS` 只将三个明确 TLS 值返回 true，其他值 false。
- [x] 验收：行为与 spec 的三个 TLS/insecure 场景一致。

### Task 4: 实现 Admin TLS/Auth 基础函数

**依赖:** Tasks 1–3

**Files:** Create `pkg/tools/admin.go`；Test `pkg/tools/admin_test.go`

**目标代码片段:**
```go
func isTLSForAdmin(sslmode, sslcert, sslkey, sslrootcert string) bool
func buildAuthHeader(password, user string) string
```

- [x] 写测试：三个 TLS sslmode 为 true；allow/prefer/disable/unknown 为 false；`buildAuthHeader("p@ss", "root")` 等于 `Basic ` + base64(`root:p@ss`)；空 password 返回空。
- [x] 运行 `go test ./pkg/tools -run 'TestIsTLSForAdmin|TestBuildAuthHeader' -count=1`，预期失败。
- [x] 实现 `isTLSForAdmin` 委托同一 TLS 语义，`buildAuthHeader` 使用 `base64.StdEncoding.EncodeToString([]byte(user+":"+password))`。
- [x] 验收：insecure 不因 password 存在而生成 header；TLS 认证值可被标准 Basic Auth 解码。

### Task 5: 实现 Admin URL 三级降级

**依赖:** Tasks 1–4

**Files:** Modify `pkg/tools/admin.go`；Test `pkg/tools/admin_test.go`

**目标代码片段:**
```go
func resolveAdminBaseURL(headerValue, flagValue, dbURL string) (string, error)
```

- [x] 写表驱动测试：header 优先、flag 次之、require 推导 `https://host:8080`、disable 推导 `http://host:8080`、全缺省返回 `missing admin URL and database URI`。
- [x] 运行 `go test ./pkg/tools -run TestResolveAdminBaseURL -count=1`，预期失败。
- [x] 实现 trim 空白；显式 header/flag 原样保留 scheme；仅 DB URL 时调用 `db.ParseDBURL` 并按 `IsTLS` 推导。
- [x] 验收：header 不被 DB URL 覆盖，DB URL 错误包装为 `invalid X-Database-URI: ...`。

### Task 6: 实现 Admin 响应解析

**依赖:** Task 4

**Files:** Modify `pkg/tools/admin.go`；Test `pkg/tools/admin_test.go`

**目标代码片段:**
```go
type AdminResponse struct { Code int `json:"code"`; Desc string `json:"desc"`; Raw json.RawMessage }
func parseAdminResponse(body []byte) (*AdminResponse, error)
```

- [x] 写测试覆盖 code 0 成功、code -1 转换为 `auth failed: ...`、code 42 返回 desc、纯文本/HTML 返回前 200 字节的 `unexpected response format: ...`。
- [x] 运行 `go test ./pkg/tools -run TestParseAdminResponse -count=1`，预期失败。
- [x] 实现 JSON 解码并保留完整 Raw；非 JSON 截断 200 字节；code != 0 返回设计矩阵要求的错误。
- [x] 验收：HTTP 200 + code -1 必须判错，不能仅按 HTTP 状态判断。

### Task 7: 实现 Admin HTTP 请求客户端

**依赖:** Tasks 5–6

**Files:** Modify `pkg/tools/admin.go`；Test `pkg/tools/admin_test.go`

**目标代码片段:**
```go
func doAdminRequest(ctx context.Context, method, url string, body []byte, user, password string, isTLS bool) (*http.Response, []byte, error)
```

- [x] 写 `httptest.NewTLSServer` 测试：正确 Basic Auth 时成功；TLS transport 接受自签名证书；insecure 请求没有 Authorization；TLS 无 password 在发送前返回精确错误。
- [x] 运行 `go test ./pkg/tools -run TestDoAdminRequest -count=1`，预期失败。
- [x] 实现 30 秒 timeout、TLS `InsecureSkipVerify: true`、Content-Type、请求体读取；将 4xx/5xx 翻译为 `admin API client/server error: status=N body=...`，网络错误翻译为 `admin request failed: ...`。
- [x] 验收：TLS 无密码时 mock handler 计数为 0；所有请求可取消并关闭 response body。

### Task 8: 固化 Admin 公共模块回归测试

**依赖:** Tasks 4–7

**Files:** Modify `pkg/tools/admin_test.go`

**目标测试代码片段:**
```go
func TestAdminErrorMatrix(t *testing.T) { /* 4xx, 5xx, 200/code=-1, 200/non-JSON */ }
```

- [x] 汇总所有 Admin 场景为独立测试，增加显式 URL 任意 scheme/端口、空 header/flag trim 测试。
- [x] 运行 `go test ./pkg/tools -run 'Test(Admin|Resolve|Parse)' -count=1`。
- [x] 验收：Admin 模块覆盖设计文档 Decision 2–6，测试可单独运行且无真实数据库依赖。

### Task 9: 定义 query-metrics 指标映射

**依赖:** Task 8

**Files:** Create `pkg/tools/metrics.go`；Test `pkg/tools/metrics_test.go`

**目标代码片段:**
```go
type metricSpec struct { Downsampler, SourceAggregator, Derivative string }
var inspectionMetrics map[string]metricSpec
```

- [x] 写测试断言 map 含 30 个设计文档指标，`cr.node.sql.query.count` 为 `avg/sum/none`，所有 derivative 为 none。
- [x] 运行 `go test ./pkg/tools -run TestInspectionMetrics -count=1`，预期失败。
- [x] 实现完整固定 map，不接受调用方自定义指标。
- [x] 验收：列表与设计文档 Decision 7 逐项一致。

### Task 10: 定义 query-metrics 输入与校验

**依赖:** Task 9

**Files:** Modify `pkg/tools/metrics.go`；Test `pkg/tools/metrics_test.go`

**目标代码片段:**
```go
type metricsInput struct { MetricNames []string `json:"metric_names"`; Start, End, Sample int64 `json:"start,omitempty" json:"end,omitempty" json:"sample,omitempty"` }
func validateMetricsInput(input metricsInput) error
```

- [x] 写测试：空列表、未知名返回 `unknown metric: <name>`；合法单指标通过；start/end/sample 边界符合工具约定。
- [x] 运行 `go test ./pkg/tools -run TestMetricsInput -count=1`，预期失败。
- [x] 实现 JSON Schema 所需 metric_names 数组和可选时间字段，逐项 trim/校验。
- [x] 验收：未知指标在发 HTTP 前拒绝，错误文本精确匹配 spec。

### Task 11: 实现 query-metrics 请求构造与执行

**依赖:** Tasks 8–10

**Files:** Modify `pkg/tools/metrics.go`；Test `pkg/tools/metrics_test.go`

**目标代码片段:**
```go
func executeMetricsQuery(ctx context.Context, input metricsInput, headerAdmin, flagAdmin, dbURL string) (any, error)
func registerQueryMetricsTool(s *server.MCPServer)
```

- [x] 写 `httptest.NewServer` 集成测试，断言 POST `/ts/query`、Content-Type、metric payload、返回 `results`。
- [x] 增加 TLS + password Basic Auth、TLS + 无 password fail-fast、insecure 不带 Auth 三个测试。
- [x] 运行 `go test ./pkg/tools -run TestQueryMetrics -count=1`，预期失败。
- [x] 实现按 Admin 公共函数执行请求并解析 `AdminResponse`，成功返回结构化 MCP response。
- [x] 验收：header → flag → DB URL 推导贯穿工具；200/code=-1 返回 `auth failed`。

### Task 12: 完善 query-metrics 工具注册与输入 schema

**依赖:** Task 11

**Files:** Modify `pkg/tools/metrics.go`；Test `pkg/tools/metrics_test.go`

**目标代码片段:**
```go
metricsTool := mcp.NewToolWithRawSchema("query-metrics", description, json.RawMessage(metricsInputSchema))
metricsTool.RawOutputSchema = json.RawMessage(validOutputSchema)
```

- [x] 写注册测试，枚举工具名和 raw schema，确认包含 required `metric_names` 及 start/end/sample 描述。
- [x] 运行 `go test ./pkg/tools -run TestRegisterQueryMetricsTool -count=1`。
- [x] 实现 handler 的 `request.BindArguments`、`X-Database-URI`/`X-Admin-Base-URL` 读取和 MCP 错误包装。
- [x] 验收：工具名精确为 `query-metrics`，不暴露旧查询参数结构。

### Task 13: 定义 query-slow-sql 输入与 statement 类型

**依赖:** Task 8

**Files:** Create `pkg/tools/slow_sql.go`；Test `pkg/tools/slow_sql_test.go`

**目标代码片段:**
```go
type slowSqlInput struct { Limit int `json:"limit,omitempty"`; MinLatencyMS float64 `json:"min_latency_ms,omitempty"`; SortBy string `json:"sort_by,omitempty"` }
type statement struct { ID, Fingerprint, Query string; ServiceLatency, RunLatency, PlanLatency float64; Count int64 }
```

- [x] 写测试：零值表示 limit 10、min latency 0、sort_by service_lat；负 limit/负 latency/未知 sort_by 返回明确错误。
- [x] 运行 `go test ./pkg/tools -run TestSlowSQLInput -count=1`，预期失败。
- [x] 实现输入校验和 raw schema，sort_by 允许 `service_lat/run_lat/plan_lat/count`。
- [x] 验收：默认行为与 spec 的 top 10/service_latency 场景一致。

### Task 14: 实现 statements JSON 解析

**依赖:** Task 13

**Files:** Modify `pkg/tools/slow_sql.go`；Test `pkg/tools/slow_sql_test.go`

**目标代码片段:**
```go
func parseStatementsResponse(body []byte) ([]statement, error)
```

- [x] 用包含 KaiwuDB statements envelope、延迟字段和 count 的 fixture 写成功测试；增加缺字段/非 JSON 测试。
- [x] 运行 `go test ./pkg/tools -run TestParseStatementsResponse -count=1`，预期失败。
- [x] 实现 JSON 解码为内部 statement 列表，兼容 API 的字段命名并对格式错误返回可定位错误。
- [x] 验收：解析结果不丢失原始 query、fingerprint、四个排序字段。

### Task 15: 实现慢 SQL 过滤排序

**依赖:** Task 14

**Files:** Modify `pkg/tools/slow_sql.go`；Test `pkg/tools/slow_sql_test.go`

**目标代码片段:**
```go
func filterAndSortStatements(items []statement, input slowSqlInput) []statement
```

- [x] 写测试：过滤掉 service_latency < 100 的项；分别按 service_lat/run_lat/plan_lat/count 降序；limit 截断且不修改输入切片。
- [x] 运行 `go test ./pkg/tools -run TestFilterAndSortStatements -count=1`，预期失败。
- [x] 实现过滤、稳定降序排序、默认 limit=10，limit 大于结果数返回全部。
- [x] 验收：默认查询返回 top 10，排序字段与输入 sort_by 一一对应。

### Task 16: 实现 query-slow-sql 请求执行

**依赖:** Tasks 8、13–15

**Files:** Modify `pkg/tools/slow_sql.go`；Test `pkg/tools/slow_sql_test.go`

**目标代码片段:**
```go
func executeSlowSQLQuery(ctx context.Context, input slowSqlInput, headerAdmin, flagAdmin, dbURL string) ([]statement, error)
func registerQuerySlowSqlTool(s *server.MCPServer)
```

- [x] 写 httptest 集成测试，断言 GET `/_status/statements`、认证 header、返回过滤排序结果。
- [x] 增加 TLS 无密码不发请求、insecure 有密码不带 Auth、200/code 错误翻译测试。
- [x] 运行 `go test ./pkg/tools -run 'TestQuerySlowSQL|TestExecuteSlowSQL' -count=1`，预期失败。
- [x] 实现调用公共 Admin request、解析 statements、过滤排序和结构化 MCP response。
- [x] 验收：默认 GET 路径精确为 `/_status/statements`，limit/min_latency_ms/sort_by 可用。

### Task 17: 完善 query-slow-sql schema 与 handler

**依赖:** Task 16

**Files:** Modify `pkg/tools/slow_sql.go`；Test `pkg/tools/slow_sql_test.go`

**目标代码片段:**
```go
slowSQLTool := mcp.NewToolWithRawSchema("query-slow-sql", description, json.RawMessage(slowSQLInputSchema))
```

- [x] 写注册测试断言工具名、三个输入属性、sort_by 枚举和 valid output schema。
- [x] 运行 `go test ./pkg/tools -run TestRegisterQuerySlowSqlTool -count=1`。
- [x] 实现 `BindArguments` 错误、Admin header 和 DB URI 读取，错误统一包装为 MCP tool result error。
- [x] 验收：无参数调用等价于 limit 10/service_lat，不引用旧工具配置。

### Task 18: 注册两个新工具并收敛 tools.Config

**依赖:** Tasks 12、17

**Files:** Modify `pkg/tools/tools.go`；Test `pkg/tools/tools_test.go`（若已有则追加）

**目标代码片段:**
```go
func RegisterToolsWithConfig(s *server.MCPServer, config Config) {
    registerReadQueryTool(s); registerWriteQueryTool(s)
    registerQueryMetricsTool(s); registerQuerySlowSqlTool(s)
}
```

- [x] 写注册表测试：工具包含 read-query、write-query、query-metrics、query-slow-sql，不包含 query-metrics-history。
- [x] 运行 `go test ./pkg/tools -run TestRegisterTools -count=1`，预期旧注册仍存在时失败。
- [x] 移除 `DefaultAdminBaseURL` 传递或改为新设计需要的最小配置；调用两个无 config 注册函数。
- [x] 验收：工具注册顺序稳定，编译期没有旧函数引用。

### Task 19: 清理 server 配置

**依赖:** Task 18

**Files:** Modify `pkg/server/server.go`；Test `pkg/server/server_test.go`（如存在则追加）

**目标代码片段:**
```go
type ServerConfig struct { ConnectionString string }
// CreateServerWithConfig 仅向 tools.RegisterToolsWithConfig 传递无 Admin flag 的配置。
```

- [x] 写编译/配置测试，构造 `ServerConfig{ConnectionString: ""}` 并确认 server 可创建。
- [x] 运行 `go test ./pkg/server ./pkg/tools -count=1`，预期旧字段引用失败。
- [x] 删除 `DefaultAdminBaseURL` 字段和注册传递，保留数据库连接行为不变。
- [x] 验收：`CreateServer`、stateless `X-Database-URI` 模式仍可用。

### Task 20: 清理 CLI flag

**依赖:** Task 19

**Files:** Modify `cmd/kwdb-mcp-server/main.go`

**目标代码片段:**
```go
s, err := server.CreateServerWithConfig(server.ServerConfig{ConnectionString: connectionString})
```

- [x] 写/运行 `go test ./cmd/kwdb-mcp-server -count=1` 或 `go build ./cmd/kwdb-mcp-server`，确认入口编译。
- [x] 删除 `adminBaseURL` 变量、`--admin-base-url` flag 和 config 字段传递。
- [x] 验收：`--help` 不再显示旧 flag，HTTP/SSE/stdio 参数不受影响。

### Task 21: 删除旧工具实现与旧设计文档

**依赖:** Task 20

**Files:** Delete `pkg/tools/metrics_history.go`、`pkg/tools/metrics_history_test.go`、`docs/design-metrics-history-tool.md`

**目标验证代码片段:**
```bash
test ! -e pkg/tools/metrics_history.go
test ! -e pkg/tools/metrics_history_test.go
test ! -e docs/design-metrics-history-tool.md
test -z "$(git grep -n 'query-metrics-history' || true)"
```

- [x] 删除三个旧文件并移除所有残余符号/引用。
- [x] 运行上述 shell 验证，预期无输出且三个 `test` 成功。
- [x] 运行 `go test ./...`，修复仅由删除引起的编译引用。
- [x] 验收：源码、测试、文档、注册表均无 `query-metrics-history`。

### Task 22: 更新中文文档和仓库设计说明

**依赖:** Task 21

**Files:** Modify `README_zh.md`; Create `docs/design-tls-inspection-tools.md`

**目标文档片段:**
```markdown
#### 指标查询（query-metrics）
通过 Admin `/ts/query` 查询固定巡检指标；TLS DB URL 使用 Basic Auth。

#### 慢 SQL 查询（query-slow-sql）
通过 `/_status/statements` 查询并按延迟、执行次数过滤排序。
```

- [x] 更新 MCP Tools 章节，删除旧工具示例，新增两个工具的输入示例、`X-Database-URI`/`X-Admin-Base-URL`、TLS 无 password 错误和 insecure 无 Auth 说明。
- [x] 创建中文设计说明，引用技术设计的三级 URL、30 指标、响应 code 和边界场景决策。
- [x] 运行 `git grep -n 'query-metrics-history' -- README_zh.md docs || true`，预期无匹配；检查新工具名和关键错误文本存在。
- [x] 验收：覆盖 spec 中中文 README 场景和迁移提示。

### Task 23: 更新英文 README

**依赖:** Task 22

**Files:** Modify `README.md`

**目标文档片段:**
```markdown
#### query-metrics
Query fixed inspection metrics through the Admin `/ts/query` endpoint.

#### query-slow-sql
Query and filter KaiwuDB slow statements through `/_status/statements`.
```

- [x] 同步工具列表、TLS/Basic Auth、Admin URL 来源和最小调用 JSON 示例；删除旧工具章节。
- [x] 运行 `git grep -n 'query-metrics-history' -- README.md`，预期无匹配；确认两个新工具名均出现。
- [x] 验收：英文 README 与中文 README 的能力边界一致，不宣称 mTLS 支持。

### Task 24: 集成测试与最终验证

**依赖:** Tasks 1–23

**Files:** Test/Modify `pkg/db/dburl_test.go`, `pkg/tools/*_test.go`；验证全仓库

**目标验证命令:**
```bash
go test ./... -race -count=1
go vet ./...
test -z "$(gofmt -l .)"
go build ./...
```

- [x] 运行 `go test ./... -race -count=1`，预期全部 PASS；记录覆盖率 `go test ./pkg/db ./pkg/tools -cover`，目标 ≥80%。
- [x] 运行 `go vet ./...` 和 `gofmt -l .`，预期无警告且无文件列表。
- [x] 用 `httptest` 驱动两个已注册 MCP handler，确认 POST/GET、payload、Basic Auth、insecure 无 Auth 和错误矩阵均通过。
- [x] 若有真实 KaiwuDB 环境，记录 TLS 错误响应的 HTTP status/body code；若行为与假设不同，只调整 `parseAdminResponse` 测试和实现并重新运行全套验证。
- [x] 验收：所有 spec 场景通过、构建通过、覆盖率达标；按项目流程提交最终验证证据。

## 自审清单

- **Spec coverage:** Tasks 1–3 覆盖 DB URL/TLS 全部场景；Tasks 4–8 覆盖 Admin URL、Auth、证书跳过、响应矩阵；Tasks 9–12 覆盖 query-metrics；Tasks 13–17 覆盖 query-slow-sql；Tasks 18–21 覆盖注册、配置、旧工具删除；Tasks 22–23 覆盖 README/设计文档；Task 24 覆盖测试、构建、真实环境验证。
- **边界 coverage:** 明确测试空 password、无 password 段、缺 user、空 dbname、缺端口、大写/混合大小写 query 参数、重复 query 参数、URL-encoded password、未编码多 `@`。
- **Type consistency:** `ParseDBURL`、`DBURLInfo.IsTLS`、`resolveAdminBaseURL`、`buildAuthHeader`、`parseAdminResponse`、`doAdminRequest` 及两个工具的 execute/register 函数在依赖任务中保持同名同签名。
- **No placeholders:** 所有实现任务给出具体路径、函数/结构体、测试命令、代码片段和验收条件；无 TBD/TODO 或“自行处理边界”类步骤。

计划完成后，使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 执行；执行前按 Comet build 阶段要求确认 build mode、TDD、review mode 和隔离方式。
