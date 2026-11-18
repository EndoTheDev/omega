// omega webui: unit tests for the dock webui (app/composables/dock.ts).
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import {
  makePane,
  makeSplit,
  insertPane,
  removePane,
  movePane,
  splitPane,
  graftSplit,
  focusTab,
  setPaneComponent,
  openTab,
  closeTab,
  moveTab,
  MAIN_DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  PRIMARY_TAB_COMPONENTS,
  dragSplitDivider,
  narrowGiveWay,
  minDockPx,
  CHAT_FLOOR_PX,
  type NarrowHiddenEntry,
  serialize,
  deserialize,
  deserializeLayout,
  migrateLegacy,
  EDGE_DEFAULT_PX,
  nextNodeId,
  syncIdSeed,
  findPane,
  findComponentPane,
  findComponentTab,
  findEdgePane,
  openSingleton,
  createDock,
  DOCK_LAYOUT_KEY,
  BOTTOM_CLOSED_KEY,
  hideEdgePane,
  revealEdgePane,
  rememberEdgePx,
  recalledEdgePx,
  edgeComponentOf,
  allNodes,
  isPinnedPane,
  paneCount,
  firstMainPaneId,
  insertMainPane,
  expandInspectorTabs,
  rehomeInsightsTab,
  retireInspectorSettings,
  ensurePinnedEdges,
  bottomShellPane,
  forceSessionsLeft,
  restoreSeatSet,
  deriveEdgeMarkers,
  normalizeSizes,
  PANE_OPEN_MIN,
  LEGACY_WIDTH_KEYS,
  MIN_PANE_PX,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import { registerScratchSingleton } from "./scratchDockable.ts";
import { dropAllowedFor } from "../app/composables/dockdnd.ts";

// the tab-op fixtures below need a MAIN-area OPENABLE component.
let restoreScratch: (() => void) | null = null;
before(() => {
  restoreScratch = registerScratchSingleton();
});
after(() => {
  restoreScratch?.();
});

// helpers ---------------------------------------------------------------

function asPane(n: DockNode): DockPane {
  assert.equal(n.kind, "pane");
  return n as DockPane;
}
function asSplit(n: DockNode): DockSplit {
  assert.equal(n.kind, "split");
  return n as DockSplit;
}
/**
 * default shell shape: row [sidebar(fixed 240), chat(flex), inspector-pane.
 */
function contentTree(): DockSplit {
  const inspector: DockPane = {
    kind: "pane",
    id: nextNodeId(),
    tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
    active: 0,
    fixed: 300,
  };
  return asSplit(
    makeSplit("row", [
      makePane("sidebar", 240),
      makePane("chat", null),
      inspector,
    ]),
  );
}
function defaultTree(): DockSplit {
  return asSplit(ensurePinnedEdges(contentTree()));
}
function panes(t: DockNode): DockPane[] {
  return [...allNodes(t)].filter((n) => n.kind === "pane") as DockPane[];
}
function paneIds(t: DockNode): string[] {
  return panes(t).map((p) => p.id);
}
function comps(t: DockNode): (string | undefined)[] {
  const root = asSplit(t);
  const walk = (n: DockNode): (string | undefined)[] =>
    n.kind === "pane"
      ? [n.tabs[n.active]?.component]
      : n.children.flatMap(walk);
  return walk(root);
}
function paneByComponent(t: DockNode, c: "sidebar" | "chat" | "files"): DockPane {
  // ANY tab hosting, not active-only.
  const loc = findComponentTab(t, c);
  assert.ok(loc, `expected a pane hosting ${c}`);
  return findPane(t, loc.paneId)!;
}
function splits(t: DockNode): DockSplit[] {
  return [...allNodes(t)].filter((n) => n.kind === "split") as DockSplit[];
}
/** every tab component hosted by the pane `id`, in order */
function tabCompsIn(t: DockNode, id: string): (string | undefined)[] {
  const p = findPane(t, id);
  assert.ok(p, `expected pane ${id}`);
  return p!.tabs.map((x) => x.component);
}

describe("tree construction", () => {
  it("mints unique ids", () => {
    const ids = new Set([
      defaultTree().id,
      ...paneIds(defaultTree()),
      makePane("chat").id,
      nextNodeId(),
    ]);
    assert.equal(ids.size, 6);
  });

  it("normalizeSizes: equal default, fraction normalize, rejects bad input", () => {
    assert.deepEqual(normalizeSizes(3, undefined), [1 / 3, 1 / 3, 1 / 3]);
    assert.deepEqual(normalizeSizes(2, [1, 3]), [0.25, 0.75]);
    assert.deepEqual(normalizeSizes(2, [1]), [0.5, 0.5]); // length mismatch
    assert.deepEqual(normalizeSizes(2, [0, 0]), [0.5, 0.5]); // zero sum
    assert.deepEqual(normalizeSizes(2, [NaN, 1]), [0.5, 0.5]);
  });

  it("default shape carries the inspector tabs in registry order (files active)", () => {
    const t = defaultTree();
    const insp = asSplit(t).children[2];
    assert.deepEqual(
      asPane(insp).tabs.map((x) => x.component),
      [...INSPECTOR_TAB_COMPONENTS],
    );
    assert.equal(asPane(insp).active, 0);
  });
});

describe("insertPane / splitPane", () => {
  it("splits a mid pane: the new pane starts EMPTY (card t_9855a20e)", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const out = asSplit(splitPane(t, chat.id, "row", "after"));
    assert.equal(out.id, t.id); // root untouched
    const inner = asSplit(out.children[1]);
    assert.equal(inner.direction, "row");
    assert.equal(asPane(inner.children[0]).id, chat.id);
    // the empty-pane component menu is deleted AND the never-zero placeholder retired.
    assert.deepEqual(asPane(inner.children[1]).tabs.map((x) => x.component), []);
    assert.deepEqual(comps(out), ["sidebar", "chat", undefined, "files"]);
  });
  it("side 'before' puts the new pane left", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const out = asSplit(splitPane(t, chat.id, "row", "before"));
    const inner = asSplit(out.children[1]);
    assert.deepEqual(asPane(inner.children[0]).tabs.map((x) => x.component), []);
    assert.equal(asPane(inner.children[1]).id, chat.id);
  });
  it("wraps the root when the target IS the root pane", () => {
    const t = asPane(makePane("chat"));
    const out = asSplit(insertPane(t, t.id, "column", "after", makePane(null)));
    assert.equal(out.direction, "column");
    assert.equal(out.children[0].id, t.id);
  });
  it("depth-unlimited: split the new pane again", () => {
    let t: DockNode = defaultTree();
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after");
    const seat = panes(t).find((p) => p.tabs.length === 0)!;
    t = splitPane(t, seat.id, "column", "before");
    assert.equal(splits(t).length, 3); // root + row + column
  });
  it("no-op for an unknown target id", () => {
    const t = defaultTree();
    assert.equal(splitPane(t, "n999", "row", "after"), t);
  });
});

describe("removePane (card t_4ae27e0d refusals)", () => {
  it("removes a pane; the split keeps its survivors", () => {
    let t: DockNode = defaultTree();
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after"); // 4 panes now
    assert.equal(paneCount(t), 4);
    const fresh = panes(t).find((p) => p.tabs.length === 0)!; // the split-away pane
    const out = asSplit(removePane(t, fresh.id));
    assert.ok(!findPane(out, fresh.id));
    assert.equal(paneCount(out), 3);
  });
  it("a single-child split hoists to the child", () => {
    // split the chat: root [sidebar, row[chat, empty], files]; dropping
    // the fresh pane single-childrens the interior row - it hoists to the
    // chat pane itself (the webui guard never even called here: the
    // split never went below 2 children, it collapsed instead)
    let t: DockNode = defaultTree();
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after");
    const fresh = panes(t).find((p) => p.tabs.length === 0)!;
    t = removePane(t, fresh.id);
    assert.equal(paneCount(t), 3);
    const survivor = asSplit(t).children[1];
    assert.equal(survivor.kind, "pane", "the interior row hoisted to its child");
    assert.equal(survivor.id, chat.id, "the chat pane is the survivor");
  });
  it("the LAST REMAINING pane is refused (never-empty invariant)", () => {
    const t = asPane(makePane("chat"));
    assert.equal(removePane(t, t.id), t, "the tree object is returned untouched");
    assert.equal(removePane(t, t.id), t);
  });
  it("pinned panes refuse removal (sidebar panes are fixed chrome)", () => {
    const t = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const files = paneByComponent(t, "files");
    assert.equal(removePane(t, side.id), t, "sessions pane unclosable");
    assert.equal(removePane(t, files.id), t, "inspector pane unclosable");
    assert.equal(paneCount(t), 3);
  });
  it("no-op for an unknown id", () => {
    const t = defaultTree();
    assert.equal(removePane(t, "n999"), t);
  });
});

describe("movePane", () => {
  it("a MAIN pane moves: root [sidebar, hoisted-empty, chat, files]", () => {
    let t: DockNode = defaultTree();
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after");
    const mainHalf = findPane(t, chat.id)!;
    const out = movePane(t, mainHalf.id, paneByComponent(t, "files").id, "before");
    // the detach hoisted the chat's split to its EMPTY sibling, which now sits.
    assert.deepEqual(comps(out), ["sidebar", undefined, "chat", "files"]);
  });
  it("pinned panes refuse pane-moves (fixed chrome)", () => {
    let t: DockNode = defaultTree();
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after");
    const side = paneByComponent(t, "sidebar");
    const files = paneByComponent(t, "files");
    assert.equal(movePane(t, side.id, files.id, "after"), t);
    assert.equal(movePane(t, files.id, side.id, "before"), t);
  });
  it("moving out of a nested split collapses it to the survivor", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const t2 = splitPane(t, chat.id, "row", "after");
    const rowPane = findPane(t2, chat.id)!;
    const out = movePane(t2, rowPane.id, paneByComponent(t2, "files").id, "after");
    assert.deepEqual(comps(out), ["sidebar", undefined, "files", "chat"]);
    const empty = panes(out).find((p) => p.tabs.length === 0)!;
    assert.ok(empty, "the split-away pane survives EMPTY (no placeholder is seated)");
    assert.equal(splits(out).length, 2); // root + the split wrapping the drop target
  });
  it("no-ops: self-move / unknown ids", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    assert.equal(movePane(t, chat.id, chat.id, "after"), t);
    assert.equal(movePane(t, chat.id, "n999", "after"), t);
    assert.equal(movePane(t, "n999", chat.id, "after"), t);
  });
});

describe("tab ops", () => {
  it("focusTab clamps into the tab range", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    assert.equal(findPane(focusTab(t, chat.id, 5), chat.id)!.active, 0);
  });
  it("setPaneComponent fills an empty pane", () => {
    let t: DockNode = defaultTree();
    const empty = makePane(null);
    t = insertPane(t, paneIds(t)[0], "row", "after", empty);
    const out = setPaneComponent(t, empty.id, "chat");
    assert.deepEqual(tabComps(out, empty.id), ["chat"]);
  });
  function tabComps(t: DockNode, id: string): string[] {
    return (findPane(t, id) as DockPane).tabs.map((x) => x.component);
  }
  it("openTab appends and focuses; closeTab removes, the last tab locks", () => {
    // closeTab never consults the last-main-pane census, so the mechanics run.
    const left = asPane(makePane("chat"));
    let t: DockNode = left;
    t = openTab(t, left.id, { component: "scratch" });
    let p = findPane(t, left.id)!;
    assert.equal(p.tabs.length, 2);
    assert.equal(p.active, 1);
    t = focusTab(t, left.id, 0);
    t = closeTab(t, left.id, 0);
    p = findPane(t, left.id)!;
    assert.equal(p.tabs.length, 1);
    assert.equal(p.active, 0); // focus shifted back onto the survivor
    const last = closeTab(t, left.id, 0);
    assert.equal(last, t, "the last tab is locked - the tree is untouched");
    assert.deepEqual(findPane(last, left.id)!.tabs.map((x) => x.component), ["scratch"]);
    assert.equal(findPane(last, left.id)!.active, 0);
    assert.ok(findPane(last, left.id), "the pane stays in the tree");
  });
  it("an UNCLOSEABLE tab refuses closeTab (sidebar tabs have no X)", () => {
    const t = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const files = paneByComponent(t, "files");
    assert.equal(closeTab(t, side.id, 0), t, "sessions tab unclosable");
    assert.equal(closeTab(t, files.id, 0), t, "files tab unclosable");
    assert.equal(closeTab(t, files.id, 4), t, "tools tab unclosable");
  });

  describe("moveTab (card 2 DnD)", () => {
    function tabComps(t: DockNode, id: string): (string | undefined)[] {
      return (findPane(t, id) as DockPane).tabs.map((x) => x.component);
    }
    function twoPaneTree(): { t: DockNode; left: DockPane; right: DockPane } {
      const root = makePane("chat");
      const left = asPane(openTab(root, root.id, { component: "scratch" }));
      const right = makePane("chat", null);
      const t = makeSplit("row", [left, right]);
      return { t, left: findPane(t, left.id)!, right: findPane(t, right.id)! };
    }

    it("reorders within a pane, keeping the moved tab focused", () => {
      let t: DockNode = makePane("chat");
      const id = t.id;
      t = openTab(t, id, { component: "scratch" });
      assert.deepEqual(tabComps(t, id), ["chat", "scratch"]);
      // [chat, settings]; chat is NOT the focused tab (openTab focused settings).
      t = moveTab(t, id, 0, id, 1);
      let p = findPane(t, id)!;
      assert.deepEqual(tabComps(t, id), ["scratch", "chat"]);
      assert.equal(p.active, 0, "the parked active tab tracks its component");
      t = moveTab(t, id, 0, id, 0);
      p = findPane(t, id)!;
      assert.equal(p.active, 0, "focused tab keeps its position");
      t = focusTab(t, id, 1);
      t = moveTab(t, id, 1, id, 0);
      p = findPane(t, id)!;
      assert.deepEqual(tabComps(t, id), ["chat", "scratch"]);
      assert.equal(p.active, 0, "the moved tab stays focused");
    });

    it("same-position drag is a no-op returning the ORIGINAL tree", () => {
      const { t, left } = twoPaneTree();
      assert.equal(moveTab(t, left.id, 0, left.id, 0), t);
    });

    it("moves between panes, focusing the dropped tab in the target", () => {
      const { t, left, right } = twoPaneTree();
      const out = moveTab(t, left.id, 0, right.id, right.tabs.length);
      const l = findPane(out, left.id)!;
      const r = findPane(out, right.id)!;
      assert.equal(l.tabs.length, 1);
      assert.deepEqual(tabComps(out, left.id), ["scratch"]);
      assert.deepEqual(tabComps(out, right.id), ["chat", "chat"]);
      assert.equal(r.active, 1, "dropped tab focuses in the target");
      assert.equal(l.active, 0, "survivor keeps the focus");
    });

    it("inserts at the caret index in the target pane", () => {
      const { t, left, right } = twoPaneTree();
      const out = moveTab(t, left.id, 0, right.id, 0);
      assert.deepEqual(tabComps(out, right.id), ["chat", "chat"]);
      assert.equal(findPane(out, right.id)!.active, 0);
    });

    it("moving a pane's LAST tab closes the pane (card t_aa9dbada)", () => {
      const { t, left, right } = twoPaneTree();
      // drain left pane one tab at a time.
      let cur = moveTab(t, left.id, 0, right.id, findPane(t, right.id)!.tabs.length);
      assert.equal(panes(cur).length, 2, "the source still hosts a tab, it stays");
      cur = moveTab(cur, left.id, 0, right.id, findPane(cur, right.id)!.tabs.length);
      assert.ok(!findPane(cur, left.id), "the last tab out closes the pane");
      assert.equal(panes(cur).length, 1, "the survivor hoists (same result as the remove button)");
      assert.deepEqual(tabComps(cur, right.id), ["chat", "chat", "scratch"]);
      assert.ok(
        findPane(cur, right.id)!.tabs.some((x) => x.component === "scratch"),
        "the drained tab landed in the right pane",
      );
    });

    it("a PINNED shell emptied by a move sits EMPTY (nothing is seated)", () => {
      const shell = makePane("files", 240, "right");
      const chat = makePane("chat");
      const t = makeSplit("row", [shell, chat]);
      const out = moveTab(t, shell.id, 0, chat.id, 0);
      const s = findPane(out, shell.id)!;
      assert.ok(s, "the pinned shell is not pruned");
      assert.deepEqual(
        s.tabs.map((x) => x.component),
        [],
        "the drained shell sits empty - no placeholder sticker (card t_9855a20e)",
      );
      assert.deepEqual(tabComps(out, chat.id), ["files", "chat"], "the tab landed");
    });

    it("emptying the source moves focus to the remaining neighbor", () => {
      const two = twoPaneTree();
      const out = moveTab(two.t, two.left.id, 0, two.right.id, 0);
      const l = findPane(out, two.left.id)!;
      assert.equal(l.tabs.length, 1);
      assert.equal(l.active, 0, "neighbor takes over the focus");
    });

    it("the seat law: sessions stays in its bar, sidebar views visit the main pane, main members never leave it", () => {
      const t = defaultTree();
      const side = paneByComponent(t, "sidebar");
      const files = paneByComponent(t, "files");
      const chat = paneByComponent(t, "chat");
      // the sessions tab never leaves the left shell's own bar
      assert.equal(moveTab(t, side.id, 0, chat.id, 0), t, "sessions does not drag right");
      assert.equal(moveTab(t, side.id, 0, files.id, 0), t, "sessions does not drag into the right shell");
      // canvas C1: a MAIN member never leaves the main pane.
      assert.equal(moveTab(t, chat.id, 0, files.id, 0), t, "chat does not drag into the right shell");
      assert.equal(moveTab(t, chat.id, 0, side.id, 0), t, "chat does not drag into the left shell");
      // a SIDEBAR view visits: right shell -> main pane -> back
      const movedOut = moveTab(t, files.id, 0, chat.id, 0);
      assert.deepEqual(
        tabComps(movedOut, chat.id),
        ["files", "chat"],
        "a right-shell tab becomes a main-pane tab (a visit)",
      );
      const movedBack = moveTab(movedOut, chat.id, 0, files.id, 0);
      assert.deepEqual(
        tabComps(movedBack, files.id),
        [...INSPECTOR_TAB_COMPONENTS],
        "the moved tab rejoins the inspector bar at the caret",
      );
      assert.deepEqual(tabComps(t, chat.id), ["chat"], "refusals leave the tree untouched");
    });

    it("guards: unknown panes, bad index return the original tree", () => {
      const { t, left, right } = twoPaneTree();
      assert.equal(moveTab(t, "nzz", 0, right.id, 0), t);
      assert.equal(moveTab(t, left.id, 0, "nzz", 0), t);
      assert.equal(moveTab(t, left.id, 5, right.id, 0), t);
      assert.equal(moveTab(t, left.id, -1, right.id, 0), t);
    });

    it("round-trips through persistence after a move", () => {
      const { t, left, right } = twoPaneTree();
      const moved = moveTab(t, left.id, 0, right.id, 0);
      const back = deserialize(serialize(moved))!;
      assert.deepEqual(tabComps(back, left.id), ["scratch"]);
      assert.deepEqual(tabComps(back, right.id), ["chat", "chat"]);
    });
  });

  it("MAIN_DOCKABLES: the MAIN-area inventory (card t_4ae27e0d; insights left, t_d7fd3ad3; the viewer joined, t_36ceb99b; the browser joined, t_95c91f9c; settings rejoined on demand, t_00e4a784)", () => {
    assert.deepEqual([...MAIN_DOCKABLES], ["chat", "viewer", "settings", "browser"]);
  });
});

describe("serialize / deserialize", () => {
  it("round-trips a nested tree exactly", () => {
    let t: DockNode = defaultTree();
    t = splitPane(t, paneByComponent(t, "chat").id, "row", "after");
    t = setPaneComponent(t, panes(t).find((p) => p.tabs.length === 0)!.id, "chat");
    const back = deserialize(serialize(t));
    assert.ok(back);
    assert.deepEqual(JSON.parse(serialize(back)), JSON.parse(serialize(t)));
  });
  it("normalizes drifted sizes on load", () => {
    const t = defaultTree();
    const obj = JSON.parse(serialize(t));
    obj.tree.sizes = [2, 4, 6]; // ratio 1:2:3, not summing to 1
    const back = asSplit(deserialize(JSON.stringify(obj))!);
    const sum = back.sizes.reduce((a, s) => a + s, 0);
    assert.ok(Math.abs(sum - 1) < 1e-9);
  });
  it("rejects garbage / wrong version / broken nodes", () => {
    assert.equal(deserialize(null), null);
    assert.equal(deserialize(""), null);
    assert.equal(deserialize("{broken"), null);
    assert.equal(deserialize(JSON.stringify({ v: 0, tree: {} })), null, "wrong version");
    assert.equal(
      deserialize(JSON.stringify({ v: 1, tree: { kind: "pane", id: "X1", tabs: [], active: 0 } })),
      null,
      "bad id shape",
    );
    assert.equal(
      deserialize(
        JSON.stringify({
          v: 1,
          tree: { kind: "split", id: "n1", direction: "diagonal", children: [], sizes: [] },
        }),
      ),
      null,
      "bad direction",
    );
    assert.equal(
      deserialize(
        JSON.stringify({
          v: 1,
          tree: {
            kind: "split",
            id: "n1",
            direction: "row",
            children: [asPane(makePane("chat"))],
            sizes: [1],
          },
        }),
      ),
      null,
      "single-child split (should have been hoisted)",
    );
  });
  it("rejects unknown components and future versions (forces migration)", () => {
    assert.equal(
      deserialize(
        JSON.stringify({
          v: 2,
          tree: { kind: "pane", id: "n1", tabs: [{ component: "future" }], active: 0, fixed: 100 },
        }),
      ),
      null,
    );
    assert.equal(
      deserialize(
        JSON.stringify({
          v: 99,
          tree: { kind: "pane", id: "n1", tabs: [{ component: "future" }], active: 0, fixed: 100 },
        }),
      ),
      null,
      "a future version forces migration instead of crashing",
    );
    // v:3 is a KNOWN version now (px-on-disk): a valid v:3 record loads
    const v3 = deserialize(
      JSON.stringify({
        v: 3,
        tree: { kind: "split", id: "n1", direction: "row", px: [500, 500], children: [
          { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null },
          { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: null },
        ] },
      }),
    );
    assert.ok(v3, "the strict reader reads v:3");
  });
  it("v2 round-trips the edge markers and the free tab arrangement", () => {
    let t: DockNode = defaultTree();
    const files = paneByComponent(t, "files");
    const chat = paneByComponent(t, "chat");
    t = moveTab(t, files.id, 0, chat.id, 0);
    const insp = asSplit(t).children[2];
    (insp as DockPane).tabs = [];
    const back = deserialize(serialize(t))!;
    assert.ok(back);
    assert.deepEqual(tabCompsIn(back, chat.id), ["files", "chat"]);
    assert.equal(paneCount(back), 3, "the emptied shell survives as a pane");
    assert.equal(isPinnedPane(findPane(back, asPane(insp).id)!), true, "the shell keeps its marker");
    assert.deepEqual([...allNodes(back)].filter((n) => n.kind === "pane")
      .flatMap((n) => (n as DockPane).edge ? [n.edge] : []), ["left", "right"]);
  });
  it("marker-bearing v2 trees deserialize; bogus markers fall back to main", () => {
    const t = defaultTree();
    const back = deserializeLayout(serialize(t))!;
    assert.equal(back.version, 4, "the record shape is v:4 (px on disk, sidebar shape law)");
    assert.equal(findPane(back.tree, asSplit(t).children[0].id)!.edge, "left");
    assert.equal(findPane(back.tree, asSplit(t).children[2].id)!.edge, "right");
    const bogus = JSON.parse(serialize(t));
    bogus.tree.children[0].edge = "up";
    const fixed = deserialize(JSON.stringify(bogus))!;
    assert.equal(findPane(fixed, asSplit(t).children[0].id)!.edge, undefined);
  });
  it("ACCEPTS the legacy inspector tab (pre-reset trees deserialize)", () => {
    const back = deserialize(
      JSON.stringify({
        v: 1,
        tree: {
          kind: "split",
          id: "n1",
          direction: "row",
          children: [
            { kind: "pane", id: "n2", tabs: [{ component: "sidebar" }], active: 0, fixed: 240 },
            { kind: "pane", id: "n3", tabs: [{ component: "chat", key: "c1" }], active: 0, fixed: null },
            { kind: "pane", id: "n4", tabs: [{ component: "inspector" }], active: 0, fixed: 300 },
          ],
          sizes: [1 / 3, 1 / 3, 1 / 3],
        },
      }),
    );
    assert.ok(back, "legacy trees load, the modernizer owns the rest");
    const insp = findPane(back!, "n4")!;
    assert.deepEqual(insp.tabs.map((x) => x.component), ["inspector"]);
  });
});

describe("migrateLegacy (card t_4ae27e0d: FIXED shape)", () => {
  // the default tree gains the bottom panel - the root is a COLUMN now.
  function mainRow(t: DockNode): DockSplit {
    const root = asSplit(t);
    assert.equal(root.direction, "column", "the default root is a column (bottom-panel era)");
    assert.equal(root.children.length, 2, "main row + bottom shell");
    return asSplit(root.children[0]);
  }
  it("always four panes: sidebar(the primary class), chat, the secondary class, bottom(200, logs)", () => {
    const t = migrateLegacy(() => null);
    const row = mainRow(t);
    assert.equal(row.children.length, 3);
    // canvas C1: the fresh left shell seats its CLASS (sessions + agents + crons).
    assert.deepEqual(
      asPane(row.children[0]).tabs.map((x) => x.component),
      [...PRIMARY_TAB_COMPONENTS],
    );
    assert.equal(asPane(row.children[0]).fixed, EDGE_DEFAULT_PX.sidebar);
    assert.equal(asPane(row.children[1]).tabs[0].component, "chat");
    assert.equal(asPane(row.children[1]).fixed, null);
    assert.equal(asPane(row.children[2]).fixed, EDGE_DEFAULT_PX.inspector);
    assert.deepEqual(
      asPane(row.children[2]).tabs.map((x) => x.component),
      [...INSPECTOR_TAB_COMPONENTS],
    );
    const bottom = asPane(asSplit(t).children[1]);
    assert.equal(bottom.edge, "bottom");
    assert.equal(bottom.fixed, 200);
    assert.deepEqual(bottom.tabs.map((x) => x.component), ["terminal", "observability"]);
  });
  it("persisted widths render verbatim, open flags are dead history", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "300"
      : k === LEGACY_WIDTH_KEYS.inspector ? "400"
      : "false", // even a persisted 'closed' flag changes nothing
    );
    const row = mainRow(t);
    assert.equal(asPane(row.children[0]).fixed, 300);
    assert.equal(asPane(row.children[2]).fixed, 400);
  });
  it("sub-floor legacy width renders verbatim (open pane keeps its width)", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "150" : null,
    );
    assert.equal(asPane(mainRow(t).children[0]).fixed, 150);
  });
  it("the edge-pane default carries the 0.17.3 strip bump (card t_d4443ab5)", () => {
    // the sidebars' tab strip spans the PANE.
    assert.equal(EDGE_DEFAULT_PX.sidebar, 240 + 24, "sidebar: 240 -> 264");
    assert.equal(EDGE_DEFAULT_PX.inspector, 240 + 24, "inspector: 240 -> 264");
  });

  it("bad flag strings fall back to the default widths", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "abc" : k === LEGACY_WIDTH_KEYS.inspector ? "maybe" : null,
    );
    const row = mainRow(t);
    assert.equal(asPane(row.children[0]).fixed, EDGE_DEFAULT_PX.sidebar);
    assert.equal(asPane(row.children[2]).fixed, EDGE_DEFAULT_PX.inspector);
  });
});

// the pane-model reset specifics ---------------------

describe("pane-model reset", () => {
  it("expandInspectorTabs expands the legacy id into the real tabs", () => {
    const legacy: DockNode = {
      kind: "pane",
      id: "n1",
      tabs: [{ component: "inspector" }],
      active: 0,
      fixed: 300,
    };
    const out = expandInspectorTabs(legacy);
    assert.deepEqual(
      (out as DockPane).tabs.map((x) => x.component),
      [...INSPECTOR_TAB_COMPONENTS],
    );
    assert.equal((out as DockPane).active, 0);
  });
  it("expandInspectorTabs is a no-op (same reference) on a modern tree", () => {
    const t = defaultTree();
    assert.equal(expandInspectorTabs(t), t);
  });
  it("ensurePinnedEdges re-inserts a missing sessions pane at the LEFT edge", () => {
    const t: DockNode = makePane("chat");
    const out = asSplit(ensurePinnedEdges(t));
    assert.equal(out.direction, "row");
    assert.equal(
      asPane(out.children[0]).tabs.map((x) => x.component)[0],
      "sidebar",
    );
    assert.equal(asPane(out.children[0]).fixed, EDGE_DEFAULT_PX.sidebar);
  });
  it("ensurePinnedEdges re-inserts the inspector pane at the RIGHT edge", () => {
    const t: DockNode = makePane("chat");
    const out = asSplit(ensurePinnedEdges(t));
    const right = asPane(out.children[2]); // [sidebar, chat, inspector]
    assert.deepEqual(
      right.tabs.map((x) => x.component),
      [...INSPECTOR_TAB_COMPONENTS],
    );
    assert.equal(right.fixed, EDGE_DEFAULT_PX.inspector);
  });

  it("a closed sidebar reopens ABOVE the bottom band, not beside it (card t_556ee727)", () => {
    // the aligned-bottom tree: a root COLUMN whose last child is the bottom shell. With
    // the sidebar closed, the reopen insert must target the head row - wrapping the
    // whole column would seat the sidebar next to a squashed half-width band and drag
    // the band into the row's shrink math.
    const t: DockNode = makeSplit("column", [
      makePane("chat", null),
      bottomShellPane(),
    ]);
    const out = asSplit(ensurePinnedEdges(t));
    // the root stays the column and the band stays its LAST child, full row-width.
    assert.equal(out.direction, "column");
    assert.equal(out.children.length, 2);
    assert.equal(asPane(out.children[1]).edge, "bottom");
    const head = asSplit(out.children[0]);
    assert.equal(head.direction, "row");
    assert.equal(asPane(head.children[0]).tabs[0]!.component, "sidebar");
    assert.equal(asPane(head.children[1]).tabs[0]!.component, "chat");
  });
  it("rehomeInsightsTab lifts the main-pane strand; the loader seats it in the right shell (t_d7fd3ad3)", () => {
    let t: DockNode = defaultTree();
    const chatId = findComponentPane(t, "chat")!.id;
    t = openTab(t, chatId, { component: "insights" });
    const strand = findComponentTab(t, "insights")!;
    assert.notEqual(strand.paneId, findEdgePane(t, "inspector")!.id);
    assert.equal(
      asPane(findPane(t, strand.paneId)!).edge,
      undefined,
      "pre: the strand lives in a main pane",
    );
    const lifted = rehomeInsightsTab(t);
    const liftedPane = findPane(lifted, strand.paneId)!;
    assert.ok(liftedPane, "the main pane survives");
    assert.ok(
      !liftedPane.tabs.some((x) => x.component === "insights"),
      "the strand's tab is gone",
    );
    assert.ok(
      findEdgePane(lifted, "inspector")!.tabs.some((x) => x.component === "insights"),
      "the right shell's instance survives the lift",
    );
    // the loader (ensurePinnedEdges -> restoreSeatSet) seats it in the right shell.
    const loaded = ensurePinnedEdges(lifted);
    const right = findEdgePane(loaded, "inspector")!;
    assert.ok(
      right.tabs.some((x) => x.component === "insights"),
      "restore keeps insights in the right shell",
    );
    // a clean tree is a no-op (same reference - the pure contract)
    assert.equal(rehomeInsightsTab(loaded), loaded);
  });
  it("a modern tree passes ensurePinnedEdges untouched", () => {
    const t = defaultTree();
    assert.equal(ensurePinnedEdges(t), t);
  });
  it("boot repair never mints an id the loaded tree already owns (n1 collision, t_f6d5a771)", () => {
    // reload after a divider hide. The boot repair re-inserts the sidebar; with the
    // per-page seed at 0 that mint would be "n1" TOO unless the seed synced first -
    // duplicate ids, and every id-keyed op (the divider intents' prunePane,
    // canRemovePane) acts on BOTH panes at once.
    const stored = asSplit({
      kind: "split",
      id: "n2",
      direction: "row",
      children: [
        { kind: "pane", id: "n3", tabs: [{ component: "chat", key: "c1" }], active: 0, fixed: null },
        { kind: "pane", id: "n1", tabs: [{ component: "files" }], active: 0, fixed: 240 },
      ],
      sizes: [0.8, 0.2],
    });
    syncIdSeed(stored);
    const out = asSplit(ensurePinnedEdges(stored));
    const ids = [...allNodes(out)].map((n) => n.id);
    assert.equal(new Set(ids).size, ids.length, "no duplicate ids after repair");
    assert.equal(asPane(out.children[0]).tabs[0].component, "sidebar");
    assert.notEqual(asPane(out.children[0]).id, "n1");
  });
  it("firstMainPaneId skips the pinned sidebars", () => {
    const t = defaultTree();
    assert.equal(firstMainPaneId(t), paneByComponent(t, "chat").id);
  });
  it("insertMainPane appends a flexible pane (seated) and returns its id", () => {
    const t = defaultTree();
    const before = paneIds(t);
    const res = insertMainPane(t);
    const root = asSplit(res.tree);
    assert.deepEqual(paneIds(res.tree), [...before, res.paneId]);
    const fresh = findPane(res.tree, res.paneId)!;
    assert.equal(fresh.fixed, null);
    // the minted pane is EMPTY.
    assert.deepEqual(fresh.tabs.map((x) => x.component), []);
    assert.equal(paneCount(res.tree), 4);
  });
  it("paneCount / isPinnedPane read the whole tree", () => {
    const t = defaultTree();
    assert.equal(paneCount(t), 3);
    assert.equal(isPinnedPane(paneByComponent(t, "sidebar")), true);
    assert.equal(isPinnedPane(paneByComponent(t, "files")), true);
    assert.equal(isPinnedPane(paneByComponent(t, "chat")), false);
  });
});

// gate: the edge toggle --------------------------------

describe("edge toggle (card t_47b776ad)", () => {
  it("findEdgePane fingerprints the two dedicated pinned panes", () => {
    const t = defaultTree();
    const sidebar = findEdgePane(t, "sidebar");
    const inspector = findEdgePane(t, "inspector");
    assert.equal(sidebar?.tabs[0]?.component, "sidebar");
    assert.deepEqual(
      inspector?.tabs.map((x) => x.component),
      INSPECTOR_TAB_COMPONENTS,
    );
    assert.equal(findEdgePane(hideEdgePane(t, "sidebar"), "sidebar"), null);
  });

  it("hideEdgePane removes the edge pane as a whole; a main pane untouched", () => {
    const t = defaultTree();
    const sidebarId = findEdgePane(t, "sidebar")!.id;
    const hidden = hideEdgePane(t, "sidebar");
    assert.deepEqual(
      paneIds(hidden),
      paneIds(t).filter((id) => id !== sidebarId),
    );
    assert.equal(findEdgePane(hidden, "sidebar"), null);
    assert.notEqual(findEdgePane(hidden, "inspector"), null);
  });

  it("revealEdgePane reopens the pane at its edge with the clamp width", () => {
    const hidden = hideEdgePane(defaultTree(), "sidebar");
    const reopened = asSplit(revealEdgePane(hidden, "sidebar", "before")).children[0];
    assert.equal(asPane(reopened).tabs[0]?.component, "sidebar");
    assert.equal(asPane(reopened).fixed, Math.max(PANE_OPEN_MIN, 240));
    assert.equal(isPinnedPane(asPane(reopened)), true);
    const insp = asPane(
      asSplit(revealEdgePane(hideEdgePane(defaultTree(), "inspector"), "inspector", "after")).children[2],
    );
    assert.deepEqual(
      insp.tabs.map((x): string => x.component),
      [...INSPECTOR_TAB_COMPONENTS],
    );
  });

  it("hide is reversible round-trip; the toggle converges after hide+reveal", () => {
    const t = defaultTree();
    const round = revealEdgePane(hideEdgePane(t, "sidebar"), "sidebar", "before");
    assert.notEqual(findEdgePane(round, "sidebar"), null);
    assert.equal(paneCount(round), paneCount(t));
    // never-empty: a lone edge pane refuses to go
    const lone = makePane("sidebar", 240);
    assert.equal(hideEdgePane(lone, "sidebar"), lone);
    const main = makePane("chat");
    assert.equal(hideEdgePane(main, "inspector"), main);
  });
});

describe("edge width memory (card t_f6d5a771)", () => {
  const stubStore = () => {
    const mem: Record<string, string> = {};
    return {
      store: {
        getItem: (k: string) => (k in mem ? mem[k] : null),
        setItem: (k: string, v: string) => {
          mem[k] = v;
        },
        removeItem: (k: string) => {
          delete mem[k];
        },
        clear: () => {
          for (const k of Object.keys(mem)) delete mem[k];
        },
        key: () => null,
        length: 0,
      } as Storage,
      mem,
    };
  };

  it("hide remembers the edge pane's width; reveal restores it", () => {
    const { store } = stubStore();
    const t = defaultTree();
    const sidebar = findEdgePane(t, "sidebar")!;
    sidebar.fixed = 704;
    rememberEdgePx(store, "sidebar", 704);
    assert.equal(recalledEdgePx(store, "sidebar"), 704);
    assert.equal(recalledEdgePx(store, "inspector"), null);
    const hidden = hideEdgePane(t, "sidebar");
    assert.equal(findEdgePane(hidden, "sidebar"), null);
    const back = asSplit(
      revealEdgePane(hidden, "sidebar", "before", recalledEdgePx(store, "sidebar")),
    );
    assert.equal(asPane(back.children[0]).fixed, 704);
    assert.equal(asPane(back.children[0]).tabs[0]?.component, "sidebar");
  });

  it("a stale sub-floor remembered width reopens AT the 320 floor", () => {
    const { store } = stubStore();
    rememberEdgePx(store, "inspector", 100);
    assert.equal(recalledEdgePx(store, "inspector"), PANE_OPEN_MIN);
  });

  it("corrupt storage reads back null (defaults win)", () => {
    const { store, mem } = stubStore();
    rememberEdgePx(store, "sidebar", 640);
    mem["omega.dock-edge-px"] = "{oops";
    assert.equal(recalledEdgePx(store, "sidebar"), null);
  });

  it("edgeComponentOf fingerprints the edge panes; main panes are null", () => {
    const t = defaultTree();
    const sidebar = findEdgePane(t, "sidebar")!;
    const inspector = findEdgePane(t, "inspector")!;
    assert.equal(edgeComponentOf(sidebar), "sidebar");
    assert.equal(edgeComponentOf(inspector), "inspector");
    const mainPaneId = firstMainPaneId(t)!;
    const chat = findPane(t, mainPaneId)!;
    assert.equal(edgeComponentOf(chat), null);
  });
});

describe("window-narrow give-way (card t_33f8756c)", () => {
  // the default shell needs 240 (sidebar) + 320 (chat floor) + 300 (inspector) + 8.
  it("minDockPx: rows sum children + dividers, columns take the widest", () => {
    assert.equal(minDockPx(defaultTree()), 868);
    assert.equal(minDockPx(makePane("chat")), CHAT_FLOOR_PX);
    const col = makeSplit("column", [
      makePane("sidebar", 240),
      makePane("chat", null),
    ]);
    assert.equal(minDockPx(col), 320);
  });

  it("narrowGiveWay: the secondary (inspector) collapses first", () => {
    const t = defaultTree();
    const res = narrowGiveWay(t, 800);
    assert.deepEqual(
      res.hidden.map((h) => ({ component: h.component, px: h.px })),
      [{ component: "inspector", px: 300 }] satisfies Pick<NarrowHiddenEntry, "component" | "px">[],
    );
    assert.equal(res.hidden[0]!.pane.edge, "right");
    assert.equal(findEdgePane(res.tree, "inspector"), null);
    assert.notEqual(findEdgePane(res.tree, "sidebar"), null);
    assert.equal(minDockPx(res.tree), 564);
  });

  it("narrowGiveWay: starved further, the primary gives way next", () => {
    const res = narrowGiveWay(defaultTree(), 500);
    assert.deepEqual(
      res.hidden.map((h) => h.component),
      ["inspector", "sidebar"],
    );
    assert.equal(findEdgePane(res.tree, "inspector"), null);
    assert.equal(findEdgePane(res.tree, "sidebar"), null);
    assert.equal(minDockPx(res.tree), CHAT_FLOOR_PX);
  });

  it("narrowGiveWay: a window that fits hides nothing; already-hidden stay gone", () => {
    const t = defaultTree();
    assert.deepEqual(narrowGiveWay(t, 1000).hidden, []);
    assert.deepEqual(narrowGiveWay(t, 868).hidden, []); // exactly seated
    // once both are gone nothing further can collapse (never-empty dock)
    const squeezed = narrowGiveWay(t, 500).tree;
    const again = narrowGiveWay(squeezed, 300);
    assert.deepEqual(again.hidden, []);
    assert.equal(again.tree, squeezed); // nothing hidden: identity preserved
  });

  it("narrowGiveWay: justRevealed=inspector survives the squeeze; the sidebar gives way", () => {
    const res = narrowGiveWay(defaultTree(), 800, "inspector");
    assert.deepEqual(
      res.hidden.map((h) => h.component),
      ["sidebar"],
    );
    // the inspector survives WITH its full record intact.
    assert.equal(res.hidden[0]!.pane.edge, "left");
    assert.notEqual(findEdgePane(res.tree, "inspector"), null);
    assert.equal(findEdgePane(res.tree, "sidebar"), null);
  });

  it("narrowGiveWay: justRevealed=sidebar mirror - the inspector gives way", () => {
    const res = narrowGiveWay(defaultTree(), 800, "sidebar");
    assert.deepEqual(
      res.hidden.map((h) => ({ component: h.component, px: h.px })),
      [{ component: "inspector", px: 300 }] satisfies Pick<NarrowHiddenEntry, "component" | "px">[],
    );
    assert.notEqual(findEdgePane(res.tree, "sidebar"), null);
    assert.equal(findEdgePane(res.tree, "inspector"), null);
  });

  it("narrowGiveWay: justRevealed is only absolute-last-resort - starved further, it hides too", () => {
    // 500 seats the chat floor alone: BOTH panes go, protected one last
    const res = narrowGiveWay(defaultTree(), 500, "inspector");
    assert.deepEqual(
      res.hidden.map((h) => h.component),
      ["sidebar", "inspector"],
    );
    assert.equal(minDockPx(res.tree), CHAT_FLOOR_PX);
  });

  it("narrowGiveWay: a revealing squeeze that fits hides nothing (even with justRevealed)", () => {
    const t = defaultTree();
    assert.deepEqual(narrowGiveWay(t, 1000, "inspector").hidden, []);
    assert.deepEqual(narrowGiveWay(t, 868, "sidebar").hidden, []); // exactly seated
  });

  it("narrowGiveWay: null justRevealed keeps the default inspector-first order", () => {
    // explicit despite the earlier tests: only a REQUEST exempts a pane
    const res = narrowGiveWay(defaultTree(), 800, null);
    assert.deepEqual(
      res.hidden.map((h) => h.component),
      ["inspector"],
    );
    assert.notEqual(findEdgePane(res.tree, "sidebar"), null);
    assert.equal(findEdgePane(res.tree, "inspector"), null);
  });

  it("dock.narrowSqueeze prunes through the store and remembers the widths", () => {
    const mem: Record<string, string> = {};
    const store = {
      getItem: (k: string) => (k in mem ? mem[k] : null),
      setItem: (k: string, v: string) => {
        mem[k] = v;
      },
      removeItem: (k: string) => {
        delete mem[k];
      },
      clear: () => {
        for (const k of Object.keys(mem)) delete mem[k];
      },
      key: () => null,
      length: 0,
    } as Storage;
    createDock(store); // boot pass runs the load pipeline; ignore the value
    const dock = createDock(store);
    dock.narrowSqueeze(800);
    assert.equal(findEdgePane(dock.tree.value, "inspector"), null);
    assert.notEqual(findEdgePane(dock.tree.value, "sidebar"), null);
    // the inspector's width went into the edge-px memory; the reopen clamp
    // lifts the sub-floor 300 to the 320 open minimum (openClamp contract)
    assert.equal(recalledEdgePx(store, "inspector"), 320);
    dock.toggleEdge("inspector", "after");
    assert.equal(asPane(findEdgePane(dock.tree.value, "inspector")!).fixed, 320);
    // a roomy resize is a no-op - the tree never rewrites
    const before = serialize(dock.tree.value);
    dock.narrowSqueeze(4000);
    assert.equal(serialize(dock.tree.value), before);
  });

  // the reveal funnel owns the NEXT squeeze.
  it("dock squeeze: a just-revealed pane wins the space; the pending is one-shot", () => {
    const mem: Record<string, string> = {};
    const store = {
      getItem: (k: string) => (k in mem ? mem[k] : null),
      setItem: (k: string, v: string) => {
        mem[k] = v;
      },
      removeItem: (k: string) => {
        delete mem[k];
      },
      clear: () => {
        for (const k of Object.keys(mem)) delete mem[k];
      },
      key: () => null,
      length: 0,
    } as Storage;
    const dock = createDock(store);
    dock.toggleEdge("inspector", "after"); // close
    dock.toggleEdge("inspector", "after"); // reopen -> the reveal is pending
    dock.narrowSqueeze(800);
    assert.notEqual(findEdgePane(dock.tree.value, "inspector"), null);
    assert.equal(findEdgePane(dock.tree.value, "sidebar"), null);
    // the law holds for EVERY reveal: reopening the closed sidebar swaps
    dock.toggleEdge("sidebar", "before");
    dock.narrowSqueeze(800);
    assert.notEqual(findEdgePane(dock.tree.value, "sidebar"), null);
    assert.equal(findEdgePane(dock.tree.value, "inspector"), null);
    // the pending is consumed: this squeeze has NO request in flight.
    dock.narrowSqueeze(400);
    assert.equal(findEdgePane(dock.tree.value, "sidebar"), null);
    dock.narrowSqueeze(4000);
    dock.toggleEdge("inspector", "after");
    assert.notEqual(findEdgePane(dock.tree.value, "inspector"), null);
    const inspectorPane = findEdgePane(dock.tree.value, "inspector")!;
    assert.ok(inspectorPane.tabs.length > 0, "the reopened shell carries its stowed tabs");
  });
});

describe("dragSplitDivider", () => {
  const CONTENT = 1000;

  it("left fixed: grows over the flexible chat", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 100, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, 340);
    assert.equal(asPane(out.split.children[1]).fixed, null, "chat stays flexible");
    assert.equal(out.collapsedPaneId, null);
  });
  it("left fixed: caps against the flexible chat floor (320)", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 900, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, 380);
  });
  it("left fixed: honors the 128px drag floor without collapsing", () => {
    const t = defaultTree();
    // req 240 - 112 = 128: ON the floor, still alive
    const out = dragSplitDivider(t, 1, [240, 460, 300], -112, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, MIN_PANE_PX);
    assert.equal(out.collapsedPaneId, null);
  });
  it("left fixed: snap attractor sticks the 320+-15 zone to 320", () => {
    const t = defaultTree();
    // zone is 305.335: 304 stays free, 318/322 snap, 336 stays free
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 64, CONTENT).split.children[0]).fixed, 304);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 78, CONTENT).split.children[0]).fixed, 320);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 82, CONTENT).split.children[0]).fixed, 320);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 96, CONTENT).split.children[0]).fixed, 336);
  });
  it("snap never crosses the cap (clamped after snapping)", () => {
    const t = defaultTree();
    // room = 1000 - 300; max 380: dragging toward 640 snaps to 640 but clamps to 380
    const out = dragSplitDivider(t, 1, [240, 460, 300], 700, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, 380);
  });
  it("right fixed (inspector): drag inverts - dragging left widens it", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 2, [240, 460, 300], -60, CONTENT);
    assert.equal(asPane(out.split.children[2]).fixed, 360);
  });
  it("right fixed: caps against the flexible chat floor and floors at 128", () => {
    const t = defaultTree();
    // room = 1000 - 240 = 760; inspector max = 760 - 320 = 440 (drag LEFT)
    assert.equal(asPane(dragSplitDivider(t, 2, [240, 460, 300], -900, CONTENT).split.children[2]).fixed, 440);
    // drag right narrows it to exactly the floor: still alive
    const narrowed = dragSplitDivider(t, 2, [240, 460, 300], 172, CONTENT);
    assert.equal(asPane(narrowed.split.children[2]).fixed, MIN_PANE_PX);
    assert.equal(narrowed.collapsedPaneId, null);
  });
  it("right fixed: snap reads the inspector's px", () => {
    const t = defaultTree();
    assert.equal(asPane(dragSplitDivider(t, 2, [240, 460, 300], -25, CONTENT).split.children[2]).fixed, 320);
  });

  // the divider close (drag-past-min) - and. If the far pane must donate space, the
  // user moves its divider - two gestures, zero surprises.
  const sidebarIdOf = (t: DockSplit) => asPane(t.children[0]).id;
  const inspectorIdOf = (t: DockSplit) => asPane(t.children[2]).id;

  it("drag-past-min CLOSES the sidebar: the left divider reports a hide intent", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], -113, CONTENT);
    assert.equal(out.collapsedPaneId, sidebarIdOf(t));
  });
  it("drag-past-min CLOSES the inspector: the right divider mirrors it", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 2, [240, 460, 300], 173, CONTENT);
    assert.equal(out.collapsedPaneId, inspectorIdOf(t));
  });
  it("WALL: over-cap drag stops AT the cap - the inspector beyond never moves", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 240, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, 380, "stops dead at the wall");
    assert.equal(asPane(out.split.children[2]).fixed, 300, "the inspector is bit-identical");
    assert.equal(out.collapsedPaneId, null, "no close intent: the wall is a stop, not a hide");
  });
  it("WALL: a cap-busting drag still closes nothing and moves nothing else", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 900, CONTENT);
    assert.equal(out.collapsedPaneId, null, "the pushed pane close intent is gone");
    assert.equal(asPane(out.split.children[0]).fixed, 380, "clamped at room - 320");
    assert.equal(asPane(out.split.children[2]).fixed, 300, "inspector untouched");
  });
  it("WALL (inspector divider, row): the sidebar beyond never moves", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 2, [240, 460, 300], -900, CONTENT);
    assert.equal(asPane(out.split.children[2]).fixed, 440);
    assert.equal(asPane(out.split.children[0]).fixed, 240, "the sidebar is bit-identical");
    assert.equal(out.collapsedPaneId, null);
  });
  it("WALL with no fixed pane beyond: plain cap (the last fixed pane owns no wall)", () => {
    const t = hideEdgePane(defaultTree(), "inspector");
    const out = dragSplitDivider(t, 1, [240, 460], 900, 700);
    assert.equal(asPane(out.split.children[0]).fixed, 380);
    assert.equal(out.collapsedPaneId, null);
  });
  it("both flexible: fractions shift in tandem against the content box", () => {
    const t = defaultTree();
    const inner = asSplit(splitPane(t, paneByComponent(t, "chat").id, "row", "after").children[1]);
    // inner: [chat(flex), empty(flex)], fractions 0.5/0.5.
    const out = dragSplitDivider(inner, 1, [498, 498], 100, 996);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 598) < 1e-6, `left got ${out.split.sizes[0] * 996}`);
    assert.ok(Math.abs(out.split.sizes[1] * 996 - 398) < 1e-6);
    const sum = out.split.sizes.reduce((a, s) => a + s, 0);
    assert.ok(Math.abs(sum - 1) < 1e-9);
  });
  it("both flexible: 320 floor between them, cap clamps the fraction", () => {
    const t = defaultTree();
    // the split's fresh pane seats the new-tab placeholder, which keeps the EMPTY.
    const inner = asSplit(asSplit(splitPane(t, paneByComponent(t, "chat").id, "row", "after")).children[1]);
    const out = dragSplitDivider(inner, 1, [498, 498], 900, 996);
    // room 996, max left 996 - 320 = 676 -> fraction 676/996; right keeps >=320
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 676) < 1e-9);
    assert.ok(out.split.sizes[1] * 996 >= 320 - 0.001);
  });
  it("no-op on out-of-range divider index", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 0, [240, 460, 300], 10, CONTENT);
    assert.deepEqual(out.split, t);
    assert.equal(out.collapsedPaneId, null);
  });
});
describe("graftSplit", () => {
  it("swaps a nested split by id without touching ids", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const t2 = asSplit(splitPane(t, chat.id, "row", "after"));
    const inner = asSplit(t2.children[1]);
    const replaced = dragSplitDivider(inner, 1, [498, 498], 50, 996);
    const out = graftSplit(t2, replaced.split);
    assert.equal(asSplit(out).children[1].id, inner.id);
    assert.equal(asSplit(asSplit(out).children[1]).sizes[0], replaced.split.sizes[0]);
    assert.equal(asPane(asSplit(out).children[0]).fixed, 240);
  });
  it("replaces the root when the replacement carries the root id; identity when absent", () => {
    const t = defaultTree();
    // dragging the ROOT's divider produces a replacement split with t's id
    const dragged = dragSplitDivider(t, 1, [240, 460, 300], 50, 1000);
    // root id == replacement id -> the replacement IS the new root
    assert.equal(graftSplit(t, dragged.split), dragged.split);
    const fresh = defaultTree();
    const missing = asSplit(splitPane(fresh, paneByComponent(fresh, "chat").id, "row", "after").children[1]);
    const other = defaultTree();
    assert.equal(graftSplit(other, missing), other); // no such split inside
  });
});

// locked sessions, free content, pinned shells --------------

describe("tab DnD under the seat law (canvas C1)", () => {
  it("dropAllowedFor: the class law answers every target zone", () => {
    const locked = { paneId: "n1", index: 0, component: "sidebar" as const };
    const free = { paneId: "n1", index: 0, component: "files" as const };
    const chat = { paneId: "n1", index: 0, component: "chat" as const };
    const logs = { paneId: "n1", index: 0, component: "observability" as const };
    const none: typeof free | null = null;
    assert.equal(dropAllowedFor("left", locked), true, "sessions stays in its own bar");
    assert.equal(dropAllowedFor(null, locked), false, "sessions refuses main panes");
    assert.equal(dropAllowedFor("right", locked), false, "sessions refuses the right shell");
    assert.equal(dropAllowedFor(null, free), true, "files visits main");
    assert.equal(dropAllowedFor("left", free), true, "files visits the left shell");
    assert.equal(dropAllowedFor("right", free), true, "files visits the right shell");
    assert.equal(dropAllowedFor("bottom", free), true, "files visits the bottom pane");
    // a MAIN member never leaves the main column (the law's whole point)
    assert.equal(dropAllowedFor(null, chat), true, "chat's own seat");
    assert.equal(dropAllowedFor("left", chat), false, "chat never lands in a sidebar");
    assert.equal(dropAllowedFor("right", chat), false, "chat never lands in the inspector");
    assert.equal(dropAllowedFor("bottom", chat), false, "chat never lands in the bottom pane");
    // a BOTTOM member visits the sidebars, never main
    assert.equal(dropAllowedFor("left", logs), true, "logs visit the left shell");
    assert.equal(dropAllowedFor("right", logs), true, "logs visit the right shell");
    assert.equal(dropAllowedFor(null, logs), false, "logs never enter the main pane");
    assert.equal(dropAllowedFor(null, none), false, "no gesture - no drop");
  });

  it("drag sim: reorder WITHIN the sidebar bar keeps everything in place", () => {
    let t: DockNode = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const seat = side.tabs.map((x) => x.component);
    const insp = paneByComponent(t, "files");
    t = moveTab(t, insp.id, 0, side.id, 1);
    t = moveTab(t, side.id, 1, side.id, 0);
    const bar = paneByComponent(t, "sidebar");
    assert.deepEqual(bar.tabs.slice(0, 2).map((x) => x.component), ["files", "sidebar"]);
    assert.equal(bar.tabs.length, seat.length + 1, "no tab minted or lost");
    assert.equal(bar.id, side.id, "no new pane minted");
  });
  it("drag sim: sessions reorder within its bar is the ONLY locked move", () => {
    let t: DockNode = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const seat = side.tabs.map((x) => x.component);
    const from = seat.indexOf("sidebar");
    assert.equal(from, 0, "sessions heads the strip");
    // the bar's own bar: dragging sessions to the end (caret = last index)
    // is a LEGAL reorder inside the shell it may never leave
    const reordered = moveTab(t, side.id, from, side.id, seat.length - 1);
    const bar = paneByComponent(reordered, "sidebar");
    assert.deepEqual(
      bar.tabs.map((x) => x.component),
      [...seat.slice(1), "sidebar"],
      "sessions lands where the caret asked, still inside its own bar",
    );
  });

  it("empty shells are refillable: openSingleton / setPaneComponent target them", () => {
    let t: DockNode = defaultTree();
    const insp = paneByComponent(t, "files");
    const chat = paneByComponent(t, "chat");
    for (let i = 0; i < INSPECTOR_TAB_COMPONENTS.length; i++) {
      t = moveTab(t, insp.id, 0, chat.id, findPane(t, chat.id)!.tabs.length);
    }
    const shell = findPane(t, insp.id)!;
    assert.deepEqual(
      shell.tabs.map((x) => x.component),
      [],
      "the drained shell sits empty (no seat)",
    );
    assert.equal(isPinnedPane(shell), true, "still a shell");
    // the open path fills the shell again. the chat pane is the LAST MAIN
    // pane, so a split must mint a second main pane before the remove may land
    t = splitPane(t, chat.id, "row", "after");
    t = removePane(t, chat.id);
    const res = openSingleton(t, insp.id, "files");
    assert.equal(res.action, "created");
    assert.deepEqual(
      findPane(res.tree, insp.id)!.tabs.map((x) => x.component),
      ["files"],
    );
    // and the direct setComponent path works too
    const filled = setPaneComponent(t, insp.id, "kanban");
    assert.deepEqual(
      findPane(filled, insp.id)!.tabs.map((x) => x.component),
      ["kanban"],
    );
  });

  it("the loader forces sessions into the left shell (forceSessionsLeft)", () => {
    // a (corrupt) tree whose sessions tab landed in a MAIN pane: relocated
    const t: DockNode = {
      kind: "split",
      id: "n1",
      direction: "row",
      children: [
        { kind: "pane", id: "n2", tabs: [], active: 0, fixed: 240, edge: "left" },
        { kind: "pane", id: "n3", tabs: [{ component: "sidebar" }, { component: "chat" }], active: 1, fixed: null },
        {
          kind: "pane",
          id: "n4",
          tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
          active: 0,
          fixed: 300,
          edge: "right",
        },
      ],
      sizes: [1 / 3, 1 / 3, 1 / 3],
    };
    const out = forceSessionsLeft(t)!;
    const left = findPane(out, "n2")!;
    assert.deepEqual(
      left.tabs.map((x) => x.component),
      ["sidebar"],
    );
    const main = findPane(out, "n3")!;
    assert.deepEqual(
      main.tabs.map((x) => x.component),
      ["chat"],
      "the stray copy left the main pane",
    );
    assert.equal(main.active, 0, "the focused chat tab takes the vacated index");
  });

  it("a left shell WITHOUT sessions gets sessions injected at index 0", () => {
    const t: DockNode = {
      kind: "split",
      id: "n1",
      direction: "row",
      children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: 240, edge: "left" },
        {
          kind: "pane",
          id: "n4",
          tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
          active: 0,
          fixed: 300,
          edge: "right",
        },
      ],
      sizes: [1 / 2, 1 / 2],
    };
    const out = forceSessionsLeft(t)!;
    const left = findPane(out, "n2")!;
    assert.deepEqual(
      left.tabs.map((x) => x.component),
      ["sidebar", "chat"],
      "sessions rides at the head of its shell's bar",
    );
    assert.equal(left.active, 1, "the focused chat tab keeps focus");
    assert.equal(out !== t, true);
  });

  it("restoreSeatSet re-adds a secondary-sidebar singleton that died with its pane", () => {
    let t: DockNode = defaultTree();
    // kanban can only die via a pane prune. that pane is the LAST MAIN
    // pane, so a split must mint a second main pane first.: settings is NOT in this
    // class any more - it left the inspector for the on-demand main tab (see the retire
    // test).
    const insp = paneByComponent(t, "files");
    const kanbanIdx = insp.tabs.findIndex((x) => x.component === "kanban");
    t = moveTab(t, insp.id, kanbanIdx, paneByComponent(t, "chat").id, 0); // kanban out (settings left the strip -)
    const kanbanPane = findComponentPane(t, "kanban")!;
    t = splitPane(t, kanbanPane.id, "row", "after");
    t = removePane(t, kanbanPane.id);
    assert.equal(findComponentTab(t, "kanban"), null, "kanban is gone");
    const out = restoreSeatSet(t, "right");
    const loc = findComponentTab(out, "kanban")!;
    assert.equal(loc.paneId, paneByComponent(out, "files").id, "kanban returns to the right shell");
    // idempotent once everything is present
    assert.equal(restoreSeatSet(out, "right"), out);
  });

  it("retireInspectorSettings drops a persisted inspector seat, keeps a main-pane one (t_00e4a784)", () => {
    let t: DockNode = defaultTree();
    assert.equal(findComponentTab(t, "settings"), null, "a fresh tree never seats settings");
    const insp = paneByComponent(t, "files");
    insp.tabs = [...insp.tabs, { component: "settings" }];
    assert.equal(findComponentTab(t, "settings")!.paneId, insp.id);
    const out = retireInspectorSettings(t);
    assert.equal(findComponentTab(out, "settings"), null, "the pinned seat retires on load");
    assert.deepEqual(
      paneByComponent(out, "files").tabs.map((x) => x.component),
      INSPECTOR_TAB_COMPONENTS.map((c) => c as string),
      "the rest of the strip is untouched",
    );
    // a settings tab parked in a MAIN pane is the new home - it survives
    const chatPane = paneByComponent(t, "chat");
    const moved = moveTab(t, insp.id, INSPECTOR_TAB_COMPONENTS.length, chatPane.id, 1);
    assert.equal(findComponentTab(moved, "settings")!.paneId, chatPane.id);
    assert.equal(retireInspectorSettings(moved), moved, "no-op returns the SAME tree");
  });

  it("deriveEdgeMarkers fingerprints pre-marker trees and never grabs marked sides", () => {
    const modern = defaultTree(); // both markers present
    assert.equal(deriveEdgeMarkers(modern), modern, "no derivation on a marked tree");
    const legacy = contentTree(); // no markers: content fingerprints decide
    const derived = deriveEdgeMarkers(legacy)!;
    assert.equal(findPane(derived, paneByComponent(legacy, "sidebar").id)!.edge, "left");
    assert.equal(findPane(derived, paneByComponent(legacy, "files").id)!.edge, "right");
    assert.equal(isPinnedPane(paneByComponent(derived, "chat")), false);
  });

  it("the loader pipeline (ensurePinnedEdges) repairs a v1-era tree end to end", () => {
    const out = ensurePinnedEdges(makePane("chat"));
    const root = asSplit(out);
    assert.deepEqual(
      root.children.map((c) => (c.kind === "pane" ? c.edge : undefined)),
      ["left", undefined, "right"],
      "shells at both edges, main in the middle",
    );
    assert.equal(asPane(root.children[0]).tabs[0]?.component, "sidebar");
    assert.deepEqual(
      (root.children[2] as DockPane).tabs.map((x) => x.component),
      INSPECTOR_TAB_COMPONENTS,
      "the default arrangement comes back with the shell",
    );
    const t = defaultTree();
    assert.equal(ensurePinnedEdges(t), t);
  });
});

describe("v1 layout migration (t_d6b657a7)", () => {
  it("createDock upgrades a v1 tree to v2 with markers and persists it", async () => {
    const legacy: DockNode = {
      kind: "split",
      id: "n1",
      direction: "row",
      children: [
        { kind: "pane", id: "n2", tabs: [{ component: "sidebar" }], active: 0, fixed: 240 },
        { kind: "pane", id: "n3", tabs: [{ component: "chat" }], active: 0, fixed: null },
        {
          kind: "pane",
          id: "n4",
          tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
          active: 0,
          fixed: 300,
        },
      ],
      sizes: [1 / 3, 1 / 3, 1 / 3],
    };
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, JSON.stringify({ v: 1, tree: legacy }));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const loaded = deserializeLayout(mem.getItem(DOCK_LAYOUT_KEY))!;
    assert.equal(loaded.version, 4, "the migrated tree persisted as v4 (px on disk, sidebar shape law)");
    assert.equal(findPane(loaded.tree, "n2")!.edge, "left");
    assert.equal(findPane(loaded.tree, "n4")!.edge, "right");
  });

  it("a v2 arrangement reloads verbatim: shells empty stay empty - except insights' home (t_d7fd3ad3)", async () => {
    let t: DockNode = defaultTree();
    // empty the right shell into the main pane, then persist.: insights' home is the
    // right shell now - its old main-area tab (closable then, unclosable in the new
    // registry) is lifted back on boot, so the bar refills with exactly insights.
    const insp = paneByComponent(t, "files");
    let chatPane = paneByComponent(t, "chat");
    for (let i = 0; i < INSPECTOR_TAB_COMPONENTS.length; i++) {
      chatPane = paneByComponent(t, "chat");
      t = moveTab(t, insp.id, 0, chatPane.id, findPane(t, chatPane.id)!.tabs.length);
    }
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const shell = findEdgePane(tree, "inspector")!;
    assert.deepEqual(
      shell.tabs.map((x) => x.component),
      ["insights"],
      "the bar refills with insights - its home, never main",
    );
    assert.equal(isPinnedPane(shell), true, "the marker survived");
    // settings is NOT part of the arrangement any more.
    assert.equal(findComponentTab(tree, "settings"), null, "no settings seat on boot");
  });
});

// pane visibility saves WITH the layout.
describe("closed-edge persistence (card t_aa9dbada)", () => {
  it("serialize/deserializeLayout round-trips the closed-edge set", () => {
    const rec = deserializeLayout(serialize(defaultTree(), ["sidebar", "bottom"]))!;
    assert.deepEqual(rec.closedEdges, ["sidebar", "bottom"]);
    assert.equal(
      deserializeLayout(JSON.stringify({ v: 2, tree: defaultTree() }))!.closedEdges,
      undefined,
    );
    const filtered = deserializeLayout(
      JSON.stringify({ v: 2, tree: defaultTree(), closedEdges: ["sidebar", "alien", 42] }),
    )!;
    assert.deepEqual(filtered.closedEdges, ["sidebar"]);
    // garbage shapes -> empty, never thrown
    const shaped = deserializeLayout(
      JSON.stringify({ v: 2, tree: defaultTree(), closedEdges: "nope" }),
    )!;
    assert.deepEqual(shaped.closedEdges, []);
  });

  it("ensurePinnedEdges leaves a shell the record says is closed OUT", () => {
    const bare = makePane("chat");
    const out = ensurePinnedEdges(bare, ["sidebar", "inspector"]);
    assert.equal(findEdgePane(out, "sidebar"), null);
    assert.equal(findEdgePane(out, "inspector"), null);
    // a closed side is never marker-MINTED from content either.
    const main = makeSplit("row", [makePane("files"), makePane("chat")]);
    const unclaimed = deriveEdgeMarkers(main, ["inspector"]);
    assert.equal(findEdgePane(unclaimed, "inspector"), null);
  });

  it("RELOAD restores closed sidebars: close, flush, boot -> still closed; reopen -> back", async () => {
    const mem = new MemoryStorage();
    const first = createDock(mem as unknown as Storage);
    assert.ok(findEdgePane(first.tree.value, "sidebar"));
    assert.ok(findEdgePane(first.tree.value, "inspector"));
    first.toggleEdge("sidebar", "before");
    first.toggleEdge("inspector", "after");
    assert.equal(findEdgePane(first.tree.value, "sidebar"), null);
    await flush(); // the record carries the closed-edge set now
    const second = createDock(mem as unknown as Storage);
    assert.equal(findEdgePane(second.tree.value, "sidebar"), null, "sessions sidebar stays closed");
    assert.equal(findEdgePane(second.tree.value, "inspector"), null, "inspector sidebar stays closed");
    assert.ok(paneCount(second.tree.value) >= 1, "the dock is never empty");
    second.toggleEdge("sidebar", "before");
    await flush();
    const third = createDock(mem as unknown as Storage);
    assert.ok(findEdgePane(third.tree.value, "sidebar"), "reopen persists too");
  });

  it("a legacy record (no closedEdges) migrates the bottom flag once, then the key retires", async () => {
    const mem = new MemoryStorage();
    const rec = JSON.parse(serialize(defaultTree()));
    delete rec.closedEdges; // the pre-fix record shape
    mem.setItem(DOCK_LAYOUT_KEY, JSON.stringify(rec));
    mem.setItem(BOTTOM_CLOSED_KEY, "1");
    const dock = createDock(mem as unknown as Storage);
    assert.equal(findEdgePane(dock.tree.value, "bottom"), null, "the legacy closure migrates");
    assert.ok(findEdgePane(dock.tree.value, "sidebar"), "sidebars are untouched by the flag");
    assert.equal(mem.getItem(BOTTOM_CLOSED_KEY), null, "the key retired after the read");
    await flush(); // the migrated closure persists as record data
    const after = createDock(mem as unknown as Storage);
    assert.equal(findEdgePane(after.tree.value, "bottom"), null, "the migrated closure persists");
  });
});

// the v:3 writer poisons its own record when a pane omits the fixed field. The writer
// must NEVER emit a null px.
describe("v:3 px shares tolerate a pane that omits fixed (t_f1898589)", () => {
  it("serialize emits finite positive px; the record loads back", () => {
    // a pane built WITHOUT the fixed field.
    const noFixedA = {
      kind: "pane", id: nextNodeId(), tabs: [{ component: "sidebar" }], active: 0,
    } as unknown as DockPane;
    const noFixedB = {
      kind: "pane", id: nextNodeId(), tabs: [{ component: "chat" }], active: 0,
    } as unknown as DockPane;
    const tree: DockNode = makeSplit("row", [noFixedA, noFixedB]);
    tree.sizes = [0.25, 0.75];

    const rec = JSON.parse(serialize(tree)) as {
      v: 4;
      tree: { kind: "split"; px: unknown[]; children: unknown[] };
    };
    assert.equal(rec.v, 4);
    const px = rec.tree.px;
    assert.ok(Array.isArray(px), "the split record carries px shares");
    for (const p of px)
      assert.ok(
        typeof p === "number" && Number.isFinite(p) && p > 0,
        `every px entry is a finite positive number, got ${JSON.stringify(px)}`,
      );

    const loaded = deserializeLayout(serialize(tree));
    assert.ok(loaded, "the record the writer just produced must deserialize");
    const back = asSplit(loaded!.tree);
    assert.deepEqual(
      asPane(back.children[0]!).tabs.map((t) => t.component),
      ["sidebar"],
      "tab 0 round-trips",
    );
    assert.deepEqual(
      asPane(back.children[1]!).tabs.map((t) => t.component),
      ["chat"],
      "tab 1 round-trips",
    );
  });
});

// tiny Storage stand-in shared by the loader tests above
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