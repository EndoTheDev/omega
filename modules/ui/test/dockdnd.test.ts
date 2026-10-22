// omega webui: unit tests for the docking prediction + mutation (card
// t_0e833eae): the pointer->zone math (dockdnd.ts) and the tree mutations
// the zones land (dock.ts dockDropToZone). Render-free; node's built-in
// test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  tabDrag,
  dropAllowedFor,
  zoneForPoint,
  zoneRectFractions,
  splitAllowedFor,
  zoneSplitOf,
  type DragZone,
} from "../app/composables/dockdnd.ts";
import {
  makePane,
  makeSplit,
  dockDropToZone,
  findPane,
  allNodes,
  isPane,
  paneCount,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

// a two-pane main row: source A (chat) over target B (files)
function row(): { root: DockSplit; a: DockPane; b: DockPane } {
  const a = makePane("chat");
  const b = makePane("files");
  return { root: makeSplit("row", [a, b]), a, b };
}
function paneOf(t: DockNode, id: string): DockPane {
  return findPane(t, id) as DockPane;
}
function panes(t: DockNode): DockPane[] {
  return [...allNodes(t)].filter(isPane);
}

describe("zone math (pointer -> region)", () => {
  // 500x400 box at (100, 50)
  const rect = { x: 100, y: 50, w: 500, h: 400 };

  it("the middle band on BOTH axes reads center", () => {
    assert.equal(zoneForPoint(350, 250, rect), "center");
    // the band's exact edges (0.25 / 0.75 of each axis) stay center
    assert.equal(zoneForPoint(100 + 125, 50 + 100, rect), "center");
    assert.equal(zoneForPoint(100 + 375, 50 + 300, rect), "center");
  });

  it("the four halves read their edge", () => {
    assert.equal(zoneForPoint(120, 250, rect), "left");
    assert.equal(zoneForPoint(580, 250, rect), "right");
    assert.equal(zoneForPoint(350, 60, rect), "top");
    assert.equal(zoneForPoint(350, 440, rect), "bottom");
  });

  it("corners score the NEARER crossed edge", () => {
    // top-left corner: both bands crossed, the smaller distance wins
    assert.equal(zoneForPoint(107.5, 70, rect), "left"); // rx .015 < ry .05
    assert.equal(zoneForPoint(115, 65, rect), "left"); // rx .03 < ry .0375
    // top-right corner: ry .015 < 1-rx .03
    assert.equal(zoneForPoint(585, 56, rect), "top");
    assert.equal(zoneForPoint(590, 440, rect), "right"); // 1-rx .02 beats 1-ry .025
  });

  it("clamps a pointer marginally outside the box to the nearest edge", () => {
    assert.equal(zoneForPoint(60, 250, rect), "left"); // rx clamped to 0
    assert.equal(zoneForPoint(700, 250, rect), "right");
    assert.equal(zoneForPoint(350, -30, rect), "top");
  });

  it("a degenerate (zero-size) rect folds to center", () => {
    assert.equal(zoneForPoint(5, 5, { x: 0, y: 0, w: 0, h: 100 }), "center");
    assert.equal(zoneForPoint(5, 5, { x: 0, y: 0, w: 100, h: 0 }), "center");
    assert.equal(zoneForPoint(5, 5, { x: 0, y: 0, w: -1, h: 0 }), "center");
  });

  it("zone rects are body fractions", () => {
    assert.deepEqual(zoneRectFractions("left"), { x: 0, y: 0, w: 0.5, h: 1 });
    assert.deepEqual(zoneRectFractions("right"), { x: 0.5, y: 0, w: 0.5, h: 1 });
    assert.deepEqual(zoneRectFractions("top"), { x: 0, y: 0, w: 1, h: 0.5 });
    assert.deepEqual(zoneRectFractions("bottom"), { x: 0, y: 0.5, w: 1, h: 0.5 });
    assert.deepEqual(zoneRectFractions("center"), { x: 0, y: 0, w: 1, h: 1 });
  });
});

describe("zone gates", () => {
  it("edge shells never split; main panes take every zone", () => {
    assert.equal(splitAllowedFor(undefined), true);
    for (const edge of ["left", "right", "bottom"] as const)
      assert.equal(splitAllowedFor(edge), false);
  });

  it("zones encode direction + side; center encodes a plain move", () => {
    assert.deepEqual(zoneSplitOf("left"), { direction: "row", side: "before" });
    assert.deepEqual(zoneSplitOf("right"), { direction: "row", side: "after" });
    assert.deepEqual(zoneSplitOf("top"), { direction: "column", side: "before" });
    assert.deepEqual(zoneSplitOf("bottom"), { direction: "column", side: "after" });
    assert.equal(zoneSplitOf("center"), null);
  });
});

describe("dockDropToZone (the docking mutation)", () => {
  it("drop left: B splits, the dragged tab takes the left half", () => {
    const { root, a, b } = row();
    const out = dockDropToZone(root, a.id, 0, b.id, "left");
    assert.notEqual(out, root); // a real mutation
    assert.equal(paneCount(out), 3); // A stays, B split around N
    // the row: [A, row[N, B]]
    const kids = (out as DockSplit).children as DockNode[];
    const A = paneOf(out, a.id);
    assert.deepEqual(A.tabs.map((t) => t.component), [], "A emptied to the '+' menu");
    const pair = kids[1] as DockSplit;
    assert.equal(pair.direction, "row");
    assert.deepEqual(pair.sizes, [0.5, 0.5], "the half goes to the dragged pane");
    const N = pair.children[0] as DockPane;
    assert.deepEqual(N.tabs.map((t) => t.component), ["chat"], "the tab lands in the new half");
    assert.equal(N.active, 0, "the landed tab FOCUSES");
    assert.equal((pair.children[1] as DockPane).id, b.id, "B keeps its slot");
  });

  it("drop right/top/bottom map the zone's direction + side", () => {
    const mk = (zone: DragZone) => {
      const { root, a, b } = row();
      return { root, a, b, out: dockDropToZone(root, a.id, 0, b.id, zone) };
    };
    // right: [A, row[B, N]] - the fresh pane rides right of the target
    const r = mk("right");
    const rightPair = (r.out as DockSplit).children[1] as DockSplit;
    assert.equal(rightPair.direction, "row");
    assert.equal((rightPair.children[0] as DockPane).id, r.b.id, "B keeps its slot");
    assert.deepEqual(
      (rightPair.children[1] as DockPane).tabs.map((t) => t.component),
      ["chat"],
    );

    // top: [A, column[N, B]] - the new pane rides ABOVE the target
    const r2 = mk("top");
    const topPair = (r2.out as DockSplit).children[1] as DockSplit;
    assert.equal(topPair.direction, "column");
    assert.deepEqual((topPair.children[0] as DockPane).tabs.map((t) => t.component), ["chat"]);
    assert.deepEqual((topPair.children[1] as DockPane).tabs.map((t) => t.component), ["files"]);

    // bottom: [A, column[B, N]] - the new pane rides BELOW the target
    let r3 = mk("bottom");
    const bottomPair = (r3.out as DockSplit).children[1] as DockSplit;
    assert.equal(bottomPair.direction, "column");
    assert.deepEqual((bottomPair.children[0] as DockPane).tabs.map((t) => t.component), ["files"]);
    assert.deepEqual((bottomPair.children[1] as DockPane).tabs.map((t) => t.component), ["chat"]);
  });

  it("drop center groups the tab into the target (no split)", () => {
    const { root, a, b } = row();
    const out = dockDropToZone(root, a.id, 0, b.id, "center");
    assert.equal(paneCount(out), 2);
    const B = paneOf(out, b.id);
    assert.deepEqual(B.tabs.map((t) => t.component), ["files", "chat"]);
    assert.equal(B.active, 1, "the moved tab FOCUSES in the target");
  });

  it("zones never self-dock (same-pane body hover keeps focus-on-drop)", () => {
    const { root, a } = row();
    assert.equal(dockDropToZone(root, a.id, 0, a.id, "left"), root);
  });

  it("edge shells keep their shape: split zones refuse, center groups", () => {
    // A(chat) | B(files, pinned right shell)
    const a = makePane("chat");
    const b = makePane("files", 240, "right");
    const root = makeSplit("row", [a, b]);
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "left"), root, "shells don't split");
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "bottom"), root);
    const center = dockDropToZone(root, a.id, 0, b.id, "center");
    const B = paneOf(center, b.id);
    assert.deepEqual(B.tabs.map((t) => t.component), ["files", "chat"], "center still groups");
    assert.equal(paneCount(center), 2, "no pane minted");
  });

  it("the LOCKED tab (sessions) refuses every zone landing outside left", () => {
    const a = makePane("sidebar", 240, "left");
    const b = makePane("chat");
    const root = makeSplit("row", [a, b]);
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "left"), root, "sessions cannot dock");
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "center"), root, "not even group");
  });

  it("a vanished pane mid-gesture is a no-op", () => {
    const { root, a, b } = row();
    assert.equal(dockDropToZone(root, "n999", 0, b.id, "left"), root);
    assert.equal(dockDropToZone(root, a.id, 0, "n999", "left"), root);
    // a vanishing SOURCE index
    assert.equal(dockDropToZone(root, a.id, 7, b.id, "left"), root);
  });
});

describe("the gesture record + legacy hit-test", () => {
  it("tabDrag start/current/end", () => {
    assert.equal(tabDrag.current(), null);
    tabDrag.start({ paneId: "p1", index: 0, component: "chat" });
    assert.deepEqual(tabDrag.current(), { paneId: "p1", index: 0, component: "chat" });
    tabDrag.end();
    assert.equal(tabDrag.current(), null);
  });

  it("dropAllowedFor: free tabs drop anywhere, sessions only left, null gesture refuses", () => {
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor("right", { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor("bottom", { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "sidebar" }), false);
    assert.equal(dropAllowedFor("left", { paneId: "p", index: 0, component: "sidebar" }), true);
    assert.equal(dropAllowedFor("left", null), false);
  });
});