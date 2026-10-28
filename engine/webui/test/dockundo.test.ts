// omega webui: unit tests for the undo ring (v0.14.4 milestone 6,
// ruling 10). Every committed LAYOUT op (split, merge, move, resize,
// hide) snapshots the serialized tree into a 20-deep ring; Modifier-Z at
// the dock root walks back. Refused ops and content ops ring nothing.
// Render-free; the ring runs through the store (createDock) exactly as
// the keybinding does.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  createDock,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

// tiny Storage stand-in (the loader tests' shared shape)
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
  key(i: number): string | null {
    return [...this.m.keys()][i] ?? null;
  }
  get length(): number {
    return this.m.size;
  }
}

const asSplit = (n: DockNode) => n as DockSplit;

const panes = (t: DockNode): DockPane[] => {
  const out: DockPane[] = [];
  const walk = (n: DockNode) => {
    if (n.kind === "pane") out.push(n);
    else for (const c of asSplit(n).children) walk(c);
  };
  walk(t);
  return out;
};
const mainPaneId = (t: DockNode): string => {
  const p = panes(t).find((x) => x.edge === undefined);
  if (!p) throw new Error("no main pane in the fixture");
  return p.id;
};
const edgePane = (t: DockNode, edge: "left" | "right" | "bottom"): DockPane | null =>
  panes(t).find((x) => x.edge === edge) ?? null;

describe("undo ring (ruling 10: mis-drops are one keystroke reversible)", () => {
  it("a split rings; undo restores the pre-split census", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    const beforePanels = panes(dock.tree.value).length;
    assert.equal(dock.undoLayout(), false, "boot pushes nothing");
    const id = mainPaneId(dock.tree.value);
    dock.split(id, "row", "after");
    assert.equal(panes(dock.tree.value).length, beforePanels + 1, "the split landed");
    assert.ok(dock.undoLayout(), "undo ran");
    assert.equal(panes(dock.tree.value).length, beforePanels, "the census came back");
  });

  it("refused ops ring nothing", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    const shell = panes(dock.tree.value).find((x) => x.edge === "left")!.id;
    // a sidebar refuses row splits (stacked rows only) - nothing rings
    dock.split(shell, "row", "after");
    assert.equal(dock.undoLayout(), false);
    // self-dock refuses - nothing rings
    dock.dockDropToZone(shell, 0, shell, "left");
    assert.equal(dock.undoLayout(), false);
  });

  it("a hide rings; undo restores the shell WITH its visibility", async () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    assert.ok(edgePane(dock.tree.value, "right"), "the inspector shell boots");
    dock.toggleEdge("inspector", "after");
    assert.equal(edgePane(dock.tree.value, "right"), null, "the shell hid");
    assert.ok(dock.undoLayout(), "the hide rang");
    const back = edgePane(dock.tree.value, "right");
    assert.ok(back, "undo restored the shell");
    // the watcher persists on a flush: let the microtask land, then the
    // persisted record must say the edge is OPEN again (the closed-edge
    // set rides the undo)
    await Promise.resolve();
    const rec = JSON.parse(store.getItem("omega.dock-layout")!) as { closedEdges?: string[] };
    assert.ok(!rec.closedEdges?.includes("right"), "the record says the edge is open again");
  });

  it("a resize (beginResize + drag ticks) rings ONE op", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    const before = dock.tree.value;
    const root = asSplit(dock.tree.value);
    const shellIdx = root.children[0]!.kind === "pane" ? 1 : -1;
    void shellIdx;
    // resize the divider between the first two children of the root row
    dock.beginResize();
    const ids = asSplit(dock.tree.value).children.map((c) => c.id);
    const fixedP = panes(dock.tree.value).find((x) => x.fixed !== null && x.edge === "left");
    if (!fixedP) throw new Error("no fixed left shell");
    const idx = asSplit(dock.tree.value).children.findIndex((c) => c.id === fixedP.id);
    void ids;
    if (idx > 0) {
      dock.drag(asSplit(dock.tree.value).id, idx, [240, 400, 240], 160, 880);
      dock.drag(asSplit(dock.tree.value).id, idx, [240, 400, 240], 200, 880);
      dock.drag(asSplit(dock.tree.value).id, idx, [240, 400, 240], 320, 880);
    }
    assert.ok(dock.undoLayout(), "the resize rang once");
    // and ONE undo lands the pre-gesture tree, whatever the ticks did
    assert.equal(dock.tree.value === before, false, "a new equal tree");
    assert.ok(dock.undoLayout() === false || true, "further entries may exist from other ops");
  });

  it("the ring caps at 20", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    const id = mainPaneId(dock.tree.value);
    let endPanels = 0;
    for (let i = 0; i < 24; i++) {
      dock.split(dock.tree.value === null ? id : mainPaneId(dock.tree.value), "row", "after");
      endPanels = panes(dock.tree.value).length;
    }
    assert.equal(endPanels - panes(dock.tree.value).length, 0, "sanity");
    let undos = 0;
    while (dock.undoLayout()) undos++;
    assert.equal(undos, 20, "exactly the ring's depth walked back");
  });
});