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
  migrateLegacy,
  nextNodeId,
  findPane,
  findComponentPane,
  findEdgePane,
  hideEdgePane,
  revealEdgePane,
  allNodes,
  isPinnedPane,
  paneCount,
  firstMainPaneId,
  insertMainPane,
  expandInspectorTabs,
  ensurePinnedEdges,
  normalizeSizes,
  PANE_OPEN_MIN,
  LEGACY_WIDTH_KEYS,
  MIN_PANE_PX,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import { registerScratchSingleton } from "./scratchDockable.ts";

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
 * default shell shape (card t_4ae27e0d): row
 * [sidebar(fixed 240), chat(flex), inspector-pane(fixed 300: the five
 * real view tabs, files active)].
 */
function defaultTree(): DockSplit {
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
  const p = findComponentPane(t, c);
  assert.ok(p, `expected a pane hosting ${c}`);
  return p;
}
function splits(t: DockNode): DockSplit[] {
  return [...allNodes(t)].filter((n) => n.kind === "split") as DockSplit[];
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
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
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

    it("PINNED tab refuses to move; PINNED target refuses a drop", () => {
      const t = defaultTree();
      const side = paneByComponent(t, "sidebar");
      const files = paneByComponent(t, "files");
      const chat = paneByComponent(t, "chat");
      // a pinned tab never leaves its pane...
      assert.equal(moveTab(t, side.id, 0, chat.id, 0), t);
      assert.equal(moveTab(t, files.id, 0, chat.id, 0), t);
      // ...and no tab may move INTO a pinned pane
      assert.equal(moveTab(t, chat.id, 0, side.id, 0), t);
      assert.equal(moveTab(t, chat.id, 0, files.id, 0), t);
      assert.deepEqual(tabComps(t, chat.id), ["chat"]);
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

  it("MAIN_DOCKABLES: the MAIN-area inventory only (card t_4ae27e0d; settings left for the inspector, t_49d0b308)", () => {
    assert.deepEqual([...MAIN_DOCKABLES], ["chat"]);
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
          v: 1,
          tree: { kind: "pane", id: "n1", tabs: [{ component: "future" }], active: 0, fixed: 100 },
        }),
      ),
      null,
    );
    assert.equal(
      deserialize(
        JSON.stringify({
          v: 2,
          tree: { kind: "pane", id: "n1", tabs: [], active: 0, fixed: null },
        }),
      ),
      null,
      "future version forces migration instead of crashing",
    );
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
  it("always three panes: sidebar(240), chat, inspector-5(240 default)", () => {
    const t = migrateLegacy(() => null);
    const root = asSplit(t);
    assert.equal(root.children.length, 3);
    assert.equal(asPane(root.children[0]).tabs[0].component, "sidebar");
    assert.equal(asPane(root.children[0]).fixed, 240);
    assert.equal(asPane(root.children[1]).tabs[0].component, "chat");
    assert.equal(asPane(root.children[1]).fixed, null);
    assert.equal(asPane(root.children[2]).fixed, 240);
    assert.deepEqual(
      asPane(root.children[2]).tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
    );
  });
  it("persisted widths render verbatim, open flags are dead history", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "300"
      : k === LEGACY_WIDTH_KEYS.inspector ? "400"
      : "false", // even a persisted 'closed' flag changes nothing
    );
    const root = asSplit(t);
    assert.equal(asPane(root.children[0]).fixed, 300);
    assert.equal(asPane(root.children[2]).fixed, 400);
  });
  it("sub-floor legacy width renders verbatim (open pane keeps its width)", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "150" : null,
    );
    assert.equal(asPane(asSplit(t).children[0]).fixed, 150);
  });
  it("bad flag strings fall back to the default widths", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_WIDTH_KEYS.sidebar ? "abc" : k === LEGACY_WIDTH_KEYS.inspector ? "maybe" : null,
    );
    const root = asSplit(t);
    assert.equal(asPane(root.children[0]).fixed, 240);
    assert.equal(asPane(root.children[2]).fixed, 240);
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
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
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
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
    );
    assert.equal(right.fixed, 240);
  });
  it("a modern tree passes ensurePinnedEdges untouched", () => {
    const t = defaultTree();
    assert.equal(ensurePinnedEdges(t), t);
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
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
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

describe("dragSplitDivider", () => {
  // shell: row [sidebar(fixed 240), chat(flex), inspector pane(fixed 300)],
  // split content box 1000px. Rendered start widths sum to the content box
  // minus gutters; the pure math only needs consistent startPx + contentPx.
  const CONTENT = 1000;

  it("left fixed: grows over the flexible chat", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 100, CONTENT);
    assert.equal(asPane(out.children[0]).fixed, 340);
    assert.equal(asPane(out.children[1]).fixed, null, "chat stays flexible");
  });
  it("left fixed: caps against the flexible chat floor (320)", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], 900, CONTENT);
    // room = 1000 - 300(inspector) = 700; max = 700 - 320 = 380
    assert.equal(asPane(out.children[0]).fixed, 380);
  });
  it("left fixed: honors the 128px drag floor", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 1, [240, 460, 300], -500, CONTENT);
    assert.equal(asPane(out.children[0]).fixed, MIN_PANE_PX);
  });
  it("left fixed: snap attractor sticks the 320+-15 zone to 320", () => {
    const t = defaultTree();
    // zone is 305..335: 304 stays free, 318/322 snap, 336 stays free
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 64, CONTENT).children[0]).fixed, 304);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 78, CONTENT).children[0]).fixed, 320);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 82, CONTENT).children[0]).fixed, 320);
    assert.equal(asPane(dragSplitDivider(t, 1, [240, 460, 300], 96, CONTENT).children[0]).fixed, 336);
  });
  it("snap never crosses the cap (clamped after snapping)", () => {
    const t = defaultTree();
    // room = 1000 - 300; max 380: dragging toward 640 snaps to 640 but clamps
    // to 380
    const out = dragSplitDivider(t, 1, [240, 460, 300], 700, CONTENT);
    assert.equal(asPane(out.children[0]).fixed, 380);
  });
  it("right fixed (inspector): drag inverts - dragging left widens it", () => {
    const t = defaultTree();
    const out = dragSplitDivider(t, 2, [240, 460, 300], -60, CONTENT);
    assert.equal(asPane(out.children[2]).fixed, 360);
  });
  it("right fixed: caps against the flexible chat floor and floors at 128", () => {
    const t = defaultTree();
    // room = 1000 - 240 = 760; inspector max = 760 - 320 = 440 (drag LEFT)
    assert.equal(asPane(dragSplitDivider(t, 2, [240, 460, 300], -900, CONTENT).children[2]).fixed, 440);
    // drag RIGHT narrows it below the floor
    assert.equal(asPane(dragSplitDivider(t, 2, [240, 460, 300], 600, CONTENT).children[2]).fixed, MIN_PANE_PX);
  });
  it("right fixed: snap reads the inspector's px", () => {
    const t = defaultTree();
    // widening (drag left) by 25: 325 -> inside 320's zone -> snaps to 320
    assert.equal(asPane(dragSplitDivider(t, 2, [240, 460, 300], -25, CONTENT).children[2]).fixed, 320);
  });
  it("both flexible: fractions shift in tandem against the content box", () => {
    const t = defaultTree();
    const inner = asSplit(splitPane(t, paneByComponent(t, "chat").id, "row", "after").children[1]);
    // inner: [chat(flex), empty(flex)], fractions 0.5/0.5; content box 996px
    // (1000 split width - one 4px gutter) splits 498/498 at drag start
    const out = dragSplitDivider(inner, 1, [498, 498], 100, 996);
    assert.ok(Math.abs(out.sizes[0] * 996 - 598) < 1e-6, `left got ${out.sizes[0] * 996}`);
    assert.ok(Math.abs(out.sizes[1] * 996 - 398) < 1e-6);
    const sum = out.sizes.reduce((a, s) => a + s, 0);
    assert.ok(Math.abs(sum - 1) < 1e-9);
  });
  it("both flexible: 320 floor between them, cap clamps the fraction", () => {
    const t = defaultTree();
    const inner = asSplit(splitPane(t, paneByComponent(t, "chat").id, "row", "after").children[1]);
    const out = dragSplitDivider(inner, 1, [498, 498], 900, 996);
    // room 996, max left 996 - 320 = 676 -> fraction 676/996; right keeps >=320
    assert.ok(Math.abs(out.sizes[0] * 996 - 676) < 1e-9);
    assert.ok(out.sizes[1] * 996 >= 320 - 0.001);
  });
  it("no-op on out-of-range divider index", () => {
    const t = defaultTree();
    assert.equal(dragSplitDivider(t, 0, [240, 460, 300], 10, CONTENT), t);
  });
});
describe("graftSplit", () => {
  it("swaps a nested split by id without touching ids", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const t2 = asSplit(splitPane(t, chat.id, "row", "after"));
    const inner = asSplit(t2.children[1]);
    const replaced = dragSplitDivider(inner, 1, [498, 498], 50, 996);
    const out = graftSplit(t2, replaced);
    assert.equal(asSplit(out).children[1].id, inner.id);
    assert.equal(asSplit(asSplit(out).children[1]).sizes[0], replaced.sizes[0]);
    // ids elsewhere untouched
    assert.equal(asPane(asSplit(out).children[0]).fixed, 240);
  });
  it("replaces the root when the replacement carries the root id; identity when absent", () => {
    const t = defaultTree();
    // dragging the ROOT's divider produces a replacement split with t's id
    const dragged = dragSplitDivider(t, 1, [240, 460, 300], 50, 1000);
    // root id == replacement id -> the replacement IS the new root
    assert.equal(graftSplit(t, dragged), dragged);
    // no such split inside -> original reference returned
    const fresh = defaultTree();
    const missing = asSplit(splitPane(fresh, paneByComponent(fresh, "chat").id, "row", "after").children[1]);
    const other = defaultTree();
    assert.equal(graftSplit(other, missing), other); // no such split inside
  });
});