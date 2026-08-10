package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// metricSpec describes the KaiwuDB admin /ts/query parameters for a single
// fixed inspection metric. The three fields mirror the query string keys the
// admin endpoint expects (`downsampler`, `source_aggregator`, `derivative`).
type metricSpec struct {
	Downsampler      string
	SourceAggregator string
	Derivative       string
}

// inspectionMetrics is the read-only catalog of metrics the MCP
// `query-metrics` tool exposes to the LLM. It is the Go equivalent of the
// Python skill's `METRICS_MAP` and is locked at the values enumerated in
// Design Doc Decision 7 (`docs/superpowers/specs/2026-08-04-tls-inspection-tools-design.md`).
//
// Notes for future maintainers:
//   - The map MUST stay a fixed closed set. Callers are not allowed to pass
//     arbitrary metric names; the tool rejects unknown names before reaching
//     the admin endpoint.
//   - Decision 7 enumerates 32 metrics (commit `3ce3b6e` reconciled the
//     doc text from "30" to "32" to match the table).
//   - All `Derivative` values are "none" today. The field exists so future
//     rate-style metrics (e.g. counters converted to per-second) can be
//     added without changing the call site signature.
var inspectionMetrics = map[string]metricSpec{
	// --- 基础 ---
	"cr.node.liveness.livenodes": {"avg", "avg", "none"},
	"cr.node.sys.uptime":         {"avg", "avg", "none"},

	// --- 系统 ---
	"cr.node.sys.cpu.user.percent":                {"avg", "sum", "none"},
	"cr.node.sys.cpu.sys.percent":                 {"avg", "sum", "none"},
	"cr.node.sys.cpu.combined.percent-normalized": {"avg", "sum", "none"},
	"cr.store.capacity":                           {"avg", "sum", "none"},
	"cr.store.capacity.available":                 {"avg", "sum", "none"},
	"cr.store.capacity.used":                      {"avg", "sum", "none"},
	"cr.node.sys.rss":                             {"avg", "sum", "none"},
	"cr.node.sys.go.allocbytes":                   {"avg", "sum", "none"},
	"cr.node.sys.go.totalbytes":                   {"avg", "sum", "none"},

	// --- 数据库 ---
	"cr.node.sql.insert.count":              {"avg", "sum", "none"},
	"cr.node.sql.update.count":              {"avg", "sum", "none"},
	"cr.node.sql.delete.count":              {"avg", "sum", "none"},
	"cr.node.sql.select.count":              {"avg", "sum", "none"},
	"cr.node.sql.query.count":               {"avg", "sum", "none"},
	"cr.store.rebalancing.writespersecond":  {"avg", "sum", "none"},
	"cr.store.rebalancing.queriespersecond": {"avg", "sum", "none"},
	"cr.node.exec.latency-p99":              {"avg", "avg", "none"},
	"cr.node.sql.service.latency-p99":       {"avg", "avg", "none"},
	"cr.node.sql.distsql.exec.latency-p99":  {"avg", "avg", "none"},

	// --- 存储 ---
	"cr.store.totalbytes": {"avg", "sum", "none"},
	"cr.store.livebytes":  {"avg", "sum", "none"},

	// --- 集群 ---
	"cr.store.replicas":                            {"avg", "sum", "none"},
	"cr.store.replicas.leaders":                    {"avg", "sum", "none"},
	"cr.store.replicas.leaseholders":               {"avg", "sum", "none"},
	"cr.store.ranges.unavailable":                  {"avg", "max", "none"},
	"cr.store.ranges.underreplicated":              {"avg", "max", "none"},
	"cr.store.ranges.overreplicated":               {"avg", "max", "none"},
	"cr.store.raftlog.behind":                      {"avg", "max", "none"},
	"cr.store.raft.replica.consistent.latency-p99": {"avg", "avg", "none"},

	// --- 网络 ---
	"cr.node.clock-offset.meannanos": {"avg", "max", "none"},
}

// metricsInput is the bound argument payload for the MCP `query-metrics`
// tool. MetricNames is the required closed-set list of inspection metrics
// the LLM wants to read; Start/End/Sample carry the query window and the
// sampling interval in milliseconds.
//
// The JSON tags MUST stay in sync with queryMetricsInputSchema below: the
// schema advertises the `_ms` suffix to make the unit explicit to the LLM,
// and request.BindArguments matches on those exact names. A mismatch binds
// nothing and surfaces later as a confusing "sample_ms must be greater than
// 0" validation error rather than as a binding failure.
type metricsInput struct {
	MetricNames []string `json:"metric_names"`
	Start       int64    `json:"start_ms"`
	End         int64    `json:"end_ms"`
	Sample      int64    `json:"sample_ms"`
}

// validateMetricsInput enforces the spec contract for the `query-metrics`
// tool. It must reject bad inputs before any HTTP call so callers never
// reach the admin endpoint with an invalid metric name.
//
//   - MetricNames must be non-empty; each entry is trimmed of whitespace
//     and looked up in inspectionMetrics. Unknown names fail with the
//     exact error text `unknown metric: <name>` (the spec requires this
//     verbatim so the LLM can match the error).
//   - Start, End, and Sample must all be strictly positive. The schema
//     marks them required but only enforces presence and type, so an LLM
//     that sends `end_ms: 0` (or a negative value) would otherwise produce
//     an inverted time window like `end_nanos=0` at the admin endpoint.
//     The error mirrors the `query-metrics-history` conventions
//     `start_ms must be less than end_ms` and `sample_ms must be greater
//     than 0`.
//
// The function is total and side-effect free: any caller can safely run
// it without standing up an admin client.
func validateMetricsInput(input metricsInput) error {
	if len(input.MetricNames) == 0 {
		return fmt.Errorf("metric_names must not be empty")
	}
	for _, raw := range input.MetricNames {
		name := strings.TrimSpace(raw)
		if _, ok := inspectionMetrics[name]; !ok {
			return fmt.Errorf("unknown metric: %s", name)
		}
	}
	if input.Start <= 0 {
		return fmt.Errorf("start_ms must be greater than 0")
	}
	if input.End <= 0 {
		return fmt.Errorf("end_ms must be greater than 0")
	}
	if input.Start >= input.End {
		return fmt.Errorf("start_ms must be less than end_ms")
	}
	if input.Sample <= 0 {
		return fmt.Errorf("sample_ms must be greater than 0")
	}
	return nil
}

// queryMetricsInputSchema is the JSON Schema the LLM tool descriptor advertises
// for `query-metrics`. metric_names is a required, closed-set list of names
// from inspectionMetrics (validation rejects unknown names); the time fields
// are required because the admin /ts/query endpoint never accepts a missing
// window or sampling interval.
var queryMetricsInputSchema = []byte(`{
  "type": "object",
  "properties": {
    "metric_names": {
      "type": "array",
      "minItems": 1,
      "items": {"type": "string"},
      "description": "List of inspection metric names from the fixed catalog"
    },
    "start_ms": {"type": "integer", "description": "Start time in Unix milliseconds"},
    "end_ms":   {"type": "integer", "description": "End time in Unix milliseconds"},
    "sample_ms":{"type": "integer", "description": "Sampling interval in milliseconds"}
  },
  "required": ["metric_names", "start_ms", "end_ms", "sample_ms"]
}`)

// executeMetricsQuery is the admin-side executor for the `query-metrics`
// tool. It validates the input, builds a ts/query request body using the
// same integer encodings as `query-metrics-history` (avg=1, sum=2, max=3,
// min=4 for downsampler/source_aggregator; none=0, rate=1,
// non_negative_rate=2 for derivative), POSTs it through the shared admin
// client, and returns the parsed AdminResponse on success. The return
// signature is `any` so the caller (the tool handler) can wrap the
// response into the MCP envelope without exposing AdminResponse to the
// LLM-visible contract.
//
// The three-level fallback for the admin URL (headerAdmin > flagAdmin >
// DB URL) is delegated to resolveAdminBaseURL. Auth classification is the
// OR of the resolved admin URL's scheme (https → require credentials) and
// the DB URL's sslmode, so an https admin URL is always treated as TLS
// even when the DB URL is insecure — the secure-cluster-with-insecure-DB-URL
// path that would otherwise be silently dropped (KaiwuDB secure admin
// redirects HTTP→HTTPS and rejects unauthenticated HTTPS with 401).
// Basic-Auth credentials still come from dbURL alone (a blank dbURL yields
// no credentials, which triggers the fail-fast error when the combined
// isTLS is true).
func executeMetricsQuery(
	ctx context.Context,
	input metricsInput,
	headerAdmin string,
	flagAdmin string,
	dbURL string,
) (any, error) {
	if err := validateMetricsInput(input); err != nil {
		return nil, err
	}

	adminBaseURL, adminURLIsTLS, err := resolveAdminBaseURL(headerAdmin, flagAdmin, dbURL)
	if err != nil {
		return nil, err
	}

	// dbURL may be empty (the caller passed an explicit header/flag admin
	// URL and no DB URI); in that case isTLSForAdmin returns false and
	// credentials are empty. The OR with adminURLIsTLS keeps isTLS true
	// for an https admin URL, which then triggers the fail-fast error
	// below for missing credentials.
	var info db.DBURLInfo
	if dbURL != "" {
		info, err = db.ParseDBURL(dbURL)
		if err != nil {
			return nil, fmt.Errorf("invalid X-Database-URI: %w", err)
		}
	}
	isTLS := adminURLIsTLS || isTLSForAdmin(info.SSLMode, info.SSLCert, info.SSLKey, info.SSLRootCert)

	reqBody, err := buildQueryMetricsRequest(input)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal metrics request: %w", err)
	}

	_, rawBody, err := doAdminRequest(ctx, "POST", buildTSQueryURL(adminBaseURL), payload, info.User, info.Password, isTLS)
	if err != nil {
		return nil, err
	}
	// parseAdminResponse returns (*AdminResponse)(nil) on error; converting
	// a typed nil to any would leak a non-nil interface value, so unwrap
	// the error path explicitly to keep the success/error contract clean.
	resp, err := parseAdminResponse(rawBody)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// buildQueryMetricsRequest turns a metricsInput into a tsQueryRequest ready
// for POST /ts/query. Each metric maps onto a tsQueryRequestQuery whose
// downsampler/source_aggregator/derivative are the integers defined in
// mapDownsampler/mapAggregator/mapDerivative. Sample sizes and time
// windows are converted to nanoseconds (ms * 1e6), matching the time
// encoding used by `query-metrics-history`.
func buildQueryMetricsRequest(input metricsInput) (tsQueryRequest, error) {
	queries := make([]tsQueryRequestQuery, 0, len(input.MetricNames))
	for _, raw := range input.MetricNames {
		name := strings.TrimSpace(raw)
		spec, ok := inspectionMetrics[name]
		if !ok {
			// validateMetricsInput has already run, so reaching here means
			// a caller bypassed validation; surface the same spec text.
			return tsQueryRequest{}, fmt.Errorf("unknown metric: %s", name)
		}
		downsampler, err := mapDownsampler(spec.Downsampler)
		if err != nil {
			return tsQueryRequest{}, fmt.Errorf("unsupported downsampler for %s: %w", name, err)
		}
		sourceAggregator, err := mapAggregator(spec.SourceAggregator)
		if err != nil {
			return tsQueryRequest{}, fmt.Errorf("unsupported source aggregator for %s: %w", name, err)
		}
		derivative, err := mapDerivative(spec.Derivative)
		if err != nil {
			return tsQueryRequest{}, fmt.Errorf("unsupported derivative for %s: %w", name, err)
		}
		queries = append(queries, tsQueryRequestQuery{
			Name:             name,
			Downsampler:      downsampler,
			SourceAggregator: sourceAggregator,
			Derivative:       derivative,
		})
	}

	return tsQueryRequest{
		StartNanos:  input.Start * 1_000_000,
		EndNanos:    input.End * 1_000_000,
		SampleNanos: input.Sample * 1_000_000,
		Queries:     queries,
	}, nil
}

// registerQueryMetricsTool wires the MCP `query-metrics` tool descriptor and
// its handler into the provided server. The handler pulls the admin URL
// from the X-Admin-Base-URL request header (falling back to defaultAdminBaseURL
// and finally to the DB URL via the standard three-level chain), runs
// `executeMetricsQuery`, and wraps the AdminResponse in the structured MCP
// envelope that downstream tools use (status/type/data/error shape,
// mirroring `read-query`).
//
// defaultAdminBaseURL seeds the `--admin-base-url` flag tier of the
// fallback. Per-request X-Admin-Base-URL headers still take precedence —
// this lets multi-tenant callers override the single-DB default at request
// time without restarting the server.
//
// defaultDatabaseURI seeds the same single-DB fallback for the DSN. It is
// used to extract credentials when the per-request X-Database-URI header is
// omitted (e.g. Claude Code MCP clients that do not pass custom headers).
// Admin endpoints that redirect HTTP→HTTPS require Basic Auth, and the
// credentials come from this DSN.
func registerQueryMetricsTool(s *server.MCPServer, defaultAdminBaseURL, defaultDatabaseURI string) {
	tool := mcp.NewToolWithRawSchema(
		"query-metrics",
		"Query KWDB runtime inspection metrics through the admin /ts/query API using a closed set of metric names and millisecond timestamps.",
		json.RawMessage(queryMetricsInputSchema),
	)
	tool.RawOutputSchema = json.RawMessage(validOutputSchema)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input metricsInput
		if err := request.BindArguments(&input); err != nil {
			return mcp.NewToolResultErrorFromErr("Invalid metrics arguments", err), nil
		}

		headerURI := request.Header.Get("X-Database-URI")
		headerAdmin := request.Header.Get("X-Admin-Base-URL")

		// X-Database-URI header wins when it carries usable credentials;
		// otherwise fall back to the registration-time default DSN. Some MCP
		// clients (notably Claude Code's HTTP transport) auto-inject an
		// X-Database-URI header like `postgresql://root:@127.0.0.1:26257/...`
		// — empty user/password and pointing at localhost. That stub header
		// would otherwise poison admin credentials when the admin endpoint
		// redirects HTTP→HTTPS and demands Basic Auth. Fall back whenever
		// the header URI either is missing or parses to empty credentials.
		effectiveDBURI := headerURI
		if effectiveDBURI == "" || !hasUsableCredentials(effectiveDBURI) {
			effectiveDBURI = defaultDatabaseURI
		}

		// resolveAdminBaseURL inside admin.go enforces the priority
		// header → flag → DB URL derivation; pass both tiers through and
		// let the resolver pick. Multi-tenant callers set headerAdmin to
		// override the registration-time default without a restart.
		result, err := executeMetricsQuery(ctx, input, headerAdmin, defaultAdminBaseURL, effectiveDBURI)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("Metrics query failed", err), nil
		}

		// KaiwuDB /restapi/ts/query returns a {"results":[{...}]} envelope
		// rather than the {"code":0,"desc":""} envelope used by /restapi/ddl
		// et al. AdminResponse.Raw holds the verbatim response body so the
		// caller can inspect the actual datapoints; exposing it here via
		// json.RawMessage (instead of the marshaled *AdminResponse) keeps
		// the response payload intact instead of collapsing it to
		// {"code":0,"desc":""}. The `json:"-"` tag on Raw would otherwise
		// drop the envelope-level fields.
		adminResp, _ := result.(*AdminResponse)
		var data any = adminResp // fallback when Raw is empty
		if adminResp != nil && len(adminResp.Raw) > 0 {
			data = json.RawMessage(adminResp.Raw)
		}

		response := map[string]any{
			"status": "success",
			"type":   "metrics_inspection",
			"data":   data,
			"error":  nil,
		}

		jsonResult, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to serialize metrics result: %v", err)
		}

		return mcp.NewToolResultStructured(response, string(jsonResult)), nil
	})
}

// tsQueryRequest is the wire shape for a KaiwuDB admin /ts/query POST body.
// It is the format produced for both the historical timeseries tool and the
// inspection metrics tool, so it lives next to the inspection tool now that
// the historical tool has been removed.
type tsQueryRequest struct {
	StartNanos  int64                 `json:"start_nanos"`
	EndNanos    int64                 `json:"end_nanos"`
	SampleNanos int64                 `json:"sample_nanos"`
	Queries     []tsQueryRequestQuery `json:"queries"`
}

type tsQueryRequestQuery struct {
	Name             string `json:"name"`
	Downsampler      int    `json:"downsampler"`
	SourceAggregator int    `json:"source_aggregator"`
	Derivative       int    `json:"derivative"`
}

// buildTSQueryURL appends the canonical /restapi/ts/query path to a trimmed
// admin base URL. KaiwuDB enterprise routes the time-series inspection API
// under /restapi/* (not the CockroachDB-style /_status/* or /ts/query paths
// used by older builds); POST /restapi/ts/query accepts a JSON body and
// returns the datapoints in the standard `{"results":[{...}]}` envelope.
//
// Historical note: the deleted `query-metrics-history` tool used the
// /ts/query path that CockroachDB exposed before KaiwuDB forked it; that
// path was removed in KaiwuDB enterprise 3.3.0 and replaced by
// /restapi/ts/query. The 3.3.0 dev30 build confirms 200 OK with Basic Auth
// against this path.
func buildTSQueryURL(adminBaseURL string) string {
	return strings.TrimRight(strings.TrimSpace(adminBaseURL), "/") + "/restapi/ts/query"
}

// mapDownsampler maps the human-readable downsampler name to the integer
// encoding the admin /ts/query endpoint expects (avg=1, sum=2, max=3, min=4).
func mapDownsampler(value string) (int, error) {
	switch normalizeEnum(value) {
	case "avg":
		return 1, nil
	case "sum":
		return 2, nil
	case "max":
		return 3, nil
	case "min":
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported downsampler: %s", value)
	}
}

// mapAggregator maps the source-aggregator name using the same integer
// encoding as the downsampler (avg=1, sum=2, max=3, min=4).
func mapAggregator(value string) (int, error) {
	return mapDownsampler(value)
}

// mapDerivative maps the derivative name to the admin endpoint integer
// encoding (none=0, rate/derivative=1, non_negative_rate/non_negative_derivative=2).
func mapDerivative(value string) (int, error) {
	switch normalizeEnum(value) {
	case "none":
		return 0, nil
	case "rate", "derivative":
		return 1, nil
	case "non_negative_rate", "non_negative_derivative":
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported derivative: %s", value)
	}
}

// normalizeEnum lowercases, trims, and converts dashes to underscores so
// downstream switch statements can compare against a single canonical form.
func normalizeEnum(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
}

// hasUsableCredentials reports whether the given DSN carries both a non-empty
// username and a non-empty password. Admin endpoints that redirect HTTP→HTTPS
// demand Basic Auth; a header DSN with empty credentials (e.g. the stub
// `postgresql://root:@127.0.0.1:26257/...` injected by some MCP clients)
// would otherwise be silently picked over the registration-time default
// and break the auth path.
func hasUsableCredentials(dsn string) bool {
	if dsn == "" {
		return false
	}
	info, err := db.ParseDBURL(dsn)
	if err != nil {
		return false
	}
	return info.User != "" && info.Password != ""
}
