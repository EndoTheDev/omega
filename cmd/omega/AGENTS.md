# cmd/omega

## Purpose

The single binary entry point for omega. It parses the subcommand
(`serve`, `run`, `health`, `chat`), resolves configuration and home
paths, wires the provider, agent, store, and extensions together, and
either serves HTTP (`serve`), runs one prompt to stdout (`run`), probes
the server (`health`), or launches the interactive Bubble Tea TUI
(`chat`). The TUI owns the full interactive loop: streaming, slash
commands, session persistence, autocomplete, skills, extensions, and
markdown rendering.

## Ownership

- `main.go` - CLI entry point, subcommand dispatch, config and home
  path resolution (`omegaHome`, `resolveConfigPath`, `resolveHomePaths`),
  agent wiring (`newAgent`, `buildSystemPrompt`), `cmdServe`, `cmdRun`,
  `cmdChat`, `cmdHealth`, `loadExtensions`, `loadSkills`, extension CLI
  flag parsing (`parseExtensionArgs`, `stripExtensionArgs`,
  `applyExtFlags`), global help (`helpText`)
- `trust.go` - project trust store (`TrustEntry`, `loadTrusted`,
  `saveTrusted`, `isTrusted`), trust gate (`resolveProjectContext`,
  `promptTrust`), trust flag parsing (`parseTrustArgs`,
  `stripTrustArgs`)
- `tui.go` - Bubble Tea TUI: the `model` state, `Update`/`View`/`Init`,
  streaming event handling (`handleEvent`, `appendSegment`, `drainEvents`),
  slash command dispatch (`handleCommand` and every `handle*` helper),
  session table and tree rendering, autocomplete, prompt history, inline
  skill invocation, extension command dispatch, auto-name, glamour
  rendering, status line, splash screen, theme system (`Theme` struct,
  `handleTheme`, `/theme` command)
- `theme.go` - System theme detection: Windows registry, macOS defaults,
  Linux gsettings / GTK_THEME / COLORFGBG fallback
- `main_test.go` - self-check tests for extension flag parsing,
  dispatch (chdir error), and help/version flags
- `tui_test.go` - self-check tests for channel draining, event folding,
  slash commands, persistence, resume, branch, label, rendering, and
  session ID generation

## Local Contracts

- **Subcommands are the only entry surface.** `run` dispatches `serve`,
  `run`, `health`, `chat`. `--config` and `--append-system-prompt` are
  global flags, parsed before or after the subcommand.
  `--append-system-prompt` is repeatable; each value is appended to the
  system prompt after the config's `system_prompt`.
- **`--help`/`-h` and `--version`/`-v` are global and exit before
  dispatch.** Any of these flags in the args prints to stdout and
  returns nil, even alongside a subcommand. There is no per-subcommand
  help. `--version` prints `omega <omegaVersion>`.
- **No subcommand defaults to the TUI.** `omega` (no args) starts the
  TUI. A non-subcommand argument is treated as a project path: omega
  chdirs there (erroring cleanly if it is not a directory) and starts
  the TUI. Subcommand names always win over a same-named directory.
- **Extension CLI flags are CLI-only.** `--extension`/`-e` (repeatable),
  `--no-extensions`, and `--project-extensions` have no YAML or env
  equivalent. `applyExtFlags` folds them into `cfg.Extensions` after
  `LoadConfig`: `--no-extensions` wins over everything; the other two
  each force `Enabled = true`. `--project-extensions` also loads
  `<cwd>/.omega/extensions/`.
- **Project trust gates AGENTS.md context.** The trust unit is the
  nearest directory (walking up from cwd) containing an AGENTS.md.
  Trust decisions live in `<home>/trust.yaml` (`trusted: [{path,
level}]`, level `exact` or `parent`). `--approve`/`--no-approve` are
  CLI-only overrides. The TUI prompts interactively for untrusted
  projects; `run`/`serve` skip untrusted context with a stderr warning.
  `--no-approve` wins over `--approve`.
- **`omegaHome` is the config root.** Resolution order: `OMEGA_HOME`
  env var, directory of the executable, `~/.omega/` fallback.
  `resolveHomePaths` rewrites relative defaults (`omega.db`,
  `extensions`, `skills`) to home-relative paths so omega works from
  any CWD.
- **The TUI does not call tools directly.** It constructs an
  `agent.Agent` per run via `ag.Run`, drains events from the returned
  channel, and folds them into the transcript. Tool execution stays in
  the agent layer.
- **A fresh events channel per run.** `submit` calls `ag.Run` which
  returns a new channel; the old one is closed. Reusing a channel
  across runs panics on the second write.
- **Slash commands run locally and never hit the agent.** `handleCommand`
  intercepts any input starting with `/` before constructing a user
  message. Extension commands and skill invocations are also resolved
  here.
- **Store-dependent commands are unavailable in ephemeral mode.**
  `/new --ephemeral` sets `m.ephemeral`; `/sessions`, `/resume`,
  `/branch`, `/label`, and `/tree` reject with an error in that state.
- **Session resolution accepts #, id, or label.** `resolveSession`
  tries line number from the cached `/sessions` list, exact ID, then
  case-insensitive label prefix, then a store fallback.
- **Auto-name is generation-guarded.** `autoNameGen` is bumped on
  `/new`; stale `autoNameMsg` results (gen or session mismatch) are
  dropped, not applied.
- **No re-exports.** Types from `internal/ai`, `internal/agent`, and
  `internal/gateway` are imported from there, not re-exported.

## Work Guidance

- Add new slash commands to `knownCommands` and `commandOptions` (when
  enum arguments apply), then implement the handler in `handleCommand`
  and add a test in `tui_test.go`.
- Keep `handleCommand` returning `(tea.Model, tea.Cmd)` with a value
  receiver; callers must use the returned model, not the original.
- New agent event types: add a case in `handleEvent`, append to
  `segments` via `appendSegment`, and fold into the transcript at
  `AgentEnd`. Update `renderTranscript` for the resume path.
- Streaming segments preserve narrative order (thinking, tool,
  response). Do not sort or reorder them; `refresh` renders them
  verbatim during streaming, `AgentEnd` folds them through glamour.
- The autocomplete command list is per-model: `knownCommands` is
  cloned, then extension commands and skill names are appended. Do not
  mutate the package-level `knownCommands` slice.
- `glamour` rendering strips ANSI codes first (`ansiRegex`) so
  lipgloss styling does not render as literal text. Zero width
  normalizes to 80, not a panic.
- `resolveHomePaths` must run after `LoadConfig` and before opening the
  store; it also `MkdirAll`s the home directory so SQLite can create
  its file.
- `cmdChat` owns the store and extension manager and closes both on
  every exit path (`/exit`, Ctrl+C, or a `p.Run` error).

## Verification

```bash
go test ./cmd/omega/       # TUI unit tests
go build ./...             # everything compiles
go vet ./...               # no suspicious constructs
```

## Child DOX Index

No sub-packages.
