// omega webui: unit tests for THE DOCKABLE CONTRACT (card t_30ee04fb) -
// the registry (dockables.ts), the singleton opener (openSingleton in the
// engine), the store-level singleton semantics (has / openSingleton /
// toggleEdge / ensureEdge), and the composition-manifest lift
// (dockablesFromPanels). Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  COMPONENT_TITLES,
  PLUS_TAB_COMPONENTS,
  dockableOf,
  dockablesFromPanels,
  type DockableComponent,
} from "../app/composables/dockables.ts";
import {
  DOCK_LAYOUT_KEY,
  createDock,
  focusTab,
  makePane,
  makeSplit,
  moveTab,
  openSingleton,
  serialize,
  splitPane,
  allNodes,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import { PANE_OPEN_MIN } from "../app/composables/panes.ts";

// helpers ------------------------------------------------------------------

function asPane(n: DockNode): DockPane {
  assert.equal(n.kind, "pane");
  return n as DockPane;
}
function asSplit(n: DockNode): DockSplit {
  assert.equal(n.kind, "split");
  return n as DockSplit;
}
function panes(t: DockNode): DockPane[] {
  return [...allNodes(t)].filter((n) => n.kind === "pane") as DockPane[];
}
function paneById(t: DockNode, id: string): DockPane {
  const p = panes(t).find((x) => x.id === id);
  assert.ok(p, `expected pane ${id}`);
  return p!;
}
/** how many tabs in the whole tree host `component` - the max-instance read */
function instanceCount(t: DockNode, component: string): number {
  return panes(t).reduce(
    (n, p) => n + p.tabs.filter((tab) => tab.component === component).length,
    0,
  );
}
/** the default migrateLayout shape: row [sidebar(240), chat(flex), inspector(300)] */
function defaultTree(): DockSplit {
  return asSplit(makeSplit("row", [
    makePane("sidebar", 240),
    makePane("chat", null),
    makePane("inspector", 300),
  ]));
}
/** two chat panes - a tree with NO singleton instance in it */
function chatOnlyTree(): DockSplit {
  return asSplit(makeSplit("row", [makePane("chat", null), makePane("chat", null)]));
}

// minified in-memory Storage for the store tests
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

// --- the registry itself ----------------------------------------------------

describe("the dockable registry", () => {
  it("covers the openable component universe 1:1 with unique ids", () => {
    const ids = DOCKABLES.map((d) => d.id);
    assert.equal(new Set(ids).size, ids.length);
    assert.deepEqual([...ids].sort(), [...DOCK_COMPONENTS].sort());
  });

  it("carries the contract fields: non-empty lowercase title, ink, one policy", () => {
    for (const d of DOCKABLES) {
      assert.ok(d.title.length > 0, `${d.id}: title`);
      assert.equal(d.title, d.title.toLowerCase(), `${d.id}: shell voice is lowercase`);
      const hasInk =
        d.icon.rect !== undefined ||
        (d.icon.paths.length > 0 && d.icon.paths.every((p) => p.length > 0));
      assert.ok(hasInk, `${d.id}: icon has ink`);
      assert.ok(
        d.policy === "singleton" || d.policy === "multi",
        `${d.id}: policy is one of the two`,
      );
    }
  });

  it("policy split: sessions + inspector singleton, chat the only multi", () => {
    const singletons = DOCKABLES.filter((d) => d.policy === "singleton").map((d) => d.id);
    // the settled design names singleton: sessions, files/inspector, settings
    assert.deepEqual(singletons.sort(), ["inspector", "sidebar"]);
    const multi = DOCKABLES.filter((d) => d.policy === "multi").map((d) => d.id);
    assert.deepEqual(multi, ["chat"]);
  });

  it("dockableOf resolves every openable component, throws outside the union", () => {
    for (const id of DOCK_COMPONENTS) {
      const d: DockableComponent = dockableOf(id);
      assert.equal(d.id, id);
    }
    assert.throws(() => dockableOf("kanban" as never), /unknown component/);
  });

  it("derives the tab-title fallback and the '+' inventory from the registry", () => {
    assert.deepEqual(
      Object.fromEntries(DOCKABLES.map((d) => [d.id, d.title])),
      { ...COMPONENT_TITLES },
    );
    assert.deepEqual([...PLUS_TAB_COMPONENTS], DOCKABLES.map((d) => d.id));
  });

  it("chrome is NOT in the registry (MenuBar/StatusBar are the window, not content)", () => {
    const chrome = ["menubar", "statusbar", "menu", "status"];
    for (const d of DOCKABLES) assert.ok(!chrome.includes(d.id), d.id);
  });
});

// --- openSingleton (the engine half of the contract) ------------------------

describe("openSingleton", () => {
  it("creates in an EMPTY pane when no instance exists", () => {
    const tree = chatOnlyTree();
    const base = splitPane(tree, panes(tree)[0]!.id, "row", "after");
    const empty = panes(base).find((p) => p.tabs.length === 0)!;
    const res = openSingleton(base, empty.id, "sidebar");
    assert.equal(res.action, "created");
    const pane = paneById(res.tree, empty.id);
    assert.deepEqual(pane.tabs.map((x) => x.component), ["sidebar"]);
    assert.equal(pane.active, 0);
    assert.equal(instanceCount(res.tree, "sidebar"), 1);
  });

  it("appends + activates in an OCCUPIED pane when no instance exists", () => {
    const t = chatOnlyTree();
    const target = panes(t)[1]!;
    const res = openSingleton(t, target.id, "inspector");
    assert.equal(res.action, "created");
    const pane = paneById(res.tree, target.id);
    assert.deepEqual(pane.tabs.map((x) => x.component), ["chat", "inspector"]);
    assert.equal(pane.active, 1, "the opened tab takes focus");
  });

  it("FOCUSES the one instance instead of creating a second (another pane)", () => {
    const t = defaultTree();
    const base = splitPane(t, panes(t)[1]!.id, "row", "after");
    const empty = panes(base).find((p) => p.tabs.length === 0)!;
    const before = panes(base).map((p) => p.tabs.length).join(",");
    const res = openSingleton(base, empty.id, "sidebar");
    assert.equal(res.action, "focused");
    assert.equal(instanceCount(res.tree, "sidebar"), 1, "never a second singleton");
    assert.equal(paneById(res.tree, empty.id).tabs.length, 0, "the empty pane stays empty");
    assert.equal(
      panes(res.tree).map((p) => p.tabs.length).join(","),
      before,
      "no structural change - only focus moved",
    );
  });

  it("focuses the instance even while it sits as an INACTIVE tab", () => {
    // inspector moves into the chat pane and is parked inactive
    let t: DockNode = makeSplit("row", [makePane("chat", null), makePane("inspector", null)]);
    const insPane = panes(t).find((p) => p.tabs[0]?.component === "inspector")!;
    const chatPane = panes(t).find((p) => p.tabs[0]?.component === "chat")!;
    t = moveTab(t, insPane.id, 0, chatPane.id, 1);
    t = focusTab(t, chatPane.id, 0); // inspector inactive in the chat pane
    const other = panes(t).find((p) => p.id !== chatPane.id)!;
    const res = openSingleton(t, other.id, "inspector");
    assert.equal(res.action, "focused");
    const pane = paneById(res.tree, chatPane.id);
    assert.equal(instanceCount(res.tree, "inspector"), 1);
    assert.equal(pane.active, 1, "the parked inspector tab became active");
  });

  it("re-opening in the same pane still lands on the instance (no duplicate)", () => {
    let t: DockNode = chatOnlyTree();
    const paneA = panes(t)[0]!;
    const first = openSingleton(t, paneA.id, "inspector");
    assert.equal(first.action, "created");
    t = first.tree;
    // inspector active in paneA; blur it by focusing chat, then re-open
    t = focusTab(t, paneA.id, 0);
    const res = openSingleton(t, paneA.id, "inspector");
    assert.equal(res.action, "focused");
    assert.equal(instanceCount(res.tree, "inspector"), 1);
    assert.equal(paneById(res.tree, paneA.id).tabs.length, 2);
  });

  it("no-ops when the target pane vanished (concurrent close)", () => {
    const t = chatOnlyTree();
    const res = openSingleton(t, "nDoesNotExist", "sidebar");
    assert.equal(res.action, "noop");
    assert.equal(res.tree, t, "the tree object is preserved untouched");
  });
});

// --- the store half (createDock over the singleton semantics) ---------------

describe("dock store singleton semantics", () => {
  it("has() reads EVERY tab: a parked inactive singleton exists", async () => {
    let t: DockNode = makeSplit("row", [makePane("chat", null), makePane("inspector", null)]);
    const insPane = panes(t).find((p) => p.tabs[0]?.component === "inspector")!;
    const chatPane = panes(t).find((p) => p.tabs[0]?.component === "chat")!;
    t = focusTab(moveTab(t, insPane.id, 0, chatPane.id, 1), chatPane.id, 0);
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    assert.equal(dock.has("inspector"), true, "inactive singleton still exists");
    assert.equal(dock.has("sidebar"), false);
    assert.equal(dock.has("chat"), true);
    // the MenuBar-flag regression this guards: the old active-only has()
    // under-reported and let toggleEdge insert a second singleton
  });

  it("openSingleton focuses an existing instance across panes (store-level)", async () => {
    const t = defaultTree();
    const base = splitPane(t, panes(t)[1]!.id, "row", "after");
    const empty = panes(base).find((p) => p.tabs.length === 0)!;
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(base));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const verdict = dock.openSingleton(empty.id, "sidebar");
    assert.equal(verdict, "focused");
    assert.equal(instanceCount(dock.tree.value, "sidebar"), 1);
    assert.equal(paneById(dock.tree.value, empty.id).tabs.length, 0);
  });

  it("toggleEdge closes a shared-pane singleton as a TAB (the pane survives)", async () => {
    let t: DockNode = makeSplit("row", [
      makePane("sidebar", 200),
      makePane("chat", null),
      makePane("inspector", 300),
    ]);
    // park inspector as an inactive tab inside the sidebar pane
    const insPane = panes(t).find((p) => p.tabs[0]?.component === "inspector")!;
    const sbPane = panes(t).find((p) => p.tabs[0]?.component === "sidebar")!;
    t = focusTab(moveTab(t, insPane.id, 0, sbPane.id, 1), sbPane.id, 0);
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    dock.toggleEdge("inspector", "after");
    await flush();
    const sb = paneById(dock.tree.value, sbPane.id);
    assert.deepEqual(sb.tabs.map((x) => x.component), ["sidebar"], "only the tab was closed");
    assert.equal(dock.has("inspector"), false);
    assert.equal(instanceCount(dock.tree.value, "chat"), 1, "chat pane untouched");
  });

  it("toggleEdge removes a pane the singleton owns alone (split hoists)", async () => {
    const t = defaultTree();
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    dock.toggleEdge("sidebar", "before");
    await flush();
    const tree = dock.tree.value;
    assert.equal(dock.has("sidebar"), false);
    // the two-child row hoists to its survivor; no empty shells
    for (const n of allNodes(tree)) {
      if (n.kind === "split") assert.ok((n as DockSplit).children.length >= 2);
    }
    const comps = panes(tree).map((p) => p.tabs[p.active]?.component);
    assert.deepEqual(comps.sort(), ["chat", "inspector"]);
  });

  it("ensureEdge never duplicates a singleton parked anywhere", async () => {
    const t = defaultTree();
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    dock.ensureEdge("sidebar", "before");
    await flush();
    assert.equal(instanceCount(dock.tree.value, "sidebar"), 1);
    assert.equal(panes(dock.tree.value).length, 3, "the tree did not grow");
  });

  it("reopen-clamp: a stale sub-floor width reopens at " + PANE_OPEN_MIN, async () => {
    const t = asSplit(makeSplit("row", [makePane("chat", null), makePane("sidebar", 100)]));
    const sidebarPane = panes(t).find((p) => p.tabs[0]?.component === "sidebar")!;
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    dock.toggleEdge("sidebar", "before"); // close
    await flush();
    assert.equal(dock.has("sidebar"), false);
    dock.toggleEdge("sidebar", "before"); // reopen
    await flush();
    const pane = panes(dock.tree.value).find((p) => p.tabs[0]?.component === "sidebar")!;
    assert.equal(pane.fixed, PANE_OPEN_MIN, "the stale 100px width reopens at the clamp");
  });
});

// --- the composition-manifest lift ------------------------------------------

describe("dockablesFromPanels", () => {
  it("panels without a declaration stay out of the registry", () => {
    assert.deepEqual(dockablesFromPanels([{ module: "kanban", dockable: null }]), []);
    assert.deepEqual(dockablesFromPanels([{ module: "kanban" }]), []);
  });

  it("lifts a declaration with registry defaults", () => {
    const [entry] = dockablesFromPanels([{ module: "kanban", dockable: {} }]);
    assert.ok(entry);
    assert.equal(entry.id, "kanban");
    assert.equal(entry.title, "kanban");
    assert.equal(entry.policy, "singleton");
    assert.ok(entry.icon.paths.length > 0 || entry.icon.rect !== undefined);
  });

  it("honors a full declaration", () => {
    const spec = {
      title: "board",
      icon: { paths: ["M4 4h16v16H4z"] },
      policy: "multi" as const,
    };
    const [entry] = dockablesFromPanels([{ module: "kanban", dockable: spec }]);
    assert.deepEqual(entry, { id: "kanban", title: "board", icon: spec.icon, policy: "multi" });
  });

  it("a module id colliding with the shell inventory is dropped, not shadowed", () => {
    const lifted = dockablesFromPanels([
      { module: "sidebar", dockable: { title: "impostor" } },
      { module: "kanban", dockable: {} },
    ]);
    assert.deepEqual(lifted.map((d) => d.id), ["kanban"]);
  });
});

// microtask bridge: Vue's watcher scheduler flushes on a macrotask boundary
function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}