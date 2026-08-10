## 1. DB URL 解析模块

- [x] 1.1 创建 `pkg/db/dburl.go`,实现 `ParseDBURL(dbURL string) (DBURLInfo, error)` 与 `DBURLInfo` 结构体
- [x] 1.2 创建 `pkg/db/dburl_test.go`,覆盖标准 DB URL、无 password、cert paths、URL-encoded password、非法格式场景
- [x] 1.3 验证 `go test ./pkg/db/... -run TestParseDBURL` 通过

## 2. 公共 Admin 模块

- [x] 2.1 创建 `pkg/tools/admin.go`,实现 `isTLSForAdmin(sslmode, sslcert, sslkey, sslrootcert) bool`
- [x] 2.2 实现 `resolveAdminBaseURL(headerValue, flagValue, dbURL string) (string, error)`,含三级降级逻辑
- [x] 2.3 实现 `buildAuthHeader(password, user string) string`(仅在 isTLS + 有 password 时返回 Basic Auth)
- [x] 2.4 实现 `parseAdminResponse(body []byte) (*AdminResponse, error)`,基于 `code` 字段判定错误
- [x] 2.5 实现 `doAdminRequest(ctx, method, url, body, user, password, isTLS) (*http.Response, []byte, error)`,含 TLS 证书验证跳过
- [x] 2.6 创建 `pkg/tools/admin_test.go`,覆盖 TLS 检测、各降级路径、Auth 决策、响应解析场景

## 3. query-metrics 工具

- [x] 3.1 创建 `pkg/tools/metrics.go`,定义 `inspectionMetrics` 映射(对齐 Python METRICS_MAP,~30 个指标)
- [x] 3.2 实现 `metricsInput` 结构与 JSON Schema(metric_names 数组,可选 start/end/sample)
- [x] 3.3 实现 `registerQueryMetricsTool(s *server.MCPServer)`,调用 `/ts/query` POST API
- [x] 3.4 实现 `executeMetricsQuery` 函数,封装请求构造、响应解析、错误翻译
- [x] 3.5 创建 `pkg/tools/metrics_test.go`,覆盖指标名校验、httptest 集成、Auth header 验证

## 4. query-slow-sql 工具

- [x] 4.1 创建 `pkg/tools/slow_sql.go`,定义 `slowSqlInput` 结构(limit/min_latency_ms/sort_by)
- [x] 4.2 实现 `registerQuerySlowSqlTool(s *server.MCPServer)`,调用 `/_status/statements` GET API
- [x] 4.3 实现 `parseStatementsResponse`,解析 KaiwuDB statements JSON 格式
- [x] 4.4 实现 `filterAndSortStatements` 逻辑(支持 service_lat/run_lat/plan_lat/count 排序)
- [x] 4.5 创建 `pkg/tools/slow_sql_test.go`,覆盖默认查询、limit/min_latency/sort_by 各场景、httptest 集成

## 5. 工具注册与配置清理

- [x] 5.1 修改 `pkg/tools/tools.go`,注册新工具,移除 `registerQueryMetricsHistoryTool` 调用
- [x] 5.2 修改 `pkg/server/server.go`,移除 `ServerConfig.DefaultAdminBaseURL` 字段
- [x] 5.3 修改 `cmd/main.go`,移除 `--admin-base-url` flag
- [x] 5.4 验证 `go build ./...` 通过

## 6. 旧工具清理

- [x] 6.1 删除 `pkg/tools/metrics_history.go`
- [x] 6.2 删除 `pkg/tools/metrics_history_test.go`
- [x] 6.3 删除 `docs/design-metrics-history-tool.md`
- [x] 6.4 验证 `git grep "query-metrics-history"` 无结果

## 7. 文档更新

- [x] 7.1 更新 `README_zh.md`,在 MCP Tools 章节新增 `query-metrics` 与 `query-slow-sql` 描述,删除 `query-metrics-history`
- [x] 7.2 更新 `README.md`,同步英文版本
- [x] 7.3 创建 `docs/design-tls-inspection-tools.md`,记录设计决策与已澄清的边缘场景

## 8. 集成测试与验证

- [x] 8.1 运行 `go test ./... -race -count=1`,所有测试通过
- [x] 8.2 运行 `go vet ./...` 与 `gofmt -l .`,无警告
- [x] 8.3 启动 MCP server,手动验证 `query-metrics` 与 `query-slow-sql` 在 mock admin server 下返回正确数据
- [x] 8.4 在真实 KaiwuDB 实例上验证(若环境可用):记录认证失败时的 HTTP 状态码与响应体 code 字段
- [x] 8.5 根据 8.4 实际行为,修正 `parseAdminResponse` 逻辑(如有不一致)

## 9. bugfix-2026-08-07:secure 集群 + sslmode=disable 导致 query-metrics/query-slow-sql 401

> 集成场景:KGA 在 secure 模式 KaiwuDB 集群上调用,DB URL 用 `sslmode=disable`,admin URL 由 KGA 透传,Go http.Client 跟随 307 跳到 HTTPS,但因为 `effective_isTLS` 误判为 false 而未带 Authorization → 401 "a valid authentication cookie is required"。

- [x] 9.1 `pkg/tools/admin.go::resolveAdminBaseURL` 签名扩展为 `(string, bool, error)`,新增 `adminURLIsHTTPS(url string) bool` 助手,doc comment 说明第三返回值含义
- [x] 9.2 `pkg/tools/metrics.go::executeMetricsQuery` 与 `pkg/tools/slow_sql.go::executeSlowSQLQuery` 用 `effective_isTLS = adminURLIsTLS || isTLSForAdmin(...)` 替换原 `isTLS`,doc comment 更新
- [x] 9.3 `pkg/tools/admin_test.go`:`TestResolveAdminBaseURL` / `TestResolveAdminBaseURLExhaustive` 适配新签名(断言第三返回值),新增 `TestAdminURLIsHTTPS` 覆盖 scheme/whitespace/裸 host:port/空串 6 种 case
- [x] 9.4 `pkg/tools/metrics_test.go::TestExecuteMetricsQuery` 新增 "https admin URL + insecure DB URL still attaches Authorization" 与反向 "http admin URL + insecure DB URL sends no Authorization" 两个子测试
- [x] 9.5 `pkg/tools/slow_sql_test.go::TestExecuteSlowSQLQuery` 新增同上两子测试
- [x] 9.6 跑 `go test ./pkg/tools/...`,164 个测试全部通过,无回归(预先存在的 3 个 integration 测试需外部 server,与本修复无关)
- [x] 9.7 更新 `docs/superpowers/specs/2026-08-04-tls-inspection-tools-design.md` Decision 2/4
- [x] 9.8 更新 `openspec/changes/kwdb-tls-inspection-tools/design.md` Decision 2/4
- [x] 9.9 更新 `openspec/changes/kwdb-tls-inspection-tools/specs/tls-inspection-tools/spec.md` Authorization requirement scenarios(原 3 个改写为基于 `effective_isTLS`,新增 2 个 bugfix 场景)

## 10. bugfix-2026-08-07(dev30 适配:KaiwuDB 企业版 3.3.0 端点路径与认证模型变更)

> 集成场景:KGA 在 KaiwuDB 企业版 3.3.0 dev30 上调用工具。
> 两个根因:
> (a) `/ts/query` 与 `/_status/statements` 在 dev30 已被替换为 `/restapi/ts/query` 与 SQL 系统表 `kwdb_internal.node_statement_statistics`;
> (b) `/restapi/ts/query` 返回 `{"results":[{...,"datapoints":[]}]}` 信封,而 `AdminResponse` 的 `Raw` 字段有 `json:"-"` 标签,被 `data: result` 序列化时整体吞掉。

- [x] 10.1 `pkg/tools/metrics.go::buildTSQueryURL` 路径 `/ts/query` → `/restapi/ts/query`(dev30 实际端点),doc comment 标注 CockroachDB→KaiwuDB 演变
- [x] 10.2 `pkg/tools/metrics.go` handler:`data` 字段从 `*AdminResponse` 改为 `json.RawMessage(adminResp.Raw)`,让 `{"results":[...]}` 信封原样透传
- [x] 10.3 `pkg/tools/metrics_test.go`:`/ts/query` 测试断言 → `/restapi/ts/query`
- [x] 10.4 `pkg/tools/slow_sql.go` 整体重写 `executeSlowSQLQuery`:HTTP 调 `/_status/statements` → SQL 查 `kwdb_internal.node_statement_statistics`;新增 `slowSQLExecuteQueryFn` 注入点、`slowSQLStatementsSortColumn` 排序白名单、`normalizeSQLStatementRow` 字段映射、`stringField`/`int64Field`/`floatField` 类型助手
- [x] 10.5 `pkg/tools/slow_sql_test.go`:4 个 `TestExecuteSlowSQLQuery` 子测试 + 3 个 `TestRegisterQuerySlowSqlTool` 集成子测试全部改用 `slowSQLExecuteQueryFn` stub,移除所有 `httptest` mock
- [x] 10.6 跑 `go vet ./...` 与 `gofmt -l pkg/tools/` 干净;`go test ./pkg/tools/... -count=1` 164 个测试全绿
- [x] 10.7 更新 `openspec/changes/kwdb-tls-inspection-tools/design.md` Decision 4 与 query-slow-sql 描述(SQL 化)
- [x] 10.8 更新 `openspec/changes/kwdb-tls-inspection-tools/specs/tls-inspection-tools/spec.md` `query-slow-sql` Scenario(原 HTTP 场景改为 SQL 场景)
- [x] 10.9 端到端验证:dev30 集群(`10.110.105.65:5000/svt_images/kaiwudb-x86_64-ent-0824dev30:3.3.0`)上 `/restapi/ts/query` 200 OK + `kwdb_internal.node_statement_statistics` 有数据
- [x] 10.10 端到端验证:`query-slow-sql` 在 dev30 集群实跑,返回真实慢 SQL 列表(节点元数据查询 / gossip 检查 / 容量查询等),latency 正确从秒转毫秒,排序/limit/filter 生效
- [x] 10.11 `pkg/tools/slow_sql.go` SQL 占位符 `?` → `$1`/`$2` 修正(KaiwuDB pgx 驱动要求 postgresql 协议风格,否则 `pq: at or near "?": syntax error`),`pkg/tools/slow_sql_test.go` 同步更新断言
