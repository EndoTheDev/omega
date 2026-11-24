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
- Type: Public Sans (`--sans`) + JetBrains Mono (`--mono`) - tokens in
  main.css `:root`. Body/UI base is 15px and every size is px: there is
  no rem scale or hero heading style (UI text sits mostly at 12-13px,
  mono at 10.5-12px; sizes are per-component, not tokens).
- The light ladder (the `html[data-theme='light']` block in main.css):
  `#ececec` z0 / `#f7f7f8` z1 / `#ededf0` z1-raised /
  `#ffffff` z2, stroke `#d5d5da`, text `#1a1a1d`, text-dim `#6a6a72`,
  plus the light shadow overrides. Theme selection rides `auto |
light | dark` (`ThemeMode`, composables/omega.ts) plus `theme:<name>`
  custom themes from the picker.
- Theme overrides: YAML files in `<home>/themes/` (one per theme,
  token overrides, `base:` + `name:`). Served by the HTTP channel
  (`GET /themes`, hot-reloaded from the folder poll), applied live from
  the theme picker - tokens map 1:1 to module components' styling, so
  overrides recolor modules for free.

## Geometry contracts

Constants in `main.css`. They are contracts, not suggestions - tuned
over many measured rounds, and changing one requires re-checking all
the others:

| Constant          | Value |
| ----------------- | ----- |
| MenuBar height    | 35px  |
| StatusBar height  | 29px  |
| Tool pane floor   | 128px |
| Chat pane floor   | 320px |
| Gutter (divider)  | 8px   |
| Mobile breakpoint | 768px |

**Top bar seats**: three regions
on the 35px bar. The BRAND is ONE element far left - `Ωmega`, the glyph
and the word in a single span whose text is its accessible name (the old
lone-glyph brand and the centered `Omega` wordmark are both dead). The
NAV CLUSTER (back / forward / the fake-input palette affordance / the
palette trigger) is ABSOLUTELY centered: `left: 50%` +
`translateX(-50%)` on the menubar's FULL width - the dead-center trick
the wordmark used, so the cluster stays centered whatever the left/right
widths are; never spacer-balanced. The EDGE TOGGLES (left / bottom /
right) and the settings gear stay pinned far right. Back/forward dim via
the REAL `disabled` attribute, never a lookalike. Below the 768px
breakpoint the CLUSTER HIDES and the toggles STAY - they are the drawer
handles. The palette SEAT (`.nav-palette-input`, 420x24, `26vw` cap)
holds the live `HH:MM` clock while the palette is closed - a button
dressed as an input, one click = the ctrl+k intent, the `nav-palette-clock`
span decorative (`aria-hidden`) so the affordance's name stays the
command-palette hint. **Opening the palette takes the seat over in place**: the palette's own
field teleports into
`#nav-palette-slot` (a `display: contents` anchor inside the cluster, so
the field is a flex item in exactly the box the clock filled), the clock
button stands down, and the bar's other chrome - the back/forward arrows
and the Ctrl+K toggle - hides by `visibility` (never `display`), which
keeps their layout so the seat cannot slide sideways mid-takeover. Esc
closes the palette and the clock returns, still ticking (the timer belongs
to the cluster, not to the button). Both palette doors ride app.vue's ONE
`paletteOpen` state, so both land on the identical state, and a
pointerdown anywhere outside the palette closes it (the scrimless
overlay listens on the document; the `data-palette-trigger` markers and
the teleported field itself are the exemptions, so a click that opens
cannot close on the way out).

**The command palette's geometry** (VS Code
quick-open): the panel HANGS from the top bar. Its top edge is
`var(--menubar-h)` - the same number the bar's own height reads - so the
seat you took over and the results below it read as one control, and it
is centred on that absolute-center section (420px wide, capped at
`100vw - 24px`). `position: fixed`, z 200: it overlays the panes and
never pushes them, with no scrim (the panes stay readable behind it).
That anchor is kept exactly as merged (both open paths land on x/y
505/35, 420 wide) but the panel is now the results
dropdown ALONE - the field lives in the bar's seat, so the list drops
from the bar's lower edge. Both open paths - the seat click and Ctrl+K -
ride ONE toggle, so they land on ONE geometry.

**Honest floors**: the floor is a
PER-COMPONENT registry fact, not a sidebar rule. `DOCKABLE_MIN_PX`
(composables/dockables.ts) attaches every dockable's `minPx` - chat is
the only 320px component (`CHAT_FLOOR_PX`), every tool component floors
at the shared 128px pane minimum (`MIN_PANE_PX`). One floor law backs
BOTH sides of every divider: the reveal's "no room" check and the
divider drag clamps read the same number through `paneFloorOf` /
`minDockPx` (composables/dock.ts) - a chat-holding pane keeps 320 on
both sides of a split; a tool pane keeps 128 from both sides. A reveal
or split that cannot seat every pane's floor does not squeeze into a
lie: it is refused and the shell surfaces the one-line toast
"no room - close a pane first" (app.vue) instead of a sub-floor pane.

The gutter divider (`PaneDivider`) retains its magnetic snap rebase
mechanics. On a drag between two FLEXIBLE panes the shares re-derive
from the frozen pre-drag baseline (c50b749) - the squeeze chain is
dead: a divider never pushes a third pane around (the wall-law). A divider with a FIXED pane on one side writes that pane's
px and the ADJACENT neighbor absorbs the change 1:1 - every other
flexible sibling holds its measured px (before it, the
flex reflow spread the delta over the whole flex space, so a small
pane beside a big one tracked the pointer at a fraction of the drag).
Fixed panes drag down to the shared 128 floor. Below the
mobile breakpoint the sidebars collapse into slide-in drawers over a
scrim; chrome (menu/status bar) rides above all overlays: drawers z12,
scrim z10.

**Same-pane split drag** (the VS Code read): tab
drag-and-drop runs ONE pure formula behind both the commit and the
ghost preview. Dropping a tab on an EDGE zone splits the target pane
and lands the dragged tab in the new half-pane; a drop on the CENTER
zone focuses/reorders within the target's bar. A pane's OWN edge
half-zones split that pane - center body drops stay focus-only.
Refusals render the hatched-red ghost with the reason (depth caps, a
grown re-growth, the locked sessions tab - it never leaves the left
shell).

**The sidebar shape law (canvas C2)**: a sidebar is TABS + a STACK.
It stacks panes (a column split, drop on the body's top/bottom half)
and NEVER puts two columns of content side by side - the shell's two
columns ARE the two sidebars, so the split-sidebar gesture is DELETED,
not refused: a sidebar body has no left/right half at all
(`zoneForPointer`, composables/dockdnd.ts, reads a sidebar's vertical
lane only), and its panes' actions menu carries no "split right"
(`splitFitsZone`, composables/panes.ts). One predicate answers the
pointer, the menu and the store's split law, so no entry point can
offer what the shape forbids; the hatched-red "a sidebar stacks - it
never splits into columns" line survives only as the pure law's guard.
The strip identity follows the ZONE, not the marker: a pane inside a
sidebar's stack is a sidebar pane (28px icon chips, no `+`, no split
actions), so the icons-bleeding-at-the-sidebar-bottom class - a text
strip crushed into a narrow column - cannot come back through a stack
leaf. The column split that grew the bleed is deleted; no overflow fix
is owed to the deleted element.

The universal panel band (`components/ui/TabBand.vue`) is a geometry
contract too: every tab panel (module tabs: files, cron, logs, skills,
tools, agents, notifications, insights, settings, kanban, mcp,
extensions; plus the sessions sidebar) renders ONE band (tab title
left, tab actions right, 22px min-height, 14px lead-in clearing the
-8px pane tuck, `lead=false` where the host supplies its own padding).
The band renders OUTSIDE the panel's scrollport, structurally pinned:
scrolling the body never moves the band. Band action buttons use the
shared `.band-tool` look (dimmed at rest, hover raises, disabled 0.4).
The BAND POSITION CONTRACT: the tools/settings/insights
tab roots stop sitting the band inside - the band is the host frame's
top row, never a child of the scroll body (the 22/14 numbers above are
the shared contract; a module panel that renders its own inner band is
a review finding).

**Frame rails**: a rail renders ONLY across a GUTTER between two
horizontally-adjacent cards - the row divider's own top/bottom strokes,
spanning exactly the divider's 4px width, aligned to the cards' border
rows. Nothing else strokes. The old whole-width pane-area strokes are
dead: a stroke never appears where a surface's own border is not -
one card means zero strokes, the card's border is the whole edge. The
menubar keeps its border-bottom and the statusbar its border-top; the
pane-area contributes no connecting stroke. Desktop law only.

## Registry rows and the toggle pill

The UI-family laws live in [`.agents/DESIGN.md`](../.agents/DESIGN.md).
The anatomy map:

**The registry row**: LINE 1 the name (semibold,
full opacity, ~13px), LINE 2 the subtitle (dim, smaller: the mono
schedule, the description, transport + status, module counts), and
exactly ONE control on the right, top-aligned with line 1. Expanded
content renders below the row-head in its own indent, seated on the
same paddings. Shared definition in
`assets/css/registry-row.css`, five consumers (cron, tools, skills,
mcp, modules). Status dots speak dot + word: green active, dim
sleeping (the agents roster's law); an enabled/disabled text label is
dead - the control column rides the pill.

**The toggle pill** (`components/ui/TogglePill.vue`):
the 26px rounded pill with the 11px state DOT inside (dot shifts,
border goes `--ok` when on, transparent background). No `:checked`
prop exists for Vue's prop diff to skip, so the stale-arm re-render
trap is structurally dead for every host that adopts it. ONE CLICK IS
ONE SWITCH: the pill emits a single intent per
activation and carries no armed state and no confirm label, so the
arm/confirm two-step that used to make every toggle need a double
click cannot come back through the shared component. The host owns the
write path and re-renders the row from the server's answer; the
consequence text (what an exclusion means, a module's dependents)
rides the pill's `hint` in BOTH the accessible name and the title
(one composed string, so a screen reader hears the
consequence a hover tooltip would hide) - always informative, never a
gate. Adopted across the registry tabs (skills, tools, agents, MCP,
modules) and the agents roster; the MCP tab's "from mcp.yaml" caption
is gone (rows speak transport + live state from GET /mcp/servers). The
default agent's pill is locked-on and non-interactive - the default
agent cannot be disabled (api.md's 409).

## The context menu

One menu for the whole window. `composables/contextMenu.ts` owns the
store and the laws; `components/ui/ContextMenu.vue` is the only
surface, mounted once by `app.vue`. A surface never keeps a menu of its
own: it registers a flat row list (`openContextMenu(x, y, rows)`) and
the store does the rest.

- **Chrome only.** Omega menus fire on omega chrome (tabs, panes,
  sidebars, cards, the statusbar, the canvas). Browser-rendered content
  (chat text, file and preview text, links, images) keeps the browser's
  native menu, so `contextmenu` is only prevented where a surface
  registers rows.
- **Rows are verbs that can act.** Separators are spacing, not verbs,
  and a list with no fireable row opens nothing at all - the right-click
  does nothing rather than showing a menu that lies. Leading, trailing
  and doubled separators are trimmed at registration.
- **Placement.** The menu opens at the cursor, flips up/left when the
  measured box would run past the viewport edge, then clamps to a 4px
  margin, so it is always fully on screen.
- **Keys.** Escape closes, ArrowUp/ArrowDown move the selection over the
  fireable rows (wrapping, skipping separators), Enter fires. A press
  outside closes; the surface is invisible for the frame before its own
  box is measured so it never paints at the wrong spot.
- **Selects what it menus.** A right-click on a row selects that row
  first, so the verbs act on the thing the user pointed at.
- **Dock tabs are the first surface** (`dockTabMenuItems` in
  `composables/dockables.ts`): Close and Close others. Close follows the
  tab's own X - absent on an uncloseable component and on the locked
  last tab; Close others appears only when another closable tab exists.
- **Sidebar rows are the second** (`sessionMenuItems`,
  `agentMenuItems`, `cronMenuItems`, one pure builder per capsule's
  `ui/` dir): a session row menus New session, Rename (the row's
  inline editor), Branch, Export, Copy id, Archive or Unarchive,
  Delete and, on a running row only, Stop; an agent row menus New
  agent, Sleep (only while the roster says awake - it ends a running
  wake) and Delete (never the locked default); a cron row menus Run
  now (`POST /cron/run`, one fire the schedule did not order) or Stop
  run (`POST /cron/stop`, only while the row carries the live mark),
  Pause or Resume (the `enabled` flag) and Delete (never an agent-owned
  job, whose file main's delete route cannot touch). Every row is an
  existing verb - a route or a client intent the surface already
  carried - so the menu is a second door to the same paths.
  `RegistryList` takes the builder as `row-menu`; a row surface that
  registers nothing keeps the browser's own menu. The cron console
  row also wears its last fire's status dot (green ok / red error /
  orange never fired), pulsing while a fire is live.
- **The statusbar is the third** (`components/shell/statusStats.ts`):
  every stat with a check state, one click hiding or showing it, then
  Reset statusbar - the last row, and the one write that clears the
  whole hidden set (an id-level toggle per hidden entry, so every stat
  shows again; dimmed when nothing is hidden). `checked` IS visibility.
- **Hiding is a flag, never a close.** A hidden sidebar chip keeps its
  seat, its ref and its pane; only the chip stops rendering (the pane body
  follows the active visible chip, so a hide never blanks a pane). The
  set lives in the layout record beside the tree, so it survives a reload,
  and the strip - the surface that stays visible - carries the restore
  list. Opening a view from the palette or the menubar clears its flag.
- **Board cards are the fourth** (`cardMenuItems` in
  `harness/kanban/ui/board/cardMenu.ts`): Open (the card's own click -
  the detail drawer), Copy id (client-side through the one clipboard
  path; the notice follows the copy's own answer, so a refused copy
  never reads as "copied"), Stop on a running card (the lane arrow's
  own PATCH to blocked, plus the card's live session run stopped
  through `POST /sessions/{id}/stop`), Archive, which is the lane
  arrow's own PATCH to the archived lane, and Delete, the drawer's
  DELETE /tasks/{id}. A card already archived carries no Archive row
  and an idle card carries no Stop: the verb cannot act, so the row is
  absent. Delete is irreversible, so it arms on the first pick (the row
  reads Confirm delete on the next open) and only fires on the second -
  the drawer's own armed confirm stays its door's, one confirm per
  pick.
- **The board surface is the fifth** (`boardMenuItems` in
  `harness/kanban/ui/board/boardMenu.ts`): Rename and Delete, the board
  dropdown's own two picks (PATCH and DELETE /boards/{name}). Delete
  shares the dropdown's ONE arm state, so the same destructive verb
  never grows a second confirm. A right-click on a card, in the drawer
  or in a text field keeps that surface's own menu, and no board
  selected means no rows at all.
- **The capsule interiors are the sixth** (`skillMenuItems`,
  `toolMenuItems`, `mcpMenuItems`, `fileMenuItems`,
  `notificationMenuItems`, one pure builder per capsule's `ui/` dir): a
  skills row menus Open (its viewer tab), Export (`GET /skills/export`,
  the folder as a tar.gz download), Enable or Disable, Archive
  (`POST /skills/archive`: the folder moves into `.archive`, backed up
  and ledgered, so the exit is reversible) and Delete (the editor's one
  armed confirm); a tools row menus Enable or Disable
  and nothing else, because that is the only verb the tab owns; an MCP
  server row menus Open (its detail), Edit (the detail's entry form, saved
  through `POST /mcp/save` - the route the CLI rides), Enable or Disable
  and Delete (the detail's one confirm); a file tree row menus New (the
  tab's own new-node form,
  file mode, with the row as target), Rename (the same form in rename
  mode, `POST /files/rename`) and, on a file, Open, Copy and Copy
  path through the one clipboard path, plus Add file to chat through the
  composer's own attach lane (absent without a bound chat tab), while a
  directory row keeps New, Rename and Copy path; Delete
  (`POST /files/delete`) arms on the first pick and removes on the
  second, the board card's one shape; a notification record menus Clear
  (this record, `DELETE /notifications/{id}`) and Clear all (the whole
  feed).
- **The canvas has no rows to register.** In 0.17.4 the canvas
  view-model has no rendered node surface - the generic panel renderer
  left with the canvas tab fold (`canvas.test.ts` pins its absence), so
  each panel's rows ride its capsule's own UI and the chat transcript.
  The panel action vocabulary (chat stop/retry, cron refresh, kanban
  complete_task/switch_board, notifications mark_read/mark_all) holds no
  node rename and no node delete, so a canvas node menu would have
  nothing to fire. Adding those verbs is backend work, not menu wiring.
- **The long tail is chrome too** (`paneMenuItems` in
  `composables/panes.ts`, `components/settings/settingsMenu.ts`,
  `harness/terminal/ui/terminalMenu.ts`). A main pane's own area - its
  strip's free band, an empty pane's body - menus Close pane where the
  pane's actions popup offers remove, then the popup's split verbs below
  a rule; a pinned pane (a sidebar, the bottom shell) registers nothing,
  and content keeps the browser's menu. The settings tab's one standard
  row is Close, offered only where the dock would let its X close, and a
  right-click inside an editable field (or the config editor dialog)
  stays native so paste keeps working there. The terminal is chrome by
  the design line, so its right-click is Copy / Paste / Clear - Copy
  disabled on an empty selection, Paste handing the clipboard to xterm's
  own paste through the one clipboard read path.

## Motion

main.css's header states the law: "Motion: streaming cursor + message
fade only." Three keyframes exist and nothing else moves via
`animation` - `blink` (1s steps(2): streaming cursor + thinking-live
ink), `fade-in` (0.18s message arrival), and `status-pulse` (1.6s:
session-row streaming status glyph - dot or star, not just color).
Everything else animates through `transition`, not keyframes.

## The dock model

- **The shell is a pane tree, not fixed slots.** `composables/dock.ts`
  owns the persisted tree (splits + panes, under the desktop
  persistence key); `DockSplit`/`DockPane`/`DockDivider` render it. The
  same record carries the closed shells' stows and the visibility sets
  (hidden sidebar chips, hidden statusbar stats); a record with none of
  them keeps its older shape. A
  bare pane can be the tree root (all sidebars closed) - the renderer
  has an explicit single-pane root branch. A tree with zero panes is a
  bug, not a state.
- **Pane model**: left sidebar = sessions pane (pinned singleton, no
  X); right sidebar = inspector pane with the fixed module tabs (Files,
  Cron, Skills, Tools, Agents, Settings, Kanban, Modules, Insights,
  MCP, Notifications - no X); the bottom panel under the main pane
  hosts the Logs tab (the structured event stream over a raw-tail
  chip; named "Logs" since the old raw-tail pane retired) and the
  Terminal;
  middle = main pane. The LAST main pane never closes its
  PANE: `removePane`
  (and the move-out
  auto-close) refuse it as a no-op - the main area never reaches zero
  panes. The pane X shows on hover and only while another MAIN pane
  exists (pinned shells don't count toward that census). **The last tab of
  a pane never closes** (see THE LOCKED LAST TAB below): that is how the
  pane stays non-empty, so nothing is seated in its place.
- **THE SIDEBAR SHAPE** (canvas C2): a sidebar is one column of
  content - a tab bar plus a vertical STACK of panes. It never holds
  two columns side by side; the shell's two columns ARE the two
  sidebars. `resetSidebarShape` (dock.ts) is the ONE-TIME migration
  behind the record bump (v3 -> v4): a sidebar that carried a local
  split tree - the deleted model's work - resets to the clean default
  on the first load of an older record, while center and bottom
  layouts load untouched. The boot counts a repair receipt for it
  (`bootRepairs`, the raw record backs up first), and the boot's own
  re-persist writes v4 so the reset never runs twice.
- **THE LOCKED LAST TAB** (the live eyeball on commit
  8eabefc7's never-zero placeholder): a pane's LAST
  tab is locked. `closeTab` refuses the request and `DockPane`'s
  `tabClosable` renders no X on the lone tab, so no close can empty a
  pane. The `newtab` placeholder that used to seat in the
  emptied pane is RETIRED: a blank "New Tab" body was the "empty bad
  looking space" a closed chat left behind. Nothing is minted to fill
  an empty pane - a pane a GESTURE leaves empty (a fresh split group, a
  shell whose last tab was dragged out) renders the bar-less empty strip
  and a drop refills it. Two dead states are REPAIRED rather than rendered:
  a persisted `newtab` seat (`LEGACY_DOCK_COMPONENTS`) and a zero-tab pane
  from an older record both boot onto a fresh new-chat tab
  (`repairPaneSeats`, a counted boot repair - the raw record backs up
  first), and a pane whose last chat died with its deleted session seats
  the same fresh chat. The empty-pane component menu stays deleted
  (DOCK-EMPTY-ACTIONS).
- **The empty-pane component menu is DELETED** (DOCK-EMPTY-
  ACTIONS removal): the 15-button inventory that WAS the empty pane's
  content is gone, and the icons-bleeding-at-the-sidebar-bottom bug died
  with the element (no overflow fix was owed to a deleted element). The
  `dock-empty` body is a blank surface; do not build a replacement menu
  there - the 0.17.4 canvas class-law + menu work is the future home for
  adding views. Until then the refill path is a tab drag (or the
  menubar/palette openers).
- **Sessions lock to the primary sidebar** - open a session or hit `new`
  and it opens in the MAIN pane, focusing the existing tab instead of
  reopening.
- **The new-conversation DIM**: while the
  FOCUSED chat tab IS the draft conversation, every "new conversation" door
  would only focus the tab already on screen, so each renders dimmed and
  disabled - `aria-disabled` + `tabindex -1` and the one global
  `.door-dimmed` value (0.4, the house disabled number), dimmed but never
  removed, the same law the last-tab close X obeys. The doors: the sessions
  sidebar's `+ New Session` chip and its picker's `New Session` entry, the
  palette's `New chat` command, and a main pane's `+`. The fact has ONE
  owner (`app.vue`'s `draftConversationFocused`, the pure
  `isDraftChatKey`), provided once through `DOCK_DRAFT_FOCUS_KEY` and handed
  to the store sidebar as a prop (module UI imports no shell code) - no two
  doors can disagree, and a click still runs its honest action (focus the
  draft). The dim lifts the moment a send binds the tab to a session.
- **Dockables** (`composables/dockables.ts`): the registry of openable
  domains - the pinned shells (sessions sidebar left, the inspector
  tabs right, the bottom host under the main pane) plus the main-pane
  tabs: chat (multi, closable), the viewer (multi, closable,
  row-click only), the browser (singleton main tab, closable - the
  menu opener focuses the one instance), the terminal (bottom host,
  unclosable), the Logs tab (bottom host, unclosable - the
  structured run-event stream over the raw log tail; see its own
  section), the notifications feed (right pane, unclosable,
  inbox glyph - the bell died). The `newtab` placeholder left the registry
  under a legacy id (persisted seats are retired on boot). `inspector` is
  legacy
  only: deserialization still accepts the persisted id and modernizes
  it, nothing opens one. `continuous`, the `canvas` host tab and the
  modules cockpit tab left the registry - the continuous roster folded
  into the Agents tab, the canvas renderer into the settings MODULES
  section (`composables/canvas.ts` stays as the shared view-model store
  - snapshot GET /ui/canvas, delta merge, the action pipe, the toast
    funnel), and the module lifecycle surface is CONFIGURATION, so it lives
    in the settings tab's MODULES section (rows with live
    toggles, the expandable tools/commands/panels detail, the always-on tier
    (agent-loop, provider, observability) as a chip - the toggle endpoint
    refuses it at the seam; `/modules`,
    `/extensions` and `/panels` open that section). Settings is an ON-DEMAND
    main tab: the menubar gear beside the right-sidebar
    toggle opens it, it closes like any tab, and the boot retires a
    persisted inspector seat (`retireInspectorSettings`). The `+` menu
    still carries no settings entry - the gear is the entry point.
- **The runtime module gate**: a dockable whose pane body
  ships from a capsule's `ui/` dir is MODULE-OWNED, and the set is DERIVED
  from the composition manifests (`modulePanels` + `moduleTabs`, the
  generated capsule scans) - never a second hardcoded list. When
  `GET /modules` reports that module disabled, the chip leaves the strip,
  the `+`/palette inventory and the mobile drawer in the same tick the
  settings row flips: the dock PARKS the tab (`parkGatedTabs`) - out of the
  layout record, its seat saved under `omega.dock-module-stash` - and
  `setModuleGate` re-seats it where it sat when the module mounts again.
  One module-state store (`composables/moduleState.ts`) serves both the
  settings MODULES section and the gate, so the two can never disagree: one
  boot read plus the toggle responses, no polling timer. The `off` set is
  cached (`omega.modules-off`) and hydrated synchronously before the first
  render, so a reload with a module disabled never paints its chip. Every
  path that REINTRODUCES a strip re-runs the gate afterwards - the undo
  restore and both reveal paths (`toggleEdge`, `toggleBottom`): a seat
  parked while its strip sat closed has no live pane to reseat into, so
  the reveal is the tick that brings the chip back. Shell
  panes (sessions, chat, viewer, agents, insights, settings) are not module
  ids and are never gated; `shell` (the capsule behind the Terminal tab) has
  no manifest id match yet, so its chip is not gated either.
- **Closing an edge pane HIDES state, never deletes it**:
  a sidebar/bottom pane's collapse removes the host from the tree but
  keeps the domain's state (search text, selected tab, scroll) so the
  reveal puts it back exactly as it was.
- **The give-away law**: pure window-narrow arithmetic -
  when the window cannot seat every fixed edge pane at its floor, the
  give-away squeezes the other edge roots to make room, but the
  component the user JUST revealed (the last request) is exempt: it
  survives, the other edge gives way. Reveal and resize read the same
  formula, so "no room" never fires while a give-away could seat the
  pane.
- **Sessions panel order**:
  the head row IS the universal band - tab title left, the search seats
  IN the band top right (band-tool slot, on the band's own 22px row;
  the separate search row below the band is dead), and the new-session
  control keeps its own full-width row under the band.
- **Tab drag-and-drop** moves tabs between panes (edge zones split,
  see the geometry section); sessions stay in the sidebar.
- **Boot repair**: a persisted dock shell from before a
  built host's bottom-home addition is repaired at boot - new
  bottom-home registry members are injected into the persisted bottom
  shell with the persisted layout untouched (the repair count is
  logged), so a fresh tab host survives old shells.
- **Module-owned UI (build-time composition)**: modules ship UI in
  `harness/<name>/ui/`; `nuxt.config.ts` composes them by array -
  `app/composables/module-panels.generated.ts` is generated at build
  time and there is NO runtime registry. `harness/tools/ui/ToolsList.vue`
  is the canary.
- **Mobile drawers resolve views, not panels**: below the 768px breakpoint
every edge shell becomes an
  overlay drawer, and each drawer's tab chain ends in the generic
  `InspectorPanel` catch-all - which reads the PANEL manifest. A TAB
  capsule (terminal, notifications, observability) and a shell-owned view
  (files, settings, extensions, insights, agents) therefore each need
  their own branch AHEAD of that catch-all, or a loaded tab renders
  "module not loaded". The bug is a width breakpoint, not a device: a
  desktop window resized under 768px shows it.
- **The drawer CLAMPS its tab set**: a tab is free content in
  the dock, so any unlocked tab can be dragged into any pane - which put a
  tab the drawer had no branch for into the live edge pane (agents in the
  bottom pane, terminal in the right one) and rendered "module not loaded"
  through the catch-all. Branching every permutation is not available
  (chat and the viewer render from the tab payload a drawer chain does not
  carry), so the derivation keeps only the ids the drawer actually
  resolves: `DRAWER_RENDERABLE` in `composables/mobileDrawer.ts`, plus the
  module panels the catch-all honestly resolves. A tab dragged into a
  non-default host therefore stops at the drawer; the tab stays reachable
  because each drawer's list is the live pane UNION its registry floor
  (the home strip), clamped the same way - so a drifted or emptied pane
  never strands a home tab on mobile. `DRAWER_UNBRANCHED` names the ids no
  drawer seats (chat, viewer, browser). The desktop tree is untouched:
  above the breakpoint the `#pane-<id>` outlets render any tab in any pane.
  `webui/test/drawerresolves.test.ts` derives the drawer-reachable ids from
  the registry (`hostsOf` / `defaultHome` / `defaultHostTabs`), pins each
  drawer's branches against `DRAWER_RENDERABLE` in both directions, and
  fails the next dockable that is neither drawer-rendered, panel-resolved
  nor declared unbranched.

## Streaming + rendering rules

- Streaming renders **chronologically**. A tool phase ends the current
  response segment; text after tools is a fresh bubble (mirrors
  `accumulateAssistant` in the backend - same boundary, both sides).
  Thinking blocks accumulate per reasoning run; tool results attach to
  their calls by `tool_call_id`, not position.
- Stack mode: every block renders full width on the background. The
  assistant answer keeps the full column width but insets its text 8px
  from the turn's left edge, and the branch-anchor chips sit at left 4px
  / right 8px (the tuned numbers out of the chat-spacing tuner,
  2026-11-15, round 2 - the numbers are law). Within a turn the strips
  sit flush on a 0px gap in 24px rows (the panels that reuse those rows
  keep their 44px click floor); the 32px turn boundary is the dominant
  gap. Thinking strips de-card on dark, and while reasoning STREAMS the
  row is activity - no border, no raised fill, just the glyph, label and
  ticking elapsed; the card face (border + raised background) belongs to
  the collapsed artifact the phase leaves behind. Order never changes -
  live-pin is scroll + sticky layout, not render order, and its
  full-bleed inset IS the transcript's own side padding, per breakpoint
  (12px, 8px under the mobile inset - a hardcoded column number turns the
  scrollport horizontal mid-stream).
- Statusbar telemetry is honest: the token counter ticks per chunk
  (chars/4 estimate, corrected by real usage at stream end); live
  chunks/s comes from real arrival times.
- Scroll pinning follows only the stream whose session is being
  viewed; the `pinned` flag changes only on real scroll events - never
  re-measure after appending.
- Retry is regenerate-in-place via the server: the action row's retry
  chip (composer face retired) dispatches the chat panel's
  retry action through the pipe - it re-runs the session's LAST user
  message as a sibling variant without persisting a duplicate user
  bubble; a cancelled/partial response stays visible and the new
  response appends below. The chip renders only on the NEWEST assistant
  response while the run is idle. Client-side `dropLastExchange`
  transcript surgery is RETIRED - never reintroduce it.
- Transcript rows are ADDRESSED, never positioned: one rule
  (`transcriptAddress` in `composables/omega.ts`)
  names a row by its store row id plus its rendered shape, or by an
  explicit slot while the store has not named it yet. Both render paths
  resolve it - the live canvas paint and the REST restore - so a refetch
  (a remount, a variant select, the socket's catch-up) reuses the object
  of every row it already rendered and patches it in place. Keying a row
  by its payload index was the whole remount defect family: the chat
  panel is a sliding window, so one append moved every surviving index.
  Pinned by `webui/test/identity.test.ts` (canvas) and
  `webui/test/transcript.test.ts` (restore).
- The answer row's elapsed badge is the TURN TOTAL:
  prompt arrival to answer completion, subtracted from the two row
  timestamps the transcript already carries (`time` on the user row and
  on the answer row), so thinking, tool runs, smart-gate waits and the
  user's own answer latency all bill into it. Thinking blocks and tool
  rows keep their own segment spans - the per-phase anatomy is unchanged
  and the last badge is the bottom line. Computed on every paint, never
  tracked: the live canvas and a restored transcript render the same
  number. `webui/test/turntotal.test.ts` pins the law.
- Vue reactivity trap (bit once, live-proven): after pushing an item
  into the reactive transcript, re-capture the reactive proxy
  (`st.transcript[len-1]`) before mutating it - raw-ref mutations
  bypass the proxy and never re-render. This is the stuck-cursor /
  ticking-timer bug class.
- Error payloads in the SSE driver THROW (`composables/omega.ts`) - a
  swallowed stream error is an invisible dead stream.

## Ephemeral chat

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
  search (FTS5), export, counts `/sessions` - the store never
  sees it. The `eph-` prefix is the namespace guard every endpoint
  routes on.
- **Memory tool denied** (run-scoped, `agent.WithDeniedTools`, the
  loop filters post-merge so extension ToolProviders cannot
  reintroduce it). Ceiling: nothing blocks files/shell writes to
  memory.md yet - that is the parked sandbox round's job; it does not
  block ephemeral.
- **Close = gone** (v2 has no upgrade-to-persist; a persist action is
  a separate follow-up card). Closing the last tab on an `eph-` id
  DELETEs the registry record, even mid-run (the run cancels first).
- Retry/fork/variants never exist on an ephemeral tab - the store
  tree they replay is absent. The server 400s the flag combination.

## The composer's `/` palette and `@` mentions

The webui is the shell; the palette is webui-native. Registration and
resolution mirror the one command law every surface follows:

- **A `/` token at the caret opens the command palette** (caret/token-based,
  mid-sentence included - the draft's leading word is just the
  boundary-free case). Rows: the webui's
  shell commands (`composables/commands.ts` `WEBUI_SHELL_COMMANDS` -
  `/new` and `/sessions` plus the local-action set
  `/copy /modules /fork /help /panels /retry /theme /variant`;
  the session verbs `/resume /branch /label`; `/extensions` rides the
  palette as the /modules alias), then extension commands from
  `GET /commands`, then skills as `/<skill-name>` rows.
  Typing filters case-insensitively
  over name AND description; ArrowUp/ArrowDown navigate, Enter picks
  or (with the draft not in palette mode) submits, Esc dismisses.
- **ONE row per name: a shell name wins.** The host routes a shell name
  to the app's own handler BEFORE any dispatch, so a module command of
  the same name would be a second row over a door that can never open -
  `paletteCommands` drops those extension rows (`/new`, `/sessions`,
  `/resume`, `/branch`, `/label` are registered by the store capsule
  for the terminal loop and hidden here).
- **The session verbs are the shell's, because only the shell can act
  on them.** `/resume <#|id|label>` opens a session in a chat tab (the
  `#` counts the rows the sessions pane renders, newest first inside
  each bucket; an id is exact, a label is a case-insensitive prefix);
  `/branch [id]` POSTs `/sessions/{id}/branch` and opens the child;
  `/label <text>` PUTs the label on the tab's session and the sidebar
  row follows (`/label` with no text clears it). None of them touches
  `POST /commands/dispatch` from the webui - a server could create the
  row, never the tab. The store capsule keeps the same verbs for the
  terminal loop, answering TEXT (the resolved session, the branched id,
  the label it set) and declaring no `CmdAction`: `new_session`,
  `load_session`, `branch_session` and `set_label` had no executor on
  any surface, so a client with no field for `actions` showed "no answer
  from the server" and the command read as a dead no-op.
- **Shell commands act LOCALLY and never hit the agent.** `/fork`,
  `/retry`, `/variant`, `/theme` and friends run through the app's own
  handler; extension commands and `/<skill-name>` go to
  `POST /commands/dispatch` (host-local execution - a module command
  returns its result text; a skill returns content the NEXT send arms
  as a system preamble). api.md's "Commands" section documents the
  wire.
- **`/help` is the one REFERENCE-shaped output**. It
  opens the Ctrl+K command palette and answers with a single status
  note ("the palette is the command reference - it is open"). Every
  other shell command keeps the status line: that surface is for
  notices, not reference - a command list crammed into one transient
  line is unreadable and unactionable.
- **The Ctrl+K palette IS the command reference**, so the surface
  `/help` opens cannot drift from the commands that exist. After the
  dockable rows and the shell actions it lists the `/` shell set
  straight from `WEBUI_SHELL_COMMANDS` - name plus description (title
  over description, the hint column stays keybindings-only) - and its
  filter searches names AND descriptions. Picking a `/` row runs that
  verb on the active chat tab through the same host path a typed
  command takes.
- **`@` opens the mention picker.** A bare `@` (or a non-prefixed
  token) lists workspace files plus the two prefix rows;
  `@session:` lists sessions, `@skill:` lists skills. Picking inserts
  the completed token at the caret. The picker only helps TYPE tokens
  - expansion is SERVER-SIDE on the `/chat` submit path through the
    one `core.ExpandAtMentions` law (`core/atmention.go`). The browser
    never reads or expands files;
    unresolvable tokens stay literal in the message.

## The Files tab

- **The tree is chevronless**: expand/collapse triangles
  do not exist. The folder icon alone carries the state - the glyph
  flips `lucide-folder` / `lucide-folder-open` and the row carries
  `aria-expanded`. Keyboard parity: focus + Enter toggles. Hover
  indent guides: one hairline `::before` per nesting level, revealed
  purely by CSS `:hover` on the root tree (0.15s opacity fade) - zero
  listeners, zero layout shift; no narrow exemption.
- **The path guard is the traversal set**: every file
  surface (browse, read, raw) rejects absolute paths, empty segments,
  and ONLY the `.` / `..` traversal segments - dotfiles and dot-folders
  (`.git`, `.worktrees`, ...) list and read like everything else.
  Listed rows carry no error state; a failed read is a one-line
  notice, not a phantom row.
- **The viewer renders ONE body per content class**: `GET /files/read`
  carries the sniffed `mime` (+ `size`)
  field; the viewer tab branches on it - `image/*` -> an `<img>` and
  `application/pdf` -> the browser's native viewer in an `<iframe>`,
  both served by `GET /files/raw` (raw bytes under the read surface's
  exact root jail); `.md`/`.markdown` files and skill bodies render
  through the chat's markdown grammar (same markdown-it + hljs fence
  path, same codeblock decoration); a non-UTF-8 file keeps the honest
  "binary file - no preview" state with the size and a download link
  (the raw route's `?download=1` attachment lane); everything else
  stays the line-numbered monospace text body. The raw route serves
  image/\* + pdf only - other mimes answer 400 outside the download
  lane.

## The Agents tab

- The roster is a flat list (no grouping) over `GET /agents`: one row
  per agent home, the main agent included. The subtitle line carries
  the facts the tab is for - description, model, session count, last
  activity - and the main row additionally carries a `main` badge
  beside its name (the badge also explains why that row carries no
  toggle - it is the home config, always on). The badge rides the row
  law's NAME LINE through `RegistryList`'s `name-side` slot (the `side`
  slot seats things beside the whole text stack, which is what parked the
  badge at the subtitle's tail); an empty slot renders
  nothing. One line below the
  subtitle carries the PREVIEW (ruling 5): the agent's
  newest reply in its canonical chat, whitespace-collapsed and
  ellipsized to one line, with its age at the right edge; a row whose
  agent has not spoken yet renders no preview line at all.
- **A row click opens the agent's CANONICAL chat** (superseding
  the most-recent bind): every row is clickable.
  The click POSTs `/agents/{name}/canonical`, which answers the one
  stable conversation the agent owns (`canonical`,
  `<agent>-canonical`) and MINTS the next generation when the home
  holds none - a fresh agent, or one whose canonical was archived
  (archive = retire; the retired row never comes back to the roster).
  The id the answer names rides the `omega:open-chat-session` window
  event to the shell - module UI imports no shell code, so the literal
  is copied in `harness/continuous/ui/AgentsPanel.vue` and pinned by
  `webui/test/agentschat.test.ts`. The shell runs the ORDINARY chat
  opener, so the click opens a full interactive ChatTab (never a
  read-only observer) in the main pane, and a session already open in
  any tab is FOCUSED rather than opened twice. The preview and the
  active row both follow the canonical id, never the last session that
  happened to run.
- **Agent work sessions do not enter the sessions sidebar** (ruling 4,
  full separation): that list is the maintainer's own conversations,
  and a machinery-owned row (`machine_owned`) is filtered out of it.
  A crashed worker run is inspected from the agent's EXPANDED row,
  which reads `/agents/{name}/sessions` and opens any row as a chat
  tab.
- **`/new` inside an agent chat folds the conversation** (ruling 2):
  the shell POSTs `/sessions/{id}/new`, and a machinery-owned row
  answers `compacted` - same tab, same id, the transcript whole, and
  the next run's context restarted at the fold's boundary with the
  compactor's summary when one was earned. The human's own
  conversations answer `fork` and open a fresh tab exactly as before.
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
  toggle per the pill law, and NO wake control anywhere - waking is
  the scheduler's job, the tab never spawns a wake.
- **The default agent carries NO toggle** (superseding the locked
  pill): it is on by definition - the home config the
  daemon loads - so its row renders no switch and no state text at all;
  the `main` badge explains the absence. `PUT /disabled/agents` still
  answers 409 for the default agent at the handler level and config.yaml
  stays untouched. Every non-default row keeps its pill.
- **The agents rows speak the sessions vocabulary** (round 1
  of the unified theming language): chip radius, the `--z1-raised` hover,
  the name brightening to full text, the hairlines ABOVE and BELOW the
  raised row fading, the whole head line as the open surface, and the row
  whose agent holds the FOCUSED chat session wearing the active treatment
  (`--z1-raised` plus the name in `--accent`). The chip is a hover/active
  DRESS - at rest the row is the row law's own box, so the resting
  separator keeps its width. The focus seam is both a push and a PULL: the
  shell announces the focused id over `omega:focused-session` on every
  change and a panel mounted later asks over
  `omega:query-focused-session` (the dock renders only the active tab's
  pane slot, so the panel unmounts on switch-away; the literals are
  copied into the capsule). The expander is the sessions lucide chevron
  (12px, rotating 90deg in 0.12s). It is a CONSUMER OPT-IN - `RegistryList`
  takes `feel` / `expander` / `activeName` and the other four registry tabs
  keep the quiet list until their own round.
- **Expansion = details + edit**: expanding a row seats the config
  summary and the `PUT /agents/{name}` DEFINITION-layer editor - the
  0.17.3 field set (description, system prompt, a REFERENCED provider
  instance, model name, max turns, toggles, skills allowlist, the
  inherit flags; nil = leave alone). Session ROWS are dead on this tab -
  the roster opens a session's chat, it never lists sessions (the
  per-agent session endpoints stay in the API, docs/api.md).
- **The 0.17.3 make-over** (pinned by
  `webui/test/agentsmakeover.test.ts`):
  - **One create door.** A single `+` in the band opens ONE form whose
    agent-kind choice decides the wire: `scaffold` (`POST /agents`),
    `continuous` (`POST /continuous`, directive + wake schedule) or
    `inherit from an agent` (`POST /agents` with `inherit_from` - the
    CLONE law's materialized copy at birth, the rename path is clone
    then delete). The old `@` duplicate door and its second form are
    gone, and the `+` no longer depends on the continuous capsule -
    only the continuous KIND does.
  - **Delete = delete the home.** Each secondary agent's expansion ends
    with a two-beat destructive lane: the trigger, then a confirm that
    NAMES the session count (`DELETE /agents/{name}`, receipt carries
    the same count). The main agent has no delete - its home is the
    daemon's home.
  - **No secret in the panel.** The provider is a `<select>` over the
    instance names `GET /settings` defines; host, type and api key have
    no field at all (an agent file holds none - `agent/configedit.go`).
  - **Inheritance chips.** The four inheritable knobs (system prompt,
    provider instance, model, max turns) carry an `inherited from main`
    chip plus a one-line source note when the layer file OMITS the key,
    and a reset button (the layer's `unset` verb) when it names one -
    the same authored-vs-inherited state machine as the cron editor's
    chain, fed by `GET /settings`' per-agent scopes. Toggles and skills
    are never in it: they are the agent's own set.
  - **Always off + explicit allowlist.** Toggles are checkboxes over the
    host's mounted, toggleable capsules (never the locked pair, never
    one the host has off) and an empty set says `nothing enabled`; the
    skills allowlist is a checkbox set over the agent's visible registry
    and an empty set says `no skills`.

## The kanban board (lane rails)

The board is `harness/kanban/ui/Board.vue` over the pure lane logic in
`harness/kanban/ui/board/boardLogic.ts` (pinned by
`webui/test/kanban.test.ts`). Nine lanes in the Hermes status grammar,
one horizontal scroll strip, 236px columns.

- **ANY lane folds to a rail**. A folded lane is a
  thin vertical strip (`LANE_RAIL_PX` = 36px, the 32-40 ruled band):
  the lane's colored dot on top, the lane name in `writing-mode:
vertical-rl` uppercase small, the count below when the lane holds
  cards. The header's dot + name are the fold control on every lane -
  the old settled-only fold (done/archived collapsed, the "N folded"
  note) is dead; `isSettled` no longer exists.
- **Dots align across the board.** The rail's dot row and an expanded
  lane's header row are the same height (`--lane-head-h: 24px`), so the
  dot row reads as one line whichever state each lane is in.
- **The rail is also a drop target**, not just a button. The drag
  machinery resolves the lane element (`.board-col[data-status]`), and
  the rail renders inside that same element, so dragging a card onto a
  rail moves it into that status (top of the lane - `after_id` empty).
  No rail-specific drop code exists. **A live drag freezes the lane
  shapes** (`frozenLanes`, snapshotted when the gesture turns into a
  drag): the optimistic reorder would otherwise fold the lane the card
  left and unfold the rail it entered, sliding the whole strip under the
  cursor mid-gesture - the drop then lands on whatever moved under it
  (measured: a release on the folded `blocked` rail persisted into
  `ready`). Rails stay rails while dragging; the lane the card lands in
  takes its new shape after the commit.
- **Lane tone** (`LANE_TONES`, the Hermes mapping): triage gray, todo
  light gray, scheduled violet `#a78bfa`, ready blue `#60a5fa`, running
  green `#34d399`, blocked red `#f87171`, review amber `#fbbf24`, done
  gray `#6f6f6f`, archived dim `#4a4a4a`.
- **The auto-collapse law.** An EMPTY lane folds to its rail
  automatically, and a lane holding cards stays open. A pin (`open` /
  `collapsed`) is the user's override and is the ONLY override since the
  0.17.3 make-over: a CLICK on a collapsed lane expands it - the empty
  lane included, so an empty rail opens to its create form instead of
  being a dead strip. Nothing reconciles a pin away any more (the old
  `reconcileLanePins` dropped a drained lane's pin, which re-folded the
  lane the user had just opened). Pins are component state, never
  persisted.
- **A rail keeps the create control.** The rail carries the same `+` the
  header does, so a lane can always grow from nothing; opening the create
  form holds the lane in its column shape while the editor is up.
- **~180ms width ease** (`LANE_EASE_MS`, cubic-bezier(0.4, 0, 0.2, 1))
  on `flex-basis` - Hermes snaps, omega eases. The numbers ride CSS
  custom properties set from the constants, so the view and the logic
  cannot drift.
- **The narrow stack has no rails** ("not in mobile
  view"). Below `NARROW_MAX_PX` the lanes stack full width and every
  lane renders expanded - the fold is a horizontal-scroll affordance.
- **The card grammar**. A card is a reading surface:
  the title and the body teaser clamp at `CARD_CLAMP_LINES` (2) lines
  (`-webkit-line-clamp`, fed to CSS as `--card-clamp` from the
  constant), the assignee is a bordered chip, and ONE meta row carries
  the assignee, the running heartbeat, the created-age and the move
  controls. The row never spills: `.card-meta` clips and the heartbeat
  chip is the row's shrinker (before that, a wide row gave the whole
  lane a horizontal scrollbar).
- **The running heartbeat chip.** A card in `running` wears a live
  `working - Xm` chip computed from `started_at` (`heartbeatChip` in
  boardLogic); the board re-dates ONE `now` on a `HEARTBEAT_TICK_MS`
  (5s) setTimeout chain - a clock, never a fetch (the dead poll stays
  dead, pinned by the no-setInterval test). A running card's created-age
  stands down in the chip's favour: one time telling per card. When the
  row carries `last_heartbeat_at` and it has aged past
  `HEARTBEAT_STALE_MS` (120s) the chip flips to `stale - Xm quiet` in
  the blocked-lane red with a flat dot. Staleness needs an AGED stamp:
  a row with no heartbeat field never reads stale. The Hermes board
  stamps that field; omega's own kanban store does not yet (its rows end
  at `started_at`), so the stale face stays dark on an omega store until
  the wire grows the field - heartbeat emission is a separate card, the
  eye is wired for it now.

## The Settings pane

`SettingsTab.vue` is a MASTER-DETAIL surface (settings
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
- **Selecting a section FILTERS the pane** (the VS Code
  read; it narrows what the one document RENDERS and reverses nothing).
  A nav click renders that category and its subsections and nothing else -
  the pane's row count equals the section's own. A deselect restores the
  whole document. Commonly Used filters like every other category: its
  curated section is the one thing on screen.
  It is a VIEW filter: no knob changes, no config is written. A live
  search OVERRIDES it - one global match across every section - and the
  filter returns the moment the query clears. A scroll moves the
  highlight only; only a click (or a deep link) filters. The selected
  section rides the in-app URL on the QUERY axis (`?settings=<id>`,
  replaceState only), so an in-app deep link lands the pane pre-filtered
  and outranks the remembered anchor. The PATH names the PANE too:
  `/settings/` is the settings route, and
  the client WRITES it - every door that opens or focuses the tab (the
  gear, the `/modules` family of commands, the palette's dockable entry, a
  settings chip click) replaceStates the path, so a copied address bar
  reloads into the same view. A boot or a popstate on the path opens the
  tab through the gear's own opener (a live instance focuses in place).
  Every chat focus hands the path back - both openers (the sidebar's row /
  new / new-ephemeral, the palette's recent-session switch, the capsule
  `CHAT_SESSION_OPEN_EVENT` seam), a chat tab click, a popstate into a
  chat entry, and closing the settings tab itself (the dock's
  neighbor-focus lands in the same re-focus path) - so the claim never
  outlives the tab, and the release WRITES the path the pause held (a
  derivation suppressed while the claim was up would otherwise stay stale
  when the focused chat does not change). Settings stays a CLOSABLE tab:
  the path is a focus claim, never a pinned view.
  The TRAILING SLASH is the contract, not decoration - the knob API owns
  the bare noun (agent/api/server.go registers exact `/settings`, beside
  `/settings/raw` and `/settings/reset`) while `/settings/` misses every
  API pattern and the catch-all hands it to the SPA shell, exactly the
  split `/board/` has. So a deep link lands the app, never the JSON; the
  bare form is the API and the client never writes it.
  The two axes compose - `/settings/?settings=<id>` reloads with the tab
  open and the pane pre-filtered, and a claim keeps whatever query the
  section axis already carries. The jump/clamp/spy mechanics are
  untouched: under a
  filtered document the clamp's window and the document's own limits
  coincide, and the jump simply rests on the section it selected. The
  live filter is pinned by `webui/test/settingsnav.test.ts` and
  walked by `scripts/journeys/recipes/113-settings-nav.journey.sh`; the
  two-door path contract is pinned by `TestSettingsPathTwoDoorContract`
  (api/server_test.go) and the claim/address-bar wiring by
  `webui/test/routepath.test.ts`.
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
- **A scope switch normalises both remembered ids**: the
  highlight (which the scroll-spy moves on its own) and the section filter
  (the only id the document obeys) are asked SEPARATELY whether the live
  nav can resolve them. They disagree whenever a live query moves the
  highlight onto a category the next scope carries while the filter stays
  on the category the user clicked - a category only the old scope had.
  Reading the highlight alone resolved, cleared nothing, and left the
  document filtered to a category the live nav cannot render: the pane
  answered "no settings here" and the address bar kept the hidden section.
  Pinned by `webui/test/settingsnav.test.ts` and walked live by
  `110-settings-scopes.journey.sh`.
- **The accordion is deleted, not hidden**: no `isCollapsed`, no group
  heads, no `omega.settings-collapsed`, no `.settings-group-head` CSS.
  Pinned by `webui/test/settingsnav.test.ts` (the tree, the selection
  algebra, the content view, the grey-out set, plus the deletion proof) and
  the live pair `scripts/journeys/recipes/113-settings-nav.journey.sh`
  (landing, two levels, search greying, modified-only, drill-down + back,
  remembered selection, no-accordion proof) and
  `110-settings-scopes.journey.sh` (the tier-3 scope tabs, re-witnessed
  under the new bones).
- **The whole-document band**: beside the knob rows, the
  pane's band carries three config-document actions - `reset to defaults`
  (POST `settings/reset`, the defaults belong to the host, so the client
  never assembles them), `export to yaml` (downloads the live config as a
  yaml file), and `edit as yaml` (a native `<dialog>` editing the document
  as text; the save PUTs through `/settings/raw`, which validates and
  persists via the host's own write path - the body is capped at 1 MiB, a
  larger document is refused with 413). Scope-aware: the main tab edits
  `config.yaml`; an agent scope tab edits its own layer. The endpoints are
  documented in `api.md`.

## The Logs tab

- The face of the observability capsule (`harness/observability`, the
  engine half): two read-only routes, ONE tab. The tab is NAMED "Logs":
  the old raw-tail
  `http_channel` pane answered the same question with nothing but a
  `GET /logs` tail, and retired with its component - one logs surface
  survives. The STRUCTURED event
  stream is the primary view - `GET /observability/events?session=&level=&limit=`
  answers `{events, returned, buffered, capacity}` with rows of
  `{seq, time, session, origin, type, level, tool, turn, duration_ms,
summary, error}` (level is DERIVED server-side: error on a failure field
  or an IsError result, warn on an unclean stop, info otherwise; origin
  is the run's declared surface, and it is what gives the gateway chip
  its rows). The raw log
  tail is the SECONDARY view inside the same tab (ruling 1: not a peer
  tab) - `GET /observability/logs?lines=` answers
  `{path, lines}` verbatim.
- **Band controls**: the session chip (scope defaults to the OPEN
  session, the chip flips the whole ring into view - a shell with no
  focused session says "all sessions" and disables the toggle), the
  errors-only chip, the tail chip that swaps the pane's view, and the
  clear action. No free-text search in v1 (ruling 4, deferred until the
  stream proves too loud) and no OTel anything (ruling 5).
- **The source family** (its own strip under the band, stream view
  only): one toggle per source, multi-select, AND-composed with the
  severity chip. Sources are DERIVED client-side from a record's own
  provenance (`rows.ts eventSource`), because the ring has one producer
  family - the agent loop's spine - so a record carries exactly four
  clues: its type, its tool, its session and its origin. Precedence:
  the RUN outranks the record (a `cron:`-labelled session belongs
  entirely to the scheduler, label read from `/sessions`; an
  HTTP-channel run, whose every record carries origin `gateway`, belongs
  entirely to the serving layer), then the TOOL's capsule (`kanban.*` is
  the board), then the provider stream (`stream_end`), and the loop owns
  the rest. Five sources are the whole vocabulary: the `gateway`
  producer is the serving layer itself - `agent/api` stamps each run it
  starts with `core.WithRunOrigin(ctx, "gateway")`, `SessionEventObserver`
  hands `(session, origin)` to the recorder, and the ring row carries
  `origin`. Every record lands in exactly one source; an empty enabled
  set means every source is OFF, not every source on.
- **Clear is a display watermark**, never a delete:
  `clearedThrough` holds the highest `seq` read at click time, rows at
  or below it stop rendering, the ring total in the meta line holds
  (the records are still read) and the log file is untouched - new
  records paint as they arrive. The meta line names the watermark
  ("cleared through N"). The button is disabled in the tail view (the
  tail is the file's own verbatim lines) and when the ring is empty.
- **Colors ride the existing ladder, one accent per source**: llm
  `--accent`, cron `--ok`, kanban `--text`, agent `--text-dim` (the
  loop is most rows, so it is the quietest), gateway a derivation of two
  ladder tokens (`color-mix(in srgb, var(--accent) 55%, var(--text-dim))`)
  - the palette carries no fifth hue and the accent's dimmed variant is
  the serving layer's: it sits beside the stream it carries, and a theme
  override recolors it for free. `--unread` and `--err` are
  semantic-only and never decorate a source. The accent paints the
  chip's dot/border and the row's type cell; the level rail above it
  keeps its own meaning (error `--err`, warn `--accent`).
- **Filters differ by kind, on purpose**: the session scope is a SERVER
  parameter (the ring's `Page()` selects the newest N of that session;
  filtering the newest N of everything would starve a quiet session),
  while errors-only and the source chips are CLIENT filters over the
  fetched page - one page is `limit=1000` against a 1024-record ring,
  i.e. the whole live history, so a toggle is instant and costs no round
  trip.
- **Rows paint newest first** and the DOM is capped at 300 (the ring is
  1024 records; the meta line carries "N of M events" plus the error
  count and the ring's fill). Error rows carry a `--err` rail and level
  cell, warnings the accent rail; a row's failure text rides its own
  line, and the session column appears only in the all-sessions scope.
- **Refresh is bounded by mount**, not a permanent poll: fetch on open
  plus one `REFRESH_MS` (5s) interval cleared on unmount - the dock
  unmounts a sibling-activated outlet, so a background tab costs
  nothing. The push channel (`composables/socket.ts`) carries a CLOSED
  topic set of five names the client speaks (chat, chips, canvas, runs,
  health - the server's sixth, `logs`, lost its client half with the
  raw-tail pane), none of them an observability topic;
  riding it would be an engine-side change
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

Two module capsules complete the main surface (terminal + browser):
the terminal module hosts a PTY in the
bottom pane (`TerminalTab`, journey 95), and the browser module hosts
an any-origin iframe tab in the main area (`BrowserTab` over
GET/PUT /browser/last, the resume URL; sites refusing embedding get
an explicit fallback panel, journey 96). Both are in-process capsules
of the module table (docs/architecture.md). Both views are the
capsules' own - they ship at `harness/<name>/ui/` and the shell's dock
outlets bind them from the generated module-tabs manifest
(`module-tabs.generated.ts`; the shell's registry in
`dockables.ts` keeps the composition: `TAB_MODULES`, the pinned-root
order, title/icon/policy). Removing a capsule drops its manifest
entry - the tab hosts nothing, same shape as a removed panel capsule.

## Component map

- `app.vue` - orchestrator: open/create routing (open in the main pane,
  focus-existing), URL sync (`/chat/<id>` vs `/`, plus the settings claim
  `/settings/` - written on every open/focus of that tab and handed back on
  a chat focus - and the READ-ONLY `/board` landing: the app never writes
  that one), scroll pinning, stacked-mode layout, live telemetry, tab-title
  spinner.
- `composables/chat.ts` - the stream driver (per-tab state incl. image
  and file attachments: `attachments` = the base64 image lane,
  `files` = the /attach-file reference records).
  `composables/omega.ts` - typed API client: SSE
  `parseFrame`/`handlePayload`, `streamChat` (throws
  `StreamInterruptedError` when the reader errors or ends without
  `agent_end`), sessions CRUD + pinned/read PATCH + archive/unarchive,
  search, transcript fetch, files browse/read, image attach
  (`attachImageUrl`), non-image attach (`uploadAttachment` - the
  multipart POST /attach-file, part order `session_id` then `file`),
  `renderMarkdown` (markdown-it, `html: false` - the lib's default
  escaping is the sanitizer), `createRenderScheduler`.
  `composables/moduleState.ts` - the one live module-state store: the
  settings MODULES rows and the dock's runtime gate read
  the same payload, one boot read plus the toggle responses, the disabled
  set cached for the synchronous boot hydrate.
- Shell: `MenuBar` (the three seats, see the top-bar paragraph),
  `NavCluster` (back / forward / the palette seat - clock closed, the
  palette's own field open, in place - / the Ctrl+K trigger, dead
  center), `StatusBar` (health dot,
  token/ctx/rate telemetry, build version; uptime lives in the health
  popover the dot opens), `SearchInput`,
  `PaneDivider`, `TabBand` (the universal panel top bar: title left,
  actions right, outside every scrollport), `TogglePill` (the one
  switch, see above), `BandSelect` (the themed select - chip button +
  role="listbox" pop, generalized over short option ladders: band
  toggles, the board switcher, the drawer status, settings knobs, the
  composer's reasoning-effort chip).
- Chat: `AppComposer` (auto-grow editable editor - `contenteditable`
  `.composer-input`, the chip law: a reference chip
  REPLACES the token span it represents, inline in the flow, and
  Backspace behind a chip dissolves it back to the raw token text while
  the chip's x removes outright; `/skill` tokens chip
  on the SAME machinery as `@file` ones - the composer scans with
  `ScanOpts.inlineSkills` (a skill token at ANY word boundary, the shape
  its caret-based slash palette accepts) and the chip is the one
  `ReferencePill` renderer, so a skill wears the zap + `/name` and a file
  the document glyph + basename. The transcript keeps the
  leading-invocation scan, where arbitrary prose "/" stays text. The
  `@` palette's two PREFIX rows insert the server's own injections
  (`@skill:<name>`, `@session:<id>`, core/atmention.go), and the scan
  classifies them by that prefix rather than as paths: an
  `@skill:<name>` token wears the SAME skill pill whichever door
  injected it (its click opens SKILL.md), while an opaque
  `@session:<id>` stays literal text - neither is a file reference.
  Shift+Enter writes the newline as a
  text node (the editor claims every Enter - plain sends), and Chromium
  hangs a block-final newline in pre-wrap, so every write settles one
  draft-invisible sentinel `<br>` after it (AppComposer's capTail)
  that turns the break into a real line box and seats the
  caret on the new line - pinned by the 102-composer-shift-enter
  journey and `webui/test/composershiftnl.test.ts`; ONE morphing action button -
  send/stop, dimmed at idle with no draft, `+` opens the file picker,
  attach-strip (image thumbnails + non-image reference chips: type
  glyph + name + size, never thumbnailed - the reference lane,
  composables/attachments.ts owns the glyph/size helpers), paste +
  paperclip attach; the
  retry face is retired - the newest response's action row carries the
  msg-retry chip; `/` command palette + `@` mention
  picker, see below; the composer shape law lives in
  [`.agents/DESIGN.md`](../.agents/DESIGN.md),
  pinned by the 22-composer journey and
  `webui/test/composershape.test.ts`; `ModelSelect` + the shared `BandSelect` effort chip (the dedicated
  ThinkingSelect retired - the BandSelect owns the select grammar; the
  model+effort pair is one nowrap wrap unit), `ChatMessage`,
  `ThinkingBlock`, `ToolCallLine` (running/done/error, expandable
  result), `ToolGroup` (the tool-group RENDERING law:
  a run's CONSECUTIVE tool rows collapse into ONE collapsible group -
  `composables/toolGroup.ts` folds each render list strictly, a
  variant-anchored row never gets swallowed, the group rides the shared
  `chat.tool_display` knob with auto open-while-live and a session-scoped
  pin; pinned by the 23-tool-group journey and `webui/test/toolgroup.test.ts`),
  `ChatTab` (transcript + composer, live-pin layout),
  `ChatItem`.
- **The live tool-status set has ONE owner**
  (`webui/app/composables/toolStatus.ts`): `isLive` is the call still in
  flight, `running` or `pending`, with its two named halves
  `isStreaming` (`running` only - the bytes are arriving in this
  process) and `isYielded` (`pending` only - the result re-enters as a
  later wake turn). A new status (waiting, queued) lands in
  `toolStatus.ts` and every consumer inherits the answer; no surface
  spells the set inline, and a caller that needs a narrower half names
  the predicate and says why at the call site.
- Sessions: `AppSidebar` (band head with the in-band search, the
  full-width new-session row, pinned section, day buckets, per-row
  pin/status affordance, archive/trash hover actions,
  branch/rename/export row actions, archived group, order law above).
- Files: `FileTree` + `FilesTab` + `InspectorPanel` (module panels slot
  in). See the Files tab section below. Settings: `SettingsTab` (the
  config knobs as a dock-tab singleton; the theme picker is TWO knobs -
  color mode and the custom theme; model slots ride
  `ModelField`, the BandSelect listbox with capability filters).
  Terminal view: `harness/terminal/ui/TerminalTab` (the bottom-host PTY
  over the terminal module's routes; a fresh spawn lands in the
  resolved workspace - the focused chat session's pinned root rides
  the claim as `?session=<id>`, the server resolves it against the
  `server.default_workspace` knob and the process cwd). Browser view:
  `harness/browser/ui/BrowserTab` (URL bar + iframe over
  GET/PUT /browser/last; X-Frame-Options refusals render the fallback
  panel, never a silent blank frame). Both are tab capsules - their
  views live in the capsule, the shell's outlets bind them through
  `module-tabs.generated.ts`.
  Observability view: `harness/observability/ui/ObservabilityTab` (the
  event stream + the tail toggle over the observability capsule's two
  reads; the pure filter/format half is `ui/rows.ts`) - the tab the dock
  names "Logs".
- Canvas (arc 3c): `composables/canvas.ts` is the store -
  snapshot load (GET /ui/canvas) + view_model_delta merge (forwarded
  by the chat host's `canvasDelta` dep) + the action pipe POST
  (optimistic pending, error state on 4xx/5xx); the standalone
  generic host tab (`components/canvas/CanvasPanel.vue`) is gone -
  folded into the settings MODULES section with the rest of the module
  surface, the chat transcript renders FROM the
  canvas view-model.

## Testing

```bash
node --test "test/*.test.ts"        # from webui - node's built-in runner
npm run knip                        # from webui - dead exports/files/types/duplicates
bash scripts/build.sh               # SPA generate + sync to api/static
python3 scripts/stream_check.py http://127.0.0.1:8098   # transport probe (dev omega must run)
```

`knip.json` roots the analyzer at `webui/`, so a shell export whose only
consumers are capsule UIs (`harness/<name>/ui`) reads as dead there; the
census in `test/capsulecensus.test.ts` names those imports in-workspace and
asserts every capsule-imported shell name is still exported.

`stream_check.py` exit codes: 0 healthy (incremental chunks), 1 buffered
(no incremental arrival), 2 no stream at all.

**The test law: tests receive the signal shape production sends.** A
unit test that unwraps a ref itself proves nothing - the auto-focus
lesson (2026-10-20): shipped 100% dead with every gate green, because
the test compared a `Ref` against its `.value` string, a signal-shape
mismatch that passes every gate including the health check. Same
family: a ReferenceError in a setup script passes every gate and only a
render catches it (the NUXT_E1005 lesson: `go test` and `node --test`
never see it, vue strips types, and no template check runs). Tests bind
at the production boundary or they do not exist.

Notable suites: `interrupted.test.ts` (transport-death detection + transcript
transitions), `identity.test.ts` (the chat transcript's row identity law - a
41-row window, the head leaving, every survivor keeping its object, and the
reactive-proxy binding an in-place id/content write must invalidate),
`composer-retry.test.ts` (the morphing button), `panemodel.test.ts`
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
- `scripts/build.sh` runs `nuxt generate` (NOT `nuxt build` - an
  ssr:false `nuxt build` emits a nitro server; generate emits the
  static SPA) and syncs to `../api/static/`. Run it before
  `go build`.
- Never hand-resolve `static/` conflicts - regenerate.

### Poisoned-bundle diagnosis

Conflict markers appearing in the SERVED UI = the SPA was built from
conflicted/stash-poisoned source (the build gate refuses to ship that,
but a manual build can slip one through). Diagnosis: grep the SERVED
content, never source strings - `grep -r` over the served CSS/module
files (find-based, conflict-marker pattern), then regenerate via
`scripts/build.sh`, re-serve, and verify the regenerated bundle came
out clean. A grep against the `.vue` sources will lie to you.

## Work guidance

- The visual feedback loop is the user's screenshots + the in-app
  comment system (headless-browser checks blocked on missing Chromium
  deps on the Pi; a real browser session works). In-app comment rounds
  keep scope narrow - never split one DOM group across changes.
- Verify SERVED content, never source strings.
- Vite dev-server cache ghosts are real: on mismatch, restart the dev
  server and hard-refresh before debugging.

## Related

- [api.md](api.md) - the HTTP wire the webui rides
- [configuration.md](configuration.md) - the knobs the settings pane renders
