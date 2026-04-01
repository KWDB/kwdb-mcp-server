package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestResolveAdminBaseURL(t *testing.T) {
	url, missing := resolveAdminBaseURL("http://header.example:8080", "")
	if url != "http://header.example:8080" || missing {
		t.Fatalf("header should win, got url=%q missing=%v", url, missing)
	}

	url, missing = resolveAdminBaseURL("", "http://default.example:8080")
	if url != "http://default.example:8080" || missing {
		t.Fatalf("default should be used, got url=%q missing=%v", url, missing)
	}

	url, missing = resolveAdminBaseURL("", "")
	if url != "" || !missing {
		t.Fatalf("missing admin base url should be reported, got url=%q missing=%v", url, missing)
	}
}

func TestValidateMetricsHistoryInput(t *testing.T) {
	valid := metricsHistoryInput{
		StartMS:  1000,
		EndMS:    2000,
		SampleMS: 100,
		Queries: []metricsHistoryQuery{
			{Name: "cr.node.sql.query.count", Downsampler: "avg", SourceAggregator: "sum", Derivative: "rate"},
		},
	}
	if err := validateMetricsHistoryInput(valid); err != nil {
		t.Fatalf("valid input should pass validation: %v", err)
	}

	invalid := valid
	invalid.SampleMS = 0
	if err := validateMetricsHistoryInput(invalid); err == nil {
		t.Fatal("sample_ms=0 should fail validation")
	}

	invalid = valid
	invalid.StartMS = invalid.EndMS
	if err := validateMetricsHistoryInput(invalid); err == nil {
		t.Fatal("start_ms=end_ms should fail validation")
	}
}

func TestBuildTSQueryRequest(t *testing.T) {
	req, err := buildTSQueryRequest(metricsHistoryInput{
		StartMS:  1000,
		EndMS:    4000,
		SampleMS: 500,
		Queries: []metricsHistoryQuery{
			{Name: "cr.node.sql.query.count", Downsampler: "avg", SourceAggregator: "sum", Derivative: "rate"},
			{Name: "cr.node.sys.rss", Downsampler: "max", SourceAggregator: "avg", Derivative: "none"},
		},
	})
	if err != nil {
		t.Fatalf("buildTSQueryRequest returned error: %v", err)
	}

	if req.StartNanos != 1000_000_000 || req.EndNanos != 4000_000_000 || req.SampleNanos != 500_000_000 {
		t.Fatalf("unexpected nanos conversion: %+v", req)
	}
	if len(req.Queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(req.Queries))
	}
	if req.Queries[0].Downsampler != 1 || req.Queries[0].SourceAggregator != 2 || req.Queries[0].Derivative != 1 {
		t.Fatalf("unexpected enum mapping for first query: %+v", req.Queries[0])
	}
	if req.Queries[1].Downsampler != 3 || req.Queries[1].SourceAggregator != 1 || req.Queries[1].Derivative != 0 {
		t.Fatalf("unexpected enum mapping for second query: %+v", req.Queries[1])
	}
}

func TestParseMetricsHistoryResponse_CamelCase(t *testing.T) {
	body := []byte(`{
		"results": [
			{
				"query": {
					"name": "cr.node.sql.query.count",
					"downsampler": 1,
					"sourceAggregator": 2,
					"derivative": 1,
					"sources": ["1"]
				},
				"datapoints": [
					{"timestampNanos": "1775035680000000000", "value": 0.1}
				]
			}
		]
	}`)

	results, err := parseMetricsHistoryResponse(body)
	if err != nil {
		t.Fatalf("parseMetricsHistoryResponse returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "cr.node.sql.query.count" {
		t.Fatalf("unexpected result name: %+v", results[0])
	}
	if len(results[0].Datapoints) != 1 {
		t.Fatalf("expected 1 datapoint, got %d", len(results[0].Datapoints))
	}
	if results[0].Datapoints[0].TimestampMS != 1775035680000 {
		t.Fatalf("unexpected timestamp ms: %+v", results[0].Datapoints[0])
	}
}

func TestParseMetricsHistoryResponse_SnakeCase(t *testing.T) {
	body := []byte(`{
		"results": [
			{
				"query": {
					"name": "cr.node.sys.rss",
					"downsampler": 1,
					"source_aggregator": 2,
					"derivative": 0,
					"sources": ["1"]
				},
				"datapoints": [
					{"timestamp_nanos": 1775035680000000000, "value": 330080256}
				]
			}
		]
	}`)

	results, err := parseMetricsHistoryResponse(body)
	if err != nil {
		t.Fatalf("parseMetricsHistoryResponse returned error: %v", err)
	}
	if len(results) != 1 || len(results[0].Datapoints) != 1 {
		t.Fatalf("unexpected parsed result count: %+v", results)
	}
	if results[0].Datapoints[0].TimestampMS != 1775035680000 {
		t.Fatalf("unexpected timestamp ms: %+v", results[0].Datapoints[0])
	}
}

func TestExecuteMetricsHistoryQuery(t *testing.T) {
	var captured tsQueryRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/ts/query" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{
					"query": {
						"name": "cr.node.sql.query.count",
						"downsampler": 1,
						"sourceAggregator": 2,
						"derivative": 1,
						"sources": ["1"]
					},
					"datapoints": []
				}
			]
		}`))
	}))
	defer srv.Close()

	data, err := executeMetricsHistoryQuery(context.Background(), srv.Client(), srv.URL, metricsHistoryInput{
		StartMS:  1000,
		EndMS:    4000,
		SampleMS: 500,
		Queries: []metricsHistoryQuery{
			{Name: "cr.node.sql.query.count", Downsampler: "avg", SourceAggregator: "sum", Derivative: "rate"},
		},
	})
	if err != nil {
		t.Fatalf("executeMetricsHistoryQuery returned error: %v", err)
	}
	if captured.StartNanos != 1000_000_000 || captured.EndNanos != 4000_000_000 || captured.SampleNanos != 500_000_000 {
		t.Fatalf("unexpected captured request: %+v", captured)
	}
	if len(data.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(data.Results))
	}
	if !data.Results[0].EmptySeries {
		t.Fatalf("empty datapoints should be marked as empty series: %+v", data.Results[0])
	}
}

func TestRegisterToolsWithConfig_RegistersMetricsHistoryTool(t *testing.T) {
	s := mcpserver.NewMCPServer("test", "1.0", mcpserver.WithToolCapabilities(true))
	RegisterToolsWithConfig(s, Config{})

	tools := s.ListTools()
	if _, ok := tools["query-metrics-history"]; !ok {
		t.Fatalf("query-metrics-history tool was not registered: %v", tools)
	}
}

func TestQueryMetricsHistoryTool_MissingAdminBaseURL(t *testing.T) {
	s := mcpserver.NewMCPServer("test", "1.0", mcpserver.WithToolCapabilities(true))
	RegisterToolsWithConfig(s, Config{})
	tool := s.ListTools()["query-metrics-history"]

	result, err := tool.Handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "query-metrics-history",
			Arguments: map[string]any{
				"start_ms":  1000,
				"end_ms":    2000,
				"sample_ms": 100,
				"queries": []map[string]any{
					{
						"name":              "cr.node.sql.query.count",
						"downsampler":       "avg",
						"source_aggregator": "sum",
						"derivative":        "rate",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected tool-level error, got transport error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error result, got: %+v", result)
	}
}

func TestQueryMetricsHistoryTool_DefaultAdminBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{
					"query": {
						"name": "cr.node.sql.query.count",
						"downsampler": 1,
						"sourceAggregator": 2,
						"derivative": 1,
						"sources": ["1"]
					},
					"datapoints": [
						{"timestampNanos": "1775035680000000000", "value": 0.1}
					]
				}
			]
		}`))
	}))
	defer srv.Close()

	s := mcpserver.NewMCPServer("test", "1.0", mcpserver.WithToolCapabilities(true))
	RegisterToolsWithConfig(s, Config{DefaultAdminBaseURL: srv.URL})
	tool := s.ListTools()["query-metrics-history"]

	result, err := tool.Handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "query-metrics-history",
			Arguments: map[string]any{
				"start_ms":  1000,
				"end_ms":    2000,
				"sample_ms": 100,
				"queries": []map[string]any{
					{
						"name":              "cr.node.sql.query.count",
						"downsampler":       "avg",
						"source_aggregator": "sum",
						"derivative":        "rate",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected tool success result, got transport error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got tool error: %+v", result)
	}
}
