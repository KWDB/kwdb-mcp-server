# 双模式与无状态多租户设计说明

本文档描述 KWDB MCP Server 的**双模式**行为及**无状态多租户**设计，便于 Code Review 与后续维护时对齐实现与文档。

---

## 1. 目标与约束

### 1.1 目标

- **无状态多租户**：去掉启动时必传的数据库 URI，通过每次工具调用时的 **`X-Database-URI`** 请求头选择数据库，实现一个 server 进程服务多用户、多库。
- **兼容老产品**：保留“启动参数带 connection string”的用法，现有单库部署方式无需改动即可继续使用。

### 1.2 约束与不采纳方案

- **不使用** `KWDB_DEFAULT_URI` 等环境变量作为默认连接串；数据库选择仅依赖：
  - 启动参数中的可选连接串（用于默认池），以及
  - 请求头 `X-Database-URI`（用于按请求指定库）。

---

## 2. 双模式行为

| 模式 | 启动方式 | 工具调用时是否必须带 `X-Database-URI` | 行为简述 |
|------|----------|----------------------------------------|----------|
| **单库兼容模式** | 传入连接串（第一个非 flag 参数或 Makefile `CONNECTION_STRING`） | **否** | 服务初始化默认连接池；未带 header 时，`read-query` / `write-query` 使用该默认池。 |
| **无状态多租户模式** | 不传连接串 | **是** | 不预建任何连接池；每次 `read-query` / `write-query` 必须带 `X-Database-URI`，否则返回 `missing X-Database-URI header`。 |

- **资源与提示**：当前表结构资源（如 `kwdb://table/{table_name}`）与 Prompts 仍使用**默认连接池**；仅在无连接串启动时无默认池，访问这些能力会依赖默认池的调用会失败（与“无状态下必须用 header 指定库”的设计一致，未来若需可按需扩展）。
- **StdIO / HTTP / SSE**：三种传输方式均支持上述双模式；HTTP/SSE 下由客户端在每次工具请求的 HTTP 头中携带 `X-Database-URI`。

---

## 3. 架构图（Mermaid）

### 3.1 整体架构与数据流

```mermaid
flowchart TB
    subgraph Client["客户端"]
        Req["工具请求\n(read-query / write-query)"]
        Req -->|"可选: X-Database-URI"| Transport
    end

    subgraph Transport["传输层"]
        Transport["StdIO / HTTP / SSE"]
    end

    subgraph Server["KWDB MCP Server"]
        Main["main.go\n可选 connectionString"]
        Main --> CreateServer["CreateServer(connectionString)"]
        CreateServer -->|"connectionString != \"\""| InitDB["db.InitDB()\n默认连接池"]
        CreateServer --> Tools["tools\nread-query / write-query"]
        CreateServer --> Res["resources / prompts\n(使用默认池)"]

        Tools --> Resolve["resolveDBTarget\n(headerURI, defaultPoolInit)"]
        Resolve --> DB["db 层"]
    end

    subgraph DB["db 层"]
        IsDefault{"默认池已初始化?"}
        UseURI["ExecuteQueryWithURI /\nExecuteWriteQueryWithURI"]
        UseDefault["ExecuteQueryWithContext /\nExecuteWriteWithContext"]
        MultiMgr["MultiPoolManager\n(按 URI 多池)"]
        DefaultPool["默认 PoolManager\n(单库)"]
        UseURI --> MultiMgr
        UseDefault --> DefaultPool
        MultiMgr --> Pools["PoolManager A, B, ..."]
        InitDB --> DefaultPool
    end

    Pools --> KWDB[(KWDB)]
    DefaultPool --> KWDB
```

### 3.2 数据库选择决策流程（resolveDBTarget）

```mermaid
flowchart LR
    Start(["工具请求"]) --> GetHeader["读取 X-Database-URI"]
    GetHeader --> CheckHeader{"header 非空?"}
    CheckHeader -->|是| UseURI["使用该 URI\n(MultiPoolManager)"]
    CheckHeader -->|否| CheckDefault{"默认池已初始化?"}
    CheckDefault -->|是| UseDefault["使用默认池"]
    CheckDefault -->|否| Missing["返回错误\nmissing X-Database-URI header"]
    UseURI --> Exec["执行 SQL"]
    UseDefault --> Exec
```

### 3.3 双模式与连接池关系

```mermaid
flowchart TB
    subgraph Startup["启动时"]
        Args["命令行参数"]
        Args --> HasConn{"传入 connectionString?"}
        HasConn -->|是| Single["单库兼容模式"]
        HasConn -->|否| Stateless["无状态多租户模式"]
        Single --> InitDefault["初始化默认连接池"]
        Stateless --> NoPool["不创建任何池"]
    end

    subgraph Request["每次工具调用"]
        Header["X-Database-URI"]
        Single --> CanOmit["可不带 header\n→ 用默认池"]
        Stateless --> MustHeader["必须带 header\n→ 按 URI 取/建池"]
        MustHeader --> Header
        CanOmit --> Header
    end
```

---

## 4. 数据库选择决策逻辑

工具层（`read-query`、`write-query`）通过统一逻辑决定“本次请求使用哪个数据库”：

1. **若请求头 `X-Database-URI` 非空**：使用该 URI 对应连接（经 `MultiPoolManager` 按需建池），忽略是否存在默认池。
2. **若未带 `X-Database-URI` 且默认池已初始化**：使用默认连接池（单库兼容行为）。
3. **若未带 `X-Database-URI` 且默认池未初始化**：返回错误 `missing X-Database-URI header`，不执行 SQL。

该逻辑封装在 `pkg/tools` 的 **`resolveDBTarget(headerURI, defaultPoolInitialized)`** 中，返回三种状态：`useURI`（用指定 URI）、`useDefault`（用默认池）、`missingHeader`（需返回 missing header 错误）。  
代码依据：`pkg/tools/tools.go`（约 14–24 行：`resolveDBTarget`；约 52–57 行：read-query 使用；write-query 同理）。

---

## 5. 关键组件与代码位置

### 5.1 入口与 Server 创建

- **`cmd/kwdb-mcp-server/main.go`**  
  - 将第一个非 flag 参数作为可选 `connectionString` 传入 `CreateServer`（约 44–51 行）。  
  - 不传参数时 `connectionString` 为空，即无状态多租户模式。
- **`pkg/server/server.go`**  
  - **`CreateServer(connectionString string)`**（约 14–19 行）：  
    - `connectionString != ""` 时调用 `db.InitDB(connectionString)` 初始化默认池；  
    - `connectionString == ""` 时不初始化默认池，工具必须依赖 `X-Database-URI`。

### 5.2 数据库层

- **`pkg/db/multi_pool_manager.go`**  
  - **`MultiPoolManager`**：按数据库 URI 管理多个连接池，用于无状态多租户；按需惰性创建并常驻内存。  
  - **`getOrCreatePool(connectionString)`**：根据 URI 获取或创建 `PoolManager`。  
  - **`ExecuteWithURI(ctx, connectionString, fn)`**：在指定 URI 对应的池上执行回调。  
  - **`CloseAll()`**：关闭所有已创建池；在进程退出或 server 关闭时由 `db.Close()` 调用。
- **`pkg/db/db.go`**  
  - **`InitDB(connectionString)`**：初始化全局默认连接池（单库模式）。  
  - **`ExecuteQueryWithURI(ctx, connectionString, query)`**（约 251 行起）：只读查询，走 `MultiPoolManager`。  
  - **`ExecuteWriteQueryWithURI(ctx, connectionString, query)`**（约 316 行起）：写操作，走 `MultiPoolManager`。  
  - **`IsDefaultPoolInitialized()`**（约 362 行起）：供工具层判断是否存在默认池，用于 `resolveDBTarget` 的第二个参数。
- **`pkg/db/pool_manager.go`**  
  - 单库默认池与多租户按 URI 的池均基于 `PoolManager`；多租户池由 `MultiPoolManager` 按 URI 键管理。

### 5.3 工具层

- **`pkg/tools/tools.go`**  
  - **`resolveDBTarget(headerURI, defaultPoolInitialized)`**（约 14–24 行）：实现上述三步决策。  
  - **read-query**（约 47–57 行）：从 `request.Header.Get("X-Database-URI")` 取 header，调用 `resolveDBTarget`；若 `missingHeader` 则返回 `mcp.NewToolResultError("missing X-Database-URI header")`；否则按 `useURI` / 默认池分别调用 `ExecuteQueryWithURI` 或 `ExecuteQueryWithContext`。  
  - **write-query**：逻辑与 read-query 对称，使用 `ExecuteWriteQueryWithURI` 或 `ExecuteWriteWithContext`。

---

## 6. 测试与文档

- **单元测试**：`pkg/tools/tools_test.go` 中对 `resolveDBTarget` 的三种分支（有 header、无 header 有默认池、无 header 无默认池）进行了覆盖。
- **集成测试**：在 HTTP / StdIO / SSE 测试中，增加“不带 `X-Database-URI`”的用例，**仅断言**无 header 时得到的是工具级结果（即**不是** transport error）；若工具返回错误，会 `Logf` 错误内容便于排查，但**不**断言错误文案必须包含 `missing X-Database-URI header`。在需要连接串的用例中，对 read-query / write-query 及清理步骤统一设置 `X-Database-URI`。
- **文档**：  
  - README / README_zh：在“启动 KWDB MCP Server”处补充双模式说明及无连接串启动时的 `X-Database-URI` 约束；各传输方式示例中注明无状态模式下的 header 要求。  
  - 集成文档（integrate-llm-agent*.md）：说明无连接串启动时，调用 read-query / write-query 需携带 `X-Database-URI`。  
  - 排障文档（troubleshooting*.md）：在“数据库连接失败”中增加无状态模式下 header 缺失/错误的排查步骤。

---

## 7. 小结

- **实现**：通过可选启动连接串 + 请求头 `X-Database-URI` 实现双模式；数据库选择逻辑集中在 `resolveDBTarget`，工具与 DB 层分工清晰。  
- **兼容性**：旧命令（带连接串启动）在单库兼容模式下仍有效，无破坏性变更。  
- **文档**：双模式、`X-Database-URI` 约束及无状态下的排障路径已写入 README、集成文档与排障文档，与实现一致。
