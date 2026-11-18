// omega webui: unit tests for the LOCAL split trees in the pinned roots (v0.14.4.
// Canvas C2's shape law: a sidebar STACKS (column splits) and never splits into
// side-by-side columns, main + bottom take both directions (drop position decides), the
// tight roots cap at two split levels, tabs stay free across roots, and the auto-merge
// unwinds a root whose leaves merge away - roots never die.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makePane,
  makeSplit,
  predictDockDrop,
  dockDropToZone,
  growRoot,
  removePane,
  closeTab,
  openTab,
  moveTab,
  forceSessionsLeft,
  rootPaneOf,
  splitDepthInRoot,
  splitLawRefusal,
  seatRefusal,
  paneZone,
  serialize,
  deserializeLayout,
  parseAnyLayout,
  treeShapesMatch,
  splitPane,
  findPane,
  isPane,
  isSplit,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

const asPane = (n: DockNode) => n as DockPane;
const asSplit = (n: DockNode) => n as DockSplit;

/** a classic single-row tree: left shell | A(chat) | B(agents) | right shell */
function rowRoot(): { root: DockSplit; left: DockPane; a: DockPane; b: DockPane; right: DockPane } {
  const left = makePane("sidebar", 240, "left");
  const right = makePane("files", 240, "right");
  const a = makePane("chat");
  const b = makePane("agents");
  const root = makeSplit("row", [left, a, b, right]);
  return { root, left, a, b, right };
}

/** the column-rooted tree: row[left, A(chat), right] over bottom(http) */
function bottomRoot(): { root: DockSplit; a: DockPane; bottom: DockPane } {
  const left = makePane("sidebar", 240, "left");
  const right = makePane("files", 240, "right");
  const a = makePane("chat");
  const row = makeSplit("row", [left, a, right]);
  const bottom = makePane("terminal", 200, "bottom");
  const root = makeSplit("column", [row, bottom]);
  return { root, a, bottom };
}

/** the id of the tree's chat-hosting non-edge pane */
function chatPaneId(tree: DockNode): string {
  for (const n of allNodesOf(tree)) {
    if (isPane(n) && !n.edge && n.tabs.some((t) => t.component === "chat")) return n.id;
  }
  throw new Error("no chat pane in fixture");
}
import { allNodes as allNodesOf } from "../app/composables/dock.ts";

function expectDrop(p: ReturnType<typeof predictDockDrop>, msg?: string) {
  if ("refused" in p) {
    throw new Error(`predicted a drop, got a refusal: ${p.reason}${msg ? ` (${msg})` : ""}`);
  }
  return p;
}
function expectRefusal(p: ReturnType<typeof predictDockDrop>): string {
  if (!("refused" in p)) throw new Error("predicted a drop, expected a refusal");
  return p.reason;
}
/** canonical structural signature. */
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

describe("growRoot (v0.14.4 ruling 1: the roots grow local trees)", () => {
  it("the sidebar grows a COLUMN with its content in the first leaf", () => {
    const { root, left } = rowRoot();
    const out = growRoot(root, left.id, "column", "after")!;
    const L = findPane(out.tree, left.id)!;
    assert.ok(L.grown, "the root grew");
    assert.equal(L.grown!.direction, "column");
    assert.equal(L.tabs.length, 0, "the classic bar emptied");
    const shellLeaf = L.grown!.children[0] as DockPane;
    assert.deepEqual(shellLeaf.tabs.map((t) => t.component), ["sidebar"], "content moved into the first leaf");
    assert.equal(shellLeaf.edge, undefined, "the leaf carries NO marker - identity stays with the root");
    assert.equal(L.edge, "left", "the root keeps its marker");
    assert.equal(L.fixed, 240, "the root keeps its fixed px");
  });

  it("the fresh leaf takes the zone's side; a grown shell never grows twice", () => {
    const { root, left } = rowRoot();
    const before = growRoot(root, left.id, "column", "before")!;
    const Lb = findPane(before.tree, left.id)!;
    const lead = Lb.grown!.children[0] as DockPane;
    assert.ok(isPane(lead) && lead.tabs.length === 0, "before: the fresh leaf leads");
    assert.deepEqual((Lb.grown!.children[1] as DockPane).tabs.map((t) => t.component), ["sidebar"]);
    assert.equal(growRoot(before.tree, left.id, "column", "after"), null, "grown shells refuse");
  });
});

describe("predictDockDrop over the roots (prediction == commit, ruling 5)", () => {
  it("bottom shell: all four zones grow, drop position decides the direction", () => {
    for (const zone of ["left", "right", "top", "bottom"] as const) {
      const { root, bottom } = bottomRoot();
      // canvas C1: the dragged view must be one the seat law lets into the
      // bottom pane - a sidebar view visits it (a chat tab never does), so
      // drag the right shell's files tab
      const right = [...allNodesOf(root)].find(
        (n) => isPane(n) && n.edge === "right",
      ) as DockPane;
      const src = right.id;
      const pred = expectDrop(predictDockDrop(root, src, 0, bottom.id, zone), zone);
      const committed = dockDropToZone(structuredClone(root), src, 0, bottom.id, zone);
      sameShape(pred.tree, committed);
      const B = findPane(pred.tree, bottom.id)!;
      assert.ok(B.grown, `${zone}: the bottom root grew`);
      assert.equal(asSplit(B.grown!).direction,
        zone === "left" || zone === "right" ? "row" : "column",
        `${zone}: the webui direction matches the zone's axis`);
    }
  });

  it("inner sidebar leaf: row zones refuse, the column split lands and equals the commit, depth caps at 2", () => {
    const { root, left, right } = rowRoot();
    const grown = growRoot(root, left.id, "column", "before")!;
    const L = findPane(grown.tree, left.id)!;
    // canvas C1: the dragged view must be one the class law ALLOWS in a sidebar (a.
    const src = findPane(grown.tree, right.id)!.id;
    const filesIdx = findPane(grown.tree, right.id)!.tabs.findIndex((t) => t.component === "files");
    const freshId = (L.grown!.children[0] as DockPane).id;
    // row zones refuse on a sidebar leaf (stacked rows ONLY)
    assert.equal(expectRefusal(predictDockDrop(grown.tree, src, filesIdx, freshId, "left")), "a sidebar stacks - it never splits into columns");
    assert.equal(dockDropToZone(grown.tree, src, filesIdx, freshId, "right"), grown.tree);
    const pred = expectDrop(predictDockDrop(grown.tree, src, filesIdx, freshId, "bottom"), "column split of the leaf");
    const commit = dockDropToZone(structuredClone(grown.tree), src, filesIdx, freshId, "bottom");
    sameShape(pred.tree, commit);
    const grownSplit = findPane(pred.tree, left.id)!.grown!;
    assert.equal(splitDepthInRoot(grownSplit, freshId), 2, "depth 2 reached");
    // the fresh leaf's split sibling also sits at depth 2.
    const inner = asSplit(grownSplit.children[0]);
    const siblingId = (inner.children[0] as DockPane).id === freshId
      ? (inner.children[1] as DockPane).id
      : (inner.children[0] as DockPane).id;
    // the capped attempt sources from a DIFFERENT pane (B's agents tab) -
    // the files pane IS one of the capped pair, and zones never self-dock
    for (const n of allNodesOf(pred.tree)) {
      if (isPane(n) && n.tabs.some((t) => t.component === "agents") && !n.edge) {
        const srcB = n.id;
        assert.equal(expectRefusal(predictDockDrop(pred.tree, srcB, 0, siblingId, "bottom")), "this root caps at two split levels");
        assert.equal(dockDropToZone(pred.tree, srcB, 0, siblingId, "bottom"), pred.tree, "a refused split commits nothing");
        break;
      }
    }
  });

  it("splitLawRefusal: sidebars column-only, bottom both, depth caps at 2 (main has no cap)", () => {
    const { root, left } = rowRoot();
    const grown = growRoot(root, left.id, "column", "before")!;
    const L = findPane(grown.tree, left.id)!;
    const freshId = (L.grown!.children[0] as DockPane).id;
    assert.equal(splitLawRefusal(grown.tree, freshId, "column"), null, "a sidebar leaf may stack further");
    assert.match(splitLawRefusal(grown.tree, freshId, "row") ?? "", /a sidebar stacks - it never splits into columns/);
    const deepLeaf = makePane("agents");
    L.grown = makeSplit("column", [
      makePane("agents"),
      makeSplit("column", [deepLeaf, makePane("files")]),
    ]);
    assert.match(splitLawRefusal(grown.tree, deepLeaf.id, "column") ?? "", /caps at two split levels/);
    const { root: broot, bottom } = bottomRoot();
    bottom.grown = makeSplit("column", [deepLeaf, makePane("agents")]);
    assert.equal(splitLawRefusal(broot, deepLeaf.id, "column"), null, "bottom: a depth-1 leaf splits fine");
    assert.equal(splitLawRefusal(broot, deepLeaf.id, "row"), null, "bottom: rows too");
    bottom.grown = makeSplit("column", [
      makePane("agents"),
      makeSplit("row", [deepLeaf, makePane("files")]),
    ]);
    assert.match(splitLawRefusal(broot, deepLeaf.id, "row") ?? "", /caps at two split levels/);
    const { root: r2, a } = rowRoot();
    let tree: DockNode = r2;
    for (let i = 0; i < 5; i++) {
      assert.equal(splitLawRefusal(tree, a.id, "row"), null, `main level ${i + 1} refuses nothing`);
      assert.notEqual(splitPane(tree, chatPaneId(tree), "row", "after"), tree);
    }
  });
});

describe("auto-merge (ruling 8: leaves merge away, roots never die)", () => {
  /** a bottom root with two leaves: shellLeaf(chat) | fresh(chat). */
  function grownBottom(): { root: DockSplit; bottom: DockPane; shellLeaf: DockPane; fresh: DockPane } {
    const left = makePane("sidebar", 240, "left");
    const right = makePane("files", 240, "right");
    const a = makePane("chat");
    const row = makeSplit("row", [left, a, right]);
    const shellLeaf = makePane("chat");
    const fresh = makePane("chat");
    const bottom = makePane("terminal", 200, "bottom");
    bottom.tabs = [];
    bottom.grown = makeSplit("row", [shellLeaf, fresh]);
    const root = makeSplit("column", [row, bottom]);
    return { root, bottom, shellLeaf, fresh };
  }

  it("closing the LAST tab of an inner leaf is LOCKED; the leaf leaves by its remove X", () => {
    const { root, bottom, shellLeaf, fresh } = grownBottom();
    // (THE LOCKED LAST TAB): the leaf's only tab refuses the close.
    assert.equal(closeTab(root, fresh.id, 0), root, "the last tab of a leaf never closes");
    assert.ok(rootPaneOf(root, fresh.id), "the leaf still lives in the root");
    // the leaf's own remove X is the route (canRemovePane is true for root leaves).
    const out = removePane(root, fresh.id);
    const B = findPane(out, bottom.id)!;
    assert.equal(B.grown, undefined, "the last leaf went: the root un-grew");
    assert.deepEqual(B.tabs.map((t) => t.component), ["chat"], "the survivor's classic content returned");
    assert.equal(rootPaneOf(out, shellLeaf.id), null, "nothing hosts the survivor anymore");
  });

  it("a leaf's tab WITH company still closes; the split keeps its survivors", () => {
    const { root, bottom, shellLeaf, fresh } = grownBottom();
    // three leaves, the middle one carrying two tabs.
    const l3 = makePane("agents");
    bottom.grown = makeSplit("row", [shellLeaf, fresh, l3]);
    const withTwo = openTab(root, fresh.id, { component: "chat" });
    const first = closeTab(withTwo, fresh.id, 0);
    const B = findPane(first, bottom.id)!;
    assert.ok(B.grown, "the root keeps its split");
    assert.deepEqual(B.tabs.map((t) => t.component), [], "the root's classic bar stays empty");
    assert.deepEqual(B.grown!.children.map((c) => asPane(c).tabs.map((t) => t.component)), [["chat"], ["chat"], ["agents"]],
      "every leaf remains; only the closed tab left");
  });

  it("moveTab drains an inner leaf: the root un-grows to the survivor", () => {
    const { root, bottom, shellLeaf, fresh } = grownBottom();
    const a = asPane(asSplit(root.children[0]).children[1]);
    const out = moveTab(root, fresh.id, 0, a.id, a.tabs.length);
    const B = findPane(out, bottom.id)!;
    assert.equal(B.grown, undefined, "the drained leaf merged away, the root un-grew");
    assert.deepEqual(B.tabs.map((t) => t.component), ["chat"]);
    void shellLeaf;
  });

  it("removePane of an inner leaf prunes within the root; a lone survivor un-grows", () => {
    const { root, bottom, fresh, shellLeaf } = grownBottom();
    const out = removePane(root, fresh.id);
    const B = findPane(out, bottom.id)!;
    assert.equal(rootPaneOf(out, shellLeaf.id), null, "the removal un-grew the root");
    assert.deepEqual(B.tabs.map((t) => t.component), ["chat"]);
    void bottom;
  });

  it("removing a MIDDLE leaf keeps the root grown with normalized sizes", () => {
    const { root, bottom, shellLeaf, fresh } = grownBottom();
    const l3 = makePane("agents");
    bottom.grown = makeSplit("row", [shellLeaf, fresh, l3]);
    const out = removePane(root, fresh.id);
    const B = findPane(out, bottom.id)!;
    assert.ok(B.grown, "one leaf removed among three: the root stays grown");
    assert.equal(B.grown!.children.length, 2);
    assert.deepEqual(B.grown!.sizes, [0.5, 0.5], "sizes normalized");
    assert.equal(rootPaneOf(out, shellLeaf.id)!.id, bottom.id, "the root still hosts its survivors");
  });

  it("the root itself never removes (pinned)", () => {
    const { root, bottom } = grownBottom();
    assert.equal(removePane(root, bottom.id), root);
  });
});

describe("the sessions seat with a grown left root (ruling 4: tabs free, bar = the root area)", () => {
  it("seatRefusal: the left root's leaves allow for sessions, everything else refuses", () => {
    const { root, left, a } = rowRoot();
    const grown = growRoot(root, left.id, "column", "before")!;
    const L = findPane(grown.tree, left.id)!;
    const freshId = (L.grown!.children[0] as DockPane).id;
    assert.equal(seatRefusal(grown.tree, "sidebar", left.id), null, "the left shell itself");
    assert.equal(seatRefusal(grown.tree, "sidebar", freshId), null, "a leaf inside the left root");
    assert.notEqual(seatRefusal(grown.tree, "sidebar", a.id), null, "a main pane refuses");
    assert.notEqual(
      seatRefusal(grown.tree, "sidebar", rowRightId(grown.tree)),
      null,
      "the right shell refuses",
    );
    // canvas C1: the zone read behind that refusal.
    assert.equal(paneZone(grown.tree, freshId), "left", "the grown leaf is the root's zone");
    assert.equal(paneZone(grown.tree, a.id), "main", "an unmarked pane is main");
    assert.equal(paneZone(grown.tree, left.id), "left", "the shell's own marker");
    assert.equal(
      paneZone(grown.tree, rowRightId(grown.tree)),
      "right",
      "the secondary shell's marker",
    );
  });

  it("forceSessionsLeft: sessions anywhere inside the root is met; absent = seeded at the first leaf", () => {
    const { root, left } = rowRoot();
    // growth moves the shell's classic content into the ZONE-SIDE leaf.
    const grown = growRoot(root, left.id, "column", "before")!;
    const L = findPane(grown.tree, left.id)!;
    assert.deepEqual((L.grown!.children[1] as DockPane).tabs.map((t) => t.component), ["sidebar"]);
    assert.equal(forceSessionsLeft(grown.tree), grown.tree, "it lives inside the root: met, no-op");
    // strip sessions from the whole root: the law seeds the FIRST leaf
    const stripped = growRoot(root, left.id, "column", "before")!;
    const Ls = findPane(stripped.tree, left.id)!;
    (Ls.grown!.children[1] as DockPane).tabs = [];
    const seeded = forceSessionsLeft(stripped.tree);
    const first = asPane(Ls.grown!.children[0]);
    const seededHost = findPane(seeded, first.id)!;
    assert.deepEqual(seededHost.tabs.map((t) => t.component), ["sidebar"], "seeded at the first leaf");
  });
});

function rowRightId(tree: DockNode): string {
  for (const n of allNodesOf(tree)) if (isPane(n) && n.edge === "right") return n.id;
  throw new Error("no right shell");
}

describe("persistence of the local trees (ruling 6: grown rides the v:3 record)", () => {
  it("serialize -> deserializeLayout round-trips a grown root", () => {
    const { root, left } = rowRoot();
    const grown = growRoot(root, left.id, "column", "before")!;
    const rec = serialize(grown.tree);
    const back = deserializeLayout(rec);
    assert.ok(back, "the record validates");
    assert.equal(back!.version, 4);
    assert.ok(treeShapesMatch(back!.tree, grown.tree), "shape == the live tree");
    const L = findPane(back!.tree, left.id)!;
    assert.ok(L.grown, "the local tree survived the record");
    assert.deepEqual((L.grown!.children[1] as DockPane).tabs.map((t) => t.component), ["sidebar"]);
  });

  it("parseAnyLayout repairs a corrupt grown field with a receipt (the shell un-grows)", () => {
    const { root, left } = rowRoot();
    const grown = growRoot(root, left.id, "column", "before")!;
    const rec = JSON.parse(serialize(grown.tree)) as { tree: Record<string, unknown> };
    const smash = (n: Record<string, unknown>): void => {
      if (n.grown !== undefined) {
        n.grown = { kind: "panel", children: 7 };
        return;
      }
      if (Array.isArray(n.children)) for (const c of n.children as Record<string, unknown>[]) smash(c);
    };
    smash(rec.tree);
    const parsed = parseAnyLayout(JSON.stringify(rec), () => root);
    const L = findPane(parsed.tree, left.id)!;
    assert.equal(L.grown, undefined, "the un-readable local tree dropped");
    assert.ok(parsed.repairs.some((r) => r.includes("un-grew")), "the receipt names the un-grow");
    assert.ok(parsed.repairs.length > 0, "a repair was receipted, not silent");
  });
});