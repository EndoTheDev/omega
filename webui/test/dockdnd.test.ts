// omega webui: unit tests for the docking prediction + mutation.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  tabDrag,
  dropAllowedFor,
  zoneForPoint,
  zoneForPointer,
  zoneRectFractions,
  zoneSplitOf,
  type DragZone,
} from "../app/composables/dockdnd.ts";
import {
  makePane,
  makeSplit,
  dockDropToZone,
  predictDockDrop,
  type DockDropPrediction,
  findPane,
  allNodes,
  isPane,
  isSplit,
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
    assert.equal(zoneForPoint(107.5, 70, rect), "left"); // rx .015 < ry .05
    assert.equal(zoneForPoint(115, 65, rect), "left"); // rx .03 < ry .0375
    // top-right corner: ry.015 < 1-rx.03
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
  it("canvas C2: a sidebar reads its vertical lane only - no column half can be entered", () => {
    const rect = { x: 0, y: 0, w: 240, h: 600 };
    for (const zone of ["main", "bottom"] as const) {
      assert.equal(zoneForPointer(zone, 12, 300, rect), "left");
      assert.equal(zoneForPointer(zone, 228, 300, rect), "right");
      assert.equal(zoneForPointer(zone, 120, 20, rect), "top");
      assert.equal(zoneForPointer(zone, 120, 580, rect), "bottom");
      assert.equal(zoneForPointer(zone, 120, 300, rect), "center");
    }
    // a sidebar: x is ignored - every x reads the same vertical lane, so the
    // side-by-side zones of the deleted split gesture do not exist
    for (const zone of ["left", "right"] as const) {
      for (const x of [0, 12, 120, 228, 240]) {
        assert.notEqual(zoneForPointer(zone, x, 300, rect), "left");
        assert.notEqual(zoneForPointer(zone, x, 300, rect), "right");
      }
      assert.equal(zoneForPointer(zone, 12, 300, rect), "center");
      assert.equal(zoneForPointer(zone, 228, 20, rect), "top");
      assert.equal(zoneForPointer(zone, 0, 580, rect), "bottom");
    }
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
  it("drop left: B splits, the dragged tab takes the left half, the emptied source closes (t_aa9dbada)", () => {
    const { root, a, b } = row();
    const out = dockDropToZone(root, a.id, 0, b.id, "left");
    assert.notEqual(out, root); // a real mutation
    assert.equal(paneCount(out), 2); // A closes with its last tab; B split around N
    assert.ok(!findPane(out, a.id), "the emptied source pane closed");
    const pair = out as DockSplit;
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
      // a second tab in A: the drop moves tab 0 but A keeps hosting, so the source.
      (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
      return { root, a, b, out: dockDropToZone(root, a.id, 1, b.id, zone) };
    };
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
    (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
    const out = dockDropToZone(root, a.id, 1, b.id, "center");
    assert.equal(paneCount(out), 2);
    const B = paneOf(out, b.id);
    assert.deepEqual(B.tabs.map((t) => t.component), ["files", "chat"]);
    assert.equal(B.active, 1, "the moved tab FOCUSES in the target");
  });

  it("same-pane drop: the CENTER zone still self-refuses (card t_ae9ac5cf - the split zones self-split now)", () => {
    const { root, a } = row();
    assert.equal(dockDropToZone(root, a.id, 0, a.id, "center"), root);
    assert.notEqual(dockDropToZone(root, a.id, 0, a.id, "left"), root, "a same-pane half drop splits (VS Code edge read)");
  });

  it("edge shells GROW (v0.14.4 ruling 1): sidebar rows refuse, column grows, center groups", () => {
    // A(files - a sidebar view visiting the main pane) | B(kanban, pinned right shell).
    // canvas C1: the dragged view must be one the class law seats in a sidebar - a chat
    // tab would be refused by the seat gate before the shape law spoke.
    const a = makePane("files");
    const b = makePane("kanban", 240, "right");
    const root = makeSplit("row", [a, b]);
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "left"), root, "a sidebar stacks rows only");
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "right"), root);
    const grown = dockDropToZone(root, a.id, 0, b.id, "bottom");
    const B = paneOf(grown, b.id)!;
    assert.ok(B.grown, "the right root grew a local split");
    assert.equal(B.grown!.direction, "column");
    assert.equal(B.tabs.length, 0, "the root's classic bar emptied");
    const shellLeaf = B.grown!.children[0] as DockPane;
    assert.deepEqual(shellLeaf.tabs.map((t) => t.component), ["kanban"], "the shell's content moved to the first leaf");
    const fresh = B.grown!.children[1] as DockPane;
    assert.deepEqual(fresh.tabs.map((t) => t.component), ["files"], "the dragged tab seats in the fresh leaf");
    const center = dockDropToZone(root, a.id, 0, b.id, "center");
    const BC = paneOf(center, b.id);
    assert.deepEqual(BC.tabs.map((t) => t.component), ["kanban", "files"], "center still groups");
    const AC = findPane(center, a.id);
    assert.ok(AC, "the emptied source pane survives (the last main pane never closes)");
    // the drained last main pane sits EMPTY - the retired placeholder is never seated.
    assert.deepEqual(AC!.tabs.map((t) => t.component), [], "A stays empty, no seat is minted");
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

describe("predictDockDrop (v0.14.4 milestone 1: the one formula)", () => {
  const ZONES = ["left", "right", "top", "bottom", "center"] as const;

  // the load-bearing law: prediction and commit share ONE pure function.

  /** canonical structural signature: ids -> #n by DFS first appearance. */
  function canon(t: DockNode): unknown {
    const ids = new Map<string, string>();
    const nid = (id: string) => {
      if (!ids.has(id)) ids.set(id, "#" + ids.size);
      return ids.get(id);
    };
    const walk = (n: DockNode): unknown =>
      isPane(n)
        ? {
            kind: "p", id: nid(n.id), fixed: n.fixed, edge: n.edge ?? null,
            active: n.active, tabs: n.tabs,
            grown: n.grown && isSplit(n.grown) ? walk(n.grown) : null,
          }
        : { kind: "s", id: nid(n.id), dir: n.direction, sizes: n.sizes, children: n.children.map(walk) };
    return walk(t);
  }
  const sameShape = (x: DockNode, y: DockNode) => assert.deepEqual(canon(x), canon(y));

  /** narrowing guards (assert.ok cannot narrow the `in` union). */
  function expectDrop(p: DockDropPrediction, msg?: string): { tree: DockNode; freshPaneId: string | null } {
    if ("refused" in p) throw new Error(`predicted a drop, got a refusal: ${p.reason}${msg ? ` (${msg})` : ""}`);
    return p;
  }
  function expectRefusal(p: DockDropPrediction): string {
    if (!("refused" in p)) throw new Error("predicted a drop, expected a refusal");
    return p.reason;
  }

  function addedPane(before: DockNode, after: DockNode): DockPane | null {
    const prior = new Set<string>();
    for (const n of allNodes(before)) if (isPane(n)) prior.add(n.id);
    for (const n of allNodes(after)) {
      if (isPane(n) && !prior.has(n.id)) return n;
    }
    return null;
  }

  it("every zone on a plain two-pane row: prediction == commit", () => {
    for (const zone of ZONES) {
      const { root, a, b } = row();
      (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
      // commit first, THEN prediction off the same original: the predictor never.
      const committed = dockDropToZone(root, a.id, 1, b.id, zone);
      const predicted = predictDockDrop(root, a.id, 1, b.id, zone);
      if ("refused" in predicted) {
        assert.equal(committed, root, `${zone}: a refusal must match an unchanged commit`);
        continue;
      }
      assert.notEqual(committed, root, `${zone}: sanity - the commit really mutates here`);
      sameShape(predicted.tree, committed);
      if (predicted.freshPaneId === null) {
        assert.equal(addedPane(root, predicted.tree), null, `${zone}: freshPaneId null means no minted pane`);
        assert.equal(addedPane(root, committed), null, `${zone}: the commit mints nothing either`);
        continue;
      }
      const fresh = addedPane(root, predicted.tree)!;
      assert.equal(predicted.freshPaneId, fresh.id, `${zone}: freshPaneId names the minted pane of its own call`);
      assert.deepEqual(fresh.tabs.map((t) => t.component), ["chat"], `${zone}: the tab sits in the fresh pane`);
      const committedFresh = addedPane(root, committed)!;
      assert.deepEqual(committedFresh.tabs.map((t) => t.component), ["chat"], `${zone}: commit seats the tab too`);
    }
  });

  it("drop left deep-detail: prediction exposes the fresh pane and lands the tab", () => {
    const { root, a, b } = row();
    const predicted = expectDrop(predictDockDrop(root, a.id, 0, b.id, "left"));
    assert.notEqual(predicted.tree, root);
    const fresh = addedPane(root, predicted.tree)!;
    assert.equal(predicted.freshPaneId, fresh.id);
    const pair = predicted.tree as DockSplit;
    assert.deepEqual(pair.sizes, [0.5, 0.5]);
    sameShape(predicted.tree, dockDropToZone(root, a.id, 0, b.id, "left"));
  });

  it("center predicts a plain move with freshPaneId null", () => {
    const { root, a, b } = row();
    (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
    const predicted = expectDrop(predictDockDrop(root, a.id, 1, b.id, "center"));
    assert.equal(predicted.freshPaneId, null);
    assert.equal(addedPane(root, predicted.tree), null, "center mints nothing");
    sameShape(predicted.tree, dockDropToZone(root, a.id, 1, b.id, "center"));
  });

  it("refusal matrix: every refusal matches a tree-unchanged commit", () => {
    // same-pane CENTER self-docks never
    const { root, a } = row();
    (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
    assert.equal(dockDropToZone(root, a.id, 0, a.id, "center"), root);
    assert.equal(typeof expectRefusal(predictDockDrop(root, a.id, 0, a.id, "center")), "string");
    const { root: rv, a: av, b } = row();
    for (const zone of ZONES) {
      assert.equal(dockDropToZone(rv, "n999", 0, b.id, zone), rv);
      assert.equal(dockDropToZone(rv, av.id, 0, "n999", zone), rv);
      assert.equal(dockDropToZone(rv, av.id, 7, b.id, zone), rv);
      expectRefusal(predictDockDrop(rv, "n999", 0, b.id, zone));
      expectRefusal(predictDockDrop(rv, av.id, 0, "n999", zone));
      expectRefusal(predictDockDrop(rv, av.id, 7, b.id, zone));
    }
    // v0.14.4 the SHELL's body drop grows its root now.
    const a2 = makePane("files");
    const b2 = makePane("kanban", 240, "right");
    const sroot = makeSplit("row", [a2, b2]);
    for (const zone of ["left", "right"] as const) {
      assert.equal(dockDropToZone(sroot, a2.id, 0, b2.id, zone), sroot);
      assert.equal(dockDropToZone(sroot, "n999", 0, b2.id, zone), sroot);
      expectRefusal(predictDockDrop(sroot, a2.id, 0, b2.id, zone));
    }
    for (const zone of ["top", "bottom"] as const) {
      assert.notEqual(dockDropToZone(sroot, a2.id, 0, b2.id, zone), sroot, `${zone}: growth`);
      assert.equal(dockDropToZone(sroot, "n999", 0, b2.id, zone), sroot, `${zone}: vanished source`);
      const p = expectDrop(predictDockDrop(sroot, a2.id, 0, b2.id, zone), zone);
      sameShape(p.tree, dockDropToZone(structuredClone(sroot), a2.id, 0, b2.id, zone));
    }
  });

  it("shells: sidebar row zones refuse with a reason, column zones grow, center predicts the group", () => {
    // canvas C1: a SIDEBAR-class view is the one the seat gate lets land.
    const a = makePane("files");
    const b = makePane("kanban", 240, "right");
    const root = makeSplit("row", [a, b]);
    for (const zone of ["left", "right"] as const) {
      assert.equal(expectRefusal(predictDockDrop(root, a.id, 0, b.id, zone)), "a sidebar stacks - it never splits into columns");
      assert.equal(dockDropToZone(root, a.id, 0, b.id, zone), root);
    }
    for (const zone of ["top", "bottom"] as const) {
      const p = expectDrop(predictDockDrop(root, a.id, 0, b.id, zone), zone);
      sameShape(p.tree, dockDropToZone(structuredClone(root), a.id, 0, b.id, zone));
      assert.ok(paneOf(p.tree, b.id)!.grown, `${zone}: the root grew`);
    }
    const center = expectDrop(predictDockDrop(root, a.id, 0, b.id, "center"), "shell center still groups");
    sameShape(center.tree, dockDropToZone(root, a.id, 0, b.id, "center"));
  });

  it("the LOCKED tab refuses every landing but its own seat", () => {
    const left = makePane("sidebar", 240, "left");
    const chat = makePane("chat");
    const root = makeSplit("row", [left, chat]);
    for (const zone of ZONES) {
      expectRefusal(predictDockDrop(root, left.id, 0, chat.id, zone));
      assert.equal(dockDropToZone(root, left.id, 0, chat.id, zone), root);
    }
    expectRefusal(predictDockDrop(root, left.id, 0, left.id, "center"));
    // the one legal landing: ITS OWN seat. A persisted stray sessions tab
    // may come home to the left shell - the class lets it, the lock narrows
    // it to exactly this zone (canvas C1: seatAllowsIn reads both).
    const stray = makePane("sidebar");
    const root2 = makeSplit("row", [left, stray, chat]);
    const ok = expectDrop(predictDockDrop(root2, stray.id, 0, left.id, "center"), "its own bar accepts sessions");
    sameShape(ok.tree, dockDropToZone(root2, stray.id, 0, left.id, "center"));
    expectRefusal(predictDockDrop(root2, stray.id, 0, chat.id, "center"));
  });

  it("the predictor is pure: repeated calls match and never touch the input", () => {
    const { root, a, b } = row();
    const frozen = JSON.stringify(root);
    const p1 = expectDrop(predictDockDrop(root, a.id, 0, b.id, "left"));
    assert.equal(JSON.stringify(root), frozen, "the source tree is untouched");
    const p2 = expectDrop(predictDockDrop(root, a.id, 0, b.id, "left"));
    assert.notEqual(p2.tree, p1.tree, "separate calls mint separate ids");
    sameShape(p2.tree, p1.tree, "the structure is identical anyway");
    assert.notDeepEqual(p1.tree, root);
    sameShape(p1.tree, dockDropToZone(root, a.id, 0, b.id, "left"));
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

  it("dropAllowedFor: the class law answers the target zone, null gesture refuses", () => {
    // canvas C1: a MAIN member only lands in the main pane
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor("right", { paneId: "p", index: 0, component: "chat" }), false);
    assert.equal(dropAllowedFor("bottom", { paneId: "p", index: 0, component: "chat" }), false);
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "sidebar" }), false);
    assert.equal(dropAllowedFor("left", { paneId: "p", index: 0, component: "sidebar" }), true);
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "files" }), true);
    assert.equal(dropAllowedFor("left", { paneId: "p", index: 0, component: "files" }), true);
    // a bottom member visits the sidebars, never main
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "terminal" }), false);
    assert.equal(dropAllowedFor("left", { paneId: "p", index: 0, component: "terminal" }), true);
    assert.equal(dropAllowedFor("left", null), false);
  });
});