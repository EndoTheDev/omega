# internal/agent

## Purpose

The agent layer runs the multi-turn conversation loop between a provider
and a set of tools. It consumes provider stream events, executes tool
calls, appends results back into message history, and emits lifecycle
events for anyone observing (the TUI, the gateway, or extensions).

## Ownership

- `agent.go` - multi-turn loop, tool execution, event dispatch
- `compaction.go` - context compaction with optional focus
- `context.go` - project context loading from AGENTS.md
- `events.go` - event types emitted by the agent loop
- `extension.go` - ExtensionManager interface and no-op manager
- `extension_stdio.go` - stdio JSON-RPC extension transport
- `prompt.go` - system prompt construction
- `skills.go` - SKILL.md loader
- `tools.go` - built-in tool registry
- `*_test.go` - self-check tests for each non-trivial package

## Local Contracts

- **The agent loop is the only place tools are executed.** The gateway
  and TUI route tool calls through the agent; they do not call tools
  directly.
- **Extension tools are merged at run time.** Built-in tools take
  precedence on name conflict. The merge happens inside `run()` so
  extensions can be swapped between runs.
- **Events are dispatched synchronously to the event channel and
  best-effort to extensions.** A stalled extension cannot block the
  agent loop.
- **Tool errors are structured returns.** Tools return `(string, error)`;
  the error becomes an `IsError` tool result message, never a panic.
- **No re-exports.** Types defined in `internal/ai/` are imported from
  there, not re-exported from this package.

## Work Guidance

- Add new lifecycle events in `events.go`, then emit them in `agent.go`
  and dispatch them to `extensions.DispatchEvent`.
- New transports (e.g. WASM) implement `ExtensionManager` and plug into
  `Agent.SetExtensions`. The agent and TUI stay unchanged.
- Keep `NewAgent` defaulting to `NoopManager` so callers that do not
  care about extensions are unaffected.
- Update the `eventType` and `eventData` helpers in
  `extension_stdio.go` when adding new event types, otherwise extensions
  over stdio will not receive them.
- Prefer stdlib-only solutions for transports. JSON-RPC over stdio
  uses only `encoding/json` and `os/exec`.

## Verification

```bash
go test ./internal/agent/     # unit + integration tests
go build ./...                # everything compiles
go vet ./...                  # no suspicious constructs
```

## Child DOX Index

No sub-packages.
