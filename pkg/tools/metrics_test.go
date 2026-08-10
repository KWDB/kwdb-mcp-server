package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestValidateMetricsInput(t *testing.T) {
	// Single valid metric passes.
	single := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       1000,
		End:         2000,
		Sample:      500,
	}
	if err := validateMetricsInput(single); err != nil {
		t.Fatalf("valid single-metric input should pass: %v", err)
	}

	// Multiple valid metrics pass.
	multi := metricsInput{
		MetricNames: []string{
			"cr.node.sql.query.count",
			"cr.node.sys.rss",
			"cr.node.liveness.livenodes",
		},
		Start:  1000,
		End:    2000,
		Sample: 500,
	}
	if err := validateMetricsInput(multi); err != nil {
		t.Fatalf("valid multi-metric input should pass: %v", err)
	}

	// Empty metric_names fails.
	empty := metricsInput{
		MetricNames: []string{},
		Start:       1000,
		End:         2000,
		Sample:      500,
	}
	if err := validateMetricsInput(empty); err == nil {
		t.Fatal("empty MetricNames should fail validation")
	}

	// Unknown metric name fails with the spec error text.
	unknown := metricsInput{
		MetricNames: []string{"cr.node.does.not.exist"},
		Start:       1000,
		End:         2000,
		Sample:      500,
	}
	err := validateMetricsInput(unknown)
	if err == nil {
		t.Fatal("unknown metric should fail validation")
	}
	const wantUnknownPrefix = "unknown metric: "
	if !strings.HasPrefix(err.Error(), wantUnknownPrefix) ||
		err.Error() != wantUnknownPrefix+"cr.node.does.not.exist" {
		t.Fatalf("unknown metric error: got %q, want %q",
			err.Error(), wantUnknownPrefix+"cr.node.does.not.exist")
	}

	// Unknown metric must be rejected before any HTTP call: ensure error
	// appears even when start/end/sample are zero (i.e. validation cannot
	// be racing other dependencies).
	zero := metricsInput{
		MetricNames: []string{"cr.node.does.not.exist"},
	}
	if err := validateMetricsInput(zero); err == nil {
		t.Fatal("unknown metric must fail validation even with zero time fields")
	}

	// Start >= End fails with the metrics_history convention text.
	startGEEnd := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       2000,
		End:         2000,
		Sample:      500,
	}
	err = validateMetricsInput(startGEEnd)
	if err == nil {
		t.Fatal("Start >= End should fail validation")
	}
	if err.Error() != "start_ms must be less than end_ms" {
		t.Fatalf("Start>=End error: got %q, want %q",
			err.Error(), "start_ms must be less than end_ms")
	}

	startGTEnd := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       3000,
		End:         2000,
		Sample:      500,
	}
	if err := validateMetricsInput(startGTEnd); err == nil {
		t.Fatal("Start > End should fail validation")
	}

	// Sample <= 0 fails.
	sampleZero := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       1000,
		End:         2000,
		Sample:      0,
	}
	err = validateMetricsInput(sampleZero)
	if err == nil {
		t.Fatal("Sample=0 should fail validation")
	}
	if err.Error() != "sample_ms must be greater than 0" {
		t.Fatalf("Sample=0 error: got %q, want %q",
			err.Error(), "sample_ms must be greater than 0")
	}

	// End <= 0 fails (would otherwise send end_nanos=0 inverted window).
	endZero := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       1000,
		End:         0,
		Sample:      500,
	}
	err = validateMetricsInput(endZero)
	if err == nil {
		t.Fatal("End=0 should fail validation (avoids end_nanos=0 inverted window)")
	}
	if err.Error() != "end_ms must be greater than 0" {
		t.Fatalf("End=0 error: got %q, want %q",
			err.Error(), "end_ms must be greater than 0")
	}

	// Start <= 0 fails.
	startZero := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       0,
		End:         2000,
		Sample:      500,
	}
	err = validateMetricsInput(startZero)
	if err == nil {
		t.Fatal("Start=0 should fail validation")
	}
	if err.Error() != "start_ms must be greater than 0" {
		t.Fatalf("Start=0 error: got %q, want %q",
			err.Error(), "start_ms must be greater than 0")
	}

	// End negative fails.
	endNegative := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       1000,
		End:         -1,
		Sample:      500,
	}
	err = validateMetricsInput(endNegative)
	if err == nil {
		t.Fatal("End<0 should fail validation")
	}
	if err.Error() != "end_ms must be greater than 0" {
		t.Fatalf("End<0 error: got %q, want %q",
			err.Error(), "end_ms must be greater than 0")
	}

	sampleNegative := metricsInput{
		MetricNames: []string{"cr.node.sql.query.count"},
		Start:       1000,
		End:         2000,
		Sample:      -1,
	}
	if err := validateMetricsInput(sampleNegative); err == nil {
		t.Fatal("Sample<0 should fail validation")
	}

	// Whitespace around a known metric name is trimmed before lookup, so
	// padded valid names pass.
	padded := metricsInput{
		MetricNames: []string{"  cr.node.sql.query.count  "},
		Start:       1000,
		End:         2000,
		Sample:      500,
	}
	if err := validateMetricsInput(padded); err != nil {
		t.Fatalf("padded valid metric name should pass: %v", err)
	}

	// A whitespace-only name is empty after trim and must be rejected as
	// unknown (it is not in the catalog).
	blank := metricsInput{
		MetricNames: []string{"   "},
		Start:       1000,
		End:         2000,
		Sample:      500,
	}
	err = validateMetricsInput(blank)
	if err == nil {
		t.Fatal("whitespace-only metric name should fail validation")
	}
	if !strings.HasPrefix(err.Error(), "unknown metric: ") {
		t.Fatalf("whitespace-only name error: got %q, want prefix %q",
			err.Error(), "unknown metric: ")
	}
}

// TestInspectionMetrics locks the inspectionMetrics map against Design Doc
// Decision 7. Any add/remove/typo must update both the table and this test
// in lockstep. Decision 7 enumerates 32 metrics (doc text reconciled
// from "30" to "32" in commit `3ce3b6e`).
func TestInspectionMetrics(t *testing.T) {
	const expectedCount = 32 // rows in Design Doc Decision 7 table
	if got := len(inspectionMetrics); got != expectedCount {
		t.Fatalf("inspectionMetrics size: got %d, want %d", got, expectedCount)
	}

	want := map[string]struct {
		downsampler      string
		sourceAggregator string
		derivative       string
	}{
		"cr.node.liveness.livenodes":                   {"avg", "avg", "none"},
		"cr.node.sys.uptime":                           {"avg", "avg", "none"},
		"cr.node.sys.cpu.user.percent":                 {"avg", "sum", "none"},
		"cr.node.sys.cpu.sys.percent":                  {"avg", "sum", "none"},
		"cr.node.sys.cpu.combined.percent-normalized":  {"avg", "sum", "none"},
		"cr.store.capacity":                            {"avg", "sum", "none"},
		"cr.store.capacity.available":                  {"avg", "sum", "none"},
		"cr.store.capacity.used":                       {"avg", "sum", "none"},
		"cr.node.sys.rss":                              {"avg", "sum", "none"},
		"cr.node.sys.go.allocbytes":                    {"avg", "sum", "none"},
		"cr.node.sys.go.totalbytes":                    {"avg", "sum", "none"},
		"cr.node.sql.insert.count":                     {"avg", "sum", "none"},
		"cr.node.sql.update.count":                     {"avg", "sum", "none"},
		"cr.node.sql.delete.count":                     {"avg", "sum", "none"},
		"cr.node.sql.select.count":                     {"avg", "sum", "none"},
		"cr.node.sql.query.count":                      {"avg", "sum", "none"},
		"cr.store.rebalancing.writespersecond":         {"avg", "sum", "none"},
		"cr.store.rebalancing.queriespersecond":        {"avg", "sum", "none"},
		"cr.node.exec.latency-p99":                     {"avg", "avg", "none"},
		"cr.node.sql.service.latency-p99":              {"avg", "avg", "none"},
		"cr.node.sql.distsql.exec.latency-p99":         {"avg", "avg", "none"},
		"cr.store.totalbytes":                          {"avg", "sum", "none"},
		"cr.store.livebytes":                           {"avg", "sum", "none"},
		"cr.store.replicas":                            {"avg", "sum", "none"},
		"cr.store.replicas.leaders":                    {"avg", "sum", "none"},
		"cr.store.replicas.leaseholders":               {"avg", "sum", "none"},
		"cr.store.ranges.unavailable":                  {"avg", "max", "none"},
		"cr.store.ranges.underreplicated":              {"avg", "max", "none"},
		"cr.store.ranges.overreplicated":               {"avg", "max", "none"},
		"cr.store.raftlog.behind":                      {"avg", "max", "none"},
		"cr.store.raft.replica.consistent.latency-p99": {"avg", "avg", "none"},
		"cr.node.clock-offset.meannanos":               {"avg", "max", "none"},
	}

	// Verify each entry is present with exact (downsampler, sourceAggregator, derivative).
	for name, spec := range want {
		got, ok := inspectionMetrics[name]
		if !ok {
			t.Errorf("inspectionMetrics missing %q", name)
			continue
		}
		if got.Downsampler != spec.downsampler {
			t.Errorf("inspectionMetrics[%q].Downsampler: got %q, want %q",
				name, got.Downsampler, spec.downsampler)
		}
		if got.SourceAggregator != spec.sourceAggregator {
			t.Errorf("inspectionMetrics[%q].SourceAggregator: got %q, want %q",
				name, got.SourceAggregator, spec.sourceAggregator)
		}
		if got.Derivative != spec.derivative {
			t.Errorf("inspectionMetrics[%q].Derivative: got %q, want %q",
				name, got.Derivative, spec.derivative)
		}
	}

	// Verify the map is read-only: no extra entries beyond the table.
	// We re-walk `want` because map iteration order is not stable.
	seen := make(map[string]bool, len(inspectionMetrics))
	for name := range want {
		seen[name] = true
	}
	for name := range inspectionMetrics {
		if !seen[name] {
			t.Errorf("inspectionMetrics has unexpected entry %q not in Design Doc Decision 7", name)
		}
	}

	// Spot-check: the spec example from the task brief for cr.node.sql.query.count.
	const focus = "cr.node.sql.query.count"
	got, ok := inspectionMetrics[focus]
	if !ok {
		t.Fatalf("inspectionMetrics missing focus metric %q", focus)
	}
	if got.Downsampler != "avg" || got.SourceAggregator != "sum" || got.Derivative != "none" {
		t.Fatalf("inspectionMetrics[%q]=%+v, want {avg,sum,none}", focus, got)
	}
}

// TestInspectionMetricsAllDerivativeNone asserts the invariant that every
// derivative in the fixed map is "none" (Decision 7 last paragraph).
func TestInspectionMetricsAllDerivativeNone(t *testing.T) {
	if len(inspectionMetrics) == 0 {
		t.Fatal("inspectionMetrics is empty")
	}
	for name, spec := range inspectionMetrics {
		if spec.Derivative != "none" {
			t.Errorf("inspectionMetrics[%q].Derivative = %q, want %q",
				name, spec.Derivative, "none")
		}
	}
}

// TestExecuteMetricsQuery exercises the end-to-end behaviour of
// `executeMetricsQuery` against an `httptest` fake admin endpoint. The tests
// cover every contract cell in Design Doc Decision 6 (admin error matrix)
// that affects the inspection-metrics tool:
//
// path: /restapi/ts/query, application/json, the metric payload
//
//	  carries downsampler/source_aggregator/derivative as the integers from
//	  `metrics_history.go` (avg=1, sum=2, none=0), the response Raw payload
//	  contains the `results` field, and call counts == 1.
//	- TLS + password + Basic Auth (hardcoded skip; we match the Python
//	  tooling's `ssl._create_unverified_context()` by hitting an
//	  `httptest.NewTLSServer`).
//	- TLS + no password → fail-fast, exact error text, no traffic on the wire.
//	- Insecure + no password → no Authorization header is sent (handler
//	  asserts the canonical `Authorization` key is absent).
//	- HTTP 200 + body code=-1 → "auth failed: <desc>" error.
func TestExecuteMetricsQuery(t *testing.T) {
	validInput := func() metricsInput {
		return metricsInput{
			MetricNames: []string{"cr.node.sql.query.count"},
			Start:       1000,
			End:         2000,
			Sample:      500,
		}
	}
	const dbURLHTTP = "postgresql://root@db.example.com:26257/kwdb?sslmode=disable"
	const dbURLTLSWithPassword = "postgresql://root:secret@db.example.com:26257/kwdb?sslmode=require"
	const dbURLTLSWithoutPassword = "postgresql://root@db.example.com:26257/kwdb?sslmode=require"

	t.Run("success POSTs /restapi/ts/query with application/json and integer-encoded metric payload", func(t *testing.T) {
		var (
			calls          atomic.Int32
			gotMethod      string
			gotPath        string
			gotContentType string
			gotRawBody     []byte
			gotAuthHeader  string
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotContentType = r.Header.Get("Content-Type")
			gotAuthHeader = r.Header.Get("Authorization")
			gotRawBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok","results":[{"name":"cr.node.sql.query.count","datapoints":[]}]}`))
		}))
		defer srv.Close()

		result, err := executeMetricsQuery(context.Background(), validInput(), srv.URL, "", "")
		if err != nil {
			t.Fatalf("executeMetricsQuery unexpected error: %v", err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
		}
		if gotPath != "/restapi/ts/query" {
			t.Fatalf("path = %q, want %q", gotPath, "/restapi/ts/query")
		}
		if gotContentType != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", gotContentType, "application/json")
		}
		if gotAuthHeader != "" {
			t.Fatalf("Authorization header = %q, want empty for insecure endpoint without credentials", gotAuthHeader)
		}

		// Payload shape: parses to tsQueryRequest with the integer encodings
		// shared with `metrics_history.go` (avg=1, sum=2, none=0).
		var payload struct {
			StartNanos  int64            `json:"start_nanos"`
			EndNanos    int64            `json:"end_nanos"`
			SampleNanos int64            `json:"sample_nanos"`
			Queries     []map[string]any `json:"queries"`
		}
		if err := json.Unmarshal(gotRawBody, &payload); err != nil {
			t.Fatalf("decode request body: %v (body=%q)", err, string(gotRawBody))
		}
		if payload.StartNanos != 1_000_000_000 {
			t.Fatalf("start_nanos = %d, want %d", payload.StartNanos, 1_000_000_000)
		}
		if payload.EndNanos != 2_000_000_000 {
			t.Fatalf("end_nanos = %d, want %d", payload.EndNanos, 2_000_000_000)
		}
		if payload.SampleNanos != 500_000_000 {
			t.Fatalf("sample_nanos = %d, want %d", payload.SampleNanos, 500_000_000)
		}
		if len(payload.Queries) != 1 {
			t.Fatalf("queries count = %d, want 1", len(payload.Queries))
		}
		q := payload.Queries[0]
		if q["name"] != "cr.node.sql.query.count" {
			t.Fatalf("queries[0].name = %v, want %q", q["name"], "cr.node.sql.query.count")
		}
		if got, _ := q["downsampler"].(float64); int(got) != 1 {
			t.Fatalf("queries[0].downsampler = %v, want 1 (avg)", q["downsampler"])
		}
		if got, _ := q["source_aggregator"].(float64); int(got) != 2 {
			t.Fatalf("queries[0].source_aggregator = %v, want 2 (sum)", q["source_aggregator"])
		}
		if got, _ := q["derivative"].(float64); int(got) != 0 {
			t.Fatalf("queries[0].derivative = %v, want 0 (none)", q["derivative"])
		}

		// Response shape: AdminResponse with Raw that contains the results
		// field sent by the server.
		resp, ok := result.(*AdminResponse)
		if !ok {
			t.Fatalf("result type = %T, want *AdminResponse", result)
		}
		if resp.Code != 0 {
			t.Fatalf("AdminResponse.Code = %d, want 0", resp.Code)
		}
		if !strings.Contains(string(resp.Raw), `"results"`) {
			t.Fatalf("AdminResponse.Raw missing results field: %q", string(resp.Raw))
		}
	})

	t.Run("TLS endpoint with DB-URL password carries Basic Auth", func(t *testing.T) {
		var (
			calls       atomic.Int32
			gotAuth     string
			gotContentT string
			gotBodyRaw  []byte
		)
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			gotContentT = r.Header.Get("Content-Type")
			gotBodyRaw, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok","results":[]}`))
		}))
		defer srv.Close()

		// We must point the request at the TLS server's URL but keep the
		// admin URL resolution pointed at a DB URL that says sslmode=require
		// (so isTLS is true) — workaround for the test by setting the DB URL
		// to one whose host:port matches the test server, with sslmode=require.
		// The TLS server's URL starts with https:// — we drive the resolver
		// through `flagAdmin` so the host is the test server's host.
		result, err := executeMetricsQuery(context.Background(), validInput(),
			"", srv.URL, dbURLTLSWithPassword)
		if err != nil {
			t.Fatalf("executeMetricsQuery unexpected error: %v", err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		wantAuth := buildAuthHeader("secret", "root")
		if gotAuth != wantAuth {
			t.Fatalf("Authorization = %q, want %q", gotAuth, wantAuth)
		}
		if gotContentT != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", gotContentT)
		}
		// Sanity: payload was still POST-ed with the integer encodings.
		if !strings.Contains(string(gotBodyRaw), `"queries"`) {
			t.Fatalf("body missing queries field: %q", string(gotBodyRaw))
		}
		if resp, ok := result.(*AdminResponse); !ok || resp.Code != 0 {
			t.Fatalf("AdminResponse = %+v (ok=%v), want Code=0", result, ok)
		}
	})

	t.Run("TLS endpoint without password fails fast before any HTTP call", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok"}`))
		}))
		defer srv.Close()

		result, err := executeMetricsQuery(context.Background(), validInput(),
			"", srv.URL, dbURLTLSWithoutPassword)
		if err == nil {
			t.Fatalf("executeMetricsQuery error = nil, want credentials-required error")
		}
		const want = "TLS admin endpoint requires credentials, but DB URL has no password"
		if err.Error() != want {
			t.Fatalf("error = %q, want exactly %q", err.Error(), want)
		}
		if n := calls.Load(); n != 0 {
			t.Fatalf("handler call count = %d, want 0 (fail-fast before sending)", n)
		}
		if result != nil {
			t.Fatalf("result = %v, want nil when failing fast", result)
		}
	})

	t.Run("insecure endpoint with no password sends no Authorization header", func(t *testing.T) {
		var (
			calls       atomic.Int32
			gotAuth     string
			authPresent bool
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			_, authPresent = r.Header["Authorization"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok","results":[]}`))
		}))
		defer srv.Close()

		result, err := executeMetricsQuery(context.Background(), validInput(), srv.URL, "", "")
		if err != nil {
			t.Fatalf("executeMetricsQuery unexpected error: %v", err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		if authPresent || gotAuth != "" {
			t.Fatalf("Authorization header = %q (present=%v), want absent for insecure endpoint", gotAuth, authPresent)
		}
		if resp, ok := result.(*AdminResponse); !ok || resp.Code != 0 {
			t.Fatalf("AdminResponse = %+v (ok=%v), want Code=0", result, ok)
		}
	})

	t.Run("DB URL alone derives admin URL and TLS classification", func(t *testing.T) {
		// Sanity test: the three-level fallback chain (header → flag → DB URL)
		// is exercised through `executeMetricsQuery`. With only a DB URL that
		// says sslmode=require and no header/flag, the URL must be derived
		// as https://db.example.com:8080; the unreachable host means we hit
		// the "admin request failed" error path (not the fail-fast path),
		// confirming the DB URL branch was taken.
		_, err := executeMetricsQuery(context.Background(), validInput(),
			"", "", dbURLTLSWithoutPassword)
		if err == nil {
			t.Fatal("expected error against unreachable derived URL, got nil")
		}
		if strings.Contains(err.Error(), "TLS admin endpoint requires credentials") {
			// Acceptable — the function may also be classified as TLS without
			// password before even attempting. Both branches confirm dbURL
			// was the source of admin URL/TLS classification.
			return
		}
		if !strings.HasPrefix(err.Error(), "admin request failed: ") {
			t.Fatalf("error = %q, want prefix \"admin request failed: \"", err.Error())
		}
	})

	t.Run("HTTP 200 with body code=-1 returns auth failed error", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":-1,"desc":"token expired"}`))
		}))
		defer srv.Close()

		result, err := executeMetricsQuery(context.Background(), validInput(), srv.URL, "", "")
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		if err == nil {
			t.Fatal("executeMetricsQuery error = nil, want auth failed")
		}
		const want = "auth failed: token expired"
		if err.Error() != want {
			t.Fatalf("error = %q, want exactly %q", err.Error(), want)
		}
		if result != nil {
			t.Fatalf("result = %v, want nil on auth failure", result)
		}
	})

	// Bug regression (bugfix-2026-08-07): when the caller passes an https
	// admin URL via header but the DB URL says sslmode=disable (the KGA
	// production shape for clusters in secure mode), Basic Auth must still
	// be attached. Before the fix the auth decision was made solely from
	// dbURL.IsTLS() and the request landed at the secure cluster without
	// credentials, returning 401 "a valid authentication cookie is
	// required". The handler below proves the header was carried across
	// by failing with 401 only when Authorization is absent.
	t.Run("https admin URL + insecure DB URL (sslmode=disable) still attaches Authorization (bugfix-2026-08-07)", func(t *testing.T) {
		var (
			calls       atomic.Int32
			gotAuth     string
			authPresent bool
		)
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			_, authPresent = r.Header["Authorization"]
			if !authPresent {
				// Emulate the real KaiwuDB secure admin endpoint
				// behavior: 307 redirect for HTTP→HTTPS is followed by
				// the http.Client transparently, but a missing
				// Authorization on the HTTPS hop yields 401 with the
				// canonical "a valid authentication cookie is
				// required" body.
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("a valid authentication cookie is required"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok","results":[]}`))
		}))
		defer srv.Close()

		// DB URL with sslmode=disable and no certs — the exact KGA
		// production case. The password is in the URL so
		// buildAuthHeader has something to encode; the bug is purely
		// about whether the auth header gets sent.
		const insecureDBURL = "postgresql://root:secret@db.example.com:26257/kwdb?sslmode=disable"
		result, err := executeMetricsQuery(context.Background(), validInput(),
			srv.URL /* X-Admin-Base-URL: https://... */, "", insecureDBURL)
		if err != nil {
			t.Fatalf("executeMetricsQuery unexpected error: %v", err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("handler call count = %d, want 1", n)
		}
		if !authPresent {
			t.Fatalf("Authorization header missing on https admin URL — 401 regression")
		}
		wantAuth := buildAuthHeader("secret", "root")
		if gotAuth != wantAuth {
			t.Fatalf("Authorization = %q, want %q", gotAuth, wantAuth)
		}
		if resp, ok := result.(*AdminResponse); !ok || resp.Code != 0 {
			t.Fatalf("AdminResponse = %+v (ok=%v), want Code=0", result, ok)
		}
	})

	// Reverse regression check: an http admin URL with the same insecure
	// DB URL must NOT attach Authorization, even after the fix. This pins
	// the contract that an insecure admin endpoint never carries auth.
	t.Run("http admin URL + insecure DB URL (sslmode=disable) sends no Authorization (bugfix-2026-08-07 reverse)", func(t *testing.T) {
		var (
			calls       atomic.Int32
			gotAuth     string
			authPresent bool
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			_, authPresent = r.Header["Authorization"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok","results":[]}`))
		}))
		defer srv.Close()

		const insecureDBURL = "postgresql://root:secret@db.example.com:26257/kwdb?sslmode=disable"
		_, err := executeMetricsQuery(context.Background(), validInput(),
			srv.URL, "", insecureDBURL)
		if err != nil {
			t.Fatalf("executeMetricsQuery unexpected error: %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("handler call count = %d, want 1", calls.Load())
		}
		if authPresent || gotAuth != "" {
			t.Fatalf("Authorization = %q (present=%v), want absent for http admin URL",
				gotAuth, authPresent)
		}
	})
}

// TestRegisterQueryMetricsTool exercises the MCP registration side of the
// query-metrics tool: the tool must be reachable under its canonical name,
// must advertise the ms-based input schema the LLM is expected to fill in,
// and its handler must bind those exact argument names while honouring the
// X-Admin-Base-URL / X-Database-URI request headers. The handler is driven
// end-to-end against a fake admin endpoint so a malformed schema or a broken
// argument binding shows up immediately.
func TestRegisterQueryMetricsTool(t *testing.T) {
	s := mcpserver.NewMCPServer("test", "1.0", mcpserver.WithToolCapabilities(true))
	registerQueryMetricsTool(s)
	tools := s.ListTools()
	got, ok := tools["query-metrics"]
	if !ok {
		t.Fatalf("query-metrics tool was not registered: %v", tools)
	}
	if got.Tool.Name != "query-metrics" {
		t.Fatalf("tool.Name = %q, want %q", got.Tool.Name, "query-metrics")
	}
	// Round-trip the JSON Schema through MarshalJSON so any future
	// RawOutputSchema wiring must agree with the helper.
	if _, err := json.Marshal(got.Tool); err != nil {
		t.Fatalf("tool should marshal cleanly, got error: %v", err)
	}

	t.Run("input schema declares described metric_names array and ms time fields", func(t *testing.T) {
		var schema struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
				MinItems    int    `json:"minItems"`
				Items       struct {
					Type string `json:"type"`
				} `json:"items"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(got.Tool.RawInputSchema, &schema); err != nil {
			t.Fatalf("decode RawInputSchema: %v (raw=%q)", err, string(got.Tool.RawInputSchema))
		}
		if schema.Type != "object" {
			t.Fatalf("schema.type = %q, want %q", schema.Type, "object")
		}

		names, ok := schema.Properties["metric_names"]
		if !ok {
			t.Fatalf("schema is missing the metric_names property: %q", string(got.Tool.RawInputSchema))
		}
		if names.Type != "array" {
			t.Errorf("metric_names.type = %q, want %q", names.Type, "array")
		}
		if names.Items.Type != "string" {
			t.Errorf("metric_names.items.type = %q, want %q", names.Items.Type, "string")
		}
		if names.MinItems != 1 {
			t.Errorf("metric_names.minItems = %d, want 1", names.MinItems)
		}
		if names.Description == "" {
			t.Errorf("metric_names must carry a description so the LLM knows the names come from the fixed catalog")
		}

		for _, field := range []string{"start_ms", "end_ms", "sample_ms"} {
			prop, ok := schema.Properties[field]
			if !ok {
				t.Errorf("schema is missing the %s property", field)
				continue
			}
			if prop.Type != "integer" {
				t.Errorf("%s.type = %q, want %q", field, prop.Type, "integer")
			}
			if prop.Description == "" {
				t.Errorf("%s must carry a description", field)
			}
		}

		// path: /restapi/ts/query endpoint has no
		// server-side default for the window or the sampling interval.
		wantRequired := map[string]bool{
			"metric_names": false, "start_ms": false, "end_ms": false, "sample_ms": false,
		}
		for _, r := range schema.Required {
			if _, known := wantRequired[r]; !known {
				t.Errorf("unexpected required field %q", r)
				continue
			}
			wantRequired[r] = true
		}
		for field, seen := range wantRequired {
			if !seen {
				t.Errorf("%s must be listed in required, got %v", field, schema.Required)
			}
		}

		// The old query-metrics-history argument shape must not leak through.
		for _, legacy := range []string{"metric", "downsampler", "source_aggregator", "derivative"} {
			if _, found := schema.Properties[legacy]; found {
				t.Errorf("schema must not expose legacy query-metrics-history argument %q", legacy)
			}
		}
	})

	t.Run("handler binds ms arguments and calls the X-Admin-Base-URL endpoint", func(t *testing.T) {
		var (
			calls      atomic.Int32
			gotPath    string
			gotRawBody []byte
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotPath = r.URL.Path
			gotRawBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"desc":"ok","results":[{"name":"cr.node.sql.query.count","datapoints":[]}]}`))
		}))
		defer srv.Close()

		req := mcp.CallToolRequest{Header: http.Header{}}
		req.Header.Set("X-Admin-Base-URL", srv.URL)
		req.Header.Set("X-Database-URI", "postgresql://root@db.example.com:26257/kwdb?sslmode=disable")
		req.Params.Arguments = map[string]any{
			"metric_names": []string{"cr.node.sql.query.count"},
			"start_ms":     1000,
			"end_ms":       2000,
			"sample_ms":    500,
		}

		res, err := got.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned a transport error: %v", err)
		}
		if res.IsError {
			t.Fatalf("handler returned an error result: %+v", res.Content)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("admin endpoint call count = %d, want 1 (X-Admin-Base-URL not honoured?)", n)
		}
		if gotPath != "/restapi/ts/query" {
			t.Fatalf("path = %q, want %q", gotPath, "/restapi/ts/query")
		}

		// The ms-suffixed arguments must reach the request body as nanos; a
		// mismatch here means BindArguments silently dropped the time fields.
		var payload struct {
			StartNanos  int64 `json:"start_nanos"`
			EndNanos    int64 `json:"end_nanos"`
			SampleNanos int64 `json:"sample_nanos"`
		}
		if err := json.Unmarshal(gotRawBody, &payload); err != nil {
			t.Fatalf("decode request body: %v (body=%q)", err, string(gotRawBody))
		}
		if payload.StartNanos != 1_000_000_000 {
			t.Errorf("start_nanos = %d, want %d (start_ms was not bound)", payload.StartNanos, 1_000_000_000)
		}
		if payload.EndNanos != 2_000_000_000 {
			t.Errorf("end_nanos = %d, want %d (end_ms was not bound)", payload.EndNanos, 2_000_000_000)
		}
		if payload.SampleNanos != 500_000_000 {
			t.Errorf("sample_nanos = %d, want %d (sample_ms was not bound)", payload.SampleNanos, 500_000_000)
		}
	})

	t.Run("handler reads X-Database-URI and wraps failures as MCP error results", func(t *testing.T) {
		req := mcp.CallToolRequest{Header: http.Header{}}
		req.Header.Set("X-Admin-Base-URL", "http://admin.example.com:8080")
		req.Header.Set("X-Database-URI", "mysql://root@db.example.com:26257/kwdb")
		req.Params.Arguments = map[string]any{
			"metric_names": []string{"cr.node.sql.query.count"},
			"start_ms":     1000,
			"end_ms":       2000,
			"sample_ms":    500,
		}

		res, err := got.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler should wrap the failure in the result, got transport error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("handler should have reported an error result for a non-postgresql X-Database-URI, got %+v", res.Content)
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
