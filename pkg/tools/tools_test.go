package tools

import (
	"testing"
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
