# omega Extensions

Extensions are external processes that provide tools and event
subscriptions to omega. Each extension runs as a separate process and
communicates via JSON-RPC over stdin/stdout. A crash in one extension
does not affect others or the host.

## How it works

1. omega scans the configured extensions directory for executables.
2. Each executable is spawned as a child process.
3. omega sends an `initialize` request. The extension responds with its
   name, tools, and event subscriptions.
4. Extension tools are merged into the agent's tool registry. When the
   LLM calls an extension tool, omega sends a `tool_call` request and
   waits for the result.
5. Subscribed lifecycle events are forwarded as `event` notifications.
6. On exit, omega sends a `shutdown` notification and kills the process.

## Configuration

```yaml
extensions:
  enabled: true
  dir: extensions # relative to omega home, or absolute path
```

| Env var                    | Default             | Description                       |
| -------------------------- | ------------------- | --------------------------------- |
| `OMEGA_EXTENSIONS_ENABLED` | `false`             | Enable extension loading          |
| `OMEGA_EXTENSIONS_DIR`     | `<home>/extensions` | Directory to scan for executables |

## Protocol

All messages are single-line JSON objects terminated by `\n`. Requests
carry an `id` field; notifications omit it. Responses echo the request
`id`.

This is JSON-RPC 2.0 over stdio. The extension reads requests from
stdin and writes responses to stdout. Logs and diagnostics go to stderr.

### initialize

Sent once on startup. The extension declares its name, tools, and event
subscriptions.

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": { "protocol": "0.1" }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "name": "my-extension",
    "tools": [
      {
        "name": "my_tool",
        "description": "What the tool does",
        "parameters": {
          "type": "object",
          "properties": {
            "query": { "type": "string", "description": "Search query" }
          },
          "required": ["query"]
        }
      }
    ],
    "subscriptions": ["turn_start", "turn_end"]
  }
}
```

Fields:

- `name` - shown in the TUI `/extensions` listing.
- `tools` - merged into the agent's tool registry. Built-in tools win
  on name conflict. Each tool has a `name`, `description`, and
  `parameters` (JSON Schema object).
- `subscriptions` - filters which lifecycle events the extension
  receives. Omit or set to empty for none.

### tool_call

Sent when the LLM invokes an extension tool during the agent loop.

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tool_call",
  "params": { "tool": "my_tool", "args": { "query": "hello" } }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": { "content": "result text", "is_error": false }
}
```

Set `is_error: true` to mark the result as a tool error in the agent
history. The agent loop handles errors gracefully - they become
structured tool result messages, never panics.

### command

Sent when the user types a slash command registered by the extension.
Commands are optional - most extensions only need tools.

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "command",
  "params": { "name": "/my-cmd", "args": "optional args" }
}
```

Response:

```json
{ "jsonrpc": "2.0", "id": 3, "result": { "output": "text shown to the user" } }
```

Command output is displayed in the transcript. For search/fetch
operations, prefer registering tools instead - the LLM calls tools
during normal chat and synthesizes the results into a response.

### event

Notifications (no `id`) are best-effort. A stalled extension cannot
block the agent loop.

```json
{
  "jsonrpc": "2.0",
  "method": "event",
  "params": { "type": "turn_start", "data": { "turn": 1 } }
}
```

Available event types:

| Event               | When it fires                           |
| ------------------- | --------------------------------------- |
| `agent_start`       | Agent run begins                        |
| `turn_start`        | Each turn begins (before provider call) |
| `turn_end`          | Each turn ends (after tool execution)   |
| `assistant_message` | Assistant message emitted               |
| `tool_result`       | Tool execution completed                |
| `agent_end`         | Agent run finishes                      |

### shutdown

Sent before killing the process. No response expected.

```json
{ "jsonrpc": "2.0", "method": "shutdown" }
```

## API key passing

The host passes the provider's API key to extensions via the
`OLLAMA_API_KEY` environment variable. Extensions read it with
`os.Getenv("OLLAMA_API_KEY")` at startup. This allows extensions to
authenticate with APIs (Ollama Cloud, etc.) without a separate config
source.

## File discovery

omega uses `filepath.WalkDir` to recurse into subdirectories. This lets
each extension live in its own folder alongside its source code, config,
and binary. Files are skipped when:

- The entry is a directory (walked into, not spawned).
- The name starts with `.` (hidden).
- The extension is in a skip list: `.go`, `.md`, `.txt`, `.json`,
  `.yaml`, `.yml`, `.toml` (source/config/docs, never executables).
- Spawn fails (logged to stderr, skipped - one bad extension does not
  kill the manager).

## Windows notes

- Name extension binaries with a `.exe` suffix (e.g. `example.exe`).
- If a binary has no extension and a `.exe` variant exists in the same
  directory, omega uses the `.exe` variant automatically.
- `filepath.WalkDir` returns relative paths with backslashes, which
  `exec.Command` cannot resolve on Windows. omega converts paths to
  absolute before spawning.
- Shell scripts (`.sh`) are run through `bash` if available. Files with
  a `#!` line use the declared interpreter.

## Example

The `example/` directory contains a web extension that provides
`web.search` and `web.fetch` tools using the Ollama Cloud API. It is a
complete reference implementation - see `example/main.go`.

```bash
# Build the example extension
go build -o example/example.exe ./example/
```
