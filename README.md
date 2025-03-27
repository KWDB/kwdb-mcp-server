# KWDB MCP Server

A MCP-compatible server for interacting with KWDB (KaiwuDB) databases.

[中文版](README_zh.md)

## Overview

KWDB MCP Server provides a set of tools and resources for interacting with KWDB (KaiwuDB) databases through the MCP protocol. It supports both read and write operations, allowing you to query data, modify data, and perform DDL operations.

## Features

- **Read Operations**: Execute SELECT, SHOW, EXPLAIN, and other read-only queries
- **Write Operations**: Execute INSERT, UPDATE, DELETE, and DDL operations like CREATE, DROP, ALTER
- **Database Information**: Get information about the database, including tables and their schemas
- **Syntax Guide**: Access a comprehensive syntax guide for KWDB (KaiwuDB) via prompts
- **Automatic LIMIT**: Prevents large result sets by automatically adding LIMIT 20 to SELECT queries without a LIMIT clause

## Installation

### Prerequisites

- Go 1.23 or higher
- Access to a KWDB (KaiwuDB) database
- VSCode with AI extensions that support MCP (like Cline)

### Installation Steps

1. Clone the repository:
   ```bash
   git clone https://gitee.com/kwdb/mcp-kwdb-server-go.git
   cd mcp-kwdb-server-go
   ```

2. Install dependencies:
   ```bash
   make deps
   ```

3. Build the application:
   ```bash
   make build
   ```

## Usage

Run the server with a PostgreSQL connection string:

```bash
./bin/kwdb-mcp-server "postgresql://username:password@hostname:port/database?sslmode=disable"
```

Or using the Makefile:

```bash
CONNECTION_STRING="postgresql://username:password@hostname:port/database?sslmode=disable" make run
```

### Transport Modes

The server supports two transport modes:

#### Standard I/O Mode (Default)

This is the default mode, which uses standard input/output for communication:

```bash
./bin/kwdb-mcp-server "postgresql://username:password@hostname:port/database?sslmode=disable"
```

#### SSE Mode (Server-Sent Events over HTTP)

For remote access, you can run the server in SSE mode. Note that you still need to provide the database connection string as the last argument:

```bash
./bin/kwdb-mcp-server -t sse -addr ":8080" -base-url "http://localhost:8080" "postgresql://username:password@hostname:port/database?sslmode=disable"
```

Or using the Makefile:

```bash
CONNECTION_STRING="postgresql://username:password@hostname:port/database?sslmode=disable" ADDR=":8080" BASE_URL="http://localhost:8080" make run-sse
```

Options:
- `-t` or `-transport`: Transport type (`stdio` or `sse`)
- `-addr`: Address to listen on for SSE mode (default: `:8080`)
- `-base-url`: Base URL for SSE mode (default: `http://localhost:8080`)

## Integration with LLM Agents

KWDB Server is designed to work with any LLM Agent that supports the MCP protocol. Below is an example using Cline, but similar steps apply to other MCP-compatible agents.

### Example: Integration with Cline

1. Ensure you have Cline installed in VSCode

2. Add the MCP server configuration to your VSCode settings. Open VSCode Cline > MCP Servers > Installed > Configure MCP Servers and add:

#### For Standard I/O Mode

```json
"mcpServers": {
  "kwdb-server": {
    "command": "/path/to/bin/kwdb-mcp-server",
    "args": [
      "postgresql://username:password@host:port/database"
    ],
    "disabled": false,
    "autoApprove": []
  }
}
```

#### For SSE Mode

First, start the server in SSE mode:

```bash
./bin/kwdb-mcp-server -t sse -addr ":8080" -base-url "http://localhost:8080" "postgresql://username:password@hostname:port/database?sslmode=disable"
```

Then, configure Cline to connect to the running server:

```json
"mcpServers": {
  "kwdb-server-sse": {
    "url": "http://localhost:8080",
    "disabled": false,
    "autoApprove": []
  }
}
```

3. Restart the server from VSCode Cline > MCP Servers > Installed > kwdb-server > Restart Server

### Integration with Other MCP-Compatible Agents

For other LLM Agents that support the MCP protocol, refer to their specific documentation for how to configure MCP servers. The key requirements are:

- For Standard I/O Mode: The ability to execute the KWDB Server binary with command-line arguments
- For SSE Mode: The ability to connect to an HTTP endpoint that implements the MCP protocol

## Tools

The server provides the following tools:

### read-query

Execute SELECT, SHOW, EXPLAIN, and other read-only queries. SELECT queries without a LIMIT clause will automatically have LIMIT 20 added to prevent large result sets.

Example:
```sql
SELECT * FROM users LIMIT 10;
SHOW TABLES;
EXPLAIN ANALYZE SELECT * FROM orders WHERE user_id = 1;
```

### write-query

Execute data modification queries including DML and DDL operations.

Example:
```sql
INSERT INTO users (name, email) VALUES ('John Doe', 'john@example.com');
UPDATE users SET email = 'new-email@example.com' WHERE id = 1;
DELETE FROM users WHERE id = 1;
CREATE TABLE products (id SERIAL PRIMARY KEY, name TEXT, price DECIMAL);
ALTER TABLE products ADD COLUMN description TEXT;
DROP TABLE products;
```

## Prompts

The server provides the following prompts:
| Prompt Name | Description |
|------------|-------------|
| db_description | A comprehensive description of KWDB (KaiwuDB) database, including its capabilities, features, and use cases. |
| syntax_guide | A comprehensive syntax guide for KWDB (KaiwuDB), including examples of common queries and best practices. |
| cluster_management | A comprehensive guide for managing KWDB clusters, including node management, load balancing, and monitoring. |
| data_migration | Guide for migrating data to and from KWDB, including import/export methods and best practices. |
| installation | Step-by-step guide for installing and deploying KWDB in various environments. |
| performance_tuning | Guide for optimizing KWDB performance, including query optimization, indexing strategies, and system-level tuning. |
| troubleshooting | Guide for diagnosing and resolving common KWDB issues and errors. |
| backup_restore | Comprehensive guide for backing up and restoring KWDB databases, including strategies, tools, and best practices. |
| dba_template | Template and guidelines for prompts writing. |


## Resources

The server provides the following resources:

- `kwdb://product_info`: Information about product, including version and capabilities
- `kwdb://db_info/{database_name}`: Information about a specific database, including engine type, comment, and tables
- `kwdb://table/{table_name}`: Schema of a specific table, including columns and example queries

## API Response Format

All API responses follow a consistent JSON structure:

```json
{
  "status": "success",  // or "error"
  "type": "query_result",  // response type
  "data": { ... },  // response data
  "error": null  // error information, null on success
}
```

### Success Response Example

```json
{
  "status": "success",
  "type": "query_result",
  "data": {
    "result_type": "table",
    "columns": ["id", "name", "email"],
    "rows": [
      {"id": 1, "name": "John", "email": "john@example.com"},
      {"id": 2, "name": "Jane", "email": "jane@example.com"}
    ],
    "metadata": {
      "affected_rows": 0,
      "row_count": 2,
      "query": "SELECT * FROM users LIMIT 2"
    }
  },
  "error": null
}
```

### Error Response Example

```json
{
  "status": "error",
  "type": "error_response",
  "data": null,
  "error": {
    "code": "SYNTAX_ERROR",
    "message": "Query error: syntax error at or near \"SLECT\"",
    "details": "syntax error at or near \"SLECT\"",
    "query": "SLECT * FROM users"
  }
}
```

## Project Structure

```
mcp-kwdb-server-go/
├── cmd/
│   └── kwdb-mcp-server/
│       └── main.go           # Main application entry point
├── pkg/
│   ├── db/
│   │   └── db.go             # Database operations
│   ├── prompts/
│   │   ├── prompts.go        # MCP Prompts
│   │   └── docs/         # Markdown files for prompts
│   │       ├── ReadExamples.md     # Read query examples
│   │       ├── WriteExamples.md    # Write query examples
│   │       ├── DBDescription.md    # Database description
│   │       ├── SyntaxGuide.md      # SQL syntax guide
│   │       ├── ClusterManagementGuide.md # Cluster management guide
│   │       ├── DataMigrationGuide.md    # Data migration guide
│   │       ├── InstallationGuide.md      # Installation guide
│   │       ├── PerformanceTuningGuide.md # Performance tuning guide
│   │       ├── TroubleShootingGuide.md   # Troubleshooting guide
│   │       ├── BackupRestoreGuide.md     # Backup and restore guide
│   │       └── DBATemplate.md            # Database administration template
│   ├── resources/
│   │   └── resources.go      # MCP resources
│   ├── server/
│   │   └── server.go         # Server setup
│   └── tools/
│       └── tools.go          # MCP tools
├── Makefile                  # Build and run commands
└── README.md                 # This file
```

## Maintaining Prompt Content

The prompt content is stored in Markdown files located in the `pkg/prompts/docs/` directory. These files are embedded into the binary at compile time using Go's `embed` package. 

### Markdown Files Structure

The following Markdown files are used for prompts:

- `pkg/prompts/docs/ReadExamples.md`: Contains examples of read queries (SELECT statements)
- `pkg/prompts/docs/WriteExamples.md`: Contains examples of write queries (INSERT, UPDATE, DELETE, CREATE, ALTER)
- `pkg/prompts/docs/DBDescription.md`: Contains the database description
- `pkg/prompts/docs/SyntaxGuide.md`: Contains the SQL syntax guide
- `pkg/prompts/docs/ClusterManagementGuide.md`: Contains the cluster management guide
- `pkg/prompts/docs/DataMigrationGuide.md`: Contains the data migration guide
- `pkg/prompts/docs/InstallationGuide.md`: Contains the installation guide
- `pkg/prompts/docs/PerformanceTuningGuide.md`: Contains the performance tuning guide
- `pkg/prompts/docs/TroubleShootingGuide.md`: Contains the troubleshooting guide
- `pkg/prompts/docs/BackupRestoreGuide.md`: Contains the backup and restore guide
- `pkg/prompts/docs/DBATemplate.md`: Contains the database administration template

### How to Modify Prompt Content

To modify the prompt content:

1. Edit the appropriate Markdown file in the `pkg/prompts/docs/` directory.
2. Rebuild the application using `make build`.
3. The new content will be embedded in the binary.

### Adding New Use Case Prompts

To add a new use case prompt:

1. Create a new Markdown file in `pkg/prompts/docs/`, e.g., `new_usecase.md`
2. Add the variable and loading code in `pkg/prompts/prompts.go`
3. Create a registration function for the new prompt
4. Add the registration function call to `registerUseCasePrompts()`
5. Update the README.md to document the new prompt

Refer to the comments in `pkg/prompts/prompts.go` for detailed instructions.

## Security

KWDB Server implements the following security measures:

- Separate tools for read and write operations
- Validation of queries to ensure they match the expected operation type
- Clear error messages for unauthorized operations

## Future Enhancements

- [ ] **Query history**: Implement query history functionality
- [x] **Remote mode**: Support connecting to remote MCP Server
- [x] **Improved optimization suggestions**: Enhance query optimization recommendations
- [ ] **Metrics resource**: Add database metrics information

## Troubleshooting

If you encounter issues:

1. Verify the database connection string is correct
2. Ensure the database server is accessible from your machine
3. Check that the database user has sufficient permissions
4. Verify your Agent MCP server configuration
5. Check for existing kwdb-mcp-server processes that might be blocking the port

### SSE Mode Specific Issues

1. **Connection refused**: Make sure the server is running and listening on the specified address
2. **CORS errors**: If accessing from a web browser, ensure the server's base URL matches the URL you're connecting from
3. **Network issues**: Check if firewalls or network configurations are blocking the connection
4. **Database connectivity**: The server still needs to connect to the database, so ensure the database is accessible from the server's location

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

This project is licensed under the Mulan PSL v2 License

## Acknowledgements

- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) - MCP Go server framework
- [lib/pq](https://github.com/lib/pq) - PostgreSQL Go driver

