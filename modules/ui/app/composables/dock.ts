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

import { PANE_OPEN_MIN, snapCandidate } from "./panes.ts";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  COMPONENT_TITLES,
  isClosableTab,
  isPinnedTab,
  type DockableComponentLike,
  type DockComponentName,
} from "./dockables.ts";
// the openable component universe, the '+' inventory and the tab-title
// fallback live in THE dockable registry (dockables.ts, card t_30ee04fb);
// re-exported here so engine-side consumers keep one import surface
export {
  DOCK_COMPONENTS,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  COMPONENT_TITLES,
  isClosableTab,
  isPinnedTab,
};
export type { DockComponentName, DockableComponentLike };
// re-export for consumers that think in dock terms (tests, toggle helpers)
export { PANE_OPEN_MIN };

export const DOCK_LAYOUT_KEY = "omega.dock-layout";

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

// gutter between a split's children: the 4px divider each child of the old
// shell carried (pane-area padding contributes the outer 4px on the root).
export const SPLIT_GUTTER_PX = 4;

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
}
export interface DockPane {
  kind: "pane";
  id: string;
  tabs: DockTab[];
  active: number;
  /** fixed px size (sidebars); null = flexible (shares remainder) */
  fixed: number | null;
}
export interface DockSplit {
  kind: "split";
  id: string;
  direction: "row" | "column";
  children: DockNode[];
  /** fractions of the content box, parallel to children (fixed panes ignore theirs) */
  sizes: number[];
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

export function makePane(component: DockComponentName | null, fixed: number | null = null): DockPane {
  return {
    kind: "pane",
    id: nextNodeId(),
    tabs: component ? [{ component }] : [],
    active: 0,
    fixed,
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

/** every node, depth-first, root first */
export function* allNodes(tree: DockNode): IterableIterator<DockNode> {
  yield tree;
  if (isSplit(tree)) for (const c of tree.children) yield* allNodes(c);
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

// --- the pane-model reset (card t_4ae27e0d) ---------------------------------
// three identities the whole engine enforces:
//   pinned pane   hosts at least one sidebar-pinned tab (left/right):
//                 never closable, never removable, never a tab target.
//   main pane     everything else (chat / settings / empty) - the closable
//                 class, hover-X tabs.
//   never-empty   the dock always holds at least one pane; refusals are
//                 no-ops (the tree comes back untouched).

export function paneCount(tree: DockNode): number {
  let n = 0;
  for (const node of allNodes(tree)) if (isPane(node)) n++;
  return n;
}

/** a pane is PINNED when any of its tabs is sidebar-pinned - the sessions
    pane (one "sidebar" tab) and the inspector pane (its five view tabs).
    Empty panes and main-panel panes are main. */
export function isPinnedPane(pane: DockPane): boolean {
  return pane.tabs.some((t) => isPinnedTab(t.component));
}

/** the first main pane, depth-first - where session opens land. The pinned
    sidebars never host chat, so the walk skips them. Null = no main pane
    in the tree (all sidebars): the opener must insert one. */
export function firstMainPaneId(tree: DockNode): string | null {
  for (const n of allNodes(tree)) {
    if (isPane(n) && !isPinnedPane(n)) return n.id;
  }
  return null;
}

/** insert an EMPTY flexible main pane at the root's after-edge (the fixed
    shell's recovery path: the opener uses this when the user managed to
    remove the last main pane). Returns the new pane's id for focusing. */
export function insertMainPane(tree: DockNode): { tree: DockNode; paneId: string } {
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

/** the sidebar-pinned panes are fixed chrome: a loaded tree that somehow
    lost one of the two (a pre-reset tree that was toggled closed) gets it
    re-inserted at its edge. Pure; no-op returns the SAME tree. */
export function ensurePinnedEdges(tree: DockNode): DockNode {
  let result = tree;
  if (!findComponentTab(tree, "sidebar")) {
    result = insertRootEdge(result, "before", makePane("sidebar", 240));
  }
  if (!INSPECTOR_TAB_COMPONENTS.some((c) => findComponentTab(result, c) !== null)) {
    const pane: DockPane = {
      kind: "pane",
      id: nextNodeId(),
      tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
      active: 0,
      fixed: 240,
    };
    result = insertRootEdge(result, "after", pane);
  }
  return result;
}

/** insert `pane` at the root's edge of a row (a pane root or a column
    root wraps into a row, keeping the edge meaningful - the pre-reset
    toggle path's behavior). Private to the reset helpers. */
function insertRootEdge(tree: DockNode, side: "before" | "after", pane: DockPane): DockNode {
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
    chip strip; card t_4ae27e0d dissolved the strip - the five views are
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
  // pinned components have no open path (card t_4ae27e0d): the sidebars
  // live because the shell puts them there, never because a menu opened
  // them into an arbitrary pane
  if (isPinnedTab(component)) return { tree, action: "noop" };
  const found = findComponentTab(tree, component);
  if (found) {
    // focus it in place - even when the target pane is this one (a
    // re-click on '+' while the singleton sits in another tab of the
    // same pane still lands on it)
    return { tree: focusTab(tree, found.paneId, found.index), action: "focused" };
  }
  const pane = findPane(tree, paneId);
  if (!pane) return { tree, action: "noop" }; // target vanished - no-op
  if (isPinnedPane(pane)) return { tree, action: "noop" }; // pinned - no tab hosts
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
    card t_4ae27e0d refusals: a PINNED pane (it hosts unclosable tabs) and
    the LAST REMAINING pane both come back untouched - the dock never
    empties (the fixed shape always keeps at least one pane). */
export function removePane(tree: DockNode, paneId: string): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane)) return tree;
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
    t_4ae27e0d: pinned panes and pinned components are off-limits - the
    sidebars' tab lists are fixed. */
export function setPaneComponent(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane) || isPinnedTab(component)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [{ component }];
      n.active = 0;
    }
  });
}

/** open a new tab in a pane, appending at the end and activating it
    (the tab-bar '+' menu path; works on an empty pane too - the first
    tab revives it). Refusals (card t_4ae27e0d): a PINNED pane takes no
    tabs, and pinned components open nowhere - a main pane may only host
    the closable class. */
export function openTab(tree: DockNode, paneId: string, tab: DockTab): DockNode {
  const pane = findPane(tree, paneId);
  if (!pane || isPinnedPane(pane) || isPinnedTab(tab.component)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [...n.tabs, tab];
      n.active = n.tabs.length - 1;
    }
  });
}

/** card 2: close a tab; the last one empties the pane (menu reappears).
    card t_4ae27e0d: an UNCLOSEABLE tab refuses - the pinned sidebars'
    tabs have no X at all. */
export function closeTab(tree: DockNode, paneId: string, index: number): DockNode {
  const pane = findPane(tree, paneId);
  const tab = pane?.tabs[index];
  if (!pane || !tab || !isClosableTab(tab.component)) return tree;
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      if (n.tabs.length <= index) return;
      n.tabs = n.tabs.filter((_, i) => i !== index);
      n.active = Math.max(0, Math.min(n.active > index ? n.active - 1 : n.active, n.tabs.length - 1));
    }
  });
}

/** card 2 DnD: move a tab between positions/panes. `toIndex` is the
    insertion index into the TARGET pane's tabs AFTER the tab has been
    lifted out of the source (for a same-pane reorder, the callers pass
    the post-removal index - clamped to the remaining range). Both panes
    survive: emptying the source leaves it showing the '+' menu (settled
    ruling), never a removal. VS Code semantics: the moved tab FOCUSES in
    the target pane; the source focuses whatever neighbor remains. */
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
  const tab = from.tabs[fromIndex];
  // card t_4ae27e0d: pinned tabs never leave their sidebar pane, and no
  // tab may move INTO a pinned pane (the sidebars' tab lists are fixed)
  if (isPinnedTab(tab.component) || isPinnedPane(to)) return tree;
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
  return cloneTreeWith(tree, (n) => {
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
  /** index of the child whose fixed px changed (or null for pure fractions) */
  fixedIndex: number | null;
}

/** pure divider drag: between children [i-1, i] of `split`. startPx carries
    the RENDERED px of every child at drag start (measurement is the
    component's job). deltaPx + means right/down. Applies the 128px drag
    floor, the 320px floor against the absorbing neighbor, and the 320/640
    snap attractor on the controlled pane.
    - left child fixed: drag controls the LEFT pane (grows/shrinks over the
      right neighbor, which flexes/floors as measured).
    - left flexible, right fixed: invert - the RIGHT pane is controlled
      (mirrors the old invert=true divider), so the magnet reads right px.
    - both flexible: fractions shift relative to the content px box. */
export function dragSplitDivider(
  split: DockSplit,
  dividerIndex: number,
  startPx: number[],
  deltaPx: number,
  contentPx: number,
): DockSplit {
  const i = dividerIndex; // divider sits left of child i
  const li = i - 1;
  const left = split.children[li];
  const right = split.children[i];
  if (!left || !right || li < 0) return split;

  const next = structuredClone(split);

  // everything NOT adjacent to this divider (px at drag start) never moves
  const othersPx = startPx.reduce((a, p, k) => (k === li || k === i ? a : a + p), 0);
  const room = Math.max(0, contentPx - othersPx);

  const capFor = (floorOther: number) => Math.max(MIN_PANE_PX, room - floorOther);
  const applySnap = (px: number, lo: number, hi: number): number => {
    // attractor first, then the floors again: a snap point that overshoots
    // the cap lands ON the cap, never past it
    const snapped = snapCandidate(px);
    return clamp(snapped ?? px, lo, hi);
  };

  const lp = left as DockPane;
  const rp = right as DockPane;
  if (isPane(lp) && lp.fixed !== null) {
    // drag controls the LEFT pane; right absorbs: its floor is 128 if fixed,
    // 320 if flexible (the chat)
    const rightFloor = isPane(rp) && rp.fixed !== null ? MIN_PANE_PX : CHAT_FLOOR_PX;
    const hi = capFor(rightFloor);
    (next.children[li] as DockPane).fixed = applySnap(
      clamp(startPx[li] + deltaPx, MIN_PANE_PX, hi),
      MIN_PANE_PX,
      hi,
    );
    return next;
  }
  if (isPane(rp) && rp.fixed !== null) {
    // mirror (the old invert=true divider): dragging left widens the fixed
    // right pane; the flexible left flexes at its 320 floor
    const leftFloor = CHAT_FLOOR_PX;
    const hi = capFor(leftFloor);
    (next.children[i] as DockPane).fixed = applySnap(
      clamp(startPx[i] - deltaPx, MIN_PANE_PX, hi),
      MIN_PANE_PX,
      hi,
    );
    return next;
  }

  // both flexible: shift BOTH fractions against the content box (renormalizing
  // would rescale the untouched siblings); snap sticks the left pane to an
  // exact px attractor like the fixed path so any divider feels identical
  const content = Math.max(1, room);
  const pxLo = MIN_PANE_PX;
  const pxHi = Math.max(MIN_PANE_PX, room - CHAT_FLOOR_PX);
  const finalLeft = applySnap(clamp(startPx[li] + deltaPx, pxLo, pxHi), pxLo, pxHi);
  const shift = (finalLeft - startPx[li]) / content;
  next.sizes = next.sizes.map((s, k) =>
    k === li ? s + shift : k === i ? s - shift : s,
  );
  return next;
}

export function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

// snapCandidate lives in panes.ts (imported at the top); dock.ts must not
// re-derive the constants (single source per the card).

// --- persistence ---------------------------------------------------------------

const LAYOUT_VERSION = 1;

export function serialize(tree: DockNode): string {
  return JSON.stringify({ v: LAYOUT_VERSION, tree });
}

/** validates + parses a persisted layout. Returns null for anything the
    engine cannot safely render (old/foreign/corrupt) so callers migrate. */
export function deserialize(raw: string | null): DockNode | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
  const rec = parsed as { v?: unknown; tree?: unknown };
  if (rec.v !== LAYOUT_VERSION) return null;
  const tree = validateNode(rec.tree);
  if (!tree) return null;
  return tree;
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
    const sizes: number[] = [];
    for (const s of n.sizes as unknown[]) {
      if (typeof s !== "number" || !Number.isFinite(s) || s <= 0) return null;
      sizes.push(s);
    }
    return {
      kind: "split",
      id: n.id,
      direction: n.direction,
      children,
      sizes: normalizeSizes(children.length, sizes),
    };
  }
  if (n.kind !== "pane") return null;
  if (!Array.isArray(n.tabs)) return null;
  const tabs: DockTab[] = [];
  for (const t of n.tabs) {
    if (!t || typeof t !== "object") return null;
    const tab = t as { component?: unknown; sessionRef?: unknown; key?: unknown };
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
    tabs.push(
      typeof tab.sessionRef === "string" && tab.sessionRef
        ? { component: tab.component as DockComponentName, sessionRef: tab.sessionRef }
        : { component: tab.component as DockComponentName },
    );
    if (typeof tab.key === "string" && tab.key) tabs[tabs.length - 1]!.key = tab.key;
  }
  const fixed = typeof n.fixed === "number" && Number.isFinite(n.fixed) && n.fixed >= 0
    ? n.fixed
    : null;
  const active = typeof n.active === "number" && Number.isFinite(n.active)
    ? clamp(Math.trunc(n.active), 0, Math.max(0, tabs.length - 1))
    : 0;
  return { kind: "pane", id: n.id, tabs, active, fixed };
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
 * its five view tabs - both sidebars always present (the open flags are
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
  };
  const children: DockNode[] = [makePane("sidebar", sw), makePane("chat", null), inspector];

  // fractions across the flexible chat: fixed panes ignore theirs, the
  // chat absorbs - equal fractions keep deserialize()/normalize honest
  const root: DockSplit = {
    kind: "split",
    id: nextNodeId(),
    direction: "row",
    children,
    sizes: normalizeSizes(children.length, undefined),
  };
  return root;
}

// --- store ---------------------------------------------------------------

import { shallowRef, watch, type InjectionKey, type Ref } from "vue";

/** pure graft: replace the split node `replacement` (by id) inside `root`
    and return the NEW root. No id regeneration, no sibling churn - and no
    in-place mutation on the reactive tree (structuredClone cannot touch
    Vue proxies; the tree stays a shallowRef of PLAIN data, re-swapped on
    every op so the deep-watcher only ever sees fresh values). */
export function graftSplit(root: DockNode, replacement: DockSplit): DockNode {
  if (isSplit(root) && root.id === replacement.id) return replacement;
  const copy = clone(root);
  const walk = (n: DockSplit): boolean => {
    for (let i = 0; i < n.children.length; i++) {
      const c = n.children[i];
      if (c.id === replacement.id) {
        n.children[i] = clone(replacement);
        return true;
      }
      if (isSplit(c) && walk(c)) return true;
    }
    return false;
  };
  return isSplit(copy) && walk(copy) ? copy : root;
}

export interface DockApi {
  /** the reactive root (swap on every op; deep-watched for persistence) */
  tree: Ref<DockNode>;
  /** whether the component has an instance anywhere in the tree - the
      singleton semantics (card t_30ee04fb): a singleton exists at most
      once, so its presence check reads EVERY tab, not just active ones */
  has: (component: DockComponentName) => boolean;
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
  /** card 4 - the dockable contract opener: singletons FOCUS their one
      instance when it lives (created | focused), multi goes through the
      app's chat opener instead - this must only be called for singletons */
  openSingleton: (paneId: string, component: DockComponentName) => "created" | "focused";
  /** card 3: chat tab binding - write the session id a tab owns. */
  setSessionRef: (key: string, sessionRef: string | null) => void;
  /** card 3: close every chat tab bound to a session (delete flow). */
  closeSession: (sessionRef: string) => void;
  /** card 3: chat tab lookups (focus helpers; read-only over the live tree) */
  findChatTab: (key: string) => ChatTabLocation | null;
  firstChatTab: (sessionRef?: string) => ChatTabLocation | null;
  split: (paneId: string, direction: "row" | "column", side: "before" | "after") => void;
  remove: (paneId: string) => void;
  /** card t_4ae27e0d: the id of the FIRST main pane - the only place a
      session tab may open. The pinned sidebars are skipped; when the tree
      holds no main pane (the user removed the last one) a fresh flexible
      pane is inserted at the root's after-edge and its id returned. */
  ensureMainPane: () => string;
  /** whether the pane's X may remove it: pinned panes never (they host
      unclosable tabs) and the last remaining pane never (the dock never
      empties). The UI hides the X instead of offering a refused click. */
  canRemovePane: (paneId: string) => boolean;
  /** divider drag: measured start widths flow in on the first move event */
  drag: (splitId: string, dividerIndex: number, startPx: number[], deltaPx: number, contentPx: number) => void;
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
  const stored = deserialize(store.getItem(DOCK_LAYOUT_KEY));
  // card t_4ae27e0d: loaded trees modernize (legacy "inspector" tabs expand
  // to the five real view tabs) and the fixed shape re-asserts itself (a
  // pre-reset tree sidebars-toggled-closed gets its panes back). Any change
  // persists at once - the next boot must see the modern shape even if no
  // mutation fires this session.
  let initial = stored
    ? expandInspectorTabs(stored)
    : migrateLegacy((k) => store.getItem(k));
  initial = ensurePinnedEdges(initial);
  initial = assignChatKeys(initial);
  syncIdSeed(initial);
  // a modernized/fixed-shape-repaired load persists at once so the next
  // boot sees the modern tree even if no mutation fires this session
  if (stored) store.setItem(DOCK_LAYOUT_KEY, serialize(initial));
  // shallowRef: the tree stays PLAIN data (structuredClone rejects proxies);
  // every op swaps the root, so reactivity rides the ref change alone
  const tree = shallowRef(initial) as Ref<DockNode>;

  // persist the WHOLE tree on every mutation (card ruling: layout lives in
  // localStorage alone; the URL owns only the focused session)
  watch(
    tree,
    (t) => {
      store.setItem(DOCK_LAYOUT_KEY, serialize(t));
    },
    { deep: true },
  );

  const has = (component: DockComponentName): boolean =>
    // singleton semantics (card t_30ee04fb): exists anywhere, active tab
    // or not - findComponentPane (active-only) would under-report a
    // singleton parked as an inactive tab and let toggles duplicate it
    findComponentTab(tree.value, component) !== null;

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
    tree.value = closeTab(tree.value, paneId, index);
  };
  const moveTabIn = (fromPaneId: string, fromIndex: number, toPaneId: string, toIndex: number) => {
    tree.value = moveTab(tree.value, fromPaneId, fromIndex, toPaneId, toIndex);
  };
  const setSessionRef = (key: string, sessionRef: string | null) => {
    tree.value = setTabSessionRef(tree.value, key, sessionRef);
  };
  const closeSession = (sessionRef: string) => {
    tree.value = closeSessionTabs(tree.value, sessionRef);
  };
  const split = (paneId: string, direction: "row" | "column", side: "before" | "after") => {
    tree.value = splitPane(tree.value, paneId, direction, side);
  };
  const remove = (paneId: string) => {
    tree.value = removePane(tree.value, paneId);
  };
  const ensureMainPane = (): string => {
    const first = firstMainPaneId(tree.value);
    if (first) return first;
    const res = insertMainPane(tree.value);
    tree.value = res.tree;
    return res.paneId;
  };
  const canRemovePane = (paneId: string): boolean => {
    const pane = findPane(tree.value, paneId);
    return !!pane && !isPinnedPane(pane) && paneCount(tree.value) > 1;
  };
  const drag = (
    splitId: string,
    dividerIndex: number,
    startPx: number[],
    deltaPx: number,
    contentPx: number,
  ) => {
    const node = findSplit(tree.value, splitId);
    if (!node) return;
    const replacement = dragSplitDivider(node, dividerIndex, startPx, deltaPx, contentPx);
    if (replacement !== node) tree.value = graftSplit(tree.value, replacement);
  };

  return {
    tree, has, setComponent, focus, openTab: openTabIn, closeTab: closeTabIn, moveTab: moveTabIn,
    openSingleton: openSingletonIn,
    setSessionRef, closeSession,
    findChatTab: (key: string) => findChatTab(tree.value, key),
    firstChatTab: (sessionRef?: string) => firstChatTab(tree.value, sessionRef),
    split, remove, ensureMainPane, canRemovePane, drag,
  };
}
