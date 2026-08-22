# Extensions

Extensions are external processes that provide tools and event
subscriptions to omega. Each extension runs as a separate process and
communicates via JSON-RPC over stdio. A crash in one extension does not
affect others or the host.

omega ships with these extensions:

| Extension       | Seam     | What                                                                          |
| --------------- | -------- | ----------------------------------------------------------------------------- |
| `core-prompt`   | prompt   | System prompt builder                                                         |
| `core-provider` | provider | LLM provider (Ollama, OpenAI, Anthropic)                                      |
| `core-store`    | store    | Session store (SQLite, FTS5 search, `sessions.search` tool)                   |
| `core-skills`   | skills   | Skill loading, `skills.read` tool, `/skills` command                          |
| `core-tools`    | tools    | File and shell tools (`files.read`, `files.write`, `files.edit`, `shell.run`) |
| `core-delegate` | delegate | Subagent delegation (`delegate.task`, `delegate.status`)                      |
| `mcp-bridge`    | mcp      | MCP server bridge                                                             |
| `ollama-web`    | web      | Web search/fetch via Ollama Cloud                                             |

When no store extension is loaded, omega falls back to an in-memory
store (sessions are lost on exit).

## Enabling

```yaml
extensions:
  enabled: true
  dir: extensions # relative to omega home, or absolute path
```

Extensions can also be controlled from the command line:

```bash
bin/omega chat --extension ./my-ext            # load a specific extension (repeatable)
bin/omega chat -e ./my-ext                     # short form
bin/omega chat --no-extensions                 # disable extension loading entirely
bin/omega chat --project-extensions            # also load <cwd>/.omega/extensions/
```

`--no-extensions` wins over everything. `--extension`/`-e` and
`--project-extensions` each force extensions on even when
`extensions.enabled` is `false`.

## Example: Web Extension

omega ships a web search extension in `bin/extensions/ollama-web/` that provides
web search and fetch tools via the [Ollama Cloud API](https://ollama.com).

```bash
# Build the web search extension
go build -o bin/extensions/ollama-web/ollama-web.exe ./bin/extensions/ollama-web/

# Enable extensions in config.yaml (see above), then:
bin/omega chat
# The web extension provides web.search and web.fetch tools.
# Ask the agent to search the web and it will call them as tool calls.
```

The extension receives the provider API key via the `OLLAMA_API_KEY`
environment variable, passed by the host from `config.yaml`.

## Subagent Delegation

The `core-delegate` extension provides `delegate.task` and
`delegate.status` tools. `delegate.task` spawns a child `omega run`
process for background subtasks. Results are injected into the parent
conversation when the subagent finishes. The recursion guard
(`OMEGA_SUBAGENT=1` env var) prevents subagents from spawning their own
subagents.

## Building Custom Extensions

An extension is any executable that speaks JSON-RPC over stdio:

1. On startup, receive an `initialize` request. Respond with your
   extension name, tools, and event subscriptions.
2. Receive `tool_call` requests when the agent invokes your tools.
3. Receive `event` notifications for subscribed lifecycle events
   (`agent_start`, `turn_start`, `turn_end`, `assistant_message`,
   `tool_result`, `agent_end`).
4. Receive a `shutdown` notification on exit.

Extensions can also send notifications to the host:
`delegate_start` (subagent started) and `delegate_result` (subagent
finished with output).

See [`bin/extensions/README.md`](../bin/extensions/README.md) for the
full protocol reference and
[`bin/extensions/ollama-web/main.go`](../bin/extensions/ollama-web/main.go)
for a complete implementation.
