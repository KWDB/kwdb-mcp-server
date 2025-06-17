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
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestHTTPServer(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping HTTP integration test in CI environment")
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	httpURL := baseURL + "/mcp"
	t.Logf("Connecting to HTTP server at: %s", httpURL)

	// 检查服务器是否正在运行
	t.Logf("Testing server availability with HTTP GET to /mcp endpoint...")
	resp, err := http.Get(httpURL)
	if err != nil {
		t.Logf("Warning: HTTP GET to endpoint %s failed: %v", httpURL, err)
		t.Logf("Server may not be running or endpoint is not accessible")
	} else {
		defer resp.Body.Close()
		t.Logf("HTTP endpoint Status: %d", resp.StatusCode)
	}

	t.Log("Initializing HTTP client...")
	httpTransport, err := transport.NewStreamableHTTP(httpURL)
	if err != nil {
		t.Fatalf("Failed to create HTTP transport: %v", err)
	}
	c := client.NewClient(httpTransport)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize the client
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "http-integration-test",
		Version: "1.0.0",
	}

	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initCancel()
	t.Logf("Sending initialize request with 10s timeout...")
	initResult, err := c.Initialize(initCtx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	t.Logf("Initialized with server: %s %s", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
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
	} else {
		t.Logf("Found %d resources", len(resources.Resources))
		if len(resources.Resources) > 0 {
			t.Log("Reading resources...")
			for i, resource := range resources.Resources {
				t.Logf("Resource %d: %s (%s)", i, resource.Name, resource.URI)
				if strings.Contains(resource.URI, "{") && strings.Contains(resource.URI, "}") {
					t.Logf("  Skipping template resource")
					continue
				}
				readRequest := mcp.ReadResourceRequest{}
				readRequest.Params.URI = resource.URI
				resourceCtx, resourceCancel := context.WithTimeout(context.Background(), 5*time.Second)
				result, err := c.ReadResource(resourceCtx, readRequest)
				if err != nil {
					t.Logf("  Failed to read resource: %v", err)
				} else {
					t.Logf("  Successfully read resource, got %d content items", len(result.Contents))
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

	// List prompts
	t.Log("Listing prompts...")
	listPromptsRequest := mcp.ListPromptsRequest{}
	prompts, err := c.ListPrompts(ctx, listPromptsRequest)
	if err != nil {
		t.Errorf("Failed to list prompts: %v", err)
	} else {
		t.Logf("Found %d prompts", len(prompts.Prompts))
		if len(prompts.Prompts) > 0 {
			t.Log("Getting prompts...")
			for i, prompt := range prompts.Prompts {
				t.Logf("Prompt %d: %s", i, prompt.Name)
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
				promptCtx, promptCancel := context.WithTimeout(context.Background(), 5*time.Second)
				result, err := c.GetPrompt(promptCtx, getRequest)
				if err != nil {
					t.Logf("  Failed to get prompt: %v", err)
				} else {
					t.Logf("  Successfully got prompt with %d messages", len(result.Messages))
					for j, msg := range result.Messages {
						t.Logf("    Message %d: Role=%s", j, msg.Role)
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
	if containsToolHTTP(tools.Tools, "read-query") {
		toolName := "read-query"
		t.Logf("Testing read query with 1 second timeout...")
		queryCtx, queryCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer queryCancel()
		callToolRequest := mcp.CallToolRequest{}
		callToolRequest.Params.Name = toolName
		callToolRequest.Params.Arguments = map[string]interface{}{
			"sql": "SELECT 1",
		}
		startTime := time.Now()
		_, err := c.CallTool(queryCtx, callToolRequest)
		queryDuration := time.Since(startTime)
		if err != nil {
			t.Logf("❌ Read query failed: %v (took %v)", err, queryDuration)
		} else {
			t.Logf("✅ Read query succeeded in %v", queryDuration)
			t.Logf("Result received")
		}
	}

	// 测试写查询工具
	if containsToolHTTP(tools.Tools, "write-query") {
		toolName := "write-query"
		t.Logf("Testing write query with 1 second timeout...")
		queryCtx, queryCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer queryCancel()
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
		} else {
			t.Logf("✅ Write query succeeded in %v", queryDuration)
			t.Logf("Result received")
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
	completeRequest.Params.Ref = "SELECT * FROM"
	_, err = c.Complete(ctx, completeRequest)
	if err != nil {
		t.Logf("Completion API not supported or failed: %v", err)
	} else {
		t.Logf("✅ Completion API supported!")
		t.Logf("Completion response received")
	}
}

func containsToolHTTP(tools []mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
