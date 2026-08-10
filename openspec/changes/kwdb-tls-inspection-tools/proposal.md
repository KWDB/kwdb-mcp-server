## Why

现有的 `query-metrics-history` 工具不支持 TLS 模式部署的 KaiwuDB 数据库,且无法调用 KaiwuDB admin 端口的巡检 REST API(`/ts/query`、`/_status/statements`)。KGA 工程通过 Python skill 实现了该能力,但 Python skill 不支持 Basic Auth 用户名密码认证,无法对接 TLS 部署的 KaiwuDB。

本 change 完整移除旧的 `query-metrics-history`,基于 KGA 的 Python 脚本(`get_kwdb_ts_metrics.py`、`get_kwdb_statements.py`)实现两个新的 LLM Agent Tools,通过 MCP server 已保存的 DB URL 派生 Basic Auth 凭据,支持 TLS 部署与 insecure 部署两种形态。

> **bugfix-2026-08-07**: `query-metrics` 端点路径在 KaiwuDB 企业版 3.3.0 dev30 已从 `/ts/query` 改为 `/restapi/ts/query`;`query-slow-sql` 改走 SQL 系统表 `kwdb_internal.node_statement_statistics`(原 `/_status/statements` admin endpoint 在 3.3.0 已移除)。详见 `tasks.md` §10。

## What Changes

- **删除** `pkg/tools/metrics_history.go` 及其测试文件 `pkg/tools/metrics_history_test.go`
- **删除** 设计文档 `docs/design-metrics-history-tool.md`
- **新增** `pkg/tools/metrics.go` —— `query-metrics` 工具,封装 KaiwuDB `/restapi/ts/query` REST API(bugfix-2026-08-07 修正路径)
- **新增** `pkg/tools/slow_sql.go` —— `query-slow-sql` 工具,SQL 查 `kwdb_internal.node_statement_statistics` 系统表(bugfix-2026-08-07 从 `/_status/statements` 重写)
- **新增** `pkg/tools/admin.go` —— 公共模块:DB URL 解析、Basic Auth 派生、Admin URL 推导、HTTP 客户端封装
- **新增** `pkg/db/dburl.go` —— 公开 DB URL 解析辅助函数(`ParseDBURL` 解析 `postgresql://` 协议)
- **新增** `pkg/tools/metrics_test.go`、`pkg/tools/slow_sql_test.go`、`pkg/tools/admin_test.go`、`pkg/db/dburl_test.go` —— 单元 + httptest 集成测试(slow-sql 改用 `slowSQLExecuteQueryFn` stub)
- **修改** `pkg/tools/tools.go` —— 注册新工具,移除旧工具注册
- **修改** `cmd/main.go` —— 移除 `--admin-base-url` flag(由 DB URL 推导)
- **修改** `pkg/server/server.go` —— 移除 `ServerConfig.DefaultAdminBaseURL`(由 DB URL 推导)
- **修改** `README.md`、`README_zh.md` —— 更新工具列表,移除 `query-metrics-history` 描述
- **新增** `docs/design-tls-inspection-tools.md` —— 新工具集的设计文档

**BREAKING** 移除 `query-metrics-history` 工具,使用该工具的 client 将收到 MCP "tool not found" 错误。

## Capabilities

### New Capabilities
- `tls-inspection-tools`: 提供 `query-metrics` 与 `query-slow-sql` 两个 LLM Agent Tools,支持 TLS 部署与 insecure 部署两种形态的 KaiwuDB 巡检能力,包括 Basic Auth 认证、DB URL 解析、Admin URL 推导、TLS 检测算法、响应错误解析。

### Modified Capabilities
- 无。`read-query` 与 `write-query` 的需求不变。

## Impact

**影响模块**:
- `pkg/tools/` —— 工具实现层
- `pkg/server/` —— 服务器配置
- `pkg/db/` —— DB URL 解析辅助
- `cmd/` —— 启动参数
- `docs/` —— 设计文档
- `README.md` / `README_zh.md` —— 用户文档

**不影响**:
- `pkg/prompts/` —— 不修改任何 prompt
- `pkg/resources/` —— 不修改
- `read-query` / `write-query` —— 不修改

**API 变更**:
- **删除** MCP tool `query-metrics-history`(破坏性变更)
- **新增** MCP tool `query-metrics`
- **新增** MCP tool `query-slow-sql`
- **删除** 命令行 flag `--admin-base-url`(由 DB URL 自动推导)

**依赖**:
- 无新增外部依赖(使用 Go 标准库 `net/http`、`encoding/base64`、`net/url`)
- 现有依赖 `github.com/mark3labs/mcp-go` 保持不变
