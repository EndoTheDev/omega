# extensions/tui

## Purpose

The terminal frontend. Implements the `Frontend` seam — a Bubble Tea
program that streams agent responses, handles slash commands, renders
markdown with syntax highlighting, and manages session state. The
entry point (`cmd/omega`) mounts this extension and calls `Run`.

## Ownership

- `tui.go` - Bubble Tea TUI: the `model` state, `Update`/`View`/`Init`,
  streaming event handling (`handleEvent`, `appendSegment`, `drainEvents`),
  slash command dispatch (`handleCommand` and built-in `handle*` helpers:
  `handleCopy`, `handleExtensions`, `handleTheme`, `handleSessionDelete`),
  extension command dispatch (`handleExtensionCommand` with `CmdAction`
  interpretation), session table and tree rendering, autocomplete, prompt
  history, inline skill invocation, auto-name, glamour rendering, status
  line, splash screen, desktop notifications (`notifyTurnComplete`, `beeep`),
  model quick-cycle (Ctrl+P, `fetchModelsCmd`, `modelsLoadedMsg`),
  auto-discovered context window (`fetchModelInfoCmd`,
  `modelInfoLoadedMsg`, `contextWindow` field), bracketed paste (file drop),
  `NewModel` (public constructor for the plugin adapter).
  `summarizeForBranch` trims long branch history. `agent.NewSessionID` (shared) generates
  random hex IDs. `store.SessionDisplayName` (shared) truncates unlabeled IDs.
  `renderTranscript` renders full history for resume/resize/theme switch.
  `renderMarkdown`, `renderCodeBlock`, `highlightCode`, `langForTool`,
  `wordWrap`, `truncate` — rendering helpers.
  `registerCatppuccinChroma` registers chroma styles at init.
  Built-in commands: `/copy`, `/extensions`, `/theme`, `/exit`, `/help`.
  All other commands dispatch to extensions via `handleExtensionCommand`.
- `theme.go` - System theme detection: Windows registry, macOS defaults,
  Linux gsettings / GTK_THEME / COLORFGBG fallback. `Theme` struct with
  Catppuccin Mocha (dark) and Latte (light) palettes. `glamourStyleForTheme`
  maps themes to glamour StyleConfig. `themeNames` returns sorted list.
- `image.go` - `extractImages` scans chat input for `@path` tokens:
  image files (via `ai.DetectImage`), text files (inlined), glob patterns
  (`@*.go`), `@session:<id>` (injects session history), `@skill:<name>`
  (injects skill content). Unresolved tokens left as-is.
- `plugin.go` - `Frontend` implementation (`Run` launches the Bubble Tea
  program), `Plugin` adapter (`Provides: ["frontend"]`), `NewPlugin`.
- `tui_test.go` - TUI tests + helpers (`testContext`, `newTestCtx`,
  `newProviderTestCtx`, `ansiStrip`, `newChatModel` calls).
- `tui_pure_test.go` - Pure function tests (`truncate`, `wordWrap`).
- `tui_handlers_test.go` - Handler tests (`handleExtensions`, `handleTheme`,
  `/tools` listing via extension dispatch).
- `tui_refresh_test.go` - `refresh()` segment rendering tests.
- `glamour_style_test.go` - `glamourStyleForTheme` color assertions.
- `image_test.go` - `extractImages` and `ai.DetectImage` tests.
- `theme_test.go` - `themeNames` test.

## Local Contracts

- **Ctrl+P cycles models.** Cycles through `modelList` (populated by
  `/models`). If empty, fires `fetchModelsCmd` to fetch from the
  provider via `Provider.ListModels` asynchronously; the
  result arrives as `modelsLoadedMsg`.
- **Auto-discovered context window.** `fetchModelInfoCmd` queries the
  provider for the current model's context window
  (`Provider.ModelInfo`). Fires on `Init`, `/model`, Ctrl+P, and
  `modelsLoadedMsg`. The result arrives as `modelInfoLoadedMsg` and
  sets `m.contextWindow`. Status bar priority: provider
  (`m.contextWindow`) > config (`CompactionConfig.ContextWindow`)
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
- **Image input via `@file` in chat.** `extractImages` on submit scans
  for `@path` tokens: image files detected by `ai.DetectImage` (magic
  bytes for PNG/JPEG/GIF/WebP/BMP, base64 encoded), text files inlined,
  glob patterns expanded, `@session:<id>` injects session history,
  `@skill:<name>` injects skill content. Unresolved tokens left as-is.
- **Catppuccin Mocha/Latte theme integration.** All `Theme` struct
  colors use Catppuccin hex values. Chroma styles registered at init.
  Glamour `StyleConfig` overridden with Catppuccin colors for headings,
  code, links, blockquotes, tables.
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
  width calculations but preserves them in output.
- **Transcript re-wraps on resize.** `WindowSizeMsg` calls
  `renderTranscript` when not busy, re-wrapping all history at the
  new width. Slash command output is lost (same as theme switch).
- **The TUI does not call tools directly.** It constructs an
  `agent.Agent` per run via `agent.NewFromContext` + `ag.Run`, drains
  events from the returned channel, and folds them into the transcript.
  Tool execution stays in the agent layer.
- **A fresh events channel per run.** `submit` calls `ag.Run` which
  returns a new channel; the old one is closed. Reusing a channel
  across runs panics on the second write.
- **Slash commands run locally and never hit the agent.** `handleCommand`
  intercepts any input starting with `/` before constructing a user
  message. Extension commands and skill invocations are also resolved
  here. Extension commands dispatch via `handleExtensionCommand` with
  `CommandResult` + `CmdAction` actions. Built-in commands (`/copy`,
  `/extensions`, `/theme`, `/exit`, `/help`) are handled inline.
  `/sessions delete` is also handled inline (needs TUI state for
  session list cache). All other commands are extension-registered.
- **Store-dependent commands are unavailable in ephemeral mode.**
  `/new --ephemeral` sets `m.ephemeral`; `/sessions`, `/resume`,
  `/branch`, `/label`, `/tree`, `/export`, `/search`, `/insights`
  reject with an error in that state. The TUI checks ephemeral mode
  before dispatching to the extension handler.
- **Session resolution accepts #, id, or label.** `resolveSession`
  tries line number from the cached `/sessions` list, exact ID, then
  case-insensitive label prefix, then a store fallback. Used by
  `/sessions delete` (the only TUI-own command that needs it).
- **Auto-name is generation-guarded.** `autoNameGen` is bumped on
  `/new`; stale `autoNameMsg` results (gen or session mismatch) are
  dropped, not applied.
- **Session entry types are persisted and replayed.** `persistEntry`
  appends non-conversation entries (`ModelChange`, `ThinkingLevelChange`)
  to the store on `/model` and `/thinking`. On `/resume`, the TUI
  replays these entries to restore the model and thinking level. No-op
  for ephemeral sessions. `renderTranscript` skips these types.

## Work Guidance

- Add new slash commands to `knownCommands` and `commandOptions` (when
  enum arguments apply), then implement the handler in `handleCommand`
  and add a test in `tui_test.go`. Most commands should live in their
  owning extension, not here.
- Keep `handleCommand` returning `(tea.Model, tea.Cmd)` with a value
  receiver; callers must use the returned model, not the original.
- New agent event types: add a case in `handleEvent`, append to
  `segments` via `appendSegment`, and fold into the transcript at
  `AgentEnd`. Update `renderTranscript` for the resume path.
- Streaming segments preserve narrative order (thinking, tool,
  response). Do not sort or reorder them; `refresh` renders them
  verbatim during streaming, `AgentEnd` folds them through glamour.
- New `CmdAction` types: add the case in `handleExtensionCommand`'s
  action switch, then have the extension return it from its command
  handler.

## Verification

```bash
go test ./extensions/tui/   # TUI unit tests
go build ./...              # everything compiles
go vet ./...                # no suspicious constructs
```

## Child DOX Index

No sub-packages.
