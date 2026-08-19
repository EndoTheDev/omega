# MCP Bridge Extension

The MCP bridge is an omega extension that connects omega to MCP
(Model Context Protocol) servers. It supports both stdio and
Streamable HTTP transports, discovers tools from all connected
servers, and exposes them as omega extension tools. Tool calls from
omega are forwarded to the appropriate MCP server.

## How it works

```txt
omega agent ←→ mcp-bridge (stdio) ←→ MCP servers (stdio or HTTP)
```

1. omega starts the bridge as an extension process.
2. The bridge connects to all configured MCP servers, does the MCP
   initialize handshake, and calls `tools/list` on each.
3. The bridge responds to omega's `initialize` with all discovered
   tools, prefixed with the server name (e.g. `obsidian_read_file`).
4. When the LLM calls a tool, omega sends `tool_call` to the bridge,
   which forwards it to the right MCP server via `tools/call`.

## Config

The bridge reads its config from one of two sources:

### Environment variable

`MCP_SERVERS` as a JSON string (for environments where a config
file is impractical):

```bash
MCP_SERVERS='{"servers":[{"name":"obsidian","url":"http://127.0.0.1:27123/mcp/","headers":{"Authorization":"Bearer ***"}}]}'
```

### Config file

`~/.omega/mcp.yaml` (or `$OMEGA_HOME/mcp.yaml`). Also accepts
`mcp.json` as fallback:

```yaml
servers:
  - name: obsidian
    url: http://127.0.0.1:27123/mcp/
    headers:
      Authorization: Bearer xxx
  - name: filesystem
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /home/user
```

If `url` is set, the bridge uses Streamable HTTP transport. If
`command` is set, it uses stdio transport (spawns the process).

## Build

```bash
go build -o mcp-bridge.exe .
```

Place the binary in omega's extensions directory (`bin/extensions/`
or wherever `extensions.dir` points). Enable extensions in
`config.yaml`:

```yaml
extensions:
  enabled: true
  dir: extensions
```

## Tool naming

Tools are named `<server>_<tool>`. For example, the Obsidian MCP
server's `read_file` tool becomes `obsidian_read_file`. This avoids
collisions between MCP servers and omega's built-in tools.

Built-in omega tools always win on name conflict.

## Supported MCP features

- Tools (`tools/list`, `tools/call`)
- Stdio transport (spawn process, JSON-RPC over stdin/stdout)
- Streamable HTTP transport (POST JSON-RPC, parse JSON or SSE response)
- MCP protocol version `2025-11-25`
- Custom headers (for auth, e.g. `Authorization: Bearer`)

## Not supported

- MCP resources and prompts
- MCP sampling (server → LLM callback)
- `notifications/tools/list_changed` (tool list is fixed at startup)
