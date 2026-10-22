// omega webui: unit tests for the dock engine (app/composables/dock.ts) -
// layout tree pure functions, divider drag math, persistence round-trip and
// legacy migration. Render-free; node's built-in test runner.
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
  dragSplitDivider,
  serialize,
  deserialize,
  deserializeLayout,
  migrateLegacy,
  nextNodeId,
  syncIdSeed,
  findPane,
  findComponentPane,
  findComponentTab,
  findEdgePane,
  openSingleton,
  createDock,
  DOCK_LAYOUT_KEY,
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
  ensurePinnedEdges,
  forceSessionsLeft,
  restoreInspectorSet,
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

// card t_49d0b308: the tab-op fixtures below need a MAIN-area OPENABLE
// component; settings left that class for the inspector. The synthetic
// "scratch" singleton registers for this file's whole run (node --test
// gives every file its own process). Cast note: the engine ops take
// DockComponentName and node strips types before running - the string
// "scratch" flows through fine.
let restoreScratch: (() => void) | null = null;
before(() => {
  restoreScratch = registerScratchSingleton();
});
after(() => {
  restoreScratch?.();
});

// helpers ------------------------------------------------------------------

function asPane(n: DockNode): DockPane {
  assert.equal(n.kind, "pane");
  return n as DockPane;
}
function asSplit(n: DockNode): DockSplit {
  assert.equal(n.kind, "split");
  return n as DockSplit;
}
/**
 * default shell shape (card t_4ae27e0d, markers t_d6b657a7): row
 * [sidebar(fixed 240), chat(flex), inspector-pane(fixed 300: the seven
 * real view tabs, files active)]. The content shell is passed through
 * ensurePinnedEdges so the edge markers ride on it (the modern trees'
 * shape).
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
  // top-level left-to-right reading of every pane's active tab
  const root = asSplit(t);
  const walk = (n: DockNode): (string | undefined)[] =>
    n.kind === "pane"
      ? [n.tabs[n.active]?.component]
      : n.children.flatMap(walk);
  return walk(root);
}
function paneByComponent(t: DockNode, c: "sidebar" | "chat" | "files"): DockPane {
  // ANY tab hosting, not active-only: cross-pane DnD parks singletons as
  // inactive tabs all the time now (free content).
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

  it("default shape carries the five real inspector tabs (files active)", () => {
    const t = defaultTree();
    const insp = asSplit(t).children[2];
    assert.deepEqual(
      asPane(insp).tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "agents", "settings", "kanban"],
    );
    assert.equal(asPane(insp).active, 0);
  });
});

describe("insertPane / splitPane", () => {
  it("splits a mid pane: new pane wraps in a row split, empty (menu)", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const out = asSplit(splitPane(t, chat.id, "row", "after"));
    assert.equal(out.id, t.id); // root untouched
    const inner = asSplit(out.children[1]);
    assert.equal(inner.direction, "row");
    assert.equal(asPane(inner.children[0]).id, chat.id);
    assert.equal(asPane(inner.children[1]).tabs.length, 0);
    assert.deepEqual(comps(out), ["sidebar", "chat", undefined, "files"]);
  });
  it("side 'before' puts the new pane left", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const out = asSplit(splitPane(t, chat.id, "row", "before"));
    const inner = asSplit(out.children[1]);
    assert.equal(asPane(inner.children[0]).tabs.length, 0);
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
    const empty = panes(t).find((p) => p.tabs.length === 0)!;
    t = splitPane(t, empty.id, "column", "before");
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
    const empty = panes(t).find((p) => p.tabs.length === 0)!; // the split-away pane
    const out = asSplit(removePane(t, empty.id));
    assert.ok(!findPane(out, empty.id));
    assert.equal(paneCount(out), 3);
  });
  it("a single-child split hoists to the child", () => {
    // split the chat: root [sidebar, row[chat, empty], files]; dropping
    // the empty pane single-childrens the interior row - it hoists to the
    // chat pane itself (the engine guard never even called here: the
    // split never went below 2 children, it collapsed instead)
    let t: DockNode = defaultTree();
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after");
    const empty = panes(t).find((p) => p.tabs.length === 0)!;
    t = removePane(t, empty.id);
    assert.equal(paneCount(t), 3);
    const survivor = asSplit(t).children[1];
    assert.equal(survivor.kind, "pane", "the interior row hoisted to its child");
    assert.equal(survivor.id, chat.id, "the chat pane is the survivor");
  });
  it("the LAST REMAINING pane is refused (never-empty invariant)", () => {
    const t = asPane(makePane("chat"));
    assert.equal(removePane(t, t.id), t, "the tree object is returned untouched");
    // even sidebars-only... the guard reads COUNT, not identity: with one
    // pane left of any kind, the close is a no-op
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
    // the detach hoisted the chat's split to its EMPTY pane, which now
    // sits between sidebar and the insert point - the menu ruling again
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
    // root: [sidebar, files, chat]; the split that held half the chat
    // keeps only its empty pane -> hoisted, so the empty pane is top-level
    assert.deepEqual(comps(out), ["sidebar", undefined, "files", "chat"]);
    const empty = panes(out).find((p) => p.tabs.length === 0)!;
    assert.ok(empty, "the split-away empty pane survives the move");
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
  it("openTab appends and focuses; closeTab removes and shifts focus", () => {
    const t0 = asPane(makePane("chat"));
    const id = t0.id;
    let t: DockNode = openTab(t0, id, { component: "scratch" });
    let p = findPane(t, id)!;
    assert.equal(p.tabs.length, 2);
    assert.equal(p.active, 1);
    t = focusTab(t, id, 0);
    t = closeTab(t, id, 0);
    p = findPane(t, id)!;
    assert.equal(p.tabs.length, 1);
    assert.equal(p.active, 0); // focus shifted back onto the survivor
    t = closeTab(t, id, 0);
    p = findPane(t, id)!;
    assert.equal(p.tabs.length, 0, "closing the last tab empties the pane");
    assert.equal(p.active, 0);
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
    /** split shell: left pane main tabs, right pane chat */
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
      // [chat, settings]; chat is NOT the focused tab (openTab focused
      // settings) - dragging chat right moves it after settings, and the
      // parked active tab TRACKS its component (settings shifts left)
      t = moveTab(t, id, 0, id, 1);
      let p = findPane(t, id)!;
      assert.deepEqual(tabComps(t, id), ["scratch", "chat"]);
      assert.equal(p.active, 0, "the parked active tab tracks its component");
      // drag the focused tab (settings, now at 0) left of chat: no-op
      t = moveTab(t, id, 0, id, 0);
      p = findPane(t, id)!;
      assert.equal(p.active, 0, "focused tab keeps its position");
      // now focus chat and drag it back left across the bookkeeping
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
      // drag chat (left tab 0) onto the chat pane
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
      // target caret before tab 0: post-removal index 0
      const out = moveTab(t, left.id, 0, right.id, 0);
      assert.deepEqual(tabComps(out, right.id), ["chat", "chat"]);
      assert.equal(findPane(out, right.id)!.active, 0);
    });

    it("moving a pane's LAST tab empties but KEEPS the pane", () => {
      const { t, left, right } = twoPaneTree();
      // drain left pane one tab at a time (target index from the FRESH
      // pane - destructured refs point at the pre-move tree)
      let cur = moveTab(t, left.id, 0, right.id, findPane(t, right.id)!.tabs.length);
      assert.equal(panes(cur).length, 2, "no pane vanished");
      cur = moveTab(cur, left.id, 0, right.id, findPane(cur, right.id)!.tabs.length);
      const l = findPane(cur, left.id)!;
      assert.equal(l.tabs.length, 0, "source pane empty");
      assert.equal(panes(cur).length, 2, "empty source pane survives (menu ruling)");
      assert.deepEqual(tabComps(cur, right.id), ["chat", "chat", "scratch"]);
      assert.ok(
        findPane(cur, right.id)!.tabs.some((x) => x.component === "scratch"),
        "the drained tab landed in the right pane",
      );
    });

    it("emptying the source moves focus to the remaining neighbor", () => {
      const two = twoPaneTree();
      // left has chat(0, active) + settings(1). Move tab 0 away.
      const out = moveTab(two.t, two.left.id, 0, two.right.id, 0);
      const l = findPane(out, two.left.id)!;
      assert.equal(l.tabs.length, 1);
      assert.equal(l.active, 0, "neighbor takes over the focus");
    });

    it("LOCKED sessions refuses every drop outside the left shell; everything else moves free", () => {
      const t = defaultTree();
      const side = paneByComponent(t, "sidebar");
      const files = paneByComponent(t, "files");
      const chat = paneByComponent(t, "chat");
      // ruling 1: the sessions tab never leaves the left shell's own bar
      assert.equal(moveTab(t, side.id, 0, chat.id, 0), t, "sessions does not drag right");
      assert.equal(moveTab(t, side.id, 0, files.id, 0), t, "sessions does not drag into the right shell");
      // ruling 2: EVERYTHING ELSE drags everywhere...
      const movedOut = moveTab(t, files.id, 0, chat.id, 0);
      assert.deepEqual(
        tabComps(movedOut, chat.id),
        ["files", "chat"],
        "a right-shell tab becomes a main-pane tab",
      );
      // ...and back again (bidirectional into the pinned shell)
      const movedBack = moveTab(movedOut, chat.id, 0, files.id, 0);
      assert.deepEqual(
        tabComps(movedBack, files.id),
        ["files", "cron", "http_channel", "skills", "tools", "agents", "settings", "kanban"],
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

  it("MAIN_DOCKABLES: the MAIN-area inventory (card t_4ae27e0d; settings left for the inspector, t_49d0b308; insights joined, t_e1e61680)", () => {
    assert.deepEqual([...MAIN_DOCKABLES], ["chat", "insights"]);
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
          v: 3,
          tree: { kind: "pane", id: "n1", tabs: [], active: 0, fixed: null },
        }),
      ),
      null,
      "future version forces migration instead of crashing",
    );
  });
  it("v2 round-trips the edge markers and the free tab arrangement", () => {
    let t: DockNode = defaultTree();
    // free content: files in the main pane, an empty right shell
    const files = paneByComponent(t, "files");
    const chat = paneByComponent(t, "chat");
    t = moveTab(t, files.id, 0, chat.id, 0);
    const insp = asSplit(t).children[2];
    (insp as DockPane).tabs = [];
    const back = deserialize(serialize(t))!;
    assert.ok(back);
    // which tab lives in which pane survives the round-trip verbatim
    assert.deepEqual(tabCompsIn(back, chat.id), ["files", "chat"]);
    assert.equal(paneCount(back), 3, "the emptied shell survives as a pane");
    assert.equal(isPinnedPane(findPane(back, asPane(insp).id)!), true, "the shell keeps its marker");
    assert.deepEqual([...allNodes(back)].filter((n) => n.kind === "pane")
      .flatMap((n) => (n as DockPane).edge ? [n.edge] : []), ["left", "right"]);
  });
  it("marker-bearing v2 trees deserialize; bogus markers fall back to main", () => {
    const t = defaultTree();
    const back = deserializeLayout(serialize(t))!;
    assert.equal(back.version, 2);
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
  // card t_67fc1905: the default tree gains the bottom panel - the root is
  // a COLUMN now: [the main row [sidebar, chat, inspector], the bottom
  // shell (fixed 200, logs tab, edge "bottom")]
  function mainRow(t: DockNode): DockSplit {
    const root = asSplit(t);
    assert.equal(root.direction, "column", "the default root is a column (bottom-panel era)");
    assert.equal(root.children.length, 2, "main row + bottom shell");
    return asSplit(root.children[0]);
  }
  it("always four panes: sidebar(240), chat, inspector-7(240 default), bottom(200, logs)", () => {
    const t = migrateLegacy(() => null);
    const row = mainRow(t);
    assert.equal(row.children.length, 3);
    assert.equal(asPane(row.children[0]).tabs[0].component, "sidebar");
    assert.equal(asPane(row.children[0]).fixed, 240);
    assert.equal(asPane(row.children[1]).tabs[0].component, "chat");
    assert.equal(asPane(row.children[1]).fixed, null);
    assert.equal(asPane(row.children[2]).fixed, 240);
    assert.deepEqual(
      asPane(row.children[2]).tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "agents", "settings", "kanban"],
    );
    const bottom = asPane(asSplit(t).children[1]);
    assert.equal(bottom.edge, "bottom");
    assert.equal(bottom.fixed, 200);
    assert.deepEqual(bottom.tabs.map((x) => x.component), ["http_channel"]);
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
  it("bad flag strings fall back to the default widths", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "abc" : k === LEGACY_WIDTH_KEYS.inspector ? "maybe" : null,
    );
    const row = mainRow(t);
    assert.equal(asPane(row.children[0]).fixed, 240);
    assert.equal(asPane(row.children[2]).fixed, 240);
  });
});

// --- the pane-model reset specifics (card t_4ae27e0d) ------------------------

describe("pane-model reset", () => {
  it("expandInspectorTabs expands the legacy id into the five real tabs", () => {
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
      ["files", "cron", "http_channel", "skills", "tools", "agents", "settings", "kanban"],
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
    assert.equal(asPane(out.children[0]).fixed, 240);
  });
  it("ensurePinnedEdges re-inserts the inspector pane (five tabs) at the RIGHT edge", () => {
    const t: DockNode = makePane("chat");
    const out = asSplit(ensurePinnedEdges(t));
    const right = asPane(out.children[2]); // [sidebar, chat, inspector]
    assert.deepEqual(
      right.tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "agents", "settings", "kanban"],
    );
    assert.equal(right.fixed, 240);
  });
  it("a modern tree passes ensurePinnedEdges untouched", () => {
    const t = defaultTree();
    assert.equal(ensurePinnedEdges(t), t);
  });
  it("boot repair never mints an id the loaded tree already owns (n1 collision, t_f6d5a771)", () => {
    // reload after a divider hide: the sidebar is gone, the stored tree
    // still holds its old ids - chat "n3", files "n1". The boot repair
    // re-inserts the sidebar; with the per-page seed at 0 that mint would
    // be "n1" TOO unless the seed synced first - duplicate ids, and every
    // id-keyed op (the divider intents' prunePane, canRemovePane) acts on
    // BOTH panes at once.
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
  it("insertMainPane appends a flexible pane and returns its id", () => {
    const t = defaultTree();
    const before = paneIds(t);
    const res = insertMainPane(t);
    const root = asSplit(res.tree);
    // a row root appends at its after-edge, ids preserved in order
    assert.deepEqual(paneIds(res.tree), [...before, res.paneId]);
    const fresh = findPane(res.tree, res.paneId)!;
    assert.equal(fresh.fixed, null);
    assert.equal(fresh.tabs.length, 0);
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

// --- gate: the edge toggle (card t_47b776ad) -----------------------------------

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
    // absent -> null
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
    // the inspector pane is untouched by a sidebar hide
    assert.notEqual(findEdgePane(hidden, "inspector"), null);
  });

  it("revealEdgePane reopens the pane at its edge with the clamp width", () => {
    const hidden = hideEdgePane(defaultTree(), "sidebar");
    const reopened = asSplit(revealEdgePane(hidden, "sidebar", "before")).children[0];
    assert.equal(asPane(reopened).tabs[0]?.component, "sidebar");
    assert.equal(asPane(reopened).fixed, Math.max(PANE_OPEN_MIN, 240));
    // identical fingerprint to the boot-repair pane
    assert.equal(isPinnedPane(asPane(reopened)), true);
    const insp = asPane(
      asSplit(revealEdgePane(hideEdgePane(defaultTree(), "inspector"), "inspector", "after")).children[2],
    );
    assert.deepEqual(
      insp.tabs.map((x): string => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "agents", "settings", "kanban"],
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
    // a main-only tree has no edge pane - hide is a no-op
    const main = makePane("chat");
    assert.equal(hideEdgePane(main, "inspector"), main);
  });
});

describe("edge width memory (card t_f6d5a771)", () => {
  // minimal Storage stub: the memory lives beside the tree key
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

describe("dragSplitDivider", () => {
  // shell: row [sidebar(fixed 240), chat(flex), inspector pane(fixed 300)],
  // split content box 1000px. Rendered start widths sum to the content box
  // minus gutters; the pure math only needs consistent startPx + contentPx.
  const CONTENT = 1000;

  it("left fixed: grows over the flexible chat", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 100, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, 340);
    assert.equal(asPane(out.split.children[1]).fixed, null, "chat stays flexible");
    assert.equal(out.collapsedPaneId, null);
    assert.equal(out.pushedPaneId, null);
  });
  it("left fixed: caps against the flexible chat floor (320)", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 900, CONTENT);
    // room = 1000 - 300(inspector) = 700; max = 700 - 320 = 380
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
    // zone is 305..335: 304 stays free, 318/322 snap, 336 stays free
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 64, CONTENT).split.children[0]).fixed, 304);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 78, CONTENT).split.children[0]).fixed, 320);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 82, CONTENT).split.children[0]).fixed, 320);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 96, CONTENT).split.children[0]).fixed, 336);
  });
  it("snap never crosses the cap (clamped after snapping)", () => {
    const t = defaultTree();
    // room = 1000 - 300; max 380: dragging toward 640 snaps to 640 but clamps
    // to 380
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
    // widening (drag left) by 25: 325 -> inside 320's zone -> snaps to 320
    assert.equal(asPane(dragSplitDivider(t, 2, [240, 460, 300], -25, CONTENT).split.children[2]).fixed, 320);
  });

  // --- card t_f6d5a771: divider close (drag-past-min) + squeeze push-back ---
  const sidebarIdOf = (t: DockSplit) => asPane(t.children[0]).id;
  const inspectorIdOf = (t: DockSplit) => asPane(t.children[2]).id;

  it("drag-past-min CLOSES the sidebar: the left divider reports a hide intent", () => {
    const t = defaultTree();
    // req 240 - 113 = 127 < 128: the pane goes
    const out = dragSplitDivider(t, 1, [240, 460, 300], -113, CONTENT);
    assert.equal(out.collapsedPaneId, sidebarIdOf(t));
    assert.equal(out.pushedPaneId, null);
  });
  it("drag-past-min CLOSES the inspector: the right divider mirrors it", () => {
    const t = defaultTree();
    // req 300 - 173 = 127 < 128 (delta +173): the pane goes
    const out = dragSplitDivider(t, 2, [240, 460, 300], 173, CONTENT);
    assert.equal(out.collapsedPaneId, inspectorIdOf(t));
    assert.equal(out.pushedPaneId, null);
  });
  it("push-back: over-cap drag SHRINKS the nearest fixed pane beyond first", () => {
    const t = defaultTree();
    // widening the sidebar: req 480, cap = 700 - 320 = 380, over 100 <=
    // inspector's slack (300 - 128): the inspector shrinks 300 -> 200
    const out = dragSplitDivider(t, 1, [240, 460, 300], 240, CONTENT);
    assert.equal(asPane(out.split.children[0]).fixed, 480);
    assert.equal(asPane(out.split.children[2]).fixed, 200);
    assert.equal(out.pushedPaneId, null);
  });
  it("push-back: starved past its floor, the beyond pane CLOSES instead", () => {
    const t = defaultTree();
    // req 1140; cap 380; over 760 > inspector slack 172 -> the inspector
    // reports as closed (the store prunes; the split here comes back as-is)
    const out = dragSplitDivider(t, 1, [240, 460, 300], 900, CONTENT);
    assert.equal(out.pushedPaneId, inspectorIdOf(t));
    assert.equal(out.collapsedPaneId, null);
    assert.equal(out.split.children.length, 3, "the pure math reports; the store prunes");
    assert.equal(asPane(out.split.children[0]).fixed, 380);
  });
  it("push-back (inspector divider): the starved LEFT sidebar closes", () => {
    const t = defaultTree();
    // widening the inspector: req 1200, cap = 760 - 320 = 440, over 760 >
    // sidebar slack 112 -> the sidebar reports closed
    const out = dragSplitDivider(t, 2, [240, 460, 300], -900, CONTENT);
    assert.equal(out.pushedPaneId, sidebarIdOf(t));
    assert.equal(asPane(out.split.children[2]).fixed, 440);
  });
  it("push-back with no fixed pane beyond: plain cap (no close intent)", () => {
    const t = hideEdgePane(defaultTree(), "inspector");
    // [sidebar(fixed 240), chat(flex)]: nothing beyond the chat to give
    const out = dragSplitDivider(t, 1, [240, 460], 900, 700);
    assert.equal(asPane(out.split.children[0]).fixed, 380);
    assert.equal(out.pushedPaneId, null);
    assert.equal(out.collapsedPaneId, null);
  });
  it("both flexible: fractions shift in tandem against the content box", () => {
    const t = defaultTree();
    const inner = asSplit(splitPane(t, paneByComponent(t, "chat").id, "row", "after").children[1]);
    // inner: [chat(flex), empty(flex)], fractions 0.5/0.5; content box 996px
    // (1000 split width - one 4px gutter) splits 498/498 at drag start
    const out = dragSplitDivider(inner, 1, [498, 498], 100, 996);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 598) < 1e-6, `left got ${out.split.sizes[0] * 996}`);
    assert.ok(Math.abs(out.split.sizes[1] * 996 - 398) < 1e-6);
    const sum = out.split.sizes.reduce((a, s) => a + s, 0);
    assert.ok(Math.abs(sum - 1) < 1e-9);
  });
  it("both flexible: 320 floor between them, cap clamps the fraction", () => {
    const t = defaultTree();
    const inner = asSplit(splitPane(t, paneByComponent(t, "chat").id, "row", "after").children[1]);
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
    assert.equal(out.pushedPaneId, null);
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
    // ids elsewhere untouched
    assert.equal(asPane(asSplit(out).children[0]).fixed, 240);
  });
  it("replaces the root when the replacement carries the root id; identity when absent", () => {
    const t = defaultTree();
    // dragging the ROOT's divider produces a replacement split with t's id
    const dragged = dragSplitDivider(t, 1, [240, 460, 300], 50, 1000);
    // root id == replacement id -> the replacement IS the new root
    assert.equal(graftSplit(t, dragged.split), dragged.split);
    // no such split inside -> original reference returned
    const fresh = defaultTree();
    const missing = asSplit(splitPane(fresh, paneByComponent(fresh, "chat").id, "row", "after").children[1]);
    const other = defaultTree();
    assert.equal(graftSplit(other, missing), other); // no such split inside
  });
});

// --- t_d6b657a7: locked sessions, free content, pinned shells -----------------

describe("tab DnD everywhere (t_d6b657a7)", () => {
  it("dropAllowedFor: locked drags only into the left shell; others everywhere", () => {
    const locked = { paneId: "n1", index: 0, component: "sidebar" as const };
    const free = { paneId: "n1", index: 0, component: "files" as const };
    const none: typeof free | null = null;
    assert.equal(dropAllowedFor("left", locked), true, "sessions stays in its own bar");
    assert.equal(dropAllowedFor(null, locked), false, "sessions refuses main panes");
    assert.equal(dropAllowedFor("right", locked), false, "sessions refuses the right shell");
    assert.equal(dropAllowedFor(null, free), true, "files drops into main");
    assert.equal(dropAllowedFor("left", free), true, "files drops into the left shell");
    assert.equal(dropAllowedFor("right", free), true, "files drops into the right shell");
    assert.equal(dropAllowedFor(null, none), false, "no gesture - no drop");
  });

  it("drag sim: reorder WITHIN the sidebar bar keeps everything in place", () => {
    let t: DockNode = defaultTree();
    const side = paneByComponent(t, "sidebar");
    // files joins the sessions bar first (free content into the left shell)
    const insp = paneByComponent(t, "files");
    t = moveTab(t, insp.id, 0, side.id, 1);
    // bar: [sessions, files]; drag files' caret to before tab 0
    t = moveTab(t, side.id, 1, side.id, 0);
    assert.deepEqual(
      paneByComponent(t, "sidebar").tabs.map((x) => x.component),
      ["files", "sidebar"],
    );
    assert.equal(paneByComponent(t, "sidebar").id, side.id, "no new pane minted");
  });
  it("drag sim: sessions reorder within its bar is the ONLY locked move", () => {
    let t: DockNode = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const insp = paneByComponent(t, "files");
    t = moveTab(t, insp.id, 0, side.id, 1);
    // the bar is [sessions, files]: dragging sessions to the end (caret 2)
    // is a LEGAL reorder inside the shell's own bar
    const reordered = moveTab(t, side.id, 0, side.id, 2);
    assert.deepEqual(
      paneByComponent(reordered, "sidebar").tabs.map((x) => x.component),
      ["files", "sidebar"],
    );
  });

  it("empty shells are refillable: openSingleton / setPaneComponent target them", () => {
    let t: DockNode = defaultTree();
    // drain the right shell into the main pane (free content), one at a
    // time; the shell stays pinned + empty
    const insp = paneByComponent(t, "files");
    const chat = paneByComponent(t, "chat");
    for (let i = 0; i < 8; i++) {
      t = moveTab(t, insp.id, 0, chat.id, findPane(t, chat.id)!.tabs.length);
    }
    const shell = findPane(t, insp.id)!;
    assert.equal(shell.tabs.length, 0, "the shell sits empty");
    assert.equal(isPinnedPane(shell), true, "still a shell");
    // the '+' menu path fills the empty shell again: drain the tabs into
    // the main pane, then remove that pane so the singletons truly died
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

  it("restoreInspectorSet re-adds an inspector singleton that died with its pane", () => {
    let t: DockNode = defaultTree();
    // settings can only die via a pane prune: move it to the main pane,
    // then close the main pane
    const insp = paneByComponent(t, "files");
    t = moveTab(t, insp.id, 6, paneByComponent(t, "chat").id, 0); // settings out
    const settingsPane = findComponentPane(t, "settings")!;
    t = removePane(t, settingsPane.id);
    assert.equal(findComponentTab(t, "settings"), null, "settings is gone");
    const out = restoreInspectorSet(t);
    const loc = findComponentTab(out, "settings")!;
    assert.equal(loc.paneId, paneByComponent(out, "files").id, "settings returns to the right shell");
    // idempotent once everything is present
    assert.equal(restoreInspectorSet(out), out);
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
    // pre-marker era: sidebars toggled away, chat only
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
    // a fully-satisfied modern tree is untouched
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
    assert.equal(loaded.version, 2, "the migrated tree persisted as v2");
    assert.equal(findPane(loaded.tree, "n2")!.edge, "left");
    assert.equal(findPane(loaded.tree, "n4")!.edge, "right");
  });

  it("a v2 arrangement reloads verbatim: shells empty stay empty", async () => {
    let t: DockNode = defaultTree();
    // empty the right shell into the main pane, then persist
    const insp = paneByComponent(t, "files");
    let chatPane = paneByComponent(t, "chat");
    for (let i = 0; i < 8; i++) {
      chatPane = paneByComponent(t, "chat");
      t = moveTab(t, insp.id, 0, chatPane.id, findPane(t, chatPane.id)!.tabs.length);
    }
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const shell = findEdgePane(tree, "inspector")!;
    assert.equal(shell.tabs.length, 0, "the bar stays empty");
    assert.equal(isPinnedPane(shell), true, "the marker survived");
    const loc = findComponentTab(tree, "settings")!;
    assert.equal(loc.tab.component, "settings");
    assert.equal(findPane(tree, loc.paneId)!.id, chatPane.id, "settings still lives in the main pane");
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