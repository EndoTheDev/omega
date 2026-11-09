# CLI Reference

`omega` is a single binary. One global help covers all subcommands
(`omega --help`); per-subcommand help appears as the subcommands gain
flags. Everything below is generated from the parser in `cmd/omega` —
if a flag is not in the code, it is not in this document.

## Usage

```txt
omega                 start the CLI agent loop (first run: walks you through setup)
omega <path>          start the loop in <path> (chdir first)
omega chat            explicit alias for the CLI loop
omega setup           the first-run wizard: provider, model, api key; writes config.yaml
omega run <prompt>    run one prompt to stdout
omega run --session <id> <prompt>   run bound to an existing session (the wake contract)
omega serve           start the HTTP server (SSE streaming)
omega export <id|label> [output]   export a session as JSONL
omega insights [--days N]          session usage analytics (default: 30 days)
omega agents                       list registered agents; `omega agents create <name>` scaffolds one
omega tools                        list wire tools; `omega tools disable <name>` takes one off the model
omega update              report N versions behind + prompt y/N
omega update --yes        install the latest stable without a prompt
omega update --rollback   swap back to omega.bak + health-verify
omega rollback            alias for omega update --rollback
omega health          check the server at the configured port
omega kanban          the task board registry (see: omega kanban --help)
omega crons           scheduled prompts: list, add, edit, delete (see: omega crons --help)
omega skills          the skills registry: list, read, write, delete (see: omega skills --help)
omega mcp            MCP server entries: list, add, edit, delete (see: omega mcp --help)
```

## Subcommands

Module-owned subcommands (v0.12 phase D): the dispatch table is the
mount registry. A capsule registers its own name + handler at mount,
parses its own argv (including its own `--help`), and the global
`omega --help` lists module subcommands by registry introspection.
Deleting a module removes its registration: the subcommand vanishes
from `omega --help` and an argument that matches nothing falls back to
the project-path behavior.

### `omega` / `omega chat` (the CLI agent loop)

No argument, `chat`, or a non-subcommand argument launches the CLI
agent loop — plain readline in, streaming ANSI out, `ollama run`
mental model. The loop never owns the screen (no alt-screen, no panes,
no mouse); anything screen-shaped is the webui's job. A non-subcommand
first argument is treated as a project path: omega chdirs there first,
so project context (AGENTS.md walk) and file tools resolve relative to
that directory.

```bash
omega                       # loop in the current directory
omega ~/dev/omega           # loop rooted at a project
```

The setup gate is the law: with no config, serve and run refuse and
bare `omega` runs the setup wizard first. Loop commands: `/exit`,
`/help`, `/session [id|new]`, `/workspace [dir]`, plus every mounted
module command (`/logs`, `/verdict`, `/cron`, ...) and a `"""`
multiline input block. On exit the loop prints the webui URL — the
browser is the app; the loop is the fallback.

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

`--session <id>` binds the run to an EXISTING session: the run persists
every finished message through a TurnSink - input included, provider-
correct order - instead of printing and dropping. A typo'd id errors
rather than forking a new session. Inside a continuous-agent wake the
run's closing message is shaped before it lands: an unbacked or
contradicted claim sentence strips, a verified one gains its evidence
ledger citation (a claim over a URL is backed by the run's own ok
fetch of that URL; the verifier's cheap check stays the file stat),
and the runtime evidence tail closes the report. Any
run outside a wake child persists through the sink untouched.

A `--session` run spawned by `delegate.task` also carries `OMEGA_DB_PATH`
pointing at the CALLER's store. The child runs as an agent profile, and
the agent layer's data isolation would otherwise open that agent's own
`data/omega.db` - a different database than the one the caller
pre-created the child session in - so the run died `run: --session <id>:
session not found` (exit 1) before the loop. An env-set `OMEGA_DB_PATH`
is caller intent and survives the agent layer.

```bash
omega run --session canonical "check the deploy queue"   # the wake child's shape
```

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

### `omega crons`

The terminal face of the webui's Cron tab, on the same job set and the
same writer: every verb below rides `Runner.SaveJob` /
`Runner.DeleteJob`, the functions `POST /cron/save` and
`POST /cron/delete` call. An `omega crons add` and an editor save land
identically in the main home's `crons.yaml` (yaml.Node splice: file
comments and untouched siblings survive; a replaced entry re-marshals
clean). Validation is the loader's own schema law - a missing prompt,
a malformed schedule, a schedule on an interval job are refused before
anything reaches the disk. An agent home's wake job belongs to
`<home>/agents/<name>/config/crons.yaml` and is refused here, naming
that file.

The job name is the one positional and it comes FIRST; leftover
positionals are a usage error, never a guess.

```bash
omega crons                                   # same as `omega crons list`
omega crons list                              # name, cadence, agent, last fire, next fire
omega crons add nightly-ship --schedule "0 3 * * *" --prompt "ship the release"
omega crons add pulse --kind interval --minutes 15 --prompt "tick"
omega crons edit nightly-ship --enabled=false  # only the flags you pass apply
omega crons delete pulse
```

Flags (add/edit): `--schedule`, `--kind`, `--minutes`, `--prompt`,
`--deliver`, `--file`, `--agent`, `--model`, `--skills`, `--enabled`.
`edit` applies only the flags actually passed, so a one-scalar flip
never rewrites the prompt or the schedule.

### `omega tools`

The wire tool registry with its enable flag. Bare `omega tools` and
`omega tools list` print every registered tool with its state, the
extension that owns it, and its one-line description. `disable <name>`
takes a tool off the wire: it leaves every tool union the loop builds,
so the model is never offered it, and the system prompt's tool section
stops advertising it. `enable <name>` puts it back. A disabled tool
keeps its listing row - the row is how the name is found again - and
carries `[disabled]` (the `/tools` command in the loop shows the same
shape).

State lives in `config.yaml`'s `disabled.tools` fold, written by the
same writer the settings tab's tools toggle (`PUT /disabled/tools`)
uses: a CLI toggle and a tab toggle land on one key, and a running
server picks the CLI's change up through its config watch within a few
seconds. Disabling an unknown name is refused loudly and writes
nothing; re-enabling never validates, so a name whose tool has left the
registry can still be cleaned out of the fold.

```bash
omega tools                            # tool table, name-sorted
omega tools disable files.write        # off the wire; absent from the model's tool list
omega tools enable files.write         # back on
omega tools disable no.such.tool       # refused: no such tool in the registry
```

### `omega skills`

The terminal face of the webui's Skills tab, on the same folders and
the same writer: `list` renders the `/skills` command's own table, and
the verbs ride this capsule's provider faces rather than a second
implementation - `read` calls `ReadSkill` (the seam behind
`GET /skills/content`), `write` rides `SaveSkill` (the tab's one
writer, `POST /skills/save`) and `delete` rides the same removal law
`DeleteSkill` delegates to (`POST /skills/delete`), with the ledger's
actor naming `cli`. An `omega skills write` and a tab
save compose the identical `SKILL.md` (the description in the
frontmatter fence, the markdown body under it; an update rewrites the
body and the description line and leaves every other frontmatter key
byte-true). A delete backs the whole folder up into
`.curator_backups` with a ledger row before removing it - a removal is
receipted, never a silent `rm`.

The skill name is the one positional and it comes FIRST; leftover
positionals are a usage error, never a guess. Every verb enforces the
loader's name law - one folder component, no separators, no leading
dot - so a traversal-shaped name is refused by `read` exactly as it is
by `write`, `delete` and the HTTP handlers.

```bash
omega skills                              # same as `omega skills list`
omega skills list                         # NAME / DESCRIPTION table
omega skills read deploy                  # one SKILL.md body (no frontmatter)
omega skills write deploy --description "ship it" --file deploy.md
omega skills write deploy                 # body on stdin; description kept
omega skills delete deploy
```

Flags (`write`): `--description`, `--file` (`-` = stdin), `--content`.
With neither `--file` nor `--content` the body is read from stdin. A
name present in both the shared and the agent's own skills directory
resolves to the shared copy - the same winner the tab edits.

`omega skills` is a module subcommand: the name comes from the skills
capsule's mount registration, so it vanishes from `omega --help` (and
stops dispatching) when the module is disabled. An existing `skills/`
directory in the current directory still wins as a project path - the
dispatch checks directories before the module table, so run the
subcommand from outside your omega home.

### `omega update`

Downloads a release from GitHub (`EndoTheDev/omega` releases), verifies
the archive against the release's `checksums.txt` when present, and
replaces the running binary in place, preserving the replaced binary as
`omega.bak` (one generation). Data files (`extensions/` payloads) and
the `.example` config seeds copy over without overwriting anything that
exists.

`checksums.txt` (generated by `scripts/release-checksums.sh`) carries
TWO lines per asset with the same hash: the full release filename
(current updaters match by substring) and a bare platform-tag alias
(`windows_amd64`). The alias is load-bearing for binaries built before
2026-10-16 (v0.6.0 and older), whose checksum lookup matches by filename
suffix and otherwise aborts self-update with `no entry for <tag>` - those
installs cannot be patched, so the release side stays legible to both
matchers until no supported install predates v0.7.0. Both shapes are
pinned by `cmd/omega/checksum_test.go` and
`scripts/release-checksums_test.sh`; any checksums.txt format change
must keep both green.

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

### `omega mcp` (MCP server entries)

`omega mcp list|add|edit|delete` is the terminal face of the MCP dock
tab's editor. Entries live in the `mcp.yaml` the loader reads
(`<home>/mcp.yaml` legacy location first, then `<home>/config/mcp.yaml`)
- the file is the store, and every write is the same law the wire
rides: validate, splice the one entry at the `yaml.Node` level
(comments and keys survive), write atomically, then swap the live
bridge. No restart, and a removed or disabled server's connections
close in the swap.

```bash
omega mcp                                      # same as `omega mcp list`
omega mcp list                                 # name, transport, enabled, state, tools, error
omega mcp add docs --url http://127.0.0.1:27123/mcp/ --header "Authorization:Bearer x"
omega mcp add fs --command npx --arg -y --arg @modelcontextprotocol/server-filesystem --arg /home/me
omega mcp edit docs --enabled=false            # only the flags you pass are applied
omega mcp delete fs
```

Entry flags: `--command` (stdio) and `--url` (Streamable HTTP) are
exclusive; `--arg`, `--env KEY=VALUE` belong to stdio and `--header
KEY:VALUE` to http; `--arg`/`--env`/`--header` repeat. On `edit`,
`--arg` replaces the whole argument list while `--env`/`--header` merge
key by key. `--enabled=false` keeps the entry listed but unconnected.

A home with no `mcp.yaml` gets `<home>/config/mcp.yaml` on the first
add; deleting the last entry from such an auto-created file removes the
file again. Legacy sources stay read-only: with `MCP_SERVERS` set, or
when the config came from `mcp.json`, every write is refused with the
reason (edit that source directly). The HTTP face is
`GET /mcp/servers`, `POST /mcp/save`, `POST /mcp/delete`, `POST
/mcp/toggle`, `POST /mcp/reload` - each write answers the fresh server
listing, so one response carries the state it landed.

## Global flags

Accepted before or after the subcommand:

| Flag                          | Meaning                                                                 |
| ----------------------------- | ----------------------------------------------------------------------- |
| `--config <path>`             | Config file (default: `<home>/config.yaml`, then `<home>/config/config.yaml`; legacy home-root wins) |
| `--append-system-prompt <text>` | Append to the system prompt; repeatable; `run`/loop only            |
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
| `OMEGA_MAX_RETRIES`    | read      | Provider retry budget (`llm/retry.go`), default `3` — HTTP 429/5xx and Ollama `done_reason: load` retried with backoff |

## Related

- [configuration.md](configuration.md) — full config.yaml reference
- [api.md](api.md) — the HTTP surface `omega serve` exposes