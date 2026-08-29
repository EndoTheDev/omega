# cmd/omega

## Purpose

The single binary entry point for omega. It parses the subcommand
(`serve`, `run`, `health`, `chat`), resolves configuration and home
paths, wires the provider, agent, store, and extensions together, and
either starts all mounted channels - HTTP by default (`serve`), runs one prompt to stdout (`run`), probes
the server (`health`), or launches the frontend extension (`chat`).
The TUI is now an extension at `extensions/tui/` implementing the
`Frontend` seam.

## Ownership

- `main.go` - CLI entry point, subcommand dispatch, config and home
  path resolution (`omegaHome`, `resolveConfigPath`, `resolveHomePaths`),
  agent wiring (`newAgent`, `buildPlugins`), `cmdServe`, `cmdRun`,
  `cmdChat`, `cmdTest` (smoke test), `cmdHealth`, config hot-reload
  (`gateway.WatchConfig`), store wiring (from `ctx.Store` after
  `MountAll`), env vars (`OMEGA_HOME`, `OMEGA_SKILLS_DIR`,
  `OMEGA_BIN` for subagent delegation), global help (`helpText`)
- `export.go` - session export (`cmdExport`, `exportMessages`,
  `messageRole`, `resolveSessionCLI`)
- `insights.go` - session analytics (`cmdInsights`, `formatInsights`,
  `formatNumber`)
- `update.go` - self-update (`cmdUpdate`, `githubRelease`,
  `findAsset`, `assetNameForOS`). Archive-based: downloads zip/tar.gz,
  extracts omega + extensions (self-contained subdirectory layout) +
  config/mcp examples. Progress bar during download. Skips when already
  up to date. Preserves user config files. Security: `safeJoin` validates
  archive entries stay within dest (CWE-22 path traversal prevention),
  `io.LimitReader` caps zip reads at 200MB and API responses at 1MB,
  atomic binary replacement via temp+rename on Linux/macOS.
- `trust.go` - project trust store (`TrustEntry`, `loadTrusted`,
  `saveTrusted`, `isTrusted`), trust gate (`resolveProjectContext`,
  `promptTrust`), trust flag parsing (`parseTrustArgs`,
  `stripTrustArgs`)
- `context.go` - project context loading (`ProjectRoot`,
  `LoadProjectContext`) moved from the deleted `harness/` package
- `image.go` - CLI `@file` input (`parseFileArgs` calling `ai.DetectImage`)
- `main_test.go` - self-check tests for subcommand dispatch,
  chdir error handling, and help/version flags
- `export_test.go` - self-check tests for `exportMessages`,
  `messageRole`, and session resolution
- `update_test.go` - self-check tests for `assetNameForOS` and
  `findAsset` (asset matching across platforms)
- `image_test.go` - self-check tests for `detectImage`, `parseFileArgs`,
  `extractImages`, and `User` with images JSON round-trip
- `tui_test.go` - self-check tests for channel draining, event folding,
  slash commands, persistence, resume, branch, label, rendering, and
  session ID generation

## Local Contracts

- **Subcommands are the only entry surface.** `omega` dispatches:
  `serve`, `run`, `health`, `chat`, `export`, `update`, `insights`.
  `--config` and `--append-system-prompt` are
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
- **Project trust gates AGENTS.md context.** The trust unit is the
  nearest directory (walking up from cwd) containing an AGENTS.md.
  Trust decisions live in `<home>/trust.yaml` (`trusted: [{path,
level}]`, level `exact` or `parent`). `--approve`/`--no-approve` are
  CLI-only overrides. The TUI prompts interactively for untrusted
  projects; `run`/`serve` skip untrusted context with a stderr warning.
  `--no-approve` wins over `--approve`.
- **Notifications fire on turn complete.** `notifications` config
  (`bell` default, `desktop`, `off`) controls the mode. `bell` prints
  `\x07`, `desktop` calls `beeep.Notify` in a goroutine (non-blocking),
  `off` does nothing. `OMEGA_NOTIFICATIONS` env var overrides.
- **Ctrl+P cycles models.** Cycles through `modelList` (populated by
  `/models`). If empty, fires `fetchModelsCmd` to fetch from the
  provider via `Provider.ListModels` asynchronously; the
  result arrives as `modelsLoadedMsg`.
- **Auto-discovered context window.** `fetchModelInfoCmd` queries the
  provider for the current model's context window
  (`Provider.ModelInfo` → `/api/show` for Ollama). Fires on `Init`,
  `/model`, Ctrl+P, and `modelsLoadedMsg`. The result arrives as
  `modelInfoLoadedMsg` and sets `m.contextWindow`. Status bar priority:
  provider (`m.contextWindow`) > config (`CompactionConfig.ContextWindow`)
  > `agent.DefaultContextWindow` (8192). OpenAI/Anthropic return 0
  > (not exposed by their APIs), so config is the source of truth there.
  > `Provider.SetModel` is called before `fetchModelInfoCmd` on every
  > model switch so the provider queries the correct model.
- **Bracketed paste inserts file paths.** `msg.Paste` KeyMsgs are
  inserted into the textarea as regular runes, bypassing autocomplete.
- **Tool results get syntax highlighting.** `highlightCode` applies
  chroma highlighting with Catppuccin chroma styles (`catppuccin-mocha`
  for dark, `catppuccin-latte` for light) to tool result content.
  Language is inferred from the preceding tool call: `read_file` path
  extension (`.go` -> go, `.py` -> python, etc.), `shell` -> bash.
  Error results and collapsed results are not highlighted. Highlighted
  results use `tool_result_highlighted` segment kind to avoid the
  `theme.Tool` color overlay. Both live streaming and `renderTranscript`
  (resize/theme/resume) apply highlighting by matching `ToolCallID`
  to the preceding assistant's tool calls.
- **Export is shared.** `exportMessages` in `export.go` writes JSONL
  from `[]ai.Message`. Both `handleExport` (TUI) and `cmdExport` (CLI)
  delegate to it. `messageRole` lives in `export.go`.
- **Export resolves by ID or label.** `resolveSessionCLI` tries exact
  session ID, then case-insensitive label prefix. Multiple matches
  error with the candidates listed.
- **Insights are cross-session analytics.** `ComputeInsights` in the
  store aggregates sessions, messages, tool calls, and token estimates
  over the last N days. `formatInsights` renders the report as plain
  text. Both CLI (`omega insights [--days N]`) and TUI (`/insights
[days]`) share the same code path.
- **Session entry types are persisted and replayed.** `persistEntry`
  appends non-conversation entries (`ModelChange`,
  `ThinkingLevelChange`) to the store on `/model` and `/thinking`.
  On `/resume`, the TUI replays these entries to restore the model and
  thinking level. No-op for ephemeral sessions. `renderTranscript`
  skips these types (metadata, not conversation content).
- **Seam wiring in newAgent.** `newAgent` builds a `plugin.Context`,
  calls `MountAll` with all extension plugins (provider, store, skills,
  compactor, logging, memory, http_channel, prompt, tools, mcp, delegate, web, agent_loop), then builds
  the agent via `agent.NewFromContext(ctx, AgentOptions{...})` and
  returns it alongside the store, logger, and the plugin Context
  itself (callers start channels from it).
- **cmdServe is channel-driven.** Channels mount via the additive
  `channel` seam (`ctx.Channels`). `cmdServe` starts each in a
  goroutine with `ChannelDeps{Ctx, Store, Config}`, waits for a signal
  or the first channel error, and stops all channels on shutdown.
  `omega health` probes the HTTP channel specifically.
- **`/tools` lists tools, `/tools on|off|auto` controls display.**
  No-arg `/tools` (or `/tools list`) calls `handleToolsList` which
  renders all tools from `Infos().ToolList` (first line of description)
  grouped by extension. `/tools on|off|auto`
  toggles tool result display in the transcript.
- **Self-update downloads and extracts an archive.** `cmdUpdate` fetches
  the latest GitHub release, matches the asset by `GOOS_GOARCH`,
  downloads a zip/tar.gz archive, extracts omega + extensions
  (self-contained subdirectory layout) + config/mcp examples to a temp
  dir, then replaces the running binary and extension binaries. Preserves
  user config files. Progress bar during download. Skips when already
  up to date. On Windows the running exe is renamed to `.old` first.
  No checksum verification (no release signing yet). Security hardening:
  `safeJoin` prevents path traversal from malicious archive entries
  (CWE-22), `io.LimitReader` caps downloads and API responses to prevent
  OOM, binary replacement is atomic via temp+rename on Linux/macOS.
- **Image input via `@file` args.** `omega run @image.png what is this?`
  detects image files by magic bytes (PNG/JPEG/GIF/WebP/BMP), encodes
  them as base64, and sends them to the provider as image content
  alongside the text prompt. `ai.User.Images` carries the image data;
  each provider serializes it in its native format (Ollama `images`
  array, OpenAI `image_url` blocks, Anthropic `image` source blocks).
  Text files inlined via `@file` are appended to the prompt content.
  In the TUI, `@path` tokens in chat input are processed the same way
  via `extractImages` on submit. No auto-resize or EXIF correction
  (defer).
- **Catppuccin Mocha/Latte theme integration.** All `Theme` struct
  colors use Catppuccin hex values. Chroma styles (`catppuccin-mocha`,
  `catppuccin-latte`) registered at init for code highlighting. Glamour
  `StyleConfig` overridden with Catppuccin colors for headings, code,
  links, blockquotes, tables. `renderAssistant` uses
  `glamour.WithStyles()` per theme.
- **Bordered code blocks with language headers.** `renderMarkdown`
  splits markdown on fenced code blocks; prose goes through glamour,
  code goes through `renderCodeBlock` (chroma highlight + lipgloss
  rounded border + bold language label). Fast path: no code blocks
  -> plain glamour.
- **Live glamour rendering during streaming.** `refresh()` renders
  response segments through `renderMarkdown` with 80ms debounce.
  `lastRenderedResponse` caches the last output to avoid flicker
  between highlighted and raw frames. Final `AgentEnd` render is
  always a clean full pass.
- **ANSI-aware word wrap.** `wordWrap` strips ANSI escape codes from
  width calculations but preserves them in output. Fixes broken
  wrapping of syntax-highlighted code.
- **Transcript re-wraps on resize.** `WindowSizeMsg` calls
  `renderTranscript` when not busy, re-wrapping all history at the
  new width. Slash command output is lost (same as theme switch).
- **`omegaHome` is the config root.** Resolution order: `OMEGA_HOME`
  env var, directory of the executable, `~/.omega/` fallback.
  `resolveHomePaths` rewrites relative defaults (`omega.db`,
  `extensions`, `skills`, `memory.md`, `user.md`, `omega.log`) to home-relative paths so omega works from
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
  here. `/model`, `/provider`, and `/compact` are extension commands
  registered by the provider and compactor extensions, dispatched via
  `handleExtensionCommand` with `CommandResult` + `CmdAction` actions.
  Other slash commands are built-in to the TUI.
- **Store-dependent commands are unavailable in ephemeral mode.**
  `/new --ephemeral` sets `m.ephemeral`; `/sessions`, `/resume`,
  `/branch`, `/label`, and `/tree` reject with an error in that state.
- **Session resolution accepts #, id, or label.** `resolveSession`
  tries line number from the cached `/sessions` list, exact ID, then
  case-insensitive label prefix, then a store fallback.
- **Auto-name is generation-guarded.** `autoNameGen` is bumped on
  `/new`; stale `autoNameMsg` results (gen or session mismatch) are
  dropped, not applied.
- **No re-exports.** Types from `ai`, `agent`, and
  `gateway` are imported from there, not re-exported.

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
- `cmdChat` mounts plugins, opens the store, loads skills, and
  delegates to `ctx.Frontend.Run()` (the TUI extension). It closes
  the store and logger on every exit path.

## Verification

```bash
go test ./cmd/omega/       # TUI unit tests
go build ./...             # everything compiles
go vet ./...               # no suspicious constructs
```

## Child DOX Index

No sub-packages.
