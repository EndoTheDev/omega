# webui

## Purpose

The omega web interface: a Nuxt 4 single-page app with a dock-based
shell (VS Code-style AWE: menu bar, persisted pane tree, status bar).
In dev it runs via `nuxt dev` with a nitro devProxy to the dev omega
on `127.0.0.1:8098`. In production the built SPA is embedded into the
omega binary via `go:embed` and served by the HTTP channel at `/`. The
installed instance owns port 8099; the dev tree's omega serves 8098
and the two never touch each other.

## Architecture (dock era)

- **The shell is a pane tree, not fixed slots.** `composables/dock.ts`
  owns the persisted tree (splits + panes, JSON under the desktop
  persistence key); `DockSplit`/`DockPane`/`DockDivider` render it. A
  bare PANE can be the tree root (all sidebars closed) - the renderer
  has an explicit single-pane root branch; a tree with zero panes is a
  bug, not a state.
- **Pane model (Endo's reset spec, landed)**: left sidebar = sessions
  pane (pinned singleton, no X); right sidebar = inspector pane with
  five fixed tabs (Files, Cron, Logs, Skills, Tools - no X); middle =
  main pane. The main pane is the last-unclosable pane (the AWE always
  keeps >= 1 pane; the X on main only shows on hover, and only when
  more than one pane exists). Opening a session or hitting 'new' opens
  in the MAIN pane and focuses the existing tab instead of reopening.
- **Dockables** (`composables/dockables.ts`): the registry of openable
  domains (sidebar, inspector, chat, settings). Settings is a dock-tab
  singleton since the dock arc - it opens from the pane '+' menu, not
  a menubar gear (the gear's removal is carded).
- **Module-owned UI (build-time composition)**: modules ship UI in
  `modules/<name>/ui/`; `nuxt.config.ts` composes them by array -
  `app/composables/module-panels.generated.ts` is generated at build
  time and there is NO runtime registry. The tools inventory panel is
  the canary (modules/tools/ui/ToolsList.vue).
- **Theming**: a `themes/` folder in the omega home (one YAML per
  theme, token overrides) picked up hot without restart; the settings
  theme picker applies live.
- **Retry semantics (decided)**: regenerate-in-place via the server -
  `POST /chat {"retry": true}` re-runs the session's LAST user message
  without persisting a duplicate user bubble; a cancelled/partial
  response stays visible and the new response appends below. The former
  client-side `dropLastExchange` transcript surgery is RETIRED - never
  reintroduce it.

## Ownership

- `app.vue` - the orchestrator: session open/create routing (open in
  the main pane, focus-existing tab), URL sync (`/chat/<id>` vs `/`,
  pure helpers in composables/omega.ts), scroll pinning (follows only
  the stream whose session is being viewed; the `pinned` flag changes
  only on real scroll events - never re-measure after appending),
  stacked-mode layout, live stream telemetry, tab-title spinner.
- `composables/chat.ts` - the stream driver (per-tab state incl.
  image attach state). CRITICAL VUE TRAP (bit us once, live-proven):
  after pushing an item into the reactive transcript, re-capture the
  reactive proxy (`st.transcript[len-1]`) before mutating it - raw-ref
  mutations bypass the proxy and never re-render (the stuck-cursor /
  ticking-timer bug class).
- `composables/omega.ts` - typed client for the http_channel API:
  SSE `parseFrame`/`handlePayload` (error payloads THROW - a swallowed
  stream error is an invisible dead stream), `streamChat` (throws
  `StreamInterruptedError` when the reader errors or ends without
  `agent_end`), sessions CRUD + pinned/read PATCH + archive/unarchive,
  unified search, transcript fetch, files browse/read, image attach,
  `renderMarkdown` (markdown-it, `html: false` - the lib's default
  escaping is the sanitizer), `createRenderScheduler` (debounced
  streaming render, injected timers), pure helpers for session rows
  (`sessionRowStatus`), buckets, token formatting.
- `components/` - root: `DockSplit`/`DockPane`/`DockDivider` (the
  pane tree), `ChatTab` (a chat pane: transcript + composer, live-pin
  layout), `ChatItem`. By concern:
  - shell: `MenuBar` (brand, toggles), `StatusBar` (health dot,
    token/ctx/rate telemetry, build version + uptime), `SearchInput`,
    `PaneDivider` (legacy divider - magnetic snap rebase mechanics)
  - chat: `AppComposer` (auto-grow textarea, ONE morphing action
    button: send/stop/retry - draft beats retry, '+' opens the file
    picker, attach-strip with image thumbnails, paste and paperclip
    attach paths), `ModelSelect` + `ThinkingSelect` (custom
    listboxes - a native `<select>` popup is OS chrome that cannot be
    themed), `ChatMessage` (markdown + images), `ThinkingBlock`,
    `ToolCallLine` (running/done/error with expandable result)
  - sessions: `AppSidebar` (session list: search, pinned section,
    day buckets, per-row pin/status affordance, archive/trash hover
    actions, archived group)
  - files: `FileTree`, `FilesTab`, `InspectorPanel` (the inspector's
    five-tab host; module panels slot in)
  - settings: `SettingsTab` (28+ knobs as a dock-tab singleton)
- `assets/css/main.css` - design tokens, light/dark themes, shell
  geometry constants, stacked-mode rules (full-width rows; thinking on
  background).
- `test/` - 28 files, 374 unit tests (node's built-in runner;
  node --test "test/*.test.ts"). Notable: `interrupted.test.ts`
  (transport-death detection + transcript transitions - NO
  dropLastExchange anymore), `composer-retry.test.ts` (the morphing
  button + retry flow), `panemodel.test.ts` (the reset spec's
  invariants), `dock.test.ts`/`dockables.test.ts`/`panes.test.ts`
  (tree model), `composition.test.ts` (module UI composition),
  `themes.test.ts`, `images.test.ts` (attach state), `stacked.test.ts`,
  `reattach.test.ts` (stream reattach via runHub events).
- `scripts/build.sh` - `nuxt generate` (NOT `nuxt build` - ssr:false
  build emits a nitro server, generate emits the static SPA) + sync to
  `../http_channel/static/`. Run before `go build`; the synced output
  is committed with webui changes. The build gate refuses to ship a
  SPA built from conflicted/stash-poisoned source.
- `scripts/stream_check.py` - transport probe: POSTs a reasoning
  prompt, asserts incremental chunk arrival. Exit 0 healthy, 1
  buffered, 2 no stream.

## Local Contracts

- **Geometry constants are contracts.** MenuBar 35px; StatusBar 29px;
  chat floor 320px; gutter 8px; mobile breakpoint 768px. Changing one
  requires re-checking the others - tuned over many measured rounds.
- **Streaming renders chronologically.** A tool phase ends the current
  response segment; text after tools is a fresh bubble (mirrors
  `accumulateAssistant` in the backend - same boundary, both sides).
  Thinking blocks accumulate per reasoning run. Tool results attach to
  their calls by `tool_call_id`, not position.
- **Stacked mode**: every block (user, assistant, thinking, toolcall)
  renders full width on the background; thinking strips de-card on
  dark. Chronological order NEVER changes - live-pin is scroll +
  sticky layout (the pinned-question header card owns it), not render
  order.
- **Statusbar telemetry is honest.** The token counter ticks per chunk
  (chars/4 estimate, corrected by real usage at stream end - see the
  `ponytail:` comment); live chunks/s comes from real arrival times.
- **The built SPA is committed.** `modules/http_channel/static/` is
  tracked; `go install` and plain `go build` must produce the complete
  product. After webui changes: run `scripts/build.sh` and commit the
  synced output with the change. Never hand-resolve static/ conflicts
  - regenerate.
- **Design tokens live in main.css.** Accent #E8833A (the single
  accent); `--unread` is an unread-ONLY semantic color; radii 8px
  (panes) / 6px (cards) / 4px (chips). Type: Public Sans + JetBrains
  Mono. No component invents colors. Theme YAMLs override tokens at
  runtime - new tokens must carry a sane default in main.css.

## Work Guidance

- The visual feedback loop is the user's screenshots + the in-app
  comment system (headless browser checks are blocked on missing
  Chromium deps on the Pi; a real browser session via browser_exec
  works). In-app comment rounds keep scope narrow - never split one
  DOM group across changes; groups derive from page structure.
- Verify SERVED content, never source strings - grep the served CSS/
  module, not the .vue file.
- Vite dev-server cache ghosts are real: on mismatch, restart the dev
  server and hard-refresh before debugging.
- ReferenceError in a setup script passes every gate including the
  health check - only a render catches it (the NUXT_E1005 lesson:
  `go test` and `node --test` never see it, vue strips types and no
  template check runs). Same family, different face: signal-shape
  mismatches (a `Ref` compared against its `.value` string) pass every
  gate too, and a unit test that unwraps the ref itself proves
  nothing - tests must receive the shape production actually sends
  (auto-focus lesson, 2026-10-20: shipped 100% dead, all gates green).

## Verification

```bash
node --test "test/*.test.ts"       # 374 unit tests
bash scripts/build.sh               # SPA generate + sync to http_channel/static
python3 scripts/stream_check.py http://127.0.0.1:8098   # transport probe (dev omega must run)
```

## Child DOX Index

No sub-packages. Module UIs live in each module's `ui/` folder (see
modules/README.md for the composition contract).