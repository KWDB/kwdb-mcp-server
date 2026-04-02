package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	maxMetricsHistoryQueries = 10
	maxMetricsHistoryWindow  = int64(24 * 60 * 60 * 1000)
)

var metricsHistoryInputSchema = []byte(`{
  "type": "object",
  "properties": {
    "start_ms": {"type": "integer", "description": "Start time in Unix milliseconds"},
    "end_ms": {"type": "integer", "description": "End time in Unix milliseconds"},
    "sample_ms": {"type": "integer", "description": "Sampling interval in milliseconds"},
    "queries": {
      "type": "array",
      "minItems": 1,
      "maxItems": 10,
      "items": {
        "type": "object",
        "properties": {
          "name": {"type": "string"},
          "downsampler": {"type": "string", "enum": ["avg", "sum", "max", "min"]},
          "source_aggregator": {"type": "string", "enum": ["avg", "sum", "max", "min"]},
          "derivative": {"type": "string", "enum": ["none", "rate", "non_negative_rate", "non_negative_derivative"]}
        },
        "required": ["name", "downsampler", "source_aggregator", "derivative"]
      }
    }
  },
  "required": ["start_ms", "end_ms", "sample_ms", "queries"]
}`)

type metricsHistoryInput struct {
	StartMS  int64                 `json:"start_ms"`
	EndMS    int64                 `json:"end_ms"`
	SampleMS int64                 `json:"sample_ms"`
	Queries  []metricsHistoryQuery `json:"queries"`
}

type metricsHistoryQuery struct {
	Name             string `json:"name"`
	Downsampler      string `json:"downsampler"`
	SourceAggregator string `json:"source_aggregator"`
	Derivative       string `json:"derivative"`
}

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

type metricsHistoryData struct {
	AdminBaseURL string                 `json:"admin_base_url"`
	StartMS      int64                  `json:"start_ms"`
	EndMS        int64                  `json:"end_ms"`
	SampleMS     int64                  `json:"sample_ms"`
	Results      []metricsHistoryResult `json:"results"`
}

type metricsHistoryResult struct {
	Name             string                    `json:"name"`
	Downsampler      string                    `json:"downsampler"`
	SourceAggregator string                    `json:"source_aggregator"`
	Derivative       string                    `json:"derivative"`
	Sources          []string                  `json:"sources,omitempty"`
	Datapoints       []metricsHistoryDatapoint `json:"datapoints"`
	EmptySeries      bool                      `json:"empty_series"`
}

type metricsHistoryDatapoint struct {
	TimestampMS    int64   `json:"timestamp_ms"`
	TimestampNanos string  `json:"timestamp_nanos"`
	Value          float64 `json:"value"`
}

type tsQueryResponse struct {
	Results []tsQueryResult `json:"results"`
}

type tsQueryResult struct {
	Query      tsQueryResponseQuery       `json:"query"`
	Datapoints []tsQueryResponseDataPoint `json:"datapoints"`
}

type tsQueryResponseQuery struct {
	Name                  string   `json:"name"`
	Downsampler           int      `json:"downsampler"`
	SourceAggregatorCamel int      `json:"sourceAggregator"`
	SourceAggregatorSnake int      `json:"source_aggregator"`
	Derivative            int      `json:"derivative"`
	Sources               []string `json:"sources"`
}

type tsQueryResponseDataPoint struct {
	TimestampNanosCamel json.RawMessage `json:"timestampNanos"`
	TimestampNanosSnake json.RawMessage `json:"timestamp_nanos"`
	Value               float64         `json:"value"`
}

func registerQueryMetricsHistoryTool(s *server.MCPServer, config Config) {
	metricsHistoryTool := mcp.NewToolWithRawSchema(
		"query-metrics-history",
		"Query KWDB historical runtime metrics through the admin /ts/query API using millisecond timestamps and normalized aggregations.",
		json.RawMessage(metricsHistoryInputSchema),
	)
	metricsHistoryTool.RawOutputSchema = json.RawMessage(validOutputSchema)

	s.AddTool(metricsHistoryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input metricsHistoryInput
		if err := request.BindArguments(&input); err != nil {
			return mcp.NewToolResultErrorFromErr("Invalid metrics history arguments", err), nil
		}

		adminBaseURL, missing := resolveAdminBaseURL(request.Header.Get("X-Admin-Base-URL"), config.DefaultAdminBaseURL)
		if missing {
			return mcp.NewToolResultError("missing X-Admin-Base-URL header"), nil
		}

		data, err := executeMetricsHistoryQuery(ctx, http.DefaultClient, adminBaseURL, input)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("Metrics query failed", err), nil
		}

		response := map[string]any{
			"status": "success",
			"type":   "metrics_timeseries",
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

func resolveAdminBaseURL(headerValue string, defaultValue string) (string, bool) {
	if trimmed := strings.TrimSpace(headerValue); trimmed != "" {
		return trimmed, false
	}
	if trimmed := strings.TrimSpace(defaultValue); trimmed != "" {
		return trimmed, false
	}
	return "", true
}

func validateMetricsHistoryInput(input metricsHistoryInput) error {
	if input.StartMS >= input.EndMS {
		return fmt.Errorf("start_ms must be less than end_ms")
	}
	if input.SampleMS <= 0 {
		return fmt.Errorf("sample_ms must be greater than 0")
	}
	if len(input.Queries) == 0 {
		return fmt.Errorf("queries must not be empty")
	}
	if len(input.Queries) > maxMetricsHistoryQueries {
		return fmt.Errorf("queries must not exceed %d items", maxMetricsHistoryQueries)
	}
	if input.EndMS-input.StartMS > maxMetricsHistoryWindow {
		return fmt.Errorf("time window must not exceed %d ms", maxMetricsHistoryWindow)
	}
	for _, query := range input.Queries {
		if strings.TrimSpace(query.Name) == "" {
			return fmt.Errorf("query name must not be empty")
		}
		if _, err := mapDownsampler(query.Downsampler); err != nil {
			return err
		}
		if _, err := mapAggregator(query.SourceAggregator); err != nil {
			return err
		}
		if _, err := mapDerivative(query.Derivative); err != nil {
			return err
		}
	}
	return nil
}

func buildTSQueryRequest(input metricsHistoryInput) (tsQueryRequest, error) {
	if err := validateMetricsHistoryInput(input); err != nil {
		return tsQueryRequest{}, err
	}

	req := tsQueryRequest{
		StartNanos:  input.StartMS * 1_000_000,
		EndNanos:    input.EndMS * 1_000_000,
		SampleNanos: input.SampleMS * 1_000_000,
		Queries:     make([]tsQueryRequestQuery, 0, len(input.Queries)),
	}

	for _, query := range input.Queries {
		downsampler, err := mapDownsampler(query.Downsampler)
		if err != nil {
			return tsQueryRequest{}, err
		}
		sourceAggregator, err := mapAggregator(query.SourceAggregator)
		if err != nil {
			return tsQueryRequest{}, err
		}
		derivative, err := mapDerivative(query.Derivative)
		if err != nil {
			return tsQueryRequest{}, err
		}
		req.Queries = append(req.Queries, tsQueryRequestQuery{
			Name:             strings.TrimSpace(query.Name),
			Downsampler:      downsampler,
			SourceAggregator: sourceAggregator,
			Derivative:       derivative,
		})
	}

	return req, nil
}

func executeMetricsHistoryQuery(ctx context.Context, client *http.Client, adminBaseURL string, input metricsHistoryInput) (*metricsHistoryData, error) {
	if client == nil {
		client = http.DefaultClient
	}

	reqBody, err := buildTSQueryRequest(input)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal metrics request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, buildTSQueryURL(adminBaseURL), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build ts/query request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call ts/query: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ts/query response: %w", err)
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ts/query returned status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	results, err := parseMetricsHistoryResponse(body)
	if err != nil {
		return nil, err
	}

	return &metricsHistoryData{
		AdminBaseURL: strings.TrimSpace(adminBaseURL),
		StartMS:      input.StartMS,
		EndMS:        input.EndMS,
		SampleMS:     input.SampleMS,
		Results:      results,
	}, nil
}

func parseMetricsHistoryResponse(body []byte) ([]metricsHistoryResult, error) {
	var raw tsQueryResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode ts/query response: %w", err)
	}

	results := make([]metricsHistoryResult, 0, len(raw.Results))
	for _, result := range raw.Results {
		normalized := metricsHistoryResult{
			Name:             result.Query.Name,
			Downsampler:      downsamplerName(result.Query.Downsampler),
			SourceAggregator: aggregatorName(result.Query.sourceAggregator()),
			Derivative:       derivativeName(result.Query.Derivative),
			Sources:          result.Query.Sources,
			Datapoints:       make([]metricsHistoryDatapoint, 0, len(result.Datapoints)),
		}

		for _, datapoint := range result.Datapoints {
			timestampNanos, err := datapoint.timestampNanos()
			if err != nil {
				return nil, err
			}
			normalized.Datapoints = append(normalized.Datapoints, metricsHistoryDatapoint{
				TimestampMS:    timestampNanos / 1_000_000,
				TimestampNanos: strconv.FormatInt(timestampNanos, 10),
				Value:          datapoint.Value,
			})
		}
		normalized.EmptySeries = len(normalized.Datapoints) == 0
		results = append(results, normalized)
	}

	return results, nil
}

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

func mapAggregator(value string) (int, error) {
	return mapDownsampler(value)
}

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

func downsamplerName(value int) string {
	switch value {
	case 1:
		return "avg"
	case 2:
		return "sum"
	case 3:
		return "max"
	case 4:
		return "min"
	default:
		return ""
	}
}

func aggregatorName(value int) string {
	return downsamplerName(value)
}

func derivativeName(value int) string {
	switch value {
	case 0:
		return "none"
	case 1:
		return "rate"
	case 2:
		return "non_negative_rate"
	default:
		return ""
	}
}

func normalizeEnum(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
}

func buildTSQueryURL(adminBaseURL string) string {
	return strings.TrimRight(strings.TrimSpace(adminBaseURL), "/") + "/ts/query"
}

func (q tsQueryResponseQuery) sourceAggregator() int {
	if q.SourceAggregatorCamel != 0 {
		return q.SourceAggregatorCamel
	}
	return q.SourceAggregatorSnake
}

func (d tsQueryResponseDataPoint) timestampNanos() (int64, error) {
	raw := d.TimestampNanosCamel
	if len(raw) == 0 {
		raw = d.TimestampNanosSnake
	}
	if len(raw) == 0 {
		return 0, fmt.Errorf("timestamp nanos missing in datapoint")
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		value, convErr := strconv.ParseInt(asString, 10, 64)
		if convErr != nil {
			return 0, fmt.Errorf("parse timestamp nanos string: %w", convErr)
		}
		return value, nil
	}

	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return asInt, nil
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return int64(asFloat), nil
	}

	return 0, fmt.Errorf("unsupported timestamp nanos format: %s", string(raw))
}
