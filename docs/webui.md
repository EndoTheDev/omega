# Web UI Reference

The omega web interface: a Nuxt 4 single-page app with a dock-based
shell (VS Code-style: menu bar, persisted pane tree, status bar). In
dev it runs via `nuxt dev` with a nitro devProxy to the dev omega on
`127.0.0.1:8098`. In production the built SPA is embedded into the
omega binary via `go:embed` and served by the HTTP channel at `/`. An
installed instance owns port 8099; the dev tree's omega serves 8098.
The two never touch each other.

Source of truth: `webui/` (contracts below), `api/static/`
(the committed built bundle), and the wire contract in [api.md](api.md).

Design law lives in [`.agents/DESIGN.md`](../.agents/DESIGN.md) -
iconset, icon sizes, theme tokens, the composer shape, UI families,
a11y, the architecture bar. This file is the map: values, anatomy,
mechanics. Laws are not restated here.

## Design tokens

The token laws (the invention ban, unread-only `--unread`,
dark-first/light-parity, recolor-only overrides, defaults for new
tokens) live in [`.agents/DESIGN.md`](../.agents/DESIGN.md). The
value map, `assets/css/main.css`:

- Accent: `#E8833A` (the single accent).
- Radii: 8px (panes) / 6px (cards) / 4px (chips).
- Type: Public Sans (`--sans`) + JetBrains Mono (`--mono`) — tokens in
  main.css `:root`. Body/UI base is 15px and every size is px: there is
  no rem scale or hero heading style (UI text sits mostly at 12–13px,
  mono at 10.5–12px; sizes are per-component, not tokens).
- The light ladder (the `html[data-theme='light']` block in main.css):
  `#ececec` z0 / `#f7f7f8` z1 / `#ededf0` z1-raised /
  `#ffffff` z2, stroke `#d5d5da`, text `#1a1a1d`, text-dim `#6a6a72`,
  plus the light shadow overrides. Theme selection rides `auto |
  light | dark` (`ThemeMode`, composables/omega.ts) plus `theme:<name>`
  custom themes from the picker.
- Theme overrides: YAML files in `<home>/themes/` (one per theme,
  token overrides, `base:` + `name:`). Served by the HTTP channel
  (`GET /themes`, hot-reloaded from the folder poll), applied live from
  the theme picker — tokens map 1:1 to module components' styling, so
  overrides recolor modules for free.

## Geometry contracts

Constants in `main.css`. They are contracts, not suggestions — tuned
over many measured rounds, and changing one requires re-checking all
the others:

| Constant          | Value   |
| ----------------- | ------- |
| MenuBar height    | 35px    |
| StatusBar height  | 29px    |
| Tool pane floor   | 128px   |
| Chat pane floor   | 320px   |
| Gutter (divider)  | 8px     |
| Mobile breakpoint | 768px   |

**Honest floors** (cards t_ae9ac5cf + t_8bcbe72e): the floor is a
PER-COMPONENT registry fact, not a sidebar rule. `DOCKABLE_MIN_PX`
(composables/dockables.ts) attaches every dockable's `minPx` — chat is
the only 320px component (`CHAT_FLOOR_PX`), every tool component floors
at the shared 128px pane minimum (`MIN_PANE_PX`). One floor law backs
BOTH sides of every divider: the reveal's "no room" check and the
divider drag clamps read the same number through `paneFloorOf` /
`minDockPx` (composables/dock.ts) — a chat-holding pane keeps 320 on
both sides of a split; a tool pane keeps 128 from both sides. A reveal
or split that cannot seat every pane's floor does not squeeze into a
lie: it is refused and the shell surfaces the one-line toast
"no room - close a pane first" (app.vue) instead of a sub-floor pane.

The gutter divider (`PaneDivider`) retains its magnetic snap rebase
mechanics. On a drag between two FLEXIBLE panes the shares re-derive
from the frozen pre-drag baseline (c50b749) — the squeeze chain is
dead: a divider never pushes a third pane around (t_6a74d5a1's
wall-law). A divider with a FIXED pane on one side writes that pane's
px and the ADJACENT neighbor absorbs the change 1:1 - every other
flexible sibling holds its measured px (t_8098aa06; before it, the
flex reflow spread the delta over the whole flex space, so a small
pane beside a big one tracked the pointer at a fraction of the drag).
Fixed panes drag down to the shared 128 floor. Below the
mobile breakpoint the sidebars collapse into slide-in drawers over a
scrim; chrome (menu/status bar) rides above all overlays: drawers z12,
scrim z10.

**Same-pane split drag** (t_ae9ac5cf, the VS Code read): tab
drag-and-drop runs ONE pure formula behind both the commit and the
ghost preview. Dropping a tab on an EDGE zone splits the target pane
and lands the dragged tab in the new half-pane; a drop on the CENTER
zone focuses/reorders within the target's bar. A pane's OWN edge
half-zones split that pane — center body drops stay focus-only.
Refusals render the hatched-red ghost with the reason (a sidebar row
zone, depth caps, a grown re-growth, the locked sessions tab — it
never leaves the left shell).

The universal panel band (`components/ui/TabBand.vue`) is a geometry
contract too: every tab panel (module tabs: files, cron, logs, skills,
tools, agents, notifications, insights, settings, kanban, mcp,
extensions; plus the sessions sidebar) renders ONE band (tab title
left, tab actions right, 22px min-height, 14px lead-in clearing the
-8px pane tuck, `lead=false` where the host supplies its own padding).
The band renders OUTSIDE the panel's scrollport, structurally pinned:
scrolling the body never moves the band. Band action buttons use the
shared `.band-tool` look (dimmed at rest, hover raises, disabled 0.4).
The BAND POSITION CONTRACT (t_9ae4dea5): the tools/settings/insights
tab roots stop sitting the band inside — the band is the host frame's
top row, never a child of the scroll body (the 22/14 numbers above are
the shared contract; a module panel that renders its own inner band is
a review finding).

**Frame rails** (t_bc321318 → t_97cece1e, amended t_064b1559 /
t_e539ba29): a rail renders ONLY across a GUTTER between two
horizontally-adjacent cards — the row divider's own top/bottom strokes,
spanning exactly the divider's 4px width, aligned to the cards' border
rows. Nothing else strokes. The old whole-width pane-area strokes are
dead: a stroke never appears where a surface's own border is not —
one card means zero strokes, the card's border is the whole edge. The
menubar keeps its border-bottom and the statusbar its border-top; the
pane-area contributes no connecting stroke. Desktop law only.

## Registry rows and the toggle pill

The UI-family laws live in [`.agents/DESIGN.md`](../.agents/DESIGN.md).
The anatomy map:

**The registry row** (card t_561dc4e0): LINE 1 the name (semibold,
full opacity, ~13px), LINE 2 the subtitle (dim, smaller: the mono
schedule, the description, transport + status, module counts), and
exactly ONE control on the right, top-aligned with line 1. Expanded
content renders below the row-head in its own indent, seated on the
same paddings. Shared definition in
`assets/css/registry-row.css`, five consumers (cron, tools, skills,
mcp, modules). Status dots speak dot + word: green active, dim
sleeping (the agents roster's law); an enabled/disabled text label is
dead — the control column rides the pill.

**The toggle pill** (card t_d93864e7, `components/ui/TogglePill.vue`):
the 26px rounded pill with the 11px state DOT inside (dot shifts,
border goes `--ok` when on, transparent background). No `:checked`
prop exists for Vue's prop diff to skip, so the stale-arm re-render
trap is structurally dead for every host that adopts it. ONE CLICK IS
ONE SWITCH (card t_5051d523): the pill emits a single intent per
activation and carries no armed state and no confirm label, so the
arm/confirm two-step that used to make every toggle need a double
click cannot come back through the shared component. The host owns the
write path and re-renders the row from the server's answer; the
consequence text (what an exclusion means, a module's dependents)
rides the pill's `hint` as its title - informative on hover, never a
gate. Adopted across the registry tabs (skills, tools, agents, MCP,
modules) and the agents roster; the MCP tab's "from mcp.yaml" caption
is gone (rows speak transport + live state from GET /mcp/servers). The
default agent's pill is locked-on and non-interactive - the default
agent cannot be disabled (api.md's 409).

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
  hosts the Logs tab (the structured event stream over a raw-tail
  chip, card t_e703180e; named "Logs" since card t_01c53e78 retired
  the old raw-tail pane) and the Terminal;
  middle = main pane. The LAST main pane never closes its
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
  domains — the pinned shells (sessions sidebar left, the inspector
  tabs right, the bottom host under the main pane) plus the main-pane
  tabs: chat (multi, closable), the viewer (multi, closable,
  row-click only), the browser (singleton main tab, closable — the
  menu opener focuses the one instance), the terminal (bottom host,
  unclosable), the Logs tab (bottom host, unclosable - the
  structured run-event stream over the raw log tail; see its own
  section) and the notifications feed (right pane, unclosable,
  inbox glyph — the bell died). `inspector` is legacy
  only: deserialization still accepts the persisted id and modernizes
  it, nothing opens one. `continuous` and the `canvas` host tab left
  the registry — the continuous roster folded into the Agents tab, the
  canvas renderer into the Modules tab (`composables/canvas.ts` stays
  as the shared view-model store — snapshot GET /ui/canvas, delta
  merge, the action pipe, the toast funnel). Settings is an ON-DEMAND
  main tab (t_00e4a784): the menubar gear beside the right-sidebar
  toggle opens it, it closes like any tab, and the boot retires a
  persisted inspector seat (`retireInspectorSettings`). The `+` menu
  still carries no settings entry — the gear is the entry point.
- **Closing an edge pane HIDES state, never deletes it** (t_9ed6cad7):
  a sidebar/bottom pane's collapse removes the host from the tree but
  keeps the domain's state (search text, selected tab, scroll) so the
  reveal puts it back exactly as it was.
- **The give-away law** (t_66fa358e): pure window-narrow arithmetic —
  when the window cannot seat every fixed edge pane at its floor, the
  give-away squeezes the other edge roots to make room, but the
  component the user JUST revealed (the last request) is exempt: it
  survives, the other edge gives way. Reveal and resize read the same
  formula, so "no room" never fires while a give-away could seat the
  pane.
- **Sessions panel order** (t_311fc40b as amended by t_7ee0a673):
  the head row IS the universal band — tab title left, the search seats
  IN the band top right (band-tool slot, on the band's own 22px row;
  the separate search row below the band is dead), and the new-session
  control keeps its own full-width row under the band.
- **Tab drag-and-drop** moves tabs between panes (edge zones split,
  see the geometry section); sessions stay in the sidebar.
- **Boot repair** (t_841df625): a persisted dock shell from before a
  built host's bottom-home addition is repaired at boot — new
  bottom-home registry members are injected into the persisted bottom
  shell with the persisted layout untouched (the repair count is
  logged), so a fresh tab host survives old shells.
- **Module-owned UI (build-time composition)**: modules ship UI in
  `harness/<name>/ui/`; `nuxt.config.ts` composes them by array —
  `app/composables/module-panels.generated.ts` is generated at build
  time and there is NO runtime registry. `harness/tools/ui/ToolsList.vue`
  is the canary.
- **Mobile drawers resolve views, not panels** (t_e703180e, t_07ad4fda):
  below the 768px breakpoint every edge shell becomes an overlay drawer,
  and each drawer's tab chain ends in the generic `InspectorPanel`
  catch-all - which reads the PANEL manifest. A TAB capsule (terminal,
  notifications, observability, browser) and a shell-owned view (files,
  settings, extensions, insights, agents) therefore each need their own
  branch AHEAD of that catch-all, or a loaded tab renders "module not
  loaded". The bug is a width breakpoint, not a device: a desktop window
  resized under 768px shows it. `webui/test/drawerresolves.test.ts`
  derives every drawer-reachable id and fails the next one that forgets;
  only a real panel module may fall through to the catch-all.

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
  `ephRegistry` (api/server_ephemeral.go). Restart
  kills; reload reattaches from server memory (transcript endpoint,
  same reattach machinery). Never a persisted-then-deleted shape.
- **Invisible to every store-derived surface by law**: session lists,
  search (FTS5), export, counts `/sessions` — the store never
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

The webui is the shell; the palette is webui-native. Registration and
resolution mirror the one command law every surface follows:

- **A `/` token at the caret opens the command palette** (t_d1a2951d:
  caret/token-based, mid-sentence included - the draft's leading word
  is just the boundary-free case). Rows: the webui's
  shell commands (`composables/commands.ts` `WEBUI_SHELL_COMMANDS` —
  `/new` and `/sessions` plus the local-action set
  `/copy /modules /fork /help /panels /retry /theme /variant`;
  `/extensions` rides the palette as
  the /modules alias), then extension commands from
  `GET /commands`, then skills as `/<skill-name>` rows.
  Typing filters case-insensitively
  over name AND description; ArrowUp/ArrowDown navigate, Enter picks
  or (with the draft not in palette mode) submits, Esc dismisses.
- **Shell commands act LOCALLY and never hit the agent.** `/fork`,
  `/retry`, `/variant`, `/theme` and friends run through the app's own
  handler; extension commands and `/<skill-name>` go to
  `POST /commands/dispatch` (host-local execution — a module command
  returns its result text; a skill returns content the NEXT send arms
  as a system preamble). api.md's "Commands" section documents the
  wire.
- **`@` opens the mention picker.** A bare `@` (or a non-prefixed
  token) lists workspace files plus the two prefix rows;
  `@session:` lists sessions, `@skill:` lists skills. Picking inserts
  the completed token at the caret. The picker only helps TYPE tokens
  — expansion is SERVER-SIDE on the `/chat` submit path through the
  one `core.ExpandAtMentions` law (`core/atmention.go`). The browser
  never reads or expands files;
  unresolvable tokens stay literal in the message.

## The Files tab

- **The tree is chevronless** (t_63582d1f): expand/collapse triangles
  do not exist. The folder icon alone carries the state — the glyph
  flips `lucide-folder` / `lucide-folder-open` and the row carries
  `aria-expanded`. Keyboard parity: focus + Enter toggles. Hover
  indent guides: one hairline `::before` per nesting level, revealed
  purely by CSS `:hover` on the root tree (0.15s opacity fade) — zero
  listeners, zero layout shift; no narrow exemption.
- **The path guard is the traversal set** (t_ccd4a000): every file
  surface (browse, read, raw) rejects absolute paths, empty segments,
  and ONLY the `.` / `..` traversal segments — dotfiles and dot-folders
  (`.git`, `.worktrees`, ...) list and read like everything else.
  Listed rows carry no error state; a failed read is a one-line
  notice, not a phantom row.
- **The viewer renders ONE body per content class** (t_08d822bb,
  t_42c8aced): `GET /files/read` carries the sniffed `mime` (+ `size`)
  field; the viewer tab branches on it - `image/*` -> an `<img>` and
  `application/pdf` -> the browser's native viewer in an `<iframe>`,
  both served by `GET /files/raw` (raw bytes under the read surface's
  exact root jail); `.md`/`.markdown` files and skill bodies render
  through the chat's markdown grammar (same markdown-it + hljs fence
  path, same codeblock decoration); a non-UTF-8 file keeps the honest
  "binary file - no preview" state with the size and a download link
  (the raw route's `?download=1` attachment lane); everything else
  stays the line-numbered monospace text body. The raw route serves
  image/* + pdf only - other mimes answer 400 outside the download
  lane.

## The Agents tab

- The roster is a flat list (no grouping) over `GET /agents`: one row
  per agent home, the main agent included. The subtitle line carries
  the facts the tab is for - description, model, session count, last
  activity - and the main row additionally carries a `main` badge
  beside its name (the locked pill marks it too when the host serves
  the toggle seam; the badge survives a host that serves none).
- **A row click opens the agent's chat** (card t_81109e1c): the name
  is a button that hands the agent's MOST RECENT session id
  (`last_session_id` on the roster row, newest by `updated_at`) to the
  shell over the `omega:open-chat-session` window event - module UI
  imports no shell code, so the literal is copied in
  `harness/continuous/ui/AgentsPanel.vue` and pinned by
  `webui/test/agentschat.test.ts`. The shell runs the ORDINARY chat
  opener, so the click opens a full interactive ChatTab (never a
  read-only observer) in the main pane, and a session already open in
  any tab is FOCUSED rather than opened twice. An agent with no
  sessions has no clickable name - there is nothing to bind.
- **An agent-owned chat runs on its own home's model.** The opened tab
  is the same ChatTab, and its composer carries the app's model chip,
  but the chat floor ignores that pick for a session whose owner is an
  agent home (`agent/api/server.go`): the home's config layer names the
  provider and model, and neither the wire's pick nor one remembered
  from an earlier send sticks on the row. The stream's `agent_start`
  reports the home's `model_name`. The chip is a daemon-catalog
  control; it cannot address another home's models.
- The continuous roster folds into the same rows over
  `GET /continuous/roster`: the row law with a live dot + short word
  on the subtitle (green active, dim sleeping), the right-anchored
  toggle per the pill law, and NO wake control anywhere — waking is
  the scheduler's job, the tab never spawns a wake.
- **The default agent is locked** (t_2d8deb8c): it must always be on.
  The row's pill is locked-on and non-interactive; `PUT
  /disabled/agents` answers 409 for the default agent at the handler
  level and config.yaml stays untouched.
- **Expansion = details + edit**: expanding a row seats the config
  summary and the `PUT /agents/{name}` layer editor (description,
  provider host/type/model_name/api_key, system_prompt; nil = leave
  alone). Session ROWS are dead on this tab — the roster opens a
  session's chat, it never lists sessions (the per-agent session
  endpoints stay in the API, docs/api.md).

## The Settings pane

`SettingsTab.vue` is a MASTER-DETAIL surface (card t_af7e1150, settings
tier 4): a CATEGORY NAV on the left, ONE category's knobs on the right -
the VSCode settings bones. The tier-1/2 seat stays: the full-width search
bar is the pane's top row, the rows themselves are untouched (modified
dot, reset link, draft star, live note).

- **The nav is two levels, declared as DATA.** `settingsNav.ts` owns
  `KNOB_SUBGROUPS` (knob key -> child id) and `SUBGROUP_LABELS`; the pane
  stamps every knob with its `subgroup` (`withSubgroups`) and `buildNav`
  derives the tree. No hand-authored tree, no derivation from a config
  key's spelling, and a knob with no line lands in the implicit `general`
  child, pinned first in its domain. Adding a knob to a child is ONE line
  in the table - never a new branch anywhere else.
- **Every level renders at rest** (no click-to-expand): a parent with its
  children indented under it. A domain whose knobs all land in one child
  shows no child row (a nested copy of the parent's name is noise), and a
  category with no knobs never enters the nav (an unmounted module's
  domain is absent from the payload, so absent from the nav).
- **Commonly Used is a virtual FLAT top category** - the curated
  `COMMONLY_USED_KEYS` gathered from every domain, always first on Main.
  It renders flat because nesting curated knobs across domains would mean
  nothing. An agent scope carries no curated category (its four overridable
  domains only, tier 3b R3).
- **Search is cross-category, the nav greys instead of hiding.** The
  filter stays `filterSettingGroups` over the active scope's own groups
  (per-scope queries survive); a live query renders hits from EVERY
  category in the content pane and only dims the categories with no hit -
  the nav keeps its shape so you can see where the hits live. Search mode
  skips the curated section: a hit renders once, under the domain that
  owns it.
- **Modified-only** is a pressed-state switch on the content bar (the
  VSCode affordance): it filters the rendered list with the SAME
  `isModified` test the row dots use, so the switch and the dots can never
  disagree. Emptied child sections drop out.
- **The content pane's head** is the tier-3 scope strip (one axis per
  region: the strip is the scope axis, the nav is the category axis), then
  the category name with its knob count, then the switch.
- **Narrow panes drill down** (ruling 12): the nav and the content are two
  SCREENS of the same two components - a category tap opens the content
  full width with a `back to categories` control; the nav returns. No
  accordion path and no category dropdown exists at any width - one
  renderer, two doors.
- **The selection is remembered per client** (`omega.settings-category` in
  localStorage, never a server key), opening on Commonly Used. The
  remembered id is kept RAW and normalised against the live nav per
  render, so an agent scope that does not carry the remembered category
  falls back without clobbering the memory - switching back finds it again.
- **The accordion is deleted, not hidden**: no `isCollapsed`, no group
  heads, no `omega.settings-collapsed`, no `.settings-group-head` CSS.
  Pinned by `webui/test/settingsnav.test.ts` (the tree, the selection
  algebra, the content view, the grey-out set, plus the deletion proof) and
  the live pair `scripts/journeys/recipes/113-settings-nav.journey.sh`
  (landing, two levels, search greying, modified-only, drill-down + back,
  remembered selection, no-accordion proof) and
  `110-settings-scopes.journey.sh` (the tier-3 scope tabs, re-witnessed
  under the new bones).
## The Logs tab

- The face of the observability capsule (`harness/observability`, the
  engine half): two read-only routes, ONE tab. The tab is NAMED "Logs"
  (Endo ruling 2026-11-13, card t_01c53e78): the old raw-tail
  `http_channel` pane answered the same question with nothing but a
  `GET /logs` tail, and retired with its component - one logs surface
  survives. The STRUCTURED event
  stream is the primary view - `GET /observability/events?session=&level=&limit=`
  answers `{events, returned, buffered, capacity}` with rows of
  `{seq, time, session, type, level, tool, turn, duration_ms, summary,
  error}` (level is DERIVED server-side: error on a failure field or an
  IsError result, warn on an unclean stop, info otherwise). The raw log
  tail is the SECONDARY view inside the same tab (Endo's ruling 1: not
  a peer tab) - `GET /observability/logs?lines=` answers
  `{path, lines}` verbatim.
- **Band controls**: the session chip (scope defaults to the OPEN
  session, the chip flips the whole ring into view - a shell with no
  focused session says "all sessions" and disables the toggle), the
  errors-only chip, and the tail chip that swaps the pane's view. No
  free-text search in v1 (ruling 4, deferred until the stream proves
  too loud) and no OTel anything (ruling 5).
- **Filters differ by kind, on purpose**: the session scope is a SERVER
  parameter (the ring's `Page()` selects the newest N of that session;
  filtering the newest N of everything would starve a quiet session),
  while errors-only is a CLIENT filter over the fetched page - one page
  is `limit=1000` against a 1024-record ring, i.e. the whole live
  history, so a toggle is instant and costs no round trip.
- **Rows paint newest first** and the DOM is capped at 300 (the ring is
  1024 records; the meta line carries "N of M events" plus the error
  count and the ring's fill). Error rows carry a `--err` rail and level
  cell, warnings the accent rail; a row's failure text rides its own
  line, and the session column appears only in the all-sessions scope.
- **Refresh is bounded by mount**, not a permanent poll: fetch on open
  plus one `REFRESH_MS` (5s) interval cleared on unmount - the dock
  unmounts a sibling-activated outlet, so a background tab costs
  nothing. The push channel (`composables/socket.ts`) carries a CLOSED
  topic set of six names (chat, chips, canvas, runs, health, logs - the
  last subscription-gated), none of them an observability topic; riding
  it would be an engine-side change
  (`agent/api/sockets.go`), so v1 does not pretend to subscribe.
- **Degradation is a sentence, not a blank pane**: a 404 from the
  events route reads "observability module is not loaded" (the capsule
  is toggleable - disabling it drops both routes whole), a 404 from the
  tail reads "no log file" (logging off, or a logger with no path), and
  a transient failure never wipes the last good page.
- The view is the capsule's own (`harness/observability/ui/
  ObservabilityTab.vue` + the pure `rows.ts`); the shell's registry
  keeps the composition (`TAB_MODULES`, the bottom home) and `app.vue`
  binds the component from `module-tabs.generated.ts`, passing
  `focusedSessionId` as the default scope. Pinned by
  `webui/test/observability.test.ts`.

## Terminal and browser panes

Two module capsules complete the main surface (t_55a3fc4b terminal +
t_95c91f9c browser): the terminal module hosts a PTY in the
bottom pane (`TerminalTab`, journey 95), and the browser module hosts
an any-origin iframe tab in the main area (`BrowserTab` over
GET/PUT /browser/last, the resume URL; sites refusing embedding get
an explicit fallback panel, journey 96). Both are in-process capsules
of the module table (docs/architecture.md). Both views are the
capsules' own - they ship at `harness/<name>/ui/` and the shell's dock
outlets bind them from the generated module-tabs manifest
(`module-tabs.generated.ts`, card t_617c2ad0; the shell's registry in
`dockables.ts` keeps the composition: `TAB_MODULES`, the pinned-root
order, title/icon/policy). Removing a capsule drops its manifest
entry - the tab hosts nothing, same shape as a removed panel capsule.

## Component map

- `app.vue` — orchestrator: open/create routing (open in the main pane,
  focus-existing), URL sync (`/chat/<id>` vs `/`), scroll pinning,
  stacked-mode layout, live telemetry, tab-title spinner.
- `composables/chat.ts` — the stream driver (per-tab state incl. image
  and file attachments: `attachments` = the base64 image lane,
  `files` = the /attach-file reference records).
  `composables/omega.ts` — typed API client: SSE
  `parseFrame`/`handlePayload`, `streamChat` (throws
  `StreamInterruptedError` when the reader errors or ends without
  `agent_end`), sessions CRUD + pinned/read PATCH + archive/unarchive,
  search, transcript fetch, files browse/read, image attach
  (`attachImageUrl`), non-image attach (`uploadAttachment` — the
  multipart POST /attach-file, part order `session_id` then `file`),
  `renderMarkdown` (markdown-it, `html: false` — the lib's default
  escaping is the sanitizer), `createRenderScheduler`.
- Shell: `MenuBar` (brand, nav cluster after the brand, the plain
  'Omega' wordmark dead center, edge toggles far right), `StatusBar` (health dot,
  token/ctx/rate telemetry, build version; uptime lives in the health
  popover the dot opens), `SearchInput`,
  `PaneDivider`, `TabBand` (the universal panel top bar: title left,
  actions right, outside every scrollport), `TogglePill` (the one
  switch, see above), `BandSelect` (the themed select — chip button +
  role="listbox" pop, generalized over short option ladders: band
  toggles, the board switcher, the drawer status, settings knobs, the
  composer's reasoning-effort chip).
- Chat: `AppComposer` (auto-grow editable editor - `contenteditable`
  `.composer-input`, the t_e67c372b chip law: a reference chip
  REPLACES the token span it represents, inline in the flow, and
  Backspace behind a chip dissolves it back to the raw token text while
  the chip's x removes outright; Shift+Enter writes the newline as a
  text node (the editor claims every Enter - plain sends), and Chromium
  hangs a block-final newline in pre-wrap, so every write settles one
  draft-invisible sentinel `<br>` after it (AppComposer's capTail, card
  t_eb07b315) that turns the break into a real line box and seats the
  caret on the new line - pinned by the 102-composer-shift-enter
  journey and `webui/test/composershiftnl.test.ts`; ONE morphing action button -
  send/stop, dimmed at idle with no draft, `+` opens the file picker,
  attach-strip (image thumbnails + non-image reference chips: type
  glyph + name + size, never thumbnailed - the reference lane,
  composables/attachments.ts owns the glyph/size helpers), paste +
  paperclip attach; the
  retry face is retired - the newest response's action row carries the
  msg-retry chip, card t_95d2bc34; `/` command palette + `@` mention
  picker, see below; the composer shape law lives in
  [`.agents/DESIGN.md`](../.agents/DESIGN.md) (card t_1750a9e3),
  pinned by the 22-composer journey and
  `webui/test/composershape.test.ts`; `ModelSelect` + the shared `BandSelect` effort chip (the dedicated
  ThinkingSelect retired — the BandSelect owns the select grammar; the
  model+effort pair is one nowrap wrap unit), `ChatMessage`,
  `ThinkingBlock`, `ToolCallLine` (running/done/error, expandable
  result), `ToolGroup` (the tool-group RENDERING law, card t_e4280de7:
  a run's CONSECUTIVE tool rows collapse into ONE collapsible group -
  `composables/toolGroup.ts` folds each render list strictly, a
  variant-anchored row never gets swallowed, the group rides the shared
  `chat.tool_display` knob with auto open-while-live and a session-scoped
  pin; pinned by the 23-tool-group journey and `webui/test/toolgroup.test.ts`),
  `ChatTab` (transcript + composer, live-pin layout),
  `ChatItem`.
- Sessions: `AppSidebar` (band head with the in-band search, the
  full-width new-session row, pinned section, day buckets, per-row
  pin/status affordance, archive/trash hover actions,
  branch/rename/export row actions, archived group, order law above).
- Files: `FileTree` + `FilesTab` + `InspectorPanel` (module panels slot
  in). See the Files tab section below. Settings: `SettingsTab` (the
  config knobs as a dock-tab singleton; the theme picker is TWO knobs —
  color mode and the custom theme, t_a94b1cda; model slots ride
  `ModelField`, the BandSelect listbox with capability filters).
  Terminal view: `harness/terminal/ui/TerminalTab` (the bottom-host PTY
  over the terminal module's routes; a fresh spawn lands in the
  resolved workspace - the focused chat session's pinned root rides
  the claim as `?session=<id>`, the server resolves it against the
  `server.default_workspace` knob and the process cwd, t_62d72028). Browser view:
  `harness/browser/ui/BrowserTab` (URL bar + iframe over
  GET/PUT /browser/last; X-Frame-Options refusals render the fallback
  panel, never a silent blank frame). Both are tab capsules - their
  views live in the capsule, the shell's outlets bind them through
  `module-tabs.generated.ts` (card t_617c2ad0).
  Observability view: `harness/observability/ui/ObservabilityTab` (the
  event stream + the tail toggle over the observability capsule's two
  reads; the pure filter/format half is `ui/rows.ts`) - the tab the dock
  names "Logs" (card t_01c53e78).
- Canvas (arc 3c): `composables/canvas.ts` is the store —
  snapshot load (GET /ui/canvas) + view_model_delta merge (forwarded
  by the chat host's `canvasDelta` dep) + the action pipe POST
  (optimistic pending, error state on 4xx/5xx); the standalone
  generic host tab (`components/canvas/CanvasPanel.vue`) is gone —
  folded into the Modules tab, the chat transcript renders FROM the
  canvas view-model.

## Testing

```bash
node --test "test/*.test.ts"        # from webui — node's built-in runner
bash scripts/build.sh               # SPA generate + sync to api/static
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

- The built SPA is COMMITTED: `api/static/` is
  tracked, so `go install` and plain `go build` always produce the
  complete product. Webui cards commit **SOURCE ONLY**; the merge pass
  runs `scripts/embed-lane.sh`, which owns every `static/` commit.
- `scripts/build.sh` runs `nuxt generate` (NOT `nuxt build` — an
  ssr:false `nuxt build` emits a nitro server; generate emits the
  static SPA) and syncs to `../api/static/`. Run it before
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
- [configuration.md](configuration.md) — the knobs the settings pane renders