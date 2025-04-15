package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterResources registers all resources with the MCP server
func RegisterResources(s *server.MCPServer) error {
	// Get tables
	tables, err := db.GetTables()
	if err != nil {
		return fmt.Errorf("failed to list database tables: %v", err)
	}

	// Register KWDB product info resource
	registerKWDBProductInfo(s)

	// Register database info resource template (parameterized)
	registerDBInfoResourceTemplate(s)

	// Get all databases
	databases, err := db.GetDatabases()
	if err != nil {
		fmt.Printf("Warning: Failed to list databases: %v\n", err)
		// Continue even if we can't get the databases
	} else {
		// Register specific database info resources
		for _, dbName := range databases {
			registerSpecificDBInfoResource(s, dbName)
		}
	}

	// Register table resources
	for _, tableName := range tables {
		registerTableResource(s, tableName)
	}

	// TODO: Add metrics resource in the future

	return nil
}

// registerKWDBProductInfo registers information about the KWDB product as a whole
func registerKWDBProductInfo(s *server.MCPServer) {
	// 固定的产品信息 URI
	fixedURI := "kwdb://product_info"

	// Create KWDB product info resource
	kwdbInfoResource := mcp.NewResource(
		fixedURI,
		"KWDB (KaiwuDB) Product Information",
		mcp.WithResourceDescription("General information about the KWDB (KaiwuDB) product, version, and capabilities"),
		mcp.WithMIMEType("application/json"),
	)

	// Add KWDB product info resource handler
	s.AddResource(kwdbInfoResource, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// 不再检查 URI 是否为空，直接使用固定 URI

		// Get product info from database
		productInfo, err := db.GetProductInfo()
		if err != nil {
			// Return error if product info cannot be retrieved
			errorResponse := map[string]interface{}{
				"error":   "Failed to retrieve KWDB product information",
				"details": err.Error(),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      fixedURI, // 使用固定 URI 而不是请求中的 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Convert to JSON string
		infoJSON, err := json.MarshalIndent(productInfo, "", "  ")
		if err != nil {
			// Return error if marshaling fails
			errorResponse := map[string]interface{}{
				"error":   "Failed to marshal KWDB product information",
				"details": err.Error(),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      fixedURI, // 使用固定 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Return product info as JSON
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      fixedURI, // 使用固定 URI
				MIMEType: "application/json",
				Text:     string(infoJSON),
			},
		}, nil
	})
}

// 添加辅助函数，用于从URI中提取参数
func extractParamFromURI(uri, template string, paramName string) (string, error) {
	// 构建用于匹配的正则表达式
	pattern := template
	// 转义正则表达式中的特殊字符
	pattern = regexp.QuoteMeta(pattern)
	// 将模板中的参数替换为捕获组
	paramPattern := fmt.Sprintf(`\\\{%s\\\}`, paramName)
	pattern = regexp.MustCompile(paramPattern).
		ReplaceAllString(pattern, `([^/]+)`)
	pattern = "^" + pattern + "$"

	// 使用正则表达式提取参数
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid URI template: %v", err)
	}

	matches := re.FindStringSubmatch(uri)
	if len(matches) < 2 {
		return "", fmt.Errorf("URI does not match template")
	}

	// 返回提取的参数值
	return matches[1], nil
}

// registerDBInfoResourceTemplate registers the database info resource template
func registerDBInfoResourceTemplate(s *server.MCPServer) {
	// 模板 URI
	templateURI := "kwdb://db_info/{database_name}"

	// Create database info resource template
	dbInfoResource := mcp.NewResource(
		templateURI,
		"KWDB (KaiwuDB) Database Information",
		mcp.WithResourceDescription("Information about a specific KWDB (KaiwuDB) database, including properties and tables"),
		mcp.WithMIMEType("application/json"),
	)

	// Add database info resource handler
	s.AddResource(dbInfoResource, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// 使用请求 URI 或默认模板 URI
		uri := request.Params.URI
		if uri == "" {
			// 如果请求中 URI 为空，返回模板 URI 无法处理的说明
			errorResponse := map[string]interface{}{
				"error": "Cannot process database info template without a specific database name",
				"usage": "Use URI format: kwdb://db_info/{database_name} with a real database name",
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      templateURI,
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// 使用辅助函数提取数据库名称
		dbName, err := extractParamFromURI(uri, templateURI, "database_name")
		if err != nil {
			// 返回错误如果 URI 格式无效
			errorResponse := map[string]interface{}{
				"error": fmt.Sprintf("Invalid URI format: %v. Expected: %s", err, templateURI),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      uri, // 保留客户端提供的 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Get database info
		dbInfo, err := db.GetDatabaseInfoByName(dbName)
		if err != nil {
			// Return error if database info cannot be retrieved
			errorResponse := map[string]interface{}{
				"error":   "Failed to retrieve database information",
				"details": err.Error(),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      uri, // 保留客户端提供的 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Convert to JSON string
		dbInfoJSON, err := json.MarshalIndent(dbInfo, "", "  ")
		if err != nil {
			// Return error if marshaling fails
			errorResponse := map[string]interface{}{
				"error":   "Failed to marshal database information",
				"details": err.Error(),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      uri, // 保留客户端提供的 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Return database info as JSON
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      uri, // 保留客户端提供的 URI
				MIMEType: "application/json",
				Text:     string(dbInfoJSON),
			},
		}, nil
	})
}

// registerSpecificDBInfoResource registers a resource for a specific database
func registerSpecificDBInfoResource(s *server.MCPServer, dbName string) {
	// 为特定数据库创建固定 URI
	fixedURI := fmt.Sprintf("kwdb://db_info/%s", dbName)

	// Create database info resource
	dbInfoResource := mcp.NewResource(
		fixedURI,
		fmt.Sprintf("KWDB (KaiwuDB) Database: %s", dbName),
		mcp.WithResourceDescription(fmt.Sprintf("Information about the KWDB (KaiwuDB) database: %s", dbName)),
		mcp.WithMIMEType("application/json"),
	)

	// Add database info resource handler
	s.AddResource(dbInfoResource, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// 直接使用固定 URI，不再检查请求中的 URI

		// Get database info
		dbInfo, err := db.GetDatabaseInfoByName(dbName)
		if err != nil {
			// Return error if database info cannot be retrieved
			errorResponse := map[string]interface{}{
				"error":   "Failed to retrieve database information",
				"details": err.Error(),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      fixedURI, // 使用固定 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Convert to JSON string
		dbInfoJSON, err := json.MarshalIndent(dbInfo, "", "  ")
		if err != nil {
			// Return error if marshaling fails
			errorResponse := map[string]interface{}{
				"error":   "Failed to marshal database information",
				"details": err.Error(),
			}
			errorJSON, _ := json.Marshal(errorResponse)
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      fixedURI, // 使用固定 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Return database info as JSON
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      fixedURI, // 使用固定 URI
				MIMEType: "application/json",
				Text:     string(dbInfoJSON),
			},
		}, nil
	})
}

// registerTableResource registers a resource for a specific table
func registerTableResource(s *server.MCPServer, tableName string) {
	// 为特定表创建固定 URI
	fixedURI := fmt.Sprintf("kwdb://table/%s", tableName)

	// Create table resource
	tableResource := mcp.NewResource(
		fixedURI,
		fmt.Sprintf("Table: %s", tableName),
		mcp.WithResourceDescription(fmt.Sprintf("Schema of the %s table in KWDB (KaiwuDB)", tableName)),
		mcp.WithMIMEType("application/json"),
	)

	// Add table resource handler
	s.AddResource(tableResource, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		// 直接使用固定 URI，不再检查请求中的 URI

		// Get table columns
		tableInfo, err := db.GetTableColumns(tableName)
		if err != nil {
			// Standardized error response
			errorResponse := map[string]interface{}{
				"status": "error",
				"type":   "error_response",
				"data":   nil,
				"error": map[string]interface{}{
					"code":    "RESOURCE_NOT_FOUND",
					"message": fmt.Sprintf("Failed to get table schema: %v", err),
					"details": err.Error(),
					"table":   tableName,
				},
			}

			errorJSON, _ := json.MarshalIndent(errorResponse, "", "  ")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      fixedURI, // 使用固定 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		// Get table metadata including indexes and primary key
		tableMetadata, err := db.GetTableMetadata(tableName)
		if err != nil {
			// Log the error but continue without metadata
			fmt.Printf("Warning: Failed to get metadata for table %s: %v\n", tableName, err)
			tableMetadata = map[string]interface{}{}
		}

		// Get example queries from the database
		exampleQueries, err := db.GetTableExampleQueries(tableName)
		if err != nil {
			// Log the error but continue
			fmt.Printf("Warning: Failed to get example queries for table %s: %v\n", tableName, err)
			exampleQueries = map[string][]string{
				"read":  {},
				"write": {},
			}
		}

		// Standardized response
		response := map[string]interface{}{
			"status": "success",
			"type":   "table_schema",
			"data": map[string]interface{}{
				"table_name":            tableName,
				"columns":               tableInfo,
				"read_example_queries":  exampleQueries["read"],
				"write_example_queries": exampleQueries["write"],
			},
			"error": nil,
		}

		// Add metadata to the response
		if tableMetadata != nil {
			dataMap := response["data"].(map[string]interface{})

			// Add table_type if available
			if tableType, ok := tableMetadata["table_type"]; ok {
				dataMap["table_type"] = tableType
			}

			// Add storage_engine if available
			if storageEngine, ok := tableMetadata["storage_engine"]; ok {
				dataMap["storage_engine"] = storageEngine
			}

			// Add primary_key if available
			if primaryKey, ok := tableMetadata["primary_key"]; ok {
				dataMap["primary_key"] = primaryKey
			}

			// Add indexes if available
			if indexes, ok := tableMetadata["indexes"]; ok {
				dataMap["indexes"] = indexes
			}

			// Add partition_info if available
			if partitionInfo, ok := tableMetadata["partition_info"]; ok {
				dataMap["partition_info"] = partitionInfo
			}
		}

		// Convert table schema to JSON
		schemaJSON, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			// Standardized error response
			errorResponse := map[string]interface{}{
				"status": "error",
				"type":   "error_response",
				"data":   nil,
				"error": map[string]interface{}{
					"code":    "INTERNAL_ERROR",
					"message": "Failed to serialize table schema",
					"details": err.Error(),
					"table":   tableName,
				},
			}

			errorJSON, _ := json.MarshalIndent(errorResponse, "", "  ")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      fixedURI, // 使用固定 URI
					MIMEType: "application/json",
					Text:     string(errorJSON),
				},
			}, nil
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      fixedURI, // 使用固定 URI
				MIMEType: "application/json",
				Text:     string(schemaJSON),
			},
		}, nil
	})
}
