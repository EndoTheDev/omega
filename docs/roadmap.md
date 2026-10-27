# Roadmap

## Shipped highlights

Milestones through v0.7.0; per-feature detail lives in the release notes
and git history.

- **Core agent (v0.1-v0.2):** streaming providers with retry/backoff,
  multi-turn agent loop with parallel tool execution, session tree
  (branching, labeling, full persistence), context compaction with
  auto-retry, skills system, 15-module system (11 exclusive + 2
  additive seams), SQLite store with FTS5 search, complete TUI,
  model discovery, image input, self-update
- **Module architecture (v0.3-v0.5):** channel seam + HTTP channel,
  store and TUI moved to modules, per-module config, config
  hot-reload, subagent delegation, session insights + export,
  AGENTS.md ancestor walk, 10-level thinking control
- **Distribution (v0.6.0-v0.6.4):** Nuxt web UI embedded in the binary
  and served by the HTTP channel, loopback-only default bind with
  `server.bind` opt-in, release pipeline (tag -> Actions builds 5
  platforms + checksums), first-class `go install`, first-run config
  bootstrap, `~/.omega` home resolution
- **Concurrency (v0.7.0):** per-run provider copies (`WithModel`),
  single-flight guard removed, parallel `/chat` with different models

### v0.8.0 (2026-10-18)

- Hermes-style session list (search, pinned, time buckets, rich rows)
  with real token totals and per-session stream state
- URL-addressable sessions, markdown rendering, interrupted-stream
  retry, syntax highlighting, inspector pane with read-only file manager
- Unified session status: pin + streaming + unread, server-side
  `last_read_at`, cron sessions land unread
- Dual title generation (provisional at request start, final after the
  first exchange), cron module (`crons.yaml`, hot-reload, catch-up,
  `cron:<name>` sessions)
- Settings pane with every config knob (masked secrets, gated writes),
  codeblock UX (max height, copy, collapse), elapsed-time display with
  millisecond precision on all bubbles
- Content search over the session store (FTS5 with phrase + prefix
  matching), VS Code-style file tree in the inspector
- Web tools (search/fetch) through the provider mirror, loud non-2xx
- Provider parity: ollama-native tool loop, anthropic Messages API,
  openai-compat gap fixes
- Module restructure: `extensions/` -> `modules/`, web UI at
  `webui`, instance home gains `config/` for the yaml files
- First-run seeding removed: memory.md/user.md are agent-created on
  first use, nothing personal ships in file or binary

### v0.9.0 (shipped)

- Module-owned UI: modules ship components in `modules/<name>/ui/`,
  composed at build time (no runtime registry)
- Backend-owns-facts foundation: transcript endpoint (render-ready
  items, one contract for restore + live SSE), explicit running state
  on session rows, unified title + content search
- Kanban module: full 9-status task board with agent task tools,
  comments, events, session links, and a board view
- Dock system: VS Code-style split layouts, per-pane tab bars,
  drag-and-drop between panes, persisted layouts, dockable component
  contract, chat tabs with same-session multi-tab streaming
- Session archive + delete, cancel/retry in the composer
- Store-is-the-only-door: the tool layer refuses direct database
  writes, enforcing the agent/store boundary
- Context compaction auto-window (provider-aware), trust + config
  layout migration, magnetic pane snapping, process-group kill fix
  for cancelled shell runs, status bar durability (uptime, version,
  last-run stats)
- Theming system: token-override YAMLs in the home, hot reload,
  picker in settings (`GET /themes`)
- Webui image input (attach strip + `/attach-image` server fetch with
  the internal-address guard) and the `vision_model` captioner
- Inspector tab suite: Files (read-only working-dir surface), Cron,
  Logs, Skills, Tools, Settings, Kanban
- Reasoning-effort control: `thinking_level` end to end (TUI command,
  request field, webui select)
- Webui/HTTP reference docs live in `docs/webui.md` / `docs/api.md`;
  the binary reference in `docs/cli.md`

## Planned

- Agent self-improvement: read-only eval suite + propose-only
  reflection loop (evaluated improvements, never self-merged)
- OpenTelemetry observability (research: traces, metrics, tool timings)
- More tools (grep, glob)
- Prompt templates with variable interpolation
- Discord/Telegram channels (via the Channel seam)
- Serve-mode delegation support (SSE polling)

## Known Limitations

- `omega serve` has no authentication, TLS, or CORS - it binds loopback
  by default; exposing it requires an explicit `server.bind` entry and
  an authenticating reverse proxy in front
- The shared injected-messages channel means a delegated message drains
  into an arbitrary active run at single-user load (concurrent /chat
  runs themselves are safe since v0.7.0)
- `omega health` only checks HTTP 200 on `/health` - does not probe the
  provider or database
- Compaction is irreversible - summarized messages cannot be restored
  to their original form
- SQLite uses a pure-Go driver (`modernc.org/sqlite`, no CGO) but is
  untested under heavy load
