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
  type: ollama # ollama, openai, anthropic, or mock (deterministic, no network - tests and journeys)
  model_name: # required - e.g. llama3, gpt-4o, claude-sonnet-4-20250514
  host: http://localhost:11434 # use https://ollama.com for Ollama Cloud
  api_key: # required for Ollama Cloud, OpenAI, and Anthropic

# Named provider instances (optional): the map key IS the instance id,
# so two accounts of one type are two entries. The provider: section
# above stays the default instance.
providers:
  backup: # instance id (lowercase letters, digits, - or _)
    type: openai
    model_name: gpt-4o
    host: https://api.openai.com/v1
    api_key: sk-...
```

> **YAML indentation matters.** The keys under `provider:` must be
> indented 2 spaces. If they are at the root level, omega will not
> find them and report "provider.model_name is required".

**Named instances.** Each `providers:` entry is one named instance
(key = the id, must be lowercase letters/digits/`-`/`_`; `model_name`
is required per instance; `default` is reserved). Instances apply on
restart, like the rest of the provider section. The settings pane
maintains them through the `provider.instances` list editor (rows can
be renamed, retyped and removed; a row's name is the instance id).
A row's host and api_key are config.yaml-only - the pane keeps them
round-tripped but refuses changes with the config.yaml pointer. A
session pinned to an
instance that is later removed fails its next send LOUDLY (400, "not
configured") - it never falls back silently to the default; clearing
the pin restores the default. Send a run through an instance with the
`/chat` `provider` field (see [api.md](api.md)).

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
serving shell (http_channel) are not on the list — booting with
them disabled fails loud. The v0.12 `omega.json` sidecar is folded
into this key at boot and deleted; an unknown entry fails the boot
naming it.

## Reference

| Key                           | Env var                            | Default                  | Description                                                                                                                                                                                                                                               |
| ----------------------------- | ---------------------------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --- |
| -                             | `OMEGA_HOME`                       | Binary directory         | Omega home: config, db, skills, modules live here                                                                                                                                                                                                         |
| `provider.type`               | `OMEGA_PROVIDER`                   | `ollama`                 | Provider: `ollama`, `openai`, `anthropic`, `mock`
| `provider.model_name`         | `OMEGA_MODEL`                      | (required)               | Model name                                                                                                                                                                                                                                                |
| `provider.host`               | `OMEGA_HOST`                       | `http://localhost:11434` | Provider base URL                                                                                                                                                                                                                                         |
| `provider.api_key`            | `OMEGA_API_KEY`                    |                          | API key (Ollama Cloud, OpenAI, Anthropic). Falls back to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`                                                                                                                                                           |
| `server.port`                 | `OMEGA_PORT`                       | `8099`                   | HTTP listen port                                                                                                                                                                                                                                          |
| `server.bind`                 | -                                  | `127.0.0.1`              | Addresses to listen on (one channel each). Loopback only by default - the API is unauthenticated; extra addresses (e.g. a Tailscale IP) are an explicit opt-in for reverse-proxy routing                                                                  |     |
| `server.public_hosts`         | -                                  | `[]`                     | Identities the Host/Origin guard answers off the bind (a tunnel/proxy domain). Exact match, no wildcards. A wildcard bind (`0.0.0.0`, `::`) trusts ONLY this list and refuses to start when it is empty - docs/api.md carries the full guard law |     |
| `server.run_timeout`          | -                                  | `1800`                   | Wall-clock cap per detached chat run and per cron fire, in seconds. A hung tool call ends the run instead of holding the session's run slot until restart; a chat run takes a change on restart, a cron fire on its next fire (the host pushes its resolved value to the scheduler, so a `--config` path or an exe-dir home is never misread) |     |
| `server.default_workspace`    | -                                  | (unset)                  | The folder workspace-less sessions run their tools against: shell, files and process root here, `@`-mentions expand from it, and the webui's workspace chip mirrors it. A pinned session workspace wins; a vanished folder degrades to the unset case. Freshly spawned terminal panes start here too (the focused session's pinned root wins, else this knob, else the process cwd - t_62d72028)       |     |
| `store.db_path`               | `OMEGA_DB_PATH`                    | `<home>/data/omega.db`   | SQLite session database path; relative defaults resolve to `<home>/data/` and a legacy home-root db is migrated there on boot                                                                                                                            |
| `compaction.enabled`          | `OMEGA_COMPACTION_ENABLED`         | `true`                   | Enable context compaction                                                                                                                                                                                                                                 |
| `compaction.threshold`        | `OMEGA_COMPACTION_THRESHOLD`       | `0.6`                    | Fraction of context window that triggers compaction                                                                                                                                                                                                       |
| `compaction.context_window`   | `OMEGA_COMPACTION_CONTEXT_WINDOW`  | `32768`                  | Model context window in tokens. Fallback when the provider doesn't auto-discover it (Ollama auto-discovers via `/api/show`)                                                                                                                               |
| `compaction.keep_first`       | `OMEGA_COMPACTION_KEEP_FIRST`      | `2`                      | Messages preserved verbatim at start                                                                                                                                                                                                                      |
| `compaction.keep_last`        | `OMEGA_COMPACTION_KEEP_LAST`       | `10`                     | Messages preserved verbatim at end                                                                                                                                                                                                                        |
| `compaction.reserve_tokens`   | `OMEGA_COMPACTION_RESERVE_TOKENS`  | `16384`                  | Tokens reserved for the model response                                                                                                                                                                                                                    |
| `compaction.max_tool_output`  | `OMEGA_COMPACTION_MAX_TOOL_OUTPUT` | `32768`                  | Maximum bytes of tool output before truncation                                                                                                                                                                                                            |
| `skills.dir`                  | `OMEGA_SKILLS_DIR`                 | `<home>/skills`          | Skills directory                                                                                                                                                                                                                                          |
| `skills.nudge_interval`       | -                                  | `15`                     | Learning-loop nudge cadence: after this many write-class tool calls without a persisted lesson (a `skills.write`/`skills.update`), the next turn prep carries one reminder line. `0` = nudging off. The reminder only ARMS on write-class calls - reads, greps, searches and `shell.run` never arm it, so a read-only probe pass receives zero nudges (t_d5d4d586) |
| `memory.enabled`              | `OMEGA_MEMORY_ENABLED`             | `true`                   | Enable persistent memory                                                                                                                                                                                                                                  |
| `memory.user_profile_enabled` | `OMEGA_USER_PROFILE_ENABLED`       | `true`                   | Enable user profile memory                                                                                                                                                                                                                                |
| `memory.char_limit`           | `OMEGA_MEMORY_CHAR_LIMIT`          | `2200`                   | Max characters for memory entries                                                                                                                                                                                                                         |
| `memory.user_char_limit`      | `OMEGA_USER_CHAR_LIMIT`            | `1375`                   | Max characters for user profile entries                                                                                                                                                                                                                   |
| `memory.file`                 | -                                  | `<home>/memory.md`       | Memory file path                                                                                                                                                                                                                                          |
| `memory.user_file`            | -                                  | `<home>/user.md`         | User profile file path                                                                                                                                                                                                                                    |
| `logging.enabled`             | `OMEGA_LOGGING_ENABLED`            | `true`                   | Enable operational logging                                                                                                                                                                                                                                |
| `logging.file`                | `OMEGA_LOGGING_FILE`               | `<home>/omega.log`       | Log file path                                                                                                                                                                                                                                             |
| `http_timeout`                | `OMEGA_HTTP_TIMEOUT`               | `300`                    | HTTP timeout for provider requests (seconds)                                                                                                                                                                                                              |
| `max_turns`                   | `OMEGA_MAX_TURNS`                  | `100`                    | Maximum tool-call turns per run (applies live - the next run reads it)                                                                                                                                                                                     |
| `system_prompt`               | -                                  | (empty)                  | Extra custom prompt lines appended to the system prompt (applies live - the next run reads it); the CLI's repeatable `--append-system-prompt` flag does the same per-run                                                                   |
| `web.base_url`                | -                                  | provider host (trailing `/v1` stripped) | Override for the web tools' endpoint host (`web.search` / `web.fetch` ride the provider mirror with the same auth as every other call when unset) |
| `kanban.db_path`              | -                                  | `<home>/data/kanban.db`  | Task-board SQLite path; relative defaults resolve against the home like `store.db_path`                                                                                                   |
| `vision_model`                | `OMEGA_VISION_MODEL`               | -                        | Captioner model for image messages: when the SELECTED main model is vision-capable (the model registry's `vision` capability flag), the pixels go to the main model directly and this knob is not consulted. Otherwise images are described once (persisted, content-addressed) and the main model sees the caption as text. `default` = follow the model the current chat runs (the composer's model chip), resolved per caption call - the word persists as chosen, so a main-model switch carries the aux lane with it (settings key `provider.vision_model`, offered as the picker's first entry). Useful only when that model can see images: the caption hop exists for a blind main model, so `default` on a blind one sends the image there and the run fails on the caption call. Empty = images ride to the main model unchanged (a model that cannot see them fails loudly). Applies on restart |
| `title_model`                 | `OMEGA_TITLE_MODEL`                | -                        | Cheap model for session-title generation (provisional + final). Empty = the main model. `default` = the model the current chat runs, resolved per title call (settings key `provider.title_model`, offered as the picker's `default` entry). Applies on restart |
| `compaction_model`            | -                                  | the session model        | Model the compaction summarizer consults (settings key `provider.compaction_model`). `instance:model` routes the consult through a named provider instance. Empty or `default` = the session provider model, resolved per consult (the picker offers `default`). Applies live |
| `summarizer_model`            | -                                  | the session model        | Model the hygiene summarizer consults (settings key `provider.summarizer_model`). `instance:model` routes the consult through a named provider instance. Empty or `default` = the session provider model, resolved per sweep (the picker offers `default`). Applies live - the next sweep consults it |
| `skills_model`                | -                                  | the session model        | Model the weekly skills curator consults for merge/distill proposals (settings key `provider.skills_model`). `instance:model` routes the consult through a named provider instance. Empty or `default` = the session provider model, resolved per consult (the picker offers `default`). Applies live - the next curator action consults it |
| `approvals_model`             | -                                  | -                        | Decision model the smart approval gate consults (settings key `provider.approvals_model`; full story in [approvals.md](approvals.md)). Empty = fail-closed (every gated command escalates). Applies live. NO `default` entry: an approval decision is a deliberate choice, not an inherited one |
| `disabled`                    | -                                  | (empty)                  | Toggleable capsules to keep off (e.g. `- kanban`). The webui MODULES pane writes the same key; an unknown name fails the boot. Legacy `omega.json` sidecars fold into this key on first boot and are deleted                                               |
| `continuous.wake_turn_budget` | -                                  | `0`                      | Per-wake turn budget a continuous agent's spawned loop gets (mapped onto `OMEGA_MAX_TURNS` in the child). 0 = the config's own `max_turns` (ruling 17: the ONLY budget - no daily cap, no wallet)                                                        |
| `continuous.wake_timeout_seconds` | -                              | `1800`                   | Wall-clock fence per wake subprocess: a wedged wake is killed, never queued (crash isolation ruling 9)                                                                                                                                                    |
| `sessions.retention_days`     | -                                  | `90`                     | The hygiene job deletes sessions inactive longer than this many days (pinned sessions are exempt). 0 = never prune. Applies live - the section rides the same hot-apply push as `summarizer_model`. |
| `sessions.summarize_after_hours` | -                               | `24`                     | Settled sessions without a summary get an LLM-written one (one short paragraph, searchable) once idle at least this many hours. 0 = the default. Applies on restart                                                                                        |

## Session hygiene

The hygiene pass (a cron job's agent turn calling the
`sessions.hygiene` tool, one job definition in `crons.yaml.example`,
seeded `enabled: false`) is the end-of-life service for the session
store: it summarizes settled sessions that lack a summary (one LLM call
per session, at most 5 per pass - the backlog clears across runs),
deletes sessions past `sessions.retention_days` (by last activity,
pinned exempt, children cascade), and reclaims disk: a WAL
`TRUNCATE` checkpoint plus the incremental auto-vacuum conversion on
the first pass. Session summaries ride the unified search
(`/sessions/search`, the `summary` hit class alongside label and
content) and the session list payload. The pass's counts and the
resulting db + WAL file sizes surface in insights. The compactor is a
different organ - in-context compaction is untouched.

## Continuous agents

Module capsules own their config sections; the continuous capsule's
knobs sit in the reference table above. The rest of the shape is
file-based, not config: agent homes live at `<home>/agents/<name>/`
(folder presence is registration - there is no registry file), each
holding its own `config/` (a config.yaml layer over the home config
plus that agent's own `crons.yaml` - the agent has no tool
that touches it, ruling 15; only the webui toggle writes the per-job
`enabled:` flag (POST /cron/toggle), `data/` (its own session, notification,
board, and evidence-ledger stores), `skills/`, `user.md`, `memory.md`,
and `<name>.log`. Data isolation re-points an unset store at the agent's
own `data/omega.db`; an env-set `OMEGA_DB_PATH` is caller intent and is
never re-pointed (the delegate child's store pin, see `docs/cli.md`
`omega run --session`). Schedules ride the cron module's WHEN; the wake
subprocess and the report shape live in the continuous module. The
webui roster pane + detail strip (ruling 18) ride the module's REST
face: `GET /continuous/roster`, `GET|POST /continuous/{name}/chat`,
`GET /continuous/{name}/evidence`, `POST /continuous/{name}/sleep`,
`POST /continuous` (create).

## Cron jobs (crons.yaml)

`<home>/config/crons.yaml` holds the scheduled jobs; an agent home
carries its own copy in `agents/<name>/config/`. Every job runs its
prompt as a full agent turn in a session labeled `cron:<name>`.

| Field       | Meaning                                                                                     |
| ----------- | ------------------------------------------------------------------------------------------- |
| `name`      | required, unique, `[a-z0-9-]`                                                               |
| `kind`      | `cron` (default) or `interval`                                                              |
| `schedule`  | required iff kind `cron` — the 5-field cron grammar (minute hour day month weekday), server-local time; no seconds, no `@descriptors` |
| `minutes`   | required iff kind `interval` — integer >= 1; the job fires every N minutes on the minute grid |
| `agent`     | optional — the agent home the fire runs under                                               |
| `model`     | optional — the provider model for this fire                                                 |
| `prompt`    | required, the user message the run sends                                                    |
| `skills`    | optional list of skill names - their bodies ride the fire prompt ahead of the prompt (main-home jobs only; agent homes reject the field) |
| `deliver`   | `new-session` (default) \| `continuous-agent` \| `append-to-file`                            |
| `file`      | the append-to-file target (home-relative); empty = the journal naming                        |
| `enabled`   | default true                                                                                |

**The defaulting law** (t_ecec864e): an empty `agent` runs the fire
under the default agent; an empty `model` uses the session's provider
model. Empty = inherit, never an error. A bad agent name is a clear
fire-time error in the job log — the scheduler keeps standing.

Deliver targets: `new-session` is a fresh session (the default);
`continuous-agent` is a wake through the event spine into an existing
continuous agent session (set `agent: <name>`); `append-to-file`
appends the run's final message to `file`. Legacy values
(`none`, `journal`, `memory`) still parse but are deprecated.

omega runs the scheduler at serve; jobs hot-reload from the file.
The webui Cron tab edits every field through `POST /cron/save` +
`/cron/delete` (schema-validated upserts) and the row toggle via
`POST /cron/toggle`, which writes only the per-job `enabled:` flag
(one click, card t_5051d523).
An agent-home wake job is refused by the editor (the cross-home law);
the refusal reads the row's `agent_owned` mark, so a main-home job
that authors its own `agent:` stays editable (t_21824d98).
omega can propose jobs in conversation but never writes the file
itself.

**Last status** (t_3885c975): every fire records its outcome
(`ok` / `error` with the capped message) into `cronstate.json`, keyed
on the same `<agent>/<name>` state keys. The GET `/cron/jobs` rows, the
Cron tab and the `/cron` table carry `status`, `last` (the completion
wall clock, RFC3339) and `status_note`; the record survives restarts.
A never-fired job carries no status. A pinned skill the skills capsule
cannot read is a fire-time error — no run on a half-set table, the
scheduler keeps standing.

## The approval gate

The approval capsule (`harness/approval/`, ship-dark default
`manual`) takes two config.yaml knobs (full story:
[approvals.md](approvals.md)); the decision model rides the provider
settings (the `approvals_model` slot, above):

| Key                  | Default   | Meaning                                                                                      |
| -------------------- | --------- | -------------------------------------------------------------------------------------------- |
| `approvals.mode`     | `manual`  | `off` \| `manual` \| `smart`. Empty = manual                                                  |
| `approvals.smart_policy` | -     | The operator policy text the reviewer sees (never command text)                              |

## The shell registry

There is ONE shell domain (card t_5a49790b): `shell.run` starts a
command DETACHED under the shell registry (the machinery the retired
process capsule was built on), so a call that runs past the loop's
inline budget converts to a pending call and the child keeps running -
it survives the turn, and it survives an omega restart. There are no
modes and no background flag: everything is `shell.run`.

Addressing is by id, and `shell.list` is the list that makes ids cheap:

| Verb         | What it answers                                                     |
| ------------ | ------------------------------------------------------------------- |
| `shell.run`  | Start a command (optional `dir`); its output returns as the result  |
| `shell.list` | Running shells: id, command, age, log path                          |
| `shell.read` | Tail one shell's log by id - live, and after it exits               |
| `shell.kill` | Stop one shell by id: SIGTERM, then SIGKILL after the grace         |

A forever-server is just a call that never settles - permanently
pending, listed, readable, killable. Anything that finishes in the
inline window returns inline with zero overhead.

State: `<home>/data/process.json`; log: one `<id>.log` per shell at
`<home>/logs/process/` (both names are historical - the files carry the
pre-unification spelling so logs and state survive the change); a pid
gone at boot = exited.

Log retention (stated loud, card t_304afb4e): the log SURVIVES the
shell - `shell.read` works after the kill or the exit (the post-mortem
path, the main reason to read a log). A finished log is trimmed to
`process.log_max_bytes` once at exit (the TAIL is kept - read serves
the tail). Re-using an exited id rotates its log to `<id>.log.1` (one
generation back) and starts a fresh file. An exited entry and its logs
are dropped automatically a week after the shell finished - that sweep
is the registry's only GC (there is no removal verb; deleting a cron is
not vocabulary a tool needs).

Two config.yaml knobs (absent section = the defaults; `0` or unset
means the DEFAULT, never "disabled" - the same zero semantics as
`compaction.context_window: 0 = auto`). The section keeps its historical
name `process:` because it is the shell registry's knob row:

| Key                     | Default   | Meaning                                                     |
| ----------------------- | --------- | ----------------------------------------------------------- |
| `process.log_max_bytes` | `5242880` | Size cap for one finished log (5 MB), enforced at exit - the tail is kept |
| `process.kill_grace_seconds` | `10` | How long a SIGTERM kill waits before escalating to SIGKILL  |

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

Provider changes (`provider.type`, `provider.api_key`,
`providers:` instances) apply on restart. Per-run model changes ride
the webui: the composer's model chip and each `/chat` request's
`model` field build a provider copy for that run — the running
sessions never share model state (see [api.md](api.md)).
