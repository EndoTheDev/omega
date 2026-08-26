# Roadmap

## Done

- Three providers with streaming, retry, and backoff
- Multi-turn agent loop with parallel tool execution
- Session tree with branching, labeling, and full persistence
- Context compaction with overflow auto-retry, reserve tokens, and branch summarization
- Skills system (skills extension, agent-driven `skills.read` tool, `/skills` command)
- Extension system: in-process Go plugins, 10 extensions, 6 seams (prompt, provider, store, skills, tools, compactor)
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
- Config hot-reload (fsnotify, `OMEGA_HTTP_TIMEOUT` live-applied)
- `max_turns` configurable (default 100, `OMEGA_MAX_TURNS` env var)
- Tool schema validation at extension load
- Agent self-test (`omega test`)
- Version automation via git tags + ldflags
- Subagent delegation (core-delegate extension, `delegate.task`, `delegate.status`)
- SQLite WAL mode for concurrent parent/subagent access
- Auto-discovered context window (Ollama /api/show, provider > config > default fallback)
- Splash shows real tool count from loaded extensions
- `/search` added to help table

## Planned

- More tools (grep, glob, multi-file edit)
- More providers (Gemini, Mistral)
- Web UI (via the gateway HTTP API)
- Prompt templates with variable interpolation
- Core-trust extension (pluggable trust gate)
- Output/channel seam (Telegram, Discord, WhatsApp)
- Gateway-mode delegation support (SSE polling)
- Checksum verification for self-update

## Known Limitations

- `omega serve` has no authentication, TLS, or CORS - do not expose it
  on a public network
- `omega health` only checks HTTP 200 on `/health` - does not probe the
  provider or database
- Compaction is irreversible - summarized messages cannot be restored
  to their original form
- No concurrent session safety across HTTP requests
- SQLite uses a pure-Go driver (`modernc.org/sqlite`, no CGO) but is
  untested under heavy load
