package version

// Version is the current version of KWDB MCP Server.
//
// v3.2.0:
//   - Replace the deprecated `query-metrics-history` tool with two
//     new inspection tools: `query-metrics` (closed-set catalog of 32
//     metrics against the admin `/ts/query` API) and `query-slow-sql`
//     (top-N statements against `/_status/statements`). Both derive
//     Basic Auth from the existing DB URL and auto-derive the admin
//     endpoint URL from the same connection string, removing the
//     `--admin-base-url` startup flag.
//   - Tolerate the KaiwuDB admin API's quoted-string counter fields
//     (`count`, `firstAttemptCount`, `maxRetries`, ...) which some
//     clusters serialise as JSON strings.
const Version = "v3.2.0"
