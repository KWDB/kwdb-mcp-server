package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gitee.com/kwdb/mcp-kwdb-server-go/pkg/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTools registers all tools with the MCP server
func RegisterTools(s *server.MCPServer) {
	// Register read query tool
	registerReadQueryTool(s)

	// Register write query tool
	registerWriteQueryTool(s)
}

// registerReadQueryTool registers the read-query tool
func registerReadQueryTool(s *server.MCPServer) {
	// Create read query tool
	readQueryTool := mcp.NewTool("read-query",
		mcp.WithDescription("Execute SELECT, SHOW, EXPLAIN and other read-only queries on KWDB (KaiwuDB). SELECT queries without a LIMIT clause will automatically have LIMIT 20 added to prevent large result sets."),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("SQL query to execute. Only read operations like SELECT, SHOW, EXPLAIN are allowed."),
		),
	)

	// Add read query handler
	s.AddTool(readQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql := request.Params.Arguments["sql"].(string)
		originalSQL := sql

		// Check if the query is a SELECT statement without LIMIT
		if isSelectWithoutLimit(sql) {
			// Add LIMIT 20 to the query
			sql = addLimitToQuery(sql, 20)
		}

		// Execute query
		result, err := db.ExecuteQuery(sql)
		if err != nil {
			// Standardized error response
			errorResponse := map[string]interface{}{
				"status": "error",
				"type":   "error_response",
				"data":   nil,
				"error": map[string]interface{}{
					"code":    "SYNTAX_ERROR",
					"message": fmt.Sprintf("Query error: %v", err),
					"details": err.Error(),
					"query":   originalSQL,
				},
			}

			// Convert error response to JSON
			jsonResult, jsonErr := json.MarshalIndent(errorResponse, "", "  ")
			if jsonErr != nil {
				return nil, fmt.Errorf("failed to serialize error: %v", jsonErr)
			}

			return mcp.NewToolResultText(string(jsonResult)), nil
		}

		// Extract column names (if result is not empty)
		var columns []string
		if len(result) > 0 {
			columns = make([]string, 0, len(result[0]))
			for col := range result[0] {
				columns = append(columns, col)
			}
		}

		// Standardized response
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

// registerWriteQueryTool registers the write-query tool
func registerWriteQueryTool(s *server.MCPServer) {
	// Create write query tool
	writeQueryTool := mcp.NewTool("write-query",
		mcp.WithDescription("Execute data modification queries including DML and DDL operations on KWDB (KaiwuDB)"),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("SQL query to execute. Supports all write operations including INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, etc."),
		),
	)

	// Add write query handler
	s.AddTool(writeQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql := request.Params.Arguments["sql"].(string)

		// Execute write operation
		rowsAffected, err := db.ExecuteWriteQuery(sql)
		if err != nil {
			// Standardized error response
			errorResponse := map[string]interface{}{
				"status": "error",
				"type":   "error_response",
				"data":   nil,
				"error": map[string]interface{}{
					"code":    "SYNTAX_ERROR",
					"message": fmt.Sprintf("Query error: %v", err),
					"details": err.Error(),
					"query":   sql,
				},
			}

			// Convert error response to JSON
			jsonResult, jsonErr := json.MarshalIndent(errorResponse, "", "  ")
			if jsonErr != nil {
				return nil, fmt.Errorf("failed to serialize error: %v", jsonErr)
			}

			return mcp.NewToolResultText(string(jsonResult)), nil
		}

		// Standardized response
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
