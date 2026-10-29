// omega webui: dock engine (card t_63253264) - the layout tree.
// VS Code-style: any pane splits into rows/columns, unlimited depth, and the
// whole tree persists under omega.dock-layout (per-browser localStorage; the
// URL owns ONLY the focused chat session - app.vue's concern). Tabs and DnD
// are card 2; this module is the tree: pure, render-free, unit-testable.
//
// Node model:
//   split { direction: row|column, children: [node], sizes: [fraction] }
//   pane  { tabs: [{ component, sessionRef? }], active, fixed: px|null }
// `sizes` are fractions of the split's CONTENT box, parallel to children.
// A pane with `fixed` px ignores its fraction (flex: 0 0 Npx, exactly the
// pre-dock sidebar behavior); the flexible children share what's left
// proportionally via their fractions - flex-grow: fraction / flex-basis: 0.
// One flexible child = it absorbs all remainder (the default shell's chat
// pane). The `fixed` value is the drag handle's target: dividers adjust px
// for fixed panes (snap attractor 320/640 applies, 128px floor stands) and
// fractions for flexible pairs.

import {
  PANE_OPEN_MIN,
  SNAP_STEP,
  BOTTOM_SNAP_STEP,
  openClamp,
  snapCandidate,
  snapCandidateAt,
} from "./panes.ts";
import {
  endGesture,
  zoneSplitOf,
  type DragZone,
} from "./dockdnd.ts";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  MENU_DOCKABLES,
  COMPONENT_TITLES,
  defaultHostTabs,
  isClosableTab,
  isLockedTab,
  validViewerRef,
  type DockableComponentLike,
  type DockComponentName,
  type ViewerRef,
} from "./dockables.ts";
// the openable component universe, the menus' refill inventory and the
// tab-title fallback live in THE dockable registry (dockables.ts, card
// t_30ee04fb); re-exported here so engine-side consumers keep one import
// surface
export {
  DOCK_COMPONENTS,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  MENU_DOCKABLES,
  COMPONENT_TITLES,
  isClosableTab,
  isLockedTab,
};
export type { DockComponentName, DockableComponentLike, ViewerRef };
// re-export for consumers that think in dock terms (tests, toggle helpers)
export { PANE_OPEN_MIN };

export const DOCK_LAYOUT_KEY = "omega.dock-layout";
/** v0.14.4 ruling 6 BACKUP LAW: before the first auto-persist of a
    REPAIRED layout, the raw record is copied here - a repair-chain bug
    must never destroy the only copy of the user's layout. */
export const DOCK_LAYOUT_BAK_KEY = "omega.dock-layout.bak";

// pre-dock pane state. Consumed ONCE by migrateLayout (first boot without a
// dock key), then untouched - the tree is the single source of truth after.
export const LEGACY_WIDTH_KEYS = {
  sidebar: "omega.sidebar-width",
  inspector: "omega.inspector-width",
} as const;

// the pre-existing drag floor (both sidebars dragged to 128 before the
// squeeze chain closed them) and the hard chat floor.
export const MIN_PANE_PX = 128;
export const CHAT_FLOOR_PX = 320;
/** Endo's comment (2026-10-26): the bottom shell's max share of the
    split's content box. A drag past it (the pane swallowing the
    window) strands the divider offscreen - the pane is bottom-anchored,
    so its TOP edge (= the divider) climbs out of view and cannot be
    grabbed back. t_e539ba29 (2026-10-28): 0.65 still leaves the chat a
    cramped strip under the composer (the max-up screenshot); 0.5 keeps
    the pane and the chat honest halves - the anti-stranding rationale
    only gets SAFER as the number drops, and the divider climbing out of
    view is the high-share failure mode. */
export const BOTTOM_MAX_FRACTION = 0.5;

// --- leaf contracts (v0.14.4 ruling 7) -------------------------------------

/** a leaf's sizing constraints. A pane shipping NO contract resolves to
    today's constants exactly (ruling 7): fixed -> the 128 drag floor,
    flexible -> the 320 wall, the +320 attractor ladder (the bottom keeps
    its +200 ladder), no ceiling except the shells' geometry (the bottom's
    max share). maxPx/maxFrac ceilings and `preferred` feed the resolver,
    which runs on drag, load AND window resize (maxFrac is viewport-
    relative - the one new runtime behavior). `anchor` declares the magnet
    edge (sidebars: leading; the bottom shell: bottom). The contract is
    serialized in the layout record from v:3 on; relaxation order keys on
    the pane class (shells 2, chat-hosting 1, plain 0) - not carried here
    (it rides the record, milestone v:3). */
export interface PaneContract {
  /** the hard floor: a drag/squeeze never starves the pane below it */
  minPx: number;
  /** absolute px ceiling (null = unbounded) */
  maxPx: number | null;
  /** share-of-container ceiling (null = unbounded) */
  maxFrac: number | null;
  /** the width the pane asks to reopen at (shells: their remembered px) */
  preferred: number;
  anchor: "leading" | "trailing" | "bottom";
}

/** the primary sidebar's factory: min 128, prefers 320, leading anchor -
    the snap rungs {320, 640} stay the shared +320 ladder (SNAP_STEP).
    maxFrac omitted for now: the edge shells' give-way math is the
    narrowSqueeze resolver's job (milestone: window resize wire-up). */
export function edgeShellContract(): PaneContract {
  return { minPx: MIN_PANE_PX, maxPx: null, maxFrac: null, preferred: 320, anchor: "leading" };
}

/** the chat pane's factory: the 320 wall, flexible otherwise. */
export function chatPaneContract(): PaneContract {
  return { minPx: CHAT_FLOOR_PX, maxPx: null, maxFrac: null, preferred: 0, anchor: "leading" };
}

/** the bottom shell's factory: min 128, prefers 200, capped at the max
    share (viewport-relative), bottom-anchored. */
export function bottomShellContract(): PaneContract {
  return { minPx: MIN_PANE_PX, maxPx: null, maxFrac: BOTTOM_MAX_FRACTION, preferred: 200, anchor: "bottom" };
}

/** the drag floor a pane contracts to RIGHT NOW - the resolver the
    divider's cap math reads (it runs on drag; load + resize join when
    contracts serialize in v:3). Fixed panes drag down to the shared 128
    floor; a flexible pane walls at its contract min, defaulting to the
    chat's 320. This is the pre-contract ternary
    (`pane.fixed !== null ? 128 : 320`) reproduced verbatim - no pane
    carries a contract until v:3, so every characterized drag outcome
    holds unchanged. */
export function contractFloorOf(node: DockNode): number {
  if (!isPane(node)) return CHAT_FLOOR_PX; // a nested split absorbs like a flexible pane
  if (node.fixed !== null) return MIN_PANE_PX;
  return node.contract?.minPx ?? CHAT_FLOOR_PX;
}

// component names panes may host: the dockable registry is the single
// source (see the re-exports above)

export interface DockTab {
  component: DockComponentName;
  /** the session id a CHAT tab is bound to (null = unsaved composer-first
      chat; written by the host once /chat creates the session) */
  sessionRef?: string;
  /** chat tab instance key (card t_4f0e88f2): minted at tab creation,
      stable across pane moves and reloads - the per-tab chat host keys
      its stream state on it. Always present on chat tabs after boot. */
  key?: string;
  /** card t_36ceb99b: WHAT a viewer tab shows - the file path or skill
      name. THE viewer tab's identity: focus-reuse keys on it (same ref =
      focus the existing tab). Viewer tabs mint with it; nothing else
      carries it; a viewer tab without a viewer payload is unreachable
      (the menus can't mint one) and the reader drops it. */
  viewer?: ViewerRef;
}
export interface DockPane {
  kind: "pane";
  id: string;
  tabs: DockTab[];
  active: number;
  /** fixed px size (sidebars: WIDTH; the bottom shell: HEIGHT); null =
      flexible (shares remainder) */
  fixed: number | null;
  /** card t_d6b657a7: the pinned shell identity - "left" = sessions home,
      "right" = inspector home; card t_67fc1905: "bottom" = the bottom
      panel's slot (the pinned horizontal child under the main panel).
      undefined = main pane. "Fixed" is pane IDENTITY, not content: a
      shell pane unclosable as a whole, but its TABS are free (everything
      drags everywhere except sessions). */
  edge?: "left" | "right" | "bottom";
  /** v0.14.4 ruling 7: the leaf's sizing constraints. Optional - an
      absent contract resolves to today's constants via contractFloorOf,
      which is the pre-contract behavior verbatim. Serialized from v:3. */
  contract?: PaneContract;
  /** v0.14.4 ruling 1: the PINNED ROOT's local split tree. Only a shell
      (edge-marked pane) grows one; its leaves are ordinary panes (no edge
      markers - identity stays with the root), the root's own classic tab
      bar is empty while grown, and un-growing returns the survivor's
      content to it (ruling 8's fallback). Root geometry - fixed px,
      contract, edge marker, hide/fullscreen modes - never moves down. */
  grown?: DockSplit;
}
/** the pinned shells' markers (the toggle/find API's domain names) */
export type DockEdgeComponent = "sidebar" | "inspector" | "bottom";
/** the bottom panel's constant height ladder default (runs at rung 200) */
export const BOTTOM_DEFAULT_PX = 200;
export interface DockSplit {
  kind: "split";
  id: string;
  direction: "row" | "column";
  children: DockNode[];
  /** fractions of the content box, parallel to children (fixed panes ignore theirs) */
  sizes: number[];
  /** v:3 RECORD only: the children's integer px shares on disk. In-memory
      trees always carry fraction sizes; the reader derives them (px_i /
      sum). Never render: DockSplit.vue reads sizes. */
  px?: number[];
}
export type DockNode = DockSplit | DockPane;

// --- ids -------------------------------------------------------------------

let idSeed = 0;
/** deterministic id generator; seed it from a loaded tree so reloaded
    sessions never mint an id that already exists in the persisted tree */
export function nextNodeId(): string {
  return "n" + (++idSeed).toString(36);
}

export function syncIdSeed(tree: DockNode): number {
  for (const n of allNodes(tree)) {
    const num = Number.parseInt(n.id.slice(1), 36);
    if (Number.isFinite(num) && num > idSeed) idSeed = num;
  }
  return idSeed;
}

// --- chat tab instance keys (card t_4f0e88f2) -------------------------------

let tabKeySeed = 0;
/** tab keys use their own c-prefixed namespace so they never collide with
    pane/split node ids; seeded from the loaded tree at boot */
export function nextTabKey(): string {
  return "c" + (++tabKeySeed).toString(36);
}

export function syncTabKeySeed(tree: DockNode): number {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    for (const t of n.tabs) {
      if (!t.key) continue;
      const num = Number.parseInt(t.key.slice(1), 36);
      if (Number.isFinite(num) && num > tabKeySeed) tabKeySeed = num;
    }
  }
  return tabKeySeed;
}

/** boots every chat tab to the key invariant the host relies on: chat tabs
    minted anywhere (make Pane defaults, pre-card-3 persisted trees) that
    carry no key get one right there - mutates, so callers run it on PLAIN
    loaded data before the ref is set (and persist the result). */
export function assignChatKeys(tree: DockNode): DockNode {
  syncTabKeySeed(tree);
  // deep copy in one pass: chat tabs gaining keys is the only mutation
  const copy = clone(tree);
  for (const n of allNodes(copy)) {
    if (!isPane(n)) continue;
    n.tabs = n.tabs.map((t) =>
      (t.component === "chat" && !t.key) ? { ...t, key: nextTabKey() } : t,
    );
  }
  return copy;
}

// --- small building blocks ---------------------------------------------------

export function makePane(
  component: DockComponentName | null,
  fixed: number | null = null,
  edge?: "left" | "right" | "bottom",
): DockPane {
  return {
    kind: "pane",
    id: nextNodeId(),
    tabs: component ? [{ component }] : [],
    active: 0,
    fixed,
    ...(edge ? { edge } : {}),
  };
}

export function makeSplit(direction: "row" | "column", children: DockNode[]): DockSplit {
  return {
    kind: "split",
    id: nextNodeId(),
    direction,
    children,
    sizes: normalizeSizes(children.length, undefined),
  };
}

/** n equal fractions summing to 1; when given, normalizes the provided ones */
export function normalizeSizes(n: number, sizes: number[] | undefined): number[] {
  if (sizes && sizes.length === n) {
    const sum = sizes.reduce((a, s) => a + s, 0);
    if (sum > 0 && sizes.every((s) => Number.isFinite(s) && s > 0))
      return sizes.map((s) => s / sum);
  }
  return Array.from({ length: n }, () => 1 / n);
}

export function isPane(n: DockNode): n is DockPane {
  return n.kind === "pane";
}
export function isSplit(n: DockNode): n is DockSplit {
  return n.kind === "split";
}

/** every node, depth-first, root first. Descends into a grown root's
    local split tree (v0.14.4 ruling 1): the inner leaves are full panes
    - id seeds, chat keys, finders and the pane census all read them. */
export function* allNodes(tree: DockNode): IterableIterator<DockNode> {
  yield tree;
  if (isSplit(tree)) for (const c of tree.children) yield* allNodes(c);
  else if (tree.grown) yield* allNodes(tree.grown);
}

export function findPane(tree: DockNode, id: string): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.id === id) return n;
  }
  return null;
}

export function findSplit(tree: DockNode, id: string): DockSplit | null {
  for (const n of allNodes(tree)) {
    if (isSplit(n) && n.id === id) return n;
  }
  return null;
}

/** the first pane hosting `component` as its active tab (or any tab when no
    tabs are active-matched), or null - the shell uses this for toggles and
    the MenuBar open flags */
export function findComponentPane(tree: DockNode, component: DockComponentName): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.tabs[n.active]?.component === component) return n;
  }
  return null;
}

/** the FIRST tab hosting `component` anywhere in the tree - the singleton
    lookup (card t_30ee04fb): a singleton exists at most once, active or
    not, so its finders must read every tab, not just active ones. */
export interface ComponentTabLocation {
  paneId: string;
  index: number;
  tab: DockTab;
}
export function findComponentTab(
  tree: DockNode,
  component: DockComponentName,
): ComponentTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex((t) => t.component === component);
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

// --- the pane-model reset (card t_4ae27e0d, generalized t_d6b657a7) ----------
// three identities the whole engine enforces:
//   pinned pane   an EDGE SHELL (left = sessions home, right = inspector
//                 home) carrying the `edge` marker: never closable, never
//                 removable, never movable. Its CONTENT is free - any tab
//                 may live in it or leave it (t_d6b657a7 rulings), except
//                 the locked sessions tab, which lives ONLY in "left".
//   main pane     everything else - the closable class, hover-X pans, the
//                 '+' menus.
//   never-empty   the dock always holds at least one pane; refusals are
//                 no-ops (the tree comes back untouched). The law extends
//                 to tabs: a pinned shell CAN sit empty (it renders the
//                 empty '+' affordance) - it never disappears.

export function paneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) if (isPane(node)) n++;
  return n;
}

/** a pane is PINNED when it carries an edge marker - the sessions/inspector
    shells (card t_d6b657a7: identity, not content). Empty panes and
    main-panel panes are main. */
export function isPinnedPane(pane: DockPane): boolean {
  return pane.edge !== undefined;
}

/** the first main pane, depth-first - where session opens land. The pinned
    edge shells (marked panes) are skipped even when they host chat tabs:
    sessions and '+' opens go to the main class. */
export function firstMainPaneId(tree: DockNode): string | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && !isPinnedPane(n)) return n.id;
  }
  return null;
}

/** Endo ruling 2026-10-25: the main-pane census. Pinned shells don't
    count - the close law is about the MAIN area, which must never reach
    zero panes (the launcher pane IS a main pane; the pane itself is
    always right, only its close button was the bug). */
export function mainPaneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) {
    if (isPane(node) && !isPinnedPane(node)) n++;
  }
  return n;
}

/** the close-refusal predicate (Endo ruling 2026-10-25): `pane` is a
    main pane and no OTHER main pane exists. Every close path consults
    this against the LIVE tree, so a request computed against stale
    state still lands on the truth. */
export function isLastMainPane(tree: DockNode, pane: DockPane): boolean {
  return !isPinnedPane(pane) && mainPaneCount(tree) <= 1;
}

// --- local split trees in the pinned roots (v0.14.4 ruling 1-4) --------------
// The three roots (primary sidebar, secondary sidebar, bottom panel) grow
// LOCAL split trees - the same DockSplit/DockPane engine the main pane uses,
// wrapped inside the root so the root's geometry (fixed px, contract, resize
// band, hide/fullscreen modes) never changes. Sidebars stack ROWS only
// (column splits; zone left/right refuses), main + bottom take both
// directions (drop position decides), and the tight roots cap at two split
// levels (main is unlimited). Tabs move freely across roots; splits stay
// local. Roots never die: a degenerate local tree un-grows back into the
// shell's classic single cell (ruling 8).

/** the depth-first first pane under a split - a grown root's host leaf */
function firstPaneOf(split: DockSplit): DockPane | null {
  for (const c of split.children) {
    if (isPane(c)) return c;
    if (isSplit(c)) {
      const f = firstPaneOf(c);
      if (f) return f;
    }
  }
  return null;
}

/** the grown ROOT pane whose local tree hosts `paneId`, or null - the
    root-scoped laws (direction, depth cap, auto-merge, the sessions lock)
    read this instead of edge markers. Pure. */
export function rootPaneOf(tree: DockNode, paneId: string): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.grown && findPane(n.grown, paneId)) return n;
  }
  return null;
}

/** split levels from a grown root's split down to the leaf's parent: a
    leaf directly under the root's split sits at depth 1, nesting adds.
    The tight roots' cap (ruling 3) refuses a split past depth 2. */
export function splitDepthInRoot(split: DockSplit, paneId: string): number {
  for (let i = 0; i < split.children.length; i++) {
    const c = split.children[i]!;
    if (c.id === paneId) return 1;
    if (isSplit(c) && findPane(c, paneId)) {
      return 1 + splitDepthInRoot(c, paneId);
    }
  }
  return 0;
}

/** v0.14.4 ruling 2/3: may `direction` split `paneId` HERE? Sidebars
    stack rows only, main + bottom take both, and the tight roots cap at
    two split levels. Returns the refusal reason, or null when allowed.
    Reads the LIVE tree so a leaf's root is found wherever it sits. Pure. */
export function splitLawRefusal(
  tree: DockNode,
  paneId: string,
  direction: "row" | "column",
): string | null {
  const pane = findPane(tree, paneId);
  if (!pane) return "a pane of the gesture vanished";
  const root = rootPaneOf(tree, paneId);
  if (root) {
    // an inner leaf: the ROOT's law applies (the leaf carries no marker)
    if ((root.edge === "left" || root.edge === "right") && direction !== "column") {
      return "sidebars stack rows only";
    }
    if (root.grown && splitDepthInRoot(root.grown, paneId) >= 2) {
      return "this root caps at two split levels";
    }
    return null;
  }
  // the shell itself (the growth gesture) or a main pane
  if ((pane.edge === "left" || pane.edge === "right") && direction !== "column") {
    return "sidebars stack rows only";
  }
  return null;
}

/** t_67fc1905: a main pane for the bottom-panel era. The root is a
    COLUMN now (the main row rides on the pinned bottom child); a session
    pane must go into the ROW, not beside the whole column. The first
    row split child (the main row) takes the push; a structure that kept
    no row falls back to wrapping the first child. */
export function insertMainPaneIntoRow(tree: DockNode): { tree: DockNode; paneId: string } | null {
  if (!isSplit(tree) || tree.direction !== "column") return null;
  const rowIdx = tree.children.findIndex((c) => isSplit(c) && c.direction === "row");
  if (rowIdx < 0) return null;
  const copy = clone(tree);
  const row = copy.children[rowIdx] as DockSplit;
  row.children.push(makePane(null));
  row.sizes = normalizeSizes(row.children.length, row.sizes);
  return { tree: copy, paneId: (row.children[row.children.length - 1] as DockPane).id };
}

/** the opener's insert: delegates the column-rooted case to the row
    push (the bottom panel must NOT gain a main pane beside the whole
    column); the row-rooted/pane-rooted shapes take the classic path. */
export function insertMainPane(tree: DockNode): { tree: DockNode; paneId: string } {
  const inRow = insertMainPaneIntoRow(tree);
  if (inRow) return inRow;
  const pane = makePane(null);
  if (isPane(tree) || !isSplit(tree) || tree.direction !== "row") {
    // pane root, or a column root: wrap in a row, new pane right of the tree
    return { tree: makeSplit("row", [tree, pane]), paneId: pane.id };
  }
  const copy = clone(tree);
  copy.children.push(pane);
  copy.sizes = normalizeSizes(copy.children.length, copy.sizes);
  return { tree: copy, paneId: pane.id };
}

/** card t_d7fd3ad3: insights changed HOME. Trees persisted while its area
    was "main" (t_e1e61680) still hold a closable insights tab in a main
    pane; the new home is the right shell (t_d7fd3ad3). This lift removes
    every insights tab living OUTSIDE the right shell's pane - the loader's
    restoreInspectorSet then seats it in the right shell on the same boot
    (missing-from-tree path). A insights tab a user parked in another EDGE
    shell stays (free content; only the main-pane stranding is repaired).
    Pure; no-op returns the SAME tree. */
export function rehomeInsightsTab(tree: DockNode): DockNode {
  let stranded = false;
  for (const n of allNodes(tree)) {
    if (
      isPane(n) &&
      n.edge !== "right" &&
      // a leaf inside the RIGHT ROOT is right-hosted too (v0.14.4 m4:
      // tabs stay where they dropped - the root owns the identity)
      rootPaneOf(tree, n.id)?.edge !== "right" &&
      n.tabs.some((t) => t.component === "insights")
    ) {
      stranded = true;
      break;
    }
  }
  if (!stranded) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n) || n.edge === "right") return;
    // a leaf inside the right root keeps its copy (the root hosts it)
    if (rootPaneOf(tree, n.id)?.edge === "right") return;
    const idx = n.tabs.findIndex((t) => t.component === "insights");
    if (idx < 0) return;
    n.tabs = n.tabs.filter((_, i) => i !== idx);
    n.active = clamp(idx < n.active ? n.active - 1 : n.active, 0, n.tabs.length - 1);
  });
}

/** Endo's comment (2026-10-26): logs is bottom-hosted only - the
    secondary-sidebar copy duplicated the bottom pane. Trees persisted
    while logs was dual-host (card t_67fc1905) still seat an
    http_channel tab in the right shell; this lift removes it. The
    bottom shell keeps logs on its own: a bottom pane's tabs are
    unclosable (logs never leaves a live shell), a tree predating the
    shell gets a fresh seeded one from ensureBottomPanel, and
    restoreInspectorSet can never re-seat it right (the registry's
    right set no longer names logs). A lifted copy with no bottom
    pane (closed shell) drops - the reopen grafts a fresh seeded
    shell, logs returns with it. Free-content copies a user dragged
    into non-right panes stay (t_d6b657a7). Pure; no-op returns the
    SAME tree. */
export function rehomeLogsTab(tree: DockNode): DockNode {
  let seated = false;
  for (const n of allNodes(tree)) {
    if (
      isPane(n) &&
      n.edge === "right" &&
      n.tabs.some((t) => t.component === "http_channel")
    ) {
      seated = true;
      break;
    }
  }
  if (!seated) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n) || n.edge !== "right") return;
    if (!n.tabs.some((t) => t.component === "http_channel")) return;
    n.tabs = n.tabs.filter((t) => t.component !== "http_channel");
    n.active = clamp(n.active, 0, Math.max(0, n.tabs.length - 1));
  });
}

/** the pinned shells are fixed chrome: a loaded tree that somehow lost one
    of the two (a pre-marker tree) gets it re-inserted at its edge, and the
    loader's rulings are re-asserted: sessions ALWAYS sits in the left
    shell's bar, and inspector-area singletons missing from the whole tree
    return to the right shell (the default arrangement fallback - settings
    has no menu path). EXCEPTION (card t_aa9dbada): a shell in `closed`
    (the user left it closed - the record persists that) stays out. Pure;
    fully-satisfied tree returns unchanged. */
export function ensurePinnedEdges(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
): DockNode {
  let result = deriveEdgeMarkers(tree, closed);
  if (!closed.includes("sidebar") && !findEdgePane(result, "sidebar")) {
    result = insertRootEdge(result, "before", makePane("sidebar", 240, "left"));
  }
  if (!closed.includes("inspector") && !findEdgePane(result, "inspector")) {
    result = insertRootEdge(result, "after", defaultInspectorPane(result, 240));
  }
  result = forceSessionsLeft(result);
  return restoreInspectorSet(result);
}

/** pre-marker trees were identified by content fingerprint; mint `edge`
    markers from it when one side is unclaimed. Only fires for the FIRST
    content match of an unclaimed side, so a legit main pane hosting
    relocated inspector tabs is never grabbed - and a side the record says
    the user closed is never minted either (a main pane hosting dragged-in
    inspector tabs would otherwise be claimed by the closed shell's
    reappearance, card t_aa9dbada). Idempotent; no-op returns the SAME
    tree. */
export function deriveEdgeMarkers(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
): DockNode {
  const claimed: Record<"left" | "right", boolean> = { left: false, right: false };
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.edge) claimed[n.edge] = true;
  }
  if (claimed.left && claimed.right) return tree;
  const copy = clone(tree);
  let changed = false;
  for (const n of allNodes(copy)) {
    if (!isPane(n) || n.edge) continue;
    if (
      !claimed.left &&
      !closed.includes("sidebar") &&
      n.tabs.some((t) => t.component === "sidebar")
    ) {
      n.edge = "left";
      claimed.left = true;
      changed = true;
    } else if (
      !claimed.right &&
      !closed.includes("inspector") &&
      n.tabs.some((t) => (INSPECTOR_TAB_COMPONENTS as readonly string[]).includes(t.component))
    ) {
      n.edge = "right";
      claimed.right = true;
      changed = true;
    }
  }
  return changed ? copy : tree;
}

/** the loader's lock enforcement (t_d6b657a7 ruling 1): the sessions tab
    ALWAYS restores into the left shell's bar - even if persistence somehow
    says otherwise. Inserted at index 0 when absent, relocated when it
    landed elsewhere. Pure. */
export function forceSessionsLeft(tree: DockNode): DockNode {
  const left = findEdgePane(tree, "sidebar");
  if (!left) return tree; // repair mints it above - no shell, nothing to force
  // v0.14.4 ruling 1/4: a GROWN left root's bar is its leaf hosting
  // sessions (the shell's classic content moved there at growth; the
  // user may park it in any leaf of the root - free content), else the
  // first leaf seeds it. Absent from the root = absent from the shell.
  let host = left;
  if (left.grown) {
    for (const n of allNodes(left.grown)) {
      if (isPane(n) && n.tabs.some((t) => t.component === "sidebar")) {
        return tree; // met: it lives inside the root
      }
    }
    host = firstPaneOf(left.grown) ?? left;
  } else if (left.tabs.some((t) => t.component === "sidebar")) {
    return tree;
  }
  if (!host) return tree;
  const stray = findComponentTab(tree, "sidebar");
  const rootHost = left.grown ? left.id : null;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n)) return;
    if (n.id === host!.id) {
      n.tabs = [{ component: "sidebar" }, ...n.tabs];
      n.active = clamp(n.active + 1, 0, n.tabs.length - 1);
    } else if (stray && n.id === stray.paneId && rootHost !== stray.paneId) {
      const idx = n.tabs.findIndex((t) => t.component === "sidebar");
      if (idx >= 0) {
        n.tabs = n.tabs.filter((_, i) => i !== idx);
        n.active = clamp(idx < n.active ? n.active - 1 : n.active, 0, n.tabs.length - 1);
      }
    }
  });
}

/** the default-arrangement fallback (t_d6b657a7): any inspector-area
    singleton absent from the WHOLE tree (settings has no menu entry;
    unclosable tabs only die with their pane) is restored INTO the right
    shell, appended in the registry's canonical order. Pure. */
export function restoreInspectorSet(tree: DockNode): DockNode {
  const right = findEdgePane(tree, "inspector");
  if (!right) return tree;
  const missing = INSPECTOR_TAB_COMPONENTS.filter((c) => !findComponentTab(tree, c));
  if (missing.length === 0) return tree;
  // v0.14.4 ruling 1: a grown right root seats the restored set in the
  // leaf that already hosts right-area tabs (else its first leaf) - the
  // shell's own bar is empty while grown
  let hostId = right.id;
  if (right.grown) {
    let host: DockPane | null = null;
    for (const n of allNodes(right.grown)) {
      if (
        isPane(n) &&
        n.tabs.some((t) => (INSPECTOR_TAB_COMPONENTS as readonly string[]).includes(t.component))
      ) { host = n; break; }
    }
    host = host ?? firstPaneOf(right.grown);
    if (host) hostId = host.id;
  }
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === hostId) {
      n.tabs = [...n.tabs, ...missing.map((c) => ({ component: c }))];
      n.active = clamp(n.active, 0, n.tabs.length - 1);
    }
  });
}

/** insert `pane` at the root's edge of a row (a pane root or a column
    root wraps into a row, keeping the edge meaningful - the pre-reset
    toggle path's behavior). Private to the reset helpers.

    card t_556ee727: the bottom graft complicates the wrap - the root is
    a COLUMN whose last child is the bottom shell. Wrapping THAT into a
    row puts the reopened sidebar beside the whole column (band included:
    the sidebar takes the right edge next to a squashed half-width dock
    and the band shrinks with it). The sidebar's real home is the FIRST
    child (the row above the band): insert there, wrap that child if it
    is not a row. */
function insertRootEdge(tree: DockNode, side: "before" | "after", pane: DockPane): DockNode {
  if (
    isSplit(tree) && tree.direction === "column" &&
    tree.children.length > 0
  ) {
    const last = tree.children[tree.children.length - 1];
    if (isPane(last) && last.edge === "bottom") {
      const head = insertRootEdge(tree.children[0], side, pane);
      const copy = clone(tree);
      copy.children = [head, ...copy.children.slice(1)];
      return copy;
    }
  }
  if (isPane(tree) || !isSplit(tree) || tree.direction !== "row") {
    const pair = side === "after" ? [tree, pane] : [pane, tree];
    return makeSplit("row", pair);
  }
  const copy = clone(tree);
  if (side === "after") copy.children.push(pane);
  else copy.children.unshift(pane);
  copy.sizes = normalizeSizes(copy.children.length, copy.sizes);
  return copy;
}

/** pre-reset trees persisted the inspector as ONE tab hosting its own
    chip strip; card t_4ae27e0d dissolved the strip - the views are
    real tabs. Expands every legacy "inspector" tab into the inspector
    group (order matters: active lands on files). No-op returns the SAME
    tree. Pure. */
export function expandInspectorTabs(tree: DockNode): DockNode {
  let done = false;
  const copy = clone(tree);
  for (const n of allNodes(copy)) {
    if (!isPane(n)) continue;
    if (!n.tabs.some((t) => (t.component as string) === "inspector")) continue;
    n.tabs = n.tabs.flatMap((t) =>
      (t.component as string) === "inspector"
        ? INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c }))
        : [t],
    );
    n.active = clamp(n.active, 0, Math.max(0, n.tabs.length - 1));
    done = true;
  }
  return done ? copy : tree;
}

/** card t_49d0b308: the inspector's tab set GREW - settings moved out of
    the main-pane class and the kanban board joined it. Trees persisted
    while the inspector held five tabs get them back: every pane hosting
    a right-area tab gains the missing right-area tabs, in the registry's
    canonical order, and a "settings" tab that survived in a MAIN pane
    (its pre-move home) is removed - its only home is the inspector pane
    now. No-op returns the SAME tree. Pure. */
export function upgradeInspectorTabs(tree: DockNode): DockNode {
  const rank = new Map(INSPECTOR_TAB_COMPONENTS.map((c, i) => [c, i]));
  // "settings" alone does NOT make a pane the inspector pane: the
  // pre-move shell parked it in main panes, so a real inspector pane is
  // one hosting any OTHER right-area id
  const isInspectorTab = (c: DockComponentName) =>
    rank.has(c) && c !== "settings";
  let done = false;
  const copy = clone(tree);
  for (const n of allNodes(copy)) {
    if (!isPane(n)) continue;
    const hosts = n.tabs.some((t) => isInspectorTab(t.component));
    const next = n.tabs
      // strip the pre-move settings tab from panes that are NOT the
      // inspector pane; the inspector pane's own copy survives
      .filter((t) => hosts || t.component !== "settings")
      // missing right-area tabs join the pane (the inspector group's
      // tab list is fixed chrome - every held pane holds them all)
      .concat(
        hosts
          ? INSPECTOR_TAB_COMPONENTS
              .filter((c) => !n.tabs.some((t) => t.component === c))
              .map((c) => ({ component: c }))
          : [],
      )
      // canonical order: the ruling's default tab strip
      .sort((a, b) =>
        (rank.get(a.component) ?? INSPECTOR_TAB_COMPONENTS.length) -
        (rank.get(b.component) ?? INSPECTOR_TAB_COMPONENTS.length));
    if (next.length !== n.tabs.length ||
        next.some((t, i) => t !== n.tabs[i])) {
      n.tabs = next;
      n.active = clamp(n.active, 0, Math.max(0, n.tabs.length - 1));
      done = true;
    }
  }
  return done ? copy : tree;
}

// --- the singleton opener (card t_30ee04fb) ---------------------------------
// One contract rule, one pure function: opening a SINGLETON component
// while an instance is alive FOCUSES that instance wherever it lives;
// otherwise it opens (empty pane = becomes its only tab, occupied pane =
// appended + activated - the tab paths). Multi components (chat) never
// come through here: they mint a tab via the app's opener every time.

export type OpenSingletonAction = "created" | "focused" | "noop";

export function openSingleton(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): { tree: DockNode; action: OpenSingletonAction } {
  // locked components have no open path (t_d6b657a7 ruling 1): sessions
  // lives because the loader puts it there, never because a menu opened it
  if (isLockedTab(component)) return { tree, action: "noop" };
  const found = findComponentTab(tree, component);
  if (found) {
    // focus it in place - even when the target pane is this one (a
    // re-click on '+' while the singleton sits in another tab of the
    // same pane still lands on it)
    return { tree: focusTab(tree, found.paneId, found.index), action: "focused" };
  }
  const pane = findPane(tree, paneId);
  if (!pane) return { tree, action: "noop" }; // target vanished - no-op
  // t_d6b657a7: edge shells ACCEPT content now - refilling an empty
  // sidebar through the '+' menu is the ruling's refill path
  const next = pane.tabs.length === 0
    ? setPaneComponent(tree, paneId, component)
    : openTab(tree, paneId, { component });
  return { tree: next, action: "created" };
}

// --- pure tree operations ----------------------------------------------------
// Each returns a NEW tree (deep-cloned inputs are never mutated). The store
// swaps the reactive root, so persistence is a plain deep watch.

function clone<T>(tree: T): T {
  return structuredClone(tree);
}

function cloneTreeWith(tree: DockNode, fn: (node: DockNode) => void): DockNode {
  // no in-place cloning strategy pays off at this size: clone then walk
  const copy = clone(tree);
  for (const n of allNodes(copy)) fn(n);
  return copy;
}

/** insert `pane` as a sibling of target, splitting the target with
    `direction`. side 'after' = right of a row / below a column. */
export function insertPane(
  tree: DockNode,
  targetId: string,
  direction: "row" | "column",
  side: "before" | "after",
  pane: DockPane,
): DockNode {
  if (targetId === tree.id) {
    // root: wrap it in a new split with the new pane beside it
    const pair = side === "after" ? [tree, pane] : [pane, tree];
    return makeSplit(direction, pair);
  }
  let done = false;
  const result = cloneTreeWith(tree, (n) => {
    if (!done && isSplit(n)) {
      const idx = n.children.findIndex((c) => c.id === targetId);
      if (idx >= 0) {
        const target = n.children[idx];
        const pair = side === "after" ? [target, pane] : [pane, target];
        n.children[idx] = makeSplit(direction, pair);
        done = true;
      }
    }
  });
  if (!done) return tree; // target vanished (concurrent close) - no-op
  return result;
}

/** remove a pane by id. Its parent split hoists the lone survivor.
    card t_4ae27e0d refusals: a PINNED pane (it hosts unclosable tabs)
    refuses. Endo ruling 2026-10-25: the LAST MAIN pane refuses too - the
    main area never reaches zero panes, whatever path the request took
    (button, shortcut, API call, stale state). Shells don't count toward
    the census: closing them may still leave no main pane, which the
    store's ensureMainPane repairs on the next open. */
export function removePane(tree: DockNode, paneId: string): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane)) return tree;
  // v0.14.4 ruling 8: a leaf inside a grown root prunes WITHIN the root
  // (prunePane's split walk cannot see grown children); a degenerate
  // un-grow dissolves back to the shell's classic cell. Roots never die.
  if (rootPaneOf(tree, paneId)) return pruneInnerLeaf(tree, paneId);
  if (isLastMainPane(tree, pane)) return tree;
  return prunePane(tree, pane.id);
}

/** the shared prune body: cut `paneId`, hoist a lone survivor, repair
    sizes. Callers enforce the WHO-MAY-GO policy; this is only mechanics. */
function prunePane(tree: DockNode, paneId: string): DockNode {
  if (paneCount(tree) <= 1) return tree; // the never-empty invariant
  const hoist = (n: DockNode): DockNode => {
    if (isSplit(n)) {
      n.children = n.children.map(hoist);
      if (n.children.length === 1) return n.children[0]; // split collapsed
    }
    return n;
  };
  const pruned = clone(tree);
  const cut = (n: DockNode): DockNode => {
    if (isSplit(n)) {
      n.children = n.children
        .filter((c) => !(isPane(c) && c.id === paneId))
        .map(cut);
      if (n.children.length === 1) return n.children[0];
    }
    return n;
  };
  let result: DockNode = cut(pruned);
  result = hoist(result);
  // sizes arrays must track children lengths after any hoist
  const repair = (n: DockNode) => {
    if (isSplit(n)) {
      n.sizes = normalizeSizes(n.children.length, n.sizes);
      for (const c of n.children) repair(c);
    }
  };
  repair(result);
  return result;
}

// --- the edge toggle (card t_47b776ad) ---------------------------------------
// the pane reset made the pinned panes untouchable via the generic
// removePane/closeTab refusals; the menubar toggles get a dedicated,
// tightly-guarded route so hide/reopen survives the reset's invariants.
// card t_d6b657a7: shells are identified by their `edge` MARKER now -
// content is free, so the old tab fingerprint cannot survive.

/** the dedicated pinned shell for `component`: the pane carrying the
    matching edge marker (sessions = "left", inspector = "right",
    card t_67fc1905: bottom panel = "bottom"). */
export function findEdgePane(
  tree: DockNode,
  component: DockEdgeComponent,
): DockPane | null {
  const want = component === "sidebar" ? "left" : component === "inspector" ? "right" : "bottom";
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.edge === want) return n;
  }
  return null;
}

/** the toggle's hide half: the marked shell goes as a whole (whatever
    tabs it hosted are lost with it - unclosable singletons return via the
    loader's default-arrangement fallback on the next boot). Guards mirror
    the fixed shape: the marker found, never-empty held. */
export function hideEdgePane(
  tree: DockNode,
  component: "sidebar" | "inspector",
): DockNode {
  const pane = findEdgePane(tree, component);
  if (!pane) return tree;
  return prunePane(tree, pane.id);
}

// card t_f6d5a771: a hidden edge pane keeps its width. Hiding remembers the
// pane's last fixed px; reopening restores it - the pre-reset drag-close's
// "reopens at its width". Storage is the same store the tree lives in, so
// the memory survives reloads too. Reads clamp through openClamp: a stale
// sub-floor remembered width reopens AT 320.
const EDGE_PX_KEY = "omega.dock-edge-px";
const EDGE_COMPONENTS: readonly DockEdgeComponent[] = ["sidebar", "inspector", "bottom"];
type EdgeComponent = (typeof EDGE_COMPONENTS)[number];

export function rememberEdgePx(
  store: Storage,
  component: EdgeComponent,
  px: number,
): void {
  let mem: Partial<Record<EdgeComponent, number>> = {};
  try {
    mem = JSON.parse(store.getItem(EDGE_PX_KEY) ?? "{}") ?? {};
  } catch {
    mem = {};
  }
  mem[component] = Math.round(Math.max(0, px));
  store.setItem(EDGE_PX_KEY, JSON.stringify(mem));
}

export function recalledEdgePx(
  store: Storage,
  component: EdgeComponent,
): number | null {
  let mem: Partial<Record<EdgeComponent, number>> = {};
  try {
    mem = JSON.parse(store.getItem(EDGE_PX_KEY) ?? "{}") ?? {};
  } catch {
    return null;
  }
  const px = mem[component];
  if (typeof px !== "number" || !Number.isFinite(px)) return null;
  // the sidebars' reopen clamp lifts a stale sub-floor width to 320; the
  // bottom panel's heights are smaller scale - floor at the drag floor
  // (128), never at the sidebar's open clamp
  return component === "bottom" ? Math.max(MIN_PANE_PX, px) : openClamp(px);
}

/** which edge component a pane's marker matches (null: not an edge pane)
    - the divider intents remember widths only for edge panes */
export function edgeComponentOf(pane: DockPane): EdgeComponent | null {
  return pane.edge === "left" ? "sidebar"
    : pane.edge === "right" ? "inspector"
    : pane.edge === "bottom" ? "bottom"
    : null;
}

// --- window-narrow give-way (card t_33f8756c) --------------------------------
// Divider drags already enforce the priority floors per gesture, but a plain
// WINDOW resize skips the math entirely: the fixed sidebars keep their px,
// and the flexible main pane shrinks under its 320px contract floor (or the
// fixed sum overflows the pane-area). The give-way is the resize twin of the
// squeeze chain: the SECONDARY sidebar (inspector) collapses first, then the
// PRIMARY (sessions) - the same page-lifetime hide the menubar toggle does,
// with the width remembered so the toggle reopens the pane at it. A wider
// window never re-reveals (the user reopens by toggle, like a drag-close).

/** the minimum horizontal px a subtree needs at its axis. A row SUMS its
    children (plus the 4px dividers DockDivider renders between them), a
    column takes its WIDEST child, a fixed pane counts its px, a flexible
    pane counts the 320px chat floor (the contract: every flexible pane
    floors there - dragSplitDivider treats them identically). */
export function minDockPx(node: DockNode): number {
  if (isPane(node)) return node.fixed !== null ? node.fixed : CHAT_FLOOR_PX;
  const reqs = node.children.map(minDockPx);
  const dividers = node.direction === "row" ? (reqs.length - 1) * 4 : 0;
  return node.direction === "row"
    ? reqs.reduce((a, b) => a + b, 0) + dividers
    : Math.max(0, ...reqs);
}

/** one give-way step's record: which edge pane collapsed at what width */
export interface NarrowHiddenEntry {
  component: "sidebar" | "inspector";
  /** the collapsed pane's fixed px - the store remembers it via
      rememberEdgePx so the menubar toggle reopens the pane at it */
  px: number;
}

/** pure window-narrow give-way: while `contentPx` cannot seat every fixed
    pane plus the flexible 320 floor, the SECONDARY sidebar (inspector)
    collapses first, then the PRIMARY (sessions). Each hidden entry carries
    the pane's remembered width. When the tree already fits (or only the
    two edge panes are gone and it still doesn't), it stops - user-made
    splits are never pruned by a resize. */
export function narrowGiveWay(
  tree: DockNode,
  contentPx: number,
): { tree: DockNode; hidden: NarrowHiddenEntry[] } {
  const hidden: NarrowHiddenEntry[] = [];
  let t = tree;
  // secondary first (the inspector gives way before the sessions pane)
  for (const comp of ["inspector", "sidebar"] as const) {
    if (minDockPx(t) <= contentPx) break;
    const pane = findEdgePane(t, comp);
    if (!pane) continue;
    hidden.push({ component: comp, px: pane.fixed ?? 0 });
    t = hideEdgePane(t, comp);
  }
  return { tree: t, hidden };
}

/** the toggle's reopen half: the shell comes back at the root's edge -
    same builders ensurePinnedEdges uses, so reopen and boot-repair mint
    IDENTICAL panes. card t_d6b657a7: the reopened right shell mints only
    the inspector tabs NOT already living elsewhere - a relocated
    singleton is never duplicated by a reopen. */
export function revealEdgePane(
  tree: DockNode,
  component: "sidebar" | "inspector",
  side: "before" | "after",
  /** card t_f6d5a771: reopen at the remembered width (the drag-close's
      reopen-at-its-width); absent/invalid falls back to the default */
  fixedPx?: number | null,
): DockNode {
  const fixed = Math.max(PANE_OPEN_MIN, fixedPx ?? EDGE_DEFAULT_PX[component]);
  const pane =
    component === "sidebar"
      ? makePane("sidebar", fixed, "left")
      : defaultInspectorPane(tree, fixed);
  return insertRootEdge(tree, side, pane);
}

/** the fresh right shell: every inspector tab NOT already living somewhere
    else in the tree (t_d6b657a7 - free content never gets duplicated by a
    repair/re-reveal) */
function defaultInspectorPane(tree: DockNode, fixed: number): DockPane {
  return {
    kind: "pane",
    id: nextNodeId(),
    tabs: INSPECTOR_TAB_COMPONENTS
      .filter((c) => !findComponentTab(tree, c))
      .map((c) => ({ component: c })),
    active: 0,
    fixed,
    edge: "right",
  };
}

// --- the bottom panel (card t_67fc1905) ---------------------------------------
// the horizontal pane pinned under the main panel: the ROOT split becomes
// a COLUMN whose last child is the bottom shell (an edge pane like the
// sidebars - pinned, unclosable TO the empty-removal paths, resizable via
// the horizontal divider). It is a TAB HOST (the terminal joins later as
// another tab); its fresh tab strip comes from the dockable registry's
// defaultHome "bottom" (logs today). Since card t_aa9dbada every edge
// pane's closure persists the same way (the record's closed-edge set) -
// reloads restore the visibility the user left, sidebars included.

/** closed-flag persistence (pre card t_aa9dbada): the bottom panel's hide
    (toggle or divider push) survives reloads when this reads "1". The
    closed-edge set inside the layout record owns persistence now; this key
    is a LEGACY MIGRATION INPUT - the boot reads it once (records that
    predate `closedEdges`), then removes it. */
export const BOTTOM_CLOSED_KEY = "omega.dock-bottom-closed";

/** the fresh bottom shell: a fixed-height edge pane seeded from the
    registry's bottom home (the tab strip grows when future domain tabs
    register their defaultHome in the same place). */
export function bottomShellPane(): DockPane {
  return {
    kind: "pane",
    id: nextNodeId(),
    tabs: defaultHostTabs("bottom").map((c) => ({ component: c })),
    active: 0,
    fixed: BOTTOM_DEFAULT_PX,
    edge: "bottom",
  };
}

/** graft the bottom shell in: the tree becomes the column's first child.
    Idempotent only via the caller (ensureBottomPanel); a shell that
    already sits in the tree should not reach here. */
export function ensureBottomShell(tree: DockNode): DockNode {
  return makeSplit("column", [tree, bottomShellPane()]);
}

/** the boot repair's bottom ruling (differs from the sidebars'): the
    shell present -> present; absent AND closed (the record's closed-edge
    set - closure survives reload) -> leave absent; absent AND open ->
    graft it (fresh boots and trees that predate the panel). Pure. */
export function ensureBottomPanel(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
): DockNode {
  if (findEdgePane(tree, "bottom")) return tree;
  if (closed.includes("bottom")) return tree; // closed by choice
  return ensureBottomShell(tree);
}

/** the toggle's hide half (the bottom domain): remember the height, prune
    (like hideEdgePane - the guarded route around removePane's pinned
    refusals; the panel may close COMPLETELY). The closed-edge marking is
    the caller's (createDock owns the set that rides the record). */
export function hideBottomPanel(tree: DockNode, store: Storage): DockNode {
  const pane = findEdgePane(tree, "bottom");
  if (!pane) return tree;
  if (pane.fixed !== null) rememberEdgePx(store, "bottom", pane.fixed);
  return prunePane(tree, pane.id);
}

/** the toggle's reopen half: graft the shell back at the remembered
    height (absent/invalid falls to the default 200). */
export function revealBottomPanel(tree: DockNode, store: Storage): DockNode {
  const shell = bottomShellPane();
  const restored = recalledEdgePx(store, "bottom");
  shell.fixed = Math.max(MIN_PANE_PX, restored ?? BOTTOM_DEFAULT_PX);
  return makeSplit("column", [tree, shell]);
}

/** move a pane next to a target pane (card 2's DnD will call this). Pure
    remove-then-insert: `position` is relative to the target's position. */
export function movePane(
  tree: DockNode,
  paneId: string,
  targetId: string,
  position: "before" | "after",
): DockNode {
  if (paneId === targetId) return tree;
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane) || !findPane(tree, targetId)) return tree;
  // detach must not collapse while the source still anchors the insert:
  // locate the target BEFORE removing (ids survive the reclone)
  const without = removePane(tree, paneId);
  // Endo ruling 2026-10-25: a refused detach (the last main pane never
  // goes) must not fall through to the insert - the pane never left, so
  // inserting its clone would duplicate it beside itself
  if (without === tree) return tree;
  if (!findPane(without, targetId)) return without; // target WAS the source
  const moving = clone(pane);
  return insertPane(without, targetId, "row", position, moving);
}

/** active-tab focus */
export function focusTab(tree: DockNode, paneId: string, index: number): DockNode {
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.active = Math.max(0, Math.min(index, n.tabs.length - 1));
    }
  });
}

/** give an EMPTY pane its first component (the '+' menu path), replacing
    tabs wholesale. Emptying back is closeTab's job (card 2). card
    t_d6b657a7: edge shells accept content (the empty-shell refill); the
    only refusal left is the LOCKED sessions tab - it never enters a pane
    this way, and unknown ids refuse (the safe class). */
export function setPaneComponent(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isLockedTab(component)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [{ component }];
      n.active = 0;
    }
  });
}

/** open a new tab in a pane, appending at the end and activating it
    (the tab-bar '+' menu path; works on an empty pane too - the first
    tab revives it). Refusal (card t_d6b657a7): the LOCKED sessions tab
    opens nowhere - the loader owns it. Any pane hosts any other tab:
    sidebars are free content now. */
export function openTab(tree: DockNode, paneId: string, tab: DockTab): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isLockedTab(tab.component)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [...n.tabs, tab];
      n.active = n.tabs.length - 1;
    }
  });
}

/** card 2: close a tab; the last one empties the pane (menu reappears).
    card t_4ae27e0d: an UNCLOSEABLE tab refuses - the pinned sidebars'
    tabs have no X at all. Endo ruling 2026-10-25, corrected t_b620f99d:
    the last-main-pane law protects the PANE (removePane), not the tabs -
    a close request against the last main pane's closable tabs goes
    through; emptying the pane's last tab leaves the pane alive in place
    as the empty launcher (it never leaves the tree here), so the law
    still holds and the main area never reaches zero panes. */
export function closeTab(tree: DockNode, paneId: string, index: number): DockNode {
  const pane = findPane(tree, paneId);
  const tab = pane?.tabs[index];
  if (!pane || !tab || !isClosableTab(tab.component)) return tree;
  const emptied = cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      if (n.tabs.length <= index) return;
      n.tabs = n.tabs.filter((_, i) => i !== index);
      n.active = Math.max(0, Math.min(n.active > index ? n.active - 1 : n.active, n.tabs.length - 1));
    }
  });
  // v0.14.4 ruling 8: closing the last tab of an inner leaf MERGES the
  // leaf away (the grown root unwinds; its last leaf falls back to the
  // shell's classic single cell). Main panes keep the launcher state
  // (Endo ruling 2026-10-25); pinned roots never die.
  const closedPane = findPane(emptied, paneId);
  if (closedPane && closedPane.tabs.length === 0 && rootPaneOf(emptied, paneId)) {
    return pruneInnerLeaf(emptied, paneId);
  }
  return emptied;
}

/** card 2 DnD: move a tab between positions/panes. `toIndex` is the
    insertion index into the TARGET pane's tabs AFTER the tab has been
    lifted out of the source (for a same-pane reorder, the callers pass
    the post-removal index - clamped to the remaining range). card
    t_aa9dbada: when a cross-pane move takes the source pane's LAST tab,
    the source pane closes with it (same result as the manual remove
    button) - a non-pinned main pane never lingers empty from a move.
    Pinned shells stay empty-but-present (the shells may sit empty; the
    never-empty law keeps the last pane alive via prunePane's guard). VS
    Code semantics: the moved tab FOCUSES in the target pane; the source
    focuses whatever neighbor remains. card t_d6b657a7: ANY tab moves
    between ANY bars (sidebars <-> main, bidirectional) except the LOCKED
    sessions tab, which may only drop inside the left shell's own bar
    (reorder there, refuse everywhere else). */
export function moveTab(
  tree: DockNode,
  fromPaneId: string,
  fromIndex: number,
  toPaneId: string,
  toIndex: number,
): DockNode {
  const from = findPane(tree, fromPaneId);
  if (!from || fromIndex < 0 || fromIndex >= from.tabs.length) return tree;
  const to = findPane(tree, toPaneId);
  if (!to) return tree;
  const tab = from.tabs[fromIndex]!;
  // the locked tab's hit-test: sessions refuses every drop target that is
  // not inside the left root's own area (reorder there, refuse everywhere
  // else - the grown left root's leaves count as its bar, v0.14.4 ruling 4)
  if (isLockedTab(tab.component) && lockedBarRefusal(tree, toPaneId)) return tree;
  if (fromPaneId === toPaneId) {
    // reorder: lift the tab out, then insert within what remains
    const target = clamp(toIndex, 0, from.tabs.length - 1);
    if (target === fromIndex) return tree;
    return cloneTreeWith(tree, (n) => {
      if (isPane(n) && n.id === fromPaneId) {
        const rest = n.tabs.filter((_, i) => i !== fromIndex);
        rest.splice(target, 0, tab);
        n.tabs = rest;
        if (n.active === fromIndex) n.active = target;
        else if (fromIndex < n.active && target >= n.active) n.active -= 1;
        else if (target <= n.active && fromIndex > n.active) n.active += 1;
      }
    });
  }
  const result = cloneTreeWith(tree, (n) => {
    if (!isPane(n)) return;
    if (n.id === fromPaneId) {
      n.tabs = n.tabs.filter((_, i) => i !== fromIndex);
      // the moved tab was active: its neighbor takes over (VS Code's
      // editor-group behavior); other positions just shift
      if (n.active === fromIndex)
        n.active = Math.min(n.active, Math.max(0, n.tabs.length - 1));
      else if (n.active > fromIndex) n.active -= 1;
    } else if (n.id === toPaneId) {
      const at = clamp(toIndex, 0, n.tabs.length);
      n.tabs = [...n.tabs.slice(0, at), tab, ...n.tabs.slice(at)];
      n.active = at;
    }
  });
  // card t_aa9dbada: the last tab out closes the source pane (the manual
  // remove button's result, automatic) - a main pane never lingers empty
  // from a move. The guards live in the law itself: pinned shells stay
  // (they may sit empty), the last remaining pane stays (prunePane's
  // never-empty guard). Endo ruling 2026-10-25: the LAST MAIN pane is
  // the exception - it survives its own last tab walking out, sitting
  // empty as the launcher (the empty pane IS a main pane state).
  if (result === tree) return tree;
  const src = findPane(result, fromPaneId);
  if (src && !isPinnedPane(src) && src.tabs.length === 0) {
    // v0.14.4 ruling 8: an inner leaf that emptied MERGES AWAY (the grown
    // root unwinds) - no launcher states inside a root
    if (rootPaneOf(result, fromPaneId)) return pruneInnerLeaf(result, fromPaneId);
    if (!isLastMainPane(result, src)) return prunePane(result, fromPaneId);
  }
  return result;
}

/** split a pane into `direction` (row = side by side, column = stacked).
    The NEW pane starts empty - the '+' component menu rules it (settled
    design); VS Code duplicates only once tabs (card 2) exist to duplicate. */
export function splitPane(
  tree: DockNode,
  paneId: string,
  direction: "row" | "column",
  side: "before" | "after",
): DockNode {
  return insertPane(tree, paneId, direction, side, makePane(null));
}

/** v0.14.4 ruling 1: grow the pinned root `shellId` a LOCAL split in
    `direction` (the shell's own body drop). The shell's classic content
    (tabs + active) moves into the first leaf - the shell keeps marker,
    fixed px and contract and empties its bar - and the fresh leaf takes
    the zone's side. Returns the new tree plus BOTH leaf ids: the dragged
    tab seats in freshPaneId, the shell's content in shellLeafId. Null for
    any shell that already grown or a vanished/gone target (the caller
    reports the refusal). Pure. */
export function growRoot(
  tree: DockNode,
  shellId: string,
  direction: "row" | "column",
  side: "before" | "after",
): { tree: DockNode; shellLeafId: string; freshPaneId: string } | null {
  const shell = findPane(tree, shellId);
  if (!shell || !isPinnedPane(shell) || shell.grown) return null;
  const shellLeaf: DockPane = {
    kind: "pane", id: nextNodeId(), tabs: shell.tabs, active: shell.active, fixed: null,
  };
  const fresh = makePane(null);
  const pair = side === "after" ? [shellLeaf, fresh] : [fresh, shellLeaf];
  const grown = makeSplit(direction, pair);
  const next = cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === shellId) {
      n.tabs = [];
      n.active = 0;
      n.grown = grown;
    }
  });
  return { tree: next, shellLeafId: shellLeaf.id, freshPaneId: fresh.id };
}

/** v0.14.4 ruling 8: prune an inner leaf from its grown root - cut + hoist
    INSIDE the grown split, then dissolve a degenerate one: a single
    survivor un-grows (its classic content returns to the shell). Pure;
    no-op (same tree) when the leaf lives nowhere. */
export function pruneInnerLeaf(tree: DockNode, leafId: string): DockNode {
  const root = rootPaneOf(tree, leafId);
  if (!root || !root.grown) return tree;
  const cut = (n: DockSplit): DockNode => {
    const children: DockNode[] = [];
    for (const c of n.children) {
      if (isPane(c) && c.id === leafId) continue;
      children.push(isSplit(c) ? cut(c) : c);
    }
    // degenerate: a corrupt/one-leaf state loses its survivor wholesale -
    // the root un-grows empty rather than crashing (roots never die)
    if (children.length === 0) {
      return { kind: "pane", id: nextNodeId(), tabs: [], active: 0, fixed: null };
    }
    if (children.length === 1) return children[0]!;
    return {
      ...n, children,
      sizes: normalizeSizes(children.length, n.sizes),
    };
  };
  const next = cut(clone(root.grown));
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === root.id) {
      if (isPane(next)) {
        // the root's LAST leaf went: fall back to the classic single cell
        // (ruling 8 - the survivor's content returns, empty or not)
        n.grown = undefined;
        n.tabs = next.tabs;
        n.active = next.active;
      } else {
        n.grown = next;
      }
    }
  });
}

/** v0.14.4 ruling 5 (the load-bearing law): ONE pure formula behind BOTH
    the commit and the ghost preview. Returns the resulting tree plus the
    id of the fresh split pane (null for a center group, which mints no
    pane), or a refusal with its reason - the ghost renders hatched-red
    from the reason, the store commits from the tree. The center zone
    keeps the existing body-drop semantics (move to the end of the
    target's bar); left/right/top/bottom split the target with the zone's
    direction+side and the dragged tab lands IN the new half-pane (VS Code
    editor-group docking: drop left -> the target splits, the dragged tab
    takes the left half). Refusals: zones never self-dock (body drops of a
    pane's own tab keep the focus-only read), a vanished pane mid-gesture,
    the shells on a split zone (their fixed shape), and the LOCKED tab -
    it never leaves the left shell and never lands in a fresh pane. */
export type DockDropPrediction =
  | { tree: DockNode; freshPaneId: string | null }
  | { refused: true; reason: string };

/** v0.14.4 ruling 1/4 + the sessions lock: may the LOCKED tab (sessions)
    target `toPaneId`? Its bar is the left shell's own pane AND any leaf
    inside the left root's local tree - "the left shell's own bar" grew
    with the root. Pure. */
export function lockedBarRefusal(tree: DockNode, toPaneId: string): boolean {
  const to = findPane(tree, toPaneId);
  if (!to) return true;
  if (to.edge === "left") return false;
  const left = findEdgePane(tree, "sidebar");
  return !(left?.grown && !!findPane(left.grown, toPaneId));
}

export function predictDockDrop(
  tree: DockNode,
  fromPaneId: string,
  fromIndex: number,
  toPaneId: string,
  zone: DragZone,
): DockDropPrediction {
  if (fromPaneId === toPaneId) return { refused: true, reason: "zones never self-dock" };
  const from = findPane(tree, fromPaneId);
  const to = findPane(tree, toPaneId);
  if (!from || !to || !from.tabs[fromIndex]) {
    return { refused: true, reason: "a pane of the gesture vanished" };
  }
  const locked = isLockedTab(from.tabs[fromIndex]!.component);
  const split = zoneSplitOf(zone);
  if (!split) {
    // center: the moveTab locked-tab law owns the refusal (sessions only
    // ever lands back in the left shell) - report it before the call so
    // the ghost's refusal is visible without executing the commit
    if (locked && lockedBarRefusal(tree, toPaneId)) {
      return { refused: true, reason: "the locked tab never leaves the left root" };
    }
    return { tree: moveTab(tree, fromPaneId, fromIndex, toPaneId, to.tabs.length), freshPaneId: null };
  }
  // the LOCKED tab can never land in a fresh split pane (it is a main
  // pane, not a left-root leaf) - refuse BEFORE splitting, so a refused
  // release never reshapes the tree around a landing that never happens
  if (locked) return { refused: true, reason: "the locked tab never lands in a fresh pane" };
  // v0.14.4 ruling 1-3: the direction/depth law gates BOTH the shell's
  // growth and an inner leaf's split - a refusal renders hatched-red (the
  // ghost reads this same call), the commit no-ops on it
  const law = splitLawRefusal(tree, toPaneId, split.direction);
  if (law) return { refused: true, reason: law };
  if (isPinnedPane(to)) {
    // the SHELL's own body drop GROWS the root (its tabs move into the
    // first leaf, the fresh leaf takes the zone's side)
    const g = growRoot(tree, toPaneId, split.direction, split.side);
    if (!g) return { refused: true, reason: "the shells keep their shape" };
    return {
      tree: moveTab(g.tree, fromPaneId, fromIndex, g.freshPaneId, 0),
      freshPaneId: g.freshPaneId,
    };
  }
  // split the target (an inner leaf or a main pane), then seat the tab: the
  // fresh pane is the node the insertion minted (id diff - insertPane mints its own ids)
  const before = new Set<string>();
  for (const n of allNodes(tree)) if (isPane(n)) before.add(n.id);
  const after = insertPane(tree, toPaneId, split.direction, split.side, makePane(null));
  if (after === tree) return { refused: true, reason: "the target vanished mid-gesture" };
  let freshId: string | null = null;
  for (const n of allNodes(after)) {
    if (isPane(n) && !before.has(n.id)) {
      freshId = n.id;
      break;
    }
  }
  if (!freshId) return { tree: after, freshPaneId: null };
  return { tree: moveTab(after, fromPaneId, fromIndex, freshId, 0), freshPaneId: freshId };
}

/** the commit path: a thin read of the predictor (v0.14.4 ruling 5 -
    never a second formula). Any refusal is the tree unchanged, exactly
    as before the predictor existed. moveTab's own locked-tab law still
    guards every landing (sessions dock-drops nowhere but left). */
export function dockDropToZone(
  tree: DockNode,
  fromPaneId: string,
  fromIndex: number,
  toPaneId: string,
  zone: DragZone,
): DockNode {
  const p = predictDockDrop(tree, fromPaneId, fromIndex, toPaneId, zone);
  return "refused" in p ? tree : p.tree;
}

// --- the content viewer (card t_36ceb99b) -----------------------------------
// A viewer tab is a MULTI component like chat, but its identity is its
// CONTENT: the file path or skill name it was minted for. Focus-reuse is
// the law (the chat-precedent singleton feel without limiting tabs):
// opening the same file/skill twice FOCUSES the existing tab wherever it
// lives and never mints a duplicate. A different ref mints a fresh tab.

/** a viewer ref's equality: kind + name, undefined matches undefined */
export function viewerRefEq(a?: ViewerRef, b?: ViewerRef): boolean {
  if (a === undefined || b === undefined) return a === b;
  return a.kind === b.kind && a.name === b.name;
}

/** the FIRST tab in the tree showing `v` (active or not - like the
    singleton finders, the identity read must see parked tabs). */
export function findViewerTab(
  tree: DockNode,
  v: ViewerRef,
): ComponentTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex(
      (t) => t.component === "viewer" && viewerRefEq(t.viewer, v),
    );
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

/** the viewer opener's pure half: a tab already showing `v` focuses
    wherever it lives; otherwise one mints into `paneId` (the caller's
    ensureMainPane target - the same main pane a session would open in).
    Returns the focused/minted location. */
export function openViewerTab(
  tree: DockNode,
  v: ViewerRef,
  paneId: string,
): { tree: DockNode; paneId: string; index: number; action: "created" | "focused" } {
  const found = findViewerTab(tree, v);
  if (found) {
    return {
      tree: focusTab(tree, found.paneId, found.index),
      paneId: found.paneId,
      index: found.index,
      action: "focused",
    };
  }
  const next = openTab(tree, paneId, { component: "viewer", viewer: v });
  const pane = findPane(next, paneId);
  const index = pane ? pane.tabs.length - 1 : 0;
  return { tree: next, paneId, index, action: "created" };
}

// --- chat tab session binding (card t_4f0e88f2) -------------------------------

export interface ChatTabLocation {
  paneId: string;
  index: number;
  tab: DockTab;
}

/** locate a chat tab by its instance key (the host's state key). */
export function findChatTab(tree: DockNode, key: string): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex((t) => t.component === "chat" && t.key === key);
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

/** the first chat tab in the tree, depth-first (boot/restore + fallback
    focus helpers use it). */
export function firstChatTab(
  tree: DockNode,
  sessionRef?: string,
): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex(
      (t) => t.component === "chat" && (sessionRef === undefined || t.sessionRef === sessionRef),
    );
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

/** the first DRAFT chat tab (no session binding - the untitled new-chat
    state) in the tree, depth-first. A tab whose first send landed its
    real session id (setTabSessionRef) has sessionRef set and is no
    longer draft. */
export function firstDraftChatTab(tree: DockNode): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex((t) => t.component === "chat" && !t.sessionRef);
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

/** write the session id back onto the chat tab that owns `key` - the
    unsaved ('+' / sidebar-new) tab's first send mints its real id and the
    tab's binding lands IN THE TREE, not in some side variable. */
export function setTabSessionRef(
  tree: DockNode,
  key: string,
  sessionRef: string | null,
): DockNode {
  if (!findChatTab(tree, key)) return tree; // vanished (concurrent close) - no-op
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n)) return;
    const idx = n.tabs.findIndex((t) => t.component === "chat" && t.key === key);
    if (idx < 0) return;
    const tab = n.tabs[idx]!;
    const next: DockTab =
      sessionRef ? { component: "chat", sessionRef, key }
        : { component: "chat", key: tab.key! };
    const tabs = [...n.tabs];
    tabs[idx] = next;
    n.tabs = tabs;
  });
}

/** close EVERY chat tab bound to a session (the delete flow): the tab
    bar's close math (active adjustment, empty pane -> menu) per pane. */
export function closeSessionTabs(tree: DockNode, sessionId: string): DockNode {
  if (firstChatTab(tree, sessionId) === null) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n)) return;
    const drop = new Set<number>();
    n.tabs.forEach((t, i) => {
      if (t.component === "chat" && t.sessionRef === sessionId) drop.add(i);
    });
    if (drop.size === 0) return;
    const tabs = n.tabs.filter((_, i) => !drop.has(i));
    const before = [...drop].filter((i) => i < n.active).length;
    n.tabs = tabs;
    n.active = clamp(n.active - before, 0, tabs.length - 1);
  });
}

// --- resize ------------------------------------------------------------------

export interface DividerDragResult {
  split: DockSplit;
  /** the controlled pane dragged PAST its min at this divider - a hide
      intent (the pane model's close: prune + the menubar toggle reopens
      it). The pure math cannot prune (it only sees the split); the store
      applies the intent against the tree. */
  collapsedPaneId: string | null;
  /** a fixed pane the squeeze chain starved PAST its 128 floor - close
      intent (push-back restored, card t_f6d5a771). Applied by the store. */
  pushedPaneId: string | null;
}

/** pure divider drag: between children [i-1, i] of `split`. startPx carries
    the RENDERED px of every child at drag start (measurement is the
    component's job). deltaPx + means right/down. Applies the 128px drag
    floor, the 320px floor against the absorbing neighbor, and the +320
    snap ladder (snapCandidate) on the controlled pane.
    - left child fixed: drag controls the LEFT pane (grows/shrinks over the
      right neighbor, which flexes/floors as measured).
    - left flexible, right fixed: invert - the RIGHT pane is controlled
      (mirrors the old invert=true divider), so the magnet reads right px.
    - both flexible: fractions shift relative to the content px box.
    Squeeze chain (push-back): when the drag wants more room than the
    floors leave, the nearest FIXED pane beyond the absorbing neighbor
    gives it up - shrinking to its 128 floor, then (starved past it)
    reported as a close intent. Flexible children are walls: the chat
    keeps its 320 floor and never closes by push. */
export function dragSplitDivider(
  split: DockSplit,
  dividerIndex: number,
  startPx: number[],
  deltaPx: number,
  contentPx: number,
): DividerDragResult {
  const none = { split, collapsedPaneId: null, pushedPaneId: null };
  const i = dividerIndex; // divider sits left of child i
  const li = i - 1;
  const left = split.children[li];
  const right = split.children[i];
  if (!left || !right || li < 0) return none;

  const next = structuredClone(split);

  // everything NOT adjacent to this divider (px at drag start) never moves
  const othersPx = startPx.reduce((a, p, k) => (k === li || k === i ? a : a + p), 0);
  const room = Math.max(0, contentPx - othersPx);

  const capFor = (floorOther: number) => Math.max(MIN_PANE_PX, room - floorOther);
  const applySnap = (px: number, lo: number, hi: number, step: number = SNAP_STEP): number => {
    // attractor first, then the floors again: a snap point that overshoots
    // the cap lands ON the cap, never past it
    const snapped = snapCandidateAt(px, step);
    return clamp(snapped ?? px, lo, hi);
  };
  const pxOf = (k: number): number => startPx[k]!;
  const isPaneNode = (k: number): boolean =>
    isPane(next.children[k]!);

  // the squeeze chain's helper: the FIRST fixed pane beyond the absorbing
  // neighbor (scan toward the far edge of the split; the first non-fixed
  // child is a wall - the chain stops there)
  const nearestFixedBeyond = (from: number, step: 1 | -1): number | null => {
    for (let k = from; k >= 0 && k < next.children.length; k += step) {
      const c = next.children[k]!;
      if (isPane(c) && c.fixed !== null) return k;
      return null;
    }
    return null;
  };

  // push-back: px wants more than cap allows. The nearest fixed pane beyond
  // the neighbor gives up `over`: first shrinking toward its 128 floor (the
  // effective cap grows by what it gave up), and - starved past the floor -
  // reported as a CLOSE intent (the freed px re-enters as real layout on
  // the gesture's next re-measured step). */
  const squeeze = (px: number, cap: number, from: number, step: 1 | -1) => {
    if (px <= cap) return { px, cap, pushed: null as string | null };
    const over = px - cap;
    const j = nearestFixedBeyond(from, step);
    if (j === null || !isPaneNode(j)) return { px: Math.min(px, cap), cap, pushed: null };
    const pane = next.children[j] as DockPane;
    const slack = pxOf(j) - MIN_PANE_PX; // room before its own floor closes it
    if (over <= slack) {
      pane.fixed = pxOf(j) - over; // shrinks live, stays open
      return { px, cap: cap + over, pushed: null };
    }
    return { px: Math.min(px, cap), pushed: pane.id, cap };
  };

  const lp = left as DockPane;
  const rp = right as DockPane;
  if (isPane(lp) && lp.fixed !== null) {
    const req = pxOf(li) + deltaPx;
    // drag-past-min: the controlled pane closes - a hide, not an unpin
    // (the menubar toggle reopens it); the old pre-dock divider behavior
    if (req < MIN_PANE_PX) {
      return { split: next, collapsedPaneId: lp.id, pushedPaneId: null };
    }
    // drag controls the LEFT pane; right absorbs: its floor is the
    // resolver's (128 for fixed, 320 for a flexible pane with no contract)
    const hi = capFor(contractFloorOf(rp));
    const s = squeeze(req, hi, i + 1, 1);
    (next.children[li] as DockPane).fixed = applySnap(s.px, MIN_PANE_PX, s.cap);
    return { split: next, collapsedPaneId: null, pushedPaneId: s.pushed };
  }
  if (isPane(rp) && rp.fixed !== null) {
    // t_67fc1905 BOTTOM EDGE (sign fixed t_be305778): the bottom shell is
    // anchored to the viewport's BOTTOM - the divider is its TOP edge
    // (y = contentPx - fixed), so drag DOWN (+delta) SHRINKS it and drag
    // up grows it: the same mirror read as a row's fixed right pane
    // (req = px - delta). The old left-child read (req = px + delta)
    // assumed the divider rides under the pane's growth - inverted the
    // gesture: drag up moved the divider down. Snap ladder: the bottom's
    // +200 (panes.ts).
    if (split.direction === "column") {
      const req = pxOf(i) - deltaPx;
      if (req < MIN_PANE_PX) {
        return { split: next, collapsedPaneId: rp.id, pushedPaneId: null };
      }
      // the absorbing neighbor above: the resolver's floor (128 fixed,
      // 320 flexible). Endo's comment (2026-10-26): the growth cap
      // ALSO stops at the max share - a swallowed window strands the
      // divider offscreen (bottom-anchored geometry), ungrabbable
      const hi = Math.min(
        capFor(contractFloorOf(lp)),
        Math.max(MIN_PANE_PX, Math.floor(contentPx * BOTTOM_MAX_FRACTION)),
      );
      const s = squeeze(req, hi, li - 1, -1);
      (next.children[i] as DockPane).fixed = applySnap(s.px, MIN_PANE_PX, s.cap, BOTTOM_SNAP_STEP);
      return { split: next, collapsedPaneId: null, pushedPaneId: s.pushed };
    }
    const req = pxOf(i) - deltaPx;
    if (req < MIN_PANE_PX) {
      return { split: next, collapsedPaneId: rp.id, pushedPaneId: null };
    }
    // mirror (the old invert=true divider): dragging left widens the fixed
    // right pane; the flexible left flexes at the resolver's floor
    const hi = capFor(contractFloorOf(lp));
    const s = squeeze(req, hi, li - 1, -1);
    (next.children[i] as DockPane).fixed = applySnap(s.px, MIN_PANE_PX, s.cap);
    return { split: next, collapsedPaneId: null, pushedPaneId: s.pushed };
  }

  // both flexible: shift BOTH fractions against the content box (renormalizing
  // would rescale the untouched siblings); snap sticks the left pane to an
  // exact px attractor like the fixed path so any divider feels identical
  const content = Math.max(1, room);
  const pxLo = MIN_PANE_PX;
  const pxHi = Math.max(MIN_PANE_PX, room - contractFloorOf(rp));
  const finalLeft = applySnap(clamp(pxOf(li) + deltaPx, pxLo, pxHi), pxLo, pxHi);
  const shift = (finalLeft - pxOf(li)) / content;
  next.sizes = next.sizes.map((s, k) =>
    k === li ? s + shift : k === i ? s - shift : s,
  );
  return { split: next, collapsedPaneId: null, pushedPaneId: null };
}

export function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

// snapCandidate lives in panes.ts (imported at the top); dock.ts must not
// re-derive the constants (single source per the card).

// --- persistence ---------------------------------------------------------------

// card t_d6b657a7: v2 - shell panes carry the `edge` marker; the layout
// stores WHICH TABS live in WHICH pane (free content, restored verbatim
// on reload). v1 (= pre-card, content-fingerprint sidebars) deserializes
// and migrates in the loader. card t_aa9dbada: the record also carries the
// closed-edge set (`closedEdges` - which pinned shells the user left
// closed); a reload restores THAT state, the loader stops re-asserting an
// open shape over it. The field is optional (older records lack it) and v2
// readers that predate it ignore it - no version bump, no compat shim.
const LAYOUT_VERSION = 3;
/** v:2 -> v:3 migration container: persisted fractions convert to integer
    px against this NOMINAL container (a constant, not the live viewport,
    so the migration is deterministic offline; the render derives
    fractions from px again and lands the same ratios). */
const V2_NOMINAL_CONTAINER_PX = 1000;

/** the closed-edge flags on a layout record: undefined when the record
    predates the field (the legacy bottom-flag migration reads that), a
    validated array otherwise (unknown entries filter out) */
function validateClosedEdges(raw: unknown): DockEdgeComponent[] | undefined {
  if (raw === undefined) return undefined;
  if (!Array.isArray(raw)) return [];
  return raw.filter((c): c is DockEdgeComponent =>
    typeof c === "string" && (EDGE_COMPONENTS as readonly string[]).includes(c),
  );
}

/** the px shares one split's children hold against `containerPx` - the
    ONE formula both the v:3 writer and the round-trip gate read (ruling
    6: never two formulas). Fixed panes keep their px; every other child
    (a flexible pane, a nested split) takes round(remaining * frac/sum of
    the flexible fractions) - the render's own share law, so a recorded
    px re-derives the exact fractions it came from.
    Card t_f1898589: the fixed test is a finite-number guard (isFixed:
    typeof number AND Number.isFinite), never an identity check against
    null - a pane carrying fixed === undefined (or NaN) previously
    passed `fixed !== null` and the emit returned the undefined raw,
    JSON turned it into a null px entry, and the strict reader
    rejected the WHOLE record the writer itself produced (layout reset
    on every reload). This function must never emit a non-finite px;
    anything unreadable takes the flexible branch. */
export function splitPxShares(
  split: DockSplit,
  containerPx: number = V2_NOMINAL_CONTAINER_PX,
): number[] {
  const isFixed = (c: DockNode): c is DockPane & { fixed: number } =>
    isPane(c) && typeof c.fixed === "number" && Number.isFinite(c.fixed);
  const fixedSum = split.children.reduce(
    (a, c) => a + (isFixed(c) ? c.fixed : 0),
    0,
  );
  let flexFrac = 0;
  for (let k = 0; k < split.children.length; k++) {
    const c = split.children[k]!;
    if (isFixed(c)) continue;
    flexFrac += split.sizes[k] ?? 1 / split.children.length;
  }
  const remaining = Math.max(0, containerPx - fixedSum);
  return split.children.map((c, k) => {
    if (isFixed(c)) return c.fixed;
    const frac = split.sizes[k] ?? 1 / split.children.length;
    const rel = flexFrac > 0 ? frac / flexFrac : 1 / Math.max(1, split.children.length);
    return Math.max(0, Math.round(remaining * rel));
  });
}

/** deep copy carrying the v:3 record shape: integer px split shares in
    place of fraction sizes (fractions derive at load; impossible widths
    become unrepresentable, ruling 6). Pane nodes pass through verbatim
    (fixed px IS px already). The shape is deliberately the RECORD's
    (split px, no sizes) - JSON.parse reads it back through validateNode. */
type RecordNode =
  | { kind: "split"; id: string; direction: "row" | "column"; children: RecordNode[]; px: number[] }
  | { kind: "pane"; id: string; tabs: DockTab[]; active: number; fixed: number | null; edge?: "left" | "right" | "bottom"; grown?: RecordNode };

function recordTree(tree: DockNode, containerPx: number): RecordNode {
  const walk = (n: DockNode): RecordNode => {
    if (isPane(n)) {
      return {
        kind: "pane", id: n.id, tabs: n.tabs, active: n.active, fixed: n.fixed,
        ...(n.edge ? { edge: n.edge } : {}),
        // v0.14.4 ruling 1: the local split tree rides ITS OWN record with
        // the same px-share law (fractions derive at load from the ratios)
        ...(n.grown ? { grown: walk(n.grown) } : {}),
      };
    }
    return {
      kind: "split",
      id: n.id,
      direction: n.direction,
      children: n.children.map(walk),
      px: splitPxShares(n, containerPx),
    };
  };
  return walk(tree);
}

export function serialize(
  tree: DockNode,
  closedEdges: readonly DockEdgeComponent[] = [],
  containerPx: number = V2_NOMINAL_CONTAINER_PX,
): string {
  return JSON.stringify({ v: LAYOUT_VERSION, tree: recordTree(tree, containerPx), closedEdges });
}

export interface LoadedLayout {
  tree: DockNode;
  /** the persisted version: 1 needs the era migration, 2 loads as-is,
      3 is the px-on-disk shape (sizes derive) */
  version: 1 | 2 | 3;
  /** which pinned shells the record says the user left closed; missing on
      records that predate the field (the legacy bottom flag migrates them) */
  closedEdges?: DockEdgeComponent[];
}

/** validates + parses a persisted layout. Returns null for anything the
    engine cannot safely render (old/foreign/corrupt) so callers migrate.
    The STRICT reader: v1/v2/v3 records that fully validate load; anything
    else stays a null (the total path is parseAnyLayout below). */
export function deserializeLayout(raw: string | null): LoadedLayout | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
  const rec = parsed as { v?: unknown; tree?: unknown; closedEdges?: unknown };
  if (rec.v !== 1 && rec.v !== 2 && rec.v !== 3) return null;
  const tree = validateNode(rec.tree);
  if (!tree) return null;
  const closedEdges = validateClosedEdges(rec.closedEdges);
  return { tree, version: rec.v as 1 | 2 | 3, ...(closedEdges ? { closedEdges } : {}) };
}

/** the plain tree-only read (every caller that does not care about the
    persisted version) */
export function deserialize(raw: string | null): DockNode | null {
  return deserializeLayout(raw)?.tree ?? null;
}

/** the px shares of a RAW split record -> fractions, or null when the
    shares cannot seat the children (the strict reader rejects, the total
    reader repairs) */
function fractionsFromPxShare(raw: unknown, width: number): number[] | null {
  const arr = Array.isArray(raw) ? (raw as unknown[]) : null;
  if (!arr || arr.length !== width) return null;
  const px: number[] = [];
  for (const p of arr) {
    if (typeof p !== "number" || !Number.isFinite(p) || p <= 0) return null;
    px.push(Math.round(p));
  }
  const sum = px.reduce((a, s) => a + s, 0);
  if (sum <= 0) return null;
  return px.map((s) => s / sum);
}

function validateNode(raw: unknown): DockNode | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const n = raw as Record<string, unknown>;
  if (typeof n.id !== "string" || !/^n[0-9a-z]+$/.test(n.id)) return null;
  if (n.kind === "split") {
    if ((n.direction !== "row" && n.direction !== "column") || !Array.isArray(n.children))
      return null;
    const children: DockNode[] = [];
    for (const c of n.children) {
      const v = validateNode(c);
      if (!v) return null;
      children.push(v);
    }
    if (children.length < 2) return null; // splits with one child were hoisted
    // v:3 record: the children's shares ride px (fractions derive); a
    // fraction-sizes shape stays valid for pre-v:3 records
    const derived = fractionsFromPxShare(n.px, children.length);
    const sizes = derived ?? normalizeSizesFromRecord(n.sizes, children.length);
    if (!sizes) return null;
    return { kind: "split", id: n.id, direction: n.direction, children, sizes };
  }
  if (n.kind !== "pane") return null;
  if (!Array.isArray(n.tabs)) return null;
  const tabs: DockTab[] = [];
  for (const t of n.tabs) {
    if (!t || typeof t !== "object") return null;
    const tab = t as { component?: unknown; sessionRef?: unknown; key?: unknown; viewer?: unknown };
    // legacy "inspector" tabs deserialize (pre-reset trees carry them);
    // createDock's modernizer expands them to the inspector group on load
    if (
      typeof tab.component !== "string" ||
      !(
        (DOCK_COMPONENTS as readonly string[]).includes(tab.component) ||
        (LEGACY_DOCK_COMPONENTS as readonly string[]).includes(tab.component)
      )
    )
      return null;
    // card t_36ceb99b: a viewer payload present but unreadable rejects
    // the whole record (the strict reader; it can only be writer bug or
    // hand corruption - the tolerant path repairs instead)
    if (tab.viewer !== undefined && !validViewerRef(tab.viewer)) return null;
    tabs.push(
      typeof tab.sessionRef === "string" && tab.sessionRef
        ? { component: tab.component as DockComponentName, sessionRef: tab.sessionRef }
        : { component: tab.component as DockComponentName },
    );
    if (typeof tab.key === "string" && tab.key) tabs[tabs.length - 1]!.key = tab.key;
    if (validViewerRef(tab.viewer)) tabs[tabs.length - 1]!.viewer = tab.viewer;
  }
  const fixed = typeof n.fixed === "number" && Number.isFinite(n.fixed) && n.fixed >= 0
    ? n.fixed
    : null;
  // card t_d6b657a7: the shell identity persists (v2); card t_67fc1905:
  // "bottom" joins the edge class; anything else is a main pane
  const edge = n.edge === "left" || n.edge === "right" || n.edge === "bottom" ? n.edge : undefined;
  const active = typeof n.active === "number" && Number.isFinite(n.active)
    ? clamp(Math.trunc(n.active), 0, Math.max(0, tabs.length - 1))
    : 0;
  // v0.14.4 ruling 1: the local split tree validates like any node; a
  // grown field that reads as anything but a split rejects the record
  // (the strict reader - the total reader repairs below)
  let grown: DockSplit | undefined;
  if (n.grown !== undefined) {
    const g = validateNode(n.grown);
    if (!g || g.kind !== "split") return null;
    grown = g;
  }
  return {
    kind: "pane", id: n.id, tabs, active, fixed,
    ...(edge ? { edge } : {}),
    ...(grown ? { grown } : {}),
  };
}

/** the strict fraction-sizes read (validateNode's pre-v:3 path): every
    entry finite and positive, normalized; a count mismatch re-normalizes
    to equal shares. Returns null only when an entry is unusable. */
function normalizeSizesFromRecord(raw: unknown, width: number): number[] | null {
  if (!Array.isArray(raw)) return null;
  const sizes: number[] = [];
  for (const s of raw as unknown[]) {
    if (typeof s !== "number" || !Number.isFinite(s) || s <= 0) return null;
    sizes.push(s);
  }
  return normalizeSizes(width, sizes);
}

// --- the TOTAL deserializer (v0.14.4 ruling 6) ---------------------------------

const MAX_LAYOUT_DEPTH = 32;

export interface ParsedLayout {
  tree: DockNode;
  version: 1 | 2 | 3;
  closedEdges?: DockEdgeComponent[];
  /** the repair receipt: one plain phrase per constraint eased. Empty on
      a clean load; surfaces once as a toast (createDock keeps it). */
  repairs: string[];
}

/** consumes ANY JSON shape and hands back a renderable tree - no throw
    path, no null (the caller's `fallback` boots the default arrangement,
    ALWAYS receipted). The repair table: garbage/non-object -> default
    tree; unknown kind -> drop node; split with fewer than 2 surviving
    children -> hoist the survivor; bad/colliding ids -> re-mint;
    non-finite or negative fixed -> null; sub-floor px -> clamp to 128;
    active out of range -> clamp; depth past 32 -> collapse deepest. */
export function parseAnyLayout(raw: string | null, fallback: () => DockNode): ParsedLayout {
  if (!raw) {
    if (raw === null) return { tree: fallback(), version: 2, repairs: [] };
    return { tree: fallback(), version: 2, repairs: ["the record was empty: booted the layout default"] };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { tree: fallback(), version: 2, repairs: ["the record was not valid JSON: booted the layout default"] };
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { tree: fallback(), version: 2, repairs: ["the record was not a layout object: booted the layout default"] };
  }
  const rec = parsed as { v?: unknown; tree?: unknown; closedEdges?: unknown };
  const v = rec.v === 1 || rec.v === 2 || rec.v === 3 ? rec.v : 2;
  const repairs: string[] =
    typeof rec.v === "number" && rec.v !== v
      ? [`unknown layout version: read as v${v}-era`]
      : [];
  const seen = new Set<string>();
  const tree = tolerantNode(rec.tree, repairs, seen, { depth: 0, v });
  if (!tree) {
    repairs.push("no renderable tree in the record: booted the layout default");
    return { tree: fallback(), version: v, repairs };
  }
  const closedEdges = validateClosedEdges(rec.closedEdges);
  return { tree, version: v, ...(closedEdges ? { closedEdges } : {}), repairs };
}

interface TolerantEnv {
  depth: number;
  v: 1 | 2 | 3;
}

function tolerantNode(
  raw: unknown,
  repairs: string[],
  seen: Set<string>,
  env: TolerantEnv,
): DockNode | null {
  if (env.depth > MAX_LAYOUT_DEPTH) {
    repairs.push(`the tree collapsed past depth ${MAX_LAYOUT_DEPTH}`);
    return null;
  }
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    if (env.depth > 0) repairs.push("an unreadable node was dropped");
    return null;
  }
  const n = raw as Record<string, unknown>;
  // ids: well-formed and unique, else re-minted (the repair receipt tells)
  const usableId = typeof n.id === "string" && /^n[0-9a-z]+$/.test(n.id) ? n.id : null;
  const id = usableId && !seen.has(usableId) ? usableId : nextNodeId();
  if (id !== usableId) {
    repairs.push(`a node id (${usableId ?? "unreadable"}) re-minted as ${id}`);
  }
  seen.add(id);

  if (n.kind === "split") {
    if ((n.direction !== "row" && n.direction !== "column") || !Array.isArray(n.children)) {
      repairs.push(`split ${id} had an unreadable shape`);
      return null;
    }
    const children: DockNode[] = [];
    for (const c of n.children as unknown[]) {
      const t = tolerantNode(c, repairs, seen, { depth: env.depth + 1, v: env.v });
      if (t) children.push(t);
    }
    if (children.length === 0) {
      repairs.push(`split ${id} lost every child`);
      return null;
    }
    if (children.length === 1) return children[0]!; // hoist the survivor
    const sizes = tolerantSplitSizes(n, children.length, repairs, env.v);
    return { kind: "split", id, direction: n.direction, children, sizes };
  }
  if (n.kind !== "pane") {
    repairs.push(`a node of kind "${typeof n.kind === "string" ? n.kind : "kindless"}" was dropped`);
    return null;
  }

  const tabs: DockTab[] = [];
  if (Array.isArray(n.tabs)) {
    for (const t of n.tabs as unknown[]) {
      if (!t || typeof t !== "object") {
        repairs.push("an unreadable tab record was dropped");
        continue;
      }
      const tab = t as { component?: unknown; sessionRef?: unknown; key?: unknown; viewer?: unknown };
      if (
        typeof tab.component !== "string" ||
        !(
          (DOCK_COMPONENTS as readonly string[]).includes(tab.component) ||
          (LEGACY_DOCK_COMPONENTS as readonly string[]).includes(tab.component)
        )
      ) {
        repairs.push(`a tab (${typeof tab.component === "string" ? tab.component : "kindless"}) was dropped`);
        continue;
      }
      const nt: DockTab =
        typeof tab.sessionRef === "string" && tab.sessionRef
          ? { component: tab.component as DockComponentName, sessionRef: tab.sessionRef }
          : { component: tab.component as DockComponentName };
      if (typeof tab.key === "string" && tab.key) nt.key = tab.key;
      // card t_36ceb99b: the viewer payload rides the record when it
      // validates; an unreadable one drops with a receipt (the tab is
      // then an empty viewer tab - unreachable, so it is dropped too
      // when the component is viewer: a payloadless viewer tab renders
      // nothing)
      if (validViewerRef(tab.viewer)) nt.viewer = tab.viewer;
      else if (tab.viewer !== undefined) {
        repairs.push("a viewer tab's unreadable payload dropped the tab");
        if ((nt.component as string) === "viewer") continue;
      }
      tabs.push(nt);
    }
  } else {
    repairs.push("a pane's tabs list was unreadable: the pane booted empty");
  }
  let fixed: number | null = null;
  if (typeof n.fixed === "number") {
    if (!Number.isFinite(n.fixed) || n.fixed < 0) {
      repairs.push("a pane's unreadable fixed px was released (the pane stays flexible)");
    } else if (n.fixed < MIN_PANE_PX) {
      fixed = MIN_PANE_PX;
      repairs.push("a pane's sub-floor px clamped to the 128 floor");
    } else {
      fixed = n.fixed;
    }
  } else if (n.fixed !== undefined && n.fixed !== null) {
    repairs.push("a pane's unreadable fixed px was released (the pane stays flexible)");
  }
  let active = 0;
  if (typeof n.active === "number" && Number.isFinite(n.active)) {
    active = clamp(Math.trunc(n.active), 0, Math.max(0, tabs.length - 1));
    if (active !== Math.trunc(n.active)) {
      repairs.push("a pane's out-of-range active tab clamped into range");
    }
  }
  const edge = n.edge === "left" || n.edge === "right" || n.edge === "bottom" ? n.edge : undefined;
  // v0.14.4 ruling 1: the local split tree tolerates like any node; a
  // grown field that will not parse DROPS with a receipt (the shell falls
  // back to its classic single cell - content lives in the leaves, so a
  // dropped tree keeps the pane census honest)
  let grown: DockSplit | undefined;
  if (n.grown !== undefined) {
    const g = tolerantNode(n.grown, repairs, seen, { depth: env.depth + 1, v: env.v });
    if (g && isSplit(g)) grown = g;
    else repairs.push("a shell's local split tree was unreadable: the shell un-grew");
  }
  return {
    kind: "pane", id, tabs, active, fixed,
    ...(edge ? { edge } : {}),
    ...(grown ? { grown } : {}),
  };
}

/** a split's fractions from whatever the record carries: v:3 px shares
    derive fractions (px_i / sum, floor-clamped); fraction sizes
    normalize as before, then v:2 records migrate to px (fraction *
    nominal container, floored at 128) and re-derive - the "impossible"
    widths die here. */
function tolerantSplitSizes(
  n: Record<string, unknown>,
  childCount: number,
  repairs: string[],
  v: 1 | 2 | 3,
): number[] {
  if (Array.isArray(n.px) && (n.px as unknown[]).length === childCount) {
    const px: number[] = [];
    let ok = true;
    for (const entry of n.px as unknown[]) {
      if (typeof entry !== "number" || !Number.isFinite(entry)) {
        repairs.push("a split's px shares were unreadable: fraction fallback");
        ok = false;
        break;
      }
      if (entry < MIN_PANE_PX) {
        repairs.push("a split's px share below the floor clamped to 128");
        px.push(MIN_PANE_PX);
      } else {
        px.push(Math.round(entry));
      }
    }
    if (ok) {
      const sum = px.reduce((a, s) => a + s, 0);
      if (sum > 0) return px.map((s) => s / sum);
      repairs.push("a split's px shares summed to zero: equal shares assumed");
    }
  }
  const raw = Array.isArray(n.sizes) ? (n.sizes as unknown[]) : [];
  const sizes: number[] = [];
  for (let i = 0; i < childCount; i++) {
    const s = raw[i];
    if (typeof s === "number" && Number.isFinite(s) && s > 0) sizes.push(s);
    else {
      sizes.push(1 / childCount);
      repairs.push("a split's unreadable share re-normalized to equal");
    }
  }
  const norm = normalizeSizes(childCount, sizes);
  if (v === 1) return norm;
  // the v:2 px migration: fraction * nominal container -> px, floored,
  // fractions re-derived (the deltas ARE the receipt - impossible widths
  // become unrepresentable)
  const px = norm.map((f) => Math.max(MIN_PANE_PX, Math.round(f * V2_NOMINAL_CONTAINER_PX)));
  const sum = px.reduce((a, s) => a + s, 0);
  const migrated = px.map((p) => p / sum);
  if (migrated.some((f, i) => Math.abs(f - norm[i]!) > 1e-9)) {
    repairs.push(`v2 fraction shares migrated to px (nominal ${V2_NOMINAL_CONTAINER_PX}px, 128 floor)`);
  }
  return migrated;
}

// --- migration (first boot without a dock key) ---------------------------------

function readWidth(get: (k: string) => string | null, key: string): number | null {
  const raw = get(key);
  if (raw === null || raw === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) && n >= MIN_PANE_PX ? n : null;
}

/**
 * Seeds the tree from the pre-dock persisted pane state. card t_4ae27e0d:
 * the shape is FIXED - left sessions pane, chat, right inspector pane with
 * its view tabs - both sidebars always present (the open flags are
 * dead history). A persisted width renders verbatim; unknown width ->
 * the old default 240.
 */
export function migrateLegacy(
  get: (k: string) => string | null,
): DockNode {
  const fixedFor = (key: string): number => {
    const w = readWidth(get, key);
    return w === null ? 240 : w;
  };
  const sw = fixedFor(LEGACY_WIDTH_KEYS.sidebar);
  const iw = fixedFor(LEGACY_WIDTH_KEYS.inspector);

  const inspector: DockPane = {
    kind: "pane",
    id: nextNodeId(),
    tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
    active: 0,
    fixed: iw,
    edge: "right",
  };
  const mainRow: DockNode = {
    kind: "split",
    id: nextNodeId(),
    direction: "row",
    children: [
      makePane("sidebar", sw, "left"),
      makePane("chat", null),
      inspector,
    ],
    sizes: normalizeSizes(3, undefined),
  };
  // card t_67fc1905: the fresh boot wraps the row in a column with the
  // bottom panel pinned under it - logs is its default tab (the registry's
  // bottom home), the inspector still seats logs (dual host) - the main
  // column above owns everything else
  const bottom = bottomShellPane();
  const root: DockSplit = {
    kind: "split",
    id: nextNodeId(),
    direction: "column",
    children: [mainRow, bottom],
    sizes: normalizeSizes(2, undefined),
  };
  return root;
}

// --- store ---------------------------------------------------------------

import { shallowRef, watch, ref, type InjectionKey, type Ref } from "vue";

/** pure graft: replace the split node `replacement` (by id) inside `root`
    and return the NEW root. No id regeneration, no sibling churn - and no
    in-place mutation on the reactive tree (structuredClone cannot touch
    Vue proxies; the tree stays a shallowRef of PLAIN data, re-swapped on
    every op so the deep-watcher only ever sees fresh values). */
export function graftSplit(root: DockNode, replacement: DockSplit): DockNode {
  if (isSplit(root) && root.id === replacement.id) return replacement;
  const copy = clone(root);
  // v0.14.4 m4: a grown root's local splits graft too (its inner dividers
  // drag through the same store route)
  const walk = (n: DockSplit): boolean => {
    for (let i = 0; i < n.children.length; i++) {
      const c = n.children[i]!;
      if (c.id === replacement.id) {
        n.children[i] = clone(replacement);
        return true;
      }
      if (isSplit(c) && walk(c)) return true;
      if (isPane(c) && c.grown && walk(c.grown)) return true;
    }
    return false;
  };
  if (isPane(copy) && copy.grown && walk(copy.grown)) return copy;
  return isSplit(copy) && walk(copy) ? copy : root;
}

/** the round-trip gate's compare (v0.14.4 ruling 6): panes match on
    identity/content, splits on direction + child shapes + px shares
    within one rounding px (the parse re-derives fractions from the
    record's integer px; a re-rounding off by one is NOT drift). Exported
    for the tests that pin the record/live-tree equivalence. */
export function treeShapesMatch(a: DockNode, b: DockNode): boolean {
  if (isPane(a)) {
    if (!isPane(b)) return false;
    const tabEq =
      a.tabs.length === b.tabs.length &&
      a.tabs.every(
        (t, i) =>
          t.component === b.tabs[i]!.component &&
          (t.sessionRef ?? undefined) === (b.tabs[i]!.sessionRef ?? undefined) &&
          (t.key ?? undefined) === (b.tabs[i]!.key ?? undefined) &&
          // card t_36ceb99b: the viewer payload is tab identity - it
          // rides the shape comparison (a dropped payload IS drift)
          viewerRefEq(t.viewer, b.tabs[i]!.viewer),
      );
    return (
      a.id === b.id &&
      a.active === b.active &&
      (a.fixed ?? null) === (b.fixed ?? null) &&
      (a.edge ?? undefined) === (b.edge ?? undefined) &&
      tabEq &&
      // v0.14.4 ruling 1: the local split trees compare through the same
      // walk (structure + ids + px shares at the 1px tolerance)
      (!!a.grown === !!b.grown) &&
      (!a.grown || !b.grown || treeShapesMatch(a.grown, b.grown))
    );
  }
  if (isPane(b)) return false;
  if (a.direction !== b.direction || a.children.length !== b.children.length) return false;
  const pa = splitPxShares(a);
  const pb = splitPxShares(b);
  for (let i = 0; i < a.children.length; i++) {
    if (Math.abs(pa[i]! - pb[i]!) > 1) return false;
    if (!treeShapesMatch(a.children[i]!, b.children[i]!)) return false;
    if (a.children[i]!.id !== b.children[i]!.id) return false;
  }
  return true;
}

export interface DockApi {
  /** the reactive root (swap on every op; deep-watched for persistence) */
  tree: Ref<DockNode>;
  /** whether the component has an instance anywhere in the tree - the
      singleton semantics (card t_30ee04fb): a singleton exists at most
      once, so its presence check reads EVERY tab, not just active ones */
  has: (component: DockComponentName) => boolean;
  /** this boot's repair receipt (v0.14.4 ruling 6) - empty on a clean
      load; the shell surfaces it once as a toast. */
  bootRepairs: readonly string[];
  /** open a component as the pane's single tab (empty-pane menu path) */
  setComponent: (paneId: string, component: DockComponentName) => void;
  /** card 2 tabs: activate a tab within its pane */
  focus: (paneId: string, index: number) => void;
  /** card 2 tabs: open a NEW tab of a component in the pane, activating it */
  openTab: (paneId: string, tab: DockTab) => void;
  /** card 2 tabs: close a tab (last one empties the pane - it stays alive) */
  closeTab: (paneId: string, index: number) => void;
  /** card 2 DnD: move a tab (same-pane reorder or between panes) */
  moveTab: (fromPaneId: string, fromIndex: number, toPaneId: string, toIndex: number) => void;
  /** card t_0e833eae: the docking drop - release a dragged tab on a zone
      of another pane's body: halves split the target around the tab,
      center groups it. Refusals are no-ops (shells don't split, zones
      never self-dock). */
  dockDropToZone: (
    fromPaneId: string,
    fromIndex: number,
    toPaneId: string,
    zone: DragZone,
  ) => void;
  /** v0.14.4 ruling 1/4: the locked tab's LIVE hit-test for a pane - may
      the current locked drag drop into this pane (the left shell's pane +
      the left root's grown leaves)? The panes read it during dragover. */
  lockedBarOk: (paneId: string) => boolean;
  /** v0.14.4 ruling 10: the undo ring - walk back one committed layout op
      (split, merge, move, resize, hide) or return false when empty. */
  undoLayout: () => boolean;
  /** v0.14.4 ruling 10: a RESIZE is one layout op per gesture - the drag
      start snapshots once (per-tick moves land under the same snapshot). */
  beginResize: () => void;
  /** card 4 - the dockable contract opener: singletons FOCUS their one
      instance when it lives (created | focused), multi goes through the
      app's chat opener instead - this must only be called for singletons */
  openSingleton: (paneId: string, component: DockComponentName) => "created" | "focused";
  /** card 3: chat tab binding - write the session id a tab owns. */
  setSessionRef: (key: string, sessionRef: string | null) => void;
  /** card 3: close every chat tab bound to a session (delete flow). */
  closeSession: (sessionRef: string) => void;
  /** card t_36ceb99b: open (or FOCUS) the viewer tab for `v` - a file
      path or skill name already shown anywhere focuses that tab
      (focus-reuse, never a duplicate); otherwise one mints into the
      ensured main pane, exactly the routing a session open takes. */
  openViewer: (v: ViewerRef) => void;
  /** card 3: chat tab lookups (focus helpers; read-only over the live tree) */
  findChatTab: (key: string) => ChatTabLocation | null;
  firstChatTab: (sessionRef?: string) => ChatTabLocation | null;
  /** t_311838e4: the first DRAFT chat tab (no session binding) - the
      '+' new flow focuses it instead of minting a duplicate. */
  firstDraftChatTab: () => ChatTabLocation | null;
  split: (paneId: string, direction: "row" | "column", side: "before" | "after") => void;
  remove: (paneId: string) => void;
  /** card t_47b776ad: the menubar's desktop toggles are back. Hides the
      pinned edge pane (or reopens it at its edge, at the reopen-clamp
      width) - the sanctioned chrome route around removePane/closeTab's
      pinned refusals. The hide is page-lifetime: the next load runs
      ensurePinnedEdges, which re-asserts the fixed shape. */
  toggleEdge: (component: "sidebar" | "inspector", side: "before" | "after") => void;
  /** card t_67fc1905: the bottom panel's menubar toggle - hide reopens at
      the remembered height; closed state SURVIVES reloads (unlike the
      sidebars, whose fixed shape the loader re-asserts). The panel may
      close completely. */
  toggleBottom: () => void;
  /** Endo's comment (2026-10-26): the bottom shell's fullscreen view -
      the tabbar button flips it; the column split renders ONLY the
      shell. Session-only (a reload never boots fullscreen). */
  bottomFullscreen: Ref<boolean>;
  /** flip the fullscreen view (see bottomFullscreen) */
  toggleBottomFullscreen: () => void;
  /** card t_4ae27e0d: the id of the FIRST main pane - the only place a
      session tab may open. The pinned sidebars are skipped; when the tree
      holds no main pane (the user removed the last one) a fresh flexible
      pane is inserted at the root's after-edge and its id returned. */
  ensureMainPane: () => string;
  /** whether the pane's X may remove it: pinned panes never (they host
      unclosable tabs) and the LAST MAIN pane never (Endo ruling
      2026-10-25: the main area never reaches zero panes - the census
      counts MAIN panes only, shells don't help). The UI hides the X
      instead of offering a refused click. */
  canRemovePane: (paneId: string) => boolean;
  /** Endo ruling 2026-10-25: whether `paneId` is a MAIN pane with no
      other main pane beside it. The tab close X renders only while
      this is false - the last main pane's tab carries no close button.
      Pinned panes are never the last main pane (their tabs keep their
      own closable rules). */
  isLastMainPane: (paneId: string) => boolean;
  /** divider drag: measured start widths flow in on the first move event.
      Returns the drag's close intents for the caller to react to (the
      store itself already applies them to the tree). */
  drag: (
    splitId: string,
    dividerIndex: number,
    startPx: number[],
    deltaPx: number,
    contentPx: number,
  ) => { collapsedPaneId: string | null; pushedPaneId: string | null };
  /** card t_33f8756c: the window-narrow give-way. While the content box
      `contentPx` cannot seat every fixed pane plus the flexible 320 floor,
      the secondary sidebar (inspector) collapses first, then the primary
      (sessions); each hide remembers the pane's width via the edge-px
      memory, so the menubar toggle restores exactly what the resize took.
      Page-lifetime like a drag-close - a wider window never re-reveals. */
  narrowSqueeze: (contentPx: number) => void;
}

export const DOCK_API_KEY: InjectionKey<DockApi> = Symbol("omega-dock-api");

// card t_4f0e88f2: chat is multi-instance now. Opening a chat ('+'
// menu, empty-pane "new chat") mints a REAL tab through the app's
// opener - a session-less composer-first chat carries no legacy op.
export const DOCK_CHAT_OPEN_KEY: InjectionKey<
  (paneId: string, sessionId: string | null) => void
> = Symbol("omega-dock-chat-open");
// the app resolves chat tab titles to their session label (sidebar
// highlight follows the FOCUSED tab, so labels must read in the bar);
// null falls back to the generic COMPONENT_TITLES entry
export const DOCK_TAB_TITLE_KEY: InjectionKey<(tab: DockTab) => string | null> =
  Symbol("omega-dock-tab-title");

/** the persisted reopen sizes for edge panes (page-lifetime memory: closing
    the sidebar and reopening restores the width the user had last) */
const EDGE_DEFAULT_PX: Record<"sidebar" | "inspector", number> = {
  sidebar: 240,
  inspector: 240,
};

export function createDock(store: Storage): DockApi {
  // card t_d6b657a7: the persisted VERSION rides the load - v1 trees go
  // through the era migration (legacy "inspector" ids expand, the
  // five-tab-era inspector regains settings+kanban), then all loads pass
  // the same modernization: markers derived, shells re-asserted, sessions
  // forced into the left bar, missing inspector singletons restored.
  const stored = store.getItem(DOCK_LAYOUT_KEY);
  const loaded = parseAnyLayout(stored, () => migrateLegacy((k) => store.getItem(k)));
  // card t_aa9dbada: the closed-edge set rides the record - the reload
  // restores the visibility the user left (no shaped re-assertion over it).
  // A record that predates the field migrates its bottom closure from the
  // legacy key; the key retires after the read (the record owns closure).
  const closedEdges: DockEdgeComponent[] =
    loaded?.closedEdges ??
    (store.getItem(BOTTOM_CLOSED_KEY) === "1" ? ["bottom"] : []);
  store.removeItem(BOTTOM_CLOSED_KEY);
  let initial = loaded.tree;
  if (loaded.version === 1) {
    // v1-era semantics only: the inspector group's tab list was fixed
    // chrome then - every era tree gains its missing right-area tabs
    initial = upgradeInspectorTabs(expandInspectorTabs(initial));
  } else {
    initial = expandInspectorTabs(initial);
  }
  // card t_f6d5a771: sync the id seed BEFORE boot repair mints panes. The
  // seed is per-page (0 at boot): repair first + sync after let
  // ensurePinnedEdges mint a fresh "n1" while the loaded tree still holds
  // its own old "n1" - duplicate ids, and every later id-keyed op (the
  // divider intents' prunePane, canRemovePane, focus lookups) acts on BOTH
  // panes. Syncing first keeps every minted id above the loaded tree's max.
  syncIdSeed(initial);
  // card t_d7fd3ad3: insights' home moved (main -> the right shell) - the
  // main-pane strand lifts before the shell repair so restoreInspectorSet
  // seats it in the right pane on the same boot.
  initial = rehomeInsightsTab(initial);
  initial = ensurePinnedEdges(initial, closedEdges);
  // card t_67fc1905: the bottom panel is grafted (or left closed) AFTER the
  // sidebars repair and BEFORE chat keys - the panel hosts no chat tabs,
  // and its id must seed above the loaded tree's max (syncIdSeed ran once)
  initial = ensureBottomPanel(initial, closedEdges);
  // Endo's comment (2026-10-26): logs is bottom-hosted only - run AFTER
  // ensureBottomPanel so a lifted right-shell copy never needs to graft
  // a shell itself (a present one already seats logs; a closed one drops
  // the copy and reseeds on reopen)
  initial = rehomeLogsTab(initial);
  initial = assignChatKeys(initial);
  // v0.14.4 ruling 6 BACKUP LAW: a REPAIRED record copies its raw form
  // ONCE, BEFORE the first auto-persist - the repair chain must never be
  // able to rewrite history, so the boot's own re-persist happens after.
  if (stored && loaded.repairs.length > 0) {
    store.setItem(DOCK_LAYOUT_BAK_KEY, stored);
  }
  // the ONE gated writer (ruling 6): every record write re-reads through
  // the total deserializer and shape-compares against the tree it came
  // from; a mismatch (writer bug, encoder drift) warns and refuses the
  // write - the previous record on disk stays, which IS the last-good.
  const gatedPersist = (t: DockNode) => {
    const candidate = serialize(t, closedEdges);
    const back = parseAnyLayout(candidate, () => t);
    if (!treeShapesMatch(back.tree, t)) {
      console.warn("dock: layout round-trip mismatch - the write was refused, last-good kept");
      return;
    }
    store.setItem(DOCK_LAYOUT_KEY, candidate);
  };
  // a modernized/fixed-shape-repaired load persists at once (now as v3) so
  // the next boot sees the modern tree even if no mutation fires this session
  if (stored) gatedPersist(initial);
  // shallowRef: the tree stays PLAIN data (structuredClone rejects proxies);
  // every op swaps the root, so reactivity rides the ref change alone
  const tree = shallowRef(initial) as Ref<DockNode>;

  // persist the WHOLE tree on every mutation (card ruling: layout lives in
  // localStorage alone; the URL owns only the focused session). The
  // closed-edge set rides every write (card t_aa9dbada: visibility saves
  // WITH the layout - a reload restores the state the user left).
  watch(
    tree,
    (t) => {
      gatedPersist(t);
    },
    { deep: true },
  );

  /** the closed-edge marking helper, idempotent - toggles, drag-closes
      and the narrow give-way all route through it */
  const setClosed = (comp: DockEdgeComponent, closed: boolean) => {
    const at = closedEdges.indexOf(comp);
    if (closed && at < 0) closedEdges.push(comp);
    if (!closed && at >= 0) closedEdges.splice(at, 1);
  };

  const has = (component: DockComponentName): boolean =>
    // singleton semantics (card t_30ee04fb): exists anywhere, active tab
    // or not - findComponentPane (active-only) would under-report a
    // singleton parked as an inactive tab and let toggles duplicate it
    findComponentTab(tree.value, component) !== null;

  // --- the undo ring (v0.14.4 ruling 10) -------------------------------------
  // Every committed LAYOUT op (split, merge, move, resize, hide) snapshots
  // the serialized tree into a 20-deep ring; Modifier-Z at the dock root
  // walks back one op. A mis-drop is one keystroke reversible. Content ops
  // (tab opens/closes, focus, session refs) never enter the ring - except
  // a close that MERGES (the pane census dropped), which is ruling 8's
  // structural op. The closed-edge set rides each snapshot: undo restores
  // the visibility the snapshot came with.
  const UNDO_RING_MAX = 20;
  const undoRing: string[] = [];
  /** the snapshot push: deduped (an op that lands on the snapshot's own
      state contributes nothing) and capped at 20 */
  const undoPush = (t: DockNode) => {
    const s = serialize(t, closedEdges);
    if (undoRing[undoRing.length - 1] === s) return;
    undoRing.push(s);
    if (undoRing.length > UNDO_RING_MAX) undoRing.shift();
  };
  /** swap in a NEW tree from a layout op: no-op trees contribute no
      snapshot (a refused drop / vanished target never rings) */
  const applyLayout = (before: DockNode, next: DockNode) => {
    if (next === before) return;
    undoPush(before);
    tree.value = next;
    // card t_b2430179 belt+suspenders: any layout mutation ends an armed
    // gesture - the record's geometry and indices are pre-mutation; the
    // record must never outlive its event (drops commit here; undo and
    // reveal ops run through the same funnel).
    endGesture();
  };
  /** walk back one op: pop the ring, re-parse through the total
      deserializer (a snapshot is always well-formed - it was serialized
      here - but the reader stays total), reseed ids/chat keys above the
      restored tree, and restore the snapshot's closed-edge set. Returns
      false when the ring is empty. */
  const undoLayout = (): boolean => {
    const s = undoRing.pop();
    if (!s) return false;
    const rec = parseAnyLayout(s, () => tree.value);
    syncIdSeed(rec.tree);
    const restored = assignChatKeys(rec.tree);
    if (rec.closedEdges) {
      closedEdges.splice(0, closedEdges.length, ...rec.closedEdges);
    }
    tree.value = restored;
    endGesture(); // undo mid-gesture: the record's indices are pre-restore
    return true;
  };

  const openSingletonIn = (
    paneId: string,
    component: DockComponentName,
  ): "created" | "focused" => {
    const res = openSingleton(tree.value, paneId, component);
    if (res.action !== "noop") tree.value = res.tree;
    // noop (a vanished target pane) preserves the tree, nothing opened;
    // report "focused" - the only honest closed set on a no-op
    return res.action === "created" ? "created" : "focused";
  };

  const setComponent = (paneId: string, component: DockComponentName) => {
    tree.value = setPaneComponent(tree.value, paneId, component);
  };
  const focus = (paneId: string, index: number) => {
    tree.value = focusTab(tree.value, paneId, index);
  };
  const openTabIn = (paneId: string, tab: DockTab) => {
    tree.value = openTab(tree.value, paneId, tab);
  };
  const closeTabIn = (paneId: string, index: number) => {
    // content op EXCEPT when the close merges (the census dropped - ruling
    // 8's structural unwind): only structural closes enter the ring
    const before = tree.value;
    const next = closeTab(before, paneId, index);
    if (paneCount(next) !== paneCount(before)) {
      applyLayout(before, next);
    } else {
      tree.value = next;
    }
  };
  const moveTabIn = (fromPaneId: string, fromIndex: number, toPaneId: string, toIndex: number) => {
    applyLayout(tree.value, moveTab(tree.value, fromPaneId, fromIndex, toPaneId, toIndex));
  };
  const dockDropToZoneIn = (
    fromPaneId: string,
    fromIndex: number,
    toPaneId: string,
    zone: DragZone,
  ) => {
    applyLayout(tree.value, dockDropToZone(tree.value, fromPaneId, fromIndex, toPaneId, zone));
  };
  const setSessionRef = (key: string, sessionRef: string | null) => {
    tree.value = setTabSessionRef(tree.value, key, sessionRef);
  };
  const closeSession = (sessionRef: string) => {
    tree.value = closeSessionTabs(tree.value, sessionRef);
  };
  // card t_36ceb99b: the viewer opener - the pure op carries focus-reuse;
  // ensureMainPane re-inserts a flexible main pane when the user removed
  // every last one. A tab open is a CONTENT op: no undo-ring entry.
  const openViewer = (v: ViewerRef) => {
    const pane = ensureMainPane();
    const res = openViewerTab(tree.value, v, pane);
    tree.value = res.tree;
  };
  const split = (paneId: string, direction: "row" | "column", side: "before" | "after") => {
    // v0.14.4 ruling 2/3: the direction/depth law gates the menu path too
    // (sidebars refuse row splits, tight roots refuse past depth 2) - a
    // refused split is a no-op, exactly like the refused drop
    if (splitLawRefusal(tree.value, paneId, direction)) return;
    applyLayout(tree.value, splitPane(tree.value, paneId, direction, side));
  };
  const remove = (paneId: string) => {
    applyLayout(tree.value, removePane(tree.value, paneId));
  };
  const ensureMainPane = (): string => {
    const first = firstMainPaneId(tree.value);
    if (first) return first;
    const res = insertMainPane(tree.value);
    applyLayout(tree.value, res.tree);
    return res.paneId;
  };
  const canRemovePane = (paneId: string): boolean => {
    const pane = findPane(tree.value, paneId);
    if (!pane) return false;
    // v0.14.4 ruling 8: a leaf inside a grown root merges away (the root
    // unwinds, never dies) - it always carries its remove X
    if (rootPaneOf(tree.value, paneId)) return true;
    return !isPinnedPane(pane) && mainPaneCount(tree.value) > 1;
  };
  // v0.14.4 ruling 1/4: the locked tab's LIVE hit-test - "the left shell's
  // own bar" is the left root's area (the shell pane + its grown leaves)
  const lockedBarOk = (paneId: string): boolean => !lockedBarRefusal(tree.value, paneId);
  // Endo ruling 2026-10-25: the tab-X visibility read. Reads the live
  // tree, so a pane whose sibling main pane just closed re-renders its
  // chrome in the same tick the tree swaps.
  const lastMainPane = (paneId: string): boolean => {
    const pane = findPane(tree.value, paneId);
    return !!pane && isLastMainPane(tree.value, pane);
  };
  const drag = (
    splitId: string,
    dividerIndex: number,
    startPx: number[],
    deltaPx: number,
    contentPx: number,
  ) => {
    const node = findSplit(tree.value, splitId);
    if (!node) return { collapsedPaneId: null, pushedPaneId: null };
    const d = dragSplitDivider(node, dividerIndex, startPx, deltaPx, contentPx);
    // card t_f6d5a771: divider close + squeeze push-back are back - the
    // intents prune the tree directly (prunePane is the same body
    // hideEdgePane runs; a pinned pane hides, it is never unpinned). card
    // t_aa9dbada: the close persists now (the closed-edge set rides the
    // record) - a reload restores the visibility the user left, exactly
    // like the menubar toggle. Edge panes remember their width first - the
    // menubar toggle reopens them at it (the reopen-at-its-width mechanic).
    // v0.14.4 ruling 10: the RESIZE is one layout op per gesture - the
    // snapshot rides api.beginResize (gesture start); a squeeze collapse
    // inside the gesture lands under the same snapshot.
    if (d.split !== node) tree.value = graftSplit(tree.value, d.split);
    for (const id of [d.collapsedPaneId, d.pushedPaneId]) {
      if (!id) continue;
      const gone = findPane(tree.value, id);
      if (gone) {
        const comp = edgeComponentOf(gone);
        if (comp && gone.fixed !== null) rememberEdgePx(store, comp, gone.fixed);
        if (comp) setClosed(comp, true);
      }
      tree.value = prunePane(tree.value, id);
    }
    return { collapsedPaneId: d.collapsedPaneId, pushedPaneId: d.pushedPaneId };
  };

  const toggleEdge = (component: "sidebar" | "inspector", side: "before" | "after") => {
    const pane = findEdgePane(tree.value, component);
    if (pane) {
      // hide remembers the pane's width - the reopen restores it; the
      // closed-edge set marks the closure so a reload restores it too.
      // v0.14.4 ruling 10: the snapshot lands FIRST (the pre-hide record
      // carries the edge still open - undo restores its visibility too)
      undoPush(tree.value);
      if (pane.fixed !== null) rememberEdgePx(store, component, pane.fixed);
      setClosed(component, true);
      tree.value = hideEdgePane(tree.value, component);
    } else {
      // reopen-clamp: a stale sub-floor remembered width reopens AT 320
      setClosed(component, false);
      applyLayout(tree.value, revealEdgePane(tree.value, component, side, recalledEdgePx(store, component)));
    }
  };

  // card t_67fc1905: the bottom panel's toggle (menubar). card t_aa9dbada:
  // closure rides the record's closed-edge set (the legacy BOTTOM_CLOSED_KEY
  // is retired; the marking no longer writes a store key). Hide remembers
  // the height; reopen restores it (absent/invalid falls to the default 200).
  const toggleBottom = () => {
    if (findEdgePane(tree.value, "bottom")) {
      // v0.14.4 ruling 10: the pre-hide snapshot first (closed set intact)
      undoPush(tree.value);
      tree.value = hideBottomPanel(tree.value, store);
      setClosed("bottom", true);
    } else {
      setClosed("bottom", false);
      applyLayout(tree.value, revealBottomPanel(tree.value, store));
    }
  };

  // Endo's comment (2026-10-26): the bottom shell's FULLSCREEN toggle -
  // the pane's tabbar button flips it. t_e539ba29 (2026-10-28, source
  // read: the comment said "renders ONLY the shell, the main row above
  // hides" - WRONG, and DockPane.vue's ruling already recorded the truth):
  // the band leaves the flow (position absolute over the pane-area
  // between the sidebar insets) and the column keeps rendering its split
  // children - the main row above stays visible with BOTH sidebars live
  // and self-toggled ("the sidebars never hide - Endo"), the sidebars'
  // resize dividers stay visible/draggable, and the row's chat sits under
  // the band. Only the root's own horizontal divider hides while
  // fullscreen (bottompanel.css: it points at nothing once the band is
  // out of flow). Session-only on purpose: a reload never boots into
  // fullscreen (the stored height + the render clamp keep the divider
  // grabbable; fullscreen is a view, not a layout state).
  const bottomFullscreen = ref(false);
  const toggleBottomFullscreen = () => {
    bottomFullscreen.value = !bottomFullscreen.value;
  };

  // card t_33f8756c: the window-narrow give-way (app.vue's resize watcher
  // measures the pane-area and hands the content px in). Each collapsed
  // edge pane's width goes through the same memory the menubar toggle uses.
  // card t_aa9dbada: the give-way's closure persists like every other hide
  // (a reload never re-reveals a pane the user's own state says is closed).
  const narrowSqueeze = (contentPx: number) => {
    const res = narrowGiveWay(tree.value, contentPx);
    if (!res.hidden.length) return;
    // v0.14.4 ruling 10: the pre-give-way snapshot first (the closed set
    // mutates below it - undo restores the visible state too)
    undoPush(tree.value);
    for (const h of res.hidden) {
      rememberEdgePx(store, h.component, h.px);
      setClosed(h.component, true);
    }
    tree.value = res.tree;
  };

  return {
    bootRepairs: loaded.repairs,
    tree, has, setComponent, focus, openTab: openTabIn, closeTab: closeTabIn, moveTab: moveTabIn,
    dockDropToZone: dockDropToZoneIn, lockedBarOk,
    openSingleton: openSingletonIn,
    setSessionRef, closeSession, openViewer,
    findChatTab: (key: string) => findChatTab(tree.value, key),
    firstChatTab: (sessionRef?: string) => firstChatTab(tree.value, sessionRef),
    firstDraftChatTab: () => firstDraftChatTab(tree.value),
    split, remove, toggleEdge, ensureMainPane, canRemovePane, isLastMainPane: lastMainPane, drag, toggleBottom,
    bottomFullscreen, toggleBottomFullscreen,
    narrowSqueeze,
    undoLayout, beginResize: () => undoPush(tree.value),
  };
}
