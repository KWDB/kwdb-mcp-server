package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"strconv"
	"strings"

	"gitee.com/kwdb/kwdb-mcp-server/pkg/server"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	// 定义命令行参数
	var transport string
	var port string

	flag.StringVar(&transport, "t", "stdio", "Transport type (stdio, sse, or http)")
	flag.StringVar(&transport, "transport", "stdio", "Transport type (stdio, sse, or http)")
	flag.StringVar(&port, "p", "8080", "Port to listen on for HTTP/SSE mode")
	flag.StringVar(&port, "port", "8080", "Port to listen on for HTTP/SSE mode")

	// 解析命令行参数
	flag.Parse()

	// 获取剩余的非标志参数
	args := flag.Args()
	if len(args) < 1 {
		log.Fatalf("Usage: %s [options] <postgresql-connection-string>", os.Args[0])
	}
	connectionString := args[0]

	// 检查端口是否合法
	portNum, err := strconv.Atoi(port)
	if err != nil || port == "" || port[0] == '-' || portNum < 0 || portNum > 65535 {
		log.Fatalf("Invalid port: %s (must be 0-65535)", port)
	}
	addr := ":" + port

	// 创建服务器
	s, err := server.CreateServer(connectionString)
	if err != nil {
		transport = strings.ToLower(transport)
		if transport == "stdio" {
			log.Printf("Failed to create server: %v", err)
			resp := mcp.JSONRPCError{
				JSONRPC: mcp.JSONRPC_VERSION,
				ID:      mcp.NewRequestId(nil),
				Error: struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Data    any    `json:"data,omitempty"`
				}{
					Code:    -32603, // INTERNAL_ERROR
					Message: err.Error(),
				},
			}
			b, _ := json.Marshal(resp)
			os.Stdout.Write(b)
			os.Stdout.Sync()
			os.Stdout.Close()
			os.Exit(1)
		} else {
			log.Fatalf("Failed to create server: %v", err)
		}
	}
	defer server.Cleanup()

	// 根据传输类型启动服务器
	transport = strings.ToLower(transport)
	if transport == "http" {
		listenAddr := "0.0.0.0" + addr
		if err := server.ServeHTTP(s, listenAddr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else if transport == "sse" {
		log.Printf("[DEPRECATION WARNING] SSE mode is deprecated and will be removed in a future version. Please use http mode instead.")
		listenAddr := "0.0.0.0" + addr
		if err := server.ServeSSE(s, listenAddr, ""); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		if err := server.ServeStdio(s); err != nil {
			// 统一处理正常关闭情况
			if isNormalShutdown(err) {
				log.Printf("Server stopped: %v", err)
				os.Exit(0)
			} else {
				log.Fatalf("Server error: %v", err)
			}
		}
	}
}

// isNormalShutdown 检查是否为正常的关闭情况
func isNormalShutdown(err error) bool {
	if err == nil {
		return true
	}

	// 检查是否为context canceled
	if errors.Is(err, context.Canceled) {
		return true
	}

	errMsg := err.Error()
	// 常见的正常关闭错误信息
	normalErrors := []string{
		"EOF",
		"io: read/write on closed pipe",
		"use of closed network connection",
		"connection reset by peer",
		"broken pipe",
	}

	for _, normalErr := range normalErrors {
		if strings.Contains(errMsg, normalErr) {
			return true
		}
	}

	return false
}
