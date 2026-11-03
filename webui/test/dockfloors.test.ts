// omega webui: card t_ae9ac5cf Defect 1 - the honest floors. minDockPx
// used to floor EVERY flexible pane at the 320px chat wall, so five tool
// panes counted 1616px and refused sidebar reveals that would have fit
// (the "no room" toast on phantom room). The registry now carries a
// per-component minPx; a pane floors at the WORST tab it hosts, and
// contractFloorOf (the drag clamps) reads the same law as minDockPx (the
// reveal check) - "no room" fires exactly when the space is truly gone.
// Render-free; node --test.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { type DockComponentName } from "../app/composables/dockables.ts";
import {
  CHAT_FLOOR_PX,
  MIN_PANE_PX,
  contractFloorOf,
  dockableMinPx,
  dragSplitDivider,
  ensurePinnedEdges,
  findEdgePane,
  hideEdgePane,
  makePane,
  makeSplit,
  minDockPx,
  nextNodeId,
  paneFloorOf,
  revealEdgePane,
  splitPane,
  tryRevealEdgePane,
  allNodes,
  isPane,
  type DockPane,
  type DockNode,
  type DockSplit,
} from "../app/composables/dock.ts";

function asPane(n: DockNode): DockPane {
  assert.equal(n.kind, "pane");
  return n as DockPane;
}

describe("per-component floors (card t_ae9ac5cf Defect 1)", () => {
  it("the registry carries minPx: chat 320, the tool family 128", () => {
    assert.equal(dockableMinPx("chat"), CHAT_FLOOR_PX);
    assert.equal(dockableMinPx("files"), MIN_PANE_PX);
    assert.equal(dockableMinPx("mcp"), MIN_PANE_PX);
    // the lenient reader: a pre-reset legacy id counts as a tool tab
    assert.equal(dockableMinPx("inspector"), MIN_PANE_PX);
    assert.equal(dockableMinPx("<unknown>"), MIN_PANE_PX);
  });

  it("paneFloorOf: a tool-only pane floats at 128, a chat-hosting pane walls at 320, mixed = the worst tab", () => {
    assert.equal(paneFloorOf(makePane("files")), MIN_PANE_PX);
    assert.equal(paneFloorOf(makePane("mcp")), MIN_PANE_PX);
    assert.equal(paneFloorOf(makePane("chat")), CHAT_FLOOR_PX);
    const mixed = makePane("files");
    mixed.tabs = [{ component: "files" }, { component: "chat" }];
    assert.equal(paneFloorOf(mixed), CHAT_FLOOR_PX, "a pane that might show chat keeps chat room");
    // empty stays conservative: the next tenant may be the chat
    assert.equal(paneFloorOf(makePane(null)), CHAT_FLOOR_PX);
  });

  it("a pane's own contract lifts the floor above its tabs' floors", () => {
    const pane = makePane("files");
    pane.contract = { minPx: 260, maxPx: null, maxFrac: null, preferred: 0, anchor: "leading" };
    assert.equal(paneFloorOf(pane), 260, "the contract min wins over the tab floors");
    pane.tabs = [{ component: "files" }, { component: "chat" }];
    assert.equal(paneFloorOf(pane), CHAT_FLOOR_PX, "the chat tab still wins over a smaller contract");
  });

  it("minDockPx sums tool-only flexible panes at 128 each (the old law counted 320)", () => {
    const t: DockSplit = makeSplit("row", [
      makePane("files"),
      makePane("viewer"),
      makePane("mcp"),
      makePane("agents"),
      makePane("extensions"),
    ]);
    assert.equal(minDockPx(t), 5 * MIN_PANE_PX + 4 * 4, "5 tool panes + 4 dividers");
  });

  it("contractFloorOf reads the same law: fixed 128, flexible per tabs, a split still absorbs flat", () => {
    assert.equal(contractFloorOf(makePane("files", 250)), MIN_PANE_PX, "fixed panes keep the 128 drag floor");
    assert.equal(contractFloorOf(makePane("chat")), CHAT_FLOOR_PX);
    assert.equal(contractFloorOf(makePane("files")), MIN_PANE_PX, "a tool-only flexible pane drags to 128 now");
    // the split branch keeps the flat 320 absorb: minDockPx recursion there
    // would leak a row's HORIZONTAL sum into a vertical axis (the bottom
    // shell's column drag) - axis-aware split floors are future work
    const nested = makeSplit("row", [makePane("files"), makePane("mcp")]);
    assert.equal(contractFloorOf(nested), CHAT_FLOOR_PX, "a nested split absorbs like a flexible pane");
  });

  it("the drag honesty twin: a tool-only pane may park at 128 under a divider drag", () => {
    const inner = makeSplit("row", [makePane("files"), makePane("chat")]);
    const out = dragSplitDivider(inner, 1, [498, 498], -900, 996);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - MIN_PANE_PX) < 1e-6,
      `the files pane parked at ${out.split.sizes[0] * 996}, expected 128`);
    assert.ok(Math.abs(out.split.sizes[1] * 996 - (996 - MIN_PANE_PX)) < 1e-6, "the chat pane absorbs");
  });
});

// the END-TO-END reveal: the tool-heavy layout reveal now OPENS --------------

/** Endo's round-2 layout, test twin: both shells built + hidden, the main
    row grown to 5 flexible panes each hosting ONE TOOL tab (Endo's
    screenshot showed exactly this shape rendering at 128-250px). */
function fiveToolTree(): { tree: DockNode; mainPanes: DockPane[] } {
  let t: DockNode = ensurePinnedEdges(
    makeSplit("row", [
      makePane("sidebar", 240, "left"),
      makePane("chat", null),
      makePane("insights", 300),
    ]),
  );
  const chatId = (() => {
    for (const n of allNodes(t)) if (isPane(n) && (n as DockPane).fixed === null) return (n as DockPane).id;
    throw new Error("no flexible pane in the seed");
  })();
  for (let i = 0; i < 4; i++) t = splitPane(t, chatId, "row", "after");
  t = hideEdgePane(hideEdgePane(t, "inspector"), "sidebar");
  const tools: DockComponentName[] = ["files", "viewer", "mcp", "agents", "extensions"];
  const mainPanes: DockPane[] = [];
  for (const n of allNodes(t)) {
    if (isPane(n) && (n as DockPane).fixed === null) {
      const p = n as DockPane;
      p.tabs = [{ component: tools[mainPanes.length] as "files" }];
      p.active = 0;
      mainPanes.push(p);
    }
  }
  assert.equal(mainPanes.length, 5, "five tool panes of the seed");
  return { tree: t, mainPanes };
}

describe("the reveal with per-component floors (card t_ae9ac5cf Defect 1)", () => {
  it("minDockPx of the 5-tool layout is 656, not the phantom 1616", () => {
    const { tree } = fiveToolTree();
    assert.equal(minDockPx(tree), 5 * MIN_PANE_PX + 4 * 4);
  });

  it("five tool panes + the sidebar reveal FITS a tight window - no phantom 'no room'", () => {
    const { tree } = fiveToolTree();
    // the reveal needs its fixed 320 + one 4px divider on top of the tree
    const need = minDockPx(tree) + 320 + 4;
    const res = tryRevealEdgePane(tree, "sidebar", "before", 320, null, need);
    assert.equal(res.revealed, true, "the wall is the honest sum, the reveal proceeds");
    const pane = findEdgePane(res.tree, "sidebar");
    assert.ok(pane, "the sidebar seat exists");
    // sanity: the same reveal at room-minus-one refuses (the wall is honest)
    const tight = tryRevealEdgePane(tree, "sidebar", "before", 320, null, need - 1);
    assert.equal(tight.revealed, false, "one px short still refuses - the toast fires only when TRUE");
  });

  it("adding a chat tab to a tool pane floors THAT pane at 320 - the same reveal refuses", () => {
    const { tree, mainPanes } = fiveToolTree();
    mainPanes[0]!.tabs = [{ component: "files" }, { component: "chat" }];
    // one bumped pane: 4x128 + 320 + 16; the reveal (320 + divider) no
    // longer clears the wall
    const after = minDockPx(tree) + 320 + 4;
    assert.equal(after, 4 * MIN_PANE_PX + CHAT_FLOOR_PX + 4 * 4 + 320 + 4, "the chat tab cost exactly its wall");
    const res = tryRevealEdgePane(tree, "sidebar", "before", 320, null, after - 192);
    assert.equal(res.revealed, false, "the pane that might show chat kept the room - the reveal is refused");
    assert.strictEqual(res.tree, tree, "zero churn on the refusal");
  });

  it("the blind reveal (no measurement) is a pass-through, unchanged", () => {
    const { tree } = fiveToolTree();
    const out = revealEdgePane(tree, "sidebar", "before", 320);
    assert.notStrictEqual(out, tree);
  });
});