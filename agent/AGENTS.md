# agent

## Purpose

The agent layer defines the multi-turn conversation loop contract,
capability seam interfaces, shared data types, and the in-process
extension system (Context, Plugin, MountAll). It consumes provider
stream events, executes tool calls, appends results back into message
history, and emits lifecycle events for anyone observing (the TUI, the
gateway, or extensions).

## Ownership

- `agent.go` - Agent struct, configuration holders, capability seam
  wiring (`SetCompactionProvider`, `SetToolProvider`, `SetMaxToolOutput`,
  `SetCWD`, `SetPromptCustom`, `SetPromptAppend`, `SetPromptContext`,
  `SetLoopProvider`, `SetProvider`, `SetUserInput`, `SetLogger`). Delegates execution
  to `LoopProvider`. The loop is not set by default; the host wires one
  via `SetLoopProvider` or by mounting the agent-loop extension.
- `seams.go` - capability seam interfaces (`LoopProvider` + `LoopOptions`,
  `CompactionProvider`, `ToolProvider` + `DefaultToolProvider`,
  `StoreProvider`, `SkillsProvider`, `LoggerProvider`, `MemoryProvider`, `PromptBuilder`).
- `plugin.go` - in-process extension system: `Context` (shared service
  container with typed seam slots), `Plugin` interface (`Name`,
  `Provides`, `Requires`, `Mount`), `MountAll` (topological sort by
  dependencies, conflict detection on exclusive seams).
- `compaction.go` - compaction config (`CompactionConfig` with
  `Budget()` method), token estimation (`EstimateTokens`, `MessageText`), context window
  constants, `BuildCompactedMessages` (shared helper for branch summary
  and compactor extension). Compaction logic lives in
  `extensions/compactor/`.
- `events.go` - event types emitted by the agent loop (`AgentStart`,
  `TurnStart`, `TurnEnd`, `AgentEnd`, `StreamEvent`,
  `AssistantMessageEvent`, `ToolResultEvent`).
- `types.go` - shared data types (`Session`, `SessionNode`,
  `SearchResult`, `Skill`, `Insights`, `ToolStat`, `DayStat`,
  `NotableStat`, `InjectedMessage`, `ExtensionCommand`, `ToolInfo`,
  `ExtensionInfo`, `PromptBuildOptions`) used by the store interface,
  skills interface, and callers.
- `*_test.go` - self-check tests for each non-trivial package

## Local Contracts

- **The agent loop is the only place tools are executed.** The gateway
  and TUI route tool calls through the agent; they do not call tools
  directly. The loop implementation (in `extensions/agent_loop/`)
  handles this; a custom `LoopProvider` owns its own tool dispatch.
- **Extension tools are merged at run time.** Built-in tools take
  precedence on name conflict. The merge happens inside the loop's
  `Run()` so extensions can be swapped between runs.
- **Tool errors are structured returns.** Tools return `(string, error)`;
  the error becomes an `IsError` tool result message, never a panic.
- **Capability seams.** Harness concerns are injected via interfaces:
  `CompactionProvider` (context compaction), `ToolProvider` (tool
  registry), `LoopProvider` (conversation loop), `StoreProvider`
  (session persistence), `SkillsProvider` (skill loading),
  `LoggerProvider` (operational logging), `MemoryProvider` (persistent memory), `PromptBuilder` (system prompt). Default implementations live in
  `extensions/`. The system prompt is built by the prompt extension
  via `BuildPrompt`. The compactor is wired from the compactor
  extension; when no compactor extension is loaded, compaction is
  disabled and the agent surfaces a friendly error on context overflow.
  The LLM provider is wired via `SetProvider` from the provider
  extension's mount into `Context.Provider`. Project context and trust
  live in `cmd/omega/context.go` and `cmd/omega/trust.go`.
- **Extensions are in-process.** All extensions are Go packages under
  `extensions/`. No stdio, no IPC, no separate processes. The host
  calls `MountAll` at startup; the agent loop reads from `Context`.
- **No re-exports.** Types defined in `ai/` are imported from
  there, not re-exported from this package.
- **Subagent delegation injection.** The agent exposes
  `SetInjectedMessages` and `SetPendingDelegations` for delegation
  injection. Draining behavior is owned by the loop implementation
  (`extensions/agent_loop/`).

## Work Guidance

- Add new lifecycle events in `events.go`, then emit them in the loop
  implementation (`extensions/agent_loop/`).
- New extensions implement `agent.Plugin` and mount into `Context` via
  `MountAll`. The agent and TUI stay unchanged.
- The agent package holds only contracts, types, and the plugin system.
  Implementation logic lives in `extensions/`.

## Verification

```bash
go test ./agent/     # unit + integration tests
go build ./...       # everything compiles
go vet ./...         # no suspicious constructs
```

## Child DOX Index

No sub-packages.
