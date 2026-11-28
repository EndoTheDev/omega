// omega webui: the dock layout tree - VS Code-style splits, unlimited depth.
// Persists under omega.dock-layout in per-browser localStorage; the URL owns only
// the focused chat session (app.vue's concern).
//
// split { direction: row|column, children, sizes }, pane { tabs, active, fixed }
// `sizes` are fractions of the split's CONTENT box, parallel to children. A pane
// with `fixed` px ignores its fraction (flex: 0 0 Npx); the flexible children
// share the rest proportionally (flex-grow: fraction, flex-basis: 0), so one
// flexible child absorbs all remainder. Dividers adjust px for fixed panes
// (the sidebars' 320/640 snap attractor, the 128px floor; the bottom edge is
// free within its floors) and fractions for flexible pairs.

import {
  PANE_OPEN_MIN,
  SNAP_STEP,
  openClamp,
  snapCandidate,
  snapCandidateAt,
  splitFitsZone,
  travelSnapAt,
  zoneLabel,
  zoneOfEdge,
  type PaneZone,
} from "./panes.ts";
import {
  endGesture,
  zoneSplitOf,
  type DragZone,
} from "./dockdnd.ts";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  EMPTY_MODULE_GATE,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  CHAT_FLOOR_PX,
  MIN_PANE_PX,
  PRIMARY_TAB_COMPONENTS,
  classMembers,
  defaultHostTabs,
  dockableClass,
  dockableMinPx,
  isClosableTab,
  isLockedTab,
  moduleGated,
  seatAllowsIn,
  validViewerRef,
  type DockArea,
  type DockComponentName,
  type ModuleGate,
  type ViewerRef,
} from "./dockables.ts";
export {
  INSPECTOR_TAB_COMPONENTS,
  MAIN_DOCKABLES,
  PRIMARY_TAB_COMPONENTS,
  dockableMinPx,
};
export type { DockComponentName, ViewerRef } from "./dockables.ts";
export { PANE_OPEN_MIN };

export const DOCK_LAYOUT_KEY = "omega.dock-layout";
/** BACKUP LAW: before the first auto-persist of a REPAIRED layout the raw record is
 *  copied here - a repair bug must never destroy the only copy of the user's layout. */
export const DOCK_LAYOUT_BAK_KEY = "omega.dock-layout.bak";

// pre-dock pane widths, read once by migrateLegacy.
export const LEGACY_WIDTH_KEYS = {
  sidebar: "omega.sidebar-width",
  inspector: "omega.inspector-width",
} as const;

export { MIN_PANE_PX, CHAT_FLOOR_PX } from "./dockables.ts";
/** the bottom shell's max share of the split: a taller pane pushes its top edge (the
 *  divider) offscreen, where it cannot be grabbed back. */
export const BOTTOM_MAX_FRACTION = 0.5;

/** a leaf's sizing constraints; absent = the pre-contract constants (contractFloorOf).
 *  Serialized in the record from v:3, with the pane-class relaxation rank. */
export interface PaneContract {
  /** the hard floor: a drag/squeeze never starves the pane below it */
  minPx: number;
  maxPx: number | null;
  maxFrac: number | null;
  preferred: number;
  anchor: "leading" | "trailing" | "bottom";
}

export function edgeShellContract(): PaneContract {
  return { minPx: MIN_PANE_PX, maxPx: null, maxFrac: null, preferred: 320, anchor: "leading" };
}

export function chatPaneContract(): PaneContract {
  return { minPx: CHAT_FLOOR_PX, maxPx: null, maxFrac: null, preferred: 0, anchor: "leading" };
}

export function bottomShellContract(): PaneContract {
  return { minPx: MIN_PANE_PX, maxPx: null, maxFrac: BOTTOM_MAX_FRACTION, preferred: 200, anchor: "bottom" };
}

/** the drag floor a pane contracts to right now: fixed -> the 128 drag floor,
 *  flexible -> paneFloorOf (its contract min lifted by its worst hosted tab, so a
 *  tools-only pane drags to 128 and chat keeps 320). A nested split takes the flat
 *  320: minDockPx recursion would leak a row's HORIZONTAL sum into a vertical axis. */
export function contractFloorOf(node: DockNode): number {
  if (!isPane(node)) return CHAT_FLOOR_PX; // a nested split absorbs like a flexible pane
  if (node.fixed !== null) return MIN_PANE_PX;
  return paneFloorOf(node);
}

/** a flexible pane's HONEST floor: the worst dockableMinPx over the tabs it hosts
 *  (128 for a tools-only pane, 320 when it might show chat), lifted by its contract's
 *  minPx, and the chat wall when EMPTY (an empty pane's next tenant may be the chat).
 *  contractFloorOf (the drag clamps) and minDockPx (the reveal's "no room" check) both
 *  read this, so "no room" fires exactly when a drag could not free the space. */
export function paneFloorOf(pane: DockPane): number {
  let floor = pane.contract?.minPx ?? MIN_PANE_PX;
  for (const t of pane.tabs) floor = Math.max(floor, dockableMinPx(t.component));
  if (pane.tabs.length === 0) floor = Math.max(floor, CHAT_FLOOR_PX);
  return floor;
}

export interface DockTab {
  component: DockComponentName;
  sessionRef?: string;
  key?: string;
/** WHAT a viewer tab shows; a viewer tab without one is unreachable and dropped. */
  viewer?: ViewerRef;
}
export interface DockPane {
  kind: "pane";
  id: string;
  tabs: DockTab[];
  active: number;
  fixed: number | null;
  edge?: "left" | "right" | "bottom";
  /** sizing constraints; absent = the pre-contract constants of contractFloorOf. */
  contract?: PaneContract;
  /** the PINNED ROOT's local split tree: the root keeps its fixed px, contract, edge
   *  marker and hide/fullscreen modes. */
  grown?: DockSplit;
}
/** the pinned shells' markers (the toggle/find API's domain names) */
export type DockEdgeComponent = "sidebar" | "inspector" | "bottom";
export const BOTTOM_DEFAULT_PX = 200;
export interface DockSplit {
  kind: "split";
  id: string;
  direction: "row" | "column";
  children: DockNode[];
  sizes: number[];
  /** v:3 record only: the children's integer px shares on disk; rendering reads sizes. */
  px?: number[];
}
export type DockNode = DockSplit | DockPane;

let idSeed = 0;
/** the id generator; syncIdSeed seeds it from a loaded tree so a reload mints no dup. */
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

let tabKeySeed = 0;
/** tab keys use a c-prefix so they never collide with pane/split ids. */
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

/** give every keyless chat tab a key (the host's key invariant); returns a fresh
 *  clone, so callers run it on plain loaded data before the tree becomes a ref. */
export function assignChatKeys(tree: DockNode): DockNode {
  syncTabKeySeed(tree);
  const copy = clone(tree);
  for (const n of allNodes(copy)) {
    if (!isPane(n)) continue;
    n.tabs = n.tabs.map((t) =>
      (t.component === "chat" && !t.key) ? { ...t, key: nextTabKey() } : t,
    );
  }
  return copy;
}

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

/** n equal fractions summing to 1; provided ones are normalized. */
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

// THE LOCKED LAST TAB: a pane's LAST tab never closes - closeTab refuses and the X renders
// disabled and dimmed. The lock IS how a pane stays non-empty; nothing is seated in its place.
// A pane emptied by a GESTURE (a split, a tab dragged out) renders DockPane's empty state and a
// drop refills it; one REPAIRED into emptiness gets a fresh new-chat tab, never a "New Tab" sticker.

/** the lock's one condition; closeTab and DockPane's dimmed X both read this predicate. */
export function lastTabLocked(pane: DockPane): boolean {
  return pane.tabs.length <= 1;
}

/** the retired never-zero placeholder id (LEGACY_DOCK_COMPONENTS). */
const LEGACY_NEW_TAB = "newtab";

function isLegacySeat(component: string): boolean {
  return component === LEGACY_NEW_TAB;
}

function newChatTab(): DockTab {
  return { component: "chat", key: nextTabKey() };
}

/** the boot's pane-seat repair, one law for two dead states: a placeholder seat retires (dropped
 *  beside real content, converted into a fresh new-chat tab when alone), and a zero-tab pane
 *  seats the same fresh chat. Idempotent; a clean tree returns the SAME object. */
export function repairPaneSeats(tree: DockNode): DockNode {
  const needs = (n: DockNode): boolean =>
    isPane(n)
      ? (!n.grown && (n.tabs.length === 0 || n.tabs.some((t) => isLegacySeat(t.component))))
      : n.children.some(needs);
  if (!needs(tree)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n) || n.grown) return;
    const active = n.tabs[n.active];
    const real = n.tabs.filter((t) => !isLegacySeat(t.component));
    if (real.length === 0) {
      n.tabs = [newChatTab()];
      n.active = 0;
      return;
    }
    if (real.length !== n.tabs.length) {
      const focus = active && !isLegacySeat(active.component) ? active : real[0]!;
      n.tabs = real;
      n.active = Math.max(0, real.indexOf(focus));
    }
  });
}

// THE MODULE GATE: a module-off dockable's tab leaves the strip but is PARKED, not deleted, and
// re-seats exactly where it sat when the module returns. Parked tabs ride their own key, never
// the layout record: the record stays what is LIVE (no ghost pane), the stash what is unhosted.

export const MODULE_STASH_KEY = "omega.dock-module-stash";

export interface ParkedTab {
  component: string;
  pane: string;
  index: number;
}

export interface GateResult {
  tree: DockNode;
  parked: ParkedTab[];
  changed: boolean;
}

/** lenient stash reader, like every persisted reader: junk entries drop, never throw. */
export function parseParkedTabs(raw: string | null): ParkedTab[] {
  if (!raw) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return []; // a corrupt stash is no stash: the layout is the truth
  }
  if (!Array.isArray(parsed)) return [];
  const out: ParkedTab[] = [];
  for (const entry of parsed) {
    if (!entry || typeof entry !== "object") continue;
    const r = entry as Record<string, unknown>;
    if (typeof r.component !== "string" || r.component === "") continue;
    const index =
      typeof r.index === "number" && Number.isInteger(r.index) && r.index >= 0
        ? r.index
        : 0;
    out.push({
      component: r.component,
      pane: typeof r.pane === "string" ? r.pane : "",
      index,
    });
  }
  return out;
}

/** park every gated tab, recording its seat. Idempotent: an ungated tree comes back as-is. */
export function parkGatedTabs(tree: DockNode, gate: ModuleGate): GateResult {
  const parked: ParkedTab[] = [];
  let changed = false;
  const next = cloneTreeWith(tree, (n) => {
    if (!isPane(n)) return;
    const kept: DockTab[] = [];
    let active = 0;
    let hid = 0;
    n.tabs.forEach((tab, i) => {
      if (moduleGated(tab.component, gate)) {
        hid++;
        parked.push({ component: tab.component, pane: n.id, index: kept.length });
        return;
      }
      if (i === n.active) active = kept.length;
      kept.push(tab);
    });
    if (hid === 0) return;
    changed = true;
    n.tabs = kept;
    // focus follows whatever held it; a gate-emptied pane is left to the seat repair
    n.active = Math.max(0, Math.min(active, kept.length - 1));
  });
  return { tree: changed ? next : tree, parked, changed };
}

/** insert a tab at `index` WITHOUT stealing the focus. */
function insertTabAt(
  tree: DockNode,
  paneId: string,
  index: number,
  tab: DockTab,
): DockNode {
  if (!findPane(tree, paneId)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n) || n.id !== paneId) return;
    n.tabs.splice(Math.max(0, Math.min(index, n.tabs.length)), 0, tab);
  });
}

/** where a parked tab goes back; a main-area tab with no main pane stays parked. */
function reseatTarget(
  tree: DockNode,
  parked: ParkedTab,
  id: DockComponentName,
): { paneId: string; index: number } | null {
  const own = findPane(tree, parked.pane);
  if (own) return { paneId: own.id, index: parked.index };
  const area = DOCKABLES.find((d) => d.id === id)?.area;
  const shell = area ? edgeOfArea(area) : null;
  if (shell) {
    const edge = findEdgePane(tree, shell);
    return edge ? { paneId: edge.id, index: parked.index } : null;
  }
  const main = firstMainPaneId(tree);
  return main ? { paneId: main, index: parked.index } : null;
}

export function reseatParkedTabs(
  tree: DockNode,
  parked: readonly ParkedTab[],
  gate: ModuleGate,
): { tree: DockNode; parked: ParkedTab[] } {
  let t = tree;
  const still: ParkedTab[] = [];
  for (const p of parked) {
    if (moduleGated(p.component, gate)) {
      still.push(p);
      continue;
    }
    if (!(DOCK_COMPONENTS as readonly string[]).includes(p.component)) continue;
    const id = p.component as DockComponentName;
    // a singleton already hosted absorbs the parked seat - one instance, no duplicate
    if (findComponentTab(t, id)) continue;
    const seat = reseatTarget(t, p, id);
    if (!seat) {
      still.push(p);
      continue;
    }
    t = insertTabAt(t, seat.paneId, seat.index, { component: id });
  }
  return { tree: t, parked: still };
}

/** one parked seat per component: module-owned dockables are singletons. */
function mergeParked(
  existing: readonly ParkedTab[],
  incoming: readonly ParkedTab[],
): ParkedTab[] {
  const out = [...existing];
  for (const p of incoming) {
    if (out.some((x) => x.component === p.component)) continue;
    out.push(p);
  }
  return out;
}

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

export function findComponentPane(tree: DockNode, component: DockComponentName): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.tabs[n.active]?.component === component) return n;
  }
  return null;
}

/** the FIRST tab hosting `component` anywhere - a singleton's finders read every tab. */
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

// the pane model: a PINNED pane wears an edge marker (left = sessions home, right = inspector,
// bottom = the panel) and is never closable, removable or movable; its CONTENT is free, except
// the locked sessions tab, which lives only in "left". Everything else is a main pane. The dock
// always holds at least one pane, and a pinned shell may sit empty but never disappears.

export function paneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) if (isPane(node)) n++;
  return n;
}

export function isPinnedPane(pane: DockPane): boolean {
  return pane.edge !== undefined;
}

/** the first main pane, depth-first - where session opens land. */
export function firstMainPaneId(tree: DockNode): string | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && !isPinnedPane(n)) return n.id;
  }
  return null;
}

/** the main-pane census: pinned shells don't count - the MAIN area never reaches zero. */
export function mainPaneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) {
    if (isPane(node) && !isPinnedPane(node)) n++;
  }
  return n;
}

export function isLastMainPane(tree: DockNode, pane: DockPane): boolean {
  return !isPinnedPane(pane) && mainPaneCount(tree) <= 1;
}

// The pinned roots grow LOCAL split trees from the same split/pane machinery inside the
// root, so root geometry (fixed px, contract, resize band, hide/fullscreen) never moves.
// THE SHAPE LAW: a sidebar only STACKS (column splits - its two side-by-side columns ARE
// the two sidebars). Roots never die: a degenerate local tree un-grows to the classic cell.

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

export function rootPaneOf(tree: DockNode, paneId: string): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.grown && findPane(n.grown, paneId)) return n;
  }
  return null;
}

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

/** THE SHAPE LAW: may `direction` split `paneId` here? A sidebar - the shell or a leaf of
 *  its grown stack - only stacks, never into side-by-side columns; the tight roots cap
 *  at two split levels. */
export function splitLawRefusal(
  tree: DockNode,
  paneId: string,
  direction: "row" | "column",
): string | null {
  const pane = findPane(tree, paneId);
  if (!pane) return "a pane of the gesture vanished";
  const root = rootPaneOf(tree, paneId);
  // the shape law names the pane's zone: an inner leaf inherits its grown root's zone
  if (!splitFitsZone(zoneOfEdge((root ?? pane).edge), direction)) {
    return "a sidebar stacks - it never splits into columns";
  }
  if (root?.grown && splitDepthInRoot(root.grown, paneId) >= 2) {
    return "this root caps at two split levels";
  }
  return null;
}

/** a main pane for the column root: it goes into the ROW, never beside the whole column. */
export function insertMainPaneIntoRow(tree: DockNode): { tree: DockNode; paneId: string } | null {
  if (!isSplit(tree) || tree.direction !== "column") return null;
  const rowIdx = tree.children.findIndex((c) => isSplit(c) && c.direction === "row");
  if (rowIdx < 0) return null;
  const copy = clone(tree);
  const row = copy.children[rowIdx] as DockSplit;
  row.children.push(makePane(null));
  row.sizes = normalizeSizes(row.children.length, row.sizes);
  // the minted pane starts EMPTY - the opener (app.vue) mints the tab that lands in it
  return {
    tree: copy,
    paneId: (row.children[row.children.length - 1] as DockPane).id,
  };
}

/** the opener's insert: a column root delegates to the row push, never beside it. */
export function insertMainPane(tree: DockNode): { tree: DockNode; paneId: string } {
  const inRow = insertMainPaneIntoRow(tree);
  if (inRow) return inRow;
  const pane = makePane(null);
  if (isPane(tree) || !isSplit(tree) || tree.direction !== "row") {
    return { tree: makeSplit("row", [tree, pane]), paneId: pane.id };
  }
  const copy = clone(tree);
  copy.children.push(pane);
  copy.sizes = normalizeSizes(copy.children.length, copy.sizes);
  return { tree: copy, paneId: pane.id };
}

/** strip every insights tab that is not right-hosted (its home moved). */
export function rehomeInsightsTab(tree: DockNode): DockNode {
  let stranded = false;
  for (const n of allNodes(tree)) {
    if (
      isPane(n) &&
      n.edge !== "right" &&
      // a leaf inside the RIGHT ROOT is right-hosted too.
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
    if (rootPaneOf(tree, n.id)?.edge === "right") return;
    const idx = n.tabs.findIndex((t) => t.component === "insights");
    if (idx < 0) return;
    n.tabs = n.tabs.filter((_, i) => i !== idx);
    n.active = clamp(idx < n.active ? n.active - 1 : n.active, 0, n.tabs.length - 1);
  });
}

/** the pinned shells are fixed chrome: a loaded tree that lost one gets it re-inserted at
 *  its edge, sessions is forced into the left shell, and inspector-area singletons missing
 *  from the whole tree return to the right shell. */
export function ensurePinnedEdges(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
/** components inside CLOSED shells' stows: the missing-set repair must never re-seat them. */
  stowed: readonly DockComponentName[] = [],
): DockNode {
  let result = deriveEdgeMarkers(tree, closed);
  if (!closed.includes("sidebar") && !findEdgePane(result, "sidebar")) {
    result = insertRootEdge(
      result,
      "before",
      defaultPrimaryPane(result, EDGE_DEFAULT_PX.sidebar),
    );
  }
  if (!closed.includes("inspector") && !findEdgePane(result, "inspector")) {
    result = insertRootEdge(
      result,
      "after",
      defaultInspectorPane(result, EDGE_DEFAULT_PX.inspector),
    );
  }
  result = forceSessionsLeft(result);
  result = restoreSeatSet(result, "left", stowed);
  return restoreSeatSet(result, "right", stowed);
}

/** identify pre-marker trees by content fingerprint: only the FIRST match of an unclaimed side
 *  claims it, and a side the record says the user closed is never minted. The primary shell is
 *  anchored by its LOCKED view (sessions), never a visiting companion. Idempotent. */
export function deriveEdgeMarkers(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
): DockNode {
  const claimed: Record<"left" | "right", boolean> = { left: false, right: false };
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.edge) claimed[n.edge] = true;
  }
  if (claimed.left && claimed.right) return tree;
  const classOf = (t: DockTab) => dockableClass(t.component);
  const primaryAnchor = PRIMARY_TAB_COMPONENTS.find((c) => isLockedTab(c)) ?? "sidebar";
  const copy = clone(tree);
  let changed = false;
  for (const n of allNodes(copy)) {
    if (!isPane(n) || n.edge) continue;
    if (
      !claimed.left &&
      !closed.includes("sidebar") &&
      n.tabs.some((t) => t.component === primaryAnchor)
    ) {
      n.edge = "left";
      claimed.left = true;
      changed = true;
    } else if (
      !claimed.right &&
      !closed.includes("inspector") &&
      n.tabs.some((t) => classOf(t) === "secondary-sidebar")
    ) {
      n.edge = "right";
      claimed.right = true;
      changed = true;
    }
  }
  return changed ? copy : tree;
}

/** the loader's lock enforcement: sessions sits in the left shell's bar. */
export function forceSessionsLeft(tree: DockNode): DockNode {
  const left = findEdgePane(tree, "sidebar");
  if (!left) return tree; // repair mints it above - no shell, nothing to force
  // a GROWN left root's bar is the leaf that hosts sessions.
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

/** re-seat the zone's missing class members in its shell, never duplicating one. */
export function restoreSeatSet(
  tree: DockNode,
  zone: "left" | "right",
  /** components a CLOSED shell's stow records - never re-seated. */
  stowed: readonly DockComponentName[] = [],
): DockNode {
  const right = zone === "right";
  const shell = findEdgePane(tree, right ? "inspector" : "sidebar");
  if (!shell) return tree;
  const members = classMembers(zone);
  const missing = members.filter(
    (c) => !findComponentTab(tree, c) && !stowed.includes(c),
  );
  if (missing.length === 0) return tree;
  // a grown root seats the restored set in the leaf that already hosts.
  let hostId = shell.id;
  if (shell.grown) {
    let host: DockPane | null = null;
    for (const n of allNodes(shell.grown)) {
      if (
        isPane(n) &&
        n.tabs.some((t) => (members as readonly string[]).includes(t.component))
      ) { host = n; break; }
    }
    host = host ?? firstPaneOf(shell.grown);
    if (host) hostId = host.id;
  }
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === hostId) {
      n.tabs = [...n.tabs, ...missing.map((c) => ({ component: c }))];
      n.active = clamp(n.active, 0, n.tabs.length - 1);
    }
  });
}

/** THE SHAPE LAW's one-time migration: a sidebar carrying a local split tree records the
 *  deleted model and resets. Center and bottom layouts are legal shapes and come back
 *  exactly as persisted. */
export function resetSidebarShape(tree: DockNode): DockNode {
  let dirty = false;
  for (const n of allNodes(tree)) {
    if (isPane(n) && (n.edge === "left" || n.edge === "right") && n.grown !== undefined) {
      dirty = true;
      break;
    }
  }
  if (!dirty) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!isPane(n) || (n.edge !== "left" && n.edge !== "right") || n.grown === undefined) return;
    n.tabs = [];
    n.active = 0;
    delete n.grown;
  });
}

/** insert `pane` at the root's edge (inside a bottom-pinned column, the main row's edge). */
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

/** pre-reset trees persisted the inspector as ONE tab hosting its own chip strip. */
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

/** the inspector's tab set GREW: every pane hosting a right-area tab gains the missing ones in
 *  registry order. "settings" left the set, so it is absent from the rank map and a settings-only
 *  pane is no inspector host - retireInspectorSettings owns that seat. */
export function upgradeInspectorTabs(tree: DockNode): DockNode {
  const rank = new Map(INSPECTOR_TAB_COMPONENTS.map((c, i) => [c, i]));
  // a right-area id marks the pre-marker inspector host; a settings seat never does
  const isInspectorTab = (c: DockComponentName) => rank.has(c);
  let done = false;
  const copy = clone(tree);
  for (const n of allNodes(copy)) {
    if (!isPane(n)) continue;
    const hosts = n.tabs.some((t) => isInspectorTab(t.component));
    const next = n.tabs
      .concat(
        hosts
          ? INSPECTOR_TAB_COMPONENTS
              .filter((c) => !n.tabs.some((t) => t.component === c))
              .map((c) => ({ component: c }))
          : [],
      )
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

/** settings left the inspector set: evict it from every inspector host. */
export function retireInspectorSettings(tree: DockNode): DockNode {
  const inInspector = (n: DockNode): n is DockPane =>
    isPane(n) &&
    (n.edge === "right" ||
      rootPaneOf(tree, n.id)?.edge === "right" ||
      // legacy pre-marker trees: content fingerprint (the upgradeInspectorTabs read)
      (!n.edge &&
        n.tabs.some((t) =>
          (INSPECTOR_TAB_COMPONENTS as readonly string[]).includes(t.component))));
  let seated = false;
  for (const n of allNodes(tree)) {
    if (inInspector(n) && isPane(n) && n.tabs.some((t) => t.component === "settings")) {
      seated = true;
      break;
    }
  }
  if (!seated) return tree;
  return cloneTreeWith(tree, (n) => {
    if (!inInspector(n)) return;
    const idx = n.tabs.findIndex((t) => t.component === "settings");
    if (idx < 0) return;
    n.tabs = n.tabs.filter((t) => t.component !== "settings");
    n.active = clamp(idx < n.active ? n.active - 1 : n.active, 0, n.tabs.length - 1);
  });
}

// the singleton opener: one instance per component, focus-reused. Multi components
// (chat) never come through here - the app's opener mints a tab every time.

export type OpenSingletonAction = "created" | "focused" | "noop";

export function openSingleton(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): { tree: DockNode; action: OpenSingletonAction } {
  // locked components have no open path: the loader seats them, never a menu
  if (isLockedTab(component)) return { tree, action: "noop" };
  const found = findComponentTab(tree, component);
  if (found) {
    // focus it in place - even when the target pane is this one.
    return { tree: focusTab(tree, found.paneId, found.index), action: "focused" };
  }
  const pane = findPane(tree, paneId);
  if (!pane) return { tree, action: "noop" }; // target vanished - no-op
  // the target pane's ZONE must seat the view (the class law)
  if (seatRefusal(tree, component, paneId) !== null) return { tree, action: "noop" };
  const next = pane.tabs.length === 0
    ? setPaneComponent(tree, paneId, component)
    : openTab(tree, paneId, { component });
  return { tree: next, action: "created" };
}

// pure tree operations: each returns a NEW tree, inputs are never mutated.

function clone<T>(tree: T): T {
  return structuredClone(tree);
}

function cloneTreeWith(tree: DockNode, fn: (node: DockNode) => void): DockNode {
  const copy = clone(tree);
  for (const n of allNodes(copy)) fn(n);
  return copy;
}

export function insertPane(
  tree: DockNode,
  targetId: string,
  direction: "row" | "column",
  side: "before" | "after",
  pane: DockPane,
): DockNode {
  if (targetId === tree.id) {
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

/** remove a pane by id. The LAST MAIN pane refuses: the main area never reaches zero
 *  panes, whatever path the request took (button, shortcut, API, stale state). Shells
 *  don't count toward that census - the store's ensureMainPane repairs the next open. */
export function removePane(tree: DockNode, paneId: string): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane)) return tree;
  // a grown root's leaf prunes WITHIN the root: prunePane cannot see grown children
  if (rootPaneOf(tree, paneId)) return pruneInnerLeaf(tree, paneId);
  if (isLastMainPane(tree, pane)) return tree;
  return prunePane(tree, pane.id);
}

/** the shared prune body: cut, hoist a lone survivor, repair sizes. Callers own the policy. */
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

// the edge toggle: pinned panes are untouchable through removePane and closeTab, so the
// menubar toggles get this guarded route. Shells are found by marker, not tab fingerprint.

/** THE CLASS <-> SHELL ADAPTER: the edge component a home zone names. ONE map - handing a
 *  zone name to findEdgePane silently answers the bottom pane, so no caller spells it. */
export function edgeOfArea(area: DockArea): DockEdgeComponent | null {
  switch (area) {
    case "left":
      return "sidebar";
    case "right":
      return "inspector";
    case "bottom":
      return "bottom";
    case "main":
      return null;
  }
}

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

export function hideEdgePane(
  tree: DockNode,
  component: "sidebar" | "inspector",
): DockNode {
  const pane = findEdgePane(tree, component);
  if (!pane) return tree;
  return prunePane(tree, pane.id);
}

// a hidden edge pane keeps its width.
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
  // the sidebars' reopen clamp lifts a stale sub-floor width to 320; the bottom's smaller
  // scale floors at the drag floor (128), never at the sidebar's open clamp
  return component === "bottom" ? Math.max(MIN_PANE_PX, px) : openClamp(px);
}

export function edgeComponentOf(pane: DockPane): EdgeComponent | null {
  return pane.edge === "left" ? "sidebar"
    : pane.edge === "right" ? "inspector"
    : pane.edge === "bottom" ? "bottom"
    : null;
}

// window-narrow give-way: a plain window resize skips the divider math, so the fixed
// sidebars keep their px and the flexible main pane shrinks under its 320 floor. A wider
// window never re-reveals - the user reopens by toggle, like a drag-close.

/** the minimum px a subtree needs along its axis (a row sums, a column maxes). */
export function minDockPx(node: DockNode): number {
  if (isPane(node)) return node.fixed !== null ? node.fixed : paneFloorOf(node);
  const reqs = node.children.map(minDockPx);
  const dividers = node.direction === "row" ? (reqs.length - 1) * 4 : 0;
  return node.direction === "row"
    ? reqs.reduce((a, b) => a + b, 0) + dividers
    : Math.max(0, ...reqs);
}

export interface NarrowHiddenEntry {
  component: "sidebar" | "inspector";
  px: number;
  pane: DockPane;
}

/** pure window-narrow give-away: while `contentPx` cannot seat every fixed pane plus the
 *  flexible floors, edge panes collapse (inspector first unless just revealed, then
 *  sidebar). A tree that still doesn't fit stops - a resize never prunes user splits. */
export function narrowGiveWay(
  tree: DockNode,
  contentPx: number,
  justRevealed: "sidebar" | "inspector" | null = null,
): { tree: DockNode; hidden: NarrowHiddenEntry[] } {
  const hidden: NarrowHiddenEntry[] = [];
  let t = tree;
  const order: readonly ("sidebar" | "inspector")[] =
    justRevealed === "inspector" ? ["sidebar", "inspector"] : ["inspector", "sidebar"];
  for (const comp of order) {
    if (minDockPx(t) <= contentPx) break;
    const pane = findEdgePane(t, comp);
    if (!pane) continue;
    hidden.push({ component: comp, px: pane.fixed ?? 0, pane: clone(pane) });
    t = hideEdgePane(t, comp);
  }
  return { tree: t, hidden };
}

/** the toggle's reopen half: the shell comes back at the root's edge through the same
 *  builders ensurePinnedEdges uses. The reopened right shell mints only the inspector tabs
 *  not already living elsewhere - a relocated singleton is never duplicated by a reopen. */
export function revealEdgePane(
  tree: DockNode,
  component: "sidebar" | "inspector",
  side: "before" | "after",
  fixedPx?: number | null,
/** the closed shell's stowed pane: reopen restores THE pane (tabs, active, fixed, grown as the
 *  hide captured them), its own px winning over `fixedPx`. Nothing stowed mints the default. */
  hidden?: DockPane | null,
  /** the measured content px: a reveal the window cannot seat is refused before any mutation. */
  contentPx?: number | null,
): DockNode {
  const edge = component === "sidebar" ? "left" : "right";
  const fixed = Math.max(PANE_OPEN_MIN, fixedPx ?? EDGE_DEFAULT_PX[component]);
  const fresh = () =>
    component === "sidebar" ? makePane("sidebar", fixed, "left") : defaultInspectorPane(tree, fixed);
  let pane: DockPane = fresh();
  if (hidden && hidden.edge === edge) {
    const revived = revivePane(hidden, fixed, PANE_OPEN_MIN);
    // the no-duplication law re-checked on the way back.
    revived.tabs = revived.tabs.filter((t) => !findComponentTab(tree, t.component));
    if (revived.tabs.length > 0) {
      revived.active = clamp(revived.active, 0, revived.tabs.length - 1);
      pane = revived;
    } // every restored tab already lives elsewhere: mint the default
  }
  // the wall law: a reveal that cannot fit is checked against what the watcher's give-way
  // could actually free - the overflow that matters lives INSIDE a grown root split, where
  // hideEdgePane frees nothing and the insert would be reaped on sight.
  const candidate = insertRootEdge(tree, side, pane);
  if (
    contentPx !== null &&
    contentPx !== undefined &&
    Number.isFinite(contentPx) &&
    minDockPx(candidate) > contentPx
  ) {
    // the protected component is NEVER reaped and every OTHER root edge gives way; if that
    // fully-relieved floor still exceeds the content px the watcher could never reach a
    // fitting state - the reveal is hopeless and the caller gets the SAME tree back.
    let probe = candidate;
    for (const comp of ["inspector", "sidebar"] as const) {
      if (comp !== component && findEdgePane(probe, comp)) {
        probe = hideEdgePane(probe, comp);
      }
    }
    if (minDockPx(probe) > contentPx) return tree;
  }
  return candidate;
}

/** the reveal's decision wrapper: the tree and whether it revealed (a refusal = same tree). */
export function tryRevealEdgePane(
  tree: DockNode,
  component: "sidebar" | "inspector",
  side: "before" | "after",
  fixedPx?: number | null,
  hidden?: DockPane | null,
  contentPx?: number | null,
): { tree: DockNode; revealed: boolean } {
  const next = revealEdgePane(tree, component, side, fixedPx, hidden, contentPx);
  return { tree: next, revealed: next !== tree };
}

/** the restore twin of the minters: the stowed pane comes back with FRESH ids (its old ones
 *  may have been re-minted live, and duplicate ids make every id-keyed op act on BOTH),
 *  reopened at `floor` or its own recorded px when it carries one. */
function revivePane(hidden: DockPane, fallbackFixed: number, floor: number): DockPane {
  const pane = clone(hidden);
  pane.id = nextNodeId();
  const reId = (n: DockNode): void => {
    n.id = nextNodeId();
    if (isSplit(n)) for (const c of n.children) reId(c);
  };
  if (pane.grown) reId(pane.grown);
  pane.fixed =
    pane.fixed !== null && Number.isFinite(pane.fixed)
      ? Math.max(floor, pane.fixed)
      : Math.max(floor, fallbackFixed);
  return pane;
}

/** the fresh right shell: every secondary-sidebar tab NOT already living somewhere. */
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

function defaultPrimaryPane(tree: DockNode, fixed: number): DockPane {
  return {
    kind: "pane",
    id: nextNodeId(),
    tabs: PRIMARY_TAB_COMPONENTS
      .filter((c) => !findComponentTab(tree, c))
      .map((c) => ({ component: c })),
    active: 0,
    fixed,
    edge: "left",
  };
}

/** legacy bottom-closed flag: read once at boot, migrated into closedEdges. */
export const BOTTOM_CLOSED_KEY = "omega.dock-bottom-closed";

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

/** graft the bottom shell in: the tree becomes the column's first child; the caller owns
 *  idempotence (a shell that already sits in the tree must never reach here). */
export function ensureBottomShell(tree: DockNode): DockNode {
  return makeSplit("column", [tree, bottomShellPane()]);
}

export function ensureBottomPanel(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
): DockNode {
  if (findEdgePane(tree, "bottom")) return tree;
  if (closed.includes("bottom")) return tree; // closed by choice
  return ensureBottomShell(tree);
}

/** the boot repair's bottom-home SYNC: the whole-tree read keeps the singleton law (a default
 *  parked elsewhere is never duplicated), and a fully-seated shell returns the SAME tree. */
export function syncBottomHostTabs(tree: DockNode): DockNode {
  const shell = findEdgePane(tree, "bottom");
  if (!shell) return tree; // closed/absent: the graft owns re-seeding
  const home = defaultHostTabs("bottom");
  const missing = home.filter((c) => !findComponentTab(tree, c));
  if (missing.length === 0) return tree;
  let hostId = shell.id;
  if (shell.grown) {
    for (const n of allNodes(shell.grown)) {
      if (isPane(n) && n.tabs.some((t) => home.includes(t.component))) {
        hostId = n.id;
        break;
      }
    }
    if (hostId === shell.id) {
      hostId = firstPaneOf(shell.grown)?.id ?? shell.id;
    }
  }
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === hostId) {
      const wasEmpty = n.tabs.length === 0;
      n.tabs = [...n.tabs, ...missing.map((c) => ({ component: c }))];
      n.active = wasEmpty ? 0 : n.active;
    }
  });
}

/** the toggle's hide half (the bottom domain); the closed-edge marking is the caller's. */
export function hideBottomPanel(tree: DockNode, store: Storage): DockNode {
  const pane = findEdgePane(tree, "bottom");
  if (!pane) return tree;
  if (pane.fixed !== null) rememberEdgePx(store, "bottom", pane.fixed);
  return prunePane(tree, pane.id);
}

export function revealBottomPanel(
  tree: DockNode,
  store: Storage,
  hidden?: DockPane | null,
): DockNode {
  const restored = recalledEdgePx(store, "bottom");
  let shell: DockPane;
  if (hidden && hidden.edge === "bottom") {
    const revived = revivePane(hidden, Math.max(MIN_PANE_PX, restored ?? BOTTOM_DEFAULT_PX), MIN_PANE_PX);
    revived.tabs = revived.tabs.filter((t) => !findComponentTab(tree, t.component));
    if (revived.tabs.length > 0) {
      revived.active = clamp(revived.active, 0, revived.tabs.length - 1);
      shell = revived;
    } else {
      shell = bottomShellPane();
      shell.fixed = Math.max(MIN_PANE_PX, restored ?? BOTTOM_DEFAULT_PX);
    }
  } else {
    shell = bottomShellPane();
    shell.fixed = Math.max(MIN_PANE_PX, restored ?? BOTTOM_DEFAULT_PX);
  }
  return makeSplit("column", [tree, shell]);
}

export function movePane(
  tree: DockNode,
  paneId: string,
  targetId: string,
  position: "before" | "after",
): DockNode {
  if (paneId === targetId) return tree;
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane) || !findPane(tree, targetId)) return tree;
  // detach must not collapse while the source still anchors the insert.
  const without = removePane(tree, paneId);
  // a refused detach must not fall through to the insert - the clone would duplicate the pane
  if (without === tree) return tree;
  if (!findPane(without, targetId)) return without; // target WAS the source
  const moving = clone(pane);
  return insertPane(without, targetId, "row", position, moving);
}

export function focusTab(tree: DockNode, paneId: string, index: number): DockNode {
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.active = Math.max(0, Math.min(index, n.tabs.length - 1));
    }
  });
}

/** give an EMPTY pane its first component (the '+' path), replacing tabs wholesale; emptying
 *  back is closeTab's job. The LOCKED sessions tab never enters this way, unknown ids refuse,
 *  and the pane's ZONE must seat the view. */
export function setPaneComponent(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isLockedTab(component)) return tree;
  if (seatRefusal(tree, component, paneId) !== null) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [{ component }];
      n.active = 0;
    }
  });
}

/** open a new tab at the end of a pane and activate it. THE SEAT MAP decides: a view opens
 *  only in a zone its class allows, so chat can never be minted into a sidebar and a bottom
 *  view never opens in the main pane. */
export function openTab(tree: DockNode, paneId: string, tab: DockTab): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isLockedTab(tab.component)) return tree;
  if (seatRefusal(tree, tab.component, paneId) !== null) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [...n.tabs, tab];
      n.active = n.tabs.length - 1;
    }
  });
}

/** close a tab. A pane's LAST tab never closes and an UNCLOSEABLE tab (the pinned shells')
 *  refuses too - no close empties a pane, so nothing is ever seated in its place. A grown
 *  root's inner leaf leaves through its own remove X, never by closing its last tab. */
export function closeTab(tree: DockNode, paneId: string, index: number): DockNode {
  const pane = findPane(tree, paneId);
  const tab = pane?.tabs[index];
  if (!pane || !tab || !isClosableTab(tab.component)) return tree;
  if (lastTabLocked(pane)) return tree; // the locked last tab
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      if (n.tabs.length <= index) return;
      n.tabs = n.tabs.filter((_, i) => i !== index);
      n.active = Math.max(0, Math.min(n.active > index ? n.active - 1 : n.active, n.tabs.length - 1));
    }
  });
}

/** DnD: move a tab between positions or panes. `toIndex` is the insertion index into the
 *  TARGET pane's tabs AFTER the tab is lifted out of the source (a same-pane reorder passes
 *  the post-removal index). A cross-pane move that takes the source's last tab closes the
 *  source; pinned shells stay empty-but-present. */
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
  // THE ZONE LAW gates cross-pane landings only: a same-pane reorder never changes the seat
  if (fromPaneId !== toPaneId && seatRefusal(tree, tab.component, toPaneId) !== null) return tree;
  if (fromPaneId === toPaneId) {
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
      if (n.active === fromIndex)
        n.active = Math.min(n.active, Math.max(0, n.tabs.length - 1));
      else if (n.active > fromIndex) n.active -= 1;
    } else if (n.id === toPaneId) {
      const at = clamp(toIndex, 0, n.tabs.length);
      n.tabs = [...n.tabs.slice(0, at), tab, ...n.tabs.slice(at)];
      n.active = at;
    }
  });
  // the last tab out closes the source pane; pinned shells stay, prunePane keeps the last one
  if (result === tree) return tree;
  const src = findPane(result, fromPaneId);
  if (src && !isPinnedPane(src) && src.tabs.length === 0) {
    // an inner leaf that emptied MERGES AWAY (the grown root unwinds)
    if (rootPaneOf(result, fromPaneId)) return pruneInnerLeaf(result, fromPaneId);
    if (!isLastMainPane(result, src)) return prunePane(result, fromPaneId);
  }
  return result;
}

/** split a pane into `direction` (row = side by side, column = stacked). */
export function splitPane(
  tree: DockNode,
  paneId: string,
  direction: "row" | "column",
  side: "before" | "after",
): DockNode {
  return insertPane(tree, paneId, direction, side, makePane(null));
}

/** grow the pinned root `shellId` a LOCAL split in `direction`: the shell's classic content
 *  (tabs + active) moves into the first leaf, and the shell keeps marker, fixed px and
 *  contract while its bar empties. Null when already grown or the target vanished. */
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

export function pruneInnerLeaf(tree: DockNode, leafId: string): DockNode {
  const root = rootPaneOf(tree, leafId);
  if (!root || !root.grown) return tree;
  const cut = (n: DockSplit): DockNode => {
    const children: DockNode[] = [];
    for (const c of n.children) {
      if (isPane(c) && c.id === leafId) continue;
      children.push(isSplit(c) ? cut(c) : c);
    }
    // degenerate: the root un-grows empty rather than crashing (roots never die)
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
        n.grown = undefined;
        n.tabs = next.tabs;
        n.active = next.active;
      } else {
        n.grown = next;
      }
    }
  });
}

/** the drop predictor: the tree a drop would produce, or the refusal's reason. Refusals
 *  cover a vanished pane mid-gesture, the split law (sidebar row zones, depth caps, a grown
 *  re-growth) and the LOCKED tab, which never leaves the left shell nor lands fresh. */
export type DockDropPrediction =
  | { tree: DockNode; freshPaneId: string | null }
  | { refused: true; reason: string };

/** THE PANE'S ZONE: which seat of the shell `paneId` is; null = not in this tree. */
export function paneZone(tree: DockNode, paneId: string): PaneZone | null {
  const pane = findPane(tree, paneId);
  if (!pane) return null;
  if (pane.edge) return zoneOfEdge(pane.edge);
  return zoneOfEdge(rootPaneOf(tree, paneId)?.edge ?? null);
}

/** THE ZONE LAW's refusal - the class law, never a per-view conditional; four callers read it. */
export function seatRefusal(
  tree: DockNode,
  component: DockComponentName,
  toPaneId: string,
): string | null {
  const zone = paneZone(tree, toPaneId);
  if (!zone) return "a pane of the gesture vanished";
  if (seatAllowsIn(component, zone)) return null;
  return `${component} never sits in the ${zoneLabel(zone)}`;
}

export function predictDockDrop(
  tree: DockNode,
  fromPaneId: string,
  fromIndex: number,
  toPaneId: string,
  zone: DragZone,
): DockDropPrediction {
  const from = findPane(tree, fromPaneId);
  const to = findPane(tree, toPaneId);
  if (!from || !to || !from.tabs[fromIndex]) {
    return { refused: true, reason: "a pane of the gesture vanished" };
  }
  const component = from.tabs[fromIndex]!.component;
  const seat = seatRefusal(tree, component, toPaneId);
  const split = zoneSplitOf(zone);
  if (!split) {
    // the CENTER zone is the only zone that still self-refuses.
    if (fromPaneId === toPaneId) return { refused: true, reason: "zones never self-dock" };
    // the seat law owns the refusal (moveTab re-checks it).
    if (seat) return { refused: true, reason: seat };
    return { tree: moveTab(tree, fromPaneId, fromIndex, toPaneId, to.tabs.length), freshPaneId: null };
  }
  // a SPLIT lands in the target's zone, so the same gate refuses before the tree is reshaped
  if (seat) return { refused: true, reason: seat };
  // the direction/depth law gates BOTH the shell's growth and an inner split
  const law = splitLawRefusal(tree, toPaneId, split.direction);
  if (law) return { refused: true, reason: law };
  if (isPinnedPane(to)) {
    // the SHELL's own body drop GROWS the root.
    const g = growRoot(tree, toPaneId, split.direction, split.side);
    if (!g) return { refused: true, reason: "the shells keep their shape" };
    if (fromPaneId === toPaneId) {
  // a SAME-pane grow already moved the shell's tabs: seat from the emptied root, not the old index
      const leaf = findPane(g.tree, g.shellLeafId);
      const i = leaf && fromIndex < leaf.tabs.length ? fromIndex : 0;
      return {
        tree: moveTab(g.tree, g.shellLeafId, i, g.freshPaneId, 0),
        freshPaneId: g.freshPaneId,
      };
    }
    return {
      tree: moveTab(g.tree, fromPaneId, fromIndex, g.freshPaneId, 0),
      freshPaneId: g.freshPaneId,
    };
  }
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

// the content viewer: a viewer tab is a MULTI - opening the same file twice FOCUSES it

/** a viewer ref's equality: kind + name, undefined matches undefined */
export function viewerRefEq(a?: ViewerRef, b?: ViewerRef): boolean {
  if (a === undefined || b === undefined) return a === b;
  return a.kind === b.kind && a.name === b.name;
}

/** the FIRST tab showing `v`, active or not - the identity read must see parked tabs. */
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

/** the viewer opener's pure half: focus a tab already showing `v`, else mint into `paneId`. */
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

/** the chat tab a pane is ACTUALLY showing, depth-first: boot focus must carry over the tab the
 *  user left active, not the first chat tab in tree order (a draft may sit at index 0). */
export function activeChatTab(tree: DockNode): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const tab = n.tabs[n.active];
    if (tab?.component === "chat") return { paneId: n.id, index: n.active, tab };
  }
  return null;
}

export function firstDraftChatTab(tree: DockNode): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex((t) => t.component === "chat" && !t.sessionRef);
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

export function isDraftChatKey(tree: DockNode, key: string | null): boolean {
  if (!key) return false;
  const loc = findChatTab(tree, key);
  return loc !== null && !loc.tab.sessionRef;
}

// the /board route owns its host pane's FOREGROUND: the board view folds into the chat pane
// template, which DockPane renders only while chat is the ACTIVE tab (else Board.vue never mounts).

/** the first main pane hosting a chat tab (the /board route prefers it); null = mint a draft. */
export function firstChatMainPaneId(tree: DockNode): string | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n) || isPinnedPane(n)) continue;
    if (n.tabs.some((t) => t.component === "chat")) return n.id;
  }
  return null;
}

/** what the /board route's host pane needs for the board's outlet to exist: chat already
 *  foreground ("none"), a chat tab sitting inactive ("focus", with its index), or no chat
 *  left ("mint"). Pure - applying the plan is the caller's side. */
export type ChatForegroundPlan = { mode: "none" | "focus" | "mint"; index: number };

export function chatForegroundPlan(
  tree: DockNode,
  paneId: string,
): ChatForegroundPlan {
  const pane = findPane(tree, paneId);
  if (!pane) return { mode: "none", index: -1 };
  if (pane.tabs[pane.active]?.component === "chat") {
    return { mode: "none", index: pane.active };
  }
  const index = pane.tabs.findIndex((t) => t.component === "chat");
  if (index >= 0) return { mode: "focus", index };
  return { mode: "mint", index: -1 };
}

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
    n.tabs = tabs.length === 0 ? [newChatTab()] : tabs;
    n.active = clamp(n.active - before, 0, n.tabs.length - 1);
  });
}

export interface DividerDragResult {
  split: DockSplit;
/** the controlled pane dragged past its min here; the pure math cannot prune, the store does. */
  collapsedPaneId: string | null;
}

/** the divider's ADJACENT neighbor absorbs the fixed pane's change 1:1. Without this the flex
 *  reflow spread the delta over EVERY flexible child: the touched pane tracked the pointer at
 *  its share while panes the user never touched moved too. Both are re-derived from the
 *  gesture's FROZEN startPx each event, so the write is idempotent and the sum is preserved. */
function absorbNeighbor(
  next: DockSplit,
  fixedIdx: number,
  absorberIdx: number,
  startPx: number[],
  pairPx: number,
): void {
  const flexIdx: number[] = [];
  next.children.forEach((c, k) => {
    if (!isPane(c) || c.fixed === null) flexIdx.push(k);
  });
  if (flexIdx.length < 2 || !flexIdx.includes(absorberIdx)) return;
  const fixedPx = (next.children[fixedIdx] as DockPane).fixed;
  if (fixedPx === null) return;
  const freeStart = flexIdx.reduce((a, k) => a + (startPx[k] ?? 0), 0);
  const freeNew = freeStart - (fixedPx - (startPx[fixedIdx] ?? 0));
  const targetPx = pairPx - fixedPx;
  if (freeNew <= 0 || targetPx <= 0) return;
  const flexSizes = flexIdx.reduce((a, k) => a + (next.sizes[k] ?? 0), 0);
  if (flexSizes <= 0) return;
  const scale = flexSizes / freeNew;
  const sizes = [...next.sizes];
  for (const k of flexIdx) {
    sizes[k] = scale * (k === absorberIdx ? targetPx : (startPx[k] ?? 0));
  }
  next.sizes = sizes;
}

/** pure divider drag between [i-1, i]: the neighbor never donates - the far side is a 2nd gesture. */
export function dragSplitDivider(
  split: DockSplit,
  dividerIndex: number,
  startPx: number[],
  deltaPx: number,
  contentPx: number,
): DividerDragResult {
  const none = { split, collapsedPaneId: null };
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
  const applySnap = (px: number, lo: number, hi: number): number => {
    // attractor first, then the floors: a snap past the cap lands ON the cap
    const snapped = snapCandidateAt(px, SNAP_STEP);
    return clamp(snapped ?? px, lo, hi);
  };
  const pxOf = (k: number): number => startPx[k]!;

  // past a cap the drag simply stops at the wall (applySnap clamps); panes beyond never move
  const lp = left as DockPane;
  const rp = right as DockPane;
  if (isPane(lp) && lp.fixed !== null) {
    const req = pxOf(li) + deltaPx;
    if (req < MIN_PANE_PX) {
      return { split: next, collapsedPaneId: lp.id };
    }
    // drag controls the LEFT pane; the right one absorbs, its floor is the wall
    const hi = capFor(contractFloorOf(rp));
    (next.children[li] as DockPane).fixed = applySnap(req, MIN_PANE_PX, hi);
    absorbNeighbor(next, li, i, startPx, room);
    return { split: next, collapsedPaneId: null };
  }
  if (isPane(rp) && rp.fixed !== null) {
    // BOTTOM EDGE: the bottom shell is anchored to the viewport's BOTTOM. It follows the
    // pointer 1:1 inside its floors - the ladder belongs to the sidebars alone.
    if (split.direction === "column") {
      const req = pxOf(i) - deltaPx;
      if (req < MIN_PANE_PX) {
        return { split: next, collapsedPaneId: rp.id };
      }
      const hi = Math.min(
        capFor(contractFloorOf(lp)),
        Math.max(MIN_PANE_PX, Math.floor(contentPx * BOTTOM_MAX_FRACTION)),
      );
      (next.children[i] as DockPane).fixed = clamp(req, MIN_PANE_PX, hi);
      absorbNeighbor(next, i, li, startPx, room);
      return { split: next, collapsedPaneId: null };
    }
    const req = pxOf(i) - deltaPx;
    if (req < MIN_PANE_PX) {
      return { split: next, collapsedPaneId: rp.id };
    }
    const hi = capFor(contractFloorOf(lp));
    (next.children[i] as DockPane).fixed = applySnap(req, MIN_PANE_PX, hi);
    absorbNeighbor(next, i, li, startPx, room);
    return { split: next, collapsedPaneId: null };
  }

  // both flexible: write the pair's shares absolutely from the FROZEN start px, scaled by
  // the pair's size sum which this branch alone preserves - event n computes the same
  // fractions from the same baseline, idempotent exactly like the fixed lanes.
  const content = Math.max(1, room);
  // the LEFT pane floors at ITS contract floor, the same law the right side has.
  const pxLo = contractFloorOf(lp);
  const pxHi = Math.max(pxLo, room - contractFloorOf(rp));
  const raw = clamp(pxOf(li) + deltaPx, pxLo, pxHi);
  const rung = travelSnapAt(pxOf(li), raw, SNAP_STEP);
  const finalLeft = clamp(rung ?? raw, pxLo, pxHi);
  // rendered flexible px = contentBox * size / totalFlex.
  const pairSum = (next.sizes[li] ?? 0) + (next.sizes[i] ?? 0);
  const share = pairSum > 0 ? finalLeft / content : 0;
  next.sizes = next.sizes.map((s, k) =>
    k === li ? pairSum * share : k === i ? pairSum * (1 - share) : s,
  );
  return { split: next, collapsedPaneId: null };
}

export function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

// snapCandidate lives in panes.ts (imported at the top); dock.ts must not re-derive.

// v4: records below it were written by the deleted sidebar-split model and get ONE reset
// (resetSidebarShape at load); the bump is what makes the reset happen exactly once.
const LAYOUT_VERSION = 4;

// closing an edge pane HIDES it, it does not delete it: the hide stows the pane's full
// record (tabs, active, fixed, grown) and the reopen restores THE pane, so a tab dragged
// into a sidebar survives the closed window and one dragged out never duplicates back. On
// the record the stow rides the optional `hiddenEdges` field; records predating it load
// unchanged and the reveal falls back to minting the default.
export type HiddenEdgeState = Partial<Record<DockEdgeComponent, DockPane>>;
/** the nominal container px the v2 fraction -> px migration assumes. */
const V2_NOMINAL_CONTAINER_PX = 1000;

function validateClosedEdges(raw: unknown): DockEdgeComponent[] | undefined {
  if (raw === undefined) return undefined;
  if (!Array.isArray(raw)) return [];
  return raw.filter((c): c is DockEdgeComponent =>
    typeof c === "string" && (EDGE_COMPONENTS as readonly string[]).includes(c),
  );
}

/** the stowed pane records' reader: every entry must validate as a pane node carrying its
 *  own edge marker. A corrupted entry DROPS, never the record - the stow is auxiliary, and
 *  failing the whole layout over it would destroy the LIVE tree's record. */
function validateHiddenEdges(
  raw: unknown,
  repairs?: string[],
): HiddenEdgeState | undefined {
  if (raw === undefined || raw === null) return undefined;
  if (typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const markerOf = (comp: string): string =>
    comp === "sidebar" ? "left" : comp === "inspector" ? "right" : "bottom";
  const out: HiddenEdgeState = {};
  let dropped = 0;
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (!(EDGE_COMPONENTS as readonly string[]).includes(key)) continue;
    const node = validateNode(value);
    if (!node || !isPane(node) || node.edge !== markerOf(key)) {
      dropped++;
      continue;
    }
    out[key as DockEdgeComponent] = node;
  }
  if (dropped > 0 && repairs) {
    repairs.push(
      "a closed shell's saved pane record was unreadable and was dropped (its next reopen mints the default)",
    );
  }
  return out;
}

/** the px shares one split's children hold against `containerPx`: fixed panes keep their px,
 *  every other child takes round(remaining * frac / flexible-fraction sum) - the render's own
 *  share law. The fixed test is a finite-number guard, never `!== null`: an undefined/NaN px
 *  once reached disk as null and the strict reader rejected the record. Never emit non-finite. */
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

/** deep copy carrying the v:3 record shape. */
type RecordNode =
  | { kind: "split"; id: string; direction: "row" | "column"; children: RecordNode[]; px: number[] }
  | { kind: "pane"; id: string; tabs: DockTab[]; active: number; fixed: number | null; edge?: "left" | "right" | "bottom"; grown?: RecordNode };

function recordTree(tree: DockNode, containerPx: number): RecordNode {
  const walk = (n: DockNode): RecordNode => {
    if (isPane(n)) {
      return {
        kind: "pane", id: n.id, tabs: n.tabs, active: n.active, fixed: n.fixed,
        ...(n.edge ? { edge: n.edge } : {}),
        // the local split tree rides its own record with the same px-share
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
  /** the closed shells' stowed panes; omitted when empty so stow-free records keep their shape. */
  hiddenEdges?: HiddenEdgeState,
  /** the hidden-tab / hidden-stat visibility sets; omitted when empty, like the stows. */
  hiddenTabs: readonly string[] = [],
  hiddenStats: readonly string[] = [],
): string {
  const stowed = hiddenEdges && Object.keys(hiddenEdges).length
    ? Object.fromEntries(
        Object.entries(hiddenEdges).map(([comp, pane]) => [comp, recordTree(pane, containerPx)]),
      )
    : undefined;
  return JSON.stringify({
    v: LAYOUT_VERSION,
    tree: recordTree(tree, containerPx),
    closedEdges,
    ...(stowed ? { hiddenEdges: stowed } : {}),
    ...(hiddenTabs.length ? { hiddenTabs } : {}),
    ...(hiddenStats.length ? { hiddenStats } : {}),
  });
}

export interface LoadedLayout {
  tree: DockNode;
  version: 1 | 2 | 3 | 4;
  closedEdges?: DockEdgeComponent[];
  hiddenEdges?: HiddenEdgeState;
  /** the persisted visibility sets: hidden secondary-sidebar tabs, hidden statusbar stats */
  hiddenTabs?: string[];
  hiddenStats?: string[];
}

/** validates a persisted layout; null for anything the loader cannot safely render. */
export function deserializeLayout(raw: string | null): LoadedLayout | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
  const rec = parsed as {
    v?: unknown;
    tree?: unknown;
    closedEdges?: unknown;
    hiddenEdges?: unknown;
    hiddenTabs?: unknown;
    hiddenStats?: unknown;
  };
  if (rec.v !== 1 && rec.v !== 2 && rec.v !== 3 && rec.v !== 4) return null;
  const tree = validateNode(rec.tree);
  if (!tree) return null;
  const closedEdges = validateClosedEdges(rec.closedEdges);
  const hiddenEdges = validateHiddenEdges(rec.hiddenEdges);
  return {
    tree,
    version: rec.v as 1 | 2 | 3,
    ...(closedEdges ? { closedEdges } : {}),
    ...(hiddenEdges ? { hiddenEdges } : {}),
    ...(stringList(rec.hiddenTabs).length ? { hiddenTabs: stringList(rec.hiddenTabs) } : {}),
    ...(stringList(rec.hiddenStats).length ? { hiddenStats: stringList(rec.hiddenStats) } : {}),
  };
}

/** lenient reader for the visibility sets: a non-string entry drops, never rejects the record */
function stringList(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const out: string[] = [];
  for (const entry of raw) {
    if (typeof entry === "string" && entry !== "" && !out.includes(entry)) out.push(entry);
  }
  return out;
}

export function deserialize(raw: string | null): DockNode | null {
  return deserializeLayout(raw)?.tree ?? null;
}

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
    if (
      typeof tab.component !== "string" ||
      !(
        (DOCK_COMPONENTS as readonly string[]).includes(tab.component) ||
        (LEGACY_DOCK_COMPONENTS as readonly string[]).includes(tab.component)
      )
    )
      return null;
    // an unreadable viewer payload rejects the record: writer bug or hand corruption
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
  const edge = n.edge === "left" || n.edge === "right" || n.edge === "bottom" ? n.edge : undefined;
  const active = typeof n.active === "number" && Number.isFinite(n.active)
    ? clamp(Math.trunc(n.active), 0, Math.max(0, tabs.length - 1))
    : 0;
  // the local split tree validates like any node
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

/** the strict fraction-sizes read (validateNode's pre-v:3 path). */
function normalizeSizesFromRecord(raw: unknown, width: number): number[] | null {
  if (!Array.isArray(raw)) return null;
  const sizes: number[] = [];
  for (const s of raw as unknown[]) {
    if (typeof s !== "number" || !Number.isFinite(s) || s <= 0) return null;
    sizes.push(s);
  }
  return normalizeSizes(width, sizes);
}

const MAX_LAYOUT_DEPTH = 32;

export interface ParsedLayout {
  tree: DockNode;
  version: 1 | 2 | 3 | 4;
  closedEdges?: DockEdgeComponent[];
  hiddenEdges?: HiddenEdgeState;
  /** the persisted visibility sets: hidden secondary-sidebar tabs, hidden statusbar stats */
  hiddenTabs?: string[];
  hiddenStats?: string[];
  repairs: string[];
}

/** consumes ANY JSON shape, never throwing nor returning null: the fallback boots the default. */
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
  const rec = parsed as {
    v?: unknown;
    tree?: unknown;
    closedEdges?: unknown;
    hiddenEdges?: unknown;
    hiddenTabs?: unknown;
    hiddenStats?: unknown;
  };
  const v = rec.v === 1 || rec.v === 2 || rec.v === 3 || rec.v === 4 ? rec.v : 2;
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
  const hiddenEdges = validateHiddenEdges(rec.hiddenEdges, repairs);
  const hiddenTabs = stringList(rec.hiddenTabs);
  const hiddenStats = stringList(rec.hiddenStats);
  return {
    tree,
    version: v,
    ...(closedEdges ? { closedEdges } : {}),
    ...(hiddenEdges ? { hiddenEdges } : {}),
    ...(hiddenTabs.length ? { hiddenTabs } : {}),
    ...(hiddenStats.length ? { hiddenStats } : {}),
    repairs,
  };
}

interface TolerantEnv {
  depth: number;
  v: 1 | 2 | 3 | 4;
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

function tolerantSplitSizes(
  n: Record<string, unknown>,
  childCount: number,
  repairs: string[],
  v: 1 | 2 | 3 | 4,
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
  const px = norm.map((f) => Math.max(MIN_PANE_PX, Math.round(f * V2_NOMINAL_CONTAINER_PX)));
  const sum = px.reduce((a, s) => a + s, 0);
  const migrated = px.map((p) => p / sum);
  if (migrated.some((f, i) => Math.abs(f - norm[i]!) > 1e-9)) {
    repairs.push(`v2 fraction shares migrated to px (nominal ${V2_NOMINAL_CONTAINER_PX}px, 128 floor)`);
  }
  return migrated;
}

function readWidth(get: (k: string) => string | null, key: string): number | null {
  const raw = get(key);
  if (raw === null || raw === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) && n >= MIN_PANE_PX ? n : null;
}

export function migrateLegacy(
  get: (k: string) => string | null,
): DockNode {
  const fixedFor = (key: string, fallback: number): number => {
    const w = readWidth(get, key);
    return w === null ? fallback : w;
  };
  const sw = fixedFor(LEGACY_WIDTH_KEYS.sidebar, EDGE_DEFAULT_PX.sidebar);
  const iw = fixedFor(LEGACY_WIDTH_KEYS.inspector, EDGE_DEFAULT_PX.inspector);

  const inspector: DockPane = {
    kind: "pane",
    id: nextNodeId(),
    tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
    active: 0,
    fixed: iw,
    edge: "right",
  };
  const primary: DockPane = {
    kind: "pane",
    id: nextNodeId(),
    tabs: PRIMARY_TAB_COMPONENTS.map((c) => ({ component: c })),
    active: 0,
    fixed: sw,
    edge: "left",
  };
  const mainRow: DockNode = {
    kind: "split",
    id: nextNodeId(),
    direction: "row",
    children: [
      primary,
      makePane("chat", null),
      inspector,
    ],
    sizes: normalizeSizes(3, undefined),
  };
  // the fresh boot wraps the row in a column with the bottom panel pinned under it.
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

import { shallowRef, watch, ref, type InjectionKey, type Ref } from "vue";

/** pure graft: replace the split node `replacement` (by id) inside `root`. No id regeneration,
 *  no sibling churn, no in-place mutation on the reactive tree - structuredClone cannot touch
 *  Vue proxies, so the tree stays a shallowRef of PLAIN data re-swapped on every op. */
export function graftSplit(root: DockNode, replacement: DockSplit): DockNode {
  if (isSplit(root) && root.id === replacement.id) return replacement;
  const copy = clone(root);
  // a grown root's local splits graft too
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
          viewerRefEq(t.viewer, b.tabs[i]!.viewer),
      );
    return (
      a.id === b.id &&
      a.active === b.active &&
      (a.fixed ?? null) === (b.fixed ?? null) &&
      (a.edge ?? undefined) === (b.edge ?? undefined) &&
      tabEq &&
      // local split trees compare through the same walk
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
  tree: Ref<DockNode>;
  has: (component: DockComponentName) => boolean;
  bootRepairs: readonly string[];
  setComponent: (paneId: string, component: DockComponentName) => void;
  focus: (paneId: string, index: number) => void;
  openTab: (paneId: string, tab: DockTab) => void;
  closeTab: (paneId: string, index: number) => void;
  moveTab: (fromPaneId: string, fromIndex: number, toPaneId: string, toIndex: number) => void;
  /** the docking drop - release a dragged tab on a zone of another pane's body; refusals
   *  are no-ops. */
  dockDropToZone: (
    fromPaneId: string,
    fromIndex: number,
    toPaneId: string,
    zone: DragZone,
  ) => void;
  seatOk: (component: DockComponentName, paneId: string) => boolean;
  zoneOf: (paneId: string) => PaneZone | null;
/** THE ROUTING LAW: a sidebar NEVER hosts a session; a non-main id falls to the main column. */
  sessionPane: (preferred?: string | null) => string;
  undoLayout: () => boolean;
  beginResize: () => void;
/** the dockable contract opener: singletons FOCUS their one instance when it lives; multi goes
 *  through the app's chat opener, so call this for singletons only. */
  openSingleton: (paneId: string, component: DockComponentName) => "created" | "focused";
  setSessionRef: (key: string, sessionRef: string | null) => void;
  closeSession: (sessionRef: string) => void;
/** open (or FOCUS) the viewer tab for `v`: a file or skill already shown anywhere is focused,
 *  never duplicated; otherwise one mints into the ensured main pane. */
  openViewer: (v: ViewerRef) => void;
  findChatTab: (key: string) => ChatTabLocation | null;
  firstChatTab: (sessionRef?: string) => ChatTabLocation | null;
  /** the first DRAFT chat tab - the '+' new flow focuses it instead of minting a duplicate. */
  firstDraftChatTab: () => ChatTabLocation | null;
  activeChatTab: () => ChatTabLocation | null;
  split: (paneId: string, direction: "row" | "column", side: "before" | "after") => void;
  remove: (paneId: string) => void;
  toggleEdge: (component: "sidebar" | "inspector", side: "before" | "after") => void;
  toggleBottom: () => void;
  /** the bottom shell's fullscreen view; session-only, a reload never boots fullscreen. */
  bottomFullscreen: Ref<boolean>;
  toggleBottomFullscreen: () => void;
  ensureMainPane: () => string;
  /** whether the pane's X may remove it: pinned panes never, and the LAST MAIN pane never. */
  canRemovePane: (paneId: string) => boolean;
  isLastMainPane: (paneId: string) => boolean;
  /** divider drag; returns the drag's close intents, which the store already applied. */
  drag: (
    splitId: string,
    dividerIndex: number,
    startPx: number[],
    deltaPx: number,
    contentPx: number,
  ) => { collapsedPaneId: string | null };
  /** the window-narrow give-way: while `contentPx` cannot seat every fixed pane plus the
   *  flexible floors, the inspector collapses first, then the sidebar; each hide remembers
   *  its width so the toggle restores what the resize took. Never re-reveals. */
  narrowSqueeze: (contentPx: number) => void;
  revealRefused: Ref<"sidebar" | "inspector" | null>;
  /**
   * THE VISIBILITY SETS: hiding is a flag, never a close - the tab keeps its seat and its
   * state, the chip just stops rendering. Both sets live in the layout record, so a reload
   * keeps them. `openSingleton` clears a tab's flag: opening a view is asking to see it.
   */
  hiddenTabs: Ref<readonly string[]>;
  setTabHidden: (component: string, hidden: boolean) => void;
  hiddenStats: Ref<readonly string[]>;
  setStatHidden: (id: string, hidden: boolean) => void;
  /** THE RUNTIME MODULE GATE: idempotent, safe on every module-state change. */
  setModuleGate: (gate: ModuleGate) => void;
}

export const DOCK_API_KEY: InjectionKey<DockApi> = Symbol("omega-dock-api");

export const DOCK_CHAT_OPEN_KEY: InjectionKey<
  (paneId: string, sessionId: string | null) => void
> = Symbol("omega-dock-chat-open");
// the app resolves chat tab titles to their session label (the bar follows the FOCUSED tab);
// null falls back to the generic COMPONENT_TITLES entry.
export const DOCK_TAB_TITLE_KEY: InjectionKey<(tab: DockTab) => string | null> =
  Symbol("omega-dock-tab-title");

// The NEW-CONVERSATION DIM: the doors keep rendering, dimmed and disabled, never removed -
// aria-disabled + tabindex -1, the click still focuses the draft.
export const DOCK_DRAFT_FOCUS_KEY: InjectionKey<Ref<boolean>> = Symbol(
  "omega-dock-draft-focus",
);

export const EDGE_DEFAULT_PX: Record<"sidebar" | "inspector", number> = {
  sidebar: 264,
  inspector: 264,
};

export function createDock(store: Storage): DockApi {
  const stored = store.getItem(DOCK_LAYOUT_KEY);
  const loaded = parseAnyLayout(stored, () => migrateLegacy((k) => store.getItem(k)));
  const closedEdges: DockEdgeComponent[] =
    loaded?.closedEdges ??
    (store.getItem(BOTTOM_CLOSED_KEY) === "1" ? ["bottom"] : []);
  store.removeItem(BOTTOM_CLOSED_KEY);
  // stow chat-tab keys must seed the tab counter: nextTabKey never mints a key a stow holds
  const hiddenState: HiddenEdgeState = {};
  // the gate's parked seats ride MODULE_STASH_KEY, so the layout record never carries a ghost
  let parkedTabs: ParkedTab[] = parseParkedTabs(store.getItem(MODULE_STASH_KEY));
  let liveGate: ModuleGate = EMPTY_MODULE_GATE;
  let revealPending: "sidebar" | "inspector" | null = null;
  // the latest pane-area px the resize watcher measured - the reveal's fit-check reads it
  let lastContentPx: number | null = null;
  const revealRefused = ref<"sidebar" | "inspector" | null>(null);
  if (loaded?.hiddenEdges) {
    for (const comp of Object.keys(loaded.hiddenEdges) as DockEdgeComponent[]) {
      const pane = loaded.hiddenEdges[comp];
      // (ONE-TIME) a sidebar stow carrying a LOCAL SPLIT TREE records the deleted split-sidebar
      // model - it never comes back
      if (loaded.version < 4 && (comp === "sidebar" || comp === "inspector") && pane?.grown) continue;
      if (pane) {
        pane.tabs = pane.tabs.filter((t) => t.component !== "settings");
        hiddenState[comp] = pane;
        syncTabKeySeed(pane);
      }
    }
  }
  let initial = loaded.tree;
  if (loaded.version === 1) {
    initial = upgradeInspectorTabs(expandInspectorTabs(initial));
  } else {
    initial = expandInspectorTabs(initial);
  }
  // sync the id seed BEFORE boot repair mints panes: repair-first let ensurePinnedEdges mint
  // a fresh "n1" beside the loaded tree's own, and every later id-keyed op would act on both.
  syncIdSeed(initial);
  initial = rehomeInsightsTab(initial);
  initial = retireInspectorSettings(initial);
  // THE SHAPE LAW (ONE-TIME): derive the markers first (idempotent - a v1 record has none, and
  // the reset must see the shells the repair is about to heal), then reset both sidebars BEFORE
  // the seat repair so restoreSeatSet re-seats the class members in the reset shells on this
  // same boot; the v4 re-persist below keeps the reset from running again.
  if (loaded.version < 4) {
    const marked = deriveEdgeMarkers(initial, closedEdges);
    const reset = resetSidebarShape(marked);
    if (reset !== marked) {
      loaded.repairs.push(
        "the sidebar shape law arrived: a split sidebar was reset to the clean default (center and bottom layouts kept)",
      );
    }
    initial = reset;
  }
  // the CLOSED shells' stows count as the tree's composition while they sit hidden
  const stowedComponents = Object.keys(hiddenState) as DockEdgeComponent[];
  initial = ensurePinnedEdges(
    initial,
    closedEdges,
    stowedComponents.flatMap((c) => (hiddenState[c]?.tabs ?? []).map((t) => t.component)),
  );
  // the bottom panel is grafted AFTER the sidebars repair - it hosts no chat tabs, and its id
  // must seed above the loaded tree's max (syncIdSeed ran once)
  initial = ensureBottomPanel(initial, closedEdges);
  const syncedBottom = syncBottomHostTabs(initial);
  if (syncedBottom !== initial) {
    loaded.repairs.push(
      "a persisted bottom shell predates a bottom-home registration: the missing default tabs were appended",
    );
    initial = syncedBottom;
  }
  initial = assignChatKeys(initial);
  // a fresh chat comes back for either dead pane state, never a blank "New Tab" body
  const seated = repairPaneSeats(initial);
  if (seated !== initial) {
    loaded.repairs.push(
      "a persisted pane carried no live tab (a retired new-tab seat or a zero-tab pane): a new chat was seated",
    );
    initial = seated;
  }
  // BACKUP LAW: the repaired record's raw form is copied ONCE, before the auto-persist below
  if (stored && loaded.repairs.length > 0) {
    store.setItem(DOCK_LAYOUT_BAK_KEY, stored);
  }
  // THE VISIBILITY SETS (hidden secondary-sidebar tabs, hidden statusbar stats): flags in
  // the same record the tree persists to, so a reload keeps them.
  const hiddenTabs = ref<readonly string[]>(loaded?.hiddenTabs ?? []);
  const hiddenStats = ref<readonly string[]>(loaded?.hiddenStats ?? []);
  // the ONE gated writer: every record write re-reads through the total deserializer.
  const gatedPersist = (t: DockNode) => {
    const candidate = serialize(
      t,
      closedEdges,
      V2_NOMINAL_CONTAINER_PX,
      hiddenState,
      hiddenTabs.value,
      hiddenStats.value,
    );
    const back = parseAnyLayout(candidate, () => t);
    if (!treeShapesMatch(back.tree, t)) {
      console.warn("dock: layout round-trip mismatch - the write was refused, last-good kept");
      return;
    }
    store.setItem(DOCK_LAYOUT_KEY, candidate);
  };
  const setTabHidden = (component: string, hidden: boolean) => {
    const flagged = hiddenTabs.value.includes(component);
    if (hidden === flagged) return;
    hiddenTabs.value = hidden
      ? [...hiddenTabs.value, component]
      : hiddenTabs.value.filter((c) => c !== component);
    gatedPersist(tree.value);
  };
  const setStatHidden = (id: string, hidden: boolean) => {
    const flagged = hiddenStats.value.includes(id);
    if (hidden === flagged) return;
    hiddenStats.value = hidden
      ? [...hiddenStats.value, id]
      : hiddenStats.value.filter((s) => s !== id);
    gatedPersist(tree.value);
  };
  if (stored) gatedPersist(initial);
  // shallowRef: the tree stays PLAIN data (structuredClone rejects proxies).
  const tree = shallowRef(initial) as Ref<DockNode>;

  // persist the WHOLE tree on every mutation.
  watch(
    tree,
    (t) => {
      gatedPersist(t);
    },
    { deep: true },
  );

  /** apply the runtime module gate: park gated tabs out of the tree AND out of the closed
   *  shells' stows (a hidden inspector must not reveal a dead tab later), then re-seat what
   *  the live modules host. A park that empties a pane is left to repairPaneSeats. */
  const syncModuleGate = (gate: ModuleGate) => {
    liveGate = gate;
    let t = tree.value;
    const parked = parkGatedTabs(t, gate);
    t = parked.tree;
    if (parked.parked.length) parkedTabs = mergeParked(parkedTabs, parked.parked);
    for (const comp of Object.keys(hiddenState) as DockEdgeComponent[]) {
      const stowed = hiddenState[comp];
      if (!stowed) continue;
      const gated = parkGatedTabs(stowed, gate);
      if (gated.parked.length === 0) continue;
      parkedTabs = mergeParked(parkedTabs, gated.parked);
      hiddenState[comp] = gated.tree as DockPane;
    }
    const reseat = reseatParkedTabs(t, parkedTabs, gate);
    t = reseat.tree;
    parkedTabs = reseat.parked;
    if (t !== tree.value) tree.value = repairPaneSeats(t);
    store.setItem(MODULE_STASH_KEY, JSON.stringify(parkedTabs));
  };

  const setClosed = (comp: DockEdgeComponent, closed: boolean) => {
    const at = closedEdges.indexOf(comp);
    if (closed && at < 0) closedEdges.push(comp);
    if (!closed && at >= 0) closedEdges.splice(at, 1);
  };

  const has = (component: DockComponentName): boolean =>
    // singleton semantics: exists anywhere, active or not - an active-only read would duplicate
    findComponentTab(tree.value, component) !== null;

  // the undo ring: every committed LAYOUT op - content ops except a close that MERGES
  const UNDO_RING_MAX = 20;
  const undoRing: string[] = [];
  const undoPush = (t: DockNode) => {
    const s = serialize(t, closedEdges, V2_NOMINAL_CONTAINER_PX, hiddenState);
    if (undoRing[undoRing.length - 1] === s) return;
    undoRing.push(s);
    if (undoRing.length > UNDO_RING_MAX) undoRing.shift();
  };
  /** swap in a NEW tree from a layout op; a no-op contributes no snapshot. */
  const applyLayout = (before: DockNode, next: DockNode) => {
    if (next === before) return;
    undoPush(before);
    tree.value = next;
    // any layout mutation ends an armed gesture: the record must never outlive its event
    endGesture();
  };
  const undoLayout = (): boolean => {
    const s = undoRing.pop();
    if (!s) return false;
    const rec = parseAnyLayout(s, () => tree.value);
    syncIdSeed(rec.tree);
    const restored = assignChatKeys(rec.tree);
    if (rec.closedEdges) {
      closedEdges.splice(0, closedEdges.length, ...rec.closedEdges);
    }
    for (const k of Object.keys(hiddenState) as DockEdgeComponent[]) {
      delete hiddenState[k];
    }
    if (rec.hiddenEdges) {
      Object.assign(hiddenState, rec.hiddenEdges);
    }
    tree.value = restored;
    // a snapshot predates the gate; re-apply it so undo never resurrects a parked chip
    syncModuleGate(liveGate);
    endGesture(); // undo mid-gesture: the record's indices are pre-restore
    return true;
  };

  const openSingletonIn = (
    paneId: string,
    component: DockComponentName,
  ): "created" | "focused" => {
    const res = openSingleton(tree.value, paneId, component);
    if (res.action !== "noop") tree.value = res.tree;
    // opening a view is asking to SEE it: a hidden chip comes back with its pane
    setTabHidden(component, false);
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
    // content op EXCEPT when the close merges: only structural closes enter the ring
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
  const openViewer = (v: ViewerRef) => {
    const pane = ensureMainPane();
    const res = openViewerTab(tree.value, v, pane);
    tree.value = res.tree;
  };
  const split = (paneId: string, direction: "row" | "column", side: "before" | "after") => {
    // the direction/depth law gates the menu path too: a refused split is a no-op
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
    // a leaf inside a grown root merges away (the root unwinds, never dies)
    if (rootPaneOf(tree.value, paneId)) return true;
    return !isPinnedPane(pane) && mainPaneCount(tree.value) > 1;
  };
  const seatOk = (component: DockComponentName, paneId: string): boolean =>
    seatRefusal(tree.value, component, paneId) === null;
  const zoneOf = (paneId: string): PaneZone | null => paneZone(tree.value, paneId);
  // THE ROUTING LAW: a session opens in the main column, never in a sidebar
  const sessionPane = (preferred?: string | null): string => {
    if (preferred && paneZone(tree.value, preferred) === "main") return preferred;
    return firstChatMainPaneId(tree.value) ?? ensureMainPane();
  };
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
    if (!node) return { collapsedPaneId: null };
    const d = dragSplitDivider(node, dividerIndex, startPx, deltaPx, contentPx);
    // the divider close hides and persists: the intent prunes, prunePane is hideEdgePane's body
    if (d.split !== node) tree.value = graftSplit(tree.value, d.split);
    for (const id of [d.collapsedPaneId]) {
      if (!id) continue;
      const gone = findPane(tree.value, id);
      if (gone) {
        const comp = edgeComponentOf(gone);
        if (comp) {
          if (gone.fixed !== null) rememberEdgePx(store, comp, gone.fixed);
          hiddenState[comp] = clone(gone);
          setClosed(comp, true);
        }
      }
      tree.value = prunePane(tree.value, id);
    }
    return { collapsedPaneId: d.collapsedPaneId };
  };

  const toggleEdge = (component: "sidebar" | "inspector", side: "before" | "after") => {
    const pane = findEdgePane(tree.value, component);
    if (pane) {
      undoPush(tree.value);
      if (pane.fixed !== null) rememberEdgePx(store, component, pane.fixed);
      hiddenState[component] = clone(pane);
      setClosed(component, true);
      tree.value = hideEdgePane(tree.value, component);
    } else {
      // the fit-check runs BEFORE any state mutates.
      const stowed = hiddenState[component] ?? null;
      const res = tryRevealEdgePane(
        tree.value,
        component,
        side,
        recalledEdgePx(store, component),
        stowed,
        lastContentPx,
      );
      if (!res.revealed) {
        revealRefused.value = component;
        return;
      }
      // reopen-clamp: a stale sub-floor remembered width reopens AT 320
      setClosed(component, false);
      delete hiddenState[component];
      // the reveal owns the NEXT squeeze.
      revealPending = component;
      applyLayout(tree.value, res.tree);
      syncModuleGate(liveGate);
    }
  };

  const toggleBottom = () => {
    if (findEdgePane(tree.value, "bottom")) {
      // the pre-hide snapshot first (closed set intact)
      undoPush(tree.value);
      const pane = findEdgePane(tree.value, "bottom");
      if (pane) hiddenState.bottom = clone(pane);
      tree.value = hideBottomPanel(tree.value, store);
      setClosed("bottom", true);
    } else {
      setClosed("bottom", false);
      const stowed = hiddenState.bottom ?? null;
      delete hiddenState.bottom;
      applyLayout(tree.value, revealBottomPanel(tree.value, store, stowed));
      // the restacked strip re-runs the gate, or a seat parked while closed never returns
      syncModuleGate(liveGate);
    }
  };

  // the bottom shell's FULLSCREEN toggle: the band leaves the flow (absolute over the pane-area
  // between the sidebar insets) and the column keeps rendering its split children, so the main row
  // and both sidebars stay live. Session-only: a reload never boots into fullscreen.
  const bottomFullscreen = ref(false);
  const toggleBottomFullscreen = () => {
    bottomFullscreen.value = !bottomFullscreen.value;
  };

  // the give-way's width goes through the toggle's own memory, and the closure persists
  const narrowSqueeze = (contentPx: number) => {
    lastContentPx = contentPx;
    const justRevealed = revealPending;
    revealPending = null;
    const res = narrowGiveWay(tree.value, contentPx, justRevealed);
    if (!res.hidden.length) return;
    // the pre-give-way snapshot first
    undoPush(tree.value);
    for (const h of res.hidden) {
      rememberEdgePx(store, h.component, h.px);
      // a hide like the menubar toggle's: the stow is what the reopen restores
      hiddenState[h.component] = h.pane;
      setClosed(h.component, true);
    }
    tree.value = res.tree;
  };

  return {
    bootRepairs: loaded.repairs,
    tree, has, setComponent, focus, openTab: openTabIn, closeTab: closeTabIn, moveTab: moveTabIn,
    dockDropToZone: dockDropToZoneIn, seatOk, zoneOf, sessionPane,
    openSingleton: openSingletonIn,
    setSessionRef, closeSession, openViewer,
    findChatTab: (key: string) => findChatTab(tree.value, key),
    firstChatTab: (sessionRef?: string) => firstChatTab(tree.value, sessionRef),
    firstDraftChatTab: () => firstDraftChatTab(tree.value),
    activeChatTab: () => activeChatTab(tree.value),
    split, remove, toggleEdge, ensureMainPane, canRemovePane, isLastMainPane: lastMainPane, drag, toggleBottom,
    bottomFullscreen, toggleBottomFullscreen,
    narrowSqueeze,
    revealRefused,
    hiddenTabs,
    setTabHidden,
    hiddenStats,
    setStatHidden,
    undoLayout, beginResize: () => undoPush(tree.value),
    setModuleGate: syncModuleGate,
  };
}
