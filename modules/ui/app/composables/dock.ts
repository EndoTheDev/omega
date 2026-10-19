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
// re-export for consumers that think in dock terms (tests, toggle helpers)
export { PANE_OPEN_MIN };

export const DOCK_LAYOUT_KEY = "omega.dock-layout";

// pre-dock pane state. Consumed ONCE by migrateLayout (first boot without a
// dock key), then untouched - the tree is the single source of truth after.
export const LEGACY_OPEN_KEYS = {
  sidebar: "omega.sidebar-open",
  inspector: "omega.inspector-open",
} as const;
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

// component names panes may host. Card 2 turns these into real tabs.
export const DOCK_COMPONENTS = ["sidebar", "chat", "inspector"] as const;
export type DockComponentName = (typeof DOCK_COMPONENTS)[number];

/** the tab-bar '+' menu inventory (card 2): chat only until the dockable
    registry (card 4) hands over the migrated component list with icons. */
export const PLUS_TAB_COMPONENTS: readonly DockComponentName[] = ["chat"];

/** tab titles (card 2 tab bars): VS Code-style group titles, lowercase to
    match the shell's voice; the empty-pane menu keeps its action labels */
export const COMPONENT_TITLES: Record<DockComponentName, string> = {
  sidebar: "sidebar",
  chat: "chat",
  inspector: "inspector",
};

export interface DockTab {
  component: DockComponentName;
  sessionRef?: string;
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

/** remove a pane by id. Its parent split hoists the lone survivor; the whole
    tree going empty degrades to a single empty pane (shows the menu). */
export function removePane(tree: DockNode, paneId: string): DockNode {
  if (!findPane(tree, paneId)) return tree;
  if (isPane(tree) && tree.id === paneId) {
    return makePane(null);
  }
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
  if (!pane || !findPane(tree, targetId)) return tree;
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
    tabs wholesale. Emptying back is closeTab's job (card 2). */
export function setPaneComponent(
  tree: DockNode,
  paneId: string,
  component: DockComponentName,
): DockNode {
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [{ component }];
      n.active = 0;
    }
  });
}

/** open a new tab in a pane, appending at the end and activating it
    (the tab-bar '+' menu path; works on an empty pane too - the first
    tab revives it). */
export function openTab(tree: DockNode, paneId: string, tab: DockTab): DockNode {
  return cloneTreeWith(tree, (n) => {
    if (isPane(n) && n.id === paneId) {
      n.tabs = [...n.tabs, tab];
      n.active = n.tabs.length - 1;
    }
  });
}

/** card 2: close a tab; the last one empties the pane (menu reappears) */
export function closeTab(tree: DockNode, paneId: string, index: number): DockNode {
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
  if (!findPane(tree, toPaneId)) return tree;
  const tab = from.tabs[fromIndex];
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
    const tab = t as { component?: unknown; sessionRef?: unknown };
    if (
      typeof tab.component !== "string" ||
      !(DOCK_COMPONENTS as readonly string[]).includes(tab.component)
    )
      return null;
    tabs.push(
      typeof tab.sessionRef === "string" && tab.sessionRef
        ? { component: tab.component as DockComponentName, sessionRef: tab.sessionRef }
        : { component: tab.component as DockComponentName },
    );
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

function readFlag(get: (k: string) => string | null, key: string, fallback: boolean): boolean {
  const raw = get(key);
  if (raw === "true") return true;
  if (raw === "false") return false;
  return fallback;
}
function readWidth(get: (k: string) => string | null, key: string): number | null {
  const raw = get(key);
  if (raw === null || raw === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) && n >= MIN_PANE_PX ? n : null;
}

/**
 * Seeds the tree from the pre-dock persisted pane state so existing users
 * see their current layout, not a default. A persisted width renders
 * verbatim (sub-floor widths stayed while OPEN in the old shell; the
 * reopen-clamp fires on toggle-after-collapse in the store, not here).
 */
export function migrateLegacy(
  get: (k: string) => string | null,
): DockNode {
  const sidebarOpen = readFlag(get, LEGACY_OPEN_KEYS.sidebar, true);
  const inspectorOpen = readFlag(get, LEGACY_OPEN_KEYS.inspector, false);
  // a persisted width renders verbatim (the old shell kept sub-floor widths
  // while OPEN - reopenClip only fired on toggle-after-collapse, which the
  // store's toggle applies, not migration); unknown width -> old default 240
  const fixedFor = (key: string): number | null => {
    const w = readWidth(get, key);
    return w === null ? 240 : w;
  };
  const sw = fixedFor(LEGACY_WIDTH_KEYS.sidebar);
  const iw = fixedFor(LEGACY_WIDTH_KEYS.inspector);

  const children: DockNode[] = [];
  if (sidebarOpen) children.push(makePane("sidebar", sw));
  children.push(makePane("chat", null));
  if (inspectorOpen) children.push(makePane("inspector", iw));
  if (children.length === 1) return children[0]; // nothing to split - it IS the shell

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
// --- edge insert / component toggles -----------------------------------------

/** insert a pane with `component` at the ROOT's edge (far left/right of a
    row, top/bottom of a column) - the sidebar / inspector toggle path. A
    root pane wraps into a split; an empty root keeps its shape. */
export function insertEdgePane(
  tree: DockNode,
  component: DockComponentName,
  side: "before" | "after",
  fixed: number | null,
): DockNode {
  const pane = makePane(component, fixed);
  if (isPane(tree)) return makeSplit("row", side === "after" ? [tree, pane] : [pane, tree]);
  if (tree.direction !== "row") {
    // wrapping a column root in a row keeps the toggle's edge meaningful
    const wrapper = makeSplit("row", [
      side === "after" ? tree : pane,
      side === "after" ? pane : tree,
    ]);
    return wrapper;
  }
  const next = clone(tree);
  if (side === "after") next.children.push(pane);
  else next.children.unshift(pane);
  next.sizes = normalizeSizes(next.children.length, next.sizes);
  return next;
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
  /** first pane hosting the component, if any */
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
  split: (paneId: string, direction: "row" | "column", side: "before" | "after") => void;
  remove: (paneId: string) => void;
  /** desktop toggle: removes the edge pane or re-inserts it at the remembered
      size with the old reopen-clamp (stale sub-floor width reopens at 320) */
  toggleEdge: (component: "sidebar" | "inspector", side: "before" | "after") => void;
  /** ensure the edge pane exists (mobile drawer path; no toggle semantics) */
  ensureEdge: (component: "sidebar" | "inspector", side: "before" | "after") => void;
  /** divider drag: measured start widths flow in on the first move event */
  drag: (splitId: string, dividerIndex: number, startPx: number[], deltaPx: number, contentPx: number) => void;
}

export const DOCK_API_KEY: InjectionKey<DockApi> = Symbol("omega-dock-api");

/** the persisted reopen sizes for edge panes (page-lifetime memory: closing
    the sidebar and reopening restores the width the user had last) */
const EDGE_DEFAULT_PX: Record<"sidebar" | "inspector", number> = {
  sidebar: 240,
  inspector: 240,
};

export function createDock(store: Storage): DockApi {
  const stored = deserialize(store.getItem(DOCK_LAYOUT_KEY));
  const initial =
    stored ??
    migrateLegacy((k) => store.getItem(k));
  syncIdSeed(initial);
  // shallowRef: the tree stays PLAIN data (structuredClone rejects proxies);
  // every op swaps the root, so reactivity rides the ref change alone
  const tree = shallowRef(initial) as Ref<DockNode>;

  // persist the WHOLE tree on every mutation (card ruling: layout lives in
  // localStorage alone; the URL owns only the focused session)
  watch(
    tree,
    (t) => {
      store.setItem(DOCK_LAYOUT_KEY, serialize(t));
      // keep the reopen memory fresh: an edge pane's fixed size IS its width
      for (const n of allNodes(t)) {
        if (isPane(n) && n.fixed !== null) {
          const c = n.tabs[n.active]?.component;
          if (c === "sidebar" || c === "inspector") EDGE_DEFAULT_PX[c] = n.fixed;
        }
      }
    },
    { deep: true },
  );

  const has = (component: DockComponentName): boolean =>
    findComponentPane(tree.value, component) !== null;

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
  const split = (paneId: string, direction: "row" | "column", side: "before" | "after") => {
    tree.value = splitPane(tree.value, paneId, direction, side);
  };
  const remove = (paneId: string) => {
    tree.value = removePane(tree.value, paneId);
  };
  const toggleEdge = (component: "sidebar" | "inspector", side: "before" | "after") => {
    // reopen-clamp: a stale sub-floor remembered width reopens AT 320
    const fixed = Math.max(PANE_OPEN_MIN, EDGE_DEFAULT_PX[component]);
    tree.value = has(component)
      ? removePane(tree.value, findComponentPane(tree.value, component)!.id)
      : insertEdgePane(tree.value, component, side, fixed);
  };
  const ensureEdge = (component: "sidebar" | "inspector", side: "before" | "after") => {
    if (has(component)) return;
    tree.value = insertEdgePane(
      tree.value,
      component,
      side,
      Math.max(PANE_OPEN_MIN, EDGE_DEFAULT_PX[component]),
    );
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

  return { tree, has, setComponent, focus, openTab: openTabIn, closeTab: closeTabIn, moveTab: moveTabIn, split, remove, toggleEdge, ensureEdge, drag };
}
