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
	var showVersion bool

	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio, sse, or http)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio, sse, or http)")
	flag.StringVar(&port, "p", "8080", "Port to listen on for HTTP/SSE mode")
	flag.StringVar(&port, "port", "8080", "Port to listen on for HTTP/SSE mode")
	flag.BoolVar(&showVersion, "v", false, "Show version information")
	flag.BoolVar(&showVersion, "version", false, "Show version information")

	// Parse command line parameters
	flag.Parse()

	// Check if version flag is set
	if showVersion {
		fmt.Printf("KWDB MCP Server %s\n", version.Version)
		os.Exit(0)
	}

	// Get remaining non-flag arguments
	args := flag.Args()
	if len(args) < 1 {
		log.Fatalf("Usage: %s [options] <postgresql-connection-string>", os.Args[0])
	}
	connectionString := args[0]

	// Check if port is valid
	portNum, err := strconv.Atoi(port)
	if err != nil || port == "" || port[0] == '-' || portNum < 0 || portNum > 65535 {
		log.Fatalf("Invalid port: %s (must be 0-65535)", port)
	}
	addr := ":" + port

	// Create server - won't exit due to database connection failure
	s, err := server.CreateServer(connectionString)
	if err != nil {
		transport = strings.ToLower(transport)
		log.Fatalf("Failed to create server: %v", err)
	}
	defer server.Cleanup()

	// Start server based on transport type
	transport = strings.ToLower(transport)
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
		log.Printf("Starting MCP server with HTTP transport on %s...", addr)
		if err := server.ServeHTTP(s, addr); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	default:
		log.Fatalf("Unknown transport type: %s. Use 'stdio', 'sse', or 'http'", transport)
	}
}
