// omega webui: unit tests for the /board route's host-pane reclaim
// (card t_96844dcb, journey 98's CI red) - the pure side of the route:
// the host preference (a chat-hosting main pane first) and the foreground
// plan (none / focus / mint), applied the way app.vue's resolveBoardRoute
// applies it. The CI shape is recipe 96's persisted layout: the boot draft
// chat tab closed, the browser singleton left in the main pane's
// foreground - a /board reload must end with a chat tab ACTIVE in the
// host pane, the only state where DockPane renders the chat outlet the
// board folds into. Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  allNodes,
  chatForegroundPlan,
  firstChatMainPaneId,
  focusTab,
  makePane,
  makeSplit,
  openTab,
  type DockNode,
  type DockPane,
} from "../app/composables/dock.ts";

function panesOf(tree: DockNode): DockPane[] {
  return [...allNodes(tree)].filter((n) => n.kind === "pane") as DockPane[];
}

function mainPanes(tree: DockNode): DockPane[] {
  return panesOf(tree).filter((p) => p.edge === undefined);
}

function paneById(tree: DockNode, id: string): DockPane {
  const p = panesOf(tree).find((x) => x.id === id);
  if (!p) throw new Error(`pane ${id} missing`);
  return p;
}

/** the pinned edge shells, always present in a live tree */
function fixedTree(children: DockNode[]): DockNode {
  return makeSplit("row", [
    makePane("chat", 240, "left"),
    ...children,
    makePane("files", 300, "right"),
  ]);
}

// --- the CI shape: browser-only main pane -----------------------------------

describe("boardHost reclaim: the CI shape (t_96844dcb)", () => {
  it("a browser-only main pane plans a MINT and names no chat pane", () => {
    const tree = fixedTree([makePane("browser")]);
    assert.equal(firstChatMainPaneId(tree), null, "no chat-hosting main pane exists");
    const plan = chatForegroundPlan(tree, mainPanes(tree)[0]!.id);
    assert.equal(plan.mode, "mint");
  });

  it("the mint loop lands the chat foreground in the host pane", () => {
    let tree = fixedTree([makePane("browser")]);
    const paneId = firstChatMainPaneId(tree) ?? mainPanes(tree)[0]!.id;
    const plan = chatForegroundPlan(tree, paneId);
    if (plan.mode === "mint") {
      tree = openTab(tree, paneId, { component: "chat", key: "r1" });
    } else if (plan.mode === "focus") {
      tree = focusTab(tree, paneId, plan.index);
    }
    const after = paneById(tree, paneId);
    assert.equal(after.tabs[after.active]?.component, "chat");
  });

  it("a browser parked over an open chat plans a FOCUS at that tab", () => {
    let tree = fixedTree([makePane(null)]);
    const main = mainPanes(tree)[0]!;
    tree = openTab(tree, main.id, { component: "chat", key: "r1" });
    tree = openTab(tree, main.id, { component: "browser" });
    const before = paneById(tree, main.id);
    assert.equal(before.tabs[before.active]?.component, "browser");
    const plan = chatForegroundPlan(tree, main.id);
    assert.deepEqual(plan, { mode: "focus", index: 0 });
    const after = paneById(focusTab(tree, main.id, plan.index), main.id);
    assert.equal(after.tabs[after.active]?.component, "chat");
  });

  it("chat already in the foreground plans none (the reload stays put)", () => {
    let tree = fixedTree([makePane(null)]);
    const main = mainPanes(tree)[0]!;
    tree = openTab(tree, main.id, { component: "chat", key: "r1" });
    const after = paneById(tree, main.id);
    assert.equal(after.tabs[after.active]?.component, "chat");
    assert.deepEqual(chatForegroundPlan(tree, main.id), { mode: "none", index: 0 });
    // an inactive SECOND chat stays a no-op while chat is the foreground
    tree = openTab(tree, main.id, { component: "chat", key: "r2" });
    const again = paneById(tree, main.id);
    assert.equal(again.tabs[again.active]?.component, "chat");
    assert.deepEqual(chatForegroundPlan(tree, main.id), { mode: "none", index: 1 });
  });

  it("an unknown pane id is a no-op plan", () => {
    const tree = fixedTree([makePane("browser")]);
    assert.equal(chatForegroundPlan(tree, "nope").mode, "none");
  });

  it("the host scan prefers a chat-hosting pane over a browser-only first pane", () => {
    let tree = fixedTree([makePane(null), makePane(null)]);
    const [a, b] = mainPanes(tree);
    tree = openTab(tree, a!.id, { component: "browser" });
    tree = openTab(tree, b!.id, { component: "chat", key: "r1" });
    tree = openTab(tree, b!.id, { component: "browser" });
    const bAfter = paneById(tree, b!.id);
    assert.equal(bAfter.tabs[bAfter.active]?.component, "browser");
    const got = firstChatMainPaneId(tree);
    assert.notEqual(got, a!.id, "the browser-only pane must not host the route");
    assert.equal(got, b!.id, "the chat-hosting pane wins");
    assert.equal(chatForegroundPlan(tree, got!).mode, "focus", "b still plans a chat foreground");
  });

  it("pinned panes never host the route even when they carry a chat tab", () => {
    const tree = fixedTree([makePane("browser")]);
    assert.equal(firstChatMainPaneId(tree), null, "pinned chat stays out of the route's host scan");
    const main = mainPanes(tree)[0]!;
    assert.equal(chatForegroundPlan(tree, main.id).mode, "mint");
  });
});
