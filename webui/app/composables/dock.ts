// omega webui: the dock - the layout tree.
// VS Code-style: any pane splits into rows/columns, unlimited depth, and the
// whole tree persists under omega.dock-layout (per-browser localStorage; the
// URL owns ONLY the focused chat session - app.vue's concern). Tabs and DnD
// are card 2; this module is the tree: pure, render-free, unit-testable.
//
// Node model:
//   split { direction: row|column, children: [node], sizes: [fraction] }
// pane { tabs: [{ component, sessionRef? }], active, fixed: px|null }
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
  MENU_DOCKABLES,
  COMPONENT_TITLES,
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
  zonesOf,
  type DockArea,
  type DockableComponentLike,
  type DockComponentName,
  type ModuleGate,
  type ViewerRef,
} from "./dockables.ts";
// the openable component universe, the menus' refill inventory and the tab-title.
export {
  DOCK_COMPONENTS,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  MENU_DOCKABLES,
  COMPONENT_TITLES,
  PRIMARY_TAB_COMPONENTS,
  dockableMinPx,
  isClosableTab,
  isLockedTab,
  seatAllowsIn,
  zonesOf,
};
export type { DockArea, DockComponentName, DockableComponentLike, ViewClass, ViewerRef } from "./dockables.ts";
// re-export for consumers that think in dock terms (tests, toggle helpers)
export { PANE_OPEN_MIN };

export const DOCK_LAYOUT_KEY = "omega.dock-layout";
/**
 * v0.14.4 BACKUP LAW: before the first auto-persist of a REPAIRED layout, the raw
 * record is copied here - a repair-chain bug must never destroy the only copy of the
 * user's layout.
 */
export const DOCK_LAYOUT_BAK_KEY = "omega.dock-layout.bak";

// pre-dock pane state.
export const LEGACY_WIDTH_KEYS = {
  sidebar: "omega.sidebar-width",
  inspector: "omega.inspector-width",
} as const;

// the pre-existing drag floor (both sidebars dragged to 128 before the squeeze chain.
export { MIN_PANE_PX, CHAT_FLOOR_PX } from "./dockables.ts";
/**
 * Endo's comment: the bottom shell's max share of the split's content box.
 * A drag past it (the pane swallowing the window) strands the divider offscreen - the
 * pane is bottom-anchored, so its TOP edge (= the divider) climbs out of view and
 * cannot be grabbed back.: 0.65 still leaves the chat a cramped strip under the
 * composer (the max-up screenshot); 0.5 keeps the pane and the chat honest halves - the
 * anti-stranding rationale only gets SAFER as the number drops, and the divider
 * climbing out of view is the high-share failure mode.
 */
export const BOTTOM_MAX_FRACTION = 0.5;

// -- leaf contracts ------------------------------------

/**
 * a leaf's sizing constraints.
 * A pane shipping NO contract resolves to today's constants exactly: fixed -> the 128
 * drag floor, flexible -> the 320 wall, the +320 attractor ladder (the bottom keeps its
 * +200 ladder), no ceiling except the shells' geometry (the bottom's max share).
 * The contract is serialized in the layout record from v:3 on; relaxation order keys on
 * the pane class (shells 2, chat-hosting 1, plain 0) - not carried here (it rides the
 * record, milestone v:3).
 */
export interface PaneContract {
  /** the hard floor: a drag/squeeze never starves the pane below it */
  minPx: number;
  maxPx: number | null;
  maxFrac: number | null;
  preferred: number;
  anchor: "leading" | "trailing" | "bottom";
}

/** the primary sidebar's factory. */
export function edgeShellContract(): PaneContract {
  return { minPx: MIN_PANE_PX, maxPx: null, maxFrac: null, preferred: 320, anchor: "leading" };
}

/** the chat pane's factory: the 320 wall, flexible otherwise. */
export function chatPaneContract(): PaneContract {
  return { minPx: CHAT_FLOOR_PX, maxPx: null, maxFrac: null, preferred: 0, anchor: "leading" };
}

/** the bottom shell's factory. */
export function bottomShellContract(): PaneContract {
  return { minPx: MIN_PANE_PX, maxPx: null, maxFrac: BOTTOM_MAX_FRACTION, preferred: 200, anchor: "bottom" };
}

/**
 * the drag floor a pane contracts to RIGHT NOW.
 * Fixed panes drag down to the shared 128 floor; a flexible pane walls at honest pane
 * floor (paneFloorOf - the contract min lifted by its worst hosted tab), so a tool-only
 * flexible pane drags to 128 and chat keeps the 320 wall.
 * A nested split absorbs like a flexible pane STILL (the flat 320): minDockPx recursion
 * here would leak a row's HORIZONTAL sum into a vertical axis (the bottom shell's
 * column drag proved it), so the honest split floors are axis-aware work - a future
 * card, not this one.
 */
export function contractFloorOf(node: DockNode): number {
  if (!isPane(node)) return CHAT_FLOOR_PX; // a nested split absorbs like a flexible pane
  if (node.fixed !== null) return MIN_PANE_PX;
  return paneFloorOf(node);
}

/**
 * a flexible pane's HONEST floor - the worst floor over the tabs it HOSTS (a pane that
 * might show chat keeps chat room; a pane with only tool tabs floats at 128, Endo's
 * 250px screenshot proved the room was real), lifted by its own contract's minPx when
 * larger, and the chat wall when EMPTY (an empty pane's next tenant may be the chat -
 * the old law held there, and the fixtures hold it).
 * One consumer family: contractFloorOf (the drag clamps) and minDockPx (the reveal's
 * "no room" check) both read this - "no room" now fires exactly when a drag could not
 * free the space either.
 */
export function paneFloorOf(pane: DockPane): number {
  let floor = pane.contract?.minPx ?? MIN_PANE_PX;
  for (const t of pane.tabs) floor = Math.max(floor, dockableMinPx(t.component));
  if (pane.tabs.length === 0) floor = Math.max(floor, CHAT_FLOOR_PX);
  return floor;
}

// component names panes may host.

export interface DockTab {
  component: DockComponentName;
  sessionRef?: string;
  key?: string;
  /**
   * WHAT a viewer tab shows - the file path or skill name.
   * Viewer tabs mint with it; nothing else carries it; a viewer tab without a viewer
   * payload is unreachable (the menus can't mint one) and the reader drops it.
   */
  viewer?: ViewerRef;
}
export interface DockPane {
  kind: "pane";
  id: string;
  tabs: DockTab[];
  active: number;
  fixed: number | null;
  edge?: "left" | "right" | "bottom";
  /**
   * v0.14.4 the leaf's sizing constraints.
   * Optional - an absent contract resolves to today's constants via contractFloorOf,
   * which is the pre-contract behavior verbatim.
   */
  contract?: PaneContract;
  /**
   * v0.14.4 the PINNED ROOT's local split tree.
   * Root geometry - fixed px, contract, edge marker, hide/fullscreen modes - never
   * moves down.
   */
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
  sizes: number[];
  /**
   * v:3 RECORD only: the children's integer px shares on disk.
   * Never render: DockSplit.vue reads sizes.
   */
  px?: number[];
}
export type DockNode = DockSplit | DockPane;

// -- ids ------------------------------------------------------------------

let idSeed = 0;
/**
 * deterministic id generator; seed it from a loaded tree so reloaded sessions never.
 */
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

// -- chat tab instance keys ------------------------------

let tabKeySeed = 0;
/**
 * tab keys use their own c-prefixed namespace so they never collide with pane/split
 * node ids.
 */
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

/**
 * boots every chat tab to the key invariant the host relies on: chat tabs minted
 * anywhere that carry no key get one right there - mutates, so callers run it on PLAIN
 * loaded data before the ref is set (and persist the result).
 */
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

// -- small building blocks --------------------------------------------------

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

// -- THE LOCKED LAST TAB ----------
// "the plan was that i should not be able to close the last 'new chat' tab
// .. it should basically be locked.. the current approach is just empty
// bad looking space." - the eyeball on the never-zero placeholder
// commit 8eabefc7) a close used to seat in the emptied pane.
//
// One law, three sentences:
// a pane's LAST tab never closes: closeTab refuses the request, and the
// X on that tab renders DISABLED and DIMMED instead of vanishing
// supersedes 's invisible refusal - the lock is
//     VISIBLE now, the law itself unchanged). The lock IS how the pane
//     stays non-empty - nothing has to be seated in its place;
// nothing is minted to fill an empty pane. A pane emptied by a GESTURE
//     (a split, a tab dragged out of a pane that cannot close) renders the
//     bar-less empty state DockPane already had and a drop refills it; the
//     15-button empty-pane menu stays deleted (DOCK-EMPTY-ACTIONS);
// a pane REPAIRED into emptiness seats a fresh new-chat tab - a
//     persisted placeholder seat (retired below) or a pane whose last chat
//     died with its deleted session. That is the chat the user asked for,
//     not a "New Tab" sticker.
//
// A GROWN root's classic bar is not a pane for this law:
// its content lives in the local leaves, its own cell stays empty by
// design.

/**
 * THE LOCKED LAST TAB's single condition.
 * closeTab guards on this predicate and DockPane's dimmed X reads it through the same
 * one, so the surface can never drift from the refusal.
 */
export function lastTabLocked(pane: DockPane): boolean {
  return pane.tabs.length <= 1;
}

/** the retired never-zero placeholder id - LEGACY now (dockables.ts). */
const LEGACY_NEW_TAB = "newtab";

function isLegacySeat(component: string): boolean {
  return component === LEGACY_NEW_TAB;
}

/** a fresh, unsaved new-chat tab. */
function newChatTab(): DockTab {
  return { component: "chat", key: nextTabKey() };
}

/**
 * the boot's pane-seat repair.
 * Two dead states land here, one law fixes both: - a persisted never-zero placeholder
 * seat retires - dropped where it shared a pane with real content, converted into a
 * fresh new-chat tab where it sat alone; - a persisted ZERO-tab pane (a pre-placeholder
 * record) seats the same fresh chat rather than booting a blank pane.
 * Idempotent; a tree with neither dead state is returned as the SAME object.
 */
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
      // the seat was sharing the pane with real content.
      const focus = active && !isLegacySeat(active.component) ? active : real[0]!;
      n.tabs = real;
      n.active = Math.max(0, real.indexOf(focus));
    }
  });
}

// -- THE MODULE GATE -------------------------------------- The runtime dismounts a.
// The architecture bar then says the module's surfaces are GONE, so a module-owned
// dockable whose module is off leaves the strip: its tab is PARKED - taken out of the
// tree and remembered, not deleted - and re-seated exactly where it sat when the module
// mounts again. Parked tabs ride their own key, never the layout record: the record
// stays what is LIVE (no ghost pane in omega.dock-layout), the stash is what is merely
// unhosted. Deleting and re-creating tabs instead would lose the user's seat (a tab
// dragged into a main pane must come back there).

/** the parked-tab stash (a module-off dockable's saved seat) */
export const MODULE_STASH_KEY = "omega.dock-module-stash";

/** one parked tab: WHAT it hosted, WHERE it sat (pane id + seat index) */
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

/**
 * the stash reader: lenient like every persisted reader - junk entries are dropped,
 * never thrown.
 */
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

/**
 * park every gated tab: the tab leaves its pane's strip, the seat is recorded.
 * Idempotent; a tree with nothing gated comes back as the SAME object (the store's
 * identity no-op checks depend on that).
 */
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
    // the focus follows whatever held it; a gated ACTIVE tab lands on the
    // seat's first survivor (a pane the gate emptied is left to the seat
    // repair, exactly like any zero-tab pane)
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

/**
 * where a parked tab goes back.
 * A main-area tab with no main pane stays parked - the gate never mints panes (opening
 * the dockable from a menu does).
 */
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

/** re-seat every parked tab whose module is live again. */
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
    // a singleton already hosted (re-opened from a menu, restored by the boot's
    // inspector repair) absorbs the parked seat - one instance, no duplicate
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

/**
 * one parked seat per component (module-owned dockables are singletons: a second park
 * of the same chip would be a duplicate seat)
 */
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

/** every node, depth-first, root first. */
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

/**
 * the first pane hosting `component` as its active tab (or any tab when no tabs are
 * active-matched), or null.
 */
export function findComponentPane(tree: DockNode, component: DockComponentName): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.tabs[n.active]?.component === component) return n;
  }
  return null;
}

/**
 * the FIRST tab hosting `component` anywhere in the tree - the singleton lookup: a
 * singleton exists at most once, active or not, so its finders must read every tab, not
 * just active ones.
 */
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

// -- the pane-model reset ---------
// three identities the whole dock enforces:
// pinned pane an EDGE SHELL (left = sessions home, right = inspector
//                 home) carrying the `edge` marker: never closable, never
//                 removable, never movable. Its CONTENT is free - any tab
// may live in it or leave it, except
//                 the locked sessions tab, which lives ONLY in "left".
// main pane everything else - the closable class, hover-X pans, the
//                 '+' menus.
// never-empty the dock always holds at least one pane; refusals are
//                 no-ops (the tree comes back untouched). The law extends
//                 to tabs: a pinned shell CAN sit empty (it renders the
//                 empty '+' affordance) - it never disappears.

export function paneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) if (isPane(node)) n++;
  return n;
}

/** a pane is PINNED when it carries an edge marker - the sessions/inspector shells. */
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

/**
 * Endo ruling: the main-pane census.
 * Pinned shells don't count - the close law is about the MAIN area, which must never
 * reach zero panes (the launcher pane IS a main pane; the pane itself is always right,
 * only its close button was the bug).
 */
export function mainPaneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) {
    if (isPane(node) && !isPinnedPane(node)) n++;
  }
  return n;
}

/** the close-refusal predicate: `pane` is a main pane and no OTHER main pane exists. */
export function isLastMainPane(tree: DockNode, pane: DockPane): boolean {
  return !isPinnedPane(pane) && mainPaneCount(tree) <= 1;
}

// -- local split trees in the pinned roots -------------- The three roots (primary
// sidebar, secondary sidebar, bottom panel) grow LOCAL split trees - the same
// DockSplit/DockPane machinery the main pane uses, wrapped inside the root so the
// root's geometry (fixed px, contract, resize band, hide/fullscreen modes) never
// changes. Canvas C2's SHAPE LAW narrows the SIDEBARS to one shape: tabs + a STACK
// (column splits only - a sidebar never puts two columns of content side by side,
// because the shell's two columns ARE the two sidebars, and the deleted split-sidebar
// gesture has no entry point left). Roots never die: a degenerate local tree un-grows
// back into the shell's classic single cell.

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

/** the grown ROOT pane whose local tree hosts `paneId`, or null. */
export function rootPaneOf(tree: DockNode, paneId: string): DockPane | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.grown && findPane(n.grown, paneId)) return n;
  }
  return null;
}

/** split levels from a grown root's split down to the leaf's parent. */
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

/**
 * v0.14.4 /3 + canvas C2 THE SHAPE LAW: may `direction` split `paneId` HERE?
 * A sidebar - the shell itself or a leaf of its grown stack - STACKS (column) and never
 * splits into side-by-side columns; main + bottom take both, and the tight roots cap at
 * two split levels.
 */
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

/**
 * a main pane for the bottom-panel era.
 * The root is a COLUMN now (the main row rides on the pinned bottom child); a session
 * pane must go into the ROW, not beside the whole column.
 */
export function insertMainPaneIntoRow(tree: DockNode): { tree: DockNode; paneId: string } | null {
  if (!isSplit(tree) || tree.direction !== "column") return null;
  const rowIdx = tree.children.findIndex((c) => isSplit(c) && c.direction === "row");
  if (rowIdx < 0) return null;
  const copy = clone(tree);
  const row = copy.children[rowIdx] as DockSplit;
  row.children.push(makePane(null));
  row.sizes = normalizeSizes(row.children.length, row.sizes);
  // the minted pane starts EMPTY - the opener (app.vue) mints the tab that lands in it
  // (openChatTab / openSingleton), and no placeholder sticker is seated (the never-zero
  // seat retired)
  return {
    tree: copy,
    paneId: (row.children[row.children.length - 1] as DockPane).id,
  };
}

/**
 * the opener's insert: delegates the column-rooted case to the row push (the bottom
 * panel must NOT gain a main pane beside the whole column); the row-rooted/pane-rooted
 * shapes take the classic path.
 */
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

/** insights changed HOME. */
export function rehomeInsightsTab(tree: DockNode): DockNode {
  let stranded = false;
  for (const n of allNodes(tree)) {
    if (
      isPane(n) &&
      n.edge !== "right" &&
      // a leaf inside the RIGHT ROOT is right-hosted too (v0.14.4 m4.
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

/**
 * the pinned shells are fixed chrome: a loaded tree that somehow lost one of the two (a
 * pre-marker tree) gets it re-inserted at its edge, and the loader's rulings are
 * re-asserted: sessions ALWAYS sits in the left shell's bar, and inspector-area
 * singletons missing from the whole tree return to the right shell (the default
 * arrangement fallback - a seat that only lives on demand, like settings behind the
 * menubar gear, is never in that set).
 */
export function ensurePinnedEdges(
  tree: DockNode,
  closed: readonly DockEdgeComponent[] = [],
  /**
   * components recorded inside CLOSED shells' stowed panes - the stow IS the tree's
   * composition while the shell sits hidden, so the missing-set restoration must never
   * re-seat them (the shell's own reopen restores exactly its stow, never a duplicate).
   */
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
  // canvas C1: BOTH sidebars heal from the same law.
  result = restoreSeatSet(result, "left", stowed);
  return restoreSeatSet(result, "right", stowed);
}

/**
 * pre-marker trees were identified by content fingerprint.
 * Only fires for the FIRST content match of an unclaimed side, so a legit main pane
 * hosting relocated sidebar tabs is never grabbed - and a side the record says the user
 * closed is never minted either.
 * The PRIMARY shell is anchored by its class's LOCKED view (sessions - the one view
 * that can never leave it, so a pane hosting it IS that shell) and never by a visiting
 * class companion (agents, cron may sit anywhere, so their presence says nothing about
 * which pane is the shell).
 * Idempotent; no-op returns the SAME tree.
 */
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

/** the loader's lock enforcement. */
export function forceSessionsLeft(tree: DockNode): DockNode {
  const left = findEdgePane(tree, "sidebar");
  if (!left) return tree; // repair mints it above - no shell, nothing to force
  // v0.14.4 /4: a GROWN left root's bar is its leaf hosting sessions (the shell's.
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

/** the default-arrangement fallback. */
export function restoreSeatSet(
  tree: DockNode,
  zone: "left" | "right",
  /**
   * components a CLOSED shell's stow records - never re-seated (the reveal restores.
   */
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
  // v0.14.4 a grown root seats the restored set in the leaf that already hosts this.
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

/**
 * canvas C2 THE SHAPE LAW's ONE-TIME migration.
 * A sidebar WITHOUT a stack tree is not the deleted model's work and loads untouched
 * (the break is exactly as wide as the feature was).
 * Center and bottom layouts SURVIVE UNTOUCHED: their splits, sizes and grown trees are
 * legal shapes and come back exactly as persisted.
 */
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
    // an empty bar + no local tree.
    n.tabs = [];
    n.active = 0;
    delete n.grown;
  });
}

/**
 * insert `pane` at the root's edge of a row (a pane root or a column root wraps into.
 */
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

/**
 * pre-reset trees persisted the inspector as ONE tab hosting its own chip strip;
 * dissolved the strip.
 */
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

/**
 * the inspector's tab set GREW.
 * Trees persisted while the inspector held five tabs get them back: every pane hosting
 * a right-area tab gains the missing right-area tabs, in the registry's canonical
 * order.: settings LEFT the set again (the menubar gear opens it as a main tab) - this
 * pass never touches a settings tab, and since "settings" is absent from the rank map a
 * pane holding only settings no longer reads as an inspector host
 * (retireInspectorSettings owns that seat on every load).
 */
export function upgradeInspectorTabs(tree: DockNode): DockNode {
  const rank = new Map(INSPECTOR_TAB_COMPONENTS.map((c, i) => [c, i]));
  // a pane is the pre-marker inspector host when it carries a right-area
  // id; the pre-move main-pane settings seat never makes one
  const isInspectorTab = (c: DockComponentName) => rank.has(c);
  let done = false;
  const copy = clone(tree);
  for (const n of allNodes(copy)) {
    if (!isPane(n)) continue;
    const hosts = n.tabs.some((t) => isInspectorTab(t.component));
    const next = n.tabs
      // missing right-area tabs join the pane (the inspector group's tab list is fixed
      // chrome.
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

/** settings left the inspector. */
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

// -- the singleton opener --------------------------------- One contract rule, one pure
// function. Multi components (chat) never come through here: they mint a tab via the
// app's opener every time.

export type OpenSingletonAction = "created" | "focused" | "noop";

export function openSingleton(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): { tree: DockNode; action: OpenSingletonAction } {
  // locked components have no open path: sessions lives because the loader puts it
  // there, never because a menu opened it
  if (isLockedTab(component)) return { tree, action: "noop" };
  const found = findComponentTab(tree, component);
  if (found) {
    // focus it in place - even when the target pane is this one (a re-click on '+'.
    return { tree: focusTab(tree, found.paneId, found.index), action: "focused" };
  }
  const pane = findPane(tree, paneId);
  if (!pane) return { tree, action: "noop" }; // target vanished - no-op
  // canvas C1: the target pane's ZONE must seat the view (the class law) -
  // a sidebar never opens a chat, the main pane never opens the logs
  if (seatRefusal(tree, component, paneId) !== null) return { tree, action: "noop" };
  const next = pane.tabs.length === 0
    ? setPaneComponent(tree, paneId, component)
    : openTab(tree, paneId, { component });
  return { tree: next, action: "created" };
}

// -- pure tree operations ---------------------------------------------------- Each
// returns a NEW tree (deep-cloned inputs are never mutated).

function clone<T>(tree: T): T {
  return structuredClone(tree);
}

function cloneTreeWith(tree: DockNode, fn: (node: DockNode) => void): DockNode {
  const copy = clone(tree);
  for (const n of allNodes(copy)) fn(n);
  return copy;
}

/** insert `pane` as a sibling of target, splitting the target with `direction`. */
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

/**
 * remove a pane by id.
 * Endo ruling: the LAST MAIN pane refuses too - the main area never reaches zero panes,
 * whatever path the request took (button, shortcut, API call, stale state).
 * Shells don't count toward the census: closing them may still leave no main pane,
 * which the store's ensureMainPane repairs on the next open.
 */
export function removePane(tree: DockNode, paneId: string): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane)) return tree;
  // v0.14.4 a leaf inside a grown root prunes WITHIN the root (prunePane's split walk
  // cannot see grown children). Roots never die.
  if (rootPaneOf(tree, paneId)) return pruneInnerLeaf(tree, paneId);
  if (isLastMainPane(tree, pane)) return tree;
  return prunePane(tree, pane.id);
}

/**
 * the shared prune body: cut `paneId`, hoist a lone survivor, repair sizes.
 * Callers enforce the WHO-MAY-GO policy; this is only mechanics.
 */
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

// -- the edge toggle --------------------------------------- the pane reset made the
// pinned panes untouchable via the generic removePane/closeTab refusals; the menubar
// toggles get a dedicated, tightly-guarded route so hide/reopen survives the reset's
// invariants.: shells are identified by their `edge` MARKER now - content is free, so
// the old tab fingerprint cannot survive.

/**
 * THE CLASS <-> SHELL ADAPTER (canvas C1): the edge component a home zone names.
 * ONE map - handing a zone name to findEdgePane silently answers the bottom pane, so no
 * caller may spell the translation itself.
 */
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

/** the dedicated pinned shell for `component`. */
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

/**
 * the toggle's hide half: the marked shell goes as a whole (whatever tabs it hosted are
 * lost with it.
 * Guards mirror the fixed shape: the marker found, never-empty held.
 */
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
  // the sidebars' reopen clamp lifts a stale sub-floor width to 320; the
  // bottom panel's heights are smaller scale - floor at the drag floor
  // (128), never at the sidebar's open clamp
  return component === "bottom" ? Math.max(MIN_PANE_PX, px) : openClamp(px);
}

/** which edge component a pane's marker matches (null. */
export function edgeComponentOf(pane: DockPane): EdgeComponent | null {
  return pane.edge === "left" ? "sidebar"
    : pane.edge === "right" ? "inspector"
    : pane.edge === "bottom" ? "bottom"
    : null;
}

// -- window-narrow give-way -------------------------------- Divider drags already
// enforce the priority floors per gesture, but a plain WINDOW resize skips the math
// entirely: the fixed sidebars keep their px, and the flexible main pane shrinks under
// its 320px contract floor (or the fixed sum overflows the pane-area). A wider window
// never re-reveals (the user reopens by toggle, like a drag-close).

/** the minimum horizontal px a subtree needs at its axis. */
export function minDockPx(node: DockNode): number {
  if (isPane(node)) return node.fixed !== null ? node.fixed : paneFloorOf(node);
  const reqs = node.children.map(minDockPx);
  const dividers = node.direction === "row" ? (reqs.length - 1) * 4 : 0;
  return node.direction === "row"
    ? reqs.reduce((a, b) => a + b, 0) + dividers
    : Math.max(0, ...reqs);
}

/** one give-way step's record: which edge pane collapsed at what width */
export interface NarrowHiddenEntry {
  component: "sidebar" | "inspector";
  px: number;
  pane: DockPane;
}

/**
 * pure window-narrow give-away: while `contentPx` cannot seat every fixed pane plus.
 * When the tree already fits (or only the two edge panes are gone and it still
 * doesn't), it stops - user-made splits are never pruned by a resize.: `justRevealed` -
 * the component a user JUST revealed - is exempt from the give-away.
 */
export function narrowGiveWay(
  tree: DockNode,
  contentPx: number,
  justRevealed: "sidebar" | "inspector" | null = null,
): { tree: DockNode; hidden: NarrowHiddenEntry[] } {
  const hidden: NarrowHiddenEntry[] = [];
  let t = tree;
  // default: secondary first (the inspector gives way before the sessions pane).
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

/**
 * the toggle's reopen half: the shell comes back at the root's edge - same builders
 * ensurePinnedEdges uses, so reopen and boot-repair mint IDENTICAL panes.: the reopened
 * right shell mints only the inspector tabs NOT already living elsewhere - a relocated
 * singleton is never duplicated by a reopen.
 */
export function revealEdgePane(
  tree: DockNode,
  component: "sidebar" | "inspector",
  side: "before" | "after",
  fixedPx?: number | null,
  /**
   * the closed shell's stowed pane - reopen restores THE pane (its tabs, active, fixed,
   * grown come back exactly as the hide captured them) instead of minting a fresh
   * default.
   * The stow's own fixed px wins over `fixedPx` (the caller recalls the same width
   * anyway); nothing stowed (or an edge-marker mismatch) falls back to the default
   * minting below.
   */
  hidden?: DockPane | null,
  /**
   * the pane-area's measured content px - a reveal the window cannot seat is refused
   * BEFORE any state mutates (the caller gets the SAME tree back).
   * null/absent = no measurement in play (callers without a viewport) and the reveal
   * proceeds as today.
   */
  contentPx?: number | null,
): DockNode {
  const edge = component === "sidebar" ? "left" : "right";
  const fixed = Math.max(PANE_OPEN_MIN, fixedPx ?? EDGE_DEFAULT_PX[component]);
  const fresh = () =>
    component === "sidebar" ? makePane("sidebar", fixed, "left") : defaultInspectorPane(tree, fixed);
  let pane: DockPane = fresh();
  if (hidden && hidden.edge === edge) {
    const revived = revivePane(hidden, fixed, PANE_OPEN_MIN);
    // the no-duplication law re-checked on the way back in.
    revived.tabs = revived.tabs.filter((t) => !findComponentTab(tree, t.component));
    if (revived.tabs.length > 0) {
      revived.active = clamp(revived.active, 0, revived.tabs.length - 1);
      pane = revived;
    } // every restored tab already lives elsewhere: mint the default
  }
  // (wall law: a gesture stops at a wall, never surprises): a reveal whose result
  // cannot fit is checked against what the watcher's give-way could actually free - the
  // overflow that matters lives INSIDE a grown root split, where hideEdgePane frees
  // nothing and the insert would be reaped on sight. contentPx omitted = no measurement
  // in play (callers without a viewport) and the reveal proceeds as today.
  const candidate = insertRootEdge(tree, side, pane);
  if (
    contentPx !== null &&
    contentPx !== undefined &&
    Number.isFinite(contentPx) &&
    minDockPx(candidate) > contentPx
  ) {
    // can the root-edge give-away ever seat it? The protected pane (this
    // component) is NEVER reaped; every OTHER root edge gives way. If the
    // floor of that fully-relieved tree still exceeds the content px, the
    // watcher could never reach a fitting state (the real overflow lives
    // INSIDE a grown root split, which reaps nothing) - the reveal is
    // hopeless: the caller gets the SAME tree back, zero churn.
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

/**
 * the reveal's decision wrapper: the tree AND whether it revealed (the refusal returns
 * the caller's tree reference - the toggle stays closed with zero churn and surfaces a
 * one-line "no room" hint instead of a silent dead button).
 */
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

/**
 * the restore twin of the minters: the stowed pane comes back id-for-id fresh (its ids
 * may have been re-minted in the live tree while it sat hidden - duplicate ids would
 * make every later id-keyed op act on BOTH), reopened at `floor` (the caller's reopen
 * clamp) or its own recorded px when it carries one.
 */
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

/** the fresh left shell (canvas C1). */
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

// -- the bottom panel --------------------------------------- the horizontal pane
// pinned under the main panel.

/**
 * closed-flag persistence: the bottom panel's hide (toggle or divider push) survives.
 */
export const BOTTOM_CLOSED_KEY = "omega.dock-bottom-closed";

/**
 * the fresh bottom shell: a fixed-height edge pane seeded from the registry's bottom.
 */
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

/**
 * graft the bottom shell in: the tree becomes the column's first child.
 * Idempotent only via the caller (ensureBottomPanel); a shell that already sits in the
 * tree should not reach here.
 */
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

/**
 * the boot repair's bottom-home strip SYNC.
 * The whole-tree read (not strip-only) keeps the singleton law: a default the user
 * parked in another pane is never duplicated - it is seated, just elsewhere.
 * Persisted trees heal on the next load; a fully-seated shell returns the SAME tree
 * (idempotent).
 */
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

/**
 * the toggle's hide half (the bottom domain).
 * The closed-edge marking is the caller's (createDock owns the set that rides the
 * record).
 */
export function hideBottomPanel(tree: DockNode, store: Storage): DockNode {
  const pane = findEdgePane(tree, "bottom");
  if (!pane) return tree;
  if (pane.fixed !== null) rememberEdgePx(store, "bottom", pane.fixed);
  return prunePane(tree, pane.id);
}

/** the toggle's reopen half. */
export function revealBottomPanel(
  tree: DockNode,
  store: Storage,
  hidden?: DockPane | null,
): DockNode {
  const restored = recalledEdgePx(store, "bottom");
  let shell: DockPane;
  if (hidden && hidden.edge === "bottom") {
    const revived = revivePane(hidden, Math.max(MIN_PANE_PX, restored ?? BOTTOM_DEFAULT_PX), MIN_PANE_PX);
    // the no-duplication law, same as the sidebars' reveal
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

/** move a pane next to a target pane. */
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
  // Endo ruling: a refused detach (the last main pane never goes) must not fall through
  // to the insert - the pane never left, so inserting its clone would duplicate it
  // beside itself
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

/**
 * give an EMPTY pane its first component (the '+' menu path), replacing tabs wholesale.
 * Emptying back is closeTab's job.: the LOCKED sessions tab never enters a pane this
 * way, and unknown ids refuse (the safe class).
 * canvas C1: the pane's ZONE decides too - a pane only takes a view its zone seats (the
 * class law), so an empty sidebar refills with sidebar views, never with chat.: no seat
 * sweep rides the fill any more - the pane's tab set is exactly what this op wrote.
 */
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

/**
 * open a new tab in a pane, appending at the end and activating it (the tab-bar '+'
 * menu path.
 * canvas C1: THE SEAT MAP decides - a view opens only in a zone its class allows, so a
 * chat tab can never be minted into a sidebar and a bottom view never opens in the main
 * pane.
 */
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

/**
 * close a tab.: a pane's LAST tab never closes - the request is refused.
 * That refusal is the whole never-zero law: no close can empty a pane, so nothing is
 * seated in its place (the 8eabefc7 "new tab" placeholder is retired).: an UNCLOSEABLE
 * tab refuses too - the pinned sidebars' tabs have no X at all.
 * v0.14.4 an inner leaf leaves the tree through its own remove X (canRemovePane is true
 * for a grown root's leaves), never by closing its last tab - the lock holds there as
 * everywhere.
 */
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

/**
 * card 2 DnD: move a tab between positions/panes.
 * `toIndex` is the insertion index into the TARGET pane's tabs AFTER the tab has been
 * lifted out of the source (for a same-pane reorder, the callers pass the post-removal
 * index - clamped to the remaining range).: when a cross-pane move takes the source
 * pane's LAST tab, the source pane closes with it (same result as the manual remove
 * button) - a non-pinned main pane never lingers empty from a move.
 * Pinned shells stay empty-but-present (the shells may sit empty; the never-empty law
 * keeps the last pane alive via prunePane's guard).
 */
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
  // canvas C1: THE ZONE LAW - a tab only lands where its class allows. A same-pane
  // reorder never changes the seat (it is already there), so the gate covers cross-pane
  // landings only.
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
      // the moved tab was active.
      if (n.active === fromIndex)
        n.active = Math.min(n.active, Math.max(0, n.tabs.length - 1));
      else if (n.active > fromIndex) n.active -= 1;
    } else if (n.id === toPaneId) {
      const at = clamp(toIndex, 0, n.tabs.length);
      n.tabs = [...n.tabs.slice(0, at), tab, ...n.tabs.slice(at)];
      n.active = at;
    }
  });
  // the last tab out closes the source pane (the manual remove button's result,
  // automatic) - a main pane never lingers empty from a move. The guards live in the
  // law itself: pinned shells stay (they may sit empty), the last remaining pane stays
  // (prunePane's never-empty guard).
  if (result === tree) return tree;
  const src = findPane(result, fromPaneId);
  if (src && !isPinnedPane(src) && src.tabs.length === 0) {
    // v0.14.4 an inner leaf that emptied MERGES AWAY (the grown root unwinds)
    if (rootPaneOf(result, fromPaneId)) return pruneInnerLeaf(result, fromPaneId);
    if (!isLastMainPane(result, src)) return prunePane(result, fromPaneId);
  }
  // whatever survived the drain simply sits empty.
  return result;
}

/** split a pane into `direction` (row = side by side, column = stacked).. */
export function splitPane(
  tree: DockNode,
  paneId: string,
  direction: "row" | "column",
  side: "before" | "after",
): DockNode {
  return insertPane(tree, paneId, direction, side, makePane(null));
}

/**
 * v0.14.4 grow the pinned root `shellId` a LOCAL split in `direction` (the shell's.
 * The shell's classic content (tabs + active) moves into the first leaf - the shell
 * keeps marker, fixed px and contract and empties its bar - and the fresh leaf takes
 * the zone's side.
 * Null for any shell that already grown or a vanished/gone target (the caller reports
 * the refusal).
 */
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

/** v0.14.4 prune an inner leaf from its grown root. */
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
        n.grown = undefined;
        n.tabs = next.tabs;
        n.active = next.active;
      } else {
        n.grown = next;
      }
    }
  });
}

/**
 * v0.14.4 (the load-bearing law).
 * The split zones self-drop now, so refusals: the center zone's self-dock (body drops
 * of a pane's own tab keep the focus-only read), a vanished pane mid-gesture, the
 * shells' split-zones the law refuses (sidebar row zones, depth caps, a grown
 * re-growth), and the LOCKED tab - it never leaves the left shell and never lands in a
 * fresh pane.
 */
export type DockDropPrediction =
  | { tree: DockNode; freshPaneId: string | null }
  | { refused: true; reason: string };

/**
 * THE PANE'S ZONE (canvas C1): which seat of the shell `paneId` is.
 * null = the pane is not in this tree (the caller's refusal).
 */
export function paneZone(tree: DockNode, paneId: string): PaneZone | null {
  const pane = findPane(tree, paneId);
  if (!pane) return null;
  if (pane.edge) return zoneOfEdge(pane.edge);
  return zoneOfEdge(rootPaneOf(tree, paneId)?.edge ?? null);
}

/**
 * THE ZONE LAW's refusal (canvas C1).
 * ONE predicate read by the drag hit-test, moveTab, the drop predictor and the openers
 * - the class law, never a per-view conditional.
 */
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
  // canvas C1: ONE gate for both zone shapes - the class law's seat check
  const seat = seatRefusal(tree, component, toPaneId);
  const split = zoneSplitOf(zone);
  if (!split) {
    // the CENTER zone is the only zone that still self-refuses.
    if (fromPaneId === toPaneId) return { refused: true, reason: "zones never self-dock" };
    // the seat law owns the refusal (moveTab re-checks it).
    if (seat) return { refused: true, reason: seat };
    return { tree: moveTab(tree, fromPaneId, fromIndex, toPaneId, to.tabs.length), freshPaneId: null };
  }
  // a SPLIT lands in the target's zone (a shell grows its own root - the fresh leaf
  // inherits it; a main pane's fresh sibling is main), so the same gate refuses before
  // the tree is reshaped around a landing that could never happen
  if (seat) return { refused: true, reason: seat };
  // v0.14.4 -3: the direction/depth law gates BOTH the shell's growth and an inner
  // leaf's split.
  const law = splitLawRefusal(tree, toPaneId, split.direction);
  if (law) return { refused: true, reason: law };
  if (isPinnedPane(to)) {
    // the SHELL's own body drop GROWS the root (its tabs move into the first leaf.
    const g = growRoot(tree, toPaneId, split.direction, split.side);
    if (!g) return { refused: true, reason: "the shells keep their shape" };
    if (fromPaneId === toPaneId) {
      // a SAME-pane shell grow already moved the shell's tabs wholesale into shellLeaf.
      // Seat it from the emptied root instead and moveTab would silently no-op (the
      // drag would "vanish" into the wholesale move).
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
  // split the target (an inner leaf or a main pane), then seat the tab.
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

/**
 * the commit path: a thin read of the predictor.
 * Any refusal is the tree unchanged, exactly as before the predictor existed.
 */
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

// -- the content viewer ----------------------------------- A viewer tab is a MULTI.
// Focus-reuse is the law (the chat-precedent singleton feel without limiting tabs):
// opening the same file/skill twice FOCUSES the existing tab wherever it lives and
// never mints a duplicate.

/** a viewer ref's equality: kind + name, undefined matches undefined */
export function viewerRefEq(a?: ViewerRef, b?: ViewerRef): boolean {
  if (a === undefined || b === undefined) return a === b;
  return a.kind === b.kind && a.name === b.name;
}

/**
 * the FIRST tab in the tree showing `v` (active or not - like the singleton finders,
 * the identity read must see parked tabs).
 */
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

/**
 * the viewer opener's pure half: a tab already showing `v` focuses wherever it lives;
 * otherwise one mints into `paneId` (the caller's ensureMainPane target - the same main
 * pane a session would open in).
 */
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

// -- chat tab session binding ------------------------------

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

/**
 * the first chat tab in the tree, depth-first (boot/restore + fallback focus helpers.
 */
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

/**
 * the chat tab a pane is ACTUALLY showing (tabs[pane.active]), depth-first.: boot focus
 * must carry over the tree's chat focus - the tab the user left active - not merely the
 * first chat tab in tree order: the shipped default pane holds a sessionless "new chat"
 * draft at index 0 while the user's session tab sits ACTIVE at index 1, and the draft
 * pick left the focused-session seam null forever (the Files tab booted on the
 * default/cwd fallback and the re-root watch never fired).
 */
export function activeChatTab(tree: DockNode): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const tab = n.tabs[n.active];
    if (tab?.component === "chat") return { paneId: n.id, index: n.active, tab };
  }
  return null;
}

/** the first DRAFT chat tab (no session binding. */
export function firstDraftChatTab(tree: DockNode): ChatTabLocation | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n)) continue;
    const index = n.tabs.findIndex((t) => t.component === "chat" && !t.sessionRef);
    if (index >= 0) return { paneId: n.id, index, tab: n.tabs[index]! };
  }
  return null;
}

/** whether the chat tab `key` IS the draft conversation (no session binding yet). */
export function isDraftChatKey(tree: DockNode, key: string | null): boolean {
  if (!key) return false;
  const loc = findChatTab(tree, key);
  return loc !== null && !loc.tab.sessionRef;
}

// (journey 98): the /board route owns its host pane's FOREGROUND. The board view folds
// into the chat pane template, which DockPane renders only while the pane's ACTIVE tab
// is chat - a persisted content pane (the browser singleton) must not shadow the route,
// or Board.vue never mounts on a /board reload.

/**
 * the first main (non-pinned) pane that hosts a chat tab, depth-first.
 * The /board route prefers a chat-hosting main pane over the first main pane; null =
 * every main pane is content-only (the caller falls back to ensureMainPane and mints a
 * draft).
 */
export function firstChatMainPaneId(tree: DockNode): string | null {
  for (const n of allNodes(tree)) {
    if (!isPane(n) || isPinnedPane(n)) continue;
    if (n.tabs.some((t) => t.component === "chat")) return n.id;
  }
  return null;
}

/**
 * what the /board route's host pane needs for the board's outlet to exist: chat is
 * already the foreground ("none"), a chat tab sits inactive in the pane ("focus", with
 * its index), or the pane lost every chat tab ("mint" - the caller mints the draft chat
 * there).
 * Pure: reading the plan, not the mutation, is the caller's side.
 */
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

/** write the session id back onto the chat tab that owns `key`. */
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

/** close EVERY chat tab bound to a session (the delete flow). */
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

// -- resize -----------------------------------------------------------------

export interface DividerDragResult {
  split: DockSplit;
  /**
   * the controlled pane dragged PAST its min at this divider - a hide intent (the pane
   * model's close.
   * The pure math cannot prune (it only sees the split); the store applies the intent
   * against the tree.
   */
  collapsedPaneId: string | null;
}

/**
 * the divider's ADJACENT neighbor absorbs the fixed pane's change 1:1.
 * The fixed lanes used to write only their own `fixed` px and let the flex reflow
 * spread the delta over EVERY flexible child: the pane the divider touches then tracked
 * the pointer at its SHARE of the flex space (a small pane beside a big one shrank at a
 * fraction of the drag - Endo's "the terminal shrinks slow" - while a pane the user
 * never touched moved too).
 * Both are re-derived from the gesture's FROZEN startPx each event, so the write is
 * idempotent like the fixed lanes themselves, and the sizes sum is preserved (later
 * events re-read the same total).
 */
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

/**
 * pure divider drag: between children [i-1, i] of `split`.
 * If the far pane must donate space, the user moves its divider: two gestures, zero
 * surprises.
 */
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
  const applySnap = (px: number, lo: number, hi: number, step: number = SNAP_STEP): number => {
    // attractor first, then the floors again: a snap point that overshoots
    // the cap lands ON the cap, never past it
    const snapped = snapCandidateAt(px, step);
    return clamp(snapped ?? px, lo, hi);
  };
  const pxOf = (k: number): number => startPx[k]!;

  // the wall: with the squeeze chain gone, `req` past a cap simply stops at the wall
  // (applySnap clamps) - the neighbor never donates, the panes beyond it never move.
  const lp = left as DockPane;
  const rp = right as DockPane;
  if (isPane(lp) && lp.fixed !== null) {
    const req = pxOf(li) + deltaPx;
    // drag-past-min: the controlled pane closes.
    if (req < MIN_PANE_PX) {
      return { split: next, collapsedPaneId: lp.id };
    }
    // drag controls the LEFT pane; right absorbs: its floor is the wall
    // (128 for fixed, 320 for a flexible pane with no contract)
    const hi = capFor(contractFloorOf(rp));
    (next.children[li] as DockPane).fixed = applySnap(req, MIN_PANE_PX, hi);
    absorbNeighbor(next, li, i, startPx, room);
    return { split: next, collapsedPaneId: null };
  }
  if (isPane(rp) && rp.fixed !== null) {
    // BOTTOM EDGE: the bottom shell is anchored to the viewport's BOTTOM.
    if (split.direction === "column") {
      const req = pxOf(i) - deltaPx;
      if (req < MIN_PANE_PX) {
        return { split: next, collapsedPaneId: rp.id };
      }
      // the absorbing neighbor above: the resolver's floor (128 fixed, 320 flexible).
      const hi = Math.min(
        capFor(contractFloorOf(lp)),
        Math.max(MIN_PANE_PX, Math.floor(contentPx * BOTTOM_MAX_FRACTION)),
      );
      (next.children[i] as DockPane).fixed = applySnap(req, MIN_PANE_PX, hi, BOTTOM_SNAP_STEP);
      absorbNeighbor(next, i, li, startPx, room);
      return { split: next, collapsedPaneId: null };
    }
    const req = pxOf(i) - deltaPx;
    if (req < MIN_PANE_PX) {
      return { split: next, collapsedPaneId: rp.id };
    }
    // mirror (the old invert=true divider).
    const hi = capFor(contractFloorOf(lp));
    (next.children[i] as DockPane).fixed = applySnap(req, MIN_PANE_PX, hi);
    absorbNeighbor(next, i, li, startPx, room);
    return { split: next, collapsedPaneId: null };
  }

  // both flexible: re-derive BOTH adjacent fractions from the frozen baseline's
  // intended px each event. Writing the pair's shares absolutely - (finalLeft, room -
  // finalLeft) as fractions of the pair's own px box, scaled by the pair's size sum
  // which this branch alone preserves - makes event n compute the same fractions from
  // the same frozen baseline: idempotent, exactly the fixed lanes' law.
  const content = Math.max(1, room);
  // the LEFT pane floors at ITS contract floor, the same law the right side has always
  // had.
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

// -- persistence --------------------------------------------------------------

// v2 - shell panes carry the `edge` marker. Records below 4 were written by the deleted
// sidebar-split model and get ONE reset (resetSidebarShape, run by the loader); the
// bump is what makes the reset happen exactly once.
const LAYOUT_VERSION = 4;

// closing an edge pane HIDES state, it does not delete it. The hide stows the pane's
// full record (tabs, active, fixed, grown) and the reopen restores THE pane, not a
// fresh default - a tab dragged into a sidebar survives the closed window, and a tab
// dragged out never duplicates back in. In-flight the stow is the pre-prune pane
// object; on the record it rides the `hiddenEdges` field (edge component -> pane
// record), optional like `closedEdges` was: records that predate the field load
// unchanged and the reveal falls back to minting the default exactly as before.
export type HiddenEdgeState = Partial<Record<DockEdgeComponent, DockPane>>;
/** v:2 -> v:3 migration container. */
const V2_NOMINAL_CONTAINER_PX = 1000;

/** the closed-edge flags on a layout record. */
function validateClosedEdges(raw: unknown): DockEdgeComponent[] | undefined {
  if (raw === undefined) return undefined;
  if (!Array.isArray(raw)) return [];
  return raw.filter((c): c is DockEdgeComponent =>
    typeof c === "string" && (EDGE_COMPONENTS as readonly string[]).includes(c),
  );
}

/**
 * the stowed pane records' reader: every entry must validate as a pane node AND carry.
 * A corrupted entry DROPS, never the record (the stow is auxiliary state; failing the
 * whole layout over it would destroy the LIVE tree's record to save the hidden one).
 */
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

/**
 * the px shares one split's children hold against `containerPx`.
 * Fixed panes keep their px; every other child (a flexible pane, a nested split) takes
 * round(remaining * frac/sum of the flexible fractions) - the render's own share law,
 * so a recorded px re-derives the exact fractions it came from.: the fixed test is a
 * finite-number guard (isFixed: typeof number AND Number.isFinite), never an identity
 * check against null - a pane carrying fixed === undefined (or NaN) previously passed
 * `fixed !== null` and the emit returned the undefined raw, JSON turned it into a null
 * px entry, and the strict reader rejected the WHOLE record the writer itself produced
 * (layout reset on every reload).
 * This function must never emit a non-finite px; anything unreadable takes the flexible
 * branch.
 */
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
        // v0.14.4 the local split tree rides ITS OWN record with the same px-share.
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
  /**
   * the closed shells' stowed panes.
   * Omitted when empty: records that never stowed a pane keep today's exact shape.
   */
  hiddenEdges?: HiddenEdgeState,
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
  });
}

export interface LoadedLayout {
  tree: DockNode;
  version: 1 | 2 | 3 | 4;
  closedEdges?: DockEdgeComponent[];
  hiddenEdges?: HiddenEdgeState;
}

/**
 * validates + parses a persisted layout.
 * Returns null for anything the loader cannot safely render (old/foreign/corrupt) so
 * callers migrate.
 */
export function deserializeLayout(raw: string | null): LoadedLayout | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
  const rec = parsed as { v?: unknown; tree?: unknown; closedEdges?: unknown; hiddenEdges?: unknown };
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
  };
}

/** the plain tree-only read (every caller that does not care about the persisted. */
export function deserialize(raw: string | null): DockNode | null {
  return deserializeLayout(raw)?.tree ?? null;
}

/** the px shares of a RAW split record -> fractions, or null when the shares cannot. */
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
    // v:3 record: the children's shares ride px (fractions derive).
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
    // legacy "inspector" tabs deserialize (pre-reset trees carry them).
    if (
      typeof tab.component !== "string" ||
      !(
        (DOCK_COMPONENTS as readonly string[]).includes(tab.component) ||
        (LEGACY_DOCK_COMPONENTS as readonly string[]).includes(tab.component)
      )
    )
      return null;
    // a viewer payload present but unreadable rejects the whole record (the strict
    // reader; it can only be writer bug or hand corruption - the tolerant path repairs
    // instead)
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
  // the shell identity persists (v2).
  const edge = n.edge === "left" || n.edge === "right" || n.edge === "bottom" ? n.edge : undefined;
  const active = typeof n.active === "number" && Number.isFinite(n.active)
    ? clamp(Math.trunc(n.active), 0, Math.max(0, tabs.length - 1))
    : 0;
  // v0.14.4 the local split tree validates like any node.
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

// -- the TOTAL deserializer --------------------------------

const MAX_LAYOUT_DEPTH = 32;

export interface ParsedLayout {
  tree: DockNode;
  version: 1 | 2 | 3 | 4;
  closedEdges?: DockEdgeComponent[];
  hiddenEdges?: HiddenEdgeState;
  repairs: string[];
}

/**
 * consumes ANY JSON shape and hands back a renderable tree - no throw path, no null
 * (the caller's `fallback` boots the default arrangement, ALWAYS receipted).
 */
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
  const rec = parsed as { v?: unknown; tree?: unknown; closedEdges?: unknown; hiddenEdges?: unknown };
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
  return {
    tree,
    version: v,
    ...(closedEdges ? { closedEdges } : {}),
    ...(hiddenEdges ? { hiddenEdges } : {}),
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

/** a split's fractions from whatever the record carries. */
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

// -- migration (first boot without a dock key) --------------------------------

function readWidth(get: (k: string) => string | null, key: string): number | null {
  const raw = get(key);
  if (raw === null || raw === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) && n >= MIN_PANE_PX ? n : null;
}

/** Seeds the tree from the pre-dock persisted pane state.. */
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
  // canvas C1: the fresh primary shell seats its CLASS (sessions first in registry
  // order, then its companions).
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

// -- store --------------------------------------------------------------

import { shallowRef, watch, ref, type InjectionKey, type Ref } from "vue";

/**
 * pure graft: replace the split node `replacement` (by id) inside `root` and return.
 * No id regeneration, no sibling churn - and no in-place mutation on the reactive tree
 * (structuredClone cannot touch Vue proxies; the tree stays a shallowRef of PLAIN data,
 * re-swapped on every op so the deep-watcher only ever sees fresh values).
 */
export function graftSplit(root: DockNode, replacement: DockSplit): DockNode {
  if (isSplit(root) && root.id === replacement.id) return replacement;
  const copy = clone(root);
  // v0.14.4 m4: a grown root's local splits graft too (its inner dividers drag.
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

/** the round-trip gate's compare. */
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
          // the viewer payload is tab identity.
          viewerRefEq(t.viewer, b.tabs[i]!.viewer),
      );
    return (
      a.id === b.id &&
      a.active === b.active &&
      (a.fixed ?? null) === (b.fixed ?? null) &&
      (a.edge ?? undefined) === (b.edge ?? undefined) &&
      tabEq &&
      // v0.14.4 the local split trees compare through the same walk (structure + ids.
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
  /**
   * the docking drop - release a dragged tab on a zone of another pane's body.
   * Refusals are no-ops (shells don't split, zones never self-dock).
   */
  dockDropToZone: (
    fromPaneId: string,
    fromIndex: number,
    toPaneId: string,
    zone: DragZone,
  ) => void;
  seatOk: (component: DockComponentName, paneId: string) => boolean;
  zoneOf: (paneId: string) => PaneZone | null;
  /**
   * canvas C1 THE ROUTING LAW: the pane a session opens in.
   * A sidebar NEVER hosts a session, so a non-main preferred id falls through to the
   * main column.
   */
  sessionPane: (preferred?: string | null) => string;
  undoLayout: () => boolean;
  beginResize: () => void;
  /**
   * card 4 - the dockable contract opener: singletons FOCUS their one instance when it
   * lives (created | focused), multi goes through the app's chat opener instead - this
   * must only be called for singletons
   */
  openSingleton: (paneId: string, component: DockComponentName) => "created" | "focused";
  setSessionRef: (key: string, sessionRef: string | null) => void;
  closeSession: (sessionRef: string) => void;
  /**
   * open (or FOCUS) the viewer tab for `v` - a file path or skill name already shown
   * anywhere focuses that tab (focus-reuse, never a duplicate); otherwise one mints
   * into the ensured main pane, exactly the routing a session open takes.
   */
  openViewer: (v: ViewerRef) => void;
  findChatTab: (key: string) => ChatTabLocation | null;
  firstChatTab: (sessionRef?: string) => ChatTabLocation | null;
  /**
   * the first DRAFT chat tab (no session binding) - the '+' new flow focuses it instead
   * of minting a duplicate.
   */
  firstDraftChatTab: () => ChatTabLocation | null;
  activeChatTab: () => ChatTabLocation | null;
  split: (paneId: string, direction: "row" | "column", side: "before" | "after") => void;
  remove: (paneId: string) => void;
  toggleEdge: (component: "sidebar" | "inspector", side: "before" | "after") => void;
  toggleBottom: () => void;
  /**
   * Endo's comment: the bottom shell's fullscreen view.
   * Session-only (a reload never boots fullscreen).
   */
  bottomFullscreen: Ref<boolean>;
  toggleBottomFullscreen: () => void;
  ensureMainPane: () => string;
  /**
   * whether the pane's X may remove it: pinned panes never (they host unclosable tabs)
   * and the LAST MAIN pane never.
   */
  canRemovePane: (paneId: string) => boolean;
  /**
   * Endo ruling: whether `paneId` is a MAIN pane with no other main pane beside it.
   * Pinned panes are never the last main pane (their tabs keep their own closable
   * rules).
   */
  isLastMainPane: (paneId: string) => boolean;
  /**
   * divider drag: measured start widths flow in on the first move event.
   * Returns the drag's close intents for the caller to react to (the store itself
   * already applies them to the tree).
   */
  drag: (
    splitId: string,
    dividerIndex: number,
    startPx: number[],
    deltaPx: number,
    contentPx: number,
  ) => { collapsedPaneId: string | null };
  /**
   * the window-narrow give-way.
   * While the content box `contentPx` cannot seat every fixed pane plus the flexible
   * 320 floor, the secondary sidebar (inspector) collapses first, then the primary
   * (sessions); each hide remembers the pane's width via the edge-px memory, so the
   * menubar toggle restores exactly what the resize took.
   * Page-lifetime like a drag-close - a wider window never re-reveals.
   */
  narrowSqueeze: (contentPx: number) => void;
  revealRefused: Ref<"sidebar" | "inspector" | null>;
  /**
   * THE RUNTIME MODULE GATE.
   * Idempotent, safe to call on every module-state change; the parked seats persist
   * under MODULE_STASH_KEY.
   */
  setModuleGate: (gate: ModuleGate) => void;
}

export const DOCK_API_KEY: InjectionKey<DockApi> = Symbol("omega-dock-api");

// chat is multi-instance now.
export const DOCK_CHAT_OPEN_KEY: InjectionKey<
  (paneId: string, sessionId: string | null) => void
> = Symbol("omega-dock-chat-open");
// the app resolves chat tab titles to their session label (sidebar
// highlight follows the FOCUSED tab, so labels must read in the bar);
// null falls back to the generic COMPONENT_TITLES entry
export const DOCK_TAB_TITLE_KEY: InjectionKey<(tab: DockTab) => string | null> =
  Symbol("omega-dock-tab-title");

// The NEW-CONVERSATION DIM. The doors keep rendering - dimmed and disabled, never
// removed - the same law the last-tab close X obeys (aria-disabled + tabindex -1, the
// click still focuses the draft).
export const DOCK_DRAFT_FOCUS_KEY: InjectionKey<Ref<boolean>> = Symbol(
  "omega-dock-draft-focus",
);

/** the persisted reopen sizes for edge panes (page-lifetime memory. */
export const EDGE_DEFAULT_PX: Record<"sidebar" | "inspector", number> = {
  sidebar: 264,
  inspector: 264,
};

export function createDock(store: Storage): DockApi {
  // the persisted VERSION rides the load.
  const stored = store.getItem(DOCK_LAYOUT_KEY);
  const loaded = parseAnyLayout(stored, () => migrateLegacy((k) => store.getItem(k)));
  // the closed-edge set rides the record.
  const closedEdges: DockEdgeComponent[] =
    loaded?.closedEdges ??
    (store.getItem(BOTTOM_CLOSED_KEY) === "1" ? ["bottom"] : []);
  store.removeItem(BOTTOM_CLOSED_KEY);
  // the closed shells' stowed panes ride the record. Chat-tab keys in the stows must
  // seed the tab-key counter too (nextTabKey never mints a key a hidden pane already
  // holds).
  const hiddenState: HiddenEdgeState = {};
  // the module gate's parked seats. The stash is its own record (MODULE_STASH_KEY), so
  // the layout record never carries a ghost pane; the shell applies the gate below
  // (setModuleGate) as soon as it has the runtime facts.
  let parkedTabs: ParkedTab[] = parseParkedTabs(store.getItem(MODULE_STASH_KEY));
  let liveGate: ModuleGate = EMPTY_MODULE_GATE;
  // the component of the toggle's last reveal.
  let revealPending: "sidebar" | "inspector" | null = null;
  // the latest pane-area px the resize watcher measured - the toggle's fit-check reads
  // it (the reveal refuses when the window cannot seat the pane).
  let lastContentPx: number | null = null;
  const revealRefused = ref<"sidebar" | "inspector" | null>(null);
  if (loaded?.hiddenEdges) {
    for (const comp of Object.keys(loaded.hiddenEdges) as DockEdgeComponent[]) {
      const pane = loaded.hiddenEdges[comp];
      // canvas C2 (ONE-TIME): a sidebar stow carrying a LOCAL SPLIT TREE is a record of
      // the deleted split-sidebar model - it never comes back.
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
    // v1-era semantics only: the inspector group's tab list was fixed chrome then.
    initial = upgradeInspectorTabs(expandInspectorTabs(initial));
  } else {
    initial = expandInspectorTabs(initial);
  }
  // sync the id seed BEFORE boot repair mints panes. The seed is per-page (0 at boot):
  // repair first + sync after let ensurePinnedEdges mint a fresh "n1" while the loaded
  // tree still holds its own old "n1" - duplicate ids, and every later id-keyed op (the
  // divider intents' prunePane, canRemovePane, focus lookups) acts on BOTH panes.
  syncIdSeed(initial);
  // insights' home moved (main -> the right shell).
  initial = rehomeInsightsTab(initial);
  // settings' home moved the OTHER way (the inspector -> an on-demand main tab behind.
  initial = retireInspectorSettings(initial);
  // canvas C2 THE SHAPE LAW (ONE-TIME). Both sidebars reset to the clean default HERE -
  // before the seat repair, so restoreSeatSet re-seats the class members in the reset
  // shells on this same boot, and the boot's own re-persist writes v4 so the reset
  // never runs again. The markers derive first: a v1-era record has no edge markers at
  // all, and the reset must see the shells the repair is about to heal
  // (deriveEdgeMarkers is idempotent).
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
  // the CLOSED shells' stows count as the tree's composition while they sit hidden: the
  // missing-set repair never re-seats them
  const stowedComponents = Object.keys(hiddenState) as DockEdgeComponent[];
  initial = ensurePinnedEdges(
    initial,
    closedEdges,
    stowedComponents.flatMap((c) => (hiddenState[c]?.tabs ?? []).map((t) => t.component)),
  );
  // the bottom panel is grafted (or left closed) AFTER the sidebars repair and BEFORE
  // chat keys - the panel hosts no chat tabs, and its id must seed above the loaded
  // tree's max (syncIdSeed ran once)
  initial = ensureBottomPanel(initial, closedEdges);
  // the bottom shell's strip re-syncs against the registry's bottom home after the
  // sidebars repair.
  const syncedBottom = syncBottomHostTabs(initial);
  if (syncedBottom !== initial) {
    loaded.repairs.push(
      "a persisted bottom shell predates a bottom-home registration: the missing default tabs were appended",
    );
    initial = syncedBottom;
  }
  initial = assignChatKeys(initial);
  // (THE LOCKED LAST TAB): the boot repairs the two dead pane states the retired
  // placeholder law can leave on disk - a persisted never-zero seat and a zero-tab pane
  // from a record older still. Both land on a fresh new-chat tab: the chat the user
  // closed comes back as a chat, never as a blank "New Tab" body.
  const seated = repairPaneSeats(initial);
  if (seated !== initial) {
    loaded.repairs.push(
      "a persisted pane carried no live tab (a retired new-tab seat or a zero-tab pane): a new chat was seated",
    );
    initial = seated;
  }
  // v0.14.4 BACKUP LAW: a REPAIRED record copies its raw form ONCE, BEFORE the first
  // auto-persist - the repair chain must never be able to rewrite history, so the
  // boot's own re-persist happens after.
  if (stored && loaded.repairs.length > 0) {
    store.setItem(DOCK_LAYOUT_BAK_KEY, stored);
  }
  // the ONE gated writer: every record write re-reads through the total deserializer.
  const gatedPersist = (t: DockNode) => {
    const candidate = serialize(t, closedEdges, V2_NOMINAL_CONTAINER_PX, hiddenState);
    const back = parseAnyLayout(candidate, () => t);
    if (!treeShapesMatch(back.tree, t)) {
      console.warn("dock: layout round-trip mismatch - the write was refused, last-good kept");
      return;
    }
    store.setItem(DOCK_LAYOUT_KEY, candidate);
  };
  // a modernized/fixed-shape-repaired load persists at once (now as v3) so the next.
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

  /**
   * apply the runtime module gate.
   * Three passes, in order, because a re-seat must not be parked in the same tick: 1.
   * park them in the CLOSED shells' stows too - a hidden inspector must not reveal a
   * dead tab later (the settings-seat precedent); 3.
   * A park that empties a pane is left to repairPaneSeats, the same never-blank law
   * every other boot repair obeys.
   * The stash writes on every call (it is tiny and idempotent).
   */
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

  /** the closed-edge marking helper, idempotent. */
  const setClosed = (comp: DockEdgeComponent, closed: boolean) => {
    const at = closedEdges.indexOf(comp);
    if (closed && at < 0) closedEdges.push(comp);
    if (!closed && at >= 0) closedEdges.splice(at, 1);
  };

  const has = (component: DockComponentName): boolean =>
    // singleton semantics: exists anywhere, active tab or not - findComponentPane
    // (active-only) would under-report a singleton parked as an inactive tab and let
    // toggles duplicate it
    findComponentTab(tree.value, component) !== null;

  // -- the undo ring ------------------------------------- Every committed LAYOUT op.
  // Content ops (tab opens/closes, focus, session refs) never enter the ring - except a
  // close that MERGES (the pane census dropped), which is structural op.
  const UNDO_RING_MAX = 20;
  const undoRing: string[] = [];
  /** the snapshot push: deduped (an op that lands on the snapshot's own state. */
  const undoPush = (t: DockNode) => {
    const s = serialize(t, closedEdges, V2_NOMINAL_CONTAINER_PX, hiddenState);
    if (undoRing[undoRing.length - 1] === s) return;
    undoRing.push(s);
    if (undoRing.length > UNDO_RING_MAX) undoRing.shift();
  };
  /**
   * swap in a NEW tree from a layout op: no-op trees contribute no snapshot (a refused
   * drop / vanished target never rings)
   */
  const applyLayout = (before: DockNode, next: DockNode) => {
    if (next === before) return;
    undoPush(before);
    tree.value = next;
    // belt+suspenders: any layout mutation ends an armed gesture - the record's
    // geometry and indices are pre-mutation; the record must never outlive its event
    // (drops commit here; undo and reveal ops run through the same funnel).
    endGesture();
  };
  /**
   * walk back one op: pop the ring, re-parse through the total deserializer (a snapshot
   * is always well-formed.
   */
  const undoLayout = (): boolean => {
    const s = undoRing.pop();
    if (!s) return false;
    const rec = parseAnyLayout(s, () => tree.value);
    syncIdSeed(rec.tree);
    // a snapshot is pre-seat - no placeholder can ride it once the placeholder is.
    const restored = assignChatKeys(rec.tree);
    if (rec.closedEdges) {
      closedEdges.splice(0, closedEdges.length, ...rec.closedEdges);
    }
    // the stows ride the snapshot too (a pre-hide snapshot carries the pane it was
    // about to hide.
    for (const k of Object.keys(hiddenState) as DockEdgeComponent[]) {
      delete hiddenState[k];
    }
    if (rec.hiddenEdges) {
      Object.assign(hiddenState, rec.hiddenEdges);
    }
    tree.value = restored;
    // a snapshot predates the gate. Re-apply the live gate so undo can never resurrect
    // a chip whose module is off.
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
    // noop (a vanished target pane) preserves the tree, nothing opened; report
    // "focused".
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
  // the viewer opener - the pure op carries focus-reuse.
  const openViewer = (v: ViewerRef) => {
    const pane = ensureMainPane();
    const res = openViewerTab(tree.value, v, pane);
    tree.value = res.tree;
  };
  const split = (paneId: string, direction: "row" | "column", side: "before" | "after") => {
    // v0.14.4 /3: the direction/depth law gates the menu path too (sidebars refuse row
    // splits, tight roots refuse past depth 2) - a refused split is a no-op, exactly
    // like the refused drop
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
    // v0.14.4 a leaf inside a grown root merges away (the root unwinds, never dies).
    if (rootPaneOf(tree.value, paneId)) return true;
    return !isPinnedPane(pane) && mainPaneCount(tree.value) > 1;
  };
  // canvas C1: the live drop gate reads the class law over the pane's zone;
  // the seat map decides, never a per-view conditional
  const seatOk = (component: DockComponentName, paneId: string): boolean =>
    seatRefusal(tree.value, component, paneId) === null;
  // canvas C2: the pane's zone, read live.
  const zoneOf = (paneId: string): PaneZone | null => paneZone(tree.value, paneId);
  // canvas C1: THE ROUTING LAW - a session opens in the main column, never in a.
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
    // the divider close (drag-past-min) hides - the intent prunes the tree directly
    // (prunePane is the same body hideEdgePane runs; a pinned pane hides, it is never
    // unpinned).: the close persists now (the closed-edge set rides the record) - a
    // reload restores the visibility the user left, exactly like the menubar toggle.
    if (d.split !== node) tree.value = graftSplit(tree.value, d.split);
    for (const id of [d.collapsedPaneId]) {
      if (!id) continue;
      const gone = findPane(tree.value, id);
      if (gone) {
        const comp = edgeComponentOf(gone);
        if (comp) {
          if (gone.fixed !== null) rememberEdgePx(store, comp, gone.fixed);
          // a divider close is a hide, not a delete.
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
      // hide remembers the pane's width.
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
      // the reveal owns the NEXT squeeze (app.vue's tree watcher re-checks after the
      // DOM settles).
      revealPending = component;
      applyLayout(tree.value, res.tree);
      syncModuleGate(liveGate);
    }
  };

  // the bottom panel's toggle (menubar)..
  const toggleBottom = () => {
    if (findEdgePane(tree.value, "bottom")) {
      // v0.14.4 the pre-hide snapshot first (closed set intact)
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
      // (round 2): the same reintroduction class as the edge reveal above - the
      // restacked bottom strip re-runs the gate, or a seat parked while the panel was
      // closed never comes back.
      syncModuleGate(liveGate);
    }
  };

  // Endo's comment: the bottom shell's FULLSCREEN toggle - the pane's tabbar button
  // flips it.: the band leaves the flow (position absolute over the pane-area between
  // the sidebar insets) and the column keeps rendering its split children - the main
  // row above stays visible with BOTH sidebars live and self-toggled ("the sidebars
  // never hide - Endo"), the sidebars' resize dividers stay visible/draggable, and the
  // row's chat sits under the band. Session-only on purpose: a reload never boots into
  // fullscreen (the stored height + the render clamp keep the divider grabbable;
  // fullscreen is a view, not a layout state).
  const bottomFullscreen = ref(false);
  const toggleBottomFullscreen = () => {
    bottomFullscreen.value = !bottomFullscreen.value;
  };

  // the window-narrow give-way (app.vue's resize watcher measures the pane-area and.
  // Each collapsed edge pane's width goes through the same memory the menubar toggle
  // uses.: the give-way's closure persists like every other hide (a reload never
  // re-reveals a pane the user's own state says is closed).
  const narrowSqueeze = (contentPx: number) => {
    // every measurement updates the toggle's fit-check baseline.
    lastContentPx = contentPx;
    // a squeeze right after a user reveal is last-requested-aware.
    const justRevealed = revealPending;
    revealPending = null;
    const res = narrowGiveWay(tree.value, contentPx, justRevealed);
    if (!res.hidden.length) return;
    // v0.14.4 the pre-give-way snapshot first (the closed set mutates below it.
    undoPush(tree.value);
    for (const h of res.hidden) {
      rememberEdgePx(store, h.component, h.px);
      // the give-way is a hide - the pane's record stows like the menubar toggle's, the
      // toggle's reopen restores exactly it
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
    undoLayout, beginResize: () => undoPush(tree.value),
    setModuleGate: syncModuleGate,
  };
}
