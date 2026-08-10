package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// slowSQLSortKeys is the closed set of sort criteria the `query-slow-sql`
// tool accepts. It mirrors the Python skill's
// `get_kwdb_statements.py --sort-by` choices so both entry points rank
// statements identically:
//
//	service_lat -> mean service latency (default)
//	run_lat     -> mean execution latency
//	plan_lat    -> mean planning latency
//	count       -> execution count
//
// Keep this in sync with slowSqlInput's schema description; validation
// rejects anything outside the set before an admin call is made.
var slowSQLSortKeys = map[string]struct{}{
	"service_lat": {},
	"run_lat":     {},
	"plan_lat":    {},
	"count":       {},
}

// Defaults for the `query-slow-sql` tool. The spec's "默认查询" scenario
// requires that calling the tool with no arguments returns the top 10
// statements sorted by service latency with no latency floor, so the
// zero value of slowSqlInput MUST expand to exactly these values.
const (
	defaultSlowSQLLimit  = 10
	defaultSlowSQLSortBy = "service_lat"
)

// slowSqlInput is the bound argument payload for the MCP `query-slow-sql`
// tool. Every field is optional: the JSON tags carry `omitempty` and the
// zero value is a valid request meaning "top 10 by service latency, no
// latency floor" (see applySlowSQLDefaults).
//
//   - Limit caps how many statements are returned after filtering/sorting.
//   - MinLatencyMS drops statements whose mean service latency (in
//     milliseconds) falls below the floor, matching the Python skill's
//     `filter_and_sort` which always filters on service_latency_ms
//     regardless of the sort key.
//   - SortBy selects the ranking criterion from slowSQLSortKeys.
type slowSqlInput struct {
	Limit        int     `json:"limit,omitempty"`
	MinLatencyMS float64 `json:"min_latency_ms,omitempty"`
	SortBy       string  `json:"sort_by,omitempty"`
}

// statement is the normalized view of a single entry returned by the
// KaiwuDB `/_status/statements` admin API. Latency fields are means, in
// the same unit the caller filters on (milliseconds); the raw API reports
// seconds, so the conversion belongs to the response mapper, not here.
type statement struct {
	ID             string  `json:"id"`
	Fingerprint    string  `json:"fingerprint"`
	Query          string  `json:"query"`
	ServiceLatency float64 `json:"service_latency_ms"`
	RunLatency     float64 `json:"run_latency_ms"`
	PlanLatency    float64 `json:"plan_latency_ms"`
	Count          int64   `json:"count"`
}

// applySlowSQLDefaults returns a copy of input with unset fields replaced
// by the documented defaults. It is separate from validateSlowSQLInput
// because validation takes its argument by value and therefore cannot
// hand normalized values back to the caller; executors should call this
// once and pass the result downstream.
//
// MinLatencyMS deliberately has no default branch: its zero value (no
// filtering) is already the intended default, so an explicit 0 and an
// omitted field are indistinguishable and both mean "no floor".
func applySlowSQLDefaults(input slowSqlInput) slowSqlInput {
	if input.Limit == 0 {
		input.Limit = defaultSlowSQLLimit
	}
	if input.SortBy == "" {
		input.SortBy = defaultSlowSQLSortBy
	}
	return input
}

// validateSlowSQLInput enforces the spec contract for `query-slow-sql`.
// It normalizes the input through applySlowSQLDefaults first so that the
// zero value — the "call the tool with no arguments" case — always passes,
// then rejects out-of-range values with the exact error texts the spec
// pins:
//
//   - `limit must be positive` for a non-positive limit (0 never reaches
//     this check because defaults expand it to 10)
//   - `min_latency_ms must be non-negative` for a negative latency floor
//   - `unknown sort_by: <value>` for a sort key outside slowSQLSortKeys
//
// The function is total and side-effect free: it makes no network calls
// and does not mutate its argument.
func validateSlowSQLInput(input slowSqlInput) error {
	input = applySlowSQLDefaults(input)

	if input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if input.MinLatencyMS < 0 {
		return fmt.Errorf("min_latency_ms must be non-negative")
	}
	if _, ok := slowSQLSortKeys[input.SortBy]; !ok {
		return fmt.Errorf("unknown sort_by: %s", input.SortBy)
	}
	return nil
}

// statementsEnvelope mirrors the KaiwuDB `/_status/statements` admin API
// response shape. Only the fields parseStatementsResponse consumes are
// typed here; everything else lives in a raw map so an evolving upstream
// payload (extra metadata at the top level, new stats sub-objects) does
// not break the mapper. The nested structures follow the Python skill's
// `get_kwdb_statements.py` keys so the contract is identical across
// entry points.
type statementsEnvelope struct {
	Statements []rawStatement `json:"statements"`
}

// rawStatement is the wire shape of a single entry in `statements`. The
// `key` and `stats` sub-objects are kept as raw JSON so we can report
// a missing-field error with the offending key name rather than silently
// producing an empty statement.
type rawStatement struct {
	Key   json.RawMessage `json:"key"`
	Stats json.RawMessage `json:"stats"`
}

// rawKeyData mirrors the `key.keyData` block — the per-statement metadata
// the API returns (query text, app, user, database, flags).
type rawKeyData struct {
	Query   string `json:"query"`
	App     string `json:"app"`
	User    string `json:"user"`
	DB      string `json:"database"`
	DistSQL bool   `json:"distSQL"`
	Failed  bool   `json:"failed"`
	ImpTxn  bool   `json:"implicitTxn"`
}

// rawKey wraps keyData and the node identifier. `NodeID` is left as
// `json.RawMessage` because the upstream field surfaces as either an
// integer or a string depending on cluster version; we stringify it
// rather than guess.
type rawKey struct {
	KeyData rawKeyData      `json:"keyData"`
	NodeID  json.RawMessage `json:"nodeId"`
}

// meanFloat is the common shape `stats.*Lat.mean` and
// `stats.numRows.mean` take: a numeric mean under the `mean` key.
type meanFloat struct {
	Mean float64 `json:"mean"`
}

// rawStats mirrors the `stats` block. Fields we don't surface (rows,
// bytes, retries, sensitive info) are deliberately ignored — the
// envelope contract only requires count + the three mean latencies
// jsonStringInt accepts both a JSON number and a quoted JSON string.
// The KaiwuDB `/_status/statements` API historically serialises integer
// counters (count, firstAttemptCount, maxRetries, bytesRead, rowsRead,
// failedCount) as quoted strings in some clusters and as plain numbers
// in others, so a typed wrapper keeps the rest of rawStats readable while
// decoding succeeds either way.
type jsonStringInt int64

func (j *jsonStringInt) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %q as int64: %w", s, err)
		}
		*j = jsonStringInt(n)
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*j = jsonStringInt(n)
	return nil
}

// the spec sorts on.
type rawStats struct {
	Count             jsonStringInt `json:"count"`
	FirstAttemptCount jsonStringInt `json:"firstAttemptCount"`
	MaxRetries        jsonStringInt `json:"maxRetries"`
	BytesRead         jsonStringInt `json:"bytesRead"`
	RowsRead          jsonStringInt `json:"rowsRead"`
	FailedCount       jsonStringInt `json:"failedCount"`
	ServiceLat        meanFloat     `json:"serviceLat"`
	RunLat            meanFloat     `json:"runLat"`
	PlanLat           meanFloat     `json:"planLat"`
	ParseLat          meanFloat     `json:"parseLat"`
	OverheadLat       meanFloat     `json:"overheadLat"`
	NumRows           meanFloat     `json:"numRows"`
}

// parseStatementsResponse decodes the raw body returned by the KaiwuDB
// `/_status/statements` admin API into the normalized []statement used by
// the `query-slow-sql` tool. Latency fields are converted from seconds
// (the API's unit) to milliseconds (the unit callers filter/sort on).
//
// Errors are designed to be locatable:
//
//   - Malformed JSON → "decode statements response: <enc/json error>"
//   - `statements` present but not an array → "decode statements response: ..."
//   - Per-entry missing `key` or `stats` → "missing <field> in statement entry"
//   - Per-entry key/stats not a JSON object → "decode statement <field>: ..."
//
// The function never returns a partially populated statement: a bad
// entry aborts the whole decode with an error so callers don't have to
// inspect individual rows for validity.
func parseStatementsResponse(body []byte) ([]statement, error) {
	var envelope statementsEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode statements response: %w", err)
	}

	statements := make([]statement, 0, len(envelope.Statements))
	for index, raw := range envelope.Statements {
		normalized, err := normalizeStatement(raw, index)
		if err != nil {
			return nil, err
		}
		statements = append(statements, normalized)
	}
	return statements, nil
}

// normalizeStatement converts a single raw envelope entry into the
// internal `statement` view. The two-step decode (raw envelope then
// typed re-decode of `key` and `stats`) lets us distinguish "field
// absent" (decode succeeds with zero values) from "field missing
// entirely" (RawMessage is nil) and report the latter as a real error.
func normalizeStatement(raw rawStatement, index int) (statement, error) {
	if len(raw.Key) == 0 {
		return statement{}, fmt.Errorf("missing key in statement entry %d", index)
	}
	if len(raw.Stats) == 0 {
		return statement{}, fmt.Errorf("missing stats in statement entry %d", index)
	}

	var key rawKey
	if err := json.Unmarshal(raw.Key, &key); err != nil {
		return statement{}, fmt.Errorf("decode statement key in entry %d: %w", index, err)
	}
	var stats rawStats
	if err := json.Unmarshal(raw.Stats, &stats); err != nil {
		return statement{}, fmt.Errorf("decode statement stats in entry %d: %w", index, err)
	}

	// The API reports latencies in seconds; the tool contract uses
	// milliseconds (matches `MinLatencyMS` and the four sort keys),
	// so the conversion happens here and never leaks outward.
	const secondsToMillis = 1000.0

	return statement{
		ID: stringifyNodeID(key.NodeID),
		// TODO: derive Fingerprint from a SQL fingerprint hash once upstream
		// `/_status/statements` surfaces one. Today the API only exposes
		// `key.keyData.query/app/user/database` + `key.nodeId`; the Python
		// skill at KGA/.../get_kwdb_statements.py uses the same fallback
		// (no independent fingerprint field). Until then we surface
		// nodeId here so callers have a stable per-statement identifier.
		Fingerprint:    stringifyNodeID(key.NodeID),
		Query:          key.KeyData.Query,
		ServiceLatency: stats.ServiceLat.Mean * secondsToMillis,
		RunLatency:     stats.RunLat.Mean * secondsToMillis,
		PlanLatency:    stats.PlanLat.Mean * secondsToMillis,
		Count:          int64(stats.Count),
	}, nil
}

// stringifyNodeID renders the `nodeId` field as a stable string
// regardless of whether the upstream encodes it as a number or a
// string. An empty/missing nodeId (nil raw) falls back to ""; the
// resulting statement still parses because ID/Fingerprint are
// metadata-only and are not validated downstream.
func stringifyNodeID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Cheap fast path: numeric nodeId — strip JSON quotes/whitespace.
	trimmed := trimJSONWhitespace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	return string(trimmed)
}

// trimJSONWhitespace strips leading and trailing ASCII whitespace and
// newlines from a JSON token so we can inspect its first byte without
// allocating a copy via json.Decoder.
func trimJSONWhitespace(raw []byte) []byte {
	start, end := 0, len(raw)
	for start < end {
		switch raw[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto done
		}
	}
done:
	for end > start {
		switch raw[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return raw[start:end]
		}
	}
	return raw[start:end]
}

// filterAndSortStatements applies the spec's "默认查询" pipeline to a
// decoded statement list: filter by service latency floor, sort
// descending by the caller-selected sort key, then truncate to the
// requested limit. The sort key mapping matches slowSQLSortKeys and
// defaults via applySlowSQLDefaults so the zero-value input produces
// the documented "top 10 by service latency" behavior.
//
// Behavior contract:
//   - Filter always runs on ServiceLatency (matches the Python skill's
//     `filter_and_sort`, which floors on service_latency_ms
//     regardless of the sort key).
//   - Sort is descending and stable, so statements that tie on the
//     chosen sort key retain their upstream order (slice.SortStable).
//   - The input slice is never mutated: the function copies before
//     sorting and slicing. A nil or empty input yields an empty slice,
//     never nil-deref panics.
//   - A limit larger than the filtered result returns every survivor;
//     a zero limit falls through applySlowSQLDefaults to defaultSlowSQLLimit.
func filterAndSortStatements(items []statement, input slowSqlInput) []statement {
	normalized := applySlowSQLDefaults(input)

	// First pass: filter on service latency. Filter into a new slice so
	// we never touch the caller's backing array — sorting would otherwise
	// rewrite elements the caller can still observe.
	filtered := make([]statement, 0, len(items))
	for _, item := range items {
		if item.ServiceLatency >= normalized.MinLatencyMS {
			filtered = append(filtered, item)
		}
	}

	// Stable descending sort. slice.SortStable preserves the order of
	// equal-keyed elements, which matters when multiple statements share
	// the same service latency and we need upstream ordering to survive.
	sort.SliceStable(filtered, func(i, j int) bool {
		return statementSortKey(filtered[i], normalized.SortBy) > statementSortKey(filtered[j], normalized.SortBy)
	})

	// Truncate to the request limit. A limit larger than the filtered
	// count naturally returns every survivor because min(limit, len)
	// bounds the slice.
	if normalized.Limit > 0 && normalized.Limit < len(filtered) {
		filtered = filtered[:normalized.Limit]
	}
	return filtered
}

// statementSortKey projects a statement onto the numeric value used by
// the four documented sort keys. Any sort key reaching this function
// has already been validated against slowSQLSortKeys (by callers such
// as validateSlowSQLInput), so unrecognized keys never reach the
// sorter; the default branch is defensive only.
func statementSortKey(s statement, sortBy string) float64 {
	switch sortBy {
	case "run_lat":
		return s.RunLatency
	case "plan_lat":
		return s.PlanLatency
	case "count":
		return float64(s.Count)
	case "service_lat":
		return s.ServiceLatency
	default:
		return s.ServiceLatency
	}
}

// slowSqlInputSchema is the JSON Schema the LLM tool descriptor advertises
// for `query-slow-sql`. Every property is optional: the zero value of
// slowSqlInput is the documented default request (top 10 by service
// latency, no latency floor). `required` is deliberately omitted so an LLM
// that passes an empty argument object succeeds with the defaults.
var slowSqlInputSchema = []byte(`{
  "type": "object",
  "properties": {
    "limit":         {"type": "integer", "description": "Max statements to return (default 10)"},
    "min_latency_ms":{"type": "number",  "description": "Filter statements with service latency below this threshold"},
    "sort_by":       {"type": "string",  "enum": ["service_lat", "run_lat", "plan_lat", "count"], "description": "Sort key"}
  }
}`)

// slowSQLStatementsPath is the canonical KaiwuDB admin endpoint backing
// the `query-slow-sql` tool. It is a package-level constant so test code
// and the handler agree on the exact string; a typo here would silently
// cause 404s against a real cluster.
const slowSQLStatementsPath = "/_status/statements"

// executeSlowSQLQuery is the admin-side executor for the `query-slow-sql`
// tool. It validates and normalizes the input, runs a SELECT against
// the cluster-side system table `kwdb_internal.node_statement_statistics`,
// decodes the rows into a normalized `[]statement`, then runs the
// documented filter/sort/limit pipeline. The return is `[]statement`
// because the caller (the tool handler) wraps it directly into the MCP
// envelope.
//
// bugfix-2026-08-07: KaiwuDB enterprise 3.3.0 dev30 drops the
// `/_status/statements` admin HTTP endpoint; the only supported way to
// read per-statement latency aggregates is the cluster-side system table.
// `kwdb_internal.node_statement_statistics` is the system catalog table
// populated by every node's statement statistics collector; querying it
// directly avoids the HTTP admin port's session-cookie / Basic Auth
// friction. The DB URL alone (no separate admin URL) is sufficient — the
// SQL connection already authenticates with the user/password embedded
// in the DSN.
//
// latency unit: the system table stores latencies in seconds (FLOAT8).
// `slowSqlInput.MinLatencyMS` is in milliseconds, so the WHERE clause
// uses `min_latency_ms / 1000.0`. The `statement` view normalises back
// to milliseconds for the filter/sort/limit pipeline (see
// `normalizeStatement`).
func executeSlowSQLQuery(
	ctx context.Context,
	input slowSqlInput,
	dbURL string,
) ([]statement, error) {
	if err := validateSlowSQLInput(input); err != nil {
		return nil, err
	}
	normalized := applySlowSQLDefaults(input)
	if dbURL == "" {
		return nil, fmt.Errorf("missing X-Database-URI: required to query kwdb_internal.node_statement_statistics")
	}

	// sortBy is validated up front (slowSQLSortKeys); safe to interpolate.
	sortCol, ok := slowSQLStatementsSortColumn[normalized.SortBy]
	if !ok {
		return nil, fmt.Errorf("unknown sort_by: %s", normalized.SortBy)
	}
	minLatencySeconds := normalized.MinLatencyMS / 1000.0

	// Use parameterized query for the float+int values; the sort column
	// goes through the validated whitelist above. SELECT is permitted
	// by db.ClassifyQuery so this can flow through slowSQLExecuteQueryFn.
	//
	// Placeholder style: KaiwuDB (postgresql protocol) accepts `$1`/`$2`
	// rather than the `?` form. Sending `?` surfaces as
	// `pq: at or near "?": syntax error` at prepare time; switching to
	// `$N` runs cleanly against both the cluster-side SQL shell and the
	// pgx driver that production routes through.
	query := fmt.Sprintf(`
SELECT key, count, service_lat_avg, run_lat_avg, plan_lat_avg, database, user_name
FROM kwdb_internal.node_statement_statistics
WHERE service_lat_avg >= $1
ORDER BY %s DESC
LIMIT $2
`, sortCol)

	rows, err := slowSQLExecuteQueryFn(ctx, dbURL, query, minLatencySeconds, normalized.Limit)
	if err != nil {
		return nil, fmt.Errorf("query kwdb_internal.node_statement_statistics: %w", err)
	}

	items := make([]statement, 0, len(rows))
	for _, row := range rows {
		items = append(items, normalizeSQLStatementRow(row))
	}
	return filterAndSortStatements(items, normalized), nil
}

// slowSQLStatementsSortColumn maps the documented sort_by key to the
// corresponding column in kwdb_internal.node_statement_statistics. The
// keys are validated against this map before being interpolated into
// SQL, so SQL injection is impossible from this surface.
var slowSQLStatementsSortColumn = map[string]string{
	"service_lat": "service_lat_avg",
	"run_lat":     "run_lat_avg",
	"plan_lat":    "plan_lat_avg",
	"count":       "count",
}

// slowSQLExecuteQueryFn is the seam used by executeSlowSQLQuery to issue
// the underlying SELECT against kwdb_internal.node_statement_statistics.
// The default implementation routes through db.GetMultiPoolManager; tests
// override this with a canned-row stub.
var slowSQLExecuteQueryFn = func(ctx context.Context, dbURL, query string, args ...interface{}) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}
	err := db.GetMultiPoolManager().ExecuteWithURI(ctx, dbURL, func(sqlDB *sql.DB) error {
		r, qerr := sqlDB.QueryContext(ctx, query, args...)
		if qerr != nil {
			return fmt.Errorf("query execution failed: %w", qerr)
		}
		defer r.Close()
		columns, qerr := r.Columns()
		if qerr != nil {
			return fmt.Errorf("get columns: %w", qerr)
		}
		rows = make([]map[string]interface{}, 0, 16)
		for r.Next() {
			values := make([]interface{}, len(columns))
			ptrs := make([]interface{}, len(columns))
			for i := range columns {
				ptrs[i] = &values[i]
			}
			if serr := r.Scan(ptrs...); serr != nil {
				return fmt.Errorf("scan row: %w", serr)
			}
			row := make(map[string]interface{}, len(columns))
			for i, col := range columns {
				row[col] = values[i]
			}
			rows = append(rows, row)
		}
		return r.Err()
	})
	return rows, err
}

// normalizeSQLStatementRow projects a row from
// kwdb_internal.node_statement_statistics into the same internal
// `statement` view that `parseStatementsResponse` produced from the
// old `/_status/statements` envelope. Latency fields are converted from
// seconds to milliseconds to keep the rest of the filter/sort pipeline
// (which speaks ms) unchanged. Database/user metadata is available in the
// source columns but the `statement` view does not currently surface it;
// we leave room for it by extracting the fingerprint/key string verbatim
// and the count+latencies in ms.
func normalizeSQLStatementRow(row map[string]interface{}) statement {
	return statement{
		Query:          stringField(row, "key"),
		Fingerprint:    stringField(row, "key"),
		ServiceLatency: floatField(row, "service_lat_avg") * 1000.0,
		RunLatency:     floatField(row, "run_lat_avg") * 1000.0,
		PlanLatency:    floatField(row, "plan_lat_avg") * 1000.0,
		Count:          int64Field(row, "count"),
	}
}

func stringField(row map[string]interface{}, key string) string {
	if v, ok := row[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func int64Field(row map[string]interface{}, key string) int64 {
	if v, ok := row[key]; ok && v != nil {
		switch x := v.(type) {
		case int64:
			return x
		case int:
			return int64(x)
		case float64:
			return int64(x)
		case string:
			// KaiwuDB sometimes serialises counters as quoted strings;
			// mirror the jsonStringInt behaviour for resilience.
			n, _ := strconv.ParseInt(x, 10, 64)
			return n
		}
	}
	return 0
}

func floatField(row map[string]interface{}, key string) float64 {
	if v, ok := row[key]; ok && v != nil {
		switch x := v.(type) {
		case float64:
			return x
		case int64:
			return float64(x)
		case int:
			return float64(x)
		}
	}
	return 0
}

// registerQuerySlowSqlTool wires the MCP `query-slow-sql` tool descriptor
// and its handler into the provided server. The handler pulls the admin
// URL from the X-Admin-Base-URL request header (falling back to the DB
// URL via the standard three-level chain), runs `executeSlowSQLQuery`,
// and wraps the filtered statement list in the structured MCP envelope
// that `read-query` / `query-metrics` use (status/type/data/error shape).
func registerQuerySlowSqlTool(s *server.MCPServer, defaultDatabaseURI string) {
	tool := mcp.NewToolWithRawSchema(
		"query-slow-sql",
		"Query the KaiwuDB `/_status/statements` admin endpoint to retrieve the top slow SQL statements, optionally filtered by service latency and ranked by one of four latency/count keys.",
		json.RawMessage(slowSqlInputSchema),
	)
	tool.RawOutputSchema = json.RawMessage(validOutputSchema)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input slowSqlInput
		if err := request.BindArguments(&input); err != nil {
			return mcp.NewToolResultErrorFromErr("Invalid slow SQL arguments", err), nil
		}

		headerURI := request.Header.Get("X-Database-URI")
		_ = request.Header.Get("X-Admin-Base-URL") // no longer used; SQL-only path

		// Same Claude Code stub-header workaround as metrics: when the
		// header carries an empty-credentials DSN, fall back to the
		// registration-time default. Without this, SQL auth fails with
		// `pq: password authentication failed for user root`.
		effectiveDBURI := headerURI
		if effectiveDBURI == "" || !hasUsableCredentials(effectiveDBURI) {
			effectiveDBURI = defaultDatabaseURI
		}

		statements, err := executeSlowSQLQuery(ctx, input, effectiveDBURI)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("Slow SQL query failed", err), nil
		}

		response := map[string]any{
			"status": "success",
			"type":   "slow_sql",
			"data":   statements,
			"error":  nil,
		}

		jsonResult, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to serialize slow SQL result: %v", err)
		}

		return mcp.NewToolResultStructured(response, string(jsonResult)), nil
	})
}
