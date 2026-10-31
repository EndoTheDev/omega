// omega webui: card t_ae9ac5cf Defect 2 - same-pane split drops. A pane's
// own half-zone drop now SPLITS that pane (the dragged tab seats in the
// fresh half, the remaining tabs stay - VS Code's editor-group edge drop);
// the center zone stays the self-refused fold, the shells keep their
// identity (grow, never split, row-zon refusal for sidebars), the depth
// caps hold. Render-free; node --test.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  dockDropToZone,
  findPane,
  allNodes,
  isPane,
  isSplit,
  makePane,
  makeSplit,
  paneCount,
  predictDockDrop,
  growRoot,
  splitDepthInRoot,
  type DockDropPrediction,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import type { DragZone } from "../app/composables/dockdnd.ts";

function paneOf(t: DockNode, id: string): DockPane {
  const p = findPane(t, id);
  assert.ok(p, `pane ${id} missing`);
  return p as DockPane;
}
function panes(t: DockNode): DockPane[] {
  return [...allNodes(t)].filter(isPane) as DockPane[];
}
function tabsOf(t: DockNode, id: string): string[] {
  return paneOf(t, id).tabs.map((x) => x.component);
}
function expectDrop(p: DockDropPrediction): { tree: DockNode; freshPaneId: string | null } {
  if ("refused" in p) throw new Error(`unexpected refusal: ${p.reason}`);
  return p;
}
function expectRefusal(p: DockDropPrediction): string {
  if (!("refused" in p)) throw new Error("unexpected success");
  return p.reason;
}

/** a two-pane main row: A hosts [files, chat] (the drag source pair), B files */
function row(): { root: DockSplit; a: DockPane; b: DockPane } {
  const a = makePane("files");
  a.tabs = [{ component: "files" }, { component: "chat" }];
  a.active = 1;
  const b = makePane("chat");
  return { root: makeSplit("row", [a, b]), a, b };
}

describe("same-pane split drops (card t_ae9ac5cf Defect 2)", () => {
  it("a main pane's own LEFT half: the pane splits, the dragged tab seats in the fresh half", () => {
    const { root, a } = row();
    const before = new Set(panes(root).map((p) => p.id));
    const pred = expectDrop(predictDockDrop(root, a.id, 1, a.id, "left"));
    assert.notEqual(pred.tree, root);
    assert.ok(pred.freshPaneId);
    const out = dockDropToZone(root, a.id, 1, a.id, "left");
    // the commit mints its own fresh id (insertPane mints) - find it by diff
    const fresh = panes(out).find((p) => !before.has(p.id))!;
    assert.ok(fresh, "the commit minted the fresh half");
    // the fresh half carries the dragged chat; A keeps the remaining files
    assert.deepEqual(tabsOf(out, fresh.id), ["chat"], "the dragged tab seats in the new half");
    assert.deepEqual(tabsOf(out, a.id), ["files"], "the remaining tabs stay in the original pane");
    // the split nested in the root row, fresh to the left of the survivor
    const pair = out.children[0] as DockSplit;
    assert.ok(isSplit(pair) && (pair as DockSplit).direction === "row");
    assert.equal((pair as DockSplit).children[0].id, fresh.id, "left zone: the fresh half sits BEFORE");
  });

  it("a main pane's own BOTTOM half: stacked split, same seat law", () => {
    const { root, a } = row();
    const out = dockDropToZone(root, a.id, 0, a.id, "bottom");
    assert.notEqual(out, root, "the split happened");
    const pair = out.children[0] as DockSplit;
    assert.equal(pair.direction, "column");
    // zone "bottom" = side AFTER: the fresh half rides BELOW the survivor
    assert.deepEqual(tabsOf(out, (pair.children[1] as DockPane).id), ["files"], "the dragged tab seats in the fresh half");
    assert.deepEqual(tabsOf(out, a.id), ["chat"], "chat stays with the original pane");
  });

  it("same-pane CENTER: no split, the tree is untouched (the fold stays)", () => {
    const { root, a } = row();
    assert.strictEqual(dockDropToZone(root, a.id, 0, a.id, "center"), root);
    assert.match(expectRefusal(predictDockDrop(root, a.id, 0, a.id, "center")), /self-dock|never/);
  });

  it("the source pane never lingers empty: a single-tab same-pane split prunes the emptied launcher", () => {
    // A with ONLY the chat tab: the move empties A - the last-tab law
    // closes it (B exists, it is not the last main pane)
    const a = makePane("chat");
    const b = makePane("files");
    const root = makeSplit("row", [a, b]);
    const out = dockDropToZone(root, a.id, 0, a.id, "left");
    assert.equal(paneCount(out), 2, "the emptied source closed with its move");
    assert.deepEqual(tabsOf(out, panes(out)[0]!.id), ["chat"], "the tab survives in the seat it landed");
    assert.deepEqual(tabsOf(out, b.id), ["files"]);
  });

  // --- the laws this card must NOT break ------------------------------------

  it("EDGE SHELLS: a same-pane row zone refuses (sidebars stack rows only); bottom GROWS and seats from shellLeaf", () => {
    const a = makePane("chat");
    const b = makePane("files", 240, "right");
    b.tabs = [{ component: "files" }, { component: "agents" }];
    const root = makeSplit("row", [a, b]);
    // row zones refuse on a sidebar root - same-pane or not
    assert.strictEqual(dockDropToZone(root, b.id, 0, b.id, "left"), root);
    assert.equal(expectRefusal(predictDockDrop(root, b.id, 0, b.id, "left")), "sidebars stack rows only");
    // the column zone grows the root: the shell's content lands in
    // shellLeaf, the DRAGGED tab (index 0, files) seats in the fresh leaf
    const grown = dockDropToZone(root, b.id, 0, b.id, "bottom");
    assert.notEqual(grown, root);
    const B = paneOf(grown, b.id);
    assert.ok(B.grown, "the right root grew a local split");
    assert.deepEqual(B.tabs, [], "the root's classic bar emptied");
    const shellLeaf = B.grown!.children[0] as DockPane;
    const freshLeaf = B.grown!.children[1] as DockPane;
    assert.deepEqual(shellLeaf.tabs.map((t) => t.component), ["agents"], "the REMAINING tab stays in shellLeaf");
    assert.deepEqual(freshLeaf.tabs.map((t) => t.component), ["files"], "the dragged tab seats in the fresh leaf (not lost in the wholesale move)");
    assert.notEqual(freshLeaf.id, b.id, "the seat is the fresh leaf, identity intact");
  });

  it("a depth-capped leaf refuses the same-pane split cleanly (cap extends over the self path)", () => {
    const left = makePane("sidebar", 240, "left");
    const main = makePane("chat");
    const root = makeSplit("row", [left, main]);
    const g1 = growRoot(root, left.id, "column", "before")!;
    assert.ok(g1, "the left root grew (column on a sidebar is legal)");
    const L = findPane(g1.tree, left.id)! as DockPane;
    // hand-build the depth-2 shape (deepLeaf sits two levels down)
    const deepLeaf = makePane("agents");
    L.grown = makeSplit("column", [
      makePane("agents"),
      makeSplit("column", [deepLeaf, makePane("files")]),
    ]);
    // the DRAGGED TAB for the same-pane attempt must come from the depth-2
    // leaf itself: one tab (agents), drop on its own bottom half
    assert.equal(splitDepthInRoot(L.grown, deepLeaf.id), 2);
    assert.equal(expectRefusal(predictDockDrop(g1.tree, deepLeaf.id, 0, deepLeaf.id, "bottom")), "this root caps at two split levels");
    assert.strictEqual(dockDropToZone(g1.tree, deepLeaf.id, 0, deepLeaf.id, "bottom"), g1.tree, "a refused split commits nothing");
    // the depth-1 sibling still splits (same-pane, legal direction)
    assert.equal(splitDepthInRoot(L.grown, (L.grown!.children[0] as DockPane).id), 1);
    assert.notEqual(dockDropToZone(g1.tree, (L.grown!.children[0] as DockPane).id, 0, (L.grown!.children[0] as DockPane).id, "bottom"), g1.tree, "depth 1 splits");
  });

  it("zones self-drop law summary: every split zone seats somewhere, only center folds", () => {
    const zones: DragZone[] = ["left", "right", "top", "bottom"];
    for (const zone of zones) {
      const { root, a } = row();
      const pred = expectDrop(predictDockDrop(root, a.id, 1, a.id, zone));
      assert.ok(pred.freshPaneId, `${zone}: the fresh half exists`);
      assert.notEqual(pred.freshPaneId, a.id, `${zone}: the seat is a NEW pane`);
    }
  });
});