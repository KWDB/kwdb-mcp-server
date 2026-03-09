---
title: Troubleshooting
id: troubleshooting_en
---

# Troubleshooting

## Database Connection Failures

If you fail to connect to the KWDB database, troubleshoot the issue in the following aspects:

- Check whether the database connection string is correct.
- Stateless multi-tenant mode: If the tool returns `missing X-Database-URI header` or connection-related errors and the server was started without a connection string, ensure that every `read-query` / `write-query` call sends the **`X-Database-URI`** request header with a valid PostgreSQL connection string.
- Check whether the user can access to the KWDB database.
- Check whether the user has appropriate privileges.
- Check whether the KWDB database address in the KWDB MCP Server configuration of the LLM Agent is correct.
- Check whether the existing `kwdb-mcp-server` process is blocked.

## SSE Mode Issues

| Issue                 | Processing Strategy                                                                                                          |
|-----------------------|------------------------------------------------------------------------------------------------------------------------------|
| Connection refused    | Ensure that the KWDB MCP Server is running and listening the specified IP address.                                           |
| CORS errors           | If you access the KWDB database from a Web browser, ensure that the KWDB MCP Server's base URL matches the KWDB database URL.|
| Network issues        | Check if firewalls or network configurations are blocking the connection.                                                    |
| Database connectivity | Ensure that the KWDB MCP Server can access the KWDB database.                                                                |


