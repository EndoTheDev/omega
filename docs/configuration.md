# Configuration

All values can be set in `config.yaml` or overridden by environment
variables. omega looks for its configuration - `config.yaml`, `mcp.yaml`,
`crons.yaml` - in its home: the binary's directory for release-folder
installs, `~/.omega` when the binary lives in a PATH bin directory
(`~/go/bin`, `~/.local/bin`), or `OMEGA_HOME` when set. `memory.md`,
`user.md`, `skills/`, `themes/`, and log files live at the home root;
the databases (`omega.db`, `kanban.db`, plus their SQLite `-wal`/`-shm`
sidecars) live in a `data/` subdirectory of the home (Endo 2026-10-20
layout), where the three infrastructure yaml files live in a `config/`
subdirectory (Endo 2026-10-18 layout). On first run omega writes the
example configs into `<home>/config/`. **Existing installs keep working
unchanged** - a legacy home-root database is moved (renamed, never
copied) into `data/` automatically on the first boot of the new
version. The working directory is used only
for AGENTS.md project context and tool file operations.

## config.yaml

```yaml
provider:
  type: ollama # ollama, openai, or anthropic
  model_name: # required - e.g. llama3, gpt-4o, claude-sonnet-4-20250514
  host: http://localhost:11434 # use https://ollama.com for Ollama Cloud
  api_key: # required for Ollama Cloud, OpenAI, and Anthropic
```

> **YAML indentation matters.** The keys under `provider:` must be
> indented 2 spaces. If they are at the root level, omega will not
> find them and report "provider.model_name is required".

## Module toggles

The opinionated capsules (kanban, cron, delegate, verdict, mcp,
trust, notifications, memory, continuous) are
toggleable: the webui settings pane's MODULES section flips them, or
you can name them in config.yaml directly:

```yaml
disabled:
  - kanban
  - cron
```

A `disabled:` entry skips that capsule at boot (its routes answer 404).
Machine-critical capsules (provider, store, loop, logging) and the
engine ring (http_channel, tui) are not on the list — booting with
them disabled fails loud. The v0.12 `omega.json` sidecar is folded
into this key at boot and deleted; an unknown entry fails the boot
naming it.

## Reference

| Key                           | Env var                            | Default                  | Description                                                                                                                                                                                                                                               |
| ----------------------------- | ---------------------------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --- |
| -                             | `OMEGA_HOME`                       | Binary directory         | Omega home: config, db, skills, modules live here                                                                                                                                                                                                         |
| `provider.type`               | `OMEGA_PROVIDER`                   | `ollama`                 | Provider: `ollama`, `openai`, `anthropic`                                                                                                                                                                                                                 |
| `provider.model_name`         | `OMEGA_MODEL`                      | (required)               | Model name                                                                                                                                                                                                                                                |
| `provider.host`               | `OMEGA_HOST`                       | `http://localhost:11434` | Provider base URL                                                                                                                                                                                                                                         |
| `provider.api_key`            | `OMEGA_API_KEY`                    |                          | API key (Ollama Cloud, OpenAI, Anthropic). Falls back to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`                                                                                                                                                           |
| `server.port`                 | `OMEGA_PORT`                       | `8099`                   | HTTP listen port                                                                                                                                                                                                                                          |
| `server.bind`                 | -                                  | `127.0.0.1`              | Addresses to listen on (one channel each). Loopback only by default - the API is unauthenticated; extra addresses (e.g. a Tailscale IP) are an explicit opt-in for reverse-proxy routing                                                                  |     |
| `server.run_timeout`          | -                                  | `1800`                   | Wall-clock cap per detached chat run, in seconds. A hung tool call ends the run instead of holding the session's run slot until restart; applies on restart                                                                                               |     |
| `store.db_path`               | `OMEGA_DB_PATH`                    | `<home>/data/omega.db`   | SQLite session database path; relative defaults resolve to `<home>/data/` and a legacy home-root db is migrated there on boot                                                                                                                            |
| `compaction.enabled`          | `OMEGA_COMPACTION_ENABLED`         | `true`                   | Enable context compaction                                                                                                                                                                                                                                 |
| `compaction.threshold`        | `OMEGA_COMPACTION_THRESHOLD`       | `0.6`                    | Fraction of context window that triggers compaction                                                                                                                                                                                                       |
| `compaction.context_window`   | `OMEGA_COMPACTION_CONTEXT_WINDOW`  | `32768`                  | Model context window in tokens. Fallback when the provider doesn't auto-discover it (Ollama auto-discovers via `/api/show`)                                                                                                                               |
| `compaction.keep_first`       | `OMEGA_COMPACTION_KEEP_FIRST`      | `2`                      | Messages preserved verbatim at start                                                                                                                                                                                                                      |
| `compaction.keep_last`        | `OMEGA_COMPACTION_KEEP_LAST`       | `10`                     | Messages preserved verbatim at end                                                                                                                                                                                                                        |
| `compaction.reserve_tokens`   | `OMEGA_COMPACTION_RESERVE_TOKENS`  | `16384`                  | Tokens reserved for the model response                                                                                                                                                                                                                    |
| `compaction.max_tool_output`  | `OMEGA_COMPACTION_MAX_TOOL_OUTPUT` | `32768`                  | Maximum bytes of tool output before truncation                                                                                                                                                                                                            |
| `skills.dir`                  | `OMEGA_SKILLS_DIR`                 | `<home>/skills`          | Skills directory                                                                                                                                                                                                                                          |
| `memory.enabled`              | `OMEGA_MEMORY_ENABLED`             | `true`                   | Enable persistent memory                                                                                                                                                                                                                                  |
| `memory.user_profile_enabled` | `OMEGA_USER_PROFILE_ENABLED`       | `true`                   | Enable user profile memory                                                                                                                                                                                                                                |
| `memory.char_limit`           | `OMEGA_MEMORY_CHAR_LIMIT`          | `2200`                   | Max characters for memory entries                                                                                                                                                                                                                         |
| `memory.user_char_limit`      | `OMEGA_USER_CHAR_LIMIT`            | `1375`                   | Max characters for user profile entries                                                                                                                                                                                                                   |
| `memory.file`                 | -                                  | `<home>/memory.md`       | Memory file path                                                                                                                                                                                                                                          |
| `memory.user_file`            | -                                  | `<home>/user.md`         | User profile file path                                                                                                                                                                                                                                    |
| `logging.enabled`             | `OMEGA_LOGGING_ENABLED`            | `true`                   | Enable operational logging                                                                                                                                                                                                                                |
| `logging.file`                | `OMEGA_LOGGING_FILE`               | `<home>/omega.log`       | Log file path                                                                                                                                                                                                                                             |
| `http_timeout`                | `OMEGA_HTTP_TIMEOUT`               | `300`                    | HTTP timeout for provider requests (seconds)                                                                                                                                                                                                              |
| `max_turns`                   | `OMEGA_MAX_TURNS`                  | `100`                    | Maximum tool-call turns per run                                                                                                                                                                                                                           |
| `system_prompt`               | -                                  | (empty)                  | Extra custom prompt lines appended to the system prompt (applies on restart); the CLI's repeatable `--append-system-prompt` flag does the same per-run                                                                                                   |
| `web.base_url`                | -                                  | provider host (trailing `/v1` stripped) | Override for the web tools' endpoint host (`web.search` / `web.fetch` ride the provider mirror with the same auth as every other call when unset) |
| `kanban.db_path`              | -                                  | `<home>/data/kanban.db`  | Task-board SQLite path; relative defaults resolve against the home like `store.db_path`                                                                                                   |
| `theme`                       | `OMEGA_THEME`                      | `dark`                   | TUI color theme (dark, light, auto)                                                                                                                                                                                                                       |
| `vision_model`                | `OMEGA_VISION_MODEL`               | -                        | Captioner model for image messages: images are described once (persisted, content-addressed) and the main model sees the caption as text. Empty = images ride to the main model unchanged (a model that cannot see them fails loudly). Applies on restart |
| `title_model`                 | `OMEGA_TITLE_MODEL`                | -                        | Cheap model for session-title generation (provisional + final). Empty = the main model. Applies on restart                                                                                                                                                |
| `notifications`               | `OMEGA_NOTIFICATIONS`              | `bell`                   | Turn-complete notification (bell, desktop, off)                                                                                                                                                                                                           |
| `disabled`                    | -                                  | (empty)                  | Toggleable capsules to keep off (e.g. `- kanban`). The webui MODULES pane writes the same key; an unknown name fails the boot. Legacy `omega.json` sidecars fold into this key on first boot and are deleted                                               |
| `continuous.wake_turn_budget` | -                                  | `0`                      | Per-wake turn budget a continuous agent's spawned loop gets (mapped onto `OMEGA_MAX_TURNS` in the child). 0 = the config's own `max_turns` (ruling 17: the ONLY budget - no daily cap, no wallet)                                                        |
| `continuous.wake_timeout_seconds` | -                              | `1800`                   | Wall-clock fence per wake subprocess: a wedged wake is killed, never queued (crash isolation ruling 9)                                                                                                                                                    |

## Continuous agents

Module capsules own their config sections; the continuous capsule's
knobs sit in the reference table above. The rest of the shape is
file-based, not config: agent homes live at `<home>/agents/<name>/`
(folder presence is registration - there is no registry file), each
holding its own `config/` (a config.yaml layer over the home config
plus that agent's own human-owned `crons.yaml` - the agent has no tool
that touches it, ruling 15), `data/` (its own session, notification,
board, and evidence-ledger stores), `skills/`, `user.md`, `memory.md`,
and `<name>.log`. Schedules ride the cron module's WHEN; the wake
subprocess and the report shape live in the continuous module. The
webui roster pane + detail strip (ruling 18) ride the module's REST
face: `GET /continuous/roster`, `GET|POST /continuous/{name}/chat`,
`GET /continuous/{name}/evidence`, `POST /continuous/{name}/sleep`,
`POST /continuous` (create).

## Providers

| Provider       | Type        | Requires      | Default Host                   |
| -------------- | ----------- | ------------- | ------------------------------ |
| Ollama (local) | `ollama`    | Local install | `http://localhost:11434`       |
| Ollama Cloud   | `ollama`    | API key       | `https://ollama.com`           |
| OpenAI         | `openai`    | API key       | `https://api.openai.com/v1`    |
| Anthropic      | `anthropic` | API key       | `https://api.anthropic.com/v1` |

Ollama supports three connection modes:

- **Local** - Default. Set `host: http://localhost:11434`, leave
  `api_key` empty.
- **Cloud via local proxy** - Keep `host` as localhost. Your local
  Ollama instance handles cloud auth transparently (e.g.
  `ollama run gpt-oss:120b-cloud`).
- **Cloud direct** - Set `host: https://ollama.com` and
  `api_key: <your-key>`. omega sends a Bearer token in the
  Authorization header.

Switch providers at runtime in the TUI:

```txt
/provider openai
/model gpt-4o
```
