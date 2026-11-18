// omega webui: canvas C2 - THE SIDEBAR SHAPE LAW. A sidebar is TABS + a STACK:
// it stacks panes (direction "column") and never puts two columns of content
// side by side, because the shell's two columns ARE the two sidebars. This file
// pins the three halves of that law:
//   1. the shape predicate (panes.ts) and the bounded pointer read (dockdnd.ts)
// a sidebar body offers no left/right half at all, so the deleted
//      split-sidebar gesture has no entry point to enter through;
//   2. the pure one-time migration (resetSidebarShape) - a sidebar carrying a
//      local split tree was the deleted model's work and resets to the clean
//      default, while center and bottom layouts survive untouched;
//   3. the loader's one-time run (createDock on a pre-v4 record) plus the v4
//      record that proves the reset can never fire twice.
// Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  allNodes,
  createDock,
  deserializeLayout,
  findEdgePane,
  findPane,
  growRoot,
  isPane,
  makePane,
  makeSplit,
  resetSidebarShape,
  serialize,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import { splitFitsZone, type PaneZone } from "../app/composables/panes.ts";
import { zoneForPointer } from "../app/composables/dockdnd.ts";
import { DOCK_LAYOUT_KEY } from "../app/composables/dock.ts";

class MemoryStorage {
  private m = new Map<string, string>();
  getItem(k: string): string | null {
    return this.m.get(k) ?? null;
  }
  setItem(k: string, v: string): void {
    this.m.set(k, String(v));
  }
  removeItem(k: string): void {
    this.m.delete(k);
  }
  get length(): number {
    return this.m.size;
  }
  key(): string | null {
    return null;
  }
  clear(): void {
    this.m.clear();
  }
}
function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}
const asSplit = (n: DockNode) => n as DockSplit;
const tabComps = (p: DockPane) => p.tabs.map((t) => t.component);

/** the shell's tabs on a FRESH boot - the "clean default" this card resets to */
function freshShells(): { left: string[]; right: string[] } {
  const dock = createDock(new MemoryStorage() as unknown as Storage);
  const tree = dock.tree.value;
  return {
    left: tabComps(findEdgePane(tree, "sidebar")!),
    right: tabComps(findEdgePane(tree, "inspector")!),
  };
}

/** the shell arrangement: [left sidebar | main] over the bottom shell */
function shellTree(): DockSplit {
  const left: DockPane = {
    kind: "pane",
    id: "n2",
    tabs: [{ component: "sidebar" }, { component: "agents" }],
    active: 0,
    fixed: 264,
    edge: "left",
  };
  const main = makePane("chat");
  const right: DockPane = {
    kind: "pane",
    id: "n9",
    tabs: [{ component: "files" }, { component: "tools" }],
    active: 0,
    fixed: 264,
    edge: "right",
  };
  return makeSplit("row", [left, main, right]);
}

/** the pane whose bar holds `component` */
function paneWith(tree: DockNode, component: string): DockPane | undefined {
  for (const n of allNodes(tree)) {
    if (isPane(n) && n.tabs.some((t) => t.component === component)) return n;
  }
  return undefined;
}

describe("the shape predicate (canvas C2: tabs + stacks only)", () => {
  it("a sidebar stacks and never splits into columns; main and bottom split both ways", () => {
    for (const zone of ["left", "right"] as const) {
      assert.equal(splitFitsZone(zone, "row"), false, `${zone}: no side-by-side columns`);
      assert.equal(splitFitsZone(zone, "column"), true, `${zone}: the stack stays`);
    }
    for (const zone of ["main", "bottom"] as const) {
      assert.equal(splitFitsZone(zone, "row"), true);
      assert.equal(splitFitsZone(zone, "column"), true);
    }
  });

  it("the pointer read has no left/right half inside a sidebar (the gesture is gone, not refused)", () => {
    const rect = { x: 100, y: 50, w: 264, h: 700 };
    const sides = new Set<string>();
    for (let x = rect.x; x <= rect.x + rect.w; x += 4) sides.add(zoneForPointer("left", x, 400, rect));
    for (let x = rect.x; x <= rect.x + rect.w; x += 4) sides.add(zoneForPointer("right", x, 400, rect));
    assert.deepEqual([...sides], ["center"], "every x in a sidebar's height band is the fold");
    assert.equal(zoneForPointer("left", 5, rect.y + 20, rect), "top");
    assert.equal(zoneForPointer("left", 5, rect.y + rect.h - 20, rect), "bottom");
    assert.equal(zoneForPointer("main", rect.x + 10, 400, rect), "left");
    assert.equal(zoneForPointer("main", rect.x + rect.w - 10, 400, rect), "right");
    const zone: PaneZone = "bottom";
    assert.equal(zoneForPointer(zone, rect.x + rect.w - 10, 400, rect), "right");
  });
});

describe("resetSidebarShape (the one-time migration, pure)", () => {
  it("a grown sidebar resets to an empty single pane; the marker and width stay", () => {
    const left = findPane(shellTree(), "n2") as DockPane;
    const grown = growRoot(shellTree(), left.id, "column", "after");
    assert.ok(grown);
    const out = resetSidebarShape(grown.tree);
    const shell = findPane(out, left.id) as DockPane;
    assert.equal(shell.grown, undefined, "the stack tree is gone");
    assert.deepEqual(shell.tabs, [], "the bar empties for the seat repair to refill");
    assert.equal(shell.edge, "left", "the zone marker survives");
    assert.equal(shell.fixed, 264, "the width is not part of the deleted model");
  });

  it("a sidebar with no stack tree is untouched (same reference) - the break is as wide as the feature was", () => {
    const t = shellTree();
    assert.equal(resetSidebarShape(t), t);
  });

  it("center and bottom layouts survive untouched: main row splits and the bottom's grown tree", () => {
    const tree = shellTree();
    // a grown BOTTOM shell (its home tabs) under a main row split
    const bottom: DockPane = {
      kind: "pane", id: "n70", tabs: [{ component: "terminal" }],
      active: 0, fixed: 200, edge: "bottom",
    };
    const col = makeSplit("column", [tree, bottom]);
    const bottomGrown = growRoot(col, bottom.id, "row", "before");
    assert.ok(bottomGrown, "the bottom shell grows a side-by-side pair - legal there");
    const grownBottomTree = bottomGrown.tree;
    // ..and a grown SIDEBAR beside it
    const leftId = findPane(grownBottomTree, "n2")!.id;
    const withGrownSidebar = growRoot(grownBottomTree, leftId, "column", "after");
    assert.ok(withGrownSidebar);

    const out = resetSidebarShape(withGrownSidebar.tree);
    const outBottom = findPane(out, bottom.id) as DockPane;
    assert.ok(outBottom, "the bottom shell survives");
    assert.ok(outBottom.grown, "the bottom's grown tree is NOT the deleted model's work");
    assert.equal(asSplit(outBottom.grown!).direction, "row", "its side-by-side split stands");
    const outLeft = findPane(out, leftId) as DockPane;
    assert.equal(outLeft.grown, undefined, "the sidebar's stack tree resets");
    assert.equal(asSplit(out).children[0]!.kind, "split", "the main row's split tree stands");
  });
});

describe("the loader's one-time run (createDock)", () => {
  /** a pre-v4 record: the shell arrangement with the LEFT sidebar grown */
  function preV4Record(): string {
    const tree = shellTree();
    const grown = growRoot(tree, "n2", "column", "after");
    assert.ok(grown);
    const rec = JSON.parse(serialize(grown.tree)) as { v: number };
    rec.v = 3;
    return JSON.stringify(rec);
  }

  it("a split sidebar from the old model resets to the clean default on first load", async () => {
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, preV4Record());
    const dock = createDock(store as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const fresh = freshShells();
    const left = findEdgePane(tree, "sidebar")!;
    assert.equal(left.grown, undefined, "no stack tree survives the load");
    assert.deepEqual(tabComps(left), fresh.left, "the sidebar is the clean default");
    const right = findEdgePane(tree, "inspector")!;
    assert.equal(right.grown, undefined, "no stack tree there either");
    assert.deepEqual(tabComps(right).slice(0, 2), ["files", "tools"],
      "a sidebar with no stack tree is NOT reset - it keeps its own order");
    assert.deepEqual([...tabComps(right)].sort(), [...fresh.right].sort(),
      "and still holds the whole class (the seat repair only appends what was missing)");
    const main = paneWith(tree, "chat")!;
    assert.deepEqual(tabComps(main), ["chat"], "the main pane kept its work");
    assert.ok(dock.bootRepairs.some((r) => /sidebar shape law/.test(r)),
      `the boot says what it did: ${JSON.stringify(dock.bootRepairs)}`);
  });

  it("the reset runs ONCE: the boot re-persists v4, and a v4 record never resets again", async () => {
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, preV4Record());
    createDock(store as unknown as Storage);
    await flush();
    const persisted = deserializeLayout(store.getItem(DOCK_LAYOUT_KEY))!;
    assert.equal(persisted.version, 4, "the record is the shape law's now");

    const store2 = new MemoryStorage();
    store2.setItem(DOCK_LAYOUT_KEY, store.getItem(DOCK_LAYOUT_KEY)!);
    const dock2 = createDock(store2 as unknown as Storage);
    await flush();
    assert.deepEqual(dock2.bootRepairs, [], "a current record earns no receipt");
    assert.ok(!JSON.stringify(dock2.tree.value).includes('"grown"') ||
      !(findEdgePane(dock2.tree.value, "sidebar") as DockPane).grown,
      "no sidebar stack tree can reappear");
  });

  it("a closed sidebar's stow is dropped when it carries a stack tree", async () => {
    const tree = shellTree();
    const left = findPane(tree, "n2") as DockPane;
    const grown = growRoot(tree, left.id, "column", "after");
    assert.ok(grown);
    const grownShell = findPane(grown.tree, "n2") as DockPane;
    const hidden = { kind: "pane" as const, id: "n80", tabs: [], active: 0, fixed: 264, edge: "left" as const, grown: grownShell.grown };
    const rec = JSON.parse(serialize(grown.tree, ["sidebar"], 1000, { sidebar: hidden })) as { v: number };
    rec.v = 3;
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, JSON.stringify(rec));
    createDock(store as unknown as Storage);
    await flush();
    const back = deserializeLayout(store.getItem(DOCK_LAYOUT_KEY))!;
    assert.equal(back.hiddenEdges?.sidebar, undefined, "the stale sidebar stow is gone");
  });
});
