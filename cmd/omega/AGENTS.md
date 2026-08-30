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
  (`WatchConfig`), store wiring (from `ctx.Store` after
  `MountAll`), env vars (`OMEGA_HOME`, `OMEGA_SKILLS_DIR`,
  `OMEGA_BIN` for subagent delegation), global help (`helpText`)
- `export.go` - session export (`cmdExport`, `exportMessages`,
  `messageRole`, `resolveSessionCLI`)
- `insights.go` - session analytics (`cmdInsights`, `formatInsights`,
  `formatNumber`)
- `update.go` - self-update (`cmdUpdate`, `githubRelease`,
  `findAsset`, `assetNameForOS`, `findChecksumURL`, `verifyChecksum`).
  Archive-based: downloads zip/tar.gz, verifies SHA256 checksum,
  extracts omega + extensions (self-contained subdirectory layout) +
  config/mcp examples. Progress bar during download. Skips when already
  up to date. Preserves user config files. Security: `safeJoin` validates
  archive entries stay within dest (CWE-22 path traversal prevention),
  `io.LimitReader` caps zip reads at 200MB and API responses at 1MB,
  checksum verification before extraction, `verifyChecksum` fetch with
  30s timeout, atomic binary replacement via temp+rename on Linux/macOS.
- `trust.go` - trust flag parsing (`trustFlags`, `parseTrustArgs`,
  `stripTrustArgs`), `cwd()` helper. Trust logic lives in
  `extensions/trust/` (TrustProvider seam).
- `image.go` - CLI `@file` input (`parseFileArgs` calling `ai.DetectImage`)
- `config.go` - runtime config: `Config` struct with sub-configs
  (ProviderConfig, ServerConfig, StoreConfig, SkillsConfig, MemoryConfig,
  LoggingConfig, CompactionConfig), `LoadConfig` (YAML + env + defaults),
  `DefaultConfig`, `applyEnv`, `Validate`, `WatchConfig` (hot-reload)
- `config_test.go` - config loading, env overrides, validation tests
- `main_test.go` - self-check tests for subcommand dispatch,
  chdir error handling, and help/version flags
- `export_test.go` - self-check tests for `exportMessages`,
  `messageRole`, and session resolution
- `update_test.go` - self-check tests for `assetNameForOS` and
  `findAsset` (asset matching across platforms)
- `image_test.go` - self-check tests for `parseFileArgs`
  (CLI `@file` input via `ai.DetectImage`)

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
- **Config validation is deep.** `Validate` checks provider type
  (ollama/openai/anthropic), host URL parseability, memory char limits
  (when enabled), max_turns >= 0, http_timeout >= 0 — in addition to
  the required model_name and port > 0.
- **Export is shared.** `exportMessages` in `export.go` writes JSONL
  from `[]ai.Message`. Both `cmdExport` (CLI) and the store extension's
  `/export` command produce JSONL output.
- **Export resolves by ID or label.** `resolveSessionCLI` tries exact
  session ID, then case-insensitive label prefix. Multiple matches
  error with the candidates listed.
- **Insights are cross-session analytics.** `ComputeInsights` in the
  store aggregates sessions, messages, tool calls, and token estimates
  over the last N days. `formatInsights` renders the report as plain
  text. CLI (`omega insights [--days N]`) uses this directly.
- **Seam wiring in newAgent.** `newAgent` builds a `plugin.Context`,
  calls `MountAll` with all extension plugins (provider, store, skills,
  compactor, logging, memory, http_channel, prompt, tools, mcp, delegate, web, agent_loop), then builds
  the agent via `agent.NewFromContext(ctx, AgentOptions{...})` and
  returns it alongside the store, logger, and the plugin Context
  itself (callers start channels from it).
- **cmdServe is channel-driven.** Channels mount via the additive
  `channel` seam (`ctx.Channels`). `cmdServe` starts each in a
  goroutine with `ChannelDeps{Ctx, Store}`, waits for a signal
  or the first channel error, and stops all channels on shutdown.
  `omega health` probes the HTTP channel specifically.
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
  detects image files by magic bytes (PNG/JPEG/GIF/WebP/BMP) via
  `ai.DetectImage`, encodes them as base64, and sends them to the
  provider as image content alongside the text prompt. Text files
  inlined via `@file` are appended to the prompt content. No auto-resize
  or EXIF correction (defer).
- **`omegaHome` is the config root.** Resolution order: `OMEGA_HOME`
  env var, directory of the executable, `~/.omega/` fallback.
  `resolveHomePaths` rewrites relative defaults (`omega.db`,
  `extensions`, `skills`, `memory.md`, `user.md`, `omega.log`) to home-relative paths so omega works from
  any CWD.
- **No re-exports.** Types from `ai`, `agent`
  are imported from there, not re-exported.

## Work Guidance

- Add new subcommands to `run()` dispatch and implement as `cmd<Name>`.
  Add a test in `main_test.go` or a dedicated `<name>_test.go`.
- `buildPlugins` must list the TUI plugin (`tui.NewPlugin()`) for
  `omega chat` to work. `cmdChat` delegates to `ctx.Frontend.Run()`.
- `resolveHomePaths` must run after `LoadConfig` and before opening
  the store. New home-relative config defaults go here.
- New `CmdAction` types: add the case in the TUI extension's
  `handleExtensionCommand`, then have the owning extension return it.
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
