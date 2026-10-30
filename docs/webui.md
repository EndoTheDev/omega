# Web UI Reference

The omega web interface: a Nuxt 4 single-page app with a dock-based
shell (VS Code-style: menu bar, persisted pane tree, status bar). In
dev it runs via `nuxt dev` with a nitro devProxy to the dev omega on
`127.0.0.1:8098`. In production the built SPA is embedded into the
omega binary via `go:embed` and served by the HTTP channel at `/`. An
installed instance owns port 8099; the dev tree's omega serves 8098.
The two never touch each other.

Source of truth: `engine/webui/` (contracts below), `engine/http_channel/static/`
(the committed built bundle), and the wire contract in [api.md](api.md).

## Design tokens

Tokens live in `assets/css/main.css` — no component invents colors.

- Accent: `#E8833A` (the single accent).
- `--unread` is an **unread-ONLY semantic color** — never reuse it for a
  generic highlight.
- Radii: 8px (panes) / 6px (cards) / 4px (chips).
- Type: Public Sans (`--sans`) + JetBrains Mono (`--mono`) — tokens in
  main.css `:root`. Body/UI base is 15px and every size is px: there is
  no rem scale or hero heading style (UI text sits mostly at 12–13px,
  mono at 10.5–12px; sizes are per-component, not tokens).
- Dark-first, light-parity: dark is the default identity; light is full
  parity via the `html[data-theme='light']` block in main.css — the same
  ladder inverted (`#ececec` z0 / `#f7f7f8` z1 / `#ededf0` z1-raised /
  `#ffffff` z2, stroke `#d5d5da`, text `#1a1a1d`, text-dim `#6a6a72`,
  plus the light shadow overrides). Theme selection rides `auto |
  light | dark` (`ThemeMode`, composables/omega.ts) plus `theme:<name>`
  custom themes from the picker.
- Theme overrides: YAML files in `<home>/themes/` (one per theme,
  token overrides, `base:` + `name:`). Served by the HTTP channel
  (`GET /themes`, hot-reloaded from the folder poll), applied live from
  the theme picker. Overrides are allowed on the color tokens + two
  layer shadows only — radii and fonts are deliberately not
  overridable (v1 is recoloring, not restyling, and tokens map 1:1 to
  module components' styling, so overrides recolor modules for free).
  New tokens must carry a sane default in main.css.

## Geometry contracts

Constants in `main.css`. They are contracts, not suggestions — tuned
over many measured rounds, and changing one requires re-checking all
the others:

| Constant          | Value   |
| ----------------- | ------- |
| MenuBar height    | 35px    |
| StatusBar height  | 29px    |
| Chat floor        | 320px   |
| Gutter (divider)  | 8px     |
| Mobile breakpoint | 768px   |

The gutter divider retains magnetic snap rebase mechanics
(`PaneDivider`). Sidebar panes obey a 128px floor (`MIN_PANE_PX`,
composables/dock.ts) while the main pane's floor is the 320px wall —
the sidebars yield first. Below the mobile breakpoint the sidebars
collapse into slide-in drawers over a scrim; chrome (menu/status bar)
rides above all overlays: drawers z12, scrim z10.

The universal panel band (`components/ui/TabBand.vue`) is a geometry
contract too: every tab panel (module tabs: files, cron, logs, skills,
tools, agents, notifications, insights, settings, kanban, mcp,
extensions; plus the sessions sidebar) renders ONE band (tab title
left, tab actions right, 22px min-height, 14px lead-in clearing the
-8px pane tuck, `lead=false` where the host supplies its own padding).
The band renders OUTSIDE the panel's scrollport, structurally pinned:
scrolling the body never moves the band. Band action buttons use the
shared `.band-tool` look (dimmed at rest, hover raises, disabled 0.4).

## Motion

main.css's header states the law: "Motion: streaming cursor + message
fade only." Three keyframes exist and nothing else moves via
`animation` — `blink` (1s steps(2): streaming cursor + thinking-live
ink), `fade-in` (0.18s message arrival), and `status-pulse` (1.6s:
session-row streaming status glyph — dot or star, not just color).
Everything else animates through `transition`, not keyframes.

## The dock model

- **The shell is a pane tree, not fixed slots.** `composables/dock.ts`
  owns the persisted tree (splits + panes, under the desktop
  persistence key); `DockSplit`/`DockPane`/`DockDivider` render it. A
  bare pane can be the tree root (all sidebars closed) — the renderer
  has an explicit single-pane root branch. A tree with zero panes is a
  bug, not a state.
- **Pane model**: left sidebar = sessions pane (pinned singleton, no
  X); right sidebar = inspector pane with the fixed module tabs (Files,
  Cron, Skills, Tools, Agents, Settings, Kanban, Modules, Insights,
  MCP, Notifications — no X); the bottom panel under the main pane
  hosts Logs (bottom-only — its sidebar copy was ruled a duplicate and
  removed); middle = main pane. The LAST main pane never closes its
  PANE (Endo ruling, 2026-10-25, corrected t_b620f99d): `removePane`
  (and the move-out
  auto-close) refuse it as a no-op — the main area never reaches zero
  panes. The pane X shows on hover and only while another MAIN pane
  exists (pinned shells don't count toward that census). Every TAB's
  close X always renders: the law protects the pane, not the tabs, and
  closing the last tab empties the pane into its launcher state (the
  pane itself is a main pane and never leaves the tree).
- **Sessions lock to the primary sidebar** — open a session or hit `new`
  and it opens in the MAIN pane, focusing the existing tab instead of
  reopening.
- **Dockables** (`composables/dockables.ts`): the registry of openable
  domains — the pinned shells (sidebar, the inspector tabs above) plus
  chat and viewer in the main pane (the only closable, multi instances;
  viewer's only entry point is a row click). `inspector` is legacy
  only: deserialization still accepts the persisted id and modernizes
  it, nothing opens one. `continuous` and the `canvas` host tab left
  the registry — the continuous roster folded into the Agents tab, the
  canvas renderer into the Modules tab (`composables/canvas.ts` stays
  as the shared view-model store — snapshot GET /ui/canvas, delta
  merge, the action pipe, the toast funnel). Settings has no entry
  point: the menubar gear and the `+`-menu entry are gone; persistence
  and the default arrangement are its only paths.
- **Tab drag-and-drop** moves tabs between panes; sessions stay in the
  sidebar.
- **Module-owned UI (build-time composition)**: modules ship UI in
  `modules/<name>/ui/`; `nuxt.config.ts` composes them by array —
  `app/composables/module-panels.generated.ts` is generated at build
  time and there is NO runtime registry. `modules/tools/ui/ToolsList.vue`
  is the canary.

## Streaming + rendering rules

- Streaming renders **chronologically**. A tool phase ends the current
  response segment; text after tools is a fresh bubble (mirrors
  `accumulateAssistant` in the backend — same boundary, both sides).
  Thinking blocks accumulate per reasoning run; tool results attach to
  their calls by `tool_call_id`, not position.
- Stack mode: every block renders full width on the background;
  thinking strips de-card on dark. Order never changes — live-pin is
  scroll + sticky layout, not render order.
- Statusbar telemetry is honest: the token counter ticks per chunk
  (chars/4 estimate, corrected by real usage at stream end); live
  chunks/s comes from real arrival times.
- Scroll pinning follows only the stream whose session is being
  viewed; the `pinned` flag changes only on real scroll events — never
  re-measure after appending.
- Retry is regenerate-in-place via the server: the action row's retry
  chip (composer face retired, t_95d2bc34) dispatches the chat panel's
  retry action through the pipe - it re-runs the session's LAST user
  message as a sibling variant without persisting a duplicate user
  bubble; a cancelled/partial response stays visible and the new
  response appends below. The chip renders only on the NEWEST assistant
  response while the run is idle. Client-side `dropLastExchange`
  transcript surgery is RETIRED - never reintroduce it.
- Vue reactivity trap (bit once, live-proven): after pushing an item
  into the reactive transcript, re-capture the reactive proxy
  (`st.transcript[len-1]`) before mutating it — raw-ref mutations
  bypass the proxy and never re-render. This is the stuck-cursor /
  ticking-timer bug class.
- Error payloads in the SSE driver THROW (`composables/omega.ts`) — a
  swallowed stream error is an invisible dead stream.

## Ephemeral chat (card t_d96a5cd5)

- An ephemeral chat is a **chat tab, not a session**. Entry point: the
  sessions panel's `+ new` dropdown (`new chat` / `new ephemeral
  chat`). The tab carries the `EPHEMERAL_DRAFT_REF` marker until the
  send; the server mints an `eph-` id on the leading `session` frame.
- **Pure in-memory.** No store row, no messages on disk: conversation
  context, transcript items and telemetry live in the server's
  `ephRegistry` (engine/http_channel/server_ephemeral.go). Restart
  kills; reload reattaches from server memory (transcript endpoint,
  same reattach machinery). Never a persisted-then-deleted shape.
- **Invisible to every store-derived surface by law**: session lists,
  search (FTS5), export, counts, TUI `/sessions` — the store never
  sees it. The `eph-` prefix is the namespace guard every endpoint
  routes on.
- **Memory tool denied** (run-scoped, `agent.WithDeniedTools`, the
  loop filters post-merge so extension ToolProviders cannot
  reintroduce it). Ceiling: nothing blocks files/shell writes to
  memory.md yet — that is the parked sandbox round's job; it does not
  block ephemeral.
- **Close = gone** (v2 has no upgrade-to-persist; a persist action is
  a separate follow-up card). Closing the last tab on an `eph-` id
  DELETEs the registry record, even mid-run (the run cancels first).
- Retry/fork/variants never exist on an ephemeral tab — the store
  tree they replay is absent. The server 400s the flag combination.

## The composer's `/` palette and `@` mentions (card t_07aed3f9)

Parity with the TUI (`engine/tui/tui.go` is the spec, not a new
design):

- **`/` at input start opens the command palette.** Rows: the webui's
  shell commands (`composables/commands.ts` `WEBUI_SHELL_COMMANDS` —
  the TUI's knownCommands minus `/exit`, plus `/new` and `/sessions`;
  `/copy /modules /fork /help /panels /retry /theme /variant` map
  to webui-native local actions; `/extensions` rides the palette as
  the /modules alias), then extension commands from
  `GET /commands`, then skills as `/<skill-name>` rows — the TUI's
  `buildCommands` assembly order. Typing filters case-insensitively
  over name AND description; ArrowUp/ArrowDown navigate, Enter picks
  or (with the draft not in palette mode) submits, Esc dismisses.
- **Shell commands act LOCALLY and never hit the agent.** `/fork`,
  `/retry`, `/variant`, `/theme` and friends run through the app's own
  handler; extension commands and `/<skill-name>` go to
  `POST /commands/dispatch` (engine-local execution — a module command
  returns its result text; a skill returns content the NEXT send arms
  as a system preamble). api.md's "Commands" section documents the
  wire.
- **`@` opens the mention picker.** A bare `@` (or a non-prefixed
  token) lists workspace files plus the two prefix rows;
  `@session:` lists sessions, `@skill:` lists skills. Picking inserts
  the completed token at the caret. The picker only helps TYPE tokens
  — expansion is SERVER-SIDE on the `/chat` submit path through the
  one `core.ExpandAtMentions` law the TUI submit path also calls
  (`core/atmention.go`). The browser never reads or expands files;
  unresolvable tokens stay literal in the message.

## Component map

- `app.vue` — orchestrator: open/create routing (open in the main pane,
  focus-existing), URL sync (`/chat/<id>` vs `/`), scroll pinning,
  stacked-mode layout, live telemetry, tab-title spinner.
- `composables/chat.ts` — the stream driver (per-tab state incl. image
  attach). `composables/omega.ts` — typed API client: SSE
  `parseFrame`/`handlePayload`, `streamChat` (throws
  `StreamInterruptedError` when the reader errors or ends without
  `agent_end`), sessions CRUD + pinned/read PATCH + archive/unarchive,
  search, transcript fetch, files browse/read, image attach,
  `renderMarkdown` (markdown-it, `html: false` — the lib's default
  escaping is the sanitizer), `createRenderScheduler`.
- Shell: `MenuBar` (brand, toggles), `StatusBar` (health dot,
  token/ctx/rate telemetry, build version + uptime), `SearchInput`,
  `PaneDivider`, `TabBand` (the universal panel top bar: title left,
  actions right, outside every scrollport).
- Chat: `AppComposer` (auto-grow textarea, ONE morphing action button -
  send/stop, dimmed at idle with no draft, `+` opens the file picker,
  attach-strip with image thumbnails, paste + paperclip attach; the
  retry face is retired - the newest response's action row carries the
  msg-retry chip, card t_95d2bc34; `/` command palette + `@` mention
  picker, see below),
  `ModelSelect` + `ThinkingSelect` (custom listboxes — a native
  `<select>` popup is OS chrome that cannot be themed), `ChatMessage`,
  `ThinkingBlock`, `ToolCallLine` (running/done/error, expandable
  result), `ChatTab` (transcript + composer, live-pin layout),
  `ChatItem`.
- Sessions: `AppSidebar` (search, pinned section, day buckets, per-row
  pin/status affordance, archive/trash hover actions, branch/rename/
  export row actions, archived group).
- Files: `FileTree`, `FilesTab`, `InspectorPanel` (module panels slot
  in). Settings: `SettingsTab` (the config knobs as a dock-tab
  singleton). Canvas (arc 3c): `composables/canvas.ts` is the store —
  snapshot load (GET /ui/canvas) + view_model_delta merge (forwarded
  by the chat host's `canvasDelta` dep) + the action pipe POST
  (optimistic pending, error state on 4xx/5xx); the standalone
  generic host tab (`components/canvas/CanvasPanel.vue`) is gone —
  folded into the Modules tab, the chat transcript renders FROM the
  canvas view-model.

## Testing

```bash
node --test "test/*.test.ts"        # from webui — node's built-in runner
bash scripts/build.sh               # SPA generate + sync to engine/http_channel/static
python3 scripts/stream_check.py http://127.0.0.1:8098   # transport probe (dev omega must run)
```

`stream_check.py` exit codes: 0 healthy (incremental chunks), 1 buffered
(no incremental arrival), 2 no stream at all.

**The test law: tests receive the signal shape production sends.** A
unit test that unwraps a ref itself proves nothing — the auto-focus
lesson (2026-10-20): shipped 100% dead with every gate green, because
the test compared a `Ref` against its `.value` string, a signal-shape
mismatch that passes every gate including the health check. Same
family: a ReferenceError in a setup script passes every gate and only a
render catches it (the NUXT_E1005 lesson: `go test` and `node --test`
never see it, vue strips types, and no template check runs). Tests bind
at the production boundary or they do not exist.

Notable suites: `interrupted.test.ts` (transport-death detection + transcript
transitions), `composer-retry.test.ts` (the morphing button), `panemodel.test.ts`
(the pane invariants), `dock.test.ts`/`dockables.test.ts`/`panes.test.ts`
(tree model), `canvas.test.ts` (the canvas snapshot/delta/action flow),
`composition.test.ts` (module UI composition),
`themes.test.ts`, `images.test.ts`, `stacked.test.ts`,
`reattach.test.ts` (stream reattach via runHub events).

## The embed lane

- The built SPA is COMMITTED: `engine/http_channel/static/` is
  tracked, so `go install` and plain `go build` always produce the
  complete product. Webui cards commit **SOURCE ONLY**; the merge pass
  runs `scripts/embed-lane.sh`, which owns every `static/` commit.
- `scripts/build.sh` runs `nuxt generate` (NOT `nuxt build` — an
  ssr:false `nuxt build` emits a nitro server; generate emits the
  static SPA) and syncs to `../engine/http_channel/static/`. Run it before
  `go build`.
- Never hand-resolve `static/` conflicts — regenerate.

### Poisoned-bundle diagnosis

Conflict markers appearing in the SERVED UI = the SPA was built from
conflicted/stash-poisoned source (the build gate refuses to ship that,
but a manual build can slip one through). Diagnosis: grep the SERVED
content, never source strings — `grep -r` over the served CSS/module
files (find-based, conflict-marker pattern), then regenerate via
`scripts/build.sh`, re-serve, and verify the regenerated bundle came
out clean. A grep against the `.vue` sources will lie to you.

## Work guidance

- The visual feedback loop is the user's screenshots + the in-app
  comment system (headless-browser checks blocked on missing Chromium
  deps on the Pi; a real browser session works). In-app comment rounds
  keep scope narrow — never split one DOM group across changes.
- Verify SERVED content, never source strings.
- Vite dev-server cache ghosts are real: on mismatch, restart the dev
  server and hard-refresh before debugging.

## Related

- [api.md](api.md) — the HTTP wire the webui rides
- [tui.md](tui.md) — the TUI equivalent of this surface