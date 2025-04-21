---
title: Troubleshooting
id: troubleshooting_en
---

# Troubleshooting

## Database Connection Failures

If you fail to connect to the KWDB database, troubleshoot the issue in the following aspects:

- Check whether the database connection string is correct.
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

## Error Codes

This table lists error codes related to the KWDB MCP Server.

| Error Code | Reason                  | Processing Strategy                      |
|------------|-------------------------|------------------------------------------|
| KWDB-4001  | Syntax error            | Return the specific syntax error.        |
| KWDB-4002  | Insufficient privileges | Stop the execution and return a warning. |
| KWDB-4003  | Connection timeout      | Retry the operation (up to 3 times).     |
| KWDB-4004  | Resources do not exist  | Return a 404 status code.                |
