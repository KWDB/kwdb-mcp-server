package ctxutil

import "context"

// ctxKey 用于在 context 中存储额外的 HTTP 相关信息，避免与其他包的 key 冲突。
type ctxKey string

const (
	// DatabaseURIKey 在 HTTP 模式下存放当前请求使用的数据库连接串（X-Database-URI）。
	DatabaseURIKey ctxKey = "kwdb-mcp-database-uri"
)

// WithDatabaseURI 将数据库 URI 写入 context。
func WithDatabaseURI(ctx context.Context, uri string) context.Context {
	if uri == "" {
		return ctx
	}
	return context.WithValue(ctx, DatabaseURIKey, uri)
}

// GetDatabaseURI 从 context 中读取数据库 URI；不存在时返回空字符串。
func GetDatabaseURI(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(DatabaseURIKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
