package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

	t.Log("=== 开始并发测试 ===")
	runConcurrencyTests(t, c, ctx, tools.Tools, resources.Resources)

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

// runConcurrencyTests 运行并发测试以验证连接池的并发访问能力
func runConcurrencyTests(t *testing.T, c *client.Client, ctx context.Context, tools []mcp.Tool, resources []mcp.Resource) {
	// 并发测试配置
	const (
		concurrentRequests = 10              // 并发请求数
		testDuration       = 5 * time.Second // 测试持续时间（调整为5秒以便快速验证）
	)

	// 测试1: 并发调用读查询工具
	if containsToolHTTP(tools, "read-query") {
		t.Logf("测试1: 并发读查询 (%d个并发请求)", concurrentRequests)
		testConcurrentReadQueries(t, c, concurrentRequests)
	}

	// 测试2: 并发访问资源
	if len(resources) > 0 {
		t.Logf("测试2: 并发访问资源 (%d个并发请求)", concurrentRequests)
		testConcurrentResourceAccess(t, c, resources, concurrentRequests)
	}

	// 测试3: 混合并发访问（tools + resources）
	if containsToolHTTP(tools, "read-query") && len(resources) > 0 {
		t.Logf("测试3: 混合并发访问 (%d个并发请求)", concurrentRequests)
		testMixedConcurrentAccess(t, c, resources, concurrentRequests)
	}

	// 测试4: 高负载并发测试
	if containsToolHTTP(tools, "read-query") {
		t.Logf("测试4: 高负载并发测试 (%v持续压力)", testDuration)
		testHighLoadConcurrency(t, c, testDuration)
	}

	t.Log("=== 并发测试完成 ===")
}

// testConcurrentReadQueries 测试并发读查询
func testConcurrentReadQueries(t *testing.T, c *client.Client, concurrentRequests int) {
	var wg sync.WaitGroup
	results := make(chan TestResult, concurrentRequests)
	startTime := time.Now()

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			queryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			callToolRequest := mcp.CallToolRequest{}
			callToolRequest.Params.Name = "read-query"
			callToolRequest.Params.Arguments = map[string]interface{}{
				"sql": fmt.Sprintf("SELECT %d as request_id, 'concurrent_test' as test_type", id),
			}

			reqStart := time.Now()
			_, err := c.CallTool(queryCtx, callToolRequest)
			duration := time.Since(reqStart)

			results <- TestResult{
				ID:       id,
				Success:  err == nil,
				Duration: duration,
				Error:    err,
			}
		}(i)
	}

	wg.Wait()
	close(results)
	totalDuration := time.Since(startTime)

	// 分析结果
	successful := 0
	failed := 0
	var totalTime time.Duration
	var maxTime time.Duration
	var minTime time.Duration = time.Hour

	for result := range results {
		if result.Success {
			successful++
		} else {
			failed++
			t.Logf("  请求 %d 失败: %v", result.ID, result.Error)
		}
		totalTime += result.Duration
		if result.Duration > maxTime {
			maxTime = result.Duration
		}
		if result.Duration < minTime {
			minTime = result.Duration
		}
	}

	avgTime := totalTime / time.Duration(concurrentRequests)

	t.Logf("  并发读查询结果: %d成功, %d失败", successful, failed)
	t.Logf("  总耗时: %v, 平均响应时间: %v", totalDuration, avgTime)
	t.Logf("  最快: %v, 最慢: %v", minTime, maxTime)

	if failed > 0 {
		t.Errorf("并发读查询测试失败: %d/%d 请求失败", failed, concurrentRequests)
	}
}

// testConcurrentResourceAccess 测试并发资源访问
func testConcurrentResourceAccess(t *testing.T, c *client.Client, resources []mcp.Resource, concurrentRequests int) {
	var wg sync.WaitGroup
	results := make(chan TestResult, concurrentRequests)
	startTime := time.Now()

	// 选择非模板资源进行测试
	nonTemplateResources := []mcp.Resource{}
	for _, resource := range resources {
		if !strings.Contains(resource.URI, "{") || !strings.Contains(resource.URI, "}") {
			nonTemplateResources = append(nonTemplateResources, resource)
		}
	}

	if len(nonTemplateResources) == 0 {
		t.Logf("  跳过资源并发测试 - 没有找到非模板资源")
		return
	}

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			resourceCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// 轮流访问不同的资源
			resource := nonTemplateResources[id%len(nonTemplateResources)]

			readRequest := mcp.ReadResourceRequest{}
			readRequest.Params.URI = resource.URI

			reqStart := time.Now()
			_, err := c.ReadResource(resourceCtx, readRequest)
			duration := time.Since(reqStart)

			results <- TestResult{
				ID:       id,
				Success:  err == nil,
				Duration: duration,
				Error:    err,
			}
		}(i)
	}

	wg.Wait()
	close(results)
	totalDuration := time.Since(startTime)

	// 分析结果
	successful := 0
	failed := 0
	var totalTime time.Duration

	for result := range results {
		if result.Success {
			successful++
		} else {
			failed++
			t.Logf("  资源访问 %d 失败: %v", result.ID, result.Error)
		}
		totalTime += result.Duration
	}

	avgTime := totalTime / time.Duration(concurrentRequests)

	t.Logf("  并发资源访问结果: %d成功, %d失败", successful, failed)
	t.Logf("  总耗时: %v, 平均响应时间: %v", totalDuration, avgTime)

	if failed > 0 {
		t.Errorf("并发资源访问测试失败: %d/%d 请求失败", failed, concurrentRequests)
	}
}

// testMixedConcurrentAccess 测试混合并发访问
func testMixedConcurrentAccess(t *testing.T, c *client.Client, resources []mcp.Resource, concurrentRequests int) {
	var wg sync.WaitGroup
	results := make(chan TestResult, concurrentRequests)
	startTime := time.Now()

	// 选择非模板资源
	nonTemplateResources := []mcp.Resource{}
	for _, resource := range resources {
		if !strings.Contains(resource.URI, "{") || !strings.Contains(resource.URI, "}") {
			nonTemplateResources = append(nonTemplateResources, resource)
		}
	}

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			reqStart := time.Now()
			var err error

			// 交替执行工具调用和资源访问
			if id%2 == 0 {
				// 调用工具
				callToolRequest := mcp.CallToolRequest{}
				callToolRequest.Params.Name = "read-query"
				callToolRequest.Params.Arguments = map[string]interface{}{
					"sql": fmt.Sprintf("SELECT %d as mixed_test_id", id),
				}
				_, err = c.CallTool(reqCtx, callToolRequest)
			} else if len(nonTemplateResources) > 0 {
				// 访问资源
				resource := nonTemplateResources[id%len(nonTemplateResources)]
				readRequest := mcp.ReadResourceRequest{}
				readRequest.Params.URI = resource.URI
				_, err = c.ReadResource(reqCtx, readRequest)
			}

			duration := time.Since(reqStart)

			results <- TestResult{
				ID:       id,
				Success:  err == nil,
				Duration: duration,
				Error:    err,
			}
		}(i)
	}

	wg.Wait()
	close(results)
	totalDuration := time.Since(startTime)

	// 分析结果
	successful := 0
	failed := 0

	for result := range results {
		if result.Success {
			successful++
		} else {
			failed++
			t.Logf("  混合请求 %d 失败: %v", result.ID, result.Error)
		}
	}

	t.Logf("  混合并发访问结果: %d成功, %d失败", successful, failed)
	t.Logf("  总耗时: %v", totalDuration)

	if failed > 0 {
		t.Errorf("混合并发访问测试失败: %d/%d 请求失败", failed, concurrentRequests)
	}
}

// testHighLoadConcurrency 测试高负载并发
func testHighLoadConcurrency(t *testing.T, c *client.Client, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	requestCount := int64(0)
	successCount := int64(0)
	errorCount := int64(0)

	// 启动多个工作者goroutine
	const numWorkers = 20
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// 执行查询
					queryCtx, queryCancel := context.WithTimeout(context.Background(), 5*time.Second)
					callToolRequest := mcp.CallToolRequest{}
					callToolRequest.Params.Name = "read-query"
					callToolRequest.Params.Arguments = map[string]interface{}{
						"sql": fmt.Sprintf("SELECT %d as worker_id, NOW() as timestamp", workerID),
					}

					_, err := c.CallTool(queryCtx, callToolRequest)
					queryCancel()

					// 原子更新计数器
					atomic.AddInt64(&requestCount, 1)
					if err == nil {
						atomic.AddInt64(&successCount, 1)
					} else {
						atomic.AddInt64(&errorCount, 1)
					}

					// 小延迟避免过于激进
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// 等待测试完成
	wg.Wait()

	total := atomic.LoadInt64(&requestCount)
	success := atomic.LoadInt64(&successCount)
	errors := atomic.LoadInt64(&errorCount)

	qps := float64(total) / duration.Seconds()
	successRate := float64(success) / float64(total) * 100

	t.Logf("  高负载测试结果:")
	t.Logf("    总请求数: %d", total)
	t.Logf("    成功请求: %d", success)
	t.Logf("    失败请求: %d", errors)
	t.Logf("    QPS: %.2f", qps)
	t.Logf("    成功率: %.2f%%", successRate)

	if successRate < 95.0 {
		t.Errorf("高负载测试成功率过低: %.2f%% < 95%%", successRate)
	}
}

// TestResult 测试结果结构
type TestResult struct {
	ID       int
	Success  bool
	Duration time.Duration
	Error    error
}

func containsToolHTTP(tools []mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
