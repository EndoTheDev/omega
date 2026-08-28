# extensions

## Purpose

The extensions layer provides all extension implementations as
importable Go packages. Each implements `agent.Plugin` and mounts
into a shared `Context` via `MountAll` at startup.

## Ownership

- `README.md` - extension authoring guide
- `agent_loop/` - default conversation loop (`LoopProvider`
  implementation); streams provider responses, executes tool calls,
  feeds results back, handles compaction and overflow retry
- `provider/` - LLM provider (Ollama, OpenAI, Anthropic streaming);
  registers `/model` and `/provider` commands
- `store/` - session store (wraps `gateway.Store`, `sessions.search`
  tool)
- `skills/` - skill loading (`skills.read` tool, `/skills` command)
- `compactor/` - context compaction (keep-first/last + LLM
  summarize); registers `/compact` command
- `prompt/` - system prompt builder + guidelines
- `tools/` - shell and file tools (`shell.run`, `files.read`,
  `files.write`, `files.edit`); file tools are per-path locked via
  `sync.Mutex` keyed by absolute path
- `mcp/` - MCP bridge (stdio + HTTP MCP servers)
- `delegate/` - subagent delegation (`delegate.task`, `delegate.status`,
  `InjectedMessages` channel + `PendingDelegations` func)
- `web/` - web search/fetch (Ollama Cloud API)
- `memory/` - persistent memory (`memory` tool, two-file store with
  `§`-delimited entries, snapshot read fresh and injected into system prompt)

## Local Contracts

- Every extension implements `agent.Plugin` (`Name`, `Provides`,
  `Requires`, `Mount`).
- Exclusive seams (one plugin per slot): `provider`, `compactor`,
  `store`, `skills`, `loop`, `memory`, `prompt_builder`.
- Additive seam: `tools` (multiple plugins contribute to
  `ctx.ToolProviders`).
- Commands: extensions register slash commands via `ctx.Commands` +
  `ctx.CommandHandler`. Command handlers return `CommandResult` with
  optional `CmdAction` TUI directives. The host interprets actions;
  extensions declare intent.
- Extensions are compiled into `omega.exe`. No separate binaries, no
  runtime loading.

## Work Guidance

- To add a new extension: create `extensions/<name>/` with
  `<name>.go` (logic), `plugin.go` (Plugin adapter),
  `<name>_test.go` (self-check).
- Add the plugin to `buildPlugins()` in `cmd/omega/main.go`.
- If the extension provides tools, append to `ctx.ToolProviders`
  during `Mount()`.
- If the extension provides commands, append to `ctx.Commands` and
  chain `ctx.CommandHandler` (call prev handler on miss).

## Verification

```bash
go test ./extensions/...   # all extension tests
go build ./...             # everything compiles
go vet ./...               # no suspicious constructs
```

## Child DOX Index

No sub-packages. Each extension is a leaf package.
