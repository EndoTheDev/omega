// omega webui: card t_8bcbe72e - two wall-law follow-ups from the same wave.
// D1: the both-flexible divider branch floors the LEFT pane at ITS contract
// floor (320 for a flexible chat pane), symmetric with the right side - the
// drag may never park a flexible pane at 128. D2: a reveal the window cannot
// seat refuses cleanly BEFORE any state mutates (same tree back, no churn,
// no clean-up for the watcher's give-way to race). Render-free; node --test.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { type DockComponentName } from "../app/composables/dockables.ts";
import {
  createDock,
  DOCK_LAYOUT_KEY,
  dragSplitDivider,
  ensurePinnedEdges,
  findComponentTab,
  findEdgePane,
  findPane,
  hideEdgePane,
  makePane,
  makeSplit,
  minDockPx,
  nextNodeId,
  revealEdgePane,
  splitPane,
  tryRevealEdgePane,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

// helpers ------------------------------------------------------------------

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

function asPane(n: DockNode): DockPane {
  assert.equal(n.kind, "pane");
  return n as DockPane;
}
function asSplit(n: DockNode): DockSplit {
  assert.equal(n.kind, "split");
  return n as DockSplit;
}

/** the default boot shape's test twin: row [sidebar(240), chat, inspector
    (300)] through ensurePinnedEdges so the edge markers ride on it. */
function shellTree(): DockSplit {
  const inspector: DockPane = {
    kind: "pane",
    id: nextNodeId(),
    tabs: [{ component: "insights" }],
    active: 0,
    fixed: 300,
  };
  return asSplit(
    ensurePinnedEdges(
      makeSplit("row", [makePane("sidebar", 240, "left"), makePane("chat", null), inspector]),
    ),
  );
}

function paneByComponent(t: DockNode, c: DockComponentName): DockPane {
  const loc = findComponentTab(t, c);
  assert.ok(loc, `expected a pane hosting ${c}`);
  return findPane(t, loc!.paneId)!;
}

/** the flexPair the dock.test.ts both-flexible pins use: row [chat(flex),
    empty(flex)], fractions .5/.5, rendered 498/498 in a 996px content box. */
function flexPair(): DockSplit {
  const base = shellTree();
  return asSplit(splitPane(base, paneByComponent(base, "chat").id, "row", "after").children[1]);
}

/** Endo's edge case (screenshot 2): both shells closed, the main root grown
    to 5 inner flexible panes. minDockPx = 5x320 + 4 dividers = 1616 - the
    window seats THAT and nothing more. */
function grownRoot(): { tree: DockNode; contentPx: number } {
  let t: DockNode = hideEdgePane(hideEdgePane(ensurePinnedEdges(shellTree()), "inspector"), "sidebar");
  for (let i = 0; i < 4; i++) {
    const chat = paneByComponent(t, "chat");
    t = splitPane(t, chat.id, "row", "after");
  }
  const contentPx = minDockPx(t);
  assert.equal(contentPx, 5 * 320 + 4 * 4, "5 flexible panes + 4 dividers");
  return { tree: t, contentPx };
}

// D1: the left pane's floor in the both-flexible branch -----------------------

describe("both-flexible divider: the left pane floors at ITS contract floor (card t_8bcbe72e)", () => {
  it("a flexible chat pane on the LEFT stops at 320, never 128", () => {
    const inner = flexPair();
    // drag the divider far left: 498 - 900 << 128 - the clamp is 320
    const out = dragSplitDivider(inner, 1, [498, 498], -900, 996);
    assert.ok(out.split.sizes[0] * 996 >= 320 - 0.001, `left got ${out.split.sizes[0] * 996}`);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 320) < 1e-6);
    assert.ok(Math.abs(out.split.sizes[1] * 996 - 676) < 1e-6, "the right pane absorbs");
    assert.equal(out.collapsedPaneId, null);
  });
  it("a moderate sub-floor drag parks at 320 too (the t_f7201e02 tracking case)", () => {
    const inner = flexPair();
    // 498 - 300 = 198: the old 128 floor would have parked here
    const out = dragSplitDivider(inner, 1, [498, 498], -300, 996);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 320) < 1e-6);
  });
  it("the mirror case (drag right) is unchanged: the right keeps its 320", () => {
    const inner = flexPair();
    const out = dragSplitDivider(inner, 1, [498, 498], 900, 996);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 676) < 1e-9);
    assert.ok(out.split.sizes[1] * 996 >= 320 - 0.001);
  });
  it("a nested split on the left absorbs at 320 (contractFloorOf's split branch)", () => {
    const nested = makeSplit("row", [makePane("chat", null), makePane(null)]);
    const inner = makeSplit("row", [nested, makePane(null)]);
    const out = dragSplitDivider(inner, 1, [498, 498], -900, 996);
    assert.ok(Math.abs(out.split.sizes[0] * 996 - 320) < 1e-6);
    // the drag remaps only the outer split's pair; the nested split's own
    // fractions are byte-identical
    const outNested = asSplit(out.split.children[0]);
    assert.deepEqual(outNested.sizes, nested.sizes);
  });
});

// D2: the reveal refuses when the result cannot fit ----------------------------

describe("revealEdgePane fit-check (card t_8bcbe72e)", () => {
  it("refuses when minDockPx(after) > contentPx: the SAME tree comes back", () => {
    const { tree, contentPx } = grownRoot();
    const out = revealEdgePane(tree, "inspector", "after", null, null, contentPx);
    assert.strictEqual(out, tree, "no insert - the refusal returns the caller's tree");
    const res = tryRevealEdgePane(tree, "inspector", "after", null, null, contentPx);
    assert.strictEqual(res.tree, tree);
    assert.equal(res.revealed, false);
    assert.equal(findEdgePane(tree, "inspector"), null, "nothing appeared");
  });
  it("refuses with a stowed pane too (the t_9ed6cad7 restore path stays clean)", () => {
    const { tree, contentPx } = grownRoot();
    const stow: DockPane = {
      kind: "pane",
      id: nextNodeId(),
      tabs: [{ component: "insights" }],
      active: 0,
      fixed: 360,
      edge: "right",
    };
    const out = revealEdgePane(tree, "inspector", "after", null, stow, contentPx);
    assert.strictEqual(out, tree);
  });
  it("boundary: minDockPx(after) == contentPx fits (the wall is <=, not <)", () => {
    const { tree, contentPx } = grownRoot();
    // the revealed pane (360 fixed) + one 4px divider exactly fills the budget
    const res = tryRevealEdgePane(tree, "inspector", "after", 360, null, contentPx + 4 + 360);
    assert.equal(res.revealed, true);
    const pane = findEdgePane(res.tree, "inspector")!;
    assert.equal(pane.fixed, 360);
    assert.equal(pane.edge, "right");
    assert.notStrictEqual(res.tree, tree);
  });
  it("reveal with room available inserts normally", () => {
    const { tree, contentPx } = grownRoot();
    const res = tryRevealEdgePane(tree, "inspector", "after", 360, null, 4000);
    assert.equal(res.revealed, true);
    assert.equal(findEdgePane(res.tree, "inspector")!.fixed, 360);
    // and the same call WITHOUT a measurement (contentPx omitted) behaves
    // as today - the null guard is a pass-through
    const blind = revealEdgePane(tree, "inspector", "after", 360);
    assert.notStrictEqual(blind, tree);
  });
  it("reveals when the OTHER root edge's give-away CAN seat it (the t_66fa358e seam)", () => {
    // [sidebar(240), chat] at 800px: the inspector reveal (360 + divider)
    // only fits after the sidebar gives way - last-requested wins, so the
    // reveal proceeds and the watcher's squeeze executes exactly this
    let t: DockNode = hideEdgePane(ensurePinnedEdges(shellTree()), "inspector");
    const res = tryRevealEdgePane(t, "inspector", "after", 360, null, 800);
    assert.equal(res.revealed, true);
    assert.notStrictEqual(res.tree, t);
    assert.equal(findEdgePane(res.tree, "inspector")!.fixed, 360);
  });
});

// the store-level toggle: refusal before ANY state mutates --------------------

describe("toggleEdge refusal - zero churn (card t_8bcbe72e)", () => {
  it("the toggle no-ops under overflow: no tree change, no record write, no hiddenEdges mutation", async () => {
    const mem = new MemoryStorage();
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const grown = grownRoot().tree;
    dock.tree.value = grown;
    await flush();
    const before = mem.getItem(DOCK_LAYOUT_KEY);
    // the measurement lands (fits exactly - no give-way); the toggle reads it
    dock.narrowSqueeze(minDockPx(dock.tree.value));
    dock.toggleEdge("inspector", "after");
    await flush();
    assert.equal(dock.revealRefused.value, "inspector", "the refusal is surfaced");
    assert.strictEqual(dock.tree.value, grown, "the tree reference never changed");
    assert.equal(findEdgePane(dock.tree.value, "inspector"), null);
    assert.equal(mem.getItem(DOCK_LAYOUT_KEY), before, "the persistence record is untouched");
    // room arrives: the SAME toggle now opens cleanly
    dock.narrowSqueeze(minDockPx(dock.tree.value) + 800);
    dock.toggleEdge("inspector", "after");
    await flush();
    const pane = findEdgePane(dock.tree.value, "inspector");
    assert.ok(pane, "the same toggle opens once room exists");
    assert.notEqual(mem.getItem(DOCK_LAYOUT_KEY), before, "the successful reveal persists");
  });
});