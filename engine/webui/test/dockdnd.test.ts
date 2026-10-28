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
  predictDockDrop,
  type DockDropPrediction,
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
  it("drop left: B splits, the dragged tab takes the left half, the emptied source closes (t_aa9dbada)", () => {
    const { root, a, b } = row();
    const out = dockDropToZone(root, a.id, 0, b.id, "left");
    assert.notEqual(out, root); // a real mutation
    assert.equal(paneCount(out), 2); // A closes with its last tab; B split around N
    assert.ok(!findPane(out, a.id), "the emptied source pane closed");
    // the tree: row[N, B] - the emptied A pruned, the pair hoisted to root
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
      // a second tab in A: the drop moves tab 0 but A keeps hosting, so the
      // source pane survives and the structure reads stay split-focused
      (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
      return { root, a, b, out: dockDropToZone(root, a.id, 1, b.id, zone) };
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
    // A keeps a tab so the source pane survives - this test reads the plain
    // move semantics, not the last-tab-out close (covered in its own test)
    (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
    const out = dockDropToZone(root, a.id, 1, b.id, "center");
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
    // A(chat) | B(files, pinned right shell) - A's ONLY tab moves to B's
    // center. Endo ruling 2026-10-25: A is the LAST MAIN pane, so it
    // survives the move as the empty launcher (the move-out auto-close
    // exception); the pinned shell itself never closes either (it may
    // sit empty)
    const a = makePane("chat");
    const b = makePane("files", 240, "right");
    const root = makeSplit("row", [a, b]);
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "left"), root, "shells don't split");
    assert.equal(dockDropToZone(root, a.id, 0, b.id, "bottom"), root);
    const center = dockDropToZone(root, a.id, 0, b.id, "center");
    const B = paneOf(center, b.id);
    assert.deepEqual(B.tabs.map((t) => t.component), ["files", "chat"], "center still groups");
    const A = findPane(center, a.id);
    assert.ok(A, "the emptied source pane survives (the last main pane never closes)");
    assert.equal(A!.tabs.length, 0, "A sits empty as the launcher");
    assert.equal(paneCount(center), 2, "both panes stay");
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

  // the load-bearing law (v0.14.4 ruling 5): prediction and commit share
  // ONE pure function - every zone x every scenario asserts that
  // predictDockDrop's result matches dockDropToZone's, and that a refusal
  // matches the commit's tree-unchanged outcome. Two separate calls each
  // mint their own node ids (insertPane is the only id source), so
  // cross-call equality is structural: canon() rewrites ids to
  // first-appearance order. Within one prediction the ids are real -
  // freshPaneId names the exact pane in predicted.tree.

  /** canonical structural signature: ids -> #n by DFS first appearance. */
  function canon(t: DockNode): unknown {
    const ids = new Map<string, string>();
    const nid = (id: string) => {
      if (!ids.has(id)) ids.set(id, "#" + ids.size);
      return ids.get(id);
    };
    const walk = (n: DockNode): unknown =>
      isPane(n)
        ? { kind: "p", id: nid(n.id), fixed: n.fixed, edge: n.edge ?? null, active: n.active, tabs: n.tabs }
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

  /** id-diff helper: the pane the call actually added, if any. */
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
      // commit first, THEN prediction off the same original: the predictor
      // never reads the counter state of the commit's minting, so a match
      // here is a genuine one-formula result, not an ordered accident
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
      // and the committed fresh pane holds the same tab
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
    // same-pane zones never dock
    for (const zone of ZONES) {
      const { root, a } = row();
      (root.children[0] as DockPane).tabs = [{ component: "files" }, { component: "chat" }];
      assert.equal(dockDropToZone(root, a.id, 0, a.id, zone), root);
      const reason = expectRefusal(predictDockDrop(root, a.id, 0, a.id, zone));
      assert.equal(typeof reason, "string");
    }
    // vanished counterpart / vanished index refuse
    const { root, a, b } = row();
    for (const zone of ZONES) {
      assert.equal(dockDropToZone(root, "n999", 0, b.id, zone), root);
      assert.equal(dockDropToZone(root, a.id, 0, "n999", zone), root);
      assert.equal(dockDropToZone(root, a.id, 7, b.id, zone), root);
      expectRefusal(predictDockDrop(root, "n999", 0, b.id, zone));
      expectRefusal(predictDockDrop(root, a.id, 0, "n999", zone));
      expectRefusal(predictDockDrop(root, a.id, 7, b.id, zone));
    }
    // shells keep their shape: split zones refuse even on vanished source
    const a2 = makePane("chat");
    const b2 = makePane("files", 240, "right");
    const sroot = makeSplit("row", [a2, b2]);
    for (const zone of ["left", "right", "top", "bottom"] as const) {
      assert.equal(dockDropToZone(sroot, a2.id, 0, b2.id, zone), sroot);
      assert.equal(dockDropToZone(sroot, "n999", 0, b2.id, zone), sroot);
    }
  });

  it("shells: split zones refuse with a reason, center predicts the group", () => {
    const a = makePane("chat");
    const b = makePane("files", 240, "right");
    const root = makeSplit("row", [a, b]);
    for (const zone of ["left", "right", "top", "bottom"] as const) {
      expectRefusal(predictDockDrop(root, a.id, 0, b.id, zone));
      assert.equal(dockDropToZone(root, a.id, 0, b.id, zone), root);
    }
    const center = expectDrop(predictDockDrop(root, a.id, 0, b.id, "center"), "shell center still groups");
    sameShape(center.tree, dockDropToZone(root, a.id, 0, b.id, "center"));
  });

  it("the LOCKED tab refuses every landing the commit refuses", () => {
    const left = makePane("sidebar", 240, "left");
    const chat = makePane("chat");
    const root = makeSplit("row", [left, chat]);
    for (const zone of ZONES) {
      expectRefusal(predictDockDrop(root, left.id, 0, chat.id, zone));
      assert.equal(dockDropToZone(root, left.id, 0, chat.id, zone), root);
    }
    // the one legal landing: center on the left shell itself
    expectRefusal(predictDockDrop(root, left.id, 0, left.id, "center"));
    const other = makePane("files");
    const root2 = makeSplit("row", [left, other, chat]);
    const ok = expectDrop(predictDockDrop(root2, chat.id, 0, left.id, "center"), "center on left accepts the locked tab");
    sameShape(ok.tree, dockDropToZone(root2, chat.id, 0, left.id, "center"));
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
    // and the prediction still equals the commit executed later
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

  it("dropAllowedFor: free tabs drop anywhere, sessions only left, null gesture refuses", () => {
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor("right", { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor("bottom", { paneId: "p", index: 0, component: "chat" }), true);
    assert.equal(dropAllowedFor(null, { paneId: "p", index: 0, component: "sidebar" }), false);
    assert.equal(dropAllowedFor("left", { paneId: "p", index: 0, component: "sidebar" }), true);
    assert.equal(dropAllowedFor("left", null), false);
  });
});