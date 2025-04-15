package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/server"
)

func main() {
	// 定义命令行参数
	var transport string
	var addr string
	var baseURL string

	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio or sse)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio or sse)")
	flag.StringVar(&addr, "addr", ":8080", "Address to listen on for SSE mode")
	flag.StringVar(&baseURL, "base-url", "http://localhost:8080", "Base URL for SSE mode")

	// 解析命令行参数
	flag.Parse()

	// 获取剩余的非标志参数
	args := flag.Args()
	if len(args) < 1 {
		log.Fatalf("Usage: %s [options] <postgresql-connection-string>", os.Args[0])
	}
	connectionString := args[0]

	// 创建服务器
	s, err := server.CreateServer(connectionString)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer server.Cleanup()

	// 根据传输类型启动服务器
	transport = strings.ToLower(transport)
	if transport == "sse" {
		if err := server.ServeSSE(s, addr, baseURL); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
