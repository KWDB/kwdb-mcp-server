package server

import (
	"errors"
	"log"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/db"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/prompts"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/resources"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/tools"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/version"
	"github.com/mark3labs/mcp-go/server"
)

// CreateServer creates MCP server.
// If connectionString is non-empty, a default database pool is initialized for
// resources/prompts and for tools when X-Database-URI is not provided (single-DB mode).
// If connectionString is empty, the server runs in stateless multi-tenant mode and
// tools must use X-Database-URI to select the target database.
func CreateServer(connectionString string) (*server.MCPServer, error) {
	if connectionString != "" {
		if err := db.InitDB(connectionString); err != nil {
			return nil, err
		}
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
		return nil, err
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

// HTTPTLSConfig controls TLS for HTTP transport.
// When both CertFile and KeyFile are set, ServeHTTP uses mcp-go's WithTLSCert
// so StreamableHTTPServer starts with ListenAndServeTLS.
type HTTPTLSConfig struct {
	CertFile string
	KeyFile  string
}

// Enabled returns true when both certificate and private key are configured.
func (c *HTTPTLSConfig) Enabled() bool {
	return c != nil && c.CertFile != "" && c.KeyFile != ""
}

// Validate checks whether the TLS configuration is internally consistent.
// Only when both are empty, or both are non-empty, is the config valid.
func (c *HTTPTLSConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.CertFile == "" && c.KeyFile == "" {
		return nil
	}
	if c.CertFile == "" {
		return errors.New("tls certificate file is required when tls key file is set")
	}
	if c.KeyFile == "" {
		return errors.New("tls key file is required when tls certificate file is set")
	}
	return nil
}

// ServeHTTP starts the server using HTTP (streamable-http) mode.
// If tlsConfig is enabled, mcp-go's StreamableHTTPServer is started with
// server.WithTLSCert (TLS validation and ListenAndServeTLS are handled inside mcp-go).
func ServeHTTP(s *server.MCPServer, addr string, tlsConfig *HTTPTLSConfig) error {
	if err := tlsConfig.Validate(); err != nil {
		return err
	}

	var httpServer *server.StreamableHTTPServer
	if tlsConfig != nil && tlsConfig.Enabled() {
		httpServer = server.NewStreamableHTTPServer(s,
			server.WithTLSCert(tlsConfig.CertFile, tlsConfig.KeyFile),
		)
		log.Printf("HTTPS server listening on %s/mcp", addr)
	} else {
		httpServer = server.NewStreamableHTTPServer(s)
		log.Printf("HTTP server listening on %s/mcp", addr)
	}
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
