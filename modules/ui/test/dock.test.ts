// omega webui: unit tests for the dock engine (app/composables/dock.ts) -
// layout tree pure functions, divider drag math, persistence round-trip and
// legacy migration. Render-free; node's built-in test runner.
import { describe, it } from "node:test";
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
  PLUS_TAB_COMPONENTS,
  dragSplitDivider,
  serialize,
  deserialize,
  migrateLegacy,
  nextNodeId,
  findPane,
  findComponentPane,
  allNodes,
  normalizeSizes,
  LEGACY_OPEN_KEYS,
  LEGACY_WIDTH_KEYS,
  MIN_PANE_PX,
  PANE_OPEN_MIN,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

// helpers ------------------------------------------------------------------

function asPane(n: DockNode): DockPane {
  assert.equal(n.kind, "pane");
  return n as DockPane;
}
function asSplit(n: DockNode): DockSplit {
  assert.equal(n.kind, "split");
  return n as DockSplit;
}
/** default shell shape: row [sidebar(fixed 240), chat(flex), inspector(fixed 300)] */
function defaultTree(): DockSplit {
  return asSplit(
    makeSplit("row", [
      makePane("sidebar", 240),
      makePane("chat", null),
      makePane("inspector", 300),
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
function paneByComponent(t: DockNode, c: "sidebar" | "chat" | "inspector"): DockPane {
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
    assert.deepEqual(comps(out), ["sidebar", "chat", undefined, "inspector"]);
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

describe("removePane", () => {
  it("removes a pane; the split keeps its survivors", () => {
    const t = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const out = removePane(t, side.id);
    assert.ok(!findPane(out, side.id));
    assert.deepEqual(comps(asSplit(out)), ["chat", "inspector"]);
  });
  it("a single-child split hoists to the child (root may become a pane)", () => {
    let out: DockNode = defaultTree();
    out = removePane(out, paneByComponent(out, "sidebar").id);
    out = removePane(out, paneByComponent(out, "inspector").id);
    assert.equal(out.kind, "pane");
    assert.equal(asPane(out).tabs[0].component, "chat");
  });
  it("removing the last pane leaves ONE empty pane (the menu rules it)", () => {
    const t = asPane(makePane("chat"));
    const out = asPane(removePane(t, t.id));
    assert.equal(out.tabs.length, 0);
    assert.equal(out.active, 0);
  });
  it("no-op for an unknown id", () => {
    const t = defaultTree();
    assert.equal(removePane(t, "n999"), t);
  });
});

describe("movePane", () => {
  it("moves after a target: order [chat, inspector, sidebar]", () => {
    const t = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const insp = paneByComponent(t, "inspector");
    const out = movePane(t, side.id, insp.id, "after");
    assert.deepEqual(comps(out), ["chat", "inspector", "sidebar"]);
  });
  it("moves before a target: order [inspector, sidebar, chat]", () => {
    const t = defaultTree();
    const side = paneByComponent(t, "sidebar");
    const insp = paneByComponent(t, "inspector");
    const out = movePane(t, insp.id, side.id, "before");
    assert.deepEqual(comps(out), ["inspector", "sidebar", "chat"]);
  });
  it("moving out of a nested split collapses it to the survivor", () => {
    const t = defaultTree();
    const chat = paneByComponent(t, "chat");
    const t2 = splitPane(t, chat.id, "row", "after");
    const rowPane = findPane(t2, chat.id)!;
    const out = movePane(t2, rowPane.id, paneByComponent(t2, "inspector").id, "after");
    // root: [sidebar, inspector, chat]; the split that held half the chat
    // keeps only its empty pane -> hoisted, so the empty pane is top-level
    assert.deepEqual(comps(out), ["sidebar", undefined, "inspector", "chat"]);
    const empty = panes(out).find((p) => p.tabs.length === 0)!;
    assert.ok(empty, "the split-away empty pane survives the move");
    assert.equal(splits(out).length, 2); // root + the split wrapping the drop target
  });
  it("no-ops: self-move / unknown ids", () => {
    const t = defaultTree();
    const side = paneByComponent(t, "sidebar");
    assert.equal(movePane(t, side.id, side.id, "after"), t);
    assert.equal(movePane(t, side.id, "n999", "after"), t);
    assert.equal(movePane(t, "n999", side.id, "after"), t);
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
    let t: DockNode = openTab(t0, id, { component: "inspector" });
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

  describe("moveTab (card 2 DnD)", () => {
    function tabComps(t: DockNode, id: string): (string | undefined)[] {
      return (findPane(t, id) as DockPane).tabs.map((x) => x.component);
    }
    /** split shell: left pane sidebar/inspector tabs, right pane chat */
    function twoPaneTree(): { t: DockNode; left: DockPane; right: DockPane } {
      const root = makePane("sidebar");
      const left = asPane(openTab(root, root.id, { component: "inspector" }));
      const right = makePane("chat");
      const t = makeSplit("row", [left, right]);
      return { t, left: findPane(t, left.id)!, right: findPane(t, right.id)! };
    }

    it("reorders within a pane, keeping the moved tab focused", () => {
      let t: DockNode = makePane("chat");
      const id = t.id;
      t = openTab(t, id, { component: "inspector" });
      t = openTab(t, id, { component: "sidebar" });
      // [chat, inspector, sidebar]; focus chat first, then drag it to the end
      t = focusTab(t, id, 0);
      t = moveTab(t, id, 0, id, 2);
      let p = findPane(t, id)!;
      assert.deepEqual(tabComps(t, id), ["inspector", "sidebar", "chat"]);
      assert.equal(p.active, 2, "the moved tab stays focused");
      // drag it back left across the active bookkeeping: [chat, inspector, sidebar]
      t = moveTab(t, id, 2, id, 0);
      p = findPane(t, id)!;
      assert.deepEqual(tabComps(t, id), ["chat", "inspector", "sidebar"]);
      // focus was on inspector (1); moving sidebar(2) before it shifts focus +1
      t = focusTab(t, id, 1);
      t = moveTab(t, id, 2, id, 1);
      p = findPane(t, id)!;
      assert.deepEqual(tabComps(t, id), ["chat", "sidebar", "inspector"]);
      assert.equal(p.active, 2, "the un-touched active tab tracks its component");
    });

    it("same-position drag is a no-op returning the ORIGINAL tree", () => {
      const { t, left } = twoPaneTree();
      assert.equal(moveTab(t, left.id, 0, left.id, 0), t);
    });

    it("moves between panes, focusing the dropped tab in the target", () => {
      const { t, left, right } = twoPaneTree();
      // drag sidebar (left tab 0) onto the chat pane
      const out = moveTab(t, left.id, 0, right.id, right.tabs.length);
      const l = findPane(out, left.id)!;
      const r = findPane(out, right.id)!;
      assert.equal(l.tabs.length, 1);
      assert.deepEqual(tabComps(out, left.id), ["inspector"]);
      assert.deepEqual(tabComps(out, right.id), ["chat", "sidebar"]);
      assert.equal(r.active, 1, "dropped tab focuses in the target");
      assert.equal(l.active, 0, "survivor keeps the focus");
    });

    it("inserts at the caret index in the target pane", () => {
      const { t, left, right } = twoPaneTree();
      // target caret before tab 0: post-removal index 0
      const out = moveTab(t, left.id, 0, right.id, 0);
      assert.deepEqual(tabComps(out, right.id), ["sidebar", "chat"]);
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
      assert.deepEqual(tabComps(cur, right.id), ["chat", "sidebar", "inspector"]);
      assert.ok(
        findPane(cur, right.id)!.tabs.some((t) => t.component === "sidebar"),
        "the drained tab landed in the right pane",
      );
    });

    it("emptying the source moves focus to the remaining neighbor", () => {
      const two = twoPaneTree();
      // left has sidebar(0, active) + inspector(1). Move tab 0 away.
      const out = moveTab(two.t, two.left.id, 0, two.right.id, 0);
      const l = findPane(out, two.left.id)!;
      assert.equal(l.tabs.length, 1);
      assert.equal(l.active, 0, "neighbor takes over the focus");
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
      assert.deepEqual(tabComps(back, left.id), ["inspector"]);
      assert.deepEqual(tabComps(back, right.id), ["sidebar", "chat"]);
    });
  });

  it("PLUS_TAB_COMPONENTS: card 2 ships chat only (card 4 grows the list)", () => {
    assert.deepEqual([...PLUS_TAB_COMPONENTS], ["chat"]);
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
    const raw = serialize(t).replace('"sizes":[0.3333333333333333,0.3333333333333333,0.3333333333333333]', '"sizes":[1,2,3]');
    // default tree was built with equal sizes; craft raw with drifted ones
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
  it("rejects unknown components and empty tabs arrays of bad shape", () => {
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
});

describe("migrateLegacy", () => {
  it("default flags: sidebar open (240), inspector closed -> [sidebar, chat]", () => {
    const t = migrateLegacy(() => null);
    assert.deepEqual(comps(t), ["sidebar", "chat"]);
    const root = asSplit(t);
    assert.equal(asPane(root.children[0]).fixed, 240);
    assert.equal(asPane(root.children[1]).fixed, null);
  });
  it("both open: [sidebar, chat, inspector]; widths are fixed px", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_OPEN_KEYS.sidebar ? "true"
      : k === LEGACY_OPEN_KEYS.inspector ? "true"
      : k === LEGACY_WIDTH_KEYS.sidebar ? "300"
      : k === LEGACY_WIDTH_KEYS.inspector ? "400"
      : null,
    );
    const root = asSplit(t);
    assert.deepEqual(comps(t), ["sidebar", "chat", "inspector"]);
    assert.equal(asPane(root.children[0]).fixed, 300);
    assert.equal(asPane(root.children[2]).fixed, 400);
  });
  it("sub-floor legacy width renders verbatim (open pane keeps its width)", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_OPEN_KEYS.sidebar ? "true" : k === LEGACY_WIDTH_KEYS.sidebar ? "150" : null,
    );
    assert.equal(asPane(asSplit(t).children[0]).fixed, 150);
  });
  it("closed panes drop out of the tree", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_OPEN_KEYS.sidebar ? "false" : k === LEGACY_OPEN_KEYS.inspector ? "false" : null,
    );
    assert.equal(t.kind, "pane"); // single child hoists out of the root split
    assert.deepEqual(asPane(t).tabs.map((x) => x.component), ["chat"]);
  });
  it("bad flag strings fall back to defaults", () => {
    const t = migrateLegacy((k) =>
      k === LEGACY_OPEN_KEYS.sidebar ? "banana"
      : k === LEGACY_OPEN_KEYS.inspector ? "maybe"
      : k === LEGACY_WIDTH_KEYS.sidebar ? "abc"
      : null,
    );
    // defaults: sidebar open (default true), inspector closed (default false)
    assert.deepEqual(comps(t), ["sidebar", "chat"]);
    assert.equal(asPane(asSplit(t).children[0]).fixed, 240);
  });
});

describe("dragSplitDivider", () => {
  // shell: row [sidebar(fixed 240), chat(flex), inspector(fixed 300)],
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
