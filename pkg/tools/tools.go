package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
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

// registerReadQueryTool registers read query tool with concurrency and timeout support
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
		sql := request.GetString("sql", "")
		originalSQL := sql

		// Add timeout control
		queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Check if the query is a SELECT statement without LIMIT
		if isSelectWithoutLimit(sql) {
			// Add LIMIT 20 to the query
			sql = addLimitToQuery(sql, 20)
		}

		// 使用连接池执行查询
		result, err := db.ExecuteQueryWithContext(queryCtx, sql)
		
		var response map[string]interface{}
		
		if err != nil {
			// 将错误作为正常结果返回，而不是抛出异常
			errorMessage := err.Error()
			if queryCtx.Err() == context.DeadlineExceeded {
				errorMessage = "Query timeout: the query took too long to execute"
			}
			
			response = map[string]interface{}{
				"status": "error",
				"type":   "query_result",
				"data":   nil,
				"error": map[string]interface{}{
					"message": errorMessage,
					"query":   sql,
					"original_query": originalSQL,
				},
			}
		} else {
			// Extract column names (if result is not empty)
			var columns []string
			if len(result) > 0 {
				columns = make([]string, 0, len(result[0]))
				for col := range result[0] {
					columns = append(columns, col)
				}
			}

			// Standardized success response
			response = map[string]interface{}{
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
	)

	// Add write query handler
	s.AddTool(writeQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql := request.GetString("sql", "")

		// 添加超时控制
		queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second) // 写操作给更长的超时时间
		defer cancel()

		// 使用连接池执行写操作
		rowsAffected, err := db.ExecuteWriteQueryWithContext(queryCtx, sql)
		
		var response map[string]interface{}
		
		if err != nil {
			// 将错误作为正常结果返回，而不是抛出异常
			errorMessage := err.Error()
			if queryCtx.Err() == context.DeadlineExceeded {
				errorMessage = "Write operation timeout: the operation took too long to complete"
			}
			
			response = map[string]interface{}{
				"status": "error",
				"type":   "write_result",
				"data":   nil,
				"error": map[string]interface{}{
					"message": errorMessage,
					"query":   sql,
				},
			}
		} else {
			// Standardized success response
			response = map[string]interface{}{
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
