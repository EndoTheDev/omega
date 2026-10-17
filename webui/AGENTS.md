# webui

## Purpose

The omega web interface: a Nuxt 4 single-page app with a VS Code-style
shell (menu bar, resizable floating panes, status bar). In dev it runs
via `nuxt dev` with a nitro devProxy to the dev omega on `127.0.0.1:8098`.
In production it is NOT served from a folder - the built SPA is embedded
into the omega binary via `go:embed` and served by the HTTP channel at
`/`. The installed instance owns port 8099; the dev tree's omega serves
8098 and the two never touch each other.

## Ownership

- `app.vue` - the orchestrator. Send flow (segment split, lazy bubble
  push, streaming state - PER SESSION, not global), session restore
  (`openSession`, tool-result matching by `tool_call_id`), URL sync
  (hand-rolled History wiring: openSession/newChat pushState
  `/chat/<id>` / `/`, popstate re-opens the URL's session - pure view
  change, never interrupts a stream; first send replaceStates the
  real id, unknown id loads home; `parseSessionPath`/`sessionPath`
  pure, unit-tested in test/routepath.test.ts), scroll
  pinning (follows only the stream whose session is being viewed), pane
  geometry (sidebar widths, squeeze chain, drag caps, push-back),
  pane open-state persistence (desktop-only `omega.sidebar-open` /
  `omega.inspector-open` guarded by the same 768px breakpoint as the
  overlay mode - mobile drawers always open closed, and a live
  desktop-to-phone squeeze crossing never rewrites the desktop flags),
  overlay exclusivity (one drawer on mobile; settings and the
  inspector share the right slot), theme handling, live stream
  telemetry (token estimate + chunks/s, kept per session), tab-title
  spinner (read-only over the per-session busy set).
- `composables/omega.ts` - typed client for the http_channel API.
  Pure, testable core: `parseFrame` (SSE frame -> payload) and
  `handlePayload` (payload -> handler dispatch; error payloads THROW -
  a swallowed stream error is an invisible dead stream; `agent_end`
  marks the terminal event in `state.sawEnd`). Plus the fetch helpers
  (health, models, sessions CRUD incl. the pinned PATCH and the
  read-mark PATCH, files browse/read, `streamChat` -
  throws `StreamInterruptedError` when the reader errors or ends
  without `agent_end`, so the transport death is distinguishable from
  a clean end), the assistant-bubble markdown renderer
  (`renderMarkdown`, standard markdown-it, `html: false` - the lib's
  default escaping is the sanitizer), and `createRenderScheduler`
  (debounced streaming render: one render per ~150ms tick, injected
  timers for pure tests).
- `components/` - 11 components grouped by concern:
  - shell: `MenuBar` (brand, toggles, gear), `StatusBar` (health dot,
    token/ctx/rate telemetry), `AppSidebar` (session list: search box
    filters labels client-side, collapsed-section state persists under
    `omega.sidebar-collapsed` as a JSON array of bucket labels -
    unknown/malformed entries restore expanded, PINNED section above
    the Today / Yesterday / Earlier local-calendar-day buckets,
    per-row relative age sits absolute right of the two-line row,
    vertically centered, + a model / msgs / tools / tokens meta line,
    and ONE status element per row at the LEFT edge - a real button
    (aria-label reflects the pin action) whose shape channel covers
    pin (unpinned rows show a dot, pinned rows always a star, hovering
    an unpinned row morphs the dot into the star affordance) and whose
    color channel covers state (grey idle, pulsing-orange streaming
    even when pinned, `--unread`-blue unread; priority streaming >
    unread > idle), derived by the pure `sessionRowStatus` in
    composables/omega.ts; clicking it pins/unpins via app.vue's
    `togglePin`),
    `RightSidebar` (inspector pane: a tab shell - parked tabs slot in
    later - whose Files tab is a breadcrumb file manager over the
    working directory: dir rows descend, file rows open a read-only
    64 KiB-capped mono preview reporting truncated/binary;
    the listing lazy-loads on first open and the pane's open state
    persists desktop-side under `omega.inspector-open` (app.vue)),
    `SettingsDrawer`
    (headless - the gear is the toggle)
  - chat: `ChatMessage`, `ThinkingBlock`, `ToolCallLine` (running/
    done/error with expandable result), `AppComposer` (auto-grow
    textarea, expand button, send), `ModelSelect` (custom listbox with
    top-fixed filter search - a native `<select>` popup is OS chrome
    that cannot be themed)
  - geometry: `PaneDivider` (drag resumes from rendered
    `offsetWidth`, not the stored basis; emits `collapsed` below min)
- `assets/css/main.css` - design tokens, light/dark themes, all shell
  geometry constants (documented in comments next to the rules they
  describe)
- `test/omega.test.ts` - unit tests for the SSE parser + dispatch
  (10 tests, node's built-in runner; includes the error-throw
  regression test)
- `test/streamstate.test.ts` - unit tests for the per-session busy-map
  logic (`withBusy`/`isBusy`/`anyBusy` in composables/omega.ts): busy(A)
  never affects B; spinner derives from any-busy; base set never mutated
  (5 tests)
- `test/sessionlist.test.ts` - unit tests for the session list helpers
  in composables/omega.ts: `bucketSessions` (local-day buckets, pinned
  sessions excluded from their day bucket, malformed dates land in
  Earlier), `relativeAge`, `filterSessions`, `formatTokens` (k-abbr
  above 1000), `rowMeta` (unknown pieces omitted; session with nothing
  known renders no meta line), `parseCollapsedLabels`/`serializeCollapsedLabels`
  (persisted collapsed-section value: JSON array, unknown/non-string entries
  dropped, malformed JSON parses empty) (16 tests)
- `test/sessionstatus.test.ts` - unit tests for the pure row-status
  selector `sessionRowStatus` in composables/omega.ts: shape channel
  (pinned star, hover morph, unpinned dot) x color channel (idle grey,
  streaming pulse wins over unread, unread blue suppressed for the
  viewed session) incl. priority and neutral defaults (13 tests)
- `test/markdown.test.ts` - unit tests for the assistant-bubble
  markdown path in composables/omega.ts: `renderMarkdown` (plain text /
  inline styling / fenced code, partially-received fences still render
  as code, tables, raw HTML + event handlers + javascript: hrefs never
  render) and `createRenderScheduler` (chunk bursts coalesce into one
  render per tick, cancel drops the pending render, custom interval
  honored - fake clock, injectable timers) (13 tests)
- `test/routepath.test.ts` - unit tests for the URL-sync path helpers
  (`parseSessionPath`/`sessionPath` in composables/omega.ts) (5 tests)
- `test/polish.test.ts` - unit tests for the polish helpers in
  composables/omega.ts: `formatMsgTime` (local HH:MM for today, date-
  prefixed for older days, other years include the year), the thinking
  elapsed meter (`formatElapsed` + `createElapsedMeter`, fake clock -
  ticks per second, stop clears the timer and freezes the final label),
  and `isAtBottom` / `pillVisible` (plain numbers; the check is strict -
  distance exactly at the threshold is not at bottom; a non-scrollable
  container counts as at bottom and never shows the pill) (20 tests)
- `test/interrupted.test.ts` - unit tests for the interrupted-stream
  marker + retry: streamChat transport-death detection (stream without
  `agent_end` throws `StreamInterruptedError`; a reader error
  mid-stream throws it too; agent error payloads still throw plain
  Errors) and the transcript transitions (`lastUserText`,
  `dropLastExchange`, `withoutMarkers`) (13 tests)
- `test/inspector.test.ts` - unit tests for the file-manager helpers
  in composables/omega.ts: `cleanRelPath` (mirrors the backend segment
  rules), `joinRelPath`, `fileCrumbs`, `sortFileEntries` (7 tests)
- `scripts/build.sh` - `nuxt generate` (NOT `nuxt build` - ssr:false
  build emits a nitro server, generate emits the static SPA) + sync
  to `extensions/http_channel/static/`. Run before `go build`; the
  synced output is committed with webui changes.
- `scripts/stream_check.py` - transport probe: POSTs a reasoning
  prompt, asserts incremental chunk arrival (>=90% gaps <300ms,
  <=60% of frames in any 1s burst). Exit 0 healthy, 1 buffered,
  2 no stream.
- `nuxt.config.ts` - SPA config; devProxy targets :8098 (dev).
- `package.json` - `npm test` runs the unit tests (node >= 23 runs TS
  natively).

## Local Contracts

- **Geometry constants are contracts.** MenuBar 35px; StatusBar 29px
  (coupled to pane-area bottom padding `0 4px 3px` - 29 = 32 - 3);
  chat floor 320px (min-width AND min-height); exact gutter 8px (two
  4px dividers); sidebar walls `calc(100% - 320px - 4px)`; collapse
  threshold 128px (VS Code mechanic, deliberate); mobile breakpoint
  768px; mobile drawers `calc(100% - 30%)` sliding over a scrim
  (rgba 0 0 0 / 0.65). Changing one requires re-checking the others -
  they were tuned over many measured rounds.
- **Streaming renders chronologically.** A tool phase ends the current
  response segment; text after tools is a fresh bubble (mirrors
  `accumulateAssistant` in the backend - same boundary, both sides).
  Thinking blocks accumulate per reasoning run. Tool results attach
  to their call by `tool_call_id`, not position.
- **Scroll pinning is intent-based.** The `pinned` flag changes only
  on real scroll events - never re-measure "am I at the bottom" after
  appending content: content bursts larger than the 40px threshold
  masquerade as user scroll and silently kill the follow.
- **Overlay exclusivity.** Mobile: one drawer at a time - opening any
  pane closes the other two. Desktop: the right slot hosts either the
  inspector or settings, never both; the gear and the inspector toggle
  swap occupants with the same push-back logic.
- **Statusbar telemetry is honest.** The token counter ticks per chunk
  (chars/4 estimate, corrected by real usage at stream end - see the
  `ponytail:` comment); live chunks/s comes from real arrival times,
  which is why it doubles as the frontend-streaming proof.
- **The built SPA is committed.** `extensions/http_channel/static/`
  is tracked; `go install` and plain `go build` must produce the
  complete product. After webui changes: run `scripts/build.sh` and
  commit the synced output with the change.
- **Design tokens live in main.css.** Light z-stack
  #ececec/#f7f7f8/#ededf0/#ffffff; dark #121212-family; accent
  #E8833A (the single accent); `--unread` joins the token block as an
  unread-ONLY semantic color (dark #5B9DF9, light #2563EB - never reuse
  it for decoration); radii 8px (panes) / 6px (cards) / 4px (chips);
  `--shadow-1` 0 1px 4px (dark 0.40, light 0.10) - tight blur sized
  so falloff fits the 3px breathing room. Type: Public Sans +
  JetBrains Mono. No component invents colors.

## Work Guidance

- The visual feedback loop is the user's screenshots (headless browser
  checks are blocked on missing Chromium deps on the Pi). Design
  reviews flow through DESIGN.md; in-app comment rounds keep scope
  narrow - never split one DOM group across changes.
- Vite dev-server cache ghosts are real (verified source + stale
  served bytes): on mismatch, restart the dev server and hard-refresh
  before debugging.
- Verify SERVED content, never source strings - grep the served CSS/
  module, not the .vue file.
- `grep -c` counts lines, not matches - use `grep -oE` for plain-rule
  checks on served CSS (base64 strings inflate counts).

## Verification

```bash
npm test                       # 102 unit tests (node --test "test/*.test.ts")
npx nuxt build                 # SPA compiles
python3 scripts/stream_check.py http://127.0.0.1:8098   # transport probe (dev omega must run)
```

## Child DOX Index

No sub-packages.