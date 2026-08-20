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
    "subscriptions": ["turn_start", "turn_end"],
    "seams": ["prompt_builder"]
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
- `seams` - declares which capability seams the extension provides.
  Valid values: `"prompt_builder"` (implements `prompt/build`),
  `"compactor"` (implements `compaction/messages`), `"provider"`
  (implements `provider/stream` and related methods). The host
  matches these against `plugins` config to wire the right extension.

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
| `session_new`       | New session created (`/new`)            |
| `session_resume`    | Session resumed (`/resume`)             |
| `session_fork`      | Session branched (`/branch`)            |
| `session_label`     | Session label changed (`/label`)        |

### shutdown

Sent before killing the process. No response expected.

```json
{ "jsonrpc": "2.0", "method": "shutdown" }
```

## Customization hooks

Extensions can customize the agent's system prompt, compaction, and
branch summarization. These are request/response methods (they carry an
`id` and the host waits for the result).

### prompt/guidelines

Returns guideline lines appended to the system prompt under
`## Extension Guidelines`. Called once per agent run.

Request:

```json
{ "jsonrpc": "2.0", "id": 10, "method": "prompt/guidelines", "params": null }
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "guidelines": [
      "Always use conventional commits",
      "Run tests before reporting done"
    ]
  }
}
```

### prompt/build

Fully replaces the system prompt. If no extension returns `ok: true`,
the host uses the default `PromptBuilder`. Called once per agent run,
before the conversation loop starts.

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "prompt/build",
  "params": { "cwd": "/home/user/project", "messages": [] }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": { "prompt": "You are a coding agent...", "ok": true }
}
```

### compaction/customize

Returns a custom compaction summary string. The host assembles the
compacted message list from the summary. If no extension returns
`ok: true`, the host uses the default provider-based summarization.

Request:

```json
{ "jsonrpc": "2.0", "id": 12, "method": "compaction/customize", "params": { "messages": [...], "focus": "" } }
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "result": { "summary": "User worked on auth refactor...", "ok": true }
}
```

### compaction/messages

Fully replaces the compaction. The extension returns the complete
compacted message list. If no extension returns `ok: true`, the host
falls back to `compaction/customize`, then the default compaction.

Request:

```json
{ "jsonrpc": "2.0", "id": 13, "method": "compaction/messages", "params": { "messages": [...] } }
```

Response:

```json
{ "jsonrpc": "2.0", "id": 13, "result": { "messages": [...], "ok": true } }
```

### branch/summary

Returns a custom branch summary. Called when the user does `/branch` and
the inherited history is long. If no extension returns `ok: true`, the
host uses a heuristic trim.

Request:

```json
{ "jsonrpc": "2.0", "id": 14, "method": "branch/summary", "params": { "messages": [...] } }
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 14,
  "result": {
    "summary": "Parent session implemented image input...",
    "ok": true
  }
}
```

## Provider seam

An extension declaring the `"provider"` seam implements the LLM provider
contract. The core-provider extension (`core-provider/main.go`) contains
the Ollama, OpenAI, and Anthropic provider implementations.

### Provider configuration

The host sets `OMEGA_PROVIDER_TYPE`, `OMEGA_PROVIDER_MODEL`, and
`OMEGA_PROVIDER_HOST` environment variables before spawning the
extension. The extension reads the appropriate API key from
`OLLAMA_API_KEY`, `OPENAI_API_KEY`, or `ANTHROPIC_API_KEY` during
`initialize`.

### provider/stream (streaming RPC)

Unlike other methods, `provider/stream` uses a streaming RPC pattern:
the extension sends `stream_event` notifications (no ID) as chunks
arrive, then sends a final response (with ID) when the stream completes.

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "provider/stream",
  "params": {
    "messages": [{ "role": "user", "content": "hello" }],
    "tools": [{ "name": "shell.run", "description": "...", "parameters": {} }]
  }
}
```

Notifications (no ID, sent before the final response):

```json
{"jsonrpc": "2.0", "method": "stream_event", "params": {"type": "response_chunk", "content": "Hi"}}
{"jsonrpc": "2.0", "method": "stream_event", "params": {"type": "thinking_chunk", "content": "..."}}
{"jsonrpc": "2.0", "method": "stream_event", "params": {"type": "tool_call", "tool_call": {"id": "call_1", "name": "shell.run", "arguments": {}}}}
```

Final response (with ID):

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "finish_reason": "stop",
    "prompt_eval_count": 15,
    "eval_count": 61
  }
}
```

### Other provider methods

- `provider/model_name` - returns `{"model": "glm-5.2"}`
- `provider/list_models` - returns `{"models": ["llama3", "glm-5.2"]}`
- `provider/set_thinking` - sets thinking level: `{"level": "medium"}`
- `provider/set_model` - changes model at runtime: `{"model": "llama3"}`

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

- Name extension binaries with a `.exe` suffix (e.g. `ollama-web.exe`).
- If a binary has no extension and a `.exe` variant exists in the same
  directory, omega uses the `.exe` variant automatically.
- `filepath.WalkDir` returns relative paths with backslashes, which
  `exec.Command` cannot resolve on Windows. omega converts paths to
  absolute before spawning.
- Shell scripts (`.sh`) are run through `bash` if available. Files with
  a `#!` line use the declared interpreter.

## Example

The `ollama-web/` directory contains a web extension that provides
`web.search` and `web.fetch` tools using the Ollama Cloud API. It is a
complete reference implementation - see `ollama-web/main.go`.

```bash
# Build the web search extension
go build -o ollama-web/ollama-web.exe ./ollama-web/
```

The example directory is at `bin/extensions/ollama-web/` relative to the
repo root.
