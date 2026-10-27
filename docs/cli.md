# CLI Reference

`omega` is a single binary. One global help covers all subcommands
(`omega --help`); per-subcommand help appears as the subcommands gain
flags. Everything below is generated from the parser in `cmd/omega` —
if a flag is not in the code, it is not in this document.

## Usage

```txt
omega                 start the interactive TUI (no-arg default)
omega <path>          start the TUI in <path> (chdir first)
omega chat            explicit alias for the TUI
omega run <prompt>    run one prompt to stdout
omega serve           start the HTTP server (SSE streaming)
omega export <id|label> [output]   export a session as JSONL
omega insights [--days N]          session usage analytics (default: 30 days)
omega agents                       list registered agents; `omega agents create <name>` scaffolds one
omega update              report N versions behind + prompt y/N
omega update --yes        install the latest stable without a prompt
omega update --rollback   swap back to omega.bak + health-verify
omega rollback            alias for omega update --rollback
omega health          check the server at the configured port
omega kanban          the task board registry (see: omega kanban --help)
```

## Subcommands

Module-owned subcommands (v0.12 phase D): the dispatch table is the
mount registry. A capsule registers its own name + handler at mount,
parses its own argv (including its own `--help`), and the global
`omega --help` lists module subcommands by registry introspection.
Deleting a module removes its registration: the subcommand vanishes
from `omega --help` and an argument that matches nothing falls back to
the project-path behavior.

### `omega` / `omega chat` (TUI)

No argument, `chat`, or a non-subcommand argument launches the Bubble
Tea TUI. A non-subcommand first argument is treated as a project path:
omega chdirs there first, so project context (AGENTS.md walk) and file
tools resolve relative to that directory.

```bash
omega                       # TUI in the current directory
omega ~/dev/omega           # TUI rooted at a project
```

In-TUI slash commands (`/new`, `/variant`, `/branch`, `/files`, ...)
are documented in [tui.md](tui.md).

### `omega serve`

Loads config, wires the extensions, starts all mounted channels (HTTP,
etc.) and the cron runner until SIGINT/SIGTERM. Config file changes are
polled every 5 s and non-disruptive knobs (http timeout, compaction)
hot-apply without restart.

```bash
omega serve --config ~/.omega/config/config.yaml
```

The HTTP channel serves the embedded webui at `/` plus the JSON/SSE API
([api.md](api.md)) on `server.port` (default 8099).

### `omega run`

One prompt, final response to stdout. Takes `--append-system-prompt`
(repeatable), image file paths (attached to the message), and the
global `--config`/trust flags. A run with no prompt argument is an
error.

```bash
omega run "explain the build failure in build.log"
omega run --append-system-prompt "Be terse." --append-system-prompt "Cite files." "what changed"
omega run fix this @docs/cli.md    # file args ride along as context/images
```

Exit code is 1 and `omega: agent: <error>` prints to stderr when the
run ends with an error event.

### `omega export`

Writes a session's messages as JSONL (one `{"role","content"}` object
per line). The argument resolves first as an exact session ID, then as
a case-insensitive label prefix; ambiguous prefixes error. Output is
`<arg>.jsonl` unless a second path (`-` for stdout) is given.

```bash
omega export 77f2 session.jsonl
omega export "fix the gate" -        # label prefix match, dump to stdout
```

### `omega insights`

Aggregates session usage over the last N days (sessions, tool calls,
tokens). Nothing in the window prints "No sessions in the last N days."

```bash
omega insights --days 90
```

### `omega agents`

Lists the registered agent homes (name, description, model, skill
count, memory size, directory). Main is the home root itself: its
`memory.md`/`user.md` live at `~/.omega/` (boot retires the former
`agents/default/` profile and moves its persona files UP, exactly
once). `create` scaffolds a continuous agent home `~/.omega/agents/<name>/`
holding `config/config.yaml` (a commented layer over the home config),
`data/` (the agent's own stores - full data isolation), and an empty
`skills/`; `user.md`, `memory.md`, and `<name>.log` are created on
first use. Wake schedules live in the agent's own `config/crons.yaml`.

```bash
omega agents
omega agents create scout "fast probe agent"
omega agents create                    # fails: name required
```

### `omega update`

Downloads a release from GitHub (`EndoTheDev/omega` releases), verifies
the archive against the release's `checksums.txt` when present, and
replaces the running binary in place, preserving the replaced binary as
`omega.bak` (one generation). Data files (`extensions/` payloads) and
the `.example` config seeds copy over without overwriting anything that
exists.

```bash
omega update                      # report N versions behind, then prompt y/N
omega update --yes                # install without a prompt
omega update --nightly            # newest -nightly.* prerelease (same prompt contract)
omega update --allow-downgrade    # permit installing a lower version
omega update --rollback           # swap back to omega.bak, verify it runs
omega rollback                    # same swap
```

Prompt contract: a bare update never installs silently. It fetches,
prints how far behind it is (version/tag distance over the channel's
release list), then asks y/N. Only an explicit y installs. A non-tty
stdin or an EOF declines immediately with a hint to run
`omega update --yes` - an unattended bare update never replaces the
binary; `--yes` is the scripted path and consults no terminal.

Downgrade protection: an offered tag older than the running version is
refused (a stale mirror must never quietly revert a box) unless
`--allow-downgrade` is passed. Rollback verifies the restored binary by
executing `omega --version` before declaring success. Unknown flags
after `update` are errors, not silent falls through to a plain update.

Version resolution: builds carry the version via ldflags
(`-X main.omegaVersion=<tag>-<shorthash>`, per build.sh); `go install`
builds fall back to the module version Go stamps, already collapsed to
the same short tag-hash shape (`v0.9.1-nightly.20261022-c268097e`); a
bare `go build` reports `dev`.

### `omega health`

GETs `<host>:<port>/health` with a 5 s client timeout. `ok: <url>` on
200; exit 1 with the failure otherwise. The host comes from
`OMEGA_HEALTH_HOST` (default `localhost`), the port from config — so
`omega health --config <path>` checks the config's install, not
whatever is on 8098.

```bash
OMEGA_HEALTH_HOST=127.0.0.1 omega health
```

## Global flags

Accepted before or after the subcommand:

| Flag                          | Meaning                                                                 |
| ----------------------------- | ----------------------------------------------------------------------- |
| `--config <path>`             | Config file (default: `<home>/config.yaml`, then `<home>/config/config.yaml`; legacy home-root wins) |
| `--append-system-prompt <text>` | Append to the system prompt; repeatable; `run`/TUI only               |
| `--approve`                   | Trust the current project's AGENTS.md                                   |
| `--no-approve`                | Skip the current project's AGENTS.md                                    |
| `--version`, `-v`             | Print version                                                           |
| `--help`, `-h`                | Print the global help                                                   |

`--config` is the only flag with a value; both `--config <p>` and
`--config=<p>` forms work. No subcommand has its own `--help` —
flags are per-subcommand documented here.

## Home resolution

omega runs everything out of one **home** directory (config, databases,
skills, themes, memory files, logs). Resolution order:

1. `OMEGA_HOME` when set.
2. The binary's own directory for **release-folder installs** —
   self-contained unpacks (also a repo checkout's `bin/`).
3. `~/.omega` for **PATH-bin installs** — when the binary lives in a
   known shared bin dir (`~/go/bin`, `~/.local/bin`, `~/bin`,
   `/usr/local/bin`), so config/db don't scatter among other programs'
   binaries.

Inside the home: `config/` holds the yaml files (`config.yaml`,
`mcp.yaml`, `crons.yaml`), `data/` holds the databases (`omega.db`,
`kanban.db`) — a legacy home-root database moves into `data/` on first
boot of the new version. `memory.md`, `user.md`, `skills/`, `themes/`,
and the log live at the home root. First run seeds `config/` with the
embedded `.example` files (never overwritten).

The working directory is used only for AGENTS.md project context and
tool file operations.

## Environment variables

### Config overrides

Applied after config.yaml, before validation. Booleans accept `1`/`true`;
integers below their minimum are ignored (the YAML/default value stays).

| Env var                          | Config key                        | Default           |
| -------------------------------- | --------------------------------- | ----------------- |
| `OMEGA_PROVIDER`                 | `provider.type`                   | `ollama`          |
| `OMEGA_API_KEY`                  | `provider.api_key`                | (empty)           |
| `OMEGA_MODEL`                    | `provider.model_name`             | (required)        |
| `OMEGA_HOST`                     | `provider.host`                   | `http://localhost:11434` |
| `OMEGA_VISION_MODEL`             | `vision_model`                    | (empty)           |
| `OMEGA_TITLE_MODEL`              | `title_model`                     | (empty)           |
| `OMEGA_DB_PATH`                  | `store.db_path`                   | `<home>/data/omega.db` |
| `OMEGA_SKILLS_DIR`               | `skills.dir`                      | `<home>/skills`   |
| `OMEGA_THEME`                    | `theme`                           | (empty)           |
| `OMEGA_NOTIFICATIONS`            | `notifications`                   | `bell`            |
| `OMEGA_LOGGING_FILE`             | `logging.file`                    | `<home>/omega.log` |
| `OMEGA_COMPACTION_ENABLED`       | `compaction.enabled`              | `true`            |
| `OMEGA_MEMORY_ENABLED`           | `memory.enabled`                  | `true`            |
| `OMEGA_USER_PROFILE_ENABLED`     | `memory.user_profile_enabled`     | `true`            |
| `OMEGA_LOGGING_ENABLED`          | `logging.enabled`                 | `true`            |
| `OMEGA_PORT`                     | `server.port`                     | `8099`            |
| `OMEGA_COMPACTION_CONTEXT_WINDOW`| `compaction.context_window`       | `32768` (0 = auto-probe) |
| `OMEGA_COMPACTION_KEEP_FIRST`    | `compaction.keep_first`           | `2`               |
| `OMEGA_COMPACTION_KEEP_LAST`     | `compaction.keep_last`            | `10`              |
| `OMEGA_COMPACTION_RESERVE_TOKENS`| `compaction.reserve_tokens`       | `16384`           |
| `OMEGA_COMPACTION_MAX_TOOL_OUTPUT`| `compaction.max_tool_output`     | `32768`           |
| `OMEGA_HTTP_TIMEOUT`             | `http_timeout`                    | `300` (seconds)   |
| `OMEGA_MAX_TURNS`                | `max_turns`                       | `100`             |
| `OMEGA_MEMORY_CHAR_LIMIT`        | `memory.char_limit`               | `2200`            |
| `OMEGA_USER_CHAR_LIMIT`          | `memory.user_char_limit`          | `1375`            |
| `OMEGA_COMPACTION_THRESHOLD`     | `compaction.threshold`            | `0.6` (0 < t <= 1)|

### Plumbing (set or read by the binary itself)

| Env var                | Direction | Meaning                                                                    |
| ---------------------- | --------- | -------------------------------------------------------------------------- |
| `OMEGA_HOME`           | read      | Overrides home resolution (see above); re-exported by the binary so child processes inherit it |
| `OMEGA_SKILLS_DIR`     | set       | Re-exported every boot as `<home>/skills` for the skills extension         |
| `OMEGA_BIN`            | set       | The binary's own path, for the delegate extension's subagent spawns        |
| `OMEGA_HEALTH_HOST`    | read      | Host for `omega health` (default `localhost`)                              |
| `OMEGA_UPDATE_API_BASE`| read      | Test seam: overrides the GitHub API base during `omega update` (`https://api.github.com` by default). Never set in production |
| `OMEGA_MAX_RETRIES`    | read      | Provider HTTP retry budget (`ai/retry.go`), default `3` — 429/5xx retried with backoff |

## Related

- [tui.md](tui.md) — in-TUI slash commands and keybindings
- [configuration.md](configuration.md) — full config.yaml reference
- [api.md](api.md) — the HTTP surface `omega serve` exposes