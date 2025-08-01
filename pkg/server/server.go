package server

import (
	"fmt"
	"log"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/prompts"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/resources"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/tools"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/version"
	"github.com/mark3labs/mcp-go/server"
)

// CreateServer creates MCP server with delayed database connection initialization
func CreateServer(connectionString string) (*server.MCPServer, error) {
	// Set connection string but don't connect immediately
	if err := db.InitDB(connectionString); err != nil {
		return nil, fmt.Errorf("failed to initialize database configuration: %v", err)
	}

	// Create MCP server with capabilities
	s := server.NewMCPServer(
		"KWDB (KaiwuDB) MCP Server",
		version.Version[1:],
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithInstructions("This server allows you to interact with KWDB (KaiwuDB) databases using SQL."),
	)

	// Register resources with lazy loading - don't fetch table info at startup
	if err := registerLazyResources(s); err != nil {
		return nil, fmt.Errorf("failed to register resources: %v", err)
	}

	// Register prompts - basic prompts without table schemas
	prompts.RegisterPrompts(s)

	// Register tools
	tools.RegisterTools(s)

	log.Println("KWDB (KaiwuDB) MCP Server initialized successfully (database connection will be established on demand)")
	return s, nil
}

// registerLazyResources registers resources with lazy loading
func registerLazyResources(s *server.MCPServer) error {
	// Register basic resources
	resources.RegisterStaticResources(s)

	// Register dynamic resource templates without preloading data
	resources.RegisterDynamicResourceTemplates(s)

	return nil
}

// ServeStdio starts the server using stdio
func ServeStdio(s *server.MCPServer) error {
	return server.ServeStdio(s)
}

// ServeSSE starts the server using SSE over HTTP
func ServeSSE(s *server.MCPServer, addr string, baseURL string) error {
	sseServer := server.NewSSEServer(s, server.WithBaseURL(baseURL))
	log.Printf("SSE server listening on %s", addr)
	return sseServer.Start(addr)
}

// ServeHTTP starts the server using HTTP (streamable-http) mode
func ServeHTTP(s *server.MCPServer, addr string) error {
	httpServer := server.NewStreamableHTTPServer(s)
	log.Printf("HTTP server listening on %s/mcp", addr)
	return httpServer.Start(addr)
}

// Cleanup performs cleanup operations
func Cleanup() {
	db.Close()
}

// GetConnectionStats 获取连接池统计信息
func GetConnectionStats() interface{} {
	stats := db.GetConnectionStats()
	return map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration":        stats.WaitDuration.String(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}
