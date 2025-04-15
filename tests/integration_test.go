package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var connectionString string

func TestMCPServer(t *testing.T) {
	// Skip if running in CI environment
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping integration test in CI environment")
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

	// Create KWDB MCP client
	serverPath := "./bin/kwdb-mcp-server"

	c, err := client.NewStdioMCPClient(
		serverPath,
		[]string{}, // Empty ENV
		connectionString,
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Create context with longer timeout (2 minutes instead of 30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Initialize the client
	t.Log("Initializing client...")
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "integration-test",
		Version: "1.0.0",
	}

	initResult, err := c.Initialize(ctx, initRequest)
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
		// 确保测试被标记为失败
		t.FailNow()
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

				// 添加调试日志，显示发送时的 URI
				t.Logf("  Requesting with URI: '%s'", readRequest.Params.URI)

				// Create a context with a short timeout for each resource read
				resourceCtx, resourceCancel := context.WithTimeout(context.Background(), 5*time.Second)

				result, err := c.ReadResource(resourceCtx, readRequest)
				if err != nil {
					// 增强错误日志，记录完整错误信息
					t.Logf("  Failed to read resource: %v", err)
					testSummary.ResourceError++
					// 检查是否是 URI 错误
					if strings.Contains(err.Error(), "resource uri is missing") {
						t.Logf("  *** URI ERROR *** - This might be a protocol mismatch or URI handling issue")
					}
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
							// 记录返回内容的 URI
							t.Logf("    Response URI: '%s'", c.URI)
						case mcp.BlobResourceContents:
							t.Logf("    Content %d: [%s] <binary data>", j, c.MIMEType)
							// 记录返回内容的 URI
							t.Logf("    Response URI: '%s'", c.URI)
						default:
							t.Logf("    Content %d: <unknown type>", j)
						}
					}
				}

				resourceCancel()
			}
		}
	}

	// List prompts
	t.Log("Listing prompts...")
	listPromptsRequest := mcp.ListPromptsRequest{}
	prompts, err := c.ListPrompts(ctx, listPromptsRequest)
	if err != nil {
		t.Errorf("Failed to list prompts: %v", err)
		// 确保测试被标记为失败
		t.FailNow()
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

	// ==== SQL查询性能测试：1秒超时判定为失败 ====

	// 测试读查询 - 1秒超时
	if containsTool(tools.Tools, "read-query") {
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
		result, err := c.CallTool(queryCtx, callToolRequest)
		duration := time.Since(startTime)

		if err != nil {
			testSummary.QueryError++
			if err == context.DeadlineExceeded || duration >= 1*time.Second {
				t.Errorf("❌ 读查询超时 (>1秒): %v，用时: %v", err, duration)
			} else {
				t.Errorf("❌ 读查询失败: %v", err)
			}
			// 不让测试停止，继续执行其他测试
		} else {
			testSummary.QuerySuccess++
			t.Logf("✅ 读查询成功完成，用时: %v", duration)
			for i, content := range result.Content {
				t.Logf("Content %d: %+v", i, content)
			}
		}
	}

	// 测试写查询 - 1秒超时
	if containsTool(tools.Tools, "write-query") {
		toolName := "write-query"
		t.Logf("Testing write query with 1 second timeout...")

		// 创建只有1秒超时的上下文
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer writeCancel()

		// 使用更复杂的表结构创建语句
		callToolRequest := mcp.CallToolRequest{}
		callToolRequest.Params.Name = toolName
		callToolRequest.Params.Arguments = map[string]interface{}{
			"sql": "CREATE TABLE temp (ts TIMESTAMP NOT NULL, value FLOAT) TAGS (sensor_id INT NOT NULL) PRIMARY TAGS (sensor_id) RETENTIONS 20D PARTITION INTERVAL 5D;",
		}

		startTime := time.Now()
		result, err := c.CallTool(writeCtx, callToolRequest)
		duration := time.Since(startTime)

		if err != nil {
			testSummary.QueryError++
			if err == context.DeadlineExceeded || duration >= 1*time.Second {
				t.Errorf("❌ 表创建查询超时 (>1秒): %v，用时: %v", err, duration)
			} else {
				t.Errorf("❌ 表创建查询失败: %v", err)
			}
			// 不让测试停止，继续执行其他测试
		} else {
			testSummary.QuerySuccess++
			t.Logf("✅ 表创建查询成功完成，用时: %v", duration)
			for i, content := range result.Content {
				t.Logf("Content %d: %+v", i, content)
			}

			// 尝试删除刚创建的表
			t.Log("Dropping test table...")
			dropCtx, dropCancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer dropCancel()

			dropRequest := mcp.CallToolRequest{}
			dropRequest.Params.Name = toolName
			dropRequest.Params.Arguments = map[string]interface{}{
				"sql": "DROP TABLE IF EXISTS temp;",
			}

			dropStartTime := time.Now()
			dropResult, dropErr := c.CallTool(dropCtx, dropRequest)
			dropDuration := time.Since(dropStartTime)

			if dropErr != nil {
				testSummary.QueryError++
				if dropErr == context.DeadlineExceeded || dropDuration >= 1*time.Second {
					t.Errorf("❌ 表删除查询超时 (>1秒): %v，用时: %v", dropErr, dropDuration)
				} else {
					t.Errorf("❌ 表删除查询失败: %v", dropErr)
				}
			} else {
				testSummary.QuerySuccess++
				t.Logf("✅ 表删除查询成功完成，用时: %v", dropDuration)
				for i, content := range dropResult.Content {
					t.Logf("Drop result %d: %+v", i, content)
				}
			}
		}
	}

	// Test completion (if supported)
	// t.Log("Testing completion...")
	// // 使用短超时调用完成接口
	// compCtx, compCancel := context.WithTimeout(context.Background(), 5*time.Second)
	// defer compCancel()

	// completeRequest := mcp.CompleteRequest{}
	// completeRequest.Params.Argument.Name = "query"
	// completeRequest.Params.Argument.Value = "SELECT * FROM "

	// completion, err := c.Complete(compCtx, completeRequest)
	// if err != nil {
	// 	t.Errorf("❌ Completion failed: %v", err)
	// 	testSummary.CompletionError++
	// } else {
	// 	t.Logf("✅ Got completion suggestions: %+v", completion)
	// 	testSummary.CompletionSuccess++
	// }

	// 打印测试结果摘要
	t.Logf("\n==== 测试结果摘要 ====")
	t.Logf("资源测试: %d 成功, %d 失败", testSummary.ResourceSuccess, testSummary.ResourceError)
	t.Logf("提示测试: %d 成功, %d 失败", testSummary.PromptSuccess, testSummary.PromptError)
	t.Logf("查询测试: %d 成功, %d 失败", testSummary.QuerySuccess, testSummary.QueryError)
	t.Logf("完成测试: %d 成功, %d 失败", testSummary.CompletionSuccess, testSummary.CompletionError)

	totalErrors := testSummary.ResourceError + testSummary.PromptError + testSummary.QueryError + testSummary.CompletionError
	if totalErrors > 0 {
		// 如果有任何错误，确保测试被标记为失败
		t.Errorf("❌ 集成测试完成，但发现 %d 个错误", totalErrors)
	} else {
		t.Logf("✅ 集成测试全部通过")
	}
}

// 辅助函数：检查工具列表中是否包含指定工具
func containsTool(tools []mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// Helper function to run the test with proper timeout
func TestMain(m *testing.M) {
	// Get connection string from environment variable
	connectionString = os.Getenv("CONNECTION_STRING")
	if connectionString == "" {
		// Fallback to default connection string if not provided
		connectionString = "postgresql://kwdb:Kaiwudb%40123@localhost:26257/db_shig"
	}

	// 使用更长的全局超时（3分钟）
	timeout := time.After(3 * time.Minute)
	done := make(chan bool)

	go func() {
		result := m.Run()
		done <- true
		os.Exit(result)
	}()

	select {
	case <-timeout:
		fmt.Println("Tests timed out after 3 minutes")
		os.Exit(1)
	case <-done:
		// Tests completed within the timeout
	}
}
