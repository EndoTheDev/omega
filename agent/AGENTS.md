# agent

## Purpose

The agent layer runs the multi-turn conversation loop between a provider
and a set of tools. It consumes provider stream events, executes tool
calls, appends results back into message history, and emits lifecycle
events for anyone observing (the TUI, the gateway, or extensions).

## Ownership

- `agent.go` - multi-turn loop, tool execution, event dispatch, capability
  seam wiring (`SetPromptBuilder`, `SetCompactor`, `SetToolProvider`,
  `SetMaxToolOutput`, `SetCWD`)
- `compaction.go` - context compaction with optional focus,
  `BuildCompactedMessages` shared helper
- `events.go` - event types emitted by the agent loop (`AgentStart`,
  `TurnStart`, `TurnEnd`, `AgentEnd`, `StreamEvent`, `ToolResultEvent`,
  `AssistantMessageEvent`, `SessionEvent`)
- `extension.go` - `ExtensionManager` interface (tools, commands, events,
  `PromptGuidelines`, `CustomizeCompaction`, `CustomizeBranchSummary`,
  `BuildPrompt`, `CompactMessages`, `SeamProviders`), `NoopManager`,
  `ExtensionCommand`, `ExtensionInfo` (with `Seams` field),
  `PromptBuildOptions`
- `extension_stdio.go` - stdio JSON-RPC extension transport
  (`prompt/guidelines`, `compaction/customize`, `branch/summary`,
  `prompt/build`, `compaction/messages` JSON-RPC methods)
- `seams.go` - capability seam interfaces (`PromptBuilder`, `Compactor`,
  `ToolProvider`, `SessionStore`)
- `defaults.go` - default seam implementations (`DefaultPromptBuilder`,
  `DefaultCompactor`, `DefaultToolProvider`)
- `skill.go` - `Skill` type (data type used by `SetSkills` + `runLoadSkill`)
- `testdata/mock_extension/` - mock extension binary for extension tests
- `tools.go` - built-in tool registry (`shell`, `read_file`, `write_file`,
  `edit`), per-path file locking (`fileLocks`, `fileMutex`), `runLoadSkill`
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
- **File tools are per-path locked.** `read_file`, `write_file`, and
  `edit` acquire a `sync.Mutex` keyed by absolute path before touching
  the file, serializing concurrent access to the same path.
- **Extension customization hooks.** Extensions can customize: system
  prompt guidelines (`PromptGuidelines`), compaction summary
  (`CustomizeCompaction`), branch summary (`CustomizeBranchSummary`).
  Session lifecycle events (`SessionEvent`) are dispatched on new,
  resume, fork, and label. `BuildCompactedMessages` is the shared
  helper for assembling compacted history from a pre-computed summary.
- **Capability seams.** Harness concerns are injected via interfaces:
  `PromptBuilder` (system prompt), `Compactor` (context compaction),
  `ToolProvider` (tool registry), `SessionStore` (persistence). Default
  implementations in `defaults.go`. Extensions can fully replace the
  prompt builder via `BuildPrompt` or the compactor via `CompactMessages`.
  Harness code (prompt building, skill loading, project context) lives
  in `harness/`, not in this package.
- **No re-exports.** Types defined in `ai/` are imported from
  there, not re-exported from this package.
- **API key passing.** `Load` receives the provider API key and passes
  it to extensions via the `OLLAMA_API_KEY` env var (stdio transport).

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
  uses only stdlib packages.

## Verification

```bash
go test ./agent/     # unit + integration tests
go build ./...                # everything compiles
go vet ./...                  # no suspicious constructs
```

## Child DOX Index

No sub-packages.
