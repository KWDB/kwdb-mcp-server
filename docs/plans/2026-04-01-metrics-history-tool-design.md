# Metrics History Tool And Admin Base URL Design

## Context

`kwdb-mcp-server` 当前只封装了 SQL 读写能力，数据库选择模型也只覆盖 `X-Database-URI` / 默认连接串。  
2026-04-01 的补充验证表明，数据库侧 `POST /ts/query` 已能返回运行时指标历史序列，并能对累计计数指标直接做导数计算，因此可以补齐巡检中的 QPS 突增突降等时序场景。

问题在于：`/ts/query` 依赖的是数据库 admin HTTP 地址，而不是 SQL 连接串。若要把这类能力纳入 MCP Server，需要先把“目标数据库的 admin_base_url”变成一等输入。

## Goals

- 在不破坏现有 SQL tool 语义的前提下，为历史指标查询提供稳定的 MCP 能力。
- 让 `admin_base_url` 的输入模型与 `X-Database-URI` 尽量对称。
- 对 LLM 屏蔽后端 `/ts/query` 的原始细节，避免直接暴露枚举数字、纳秒时间戳和后端字段差异。

## Non-Goals

- 本轮不实现完整巡检规则引擎。
- 本轮不新增“任意 HTTP 请求代理”类通用工具。
- 本轮不改动现有 `read-query` / `write-query` 的行为。

## Recommendation

新增一个面向场景的 MCP tool：`query-metrics-history`。  
不建议直接暴露一个低层 `ts-query` 透传工具。原因是：

- 当前项目定位是“KWDB 能力封装”，不是通用 HTTP proxy。
- `/ts/query` 的原始请求体含枚举数字和纳秒字段，不适合直接暴露给 agent。
- 实测响应字段与文档存在差异，封装层需要统一字段与错误语义。

## Admin Base URL Resolution

新增一个与数据库目标选择对称的解析规则：

1. 若本次 tool 调用带 `X-Admin-Base-URL`，优先使用该值。
2. 否则，若服务启动时配置了 `--admin-base-url`，使用该默认值。
3. 否则，返回工具级错误：`missing X-Admin-Base-URL header`。

说明：

- 现有测试已证明 `CallToolRequest.Header` 在 `stdio` / `SSE` / `HTTP` 三种传输下都可用，因此 `X-Admin-Base-URL` 可以直接复用 `X-Database-URI` 的模型。
- v1 不建议从数据库 URI 自动推导 admin 地址。SQL 地址与 admin 地址的端口、协议、反向代理路径都可能不同，静默推导容易制造误路由。

## Proposed Tool Interface

建议输入 schema 使用毫秒而不是纳秒，并把枚举改为字符串：

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

服务端负责转换为 `/ts/query` 所需的纳秒与数字枚举：

- `avg|sum|max|min` -> `1|2|3|4`
- `none|rate|non_negative_rate` -> `0|1|2`

建议输出结构：

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
        "datapoints": [
          {
            "timestamp_ms": 1775035680000,
            "timestamp_nanos": "1775035680000000000",
            "value": 0.1
          }
        ]
      }
    ]
  },
  "error": null
}
```

## Guardrails

建议在 MCP 层增加保护：

- 单次请求最多 10 个指标。
- 单次请求最大时间窗默认 24 小时。
- 若 `sample_ms <= 0`、`start_ms >= end_ms` 或推导出的点数过大，直接返回工具级错误。
- 后端返回空 `datapoints` 时，不视为 transport error；应作为合法结果返回，并在 metadata 中标记 `empty_series=true`。

## Error Handling

- admin 地址缺失：返回 `missing X-Admin-Base-URL header`。
- 后端非 2xx：返回 `Metrics query failed`，附带状态码和响应摘要。
- 后端字段兼容：统一读取 camelCase / snake_case，内部标准化为 `timestamp_ms` 和 `timestamp_nanos`。
- 指标不存在：返回空序列，不抛错。

## Fit With Current Architecture

这个设计与当前项目边界一致：

- `read-query` / `write-query` 继续聚焦 SQL。
- 新 tool 单独承载“历史指标查询”这个动作型能力。
- `X-Admin-Base-URL` 与 `X-Database-URI` 并行存在，互不替代。

如果未来需要进一步面向巡检场景收敛，还可以在 `query-metrics-history` 之上新增更高层的 `get-inspection-metrics`，把常用指标名和口径封装成固定模板。

## Testing Plan

- 单元测试：
  - 新增 `resolveAdminBaseURL(headerValue, defaultValue)` 的三种分支覆盖。
  - 覆盖字符串枚举到数字枚举的映射。
  - 覆盖 camelCase / snake_case 响应字段解析。
- 集成测试：
  - HTTP / SSE / stdio 三种传输都验证默认 admin 地址与 header 覆盖。
  - 验证 `cr.node.sql.query.count` 的导数结果返回非空。
  - 验证缺少 admin 地址时返回工具级错误，而不是 transport error。

## Rollout Suggestion

建议按两步落地：

1. 先实现 `query-metrics-history` 和 `X-Admin-Base-URL` / `--admin-base-url`。
2. 再把巡检里常用的 QPS、CPU、RSS、延迟指标做成高层封装或指标别名。
