# agent

## Purpose

The agent layer runs the multi-turn conversation loop between a provider
and a set of tools. It consumes provider stream events, executes tool
calls, appends results back into message history, and emits lifecycle
events for anyone observing (the TUI, the gateway, or extensions).

## Ownership

- `agent.go` - agent struct, configuration holders, capability seam wiring
  (`SetCompactor`, `SetToolProvider`, `SetMaxToolOutput`, `SetCWD`,
  `SetPromptCustom`, `SetPromptAppend`, `SetPromptContext`, `SetAgentLoop`).
  Delegates execution to `AgentLoop`.
- `loop.go` - `AgentLoop` interface (Go-level seam for the conversation loop),
  `LoopOptions` struct. Default implementation is `DefaultAgentLoop`.
- `default_loop.go` - `DefaultAgentLoop` (standard turn loop: stream, execute
  tools, feed results back), `isOverflowError`, `toolSchemas`. Extracted from
  the former `run()` method on `Agent`.
- `compaction.go` - context compaction with optional focus,
  `BuildCompactedMessages` shared helper
- `events.go` - event types emitted by the agent loop (`AgentStart`,
  `TurnStart`, `TurnEnd`, `AgentEnd`, `StreamEvent`, `ToolResultEvent`,
  `AssistantMessageEvent`, `SessionEvent`)
- `extension.go` - `ExtensionManager` interface (tools, commands, events,
  `PromptGuidelines`, `CustomizeCompaction`, `CustomizeBranchSummary`,
  `BuildPrompt`, `CompactMessages`, `SeamProviders`), `NoopManager`,
  `ExtensionCommand`, `ExtensionInfo` (with `Seams`, `ToolList` fields),
  `ToolInfo` (Name + Description), `PromptBuildOptions`
- `extension_stdio.go` - stdio JSON-RPC extension transport
  (`prompt/guidelines`, `compaction/customize`, `branch/summary`,
  `prompt/build`, `compaction/messages` JSON-RPC methods)
- `seams.go` - capability seam interfaces (`Compactor`, `ToolProvider`,
  `SessionStore`)
- `defaults.go` - default seam implementations (`DefaultCompactor`,
  `DefaultToolProvider`)
- `skill.go` - `Skill` type (data type for skill listing, used by
  `harness.LoadSkills`)
- `testdata/mock_extension/` - mock extension binary for extension tests
- `tools.go` - empty tool registry (`NewRegistry` returns empty map).
  Built-in tools moved to `bin/extensions/core-tools/` extension.
  Tool naming convention: `namespace.action` (e.g. `files.read`, `shell.run`,
  `skills.read`). Extension tools use `<server>.<tool>` (e.g. `obsidian.vault_read`).
- `*_test.go` - self-check tests for each non-trivial package

## Local Contracts

- **The agent loop is the only place tools are executed.** The gateway
  and TUI route tool calls through the agent; they do not call tools
  directly. The default loop (`DefaultAgentLoop`) handles this; a custom
  `AgentLoop` owns its own tool dispatch.
- **Extension tools are merged at run time.** Built-in tools take
  precedence on name conflict. The merge happens inside
  `DefaultAgentLoop.Run()` so extensions can be swapped between runs.
- **Events are dispatched synchronously to the event channel and
  best-effort to extensions.** A stalled extension cannot block the
  agent loop.
- **Tool errors are structured returns.** Tools return `(string, error)`;
  the error becomes an `IsError` tool result message, never a panic.
- **File tools are per-path locked.** The `files.read`, `files.write`,
  and `files.edit` tools acquire a `sync.Mutex` keyed by absolute path
  before touching the file, serializing concurrent access to the same
  path. The locks now live in the `core-tools` extension, not in the
  agent package.
- **Extension customization hooks.** Extensions can customize: system
  prompt guidelines (`PromptGuidelines`), compaction summary
  (`CustomizeCompaction`), branch summary (`CustomizeBranchSummary`).
  Session lifecycle events (`SessionEvent`) are dispatched on new,
  resume, fork, and label. `BuildCompactedMessages` is the shared
  helper for assembling compacted history from a pre-computed summary.
- **Capability seams.** Harness concerns are injected via interfaces:
  `Compactor` (context compaction), `ToolProvider` (tool registry),
  `SessionStore` (persistence), `AgentLoop` (conversation loop). Default
  implementations in `defaults.go` and `default_loop.go`. The system prompt
  is built by the `core-prompt` extension via `BuildPrompt`; extensions
  can also fully replace the compactor via `CompactMessages`. A custom
  `AgentLoop` replaces the entire turn logic via `SetAgentLoop`.
  Harness code (skill loading, project context) lives in `harness/`, not
  in this package.
- **No re-exports.** Types defined in `ai/` are imported from
  there, not re-exported from this package.
- **API key passing.** `Load` receives the provider API key and passes
  it to extensions via the `OLLAMA_API_KEY` env var (stdio transport).

## Work Guidance

- Add new lifecycle events in `events.go`, then emit them in
  `default_loop.go` and dispatch them to `extensions.DispatchEvent`.
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
