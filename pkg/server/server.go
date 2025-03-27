package server

import (
	"fmt"
	"log"

	"gitee.com/kwdb/mcp-kwdb-server-go/pkg/db"
	"gitee.com/kwdb/mcp-kwdb-server-go/pkg/prompts"
	"gitee.com/kwdb/mcp-kwdb-server-go/pkg/resources"
	"gitee.com/kwdb/mcp-kwdb-server-go/pkg/tools"
	"github.com/mark3labs/mcp-go/server"
)

// CreateServer creates a new MCP server
func CreateServer(connectionString string) (*server.MCPServer, error) {
	// Initialize database connection
	if err := db.InitDB(connectionString); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	// Create MCP server with capabilities
	s := server.NewMCPServer(
		"KWDB (KaiwuDB) MCP Server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
		server.WithLogging(),
		server.WithInstructions("This server allows you to interact with KWDB (KaiwuDB) databases using SQL."),
	)

	// Register resources
	if err := resources.RegisterResources(s); err != nil {
		return nil, err
	}

	// Register prompts
	prompts.RegisterPrompts(s)

	// Register tools
	tools.RegisterTools(s)

	log.Println("KWDB (KaiwuDB) MCP Server initialized successfully")
	return s, nil
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

// Cleanup performs cleanup operations
func Cleanup() {
	db.Close()
}
