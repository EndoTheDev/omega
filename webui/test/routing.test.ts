// omega webui: THE ROUTING LAW (canvas C1) - a session opens in the ACTIVE
// center chat pane, falling back to the main column, and a SIDEBAR never
// hosts one. The store-side half is pinned here: the resolver the app's
// openChatTab calls (sessionPane) and the op-layer refusal that makes the
// violation unrepresentable (openTab/setPaneComponent read the seat law).
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  DOCK_LAYOUT_KEY,
  allNodes,
  createDock,
  findEdgePane,
  findPane,
  isPinnedPane,
  makePane,
  makeSplit,
  migrateLegacy,
  openTab,
  serialize,
  setPaneComponent,
  type DockNode,
  type DockPane,
} from "../app/composables/dock.ts";
import { seatAllowsIn, zonesOf } from "../app/composables/dockables.ts";

/** a minimal localStorage stand-in (the store only reads/writes strings) */
class MemoryStorage {
  private map = new Map<string, string>();
  getItem(k: string): string | null {
    return this.map.get(k) ?? null;
  }
  setItem(k: string, v: string): void {
    this.map.set(k, v);
  }
  removeItem(k: string): void {
    this.map.delete(k);
  }
  clear(): void {
    this.map.clear();
  }
  key(): string | null {
    return null;
  }
  get length(): number {
    return this.map.size;
  }
}

const flush = () => new Promise((r) => setTimeout(r, 0));

/** the first MAIN pane hosting a chat tab (the active center chat pane) */
function chatMainPaneId(tree: DockNode): string | null {
  for (const n of allNodes(tree)) {
    if (n.kind !== "pane" || isPinnedPane(n)) continue;
    if (n.tabs.some((t) => t.component === "chat")) return n.id;
  }
  return null;
}

describe("routing: a session only ever opens in the main column (canvas C1)", () => {
  it("sessionPane: a sidebar that ASKED is routed to the main column", async () => {
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, serialize(migrateLegacy(() => null)));
    const dock = createDock(store as unknown as Storage);
    await flush();
    const left = findEdgePane(dock.tree.value, "sidebar")!;
    assert.ok(left, "the boot seats the primary sidebar");

    // the sidebar's own '+' asks for a chat tab: the resolver answers a MAIN
    // pane, never the sidebar (the root cause this law kills)
    const routed = dock.sessionPane(left.id);
    assert.notEqual(routed, left.id, "a sidebar never hosts a session");
    const target = findPane(dock.tree.value, routed);
    assert.ok(target, "the routed pane exists");
    assert.equal(isPinnedPane(target!), false, "the target is a main pane");
    assert.equal(target!.edge ?? null, null, "no edge marker: the main column");

    // a MAIN pane that asked is honored (the active center chat pane)
    const mainPane = chatMainPaneId(dock.tree.value);
    assert.ok(mainPane, "the fresh tree seats a chat in the main column");
    assert.equal(dock.sessionPane(mainPane!), mainPane, "the asking main pane is honored");

    // no preference: the resolver still answers a main pane
    const fallback = dock.sessionPane(null);
    assert.equal(isPinnedPane(findPane(dock.tree.value, fallback)!), false);
  });

  it("openTab/setPaneComponent refuse a chat in a sidebar - the violation is unrepresentable", () => {
    const shell = makeSplit("row", [
      makePane("sidebar", 240, "left"),
      makePane("chat"),
      makePane("files", 240, "right"),
    ]);
    const left = shell.children[0] as DockPane;
    const right = shell.children[2] as DockPane;
    // the op layer reads the seat law, so no caller can seat a session in a
    // sidebar - not the '+' path, not a refill, not a drag
    assert.equal(openTab(shell, left.id, { component: "chat" }), shell);
    assert.equal(openTab(shell, right.id, { component: "chat" }), shell);
    assert.equal(setPaneComponent(shell, left.id, "chat"), shell);
    // the legal side: a sidebar view refills an empty sidebar
    const emptySide = makePane(null, 240, "left");
    const tree = makeSplit("row", [emptySide, makePane("chat")]);
    assert.notEqual(setPaneComponent(tree, emptySide.id, "files"), tree);
  });
});

describe("the seat map is the dnd legality check (canvas C1)", () => {
  it("a main member cannot be dropped into any other zone", () => {
    for (const id of ["chat", "viewer", "browser", "settings"] as const) {
      assert.equal(seatAllowsIn(id, "main"), true, `${id} keeps its home`);
      assert.equal(seatAllowsIn(id, "left"), false, `${id} never enters the primary sidebar`);
      assert.equal(seatAllowsIn(id, "right"), false, `${id} never enters the secondary sidebar`);
      assert.equal(seatAllowsIn(id, "bottom"), false, `${id} never enters the bottom pane`);
    }
    // a bottom member never enters the main pane; it visits both sidebars
    for (const id of ["terminal", "observability"] as const) {
      assert.equal(seatAllowsIn(id, "main"), false, `${id} never enters the main pane`);
      assert.equal(seatAllowsIn(id, "left"), true, `${id} visits the primary sidebar`);
      assert.equal(seatAllowsIn(id, "right"), true, `${id} visits the secondary sidebar`);
    }
    // chat is main-only; the sessions list is primary-only (its standing lock)
    assert.deepEqual([...zonesOf("chat")], ["main"]);
    assert.deepEqual([...zonesOf("sidebar")], ["left"]);
  });
});
