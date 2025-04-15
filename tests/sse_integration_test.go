package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestSSEServer(t *testing.T) {
	// Skip if running in CI environment
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping SSE integration test in CI environment")
	}

	// Get base URL from environment variable
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		// Fallback to default base URL if not provided
		baseURL = "http://localhost:8082"
	}

	// 添加测试结果计数器
	testSummary := struct {
		ResourceSuccess   int
		ResourceError     int
		PromptSuccess     int
		PromptError       int
		QuerySuccess      int
		QueryError        int
		CompletionSuccess int
		CompletionError   int
	}{}

	// Create KWDB MCP client using SSE transport
	sseURL := baseURL + "/sse" // SSE端点路径
	t.Logf("Connecting to SSE server at: %s", baseURL)
	t.Logf("Using SSE endpoint: %s", sseURL)

	// 检查环境变量是否指定了不同的URL
	if envURL := os.Getenv("SSE_SERVER_URL"); envURL != "" {
		baseURL = envURL
		sseURL = baseURL + "/sse"
		t.Logf("Using SSE server URL from environment: %s", baseURL)
		t.Logf("Using SSE endpoint: %s", sseURL)
	}

	// 检查服务器是否正在运行，但不测试SSE端点（因为SSE是长连接）
	t.Logf("Testing server availability with HTTP GET to message endpoint...")
	msgURL := baseURL + "/message"
	resp, err := http.Get(msgURL)
	if err != nil {
		t.Logf("Warning: HTTP GET to message endpoint %s failed: %v", msgURL, err)
		t.Logf("Server may not be running or message endpoint is not accessible")
	} else {
		defer resp.Body.Close()
		statusCode := resp.StatusCode
		t.Logf("Message endpoint HTTP Status: %d (404是正常的，因为需要sessionId参数)", statusCode)
	}

	// 创建SSE客户端，明确使用sseURL作为SSE端点
	t.Logf("Creating SSE client with explicit SSE endpoint: %s", sseURL)
	c, err := client.NewSSEMCPClient(sseURL)
	if err != nil {
		t.Fatalf("Failed to create SSE client: %v", err)
	}
	defer c.Close()

	// 创建上下文并设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 重要：使用更长的超时
	connectTimeout := 10 * time.Second
	t.Logf("Using connection timeout of %v", connectTimeout)

	// 打印调试信息
	t.Logf("Starting SSE client connection to %s", sseURL)

	// 添加重试逻辑
	maxRetries := 3
	var lastError error
	for i := 0; i < maxRetries; i++ {
		t.Logf("Connection attempt %d of %d...", i+1, maxRetries)

		// 启动SSE客户端连接，设置超时
		connectCtx, connectCancel := context.WithTimeout(ctx, connectTimeout)
		t.Logf("Starting connection to SSE endpoint: %s with %v timeout", sseURL, connectTimeout)
		err = c.Start(connectCtx)
		// 不要立即取消上下文，让SSE连接保持打开状态
		// connectCancel()

		if err == nil {
			// 连接成功，延迟调用connectCancel
			defer connectCancel()
			break // 连接成功
		}

		connectCancel() // 如果有错误，取消这次连接的上下文
		lastError = err
		t.Logf("Connection attempt %d failed: %v", i+1, err)

		if i < maxRetries-1 {
			// 在重试前等待
			waitTime := time.Duration(i+1) * 2 * time.Second
			t.Logf("Waiting %v before next attempt...", waitTime)
			time.Sleep(waitTime)
		}
	}

	if lastError != nil {
		t.Fatalf("Failed to start SSE client after %d attempts: %v", maxRetries, lastError)
	}

	t.Log("SSE client successfully connected")
	endpointURL := c.GetEndpoint()
	t.Logf("Received endpoint: %s", endpointURL)

	// 检查端点URL是否包含会话ID
	if !strings.Contains(endpointURL.String(), "sessionId=") {
		t.Fatalf("Endpoint URL doesn't contain a sessionId: %s", endpointURL)
	}

	t.Logf("Extracting sessionId from endpoint URL...")
	query := endpointURL.Query()
	sessionID := query.Get("sessionId")
	t.Logf("Using sessionId: %s", sessionID)

	// 注册通知处理程序
	c.OnNotification(func(notification mcp.JSONRPCNotification) {
		t.Logf("Received notification: %s", notification.Method)
	})

	// Initialize the client
	t.Log("Initializing client...")
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "sse-integration-test",
		Version: "1.0.0",
	}

	// 使用较长的超时时间
	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initCancel() // 延迟取消而不是立即取消

	t.Logf("Sending initialize request with 10s timeout...")
	initResult, err := c.Initialize(initCtx, initRequest)
	// 不要立即取消，让上下文保持活动状态
	// initCancel()

	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	t.Logf(
		"Initialized with server: %s %s",
		initResult.ServerInfo.Name,
		initResult.ServerInfo.Version,
	)
	if initResult.ServerInfo.Name == "" {
		t.Fatal("Server name should not be empty")
	}

	// Test ping
	t.Log("Testing ping...")
	err = c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// List available tools
	t.Log("Listing available tools...")
	toolsRequest := mcp.ListToolsRequest{}
	tools, err := c.ListTools(ctx, toolsRequest)
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("Server should provide some tools")
	}

	for _, tool := range tools.Tools {
		t.Logf("Tool: %s - %s", tool.Name, tool.Description)
	}

	// List resources
	t.Log("Listing resources...")
	listResourcesRequest := mcp.ListResourcesRequest{}
	resources, err := c.ListResources(ctx, listResourcesRequest)
	if err != nil {
		t.Errorf("Failed to list resources: %v", err)
		testSummary.ResourceError++
	} else {
		t.Logf("Found %d resources", len(resources.Resources))
		// Try to read each resource
		if len(resources.Resources) > 0 {
			t.Log("Reading resources...")

			// Attempt to read resources
			for i, resource := range resources.Resources {
				t.Logf("Resource %d: %s (%s)", i, resource.Name, resource.URI)

				// Skip template resources (those with placeholders in URI)
				if strings.Contains(resource.URI, "{") && strings.Contains(resource.URI, "}") {
					t.Logf("  Skipping template resource")
					continue
				}

				readRequest := mcp.ReadResourceRequest{}
				readRequest.Params.URI = resource.URI

				// Create a context with a short timeout for each resource read
				resourceCtx, resourceCancel := context.WithTimeout(context.Background(), 5*time.Second)

				result, err := c.ReadResource(resourceCtx, readRequest)
				if err != nil {
					t.Logf("  Failed to read resource: %v", err)
					testSummary.ResourceError++
				} else {
					t.Logf("  Successfully read resource, got %d content items", len(result.Contents))
					testSummary.ResourceSuccess++
					// Log the first few characters of each content item
					for j, content := range result.Contents {
						switch c := content.(type) {
						case mcp.TextResourceContents:
							preview := c.Text
							if len(preview) > 100 {
								preview = preview[:100] + "..."
							}
							t.Logf("    Content %d: [%s] %s", j, c.MIMEType, preview)
						case mcp.BlobResourceContents:
							t.Logf("    Content %d: [%s] <binary data>", j, c.MIMEType)
						default:
							t.Logf("    Content %d: <unknown type>", j)
						}
					}
				}

				resourceCancel()
			}
		}
	}

	// 测试资源订阅
	t.Log("Testing resource subscription...")
	if len(resources.Resources) > 0 {
		// 查找一个合适的资源进行订阅测试
		var targetResource string
		for _, resource := range resources.Resources {
			if !strings.Contains(resource.URI, "{") && !strings.Contains(resource.URI, "}") {
				targetResource = resource.URI
				break
			}
		}

		if targetResource != "" {
			t.Logf("Subscribing to resource: %s", targetResource)

			subRequest := mcp.SubscribeRequest{}
			subRequest.Params.URI = targetResource

			err := c.Subscribe(ctx, subRequest)
			if err != nil {
				t.Logf("Failed to subscribe to resource: %v", err)
			} else {
				t.Logf("Successfully subscribed to resource")

				// 给一些时间接收通知
				time.Sleep(2 * time.Second)

				// 尝试取消订阅
				unsubRequest := mcp.UnsubscribeRequest{}
				unsubRequest.Params.URI = targetResource

				err := c.Unsubscribe(ctx, unsubRequest)
				if err != nil {
					t.Logf("Failed to unsubscribe from resource: %v", err)
				} else {
					t.Logf("Successfully unsubscribed from resource")
				}
			}
		} else {
			t.Log("No suitable resource found for subscription test")
		}
	}

	// List prompts
	t.Log("Listing prompts...")
	listPromptsRequest := mcp.ListPromptsRequest{}
	prompts, err := c.ListPrompts(ctx, listPromptsRequest)
	if err != nil {
		t.Errorf("Failed to list prompts: %v", err)
		testSummary.PromptError++
	} else {
		t.Logf("Found %d prompts", len(prompts.Prompts))
		// Try to get each prompt
		if len(prompts.Prompts) > 0 {
			t.Log("Getting prompts...")
			for i, prompt := range prompts.Prompts {
				t.Logf("Prompt %d: %s", i, prompt.Name)

				// Skip prompt templates that require arguments
				if len(prompt.Arguments) > 0 {
					requiredArgs := 0
					for _, arg := range prompt.Arguments {
						if arg.Required {
							requiredArgs++
						}
					}
					if requiredArgs > 0 {
						t.Logf("  Skipping template prompt with %d required arguments", requiredArgs)
						continue
					}
				}

				getRequest := mcp.GetPromptRequest{}
				getRequest.Params.Name = prompt.Name

				// Create a context with a short timeout for each prompt get
				promptCtx, promptCancel := context.WithTimeout(context.Background(), 5*time.Second)

				result, err := c.GetPrompt(promptCtx, getRequest)
				if err != nil {
					t.Logf("  Failed to get prompt: %v", err)
					testSummary.PromptError++
				} else {
					t.Logf("  Successfully got prompt with %d messages", len(result.Messages))
					testSummary.PromptSuccess++
					// Log some details about each message
					for j, msg := range result.Messages {
						t.Logf("    Message %d: Role=%s", j, msg.Role)
						// Try to log a preview of the content
						switch c := msg.Content.(type) {
						case mcp.TextContent:
							preview := c.Text
							if len(preview) > 100 {
								preview = preview[:100] + "..."
							}
							t.Logf("      Content: %s", preview)
						default:
							t.Logf("      Content: <non-text content>")
						}
					}
				}

				promptCancel()
			}
		}
	}

	// 测试读查询工具 - 1秒超时
	if containsToolSSE(tools.Tools, "read-query") {
		toolName := "read-query"
		t.Logf("Testing read query with 1 second timeout...")

		// 创建只有1秒超时的上下文
		queryCtx, queryCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer queryCancel()

		callToolRequest := mcp.CallToolRequest{}
		callToolRequest.Params.Name = toolName
		callToolRequest.Params.Arguments = map[string]interface{}{
			"sql": "SELECT 1", // 简单查询
		}

		startTime := time.Now()
		_, err := c.CallTool(queryCtx, callToolRequest)
		queryDuration := time.Since(startTime)

		if err != nil {
			t.Logf("❌ Read query failed: %v (took %v)", err, queryDuration)
			testSummary.QueryError++
		} else {
			t.Logf("✅ Read query succeeded in %v", queryDuration)
			testSummary.QuerySuccess++
			t.Logf("Result received")
		}
	}

	// 测试写查询工具
	if containsToolSSE(tools.Tools, "write-query") {
		toolName := "write-query"
		t.Logf("Testing write query with 1 second timeout...")

		// 创建只有1秒超时的上下文
		queryCtx, queryCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer queryCancel()

		// 创建临时表来测试写入功能
		tempTableName := fmt.Sprintf("test_tmp_%d", time.Now().Unix())

		callToolRequest := mcp.CallToolRequest{}
		callToolRequest.Params.Name = toolName
		callToolRequest.Params.Arguments = map[string]interface{}{
			"sql": fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INT, name TEXT)", tempTableName),
		}

		startTime := time.Now()
		_, err := c.CallTool(queryCtx, callToolRequest)
		queryDuration := time.Since(startTime)

		if err != nil {
			t.Logf("❌ Write query failed: %v (took %v)", err, queryDuration)
			testSummary.QueryError++
		} else {
			t.Logf("✅ Write query succeeded in %v", queryDuration)
			testSummary.QuerySuccess++
			t.Logf("Result received")

			// 清理：删除临时表
			cleanupRequest := mcp.CallToolRequest{}
			cleanupRequest.Params.Name = toolName
			cleanupRequest.Params.Arguments = map[string]interface{}{
				"sql": fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTableName),
			}

			_, cleanupErr := c.CallTool(ctx, cleanupRequest)
			if cleanupErr != nil {
				t.Logf("Warning: Failed to clean up temporary table: %v", cleanupErr)
			}
		}
	}

	// 尝试完成API (如果支持)
	completeRequest := mcp.CompleteRequest{}
	// 适配最新的API，避免使用可能不存在的字段
	completeRequest.Params.Ref = "SELECT * FROM"
	_, err = c.Complete(ctx, completeRequest)
	if err != nil {
		t.Logf("Completion API not supported or failed: %v", err)
		testSummary.CompletionError++
	} else {
		t.Logf("✅ Completion API supported!")
		testSummary.CompletionSuccess++
		// 适配结果结构
		t.Logf("Completion response received")
	}

	// 打印测试摘要
	t.Logf("\n==== SSE测试摘要 ====")
	t.Logf("资源测试: %d 成功, %d 失败", testSummary.ResourceSuccess, testSummary.ResourceError)
	t.Logf("提示词测试: %d 成功, %d 失败", testSummary.PromptSuccess, testSummary.PromptError)
	t.Logf("查询测试: %d 成功, %d 失败", testSummary.QuerySuccess, testSummary.QueryError)
	t.Logf("补全API测试: %d 成功, %d 失败", testSummary.CompletionSuccess, testSummary.CompletionError)

	// 确定整体测试状态
	if testSummary.ResourceError > 0 || testSummary.QueryError > 0 {
		t.Errorf("❌ 测试失败: 一些测试项目出现错误")
	} else {
		t.Logf("✅ 所有测试项目成功完成")
	}
}

func containsToolSSE(tools []mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
