package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// TestValidateSlowSQLInput pins the spec contract for the `query-slow-sql`
// input payload: the zero value means "defaults" (top 10, no latency floor,
// sorted by service latency) and every out-of-range field fails with the
// exact error text the spec requires.
func TestValidateSlowSQLInput(t *testing.T) {
	// Zero value is the documented default request (no arguments at all).
	if err := validateSlowSQLInput(slowSqlInput{}); err != nil {
		t.Fatalf("zero-value input should pass validation: %v", err)
	}

	// Explicitly populated valid input passes.
	explicit := slowSqlInput{Limit: 20, MinLatencyMS: 100, SortBy: "run_lat"}
	if err := validateSlowSQLInput(explicit); err != nil {
		t.Fatalf("explicit valid input should pass validation: %v", err)
	}

	// Negative limit fails with the spec error text.
	err := validateSlowSQLInput(slowSqlInput{Limit: -1})
	if err == nil {
		t.Fatal("limit=-1 should fail validation")
	}
	if err.Error() != "limit must be positive" {
		t.Fatalf("unexpected error text: got %q, want %q", err.Error(), "limit must be positive")
	}

	// Negative min latency fails with the spec error text.
	err = validateSlowSQLInput(slowSqlInput{MinLatencyMS: -1})
	if err == nil {
		t.Fatal("min_latency_ms=-1 should fail validation")
	}
	if err.Error() != "min_latency_ms must be non-negative" {
		t.Fatalf("unexpected error text: got %q, want %q", err.Error(), "min_latency_ms must be non-negative")
	}

	// Unknown sort_by fails and echoes the offending value.
	err = validateSlowSQLInput(slowSqlInput{SortBy: "unknown"})
	if err == nil {
		t.Fatal(`sort_by="unknown" should fail validation`)
	}
	if err.Error() != "unknown sort_by: unknown" {
		t.Fatalf("unexpected error text: got %q, want %q", err.Error(), "unknown sort_by: unknown")
	}

	// Every allowed sort key passes.
	for _, sortBy := range []string{"service_lat", "run_lat", "plan_lat", "count"} {
		if err := validateSlowSQLInput(slowSqlInput{SortBy: sortBy}); err != nil {
			t.Fatalf("sort_by=%q should pass validation: %v", sortBy, err)
		}
	}

	// A limit of 0 is the "unset" marker, not an invalid value; positive
	// limits are accepted as-is.
	if err := validateSlowSQLInput(slowSqlInput{Limit: 1}); err != nil {
		t.Fatalf("limit=1 should pass validation: %v", err)
	}

	// Zero min latency is a valid explicit floor (no filtering).
	if err := validateSlowSQLInput(slowSqlInput{MinLatencyMS: 0}); err != nil {
		t.Fatalf("min_latency_ms=0 should pass validation: %v", err)
	}
}

// TestApplySlowSQLDefaults verifies the zero value expands to the documented
// defaults and that caller-provided values are never overwritten.
func TestApplySlowSQLDefaults(t *testing.T) {
	got := applySlowSQLDefaults(slowSqlInput{})
	if got.Limit != 10 {
		t.Errorf("default Limit = %d, want 10", got.Limit)
	}
	if got.MinLatencyMS != 0 {
		t.Errorf("default MinLatencyMS = %v, want 0", got.MinLatencyMS)
	}
	if got.SortBy != "service_lat" {
		t.Errorf("default SortBy = %q, want %q", got.SortBy, "service_lat")
	}

	explicit := slowSqlInput{Limit: 20, MinLatencyMS: 100, SortBy: "count"}
	if got := applySlowSQLDefaults(explicit); got != explicit {
		t.Errorf("explicit input should be unchanged: got %+v, want %+v", got, explicit)
	}

	// Defaults must not mutate the caller's value.
	original := slowSqlInput{}
	_ = applySlowSQLDefaults(original)
	if original != (slowSqlInput{}) {
		t.Errorf("applySlowSQLDefaults mutated its argument: %+v", original)
	}
}

// TestStatementFields is a compile-time-ish guard that the statement type
// carries every field the `/_status/statements` mapping needs.
func TestStatementFields(t *testing.T) {
	s := statement{
		ID:             "1234",
		Fingerprint:    "SELECT * FROM t WHERE x = _",
		Query:          "SELECT * FROM t WHERE x = 1",
		ServiceLatency: 12.5,
		RunLatency:     8.25,
		PlanLatency:    1.5,
		Count:          42,
	}
	if s.ID != "1234" || s.Fingerprint == "" || s.Query == "" {
		t.Fatalf("statement string fields not preserved: %+v", s)
	}
	if s.ServiceLatency != 12.5 || s.RunLatency != 8.25 || s.PlanLatency != 1.5 {
		t.Fatalf("statement latency fields not preserved: %+v", s)
	}
	if s.Count != 42 {
		t.Fatalf("statement Count not preserved: %+v", s)
	}
}

// TestParseStatementsResponse pins the KaiwuDB `/_status/statements` JSON
// envelope contract: a successful response decodes into a []statement
// whose query, latencies (converted from seconds to milliseconds) and
// count match the API's nested `key.keyData` / `stats.*Lat.mean` fields.
// Malformed bodies — missing required fields or non-JSON — must return a
// locatable error rather than silently producing an empty slice.
func TestParseStatementsResponse(t *testing.T) {
	// Success: standard envelope with two statements; latencies are
	// reported in seconds and must come out in milliseconds.
	successBody := []byte(`{
		"statements": [
			{
				"key": {
					"keyData": {
						"query": "SELECT * FROM t WHERE x = 1",
						"app": "app1",
						"user": "user1",
						"database": "db1"
					},
					"nodeId": "node-1"
				},
				"stats": {
					"count": 42,
					"firstAttemptCount": 40,
					"serviceLat": {"mean": 0.0125},
					"runLat": {"mean": 0.00825},
					"planLat": {"mean": 0.0015},
					"parseLat": {"mean": 0.00075}
				}
			},
			{
				"key": {
					"keyData": {
						"query": "UPDATE t SET y = 2",
						"app": "app2",
						"user": "user2",
						"database": "db2"
					},
					"nodeId": "node-2"
				},
				"stats": {
					"count": 7,
					"serviceLat": {"mean": 0.250},
					"runLat": {"mean": 0.100},
					"planLat": {"mean": 0.025}
				}
			}
		]
	}`)

	got, err := parseStatementsResponse(successBody)
	if err != nil {
		t.Fatalf("parseStatementsResponse on valid envelope failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed statement count = %d, want 2", len(got))
	}

	first := got[0]
	if first.Query != "SELECT * FROM t WHERE x = 1" {
		t.Errorf("first.Query = %q, want %q", first.Query, "SELECT * FROM t WHERE x = 1")
	}
	if first.ServiceLatency != 12.5 {
		t.Errorf("first.ServiceLatency = %v ms, want 12.5 ms", first.ServiceLatency)
	}
	if first.RunLatency != 8.25 {
		t.Errorf("first.RunLatency = %v ms, want 8.25 ms", first.RunLatency)
	}
	if first.PlanLatency != 1.5 {
		t.Errorf("first.PlanLatency = %v ms, want 1.5 ms", first.PlanLatency)
	}
	if first.Count != 42 {
		t.Errorf("first.Count = %d, want 42", first.Count)
	}

	second := got[1]
	if second.Query != "UPDATE t SET y = 2" {
		t.Errorf("second.Query = %q, want %q", second.Query, "UPDATE t SET y = 2")
	}
	if second.ServiceLatency != 250.0 {
		t.Errorf("second.ServiceLatency = %v ms, want 250 ms", second.ServiceLatency)
	}
	if second.RunLatency != 100.0 {
		t.Errorf("second.RunLatency = %v ms, want 100 ms", second.RunLatency)
	}
	if second.PlanLatency != 25.0 {
		t.Errorf("second.PlanLatency = %v ms, want 25 ms", second.PlanLatency)
	}
	if second.Count != 7 {
		t.Errorf("second.Count = %d, want 7", second.Count)
	}

	// Empty envelope: no statements → empty slice, no error.
	empty, err := parseStatementsResponse([]byte(`{"statements": []}`))
	if err != nil {
		t.Fatalf("parseStatementsResponse on empty envelope failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("empty envelope produced %d statements, want 0", len(empty))
	}

	// Missing `statements` key still decodes to an empty slice (mirrors the
	// Python helper's `data.get("statements", [])`).
	noStatements, err := parseStatementsResponse([]byte(`{"lastReset": "x"}`))
	if err != nil {
		t.Fatalf("parseStatementsResponse without statements key failed: %v", err)
	}
	if len(noStatements) != 0 {
		t.Errorf("missing-statements envelope produced %d statements, want 0", len(noStatements))
	}

	// Missing `key` → locatable error mentioning the field.
	missingKey := []byte(`{
		"statements": [
			{"stats": {"count": 1, "serviceLat": {"mean": 0.001}}}
		]
	}`)
	if _, err := parseStatementsResponse(missingKey); err == nil {
		t.Fatal("missing `key` should return an error")
	} else if !strings.Contains(err.Error(), "key") {
		t.Errorf("missing-key error %q should mention 'key'", err.Error())
	}

	// Missing `stats` → locatable error mentioning the field.
	missingStats := []byte(`{
		"statements": [
			{"key": {"keyData": {"query": "SELECT 1"}, "nodeId": "n1"}}
		]
	}`)
	if _, err := parseStatementsResponse(missingStats); err == nil {
		t.Fatal("missing `stats` should return an error")
	} else if !strings.Contains(err.Error(), "stats") {
		t.Errorf("missing-stats error %q should mention 'stats'", err.Error())
	}

	// Non-JSON input → error.
	if _, err := parseStatementsResponse([]byte("not json at all")); err == nil {
		t.Fatal("non-JSON input should return an error")
	}

	// Missing `statements` is OK; any other malformed shape that
	// produces an undecodable envelope must also surface an error.
	if _, err := parseStatementsResponse([]byte(`{"statements": "oops"}`)); err == nil {
		t.Fatal("malformed `statements` payload should return an error")
	}

	// Counter fields as quoted strings — the KaiwuDB API sometimes
	// serialises them as `"count":"11"` instead of `"count":11`.
	// Both forms must decode to the same int64 value.
	quotedCountBody := []byte(`{
		"statements": [
			{
				"key": {"keyData": {"query": "SELECT 1"}, "nodeId": "n1"},
				"stats": {
					"count": "11",
					"firstAttemptCount": "10",
					"maxRetries": "0",
					"serviceLat": {"mean": 0.5},
					"runLat":     {"mean": 0.4},
					"planLat":    {"mean": 0.05}
				}
			}
		]
	}`)
	quoted, err := parseStatementsResponse(quotedCountBody)
	if err != nil {
		t.Fatalf("parseStatementsResponse with quoted counters failed: %v", err)
	}
	if len(quoted) != 1 {
		t.Fatalf("quoted-count parser returned %d statements, want 1", len(quoted))
	}
	if quoted[0].Count != 11 {
		t.Errorf("quoted count parsed as %d, want 11", quoted[0].Count)
	}
}

// TestJSONStringInt pins the lenient JSON decoder for integer counters
// emitted by the KaiwuDB `/_status/statements` API. Some clusters ship
// them as quoted strings ("11"), others as plain numbers (11); the
// wrapper must accept either shape.
func TestJSONStringInt(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		{"plain number", `42`, 42},
		{"quoted number", `"42"`, 42},
		{"zero number", `0`, 0},
		{"zero quoted", `"0"`, 0},
		{"negative number", `-7`, -7},
		{"negative quoted", `"-7"`, -7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got jsonStringInt
			if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", tc.in, err)
			}
			if int64(got) != tc.want {
				t.Errorf("unmarshal %q = %d, want %d", tc.in, int64(got), tc.want)
			}
		})
	}

	// Non-numeric quoted strings surface as a parse error, not a silent 0.
	var got jsonStringInt
	if err := json.Unmarshal([]byte(`"not-a-number"`), &got); err == nil {
		t.Errorf("non-numeric quoted string should fail to parse, got %d", int64(got))
	}
}

// TestFilterAndSortStatements pins the post-parse filtering, sorting and
// limit behavior for `query-slow-sql`. Filtering always happens on
// service_latency_ms regardless of the sort key (matches the Python
// skill's `filter_and_sort`); sorting is descending and stable so equal
// keys preserve the upstream order; truncation must not mutate the
// caller's slice.
func TestFilterAndSortStatements(t *testing.T) {
	// Baseline fixture: four statements with distinct latencies and
	// counts so each sort key produces a different ordering. The first
	// statement has service latency below the 100ms floor and must
	// drop out under any input.
	original := []statement{
		{ID: "low", Fingerprint: "low", Query: "SELECT 1", ServiceLatency: 50, RunLatency: 40, PlanLatency: 2, Count: 1000},
		{ID: "a", Fingerprint: "a", Query: "SELECT 2", ServiceLatency: 300, RunLatency: 5, PlanLatency: 10, Count: 3},
		{ID: "b", Fingerprint: "b", Query: "SELECT 3", ServiceLatency: 200, RunLatency: 250, PlanLatency: 5, Count: 50},
		{ID: "c", Fingerprint: "c", Query: "SELECT 4", ServiceLatency: 400, RunLatency: 10, PlanLatency: 25, Count: 7},
	}

	// Snapshot the original order so the "does not mutate input" check
	// can compare after every subtest without re-deriving it.
	snapshotOriginal := func() []statement {
		copySlice := make([]statement, len(original))
		for i := range original {
			copySlice[i] = original[i]
		}
		return copySlice
	}

	// Filter + sort by service_lat descending (default sort key).
	// `low` is dropped by the latency floor; the survivors rank by
	// ServiceLatency: c(400) > a(300) > b(200).
	filtered := filterAndSortStatements(snapshotOriginal(), slowSqlInput{MinLatencyMS: 100})
	if len(filtered) != 3 {
		t.Fatalf("filtered length = %d, want 3", len(filtered))
	}
	wantOrder := []string{"c", "a", "b"}
	for i, want := range wantOrder {
		if filtered[i].ID != want {
			t.Errorf("filtered[%d].ID = %q, want %q (service_lat desc)", i, filtered[i].ID, want)
		}
	}

	// Sort by run_lat descending. Survivors rank by RunLatency:
	// b(250) > c(10) > a(5).
	byRunLat := filterAndSortStatements(snapshotOriginal(), slowSqlInput{MinLatencyMS: 100, SortBy: "run_lat"})
	if len(byRunLat) != 3 {
		t.Fatalf("byRunLat length = %d, want 3", len(byRunLat))
	}
	wantOrder = []string{"b", "c", "a"}
	for i, want := range wantOrder {
		if byRunLat[i].ID != want {
			t.Errorf("byRunLat[%d].ID = %q, want %q (run_lat desc)", i, byRunLat[i].ID, want)
		}
	}

	// Sort by plan_lat descending. Survivors rank by PlanLatency:
	// c(25) > a(10) > b(5).
	byPlanLat := filterAndSortStatements(snapshotOriginal(), slowSqlInput{MinLatencyMS: 100, SortBy: "plan_lat"})
	if len(byPlanLat) != 3 {
		t.Fatalf("byPlanLat length = %d, want 3", len(byPlanLat))
	}
	wantOrder = []string{"c", "a", "b"}
	for i, want := range wantOrder {
		if byPlanLat[i].ID != want {
			t.Errorf("byPlanLat[%d].ID = %q, want %q (plan_lat desc)", i, byPlanLat[i].ID, want)
		}
	}

	// Sort by count descending. Survivors rank by Count:
	// b(50) > c(7) > a(3).
	byCount := filterAndSortStatements(snapshotOriginal(), slowSqlInput{MinLatencyMS: 100, SortBy: "count"})
	if len(byCount) != 3 {
		t.Fatalf("byCount length = %d, want 3", len(byCount))
	}
	wantOrder = []string{"b", "c", "a"}
	for i, want := range wantOrder {
		if byCount[i].ID != want {
			t.Errorf("byCount[%d].ID = %q, want %q (count desc)", i, byCount[i].ID, want)
		}
	}

	// Limit truncates the (already-filtered) result to the first N
	// entries by the chosen sort key. limit=2 + service_lat desc →
	// c, a.
	limited := filterAndSortStatements(snapshotOriginal(), slowSqlInput{MinLatencyMS: 100, Limit: 2})
	if len(limited) != 2 {
		t.Fatalf("limited length = %d, want 2", len(limited))
	}
	if limited[0].ID != "c" || limited[1].ID != "a" {
		t.Errorf("limited order = [%q, %q], want [c, a]", limited[0].ID, limited[1].ID)
	}

	// Limit greater than the filtered count returns every survivor.
	// limit=50 + MinLatencyMS=100 → all three survivors in order.
	largeLimit := filterAndSortStatements(snapshotOriginal(), slowSqlInput{MinLatencyMS: 100, Limit: 50})
	if len(largeLimit) != 3 {
		t.Fatalf("largeLimit length = %d, want 3", len(largeLimit))
	}

	// Input slice order must be preserved across every call. The
	// snapshot we built mirrors the canonical original, so a pass that
	// mutates the input would change this slice between subtests.
	for i := range original {
		want := []string{"low", "a", "b", "c"}[i]
		if original[i].ID != want {
			t.Fatalf("original slice mutated at index %d: got %q, want %q", i, original[i].ID, want)
		}
	}

	// Stability check: ties on the sort key preserve input order.
	// Two statements with identical ServiceLatency (300) and three
	// distinct Count values; sort by count desc keeps the original
	// upstream order because equal service_latency entries tie on
	// service_lat but are also tied on the secondary key.
	stableInput := []statement{
		{ID: "first", ServiceLatency: 300, RunLatency: 1, PlanLatency: 1, Count: 100},
		{ID: "second", ServiceLatency: 200, RunLatency: 1, PlanLatency: 1, Count: 50},
		{ID: "third", ServiceLatency: 300, RunLatency: 1, PlanLatency: 1, Count: 75},
	}
	// service_lat desc: 300 tie between first & third, then 200.
	// Stable sort keeps first ahead of third.
	stableOut := filterAndSortStatements(stableInput, slowSqlInput{MinLatencyMS: 100, SortBy: "service_lat"})
	if len(stableOut) != 3 {
		t.Fatalf("stableOut length = %d, want 3", len(stableOut))
	}
	if stableOut[0].ID != "first" || stableOut[1].ID != "third" || stableOut[2].ID != "second" {
		t.Errorf("sort is not stable: got [%q, %q, %q], want [first, third, second]",
			stableOut[0].ID, stableOut[1].ID, stableOut[2].ID)
	}

	// Empty input: no statements, no panic, returns nil/empty.
	if got := filterAndSortStatements(nil, slowSqlInput{}); len(got) != 0 {
		t.Errorf("nil input produced %d results, want 0", len(got))
	}
	if got := filterAndSortStatements([]statement{}, slowSqlInput{}); len(got) != 0 {
		t.Errorf("empty input produced %d results, want 0", len(got))
	}
}

// TestExecuteSlowSQLQuery exercises executeSlowSQLQuery against the
// slowSQLExecuteQueryFn seam (default: db.GetMultiPoolManager, stubbed
// here). The SQL-only path (bugfix-2026-08-07) replaces the legacy
// `/_status/statements` admin endpoint. Each sub-test installs a stub
// that returns canned rows shaped like kwdb_internal.node_statement_statistics
// so the existing filter/sort/limit pipeline is still exercised end to end.
//
// Coverage:
//   - Default filter (min_latency_ms=100) drops a 12.5ms row, leaves a
//     single 250ms survivor by service_lat desc.
//   - Latency unit conversion (seconds → ms) for all three latency cols.
//   - count column flows through as int64.
//   - SlowSQL executor fails fast when X-Database-URI is empty (SQL-only
//     path has no admin URL fallback).
//   - SlowSQL executor surfaces DB errors wrapped with the source table
//     name so operators can tell which system catalog is missing.
func TestExecuteSlowSQLQuery(t *testing.T) {
	const dbURL = "postgresql://root@db.example.com:26257/kwdb?sslmode=disable"

	cannedRows := func() []map[string]interface{} {
		return []map[string]interface{}{
			{
				"key":             "SELECT * FROM t WHERE x = 1",
				"count":           int64(42),
				"service_lat_avg": 0.0125, // 12.5 ms
				"run_lat_avg":     0.00825,
				"plan_lat_avg":    0.0015,
				"database":        "db1",
				"user_name":       "user1",
			},
			{
				"key":             "UPDATE t SET y = 2",
				"count":           int64(7),
				"service_lat_avg": 0.250, // 250 ms
				"run_lat_avg":     0.100,
				"plan_lat_avg":    0.025,
				"database":        "db2",
				"user_name":       "user2",
			},
		}
	}

	t.Run("filter 100ms drops low-latency row, latency converted to ms", func(t *testing.T) {
		prev := slowSQLExecuteQueryFn
		slowSQLExecuteQueryFn = func(ctx context.Context, dbURL, query string, args ...interface{}) ([]map[string]interface{}, error) {
			// Validate args passed through.
			if len(args) != 2 {
				t.Fatalf("args = %d, want 2 (min_latency_seconds, limit)", len(args))
			}
			if got, _ := args[0].(float64); got != 0.1 {
				t.Fatalf("min_latency_seconds arg = %v, want 0.1 (100ms)", got)
			}
			if got, _ := args[1].(int); got != 10 {
				t.Fatalf("limit arg = %d, want 10", got)
			}
			// Validate the SQL embeds the validated sort column.
			if !strings.Contains(query, "ORDER BY service_lat_avg DESC") {
				t.Fatalf("query does not embed sorted column: %q", query)
			}
			if !strings.Contains(query, "WHERE service_lat_avg >= $1") {
				t.Fatalf("query missing WHERE clause: %q", query)
			}
			return cannedRows(), nil
		}
		defer func() { slowSQLExecuteQueryFn = prev }()

		got, err := executeSlowSQLQuery(context.Background(),
			slowSqlInput{MinLatencyMS: 100}, dbURL)
		if err != nil {
			t.Fatalf("executeSlowSQLQuery unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("filtered statements length = %d, want 1 (only service_latency >= 100ms)", len(got))
		}
		if got[0].Query != "UPDATE t SET y = 2" {
			t.Errorf("survivor query = %q, want %q", got[0].Query, "UPDATE t SET y = 2")
		}
		if got[0].ServiceLatency != 250.0 {
			t.Errorf("survivor ServiceLatency = %v ms, want 250 ms", got[0].ServiceLatency)
		}
		if got[0].Count != 7 {
			t.Errorf("survivor Count = %d, want 7", got[0].Count)
		}
	})

	t.Run("missing X-Database-URI returns explicit error", func(t *testing.T) {
		got, err := executeSlowSQLQuery(context.Background(), slowSqlInput{}, "")
		if err == nil {
			t.Fatal("expected error for missing X-Database-URI, got nil")
		}
		if !strings.Contains(err.Error(), "X-Database-URI") {
			t.Fatalf("error = %q, should mention X-Database-URI", err.Error())
		}
		if got != nil {
			t.Fatalf("result = %v, want nil on error", got)
		}
	})

	t.Run("underlying SQL error is wrapped with the source table name", func(t *testing.T) {
		prev := slowSQLExecuteQueryFn
		slowSQLExecuteQueryFn = func(ctx context.Context, dbURL, query string, args ...interface{}) ([]map[string]interface{}, error) {
			return nil, fmt.Errorf("relation \"kwdb_internal.node_statement_statistics\" does not exist")
		}
		defer func() { slowSQLExecuteQueryFn = prev }()

		_, err := executeSlowSQLQuery(context.Background(), slowSqlInput{}, dbURL)
		if err == nil {
			t.Fatal("expected error from stub, got nil")
		}
		if !strings.Contains(err.Error(), "kwdb_internal.node_statement_statistics") {
			t.Fatalf("error = %q, should mention source table", err.Error())
		}
	})

	t.Run("sort_by=count reorders by count desc and converts latencies from seconds", func(t *testing.T) {
		prev := slowSQLExecuteQueryFn
		slowSQLExecuteQueryFn = func(ctx context.Context, dbURL, query string, args ...interface{}) ([]map[string]interface{}, error) {
			if !strings.Contains(query, "ORDER BY count DESC") {
				t.Fatalf("query does not use count sort: %q", query)
			}
			return cannedRows(), nil
		}
		defer func() { slowSQLExecuteQueryFn = prev }()

		got, err := executeSlowSQLQuery(context.Background(),
			slowSqlInput{SortBy: "count"}, dbURL)
		if err != nil {
			t.Fatalf("executeSlowSQLQuery unexpected error: %v", err)
		}
		// count desc: row with count=42 first.
		if got[0].Count != 42 {
			t.Errorf("count desc top = %d, want 42", got[0].Count)
		}
		if got[1].Count != 7 {
			t.Errorf("count desc second = %d, want 7", got[1].Count)
		}
		// Latency was in seconds (0.0125 → 12.5 ms for count=42, 0.250 → 250 ms for count=7).
		if got[0].ServiceLatency != 12.5 {
			t.Errorf("count=42 row service_latency_ms = %v, want 12.5", got[0].ServiceLatency)
		}
		if got[1].ServiceLatency != 250.0 {
			t.Errorf("count=7 row service_latency_ms = %v, want 250", got[1].ServiceLatency)
		}
	})
}

// TestRegisterQuerySlowSqlTool exercises the MCP registration side of the
// query-slow-sql tool: the descriptor must appear under the exact canonical
// name, the input schema must contain the three documented optional
// fields, and the handler must bind `limit`/`min_latency_ms`/`sort_by`
// exactly while honouring the X-Admin-Base-URL / X-Database-URI request
// headers. The handler is driven end-to-end against a fake admin endpoint
// so a schema vs. bind mismatch shows up immediately as wrong query
// behaviour.
func TestRegisterQuerySlowSqlTool(t *testing.T) {
	s := mcpserver.NewMCPServer("test", "1.0", mcpserver.WithToolCapabilities(true))
	registerQuerySlowSqlTool(s)
	tools := s.ListTools()
	got, ok := tools["query-slow-sql"]
	if !ok {
		t.Fatalf("query-slow-sql tool was not registered: %v", tools)
	}
	if got.Tool.Name != "query-slow-sql" {
		t.Fatalf("tool.Name = %q, want %q", got.Tool.Name, "query-slow-sql")
	}
	if _, err := json.Marshal(got.Tool); err != nil {
		t.Fatalf("tool should marshal cleanly, got error: %v", err)
	}

	t.Run("input schema declares limit/min_latency_ms/sort_by optional fields", func(t *testing.T) {
		var schema struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type        string   `json:"type"`
				Description string   `json:"description"`
				Enum        []string `json:"enum"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(got.Tool.RawInputSchema, &schema); err != nil {
			t.Fatalf("decode RawInputSchema: %v (raw=%q)", err, string(got.Tool.RawInputSchema))
		}
		if schema.Type != "object" {
			t.Fatalf("schema.type = %q, want %q", schema.Type, "object")
		}

		limit, ok := schema.Properties["limit"]
		if !ok {
			t.Fatalf("schema is missing the limit property: %q", string(got.Tool.RawInputSchema))
		}
		if limit.Type != "integer" {
			t.Errorf("limit.type = %q, want %q", limit.Type, "integer")
		}
		if limit.Description == "" {
			t.Error("limit must carry a description")
		}

		minLat, ok := schema.Properties["min_latency_ms"]
		if !ok {
			t.Fatalf("schema is missing the min_latency_ms property: %q", string(got.Tool.RawInputSchema))
		}
		if minLat.Type != "number" {
			t.Errorf("min_latency_ms.type = %q, want %q", minLat.Type, "number")
		}
		if minLat.Description == "" {
			t.Error("min_latency_ms must carry a description")
		}

		sortBy, ok := schema.Properties["sort_by"]
		if !ok {
			t.Fatalf("schema is missing the sort_by property: %q", string(got.Tool.RawInputSchema))
		}
		if sortBy.Type != "string" {
			t.Errorf("sort_by.type = %q, want %q", sortBy.Type, "string")
		}
		wantEnum := map[string]bool{
			"service_lat": false, "run_lat": false, "plan_lat": false, "count": false,
		}
		for _, v := range sortBy.Enum {
			if _, known := wantEnum[v]; !known {
				t.Errorf("unexpected sort_by enum value %q", v)
				continue
			}
			wantEnum[v] = true
		}
		for v, seen := range wantEnum {
			if !seen {
				t.Errorf("sort_by enum missing %q (got %v)", v, sortBy.Enum)
			}
		}
		if sortBy.Description == "" {
			t.Error("sort_by must carry a description")
		}

		// All three fields are optional: the zero value is the documented
		// default request. A required entry would silently hide the
		// "call the tool with no arguments" behaviour.
		if len(schema.Required) != 0 {
			t.Errorf("schema.required should be empty for an all-optional argument set, got %v",
				schema.Required)
		}
	})

	t.Run("handler binds limit/min_latency_ms/sort_by and queries the system table", func(t *testing.T) {
		var (
			calls       atomic.Int32
			gotQuery    string
			gotDBURL    string
			gotArgCount int
		)
		prev := slowSQLExecuteQueryFn
		slowSQLExecuteQueryFn = func(ctx context.Context, dbURL, query string, args ...interface{}) ([]map[string]interface{}, error) {
			calls.Add(1)
			gotQuery = query
			gotDBURL = dbURL
			gotArgCount = len(args)
			return []map[string]interface{}{
				{
					"key":             "SELECT * FROM t WHERE x = 1",
					"count":           int64(1),
					"service_lat_avg": 0.500, // 500 ms
					"run_lat_avg":     0.100,
					"plan_lat_avg":    0.020,
				},
			}, nil
		}
		defer func() { slowSQLExecuteQueryFn = prev }()

		req := mcp.CallToolRequest{Header: http.Header{}}
		req.Header.Set("X-Database-URI", "postgresql://root@db.example.com:26257/kwdb?sslmode=disable")
		req.Params.Arguments = map[string]any{
			"limit":          5,
			"min_latency_ms": 50.0,
			"sort_by":        "run_lat",
		}

		res, err := got.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned a transport error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned an error result: %+v", res.Content)
		}
		if calls.Load() != 1 {
			t.Fatalf("SQL call count = %d, want 1", calls.Load())
		}
		if gotDBURL != "postgresql://root@db.example.com:26257/kwdb?sslmode=disable" {
			t.Fatalf("DB URL passed through = %q, want X-Database-URI value", gotDBURL)
		}
		if !strings.Contains(gotQuery, "ORDER BY run_lat_avg DESC") {
			t.Fatalf("query did not embed run_lat sort: %q", gotQuery)
		}
		if gotArgCount != 2 {
			t.Errorf("arg count = %d, want 2 (min_latency_seconds, limit)", gotArgCount)
		}
	})

	t.Run("descriptor advertises the shared valid output schema", func(t *testing.T) {
		// Clients such as Cursor reject a tool whose output schema is absent
		// or not a JSON Schema object, so the descriptor must carry the same
		// minimal schema read-query / query-metrics advertise.
		if string(got.Tool.RawOutputSchema) != string(validOutputSchema) {
			t.Fatalf("RawOutputSchema = %q, want %q",
				string(got.Tool.RawOutputSchema), string(validOutputSchema))
		}
	})

	t.Run("handler wraps BindArguments failure as an MCP error result", func(t *testing.T) {
		// A string where the schema declares an integer fails to unmarshal
		// into slowSqlInput. The handler must convert that into an error
		// result rather than a transport error, and must fail before any
		// admin call — no X-Admin-Base-URL is set here, so reaching the
		// network would surface as a different error text.
		req := mcp.CallToolRequest{Header: http.Header{}}
		req.Params.Arguments = map[string]any{"limit": "ten"}

		res, err := got.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler should wrap the bind failure in the result, got transport error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("handler should have reported an error result for an unbindable argument, got %+v",
				res.Content)
		}
		text, ok := mcp.AsTextContent(res.Content[0])
		if !ok {
			t.Fatalf("error result content[0] = %T, want mcp.TextContent", res.Content[0])
		}
		if !strings.Contains(text.Text, "Invalid slow SQL arguments") {
			t.Fatalf("error text %q should identify the argument binding failure", text.Text)
		}
	})

	t.Run("no arguments is equivalent to limit 10 sorted by service_lat", func(t *testing.T) {
		// Twelve statements whose service latency ascends with the index and
		// whose run latency descends. Defaults must keep the ten highest
		// service latencies in descending order; if the handler silently
		// sorted by run_lat instead, the head of the list would invert.
		const total = 12
		var rows []map[string]interface{}
		for i := 0; i < total; i++ {
			rows = append(rows, map[string]interface{}{
				"key":             fmt.Sprintf("SELECT %d", i),
				"count":           int64(1),
				"service_lat_avg": float64(i+1) / 1000.0,
				"run_lat_avg":     float64(total-i) / 1000.0,
				"plan_lat_avg":    0.001,
			})
		}

		prev := slowSQLExecuteQueryFn
		slowSQLExecuteQueryFn = func(ctx context.Context, dbURL, query string, args ...interface{}) ([]map[string]interface{}, error) {
			return rows, nil
		}
		defer func() { slowSQLExecuteQueryFn = prev }()

		// Arguments deliberately left nil: this is the "call the tool with
		// no arguments" path the spec's 默认查询 scenario pins.
		req := mcp.CallToolRequest{Header: http.Header{}}
		req.Header.Set("X-Database-URI", "postgresql://root@db.example.com:26257/kwdb?sslmode=disable")

		res, err := got.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned a transport error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned an error result: %+v", res.Content)
		}

		text, ok := mcp.AsTextContent(res.Content[0])
		if !ok {
			t.Fatalf("result content[0] = %T, want mcp.TextContent", res.Content[0])
		}
		var payload struct {
			Status string      `json:"status"`
			Type   string      `json:"type"`
			Data   []statement `json:"data"`
			Error  *string     `json:"error"`
		}
		if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
			t.Fatalf("decode result payload: %v (raw=%q)", err, text.Text)
		}
		if payload.Status != "success" {
			t.Errorf("status = %q, want %q", payload.Status, "success")
		}
		if payload.Type != "slow_sql" {
			t.Errorf("type = %q, want %q", payload.Type, "slow_sql")
		}
		if payload.Error != nil {
			t.Errorf("error = %v, want null", *payload.Error)
		}

		// Default limit is 10, so two of the twelve statements are dropped.
		if len(payload.Data) != defaultSlowSQLLimit {
			t.Fatalf("len(data) = %d, want %d (default limit not applied?)",
				len(payload.Data), defaultSlowSQLLimit)
		}
		// Default sort is service_lat descending: the 12ms statement leads
		// and the 3ms one closes the truncated list.
		if payload.Data[0].ServiceLatency != 12 {
			t.Errorf("data[0].service_latency_ms = %v, want 12", payload.Data[0].ServiceLatency)
		}
		if payload.Data[len(payload.Data)-1].ServiceLatency != 3 {
			t.Errorf("data[last].service_latency_ms = %v, want 3",
				payload.Data[len(payload.Data)-1].ServiceLatency)
		}
		for i := 1; i < len(payload.Data); i++ {
			if payload.Data[i-1].ServiceLatency < payload.Data[i].ServiceLatency {
				t.Fatalf("data is not sorted by service latency descending at index %d: %v",
					i, payload.Data)
			}
		}
		// The run latencies run the other way, so a run_lat sort would have
		// put the 1ms service-latency statement first instead.
		if payload.Data[0].RunLatency != 1 {
			t.Errorf("data[0].run_latency_ms = %v, want 1 (sorted by run_lat instead of service_lat?)",
				payload.Data[0].RunLatency)
		}
	})

	t.Run("handler wraps missing X-Database-URI as MCP error", func(t *testing.T) {
		// bugfix-2026-08-07: slow-sql is SQL-only — empty X-Database-URI
		// yields an explicit error mentioning the header name, so the
		// operator knows which request header is required.
		req := mcp.CallToolRequest{Header: http.Header{}}
		req.Params.Arguments = map[string]any{}

		res, err := got.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler should wrap the failure in the result, got transport error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("handler should have reported an error result for missing X-Database-URI, got %+v",
				res.Content)
		}
		text, ok := mcp.AsTextContent(res.Content[0])
		if !ok {
			t.Fatalf("error result content[0] = %T, want mcp.TextContent", res.Content[0])
		}
		if !strings.Contains(text.Text, "X-Database-URI") {
			t.Fatalf("error text %q should mention X-Database-URI", text.Text)
		}
	})
}

// ensure mcp.CallToolRequest stays referenced even if a future edit removes
// every other call site in this file.
var _ = mcp.CallToolRequest{}
