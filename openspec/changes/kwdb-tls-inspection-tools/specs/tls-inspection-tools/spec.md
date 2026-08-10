## ADDED Requirements

### Requirement: DB URL 解析模块

MCP server MUST 提供 `pkg/db/dburl.go` 模块,解析 `postgresql://` 协议 DB URL,提取 host/port/user/password/sslmode/cert 路径。

#### Scenario: 标准 DB URL 解析
- **WHEN** 输入 `postgresql://root:secret@kwdb1:26257/db?sslmode=require`
- **THEN** 返回 `DBURLInfo{User:"root", Password:"secret", Host:"kwdb1", Port:26257, Database:"db", SSLMode:"require", HasPassword:true}`

#### Scenario: 无 password DB URL
- **WHEN** 输入 `postgresql://root@kwdb1:26257/db?sslmode=disable`
- **THEN** 返回 `DBURLInfo{User:"root", Password:"", HasPassword:false, SSLMode:"disable"}`

#### Scenario: 含 cert paths 的 DB URL
- **WHEN** 输入 `postgresql://root@kwdb1:26257/db?sslmode=verify-ca&sslcert=/c&sslkey=/k&sslrootcert=/ca`
- **THEN** 返回的 `DBURLInfo.SSLCert`/`SSLKey`/`SSLRootCert` 字段正确填充

#### Scenario: URL-encoded password
- **WHEN** 输入 `postgresql://root:Kaiwudb%40123@host:26257/db`
- **THEN** 返回 `Password:"Kaiwudb@123"`(URL-decode 后)

#### Scenario: 无效 DB URL 格式
- **WHEN** 输入 `not-a-url`
- **THEN** 返回明确的 `error`(`invalid DB URL: ...`)

#### Scenario: 大写 query 参数不识别
- **WHEN** 输入 `postgresql://root:secret@host:26257/db?SSLMODE=require`(query 参数大写)
- **THEN** 返回 `DBURLInfo{SSLMode:"", ...}`(`sslmode` 视为未设置)

#### Scenario: 空 password
- **WHEN** 输入 `postgresql://root:@host:26257/db`(空 password)
- **THEN** 返回 `DBURLInfo{Password:"", HasPassword:false}`

#### Scenario: 缺 user
- **WHEN** 输入 `postgresql://host:26257/db`(无 user)
- **THEN** 返回错误 `missing user`

#### Scenario: 空 dbname
- **WHEN** 输入 `postgresql://root:secret@host:26257/`(空 dbname)
- **THEN** 返回错误 `empty database name`

#### Scenario: 缺端口默认 26257
- **WHEN** 输入 `postgresql://root:secret@host/db`(无 port)
- **THEN** 返回 `DBURLInfo{Port:26257}`

### Requirement: Admin URL 三级降级

`query-metrics` 与 `query-slow-sql` MUST 按以下优先级解析 admin URL:① `X-Admin-Base-URL` header → ② `--admin-base-url` flag → ③ 从 DB URL 推导。

#### Scenario: 显式 header 优先
- **WHEN** 请求同时携带 `X-Admin-Base-URL: https://custom:9090` 与 DB URL
- **THEN** 使用 `https://custom:9090`,DB URL host 不参与推导

#### Scenario: DB URL 推导 https
- **WHEN** DB URL `sslmode=require`,无显式 admin URL
- **THEN** 推导为 `https://<DB URL host>:8080`

#### Scenario: DB URL 推导 http
- **WHEN** DB URL `sslmode=disable`,无显式 admin URL
- **THEN** 推导为 `http://<DB URL host>:8080`

#### Scenario: 完全缺省
- **WHEN** 无 `X-Admin-Base-URL`、无 `--admin-base-url`、无 DB URL
- **THEN** 返回错误 `missing admin URL and database URI`

### Requirement: TLS 检测算法

`pkg/tools/admin.go` MUST 提供 `isTLSForAdmin(sslmode, sslcert, sslkey, sslrootcert) bool`,基于 KaiwuDB DB URL `sslmode` 语义判断 admin 端点是否启用 TLS。

#### Scenario: 明确 TLS 模式
- **WHEN** sslmode 为 `require`、`verify-ca` 或 `verify-full`
- **THEN** 返回 `true`

#### Scenario: 明确 insecure 模式
- **WHEN** sslmode 为 `disable`、空字符串或未知值
- **THEN** 返回 `false`

#### Scenario: 模糊模式按 insecure 兜底
- **WHEN** sslmode 为 `allow` 或 `prefer`
- **THEN** 返回 `false`(按 insecure 兜底,不发送 Authorization)

### Requirement: Authorization header 决策

工具 MUST 根据 `effective_isTLS = adminURLIsTLS || IsTLS(dbURL)`(bugfix-2026-08-07 引入的 OR 组合)与 `HasPassword` 联合决定是否发送 `Authorization: Basic` header。

#### Scenario: effective_isTLS=true 且 DB URL 有 password
- **WHEN** `effective_isTLS=true` 且 DB URL 包含 password
- **THEN** HTTP 请求包含 `Authorization: Basic <base64(user:password)>`

#### Scenario: effective_isTLS=true 且 DB URL 无 password
- **WHEN** `effective_isTLS=true` 且 DB URL 无 password
- **THEN** 工具返回 MCP 错误 `TLS admin endpoint requires credentials, but DB URL has no password`,不发送 HTTP 请求

#### Scenario: effective_isTLS=false 任意 password 状态
- **WHEN** `effective_isTLS=false`(无论 DB URL 是否含 password)
- **THEN** HTTP 请求不包含 `Authorization` header

#### Scenario: https admin URL + insecure DB URL(bugfix-2026-08-07)
- **WHEN** admin URL scheme 为 `https://` 且 DB URL `sslmode=disable`(无 cert)
- **THEN** `effective_isTLS=true`,DB URL 含 password 时发送 `Authorization: Basic ...` header;覆盖 secure 集群 + `sslmode=disable` 的合法场景,避免被 401 拒掉

#### Scenario: http admin URL + TLS DB URL(保守:DB 已认证,bugfix-2026-08-07)
- **WHEN** admin URL scheme 为 `http://` 且 DB URL `sslmode=require`
- **THEN** `effective_isTLS=true`,DB URL 含 password 时发送 `Authorization: Basic ...` header;保守路径,即使 admin URL 写法错误也仍走认证

### Requirement: TLS 证书验证跳过

工具 MUST 硬编码跳过 TLS 证书验证(`InsecureSkipVerify: true`),匹配 Python `ssl._create_unverified_context()` 行为,适配 KaiwuDB 自签名证书场景。

#### Scenario: TLS 自签名证书请求成功
- **WHEN** admin URL 为 `https://self-signed-host:8081`,且凭据正确
- **THEN** HTTP 请求成功,跳过证书验证不报错

### Requirement: query-metrics 工具

MCP server MUST 注册 `query-metrics` 工具,封装 KaiwuDB `/ts/query` REST API,基于固定 METRICS_MAP 子集。

#### Scenario: 接受指标名查询
- **WHEN** 调用 `query-metrics` 传入指标名列表(如 `["cr.node.sql.query.count"]`)
- **THEN** 工具向 `/ts/query` POST 请求,返回时序数据

#### Scenario: 未知指标拒绝
- **WHEN** 传入不在 METRICS_MAP 的指标名
- **THEN** 工具返回 MCP 错误 `unknown metric: <name>`

#### Scenario: 响应解析
- **WHEN** admin API 返回 HTTP 200 + `{"code":0,"results":...}`
- **THEN** 工具返回成功响应,包含 results 数组

#### Scenario: 认证错误翻译
- **WHEN** admin API 返回 HTTP 200 + `{"code":-1,"desc":"wrong username or password"}`
- **THEN** 工具返回 MCP 错误 `auth failed: wrong username or password`

### Requirement: query-slow-sql 工具

bugfix-2026-08-07: KaiwuDB 企业版 3.3.0 移除了 `/_status/statements` admin REST endpoint。`query-slow-sql` 改为通过 SQL 端口查 `kwdb_internal.node_statement_statistics` 系统表,该表存储每个节点的语句执行统计(key、count、`*_lat_avg` 三个延迟字段)。认证走 SQL 端口的 DB URL 用户名密码,无需 admin URL。SQL 占位符用 postgresql 协议的 `$1`/`$2` 形式(KaiwuDB pgx 驱动不接受 `?`)。排序字段走白名单 `slowSQLStatementsSortColumn` 防止注入。

#### Scenario: 默认查询
- **WHEN** 调用 `query-slow-sql` 不传参数
- **THEN** 工具 SQL 查 `kwdb_internal.node_statement_statistics`,返回 top 10 慢 SQL(按 service_lat_avg 排序,毫秒)

#### Scenario: 自定义 limit
- **WHEN** 传入 `limit=20`
- **THEN** 返回 top 20 慢 SQL

#### Scenario: 最小延迟过滤
- **WHEN** 传入 `min_latency_ms=100`
- **THEN** WHERE 过滤掉 service_lat_avg < 0.1s(=100ms)的语句

#### Scenario: sort_by=count 按执行次数排序
- **WHEN** 传入 `sort_by=count`
- **THEN** ORDER BY count DESC,取执行次数最多的 top N

#### Scenario: 缺失 X-Database-URI
- **WHEN** 请求无 `X-Database-URI` header 且无 flag admin URL
- **THEN** 工具返回 MCP 错误,信息明确提及 `X-Database-URI`(因为 SQL 路径必须靠 DB URL 连接,不能从 admin URL 派生)

### Requirement: 响应解析器

`pkg/tools/admin.go` MUST 提供 `parseAdminResponse(body []byte) (*AdminResponse, error)`,基于 body `code` 字段判定错误。

#### Scenario: 成功响应
- **WHEN** body 为 `{"code":0,"results":...}`
- **THEN** 返回 `AdminResponse{Code:0}`,无错误

#### Scenario: 认证失败
- **WHEN** body 为 `{"code":-1,"desc":"wrong username or password"}`
- **THEN** 返回 error `auth failed: wrong username or password`

#### Scenario: 业务错误
- **WHEN** body 为 `{"code":42,"desc":"missing parameter: foo"}`
- **THEN** 返回 error `missing parameter: foo`

#### Scenario: 非 JSON 响应
- **WHEN** body 为纯文本或 HTML
- **THEN** 返回 error `unexpected response format: <前 200 字节>`

### Requirement: 测试覆盖

新代码 MUST 包含完整单元测试 + 集成测试,目标覆盖率 ≥ 80%。

#### Scenario: 单元测试覆盖
- **WHEN** 运行 `go test ./pkg/tools/... ./pkg/db/...`
- **THEN** 所有 DB URL 解析、TLS 检测、Auth 决策、METRICS_MAP 校验、过滤排序用例通过

#### Scenario: 集成测试
- **WHEN** 使用 `httptest.NewServer` mock KaiwuDB admin API
- **THEN** 工具正确发送 Basic Auth header、payload,正确解析响应

### Requirement: 删除旧工具

MCP server MUST 完全移除 `query-metrics-history` 工具及其相关文件。

#### Scenario: 工具注册表
- **WHEN** 编译后运行 `git grep "query-metrics-history"`
- **THEN** 无任何匹配项

#### Scenario: 文件清理
- **WHEN** 检查 `pkg/tools/`
- **THEN** 不存在 `metrics_history.go` 或 `metrics_history_test.go`

#### Scenario: 文档清理
- **WHEN** 检查 `docs/`
- **THEN** 不存在 `design-metrics-history-tool.md`

### Requirement: README 更新

`README.md` 与 `README_zh.md` MUST 更新工具列表,移除 `query-metrics-history`,新增 `query-metrics` 与 `query-slow-sql` 说明。

#### Scenario: 中文 README
- **WHEN** 阅读 `README_zh.md` 的"MCP Tools"章节
- **THEN** 包含 `query-metrics` 与 `query-slow-sql` 的使用示例,且无 `query-metrics-history` 字样

#### Scenario: 英文 README
- **WHEN** 阅读 `README.md` 的"MCP Tools"章节
- **THEN** 包含两个新工具的英文描述,且无 `query-metrics-history` 字样
