package tools

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestResolveDBTarget_WithHeader(t *testing.T) {
	// X-Database-URI 优先：有 header 时无论默认池是否初始化，都使用 header 指定库
	uri, useDefault, missing := resolveDBTarget("postgresql://host/db1", false)
	if uri != "postgresql://host/db1" || useDefault || missing {
		t.Errorf("with header and no default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}

	uri, useDefault, missing = resolveDBTarget("postgresql://host/db1", true)
	if uri != "postgresql://host/db1" || useDefault || missing {
		t.Errorf("with header and default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}
}

func TestResolveDBTarget_NoHeaderWithDefaultPool(t *testing.T) {
	// 兼容模式：无 header 但默认池已初始化 → 使用默认池
	uri, useDefault, missing := resolveDBTarget("", true)
	if uri != "" || !useDefault || missing {
		t.Errorf("no header with default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}
}

func TestResolveDBTarget_NoHeaderNoDefaultPool(t *testing.T) {
	// 无状态模式：无 header 且无默认池 → 应返回 missing header 错误
	uri, useDefault, missing := resolveDBTarget("", false)
	if uri != "" || useDefault || !missing {
		t.Errorf("no header no default pool: got uri=%q useDefault=%v missing=%v", uri, useDefault, missing)
	}
}

func TestSQLToolsExposeMinimalStableJSONSchema(t *testing.T) {
	s := server.NewMCPServer("test", "1.0.0")
	RegisterTools(s)

	registered := s.ListTools()
	for _, toolName := range []string{"read-query", "write-query"} {
		entry, ok := registered[toolName]
		if !ok {
			t.Fatalf("tool %q not registered", toolName)
		}

		raw, err := json.Marshal(entry.Tool)
		if err != nil {
			t.Fatalf("marshal %q tool: %v", toolName, err)
		}

		var payload struct {
			InputSchema  map[string]any `json:"inputSchema"`
			OutputSchema map[string]any `json:"outputSchema"`
			Annotations  map[string]any `json:"annotations"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("unmarshal %q tool payload: %v", toolName, err)
		}

		if payload.InputSchema["type"] != "object" {
			t.Fatalf("%q input schema type = %v, want object", toolName, payload.InputSchema["type"])
		}

		properties, ok := payload.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%q input schema properties missing or invalid: %#v", toolName, payload.InputSchema["properties"])
		}
		sqlProperty, ok := properties["sql"].(map[string]any)
		if !ok {
			t.Fatalf("%q sql property missing or invalid: %#v", toolName, properties["sql"])
		}
		if sqlProperty["type"] != "string" {
			t.Fatalf("%q sql property type = %v, want string", toolName, sqlProperty["type"])
		}
		if sqlProperty["description"] == nil || sqlProperty["description"] == "" {
			t.Fatalf("%q sql property description missing", toolName)
		}

		required, ok := payload.InputSchema["required"].([]any)
		if !ok || len(required) != 1 || required[0] != "sql" {
			t.Fatalf("%q required = %#v, want [\"sql\"]", toolName, payload.InputSchema["required"])
		}

		if payload.OutputSchema["type"] != "object" {
			t.Fatalf("%q output schema type = %v, want object", toolName, payload.OutputSchema["type"])
		}

		if len(payload.Annotations) != 0 {
			t.Fatalf("%q annotations = %#v, want empty object for client compatibility", toolName, payload.Annotations)
		}
	}
}
