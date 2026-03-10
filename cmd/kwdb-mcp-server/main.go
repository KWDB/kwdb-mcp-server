package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/server"
	"gitee.com/kwdb/kwdb-mcp-server/pkg/version"
)

func main() {
	// Define command line parameters
	var transport string
	var port string
	var tlsCertFile string
	var tlsKeyFile string
	var showVersion bool

	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio, sse, or http)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio, sse, or http)")
	flag.StringVar(&port, "p", "8080", "Port to listen on for HTTP/SSE mode")
	flag.StringVar(&port, "port", "8080", "Port to listen on for HTTP/SSE mode")
	flag.StringVar(&tlsCertFile, "tls-cert", "", "TLS certificate file for HTTP mode (requires --tls-key)")
	flag.StringVar(&tlsKeyFile, "tls-key", "", "TLS private key file for HTTP mode (requires --tls-cert)")
	flag.BoolVar(&showVersion, "v", false, "Show version information")
	flag.BoolVar(&showVersion, "version", false, "Show version information")

	// Parse command line parameters
	flag.Parse()

	// Check if version flag is set
	if showVersion {
		fmt.Printf("KWDB MCP Server %s\n", version.Version)
		os.Exit(0)
	}

	// Check if port is valid
	portNum, err := strconv.Atoi(port)
	if err != nil || port == "" || port[0] == '-' || portNum < 0 || portNum > 65535 {
		log.Fatalf("Invalid port: %s (must be 0-65535)", port)
	}
	addr := ":" + port

	// Get remaining non-flag arguments (optional connection string)
	args := flag.Args()
	var connectionString string
	if len(args) > 0 {
		connectionString = args[0]
	}

	// Create server - if connectionString is empty, tools must use X-Database-URI
	s, err := server.CreateServer(connectionString)
	if err != nil {
		transport = strings.ToLower(transport)
		log.Fatalf("Failed to create server: %v", err)
	}
	defer server.Cleanup()

	// Start server based on transport type
	transport = strings.ToLower(transport)
	httpTLSConfig := &server.HTTPTLSConfig{
		CertFile: tlsCertFile,
		KeyFile:  tlsKeyFile,
	}

	switch transport {
	case "stdio":
		log.Println("Starting MCP server with stdio transport...")
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("Failed to start stdio server: %v", err)
		}
	case "sse":
		baseURL := os.Getenv("BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost" + addr
		}
		log.Printf("Starting MCP server with SSE transport on %s...", addr)
		if err := server.ServeSSE(s, addr, baseURL); err != nil {
			log.Fatalf("Failed to start SSE server: %v", err)
		}
	case "http":
		// TLS flags only apply to HTTP transport; validate here so stdio/sse are unaffected.
		if err := httpTLSConfig.Validate(); err != nil {
			log.Fatalf("Invalid HTTP TLS configuration: %v", err)
		}
		protocol := "HTTP"
		if httpTLSConfig.Enabled() {
			protocol = "HTTPS"
		}
		log.Printf("Starting MCP server with %s transport on %s...", protocol, addr)
		if err := server.ServeHTTP(s, addr, httpTLSConfig); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	default:
		log.Fatalf("Unknown transport type: %s. Use 'stdio', 'sse', or 'http'", transport)
	}
}
