# 历史指标查询工具设计说明

本文档描述 `query-metrics-history` 的**已实现设计**，用于对齐代码、测试与文档，不再讨论候选方案或建议方案。

---

## 1. 目标

当前 `kwdb-mcp-server` 已提供 SQL 读写工具，但巡检中的 QPS、CPU、内存和部分延迟分析需要时间序列能力。  
为此，服务新增 `query-metrics-history`，用于封装数据库 admin 端点 `/ts/query`，并对外提供统一的 MCP tool 接口。

该实现解决两个问题：

- 将数据库 admin HTTP 地址作为一等输入引入服务端。
- 将 `/ts/query` 的纳秒时间戳和数字枚举转换为更适合 MCP/LLM 使用的毫秒时间戳和字符串参数。

---

## 2. 已实现范围

当前实现包含以下能力：

- 新增 MCP tool：`query-metrics-history`
- 新增启动参数：`--admin-base-url`
- 新增请求头：`X-Admin-Base-URL`
- 支持将毫秒时间戳转换为 `/ts/query` 所需的纳秒字段
- 支持将字符串聚合参数转换为 `/ts/query` 的数字枚举
- 支持兼容解析 camelCase 与 snake_case 两种响应字段
- 支持将空 `datapoints` 归一化为合法空序列，并标记 `empty_series = true`

当前**未实现**以下能力：

- 不支持从 `X-Database-URI` 或数据库连接串自动推导 admin 地址
- 不支持 QPS/CPU 等业务别名，例如 `db.qps`
- 不支持根据采样窗口自动压缩点数
- 未新增 HTTP / SSE / StdIO 的 `query-metrics-history` 集成测试

---

## 3. Admin 地址选择规则

`query-metrics-history` 与 SQL 连接选择解耦，单独按以下顺序决定 admin 地址：

1. 若本次请求带 `X-Admin-Base-URL`，优先使用该值。
2. 否则，若启动时提供了 `--admin-base-url`，使用该默认值。
3. 若两者都没有，返回工具级错误：`missing X-Admin-Base-URL header`。

当前实现不会从数据库 URI 自动推导 admin 地址。这样做是为了避免 SQL 地址与 admin 地址端口、协议或代理路径不一致时产生误路由。

---

## 4. Tool 输入

`query-metrics-history` 使用原始 JSON Schema 注册，输入字段如下：

```json
{
  "start_ms": 1775035140000,
  "end_ms": 1775035740000,
  "sample_ms": 60000,
  "queries": [
    {
      "name": "cr.node.sql.query.count",
      "downsampler": "avg",
      "source_aggregator": "sum",
      "derivative": "rate"
    }
  ]
}
```

字段说明：

- `start_ms` / `end_ms`：Unix 毫秒时间戳
- `sample_ms`：采样间隔，单位毫秒
- `queries[].name`：数据库 `/ts/query` 的实际指标名
- `queries[].downsampler`：`avg|sum|max|min`
- `queries[].source_aggregator`：`avg|sum|max|min`
- `queries[].derivative`：`none|rate|non_negative_rate|non_negative_derivative`

---

## 5. 到 `/ts/query` 的参数映射

服务端将 MCP 输入转换为 `/ts/query` 请求体：

- `start_ms * 1_000_000` -> `start_nanos`
- `end_ms * 1_000_000` -> `end_nanos`
- `sample_ms * 1_000_000` -> `sample_nanos`

枚举映射如下：

| MCP 输入 | `/ts/query` 值 |
|----------|----------------|
| `avg` | `1` |
| `sum` | `2` |
| `max` | `3` |
| `min` | `4` |
| `none` | `0` |
| `rate` / `derivative` | `1` |
| `non_negative_rate` / `non_negative_derivative` | `2` |

---

## 6. 输出归一化

tool 返回统一的 `metrics_timeseries` 结构，核心字段如下：

```json
{
  "status": "success",
  "type": "metrics_timeseries",
  "data": {
    "admin_base_url": "http://inspur-momo:8080",
    "start_ms": 1775035140000,
    "end_ms": 1775035740000,
    "sample_ms": 60000,
    "results": [
      {
        "name": "cr.node.sql.query.count",
        "downsampler": "avg",
        "source_aggregator": "sum",
        "derivative": "rate",
        "sources": ["1"],
        "datapoints": [
          {
            "timestamp_ms": 1775035680000,
            "timestamp_nanos": "1775035680000000000",
            "value": 0.1
          }
        ],
        "empty_series": false
      }
    ]
  }
}
```

输出归一化规则：

- 响应中 `timestampNanos` 与 `timestamp_nanos` 都能解析
- `timestamp_ms` 总是由纳秒字段换算得出
- `downsampler`、`source_aggregator`、`derivative` 在输出中统一为字符串
- 若某个结果没有任何 datapoint，则返回空数组并设置 `empty_series = true`

---

## 7. 参数校验与限制

当前实现有以下校验：

- `start_ms < end_ms`
- `sample_ms > 0`
- `queries` 不能为空
- 单次最多 `10` 个指标
- 最大时间窗为 `24h`
- 每个 query 的 `name` 不能为空
- `downsampler`、`source_aggregator`、`derivative` 必须在受支持枚举内

当前实现**没有**按“时间窗 / 采样间隔”进一步限制点数规模。

---

## 8. 错误处理

当前实现的主要错误行为如下：

- 缺少 admin 地址：返回工具级错误 `missing X-Admin-Base-URL header`
- 参数绑定失败：返回 `Invalid metrics history arguments`
- 参数校验失败：返回对应校验错误
- 上游 `/ts/query` HTTP 非 2xx：返回 `Metrics query failed`，内部会附带状态码与响应摘要
- 上游 JSON 解析失败：返回 `Metrics query failed`

空序列不视为错误，只要上游调用成功，就返回 `status = success`。

---

## 9. 代码位置

- `cmd/kwdb-mcp-server/main.go`
  - 注册 `--admin-base-url`
- `pkg/server/server.go`
  - 新增 `ServerConfig`
  - `CreateServerWithConfig(...)` 将默认 admin 地址传给工具层
- `pkg/tools/tools.go`
  - 新增 `RegisterToolsWithConfig(...)`
  - 注册 `query-metrics-history`
- `pkg/tools/metrics_history.go`
  - 实现输入校验、参数映射、HTTP 调用、响应归一化

---

## 10. 测试覆盖

当前测试覆盖如下：

- `pkg/server/server_test.go`
  - 覆盖 `CreateServerWithConfig(...)`
- `pkg/tools/tools_test.go`
  - 覆盖 `resolveDBTarget(...)`
- `pkg/tools/metrics_history_test.go`
  - 覆盖 `resolveAdminBaseURL(...)`
  - 覆盖参数校验
  - 覆盖 `/ts/query` 请求体构造
  - 覆盖 camelCase / snake_case 响应解析
  - 覆盖空序列处理
  - 覆盖 tool 注册与 `Tool.MarshalJSON()` 行为
  - 覆盖默认 admin 地址和缺失 admin 地址时的工具级结果

---

## 11. 与其它设计文档的关系

- [`docs/design-dual-mode-and-stateless.md`](./design-dual-mode-and-stateless.md)
  - 描述数据库 URI 的双模式行为，以及 admin 地址默认值如何接入整体架构
- 本文档
  - 只描述 `query-metrics-history` 的具体实现与边界
