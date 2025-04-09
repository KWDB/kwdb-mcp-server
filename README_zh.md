# KWDB MCP 服务器

一个用于与 KWDB（KaiwuDB）数据库交互的 MCP 服务器。

## 概述

KWDB MCP 服务器通过 MCP 协议提供一套工具和资源，用于与 KWDB（KaiwuDB）数据库交互。它支持读取和写入操作，允许您查询数据、修改数据以及执行 DDL 操作。

## 功能特性

- **读取操作**：执行 SELECT、SHOW、EXPLAIN 和其他只读查询
- **写入操作**：执行 INSERT、UPDATE、DELETE 和 DDL 操作，如 CREATE、DROP、ALTER
- **数据库信息**：获取数据库信息，包括表及其架构
- **语法指南**：通过提示访问 KWDB（KaiwuDB）的综合语法指南
- **标准化 API 响应**：所有操作的一致 JSON 响应格式
- **自动 LIMIT**：通过自动为没有 LIMIT 子句的 SELECT 查询添加 LIMIT 20 来防止大型结果集

## 安装

### 前提条件

- Go 1.23 或更高版本
- 访问 KWDB（KaiwuDB）数据库
- 安装支持 MCP 的 VSCode AI 扩展（如 Cline）

### 安装步骤

1. 克隆仓库：
   ```bash
   git clone https://gitee.com/kwdb/mcp-kwdb-server-go.git
   cd mcp-kwdb-server-go
   ```

2. 安装依赖：
   ```bash
   make deps
   ```

3. 构建应用：
   ```bash
   make build
   ```

## 使用方法

使用 PostgreSQL 连接字符串运行服务器：

```bash
./bin/kwdb-mcp-server "postgresql://username:password@hostname:port/database?sslmode=disable"
```

或使用 Makefile：

```bash
CONNECTION_STRING="postgresql://username:password@hostname:port/database?sslmode=disable" make run
```

### 传输模式

服务器支持两种传输模式：

#### 标准 I/O 模式（默认）

这是默认模式，使用标准输入/输出进行通信：

```bash
./bin/kwdb-mcp-server "postgresql://username:password@hostname:port/database?sslmode=disable"
```

#### SSE 模式（HTTP 上的服务器发送事件）

对于远程访问，您可以在 SSE 模式下运行服务器。请注意，您仍需要提供数据库连接字符串作为最后一个参数：

```bash
./bin/kwdb-mcp-server -t sse -addr ":8080" -base-url "http://localhost:8080" "postgresql://username:password@hostname:port/database?sslmode=disable"
```

或使用 Makefile：

```bash
CONNECTION_STRING="postgresql://username:password@hostname:port/database?sslmode=disable" ADDR=":8080" BASE_URL="http://localhost:8080" make run-sse
```

选项：
- `-t` 或 `-transport`：传输类型（`stdio` 或 `sse`）
- `-addr`：SSE 模式的监听地址（默认：`:8080`）
- `-base-url`：SSE 模式的基础 URL（默认：`http://localhost:8080`）

## 与 LLM Agent 集成

KWDB 服务器设计为可与任何支持 MCP 协议的 LLM Agent 配合使用。以下是使用 Cline 的示例，但类似步骤也适用于其他兼容 MCP 的 Agent。

### 示例：与 Cline 集成

1. 确保您在 VSCode 中安装了 Cline

2. 将 MCP 服务器配置添加到您的 VSCode 设置中。打开 VSCode Cline > MCP Servers > Installed > Configure MCP Servers 并添加：

#### 标准 I/O 模式

```json
"mcpServers": {
  "kwdb-server": {
    "command": "/path/to/bin/kwdb-mcp-server",
    "args": [
      "postgresql://username:password@host:port/database"
    ],
    "disabled": false,
    "autoApprove": []
  }
}
```

#### SSE 模式

首先，在 SSE 模式下启动服务器：

```bash
./bin/kwdb-mcp-server -t sse -addr ":8080" -base-url "http://localhost:8080" "postgresql://username:password@hostname:port/database?sslmode=disable"
```

然后，配置 Cline 连接到运行中的服务器：

```json
"mcpServers": {
  "kwdb-server-sse": {
    "url": "http://localhost:8080",
    "disabled": false,
    "autoApprove": []
  }
}
```

3. 从 VSCode Cline > MCP Servers > Installed > kwdb-server > Restart Server 重启服务器

### 与其他兼容 MCP 的 Agent 集成

对于其他支持 MCP 协议的 LLM Agent，请参考它们的具体文档了解如何配置 MCP 服务器。关键要求是：

- 标准 I/O 模式：能够使用命令行参数执行 KWDB 服务器二进制文件
- SSE 模式：能够连接到实现 MCP 协议的 HTTP 端点

## Tools

服务器提供以下Tools：

### read-query

执行 SELECT、SHOW、EXPLAIN 和其他只读查询。没有 LIMIT 子句的 SELECT 查询将自动添加 LIMIT 20 以防止大型结果集。

示例：
```sql
SELECT * FROM users LIMIT 10;
SHOW TABLES;
EXPLAIN ANALYZE SELECT * FROM orders WHERE user_id = 1;
```

### write-query

执行数据修改查询，包括 DML 和 DDL 操作。

示例：
```sql
INSERT INTO users (name, email) VALUES ('John Doe', 'john@example.com');
UPDATE users SET email = 'new-email@example.com' WHERE id = 1;
DELETE FROM users WHERE id = 1;
CREATE TABLE products (id SERIAL PRIMARY KEY, name TEXT, price DECIMAL);
ALTER TABLE products ADD COLUMN description TEXT;
DROP TABLE products;
```

## Prompts

服务器提供以下Prompts：
| Prompt Name | Description |
|------------|-------------|
| db_description | KWDB（KaiwuDB）数据库的综合描述，包括其功能、特性和用例。 |
| syntax_guide | KWDB（KaiwuDB）的综合语法指南，包括常见查询示例和最佳实践。 |
| cluster_management | KWDB 集群管理的综合指南，包括节点管理、负载均衡和监控。 |
| data_migration | 数据迁移到 KWDB 和从 KWDB 迁移的指南，包括导入/导出方法和最佳实践。 |
| installation | 在各种环境中安装和部署 KWDB 的分步指南。 |
| performance_tuning | 优化 KWDB 性能的指南，包括查询优化、索引策略和系统级调优。 |
| troubleshooting | 诊断和解决常见 KWDB 问题和错误的指南。 |
| backup_restore | 备份和恢复 KWDB 数据库的综合指南，包括策略、工具和最佳实践。 |
| dba_template | 提示词编写模板。 |


## Resources

服务器提供以下Resources：

- `kwdb://product_info`: 产品信息，包括版本和功能
- `kwdb://db_info/{database_name}`：特定数据库的信息，包括引擎类型、注释和表
- `kwdb://table/{table_name}`：特定表的架构，包括列和示例查询

## API 响应格式

所有 API 响应遵循一致的 JSON 结构：

```json
{
  "status": "success",  // 或 "error"
  "type": "query_result",  // 响应类型
  "data": { ... },  // 响应数据
  "error": null  // 错误信息，成功时为 null
}
```

### 成功响应示例

```json
{
  "status": "success",
  "type": "query_result",
  "data": {
    "result_type": "table",
    "columns": ["id", "name", "email"],
    "rows": [
      {"id": 1, "name": "John", "email": "john@example.com"},
      {"id": 2, "name": "Jane", "email": "jane@example.com"}
    ],
    "metadata": {
      "affected_rows": 0,
      "row_count": 2,
      "query": "SELECT * FROM users LIMIT 2"
    }
  },
  "error": null
}
```

### 错误响应示例

```json
{
  "status": "error",
  "type": "error_response",
  "data": null,
  "error": {
    "code": "SYNTAX_ERROR",
    "message": "Query error: syntax error at or near \"SLECT\"",
    "details": "syntax error at or near \"SLECT\"",
    "query": "SLECT * FROM users"
  }
}
```

## 项目结构

```
mcp-kwdb-server-go/
├── cmd/
│   └── kwdb-mcp-server/
│       └── main.go           # 主应用入口点
├── pkg/
│   ├── db/
│   │   └── db.go             # 数据库操作
│   ├── prompts/
│   │   ├── prompts.go        # MCP 提示词
│   │   └── docs/             # 提示的 Markdown 文件
│   │       ├── ReadExamples.md     # 读取查询示例
│   │       ├── WriteExamples.md    # 写入查询示例
│   │       ├── DBDescription.md    # 数据库描述
│   │       ├── SyntaxGuide.md      # SQL 语法指南
│   │       ├── ClusterManagementGuide.md # 集群管理指南
│   │       ├── DataMigrationGuide.md    # 数据迁移指南
│   │       ├── InstallationGuide.md      # 安装指南
│   │       ├── PerformanceTuningGuide.md # 性能调优指南
│   │       ├── TroubleShootingGuide.md   # 故障排除指南
│   │       ├── BackupRestoreGuide.md     # 备份和恢复指南
│   │       └── DBATemplate.md            # 数据库管理模板
│   ├── resources/
│   │   └── resources.go      # MCP 资源
│   ├── server/
│   │   └── server.go         # 服务器设置
│   └── tools/
│       └── tools.go          # MCP 工具
├── Makefile                  # 构建和运行命令
└── README.md                 # 本文件
```

## 维护提示内容

提示内容存储在位于 `pkg/prompts/docs/` 目录的 Markdown 文件中。这些文件在编译时使用 Go 的 `embed` 包嵌入到二进制文件中。

### Markdown 文件结构

以下 Markdown 文件用于提示：

- `pkg/prompts/docs/ReadExamples.md`：包含读取查询示例（SELECT 语句）
- `pkg/prompts/docs/WriteExamples.md`：包含写入查询示例（INSERT、UPDATE、DELETE、CREATE、ALTER）
- `pkg/prompts/docs/DBDescription.md`：包含数据库描述
- `pkg/prompts/docs/SyntaxGuide.md`：包含 SQL 语法指南
- `pkg/prompts/docs/ClusterManagementGuide.md`：包含集群管理指南
- `pkg/prompts/docs/DataMigrationGuide.md`：包含数据迁移指南
- `pkg/prompts/docs/InstallationGuide.md`：包含安装指南
- `pkg/prompts/docs/PerformanceTuningGuide.md`：包含性能调优指南
- `pkg/prompts/docs/TroubleShootingGuide.md`：包含故障排除指南
- `pkg/prompts/docs/BackupRestoreGuide.md`：包含备份和恢复指南
- `pkg/prompts/docs/DBATemplate.md`：包含数据库管理模板

### 如何修改Prompts内容


1. 编辑 `pkg/prompts/docs/` 目录中的相应 Markdown 文件。
2. 使用 `make build` 重新构建应用程序。
3. 新内容将嵌入到二进制文件中。

### 添加新的Prompts


1. 在 `pkg/prompts/docs/` 中创建一个新的 Markdown 文件，例如 `NewUseCase.md`
2. 在 `pkg/prompts/prompts.go` 中添加变量和加载代码
3. 为新提示创建注册函数
4. 将注册函数调用添加到 `registerUseCasePrompts()`
5. 更新 README.md 以记录新提示

有关详细说明，请参阅 `pkg/prompts/prompts.go` 中的注释。

## 安全性

KWDB 服务器实现以下安全措施：

- 读取和写入操作的单独工具
- 验证查询以确保它们与预期的操作类型匹配
- 未授权操作的清晰错误消息

## 未来增强

- [ ] **查询历史**：实现查询历史功能
- [x] **远程模式**：支持连接到远程MCP服务器
- [x] **改进的优化建议**：增强查询优化建议
- [ ] **指标资源**：添加数据库指标信息

## 故障排除

如果遇到问题：

1. 验证数据库连接字符串是否正确
2. 确保数据库服务器可从您的机器访问
3. 检查数据库用户是否具有足够的权限
4. 验证您的 Agent MCP 服务器配置
5. 检查是否有可能阻塞端口的现有 kwdb-mcp-server 进程

### SSE 模式特定问题

1. **连接被拒绝**：确保服务器正在运行并监听指定地址
2. **CORS 错误**：如果从 Web 浏览器访问，确保服务器的基础 URL 与您连接的 URL 匹配
3. **网络问题**：检查防火墙或网络配置是否阻止连接
4. **数据库连接**：服务器仍需要连接到数据库，因此确保数据库可从服务器位置访问

## 贡献

欢迎贡献！请随时提交issue和PR。

## 许可证

本项目根据 MIT 许可证授权。

## 致谢

- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) - MCP Go 服务器框架
- [lib/pq](https://github.com/lib/pq) - PostgreSQL Go 驱动程序