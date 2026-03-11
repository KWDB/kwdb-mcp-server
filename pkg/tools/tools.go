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

// resolveDBTarget 决定本次请求使用哪个数据库：X-Database-URI 优先，无 header 时回退默认池，两者都无则报错。
// 返回: useURI（非空时用 MultiPoolManager 指定库）、useDefault（为 true 时用默认池）、missingHeader（为 true 时应返回 missing header 错误）。
func resolveDBTarget(headerURI string, defaultPoolInitialized bool) (useURI string, useDefault bool, missingHeader bool) {
	if headerURI != "" {
		return headerURI, false, false
	}
	if defaultPoolInitialized {
		return "", true, false
	}
	return "", false, true
}

// RegisterTools registers all tools with the MCP server
func RegisterTools(s *server.MCPServer) {
	// Register read query tool
	registerReadQueryTool(s)

	// Register write query tool
	registerWriteQueryTool(s)
}

// validOutputSchema is a minimal JSON Schema so clients (e.g. Cursor) that validate
// tool schema do not reject tools due to empty or invalid outputSchema.
var validOutputSchema = []byte(`{"type":"object"}`)

// registerReadQueryTool registers read query tool with concurrency and timeout support
func registerReadQueryTool(s *server.MCPServer) {
	// Create read query tool
	readQueryTool := mcp.NewTool("read-query",
		mcp.WithDescription("Execute SELECT, SHOW, EXPLAIN and other read-only queries on KWDB (KaiwuDB). SELECT queries without a LIMIT clause will automatically have LIMIT 20 added to prevent large result sets."),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("SQL query to execute. Only read operations like SELECT, SHOW, EXPLAIN are allowed."),
		),
		mcp.WithRawOutputSchema(json.RawMessage(validOutputSchema)),
	)

	// Add read query handler
	s.AddTool(readQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql := request.GetString("sql", "")
		originalSQL := sql

		// 从请求头中获取数据库 URI，用于多租户多数据库访问。
		// 兼容模式下，如果未提供 header 且默认连接池已初始化，则回退到单库连接池。
		headerURI := request.Header.Get("X-Database-URI")
		useURI, _, missingHeader := resolveDBTarget(headerURI, db.IsDefaultPoolInitialized())
		if missingHeader {
			return mcp.NewToolResultError("missing X-Database-URI header"), nil
		}

		// Check if the query is a SELECT statement without LIMIT
		if isSelectWithoutLimit(sql) {
			// Add LIMIT 20 to the query
			sql = addLimitToQuery(sql, 20)
		}

		var (
			result []map[string]interface{}
			err    error
		)
		if useURI != "" {
			result, err = db.ExecuteQueryWithURI(ctx, useURI, sql)
		} else {
			result, err = db.ExecuteQueryWithContext(ctx, sql)
		}
		if err != nil {
			return mcp.NewToolResultErrorFromErr("Query execution failed", err), nil
		}

		// Extract column names (if result is not empty)
		var columns []string
		if len(result) > 0 {
			columns = make([]string, 0, len(result[0]))
			for col := range result[0] {
				columns = append(columns, col)
			}
		}

		// Standardized success response
		response := map[string]interface{}{
			"status": "success",
			"type":   "query_result",
			"data": map[string]interface{}{
				"result_type": "table",
				"columns":     columns,
				"rows":        result,
				"metadata": map[string]interface{}{
					"affected_rows":  0,
					"row_count":      len(result),
					"query":          sql,
					"original_query": originalSQL,
					"auto_limited":   sql != originalSQL,
				},
			},
			"error": nil,
		}

		// Convert result to JSON
		jsonResult, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to serialize result: %v", err)
		}

		return mcp.NewToolResultText(string(jsonResult)), nil
	})
}

// registerWriteQueryTool 注册写查询工具，支持并发和超时
func registerWriteQueryTool(s *server.MCPServer) {
	// Create write query tool
	writeQueryTool := mcp.NewTool("write-query",
		mcp.WithDescription("Execute data modification queries including DML and DDL operations on KWDB (KaiwuDB)"),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("SQL query to execute. Supports all write operations including INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, etc."),
		),
		mcp.WithRawOutputSchema(json.RawMessage(validOutputSchema)),
	)

	// Add write query handler
	s.AddTool(writeQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql := request.GetString("sql", "")

		// 从请求头中获取数据库 URI，用于多租户多数据库访问。
		// 兼容模式下，如果未提供 header 且默认连接池已初始化，则回退到单库连接池。
		headerURI := request.Header.Get("X-Database-URI")
		useURI, _, missingHeader := resolveDBTarget(headerURI, db.IsDefaultPoolInitialized())
		if missingHeader {
			return mcp.NewToolResultError("missing X-Database-URI header"), nil
		}

		var (
			rowsAffected int64
			err          error
		)
		if useURI != "" {
			rowsAffected, err = db.ExecuteWriteQueryWithURI(ctx, useURI, sql)
		} else {
			rowsAffected, err = db.ExecuteWriteQueryWithContext(ctx, sql)
		}
		if err != nil {
			return mcp.NewToolResultErrorFromErr("Write operation failed", err), nil
		}

		// Standardized success response
		response := map[string]interface{}{
			"status": "success",
			"type":   "write_result",
			"data": map[string]interface{}{
				"result_type":   "write",
				"affected_rows": rowsAffected,
				"metadata": map[string]interface{}{
					"query": sql,
				},
			},
			"error": nil,
		}

		// Convert result to JSON
		jsonResult, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to serialize result: %v", err)
		}

		return mcp.NewToolResultText(string(jsonResult)), nil
	})
}

// isSelectWithoutLimit checks if a SQL query is a SELECT statement without a LIMIT clause
func isSelectWithoutLimit(sql string) bool {
	// Convert to uppercase for case-insensitive comparison
	sqlUpper := strings.ToUpper(sql)

	// Check if it's a SELECT statement
	if !strings.HasPrefix(strings.TrimSpace(sqlUpper), "SELECT") {
		return false
	}

	// Check if it already has a LIMIT clause
	if strings.Contains(sqlUpper, " LIMIT ") {
		return false
	}

	// Check if it's a special case like EXPLAIN or SHOW
	if strings.HasPrefix(strings.TrimSpace(sqlUpper), "EXPLAIN") ||
		strings.HasPrefix(strings.TrimSpace(sqlUpper), "SHOW") {
		return false
	}

	return true
}

// addLimitToQuery adds a LIMIT clause to a SQL query
func addLimitToQuery(sql string, limit int) string {
	// Check if the query ends with a semicolon
	endsWithSemicolon := strings.TrimSpace(sql)[len(strings.TrimSpace(sql))-1] == ';'

	// Remove the semicolon if present
	if endsWithSemicolon {
		sql = strings.TrimSpace(sql)[:len(strings.TrimSpace(sql))-1]
	}

	// Add the LIMIT clause
	sql = fmt.Sprintf("%s LIMIT %d", sql, limit)

	// Add back the semicolon if it was present
	if endsWithSemicolon {
		sql = sql + ";"
	}

	return sql
}
