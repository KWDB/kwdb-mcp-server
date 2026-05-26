package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestResolveDBTarget_WithHeader(t *testing.T) {
	// X-Database-URI 优先：有 header 时无论默认池是否初始化，都使用 header 指定库
	uri, useDefault, missing := resolveDBTarget("postgresql://host/db1", false)
	if uri != "postgresql://host/db1" || useDefault || missing {
		t.Errorf("with header and no default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}

	uri, useDefault, missing = resolveDBTarget("postgresql://host/db1", true)
	if uri != "postgresql://host/db1" || useDefault || missing {
		t.Errorf("with header and default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}
}

func TestResolveDBTarget_NoHeaderWithDefaultPool(t *testing.T) {
	// 兼容模式：无 header 但默认池已初始化 → 使用默认池
	uri, useDefault, missing := resolveDBTarget("", true)
	if uri != "" || !useDefault || missing {
		t.Errorf("no header with default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}
}

func TestResolveDBTarget_NoHeaderNoDefaultPool(t *testing.T) {
	// 无状态模式：无 header 且无默认池 → 应返回 missing header 错误
	uri, useDefault, missing := resolveDBTarget("", false)
	if uri != "" || useDefault || !missing {
		t.Errorf("no header no default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}
}

func TestIsSelectWithoutLimit(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{
			name: "plain select without limit is auto-limited",
			sql:  "SELECT * FROM test_table",
			want: true,
		},
		{
			name: "select with explicit limit is left alone",
			sql:  "SELECT * FROM test_table LIMIT 10",
			want: false,
		},
		{
			name: "select with explicit limit on next line is left alone",
			sql:  "SELECT * FROM test_table\nLIMIT 10",
			want: false,
		},
		{
			name: "select with explicit limit after tab is left alone",
			sql:  "SELECT * FROM test_table\tLIMIT 10",
			want: false,
		},
		{
			name: "select with explicit limit and newline before count is left alone",
			sql:  "SELECT * FROM test_table LIMIT\n10",
			want: false,
		},
		{
			name: "select with fetch first clause is left alone",
			sql:  "SELECT * FROM test_table ORDER BY id FETCH FIRST 10 ROW ONLY",
			want: false,
		},
		{
			name: "select with offset fetch clause is left alone",
			sql:  "SELECT * FROM test_table ORDER BY id OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY",
			want: false,
		},
		{
			name: "select with fill clause is left alone",
			sql: `SELECT k_timestamp, data_int8 FROM test_fill_db.metric_numeric
WHERE device_id = 'device1' AND k_timestamp = '2025-12-19 10:00:03'
FILL(EXACT);`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSelectWithoutLimit(tt.sql)
			if got != tt.want {
				t.Fatalf("isSelectWithoutLimit(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestAddLimitToQuery(t *testing.T) {
	sql := "SELECT * FROM test_table;"
	got := addLimitToQuery(sql, 20)

	if got != "SELECT * FROM test_table LIMIT 20;" {
		t.Fatalf("addLimitToQuery(%q, 20) = %q", sql, got)
	}
}

func TestReadQueryTool_NodeStatementStatisticsSerializes(t *testing.T) {
	dbURI := os.Getenv("KWDB_MCP_TEST_DATABASE_URI")
	if dbURI == "" {
		t.Skip("KWDB_MCP_TEST_DATABASE_URI is not set")
	}

	s := mcpserver.NewMCPServer("test", "1.0", mcpserver.WithToolCapabilities(true))
	RegisterTools(s)
	tool := s.ListTools()["read-query"]

	request := mcp.CallToolRequest{Header: http.Header{}}
	request.Header.Set("X-Database-URI", dbURI)
	request.Params.Name = "read-query"
	request.Params.Arguments = map[string]interface{}{
		"sql": "SELECT * FROM kwdb_internal.node_statement_statistics LIMIT 50",
	}

	result, err := tool.Handler(context.Background(), request)
	if err != nil {
		t.Fatalf("read-query should not return transport error: %v", err)
	}
	if result.IsError {
		t.Fatalf("read-query returned tool error: %+v", result.Content)
	}
	if _, err := json.Marshal(result.StructuredContent); err != nil {
		t.Fatalf("structured content should marshal as JSON: %v", err)
	}
}
