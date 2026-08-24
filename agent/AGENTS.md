# agent

## Purpose

The agent layer runs the multi-turn conversation loop between a provider
and a set of tools. It consumes provider stream events, executes tool
calls, appends results back into message history, and emits lifecycle
events for anyone observing (the TUI, the gateway, or extensions).

## Ownership

- `agent.go` - agent struct, configuration holders, capability seam wiring
  (`SetCompactionProvider`, `SetToolProvider`, `SetMaxToolOutput`, `SetCWD`,
  `SetPromptCustom`, `SetPromptAppend`, `SetPromptContext`, `SetLoopProvider`,
  `SetProvider`, `SetUserInput`). Delegates execution to `LoopProvider`.
- `seams.go` - capability seam interfaces (`LoopProvider` + `LoopOptions`,
  `CompactionProvider`, `ToolProvider` + `DefaultToolProvider`,
  `StoreProvider`, `SkillsProvider`). All seam interfaces and their
  default implementations in one file.
- `default_loop.go` - `DefaultLoopProvider` (standard turn loop: stream,
  execute tools concurrently via sync.WaitGroup, feed results back in
  order), `isOverflowError`, `toolSchemas`.
- `compaction.go` - compaction config (`CompactionConfig`), token
  estimation (`EstimateTokens`, `MessageText`), context window constants,
  `BuildCompactedMessages` (shared helper for branch summary).
  Compaction logic lives in the `core-compactor` extension.
- `events.go` - event types emitted by the agent loop (`AgentStart`,
  `TurnStart`, `TurnEnd`, `AgentEnd`, `StreamEvent`, `ToolResultEvent`,
  `AssistantMessageEvent`, `SessionEvent`)
- `extensions.go` - `ExtensionManager` interface (tools, commands, events,
  `PromptGuidelines`, `CustomizeBranchSummary`, `BuildPrompt`,
  `SeamProviders`, provider dispatch:
  `ProviderStream`, `ProviderModelName`, `ProviderListModels`,
  `ProviderModelInfo`, `ProviderSetThinking`, `ProviderSetModel`, `StoreProvider`,
  `SkillsProvider`, `CompactorProvider`, `InjectedMessages`, `PendingDelegations`),
  `NoopManager`, `ExtensionCommand`, `ExtensionInfo`
  (with `Seams`, `ToolList` fields), `ToolInfo` (Name + Description),
  `PromptBuildOptions`, `InjectedMessage` (Text + Source)
- `extension_stdio.go` - stdio JSON-RPC extension transport
  (`prompt/guidelines`, `branch/summary`,
  `prompt/build` JSON-RPC methods).
  Streaming RPC: `streamRequest` sets up `notifyCh` for notification
  routing, `readLoop` distinguishes notifications (no ID) from responses
  (with ID). Provider dispatch: `providerExt`, `ProviderStream`,
  `ProviderModelName`, `ProviderListModels`, `ProviderModelInfo`,
  `ProviderSetThinking`, `ProviderSetModel`. Store dispatch: `storeExt`, `StoreRequest`,
  `StoreProvider`. Skills dispatch: `skillsExt`, `SkillsRequest`,
  `SkillsProvider`. Compactor dispatch: `compactorExt`, `CompactorRequest`,
  `CompactorProvider`. Delegate dispatch: `delegate_start`/`delegate_result`
  notification handling, `delegateCh` (buffer 64), `pendingDelegations`
  (atomic, CAS-clamped at 0), `handleDelegateResult`,
  `incrementPendingDelegations`, `InjectedMessages`. Message serialization
  adds `role` field based on concrete `ai.Message` type.
- `types.go` - shared data types (`Session`, `SessionNode`, `SearchResult`,
  `Skill`, `Insights`, `ToolStat`, `DayStat`, `NotableStat`) used by the
  store interface, skills interface, and callers
- `proxy_store.go` - `ProxyStore` + `StoreDispatcher`: forwards all
  `StoreProvider` methods to the store-seam extension via JSON-RPC.
  `decodeMessages` helper for (role, payload) → `[]ai.Message`
- `proxy_skills.go` - `ProxySkills` + `SkillsDispatcher`: forwards
  `SkillsProvider.LoadSkills` to the skills-seam extension via JSON-RPC
- `proxy_compactor.go` - `ProxyCompactionProvider` +
  `CompactionProviderDispatcher`: forwards `CompactionProvider.Compact`
  to the compactor-seam extension via JSON-RPC. Passes compaction config
  in each request.
- `testdata/mock_extension/` - mock extension binary for extension tests
- `tools.go` - deleted. Built-in tools moved to extensions. Tool naming
  convention: `namespace.action` (e.g. `files.read`, `shell.run`,
  `skills.read`). Extension tools use `<server>.<tool>` (e.g.
  `obsidian.vault_read`). `skills.read` is now in the core-skills
  extension, not core-tools.
- `*_test.go` - self-check tests for each non-trivial package

## Local Contracts

- **The agent loop is the only place tools are executed.** The gateway
  and TUI route tool calls through the agent; they do not call tools
  directly. The default loop (`DefaultLoopProvider`) handles this; a
  custom `LoopProvider` owns its own tool dispatch.
- **Extension tools are merged at run time.** Built-in tools take
  precedence on name conflict. The merge happens inside
  `DefaultLoopProvider.Run()` so extensions can be swapped between runs.
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
  prompt guidelines (`PromptGuidelines`), branch summary
  (`CustomizeBranchSummary`). Session lifecycle events (`SessionEvent`)
  are dispatched on new, resume, fork, and label.
- **Capability seams.** Harness concerns are injected via interfaces:
  `CompactionProvider` (context compaction), `ToolProvider` (tool
  registry), `LoopProvider` (conversation loop). Default
  implementations in `default_loop.go` and `seams.go`. The system
  prompt is built by the `core-prompt` extension via `BuildPrompt`. The
  compactor is wired from the `core-compactor` extension's `compactor`
  seam via `CompactorProvider`; when no compactor extension is loaded,
  compaction is disabled and the agent surfaces a friendly error on
  context overflow. A custom `LoopProvider` replaces the entire turn
  logic via `SetLoopProvider`. The LLM provider is wired via
  `SetProvider` from the `core-provider` extension's `provider` seam
  (`ExtensionProvider` delegates to `ExtensionManager.ProviderStream`
  and related methods). Project context and trust live in
  `cmd/omega/context.go` and `cmd/omega/trust.go`.
- **No re-exports.** Types defined in `ai/` are imported from
  there, not re-exported from this package.
- **API key passing.** `Load` receives the provider API key and passes
  it to extensions via the `OLLAMA_API_KEY` env var (stdio transport).
- **Subagent delegation injection.** The agent loop drains
  `InjectedMessages` after every turn (non-blocking, batches multiple
  results). In one-shot mode (`UserInput == nil`), the loop blocks on
  `InjectedMessages` when `PendingDelegations() > 0`. In TUI mode
  (`UserInput != nil`), the loop never blocks — the TUI tick handler
  drains and injects results as new runs.

## Work Guidance

- Add new lifecycle events in `events.go`, then emit them in
  `default_loop.go` and dispatch them to `extensions.DispatchEvent`.
- New transports (e.g. WASM) implement `ExtensionManager` and plug into
  `Agent.SetExtensions`. The agent and TUI stay unchanged.
- Keep `NewAgent` defaulting to `NoopManager` so callers that do not
  care about extensions are unaffected.
- Update the `eventPayload` helper in
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