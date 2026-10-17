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
  registers `/model`, `/models`, `/thinking`, and `/provider` commands.
  OpenAI-compat streaming captures reasoning deltas (as ThinkingChunk)
  and token usage (`stream_options.include_usage`), restructures
  tool_calls to the OpenAI wire format in message history, and probes
  `/api/show` (root path, `/v1` stripped) for the model context window
  on ollama-lane hosts
- `store/` - session store (SQLite implementation + `sessions.search`
  tool; sessions carry a `last_read_at` read stamp - new sessions born
  read, pre-column rows backfilled to their own `updated_at`, and every
  appended message stamps the row's `updated_at` so the activity time
  tracks real output; registers `/sessions`, `/tree`, `/search`, `/insights`, `/new`,
  `/resume`, `/branch`, `/label`, `/export` commands)
- `skills/` - skill loading (`skills.read` tool, `/skills` command)
- `compactor/` - context compaction (keep-first/last + LLM
  summarize); registers `/compact` command
- `logging/` - operational logging (file logger, LoggerProvider seam)
- `prompt/` - system prompt builder + guidelines
- `tools/` - shell and file tools (`shell.run`, `files.read`,
  `files.write`, `files.edit`); file tools are per-path locked via
  `sync.Mutex` keyed by absolute path; registers `/tools` command
- `mcp/` - MCP bridge (stdio + HTTP MCP servers)
- `delegate/` - subagent delegation (`delegate.task`, `delegate.status`,
  `InjectedMessages` channel + `PendingDelegations` func)
- `web/` - web search/fetch (Ollama Cloud API)
- `memory/` - persistent memory (`memory` tool, two-file store with
  `§`-delimited entries, snapshot read fresh and injected into system prompt)
- `http_channel/` - HTTP/SSE channel + embedded SPA serving (Channel
  seam, `omega serve` transport; see `extensions/http_channel/AGENTS.md`):
  loopback-only bind list by default, SSE streaming, segmented session
  persistence (assistant + calls -> tool results -> assistant),
  session CRUD, `/models` with cached context window, detached title
  generation, serves the committed webui SPA at `/`
- `tui/` - terminal frontend (Frontend seam, Bubble Tea TUI:
  streaming, slash commands, themes, autocomplete, @file input,
  glamour rendering, desktop notifications)
- `trust/` - project trust gate (TrustProvider seam: trust.yaml
  store, AGENTS.md context loading, interactive trust prompt)
- `cron/` - scheduled prompts (CronProvider seam, `crons.yaml` in
  OMEGA_HOME; each fire is a full agent turn in a session labeled
  `cron:<name>`; catch-up for misses < 24h via `cronstate.json`;
  mtime-poll hot reload; see `extensions/cron/AGENTS.md`)

## Local Contracts

- Every extension implements `agent.Plugin` (`Name`, `Provides`,
  `Requires`, `Mount`).
- Exclusive seams (one plugin per slot): `provider`, `compactor`,
  `store`, `skills`, `loop`, `logging`, `memory`, `prompt_builder`,
  `frontend`, `trust`, `cron`.
- Additive seams: `tools` and `channel` (multiple plugins contribute
  to `ctx.ToolProviders` and `ctx.Channels`).
- Commands: extensions register slash commands via `ctx.Commands` +
  `ctx.CommandHandler`. Command handlers return `CommandResult` with
  optional `CmdAction` TUI directives. The host interprets actions;
  extensions declare intent.
- Extensions are compiled into the omega binary. No separate binaries, no
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

- `extensions/tui/` - terminal frontend (Frontend seam, Bubble Tea TUI)
- `extensions/http_channel/` - HTTP/SSE channel + embedded SPA serving (loopback-by-default bind list, segmented session persistence)
