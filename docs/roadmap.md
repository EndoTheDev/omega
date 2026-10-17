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

### v0.8.0 (dev lane, unreleased)

- Hermes-style session list (search, pinned, time buckets, rich rows)
  with real token totals and per-session stream state
- URL-addressable sessions, markdown rendering, interrupted-stream
  retry, syntax highlighting, inspector pane with read-only file manager
- Unified session status: pin + streaming + unread, server-side
  `last_read_at`, cron sessions land unread
- Dual title generation (provisional at request start, final after the
  first exchange), cron module (`crons.yaml`, hot-reload, catch-up,
  `cron:<name>` sessions)
- Module restructure: `extensions/` -> `modules/`, web UI at
  `modules/ui`, instance home gains `config/` for the yaml files
  (root-first resolution keeps existing installs unchanged)
- Session-list polish: star morphs on full row hover, dividers hidden
  with their closed panes
- First-run seeding removed: memory.md/user.md are agent-created on
  first use, nothing personal ships in file or binary

## Planned

- Web UI: codeblock UX (max height, copy button, collapse), elapsed
  timing on thinking/tool/response bubbles, content search over the
  session store, full settings parity with config knobs, VS Code-style
  file tree in the inspector
- Provider parity arc: anthropic Messages API support alongside the
  openai-compatible and ollama-native surfaces
- Agent self-improvement: read-only eval suite + propose-only
  reflection loop (evaluated improvements, never self-merged)
- OpenTelemetry observability (research: traces, metrics, tool timings)
- More tools (grep, glob, multi-file edit)
- Prompt templates with variable interpolation
- Core-trust module (pluggable trust gate)
- Discord/Telegram channels (via the Channel seam)
- Serve-mode delegation support (SSE polling)

## Known Limitations

- `omega serve` has no authentication, TLS, or CORS - it binds loopback
  by default; exposing it requires an explicit `server.bind` entry and an
  authenticating reverse proxy in front
- The shared injected-messages channel means a delegated message drains
  into an arbitrary active run at single-user load (concurrent /chat
  runs themselves are safe since v0.7.0)
- `omega health` only checks HTTP 200 on `/health` - does not probe the
  provider or database
- Compaction is irreversible - summarized messages cannot be restored
  to their original form
- SQLite uses a pure-Go driver (`modernc.org/sqlite`, no CGO) but is
  untested under heavy load