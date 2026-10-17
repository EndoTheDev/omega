# Roadmap

## Done

- Three providers with streaming, retry, and backoff
- Multi-turn agent loop with parallel tool execution
- Session tree with branching, labeling, and full persistence
- Context compaction with overflow auto-retry, reserve tokens, and branch summarization
- Skills system (skills extension, agent-driven `skills.read` tool, `/skills` command)
- Extension system: in-process Go plugins, 15 extensions, 11 exclusive + 2 additive seams (provider, compactor, store, skills, loop, logging, memory, prompt_builder, frontend, trust, tools, channel)
- Session store (SQLite, FTS5 full-text search, `sessions.search` tool)
- In-memory store fallback when no store extension loaded
- Complete TUI with streaming, markdown, autocomplete, and history
- Global installation via PATH with binary-dir resolution
- 10-level thinking control across all providers
- Model discovery (`/models` command, `/model <#|name>` selection)
- HTTP proxy support (`HTTP_PROXY`, `HTTPS_PROXY`)
- AGENTS.md ancestor walk (CWD to root, concatenated)
- Resource diagnostics (warnings for unreadable context files)
- Prompt guidelines (deduplicated bullets in system prompt)
- Tool result truncation (configurable max bytes)
- Session export (`/export` writes JSONL)
- Extension CLI flags (`--extension`/`-e`, `--no-extensions`, `--project-extensions`)
- Desktop notifications (`notifications` config: bell, desktop, off)
- Model quick-cycle (Ctrl+P)
- File drop (bracketed paste support)
- Export session subcommand (`omega export`)
- Self-update (`omega update`, archive-based with progress bar)
- Image input (`@file` args, `@`-mentions for globs, sessions, skills)
- Session insights (`omega insights [--days N]`, `/insights [days]`)
- Per-path file locks (serialize concurrent writes to the same file)
- Extension customization hooks (prompt guidelines, compaction, branch summary, session lifecycle)
- Session entry types (model_change, thinking_level_change persisted and replayed on resume)
- Config hot-reload (polling ticker, `OMEGA_HTTP_TIMEOUT` live-applied)
- `max_turns` configurable (default 100, `OMEGA_MAX_TURNS` env var)
- Tool schema validation at extension load
- Agent self-test (`omega test`)
- Version automation via git tags + ldflags
- Subagent delegation (delegate extension, `delegate.task`, `delegate.status`)
- SQLite WAL mode for concurrent parent/subagent access
- Auto-discovered context window (Ollama /api/show, provider > config > default fallback)
- Splash shows real tool count from loaded extensions
- `/search` added to help table
- Channel seam + HTTP channel extension (v0.3.0)
- Store and HTTP server implementations moved from gateway/ into extensions
- TUI extracted as Frontend extension (extensions/tui/, Frontend seam)
- Slash commands moved to owning extensions (12 commands: store, provider, tools)
- Extension-owned config (per-extension Config structs, no gateway import)
- Self-update fix: Python3 detection, unlink-before-rename on Linux
- Checksum verification for self-update (SHA256, checksums.txt in releases)
- Improver: autonomous test/coverage loop with safe auto-landing
- Web UI: Nuxt 4 SPA (VS Code-style shell, streaming, session persistence), embedded into the binary and served by the HTTP channel (v0.6.0)
- Segmented session persistence: assistant + tool calls -> tool results -> final assistant, matching the webui's live rendering (v0.6.0)
- Loopback-only default bind with explicit `server.bind` opt-in for reverse-proxy routing (v0.6.0)
- Release pipeline: tag -> Actions builds 5 platforms with the webui baked in + checksums; `omega update` self-service (v0.6.0-v0.6.4)
- First-class `go install`: committed SPA, buildinfo version resolution, first-run config bootstrap, `~/.omega` home for bin-dir installs (v0.6.1-v0.6.4)
- Concurrent sessions: per-run provider copies (`WithModel`), single-flight guard removed, parallel `/chat` with different models (v0.7.0)
- Per-session stream state in the webui: pulsing sidebar rows, tab-title spinner, composer usable in every session
- Hermes-style session list: search, server-side pinned flag (PATCH, `updated_at` untouched), Today/Yesterday/Earlier buckets, rich rows (model, msgs, tools, tokens, relative age)
- Real token totals per session (usage persisted at stream end), message + tool counts computed at read time
- URL-addressable sessions: `/chat/<id>` via hand-rolled history API, SPA fallback for non-file paths
- Markdown rendering in chat bubbles (markdown-it `html:false`, ~150ms debounced streaming renders)
- Interrupted-stream marker + retry affordance (transport death distinguishable from clean end)
- Session polish: hover timestamps, thinking elapsed meter, jump-to-live pill
- Inspector pane: tab shell, read-only file manager (`/files/browse` + `/files/read`, EvalSymlinks containment, 64 KiB cap)
- Dual title generation: provisional title at request start (user-message-only), final title with reply context after the first exchange; `title_state` machine (manual labels never overwritten, old sessions never retitled)
- Cron extension: jobs in `crons.yaml` (human-authored only, mtime-poll hot reload), robfig/cron expressions, catch-up for misses < 24h, per-job single-instance guard, sessions labeled `cron:<name>`, deliver targets journal/memory
- First-run memory seeding removed: fresh homes boot without memory.md/user.md; the agent's first 'memory add' creates them (2026-10-18)

## Planned

- More tools (grep, glob, multi-file edit)
- More providers (Gemini, Mistral)
- Prompt templates with variable interpolation
- Core-trust extension (pluggable trust gate)
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
