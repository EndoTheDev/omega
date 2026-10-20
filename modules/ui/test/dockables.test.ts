// omega webui: unit tests for THE DOCKABLE CONTRACT (card t_30ee04fb) -
// the registry (dockables.ts), the singleton opener (openSingleton in the
// engine), the store-level semantics (has / openSingleton / the reset-era
// main-pane ops), and the composition-manifest lift (dockablesFromPanels).
// card t_4ae27e0d rewrote the identities: sidebar-pinned sidebars
// (unclosable), the closable main class, the fixed five-tab inspector.
// Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  COMPONENT_TITLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  isClosableTab,
  isPinnedTab,
  dockableOf,
  dockablesFromPanels,
  type DockableComponent,
} from "../app/composables/dockables.ts";
import { withScratchSingleton } from "./scratchDockable.ts";
import {
  DOCK_LAYOUT_KEY,
  createDock,
  focusTab,
  openTab,
  removePane,
  makePane,
  makeSplit,
  nextNodeId,
  openSingleton,
  serialize,
  splitPane,
  allNodes,
  firstMainPaneId,
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
/**
 * default shell shape (card t_4ae27e0d): row
 * [sidebar(240), chat(flex), the five-tab inspector pane(300)].
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

// microtask bridge: Vue's watcher scheduler flushes on a macrotask boundary
function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

// --- the registry itself ----------------------------------------------------

describe("the dockable registry", () => {
  it("covers the openable component universe 1:1 with unique ids", () => {
    const ids = DOCKABLES.map((d) => d.id);
    assert.equal(new Set(ids).size, ids.length);
    assert.deepEqual([...ids].sort(), [...DOCK_COMPONENTS].sort());
  });

  it("carries the contract fields: non-empty title, ink, one policy, an area", () => {
    for (const d of DOCKABLES) {
      assert.ok(d.title.length > 0, `${d.id}: title`);
      const hasInk =
        d.icon.rect !== undefined ||
        (d.icon.paths.length > 0 && d.icon.paths.every((p) => p.length > 0));
      assert.ok(hasInk, `${d.id}: icon has ink`);
      assert.ok(
        d.policy === "singleton" || d.policy === "multi",
        `${d.id}: policy is one of the two`,
      );
      assert.ok(
        d.area === "left" || d.area === "right" || d.area === "main",
        `${d.id}: area is one of the three`,
      );
      assert.equal(typeof d.closable, "boolean", `${d.id}: closable flag`);
      // the pinned identity is the whole invariant: pin <-> unclosable
      assert.equal(!isClosableTab(d.id), isPinnedTab(d.id), `${d.id}: pin/unclosable agree`);
    }
  });

  it("policy split (card t_4ae27e0d + t_49d0b308): chat the only multi AND the only closable", () => {
    const multi = DOCKABLES.filter((d) => d.policy === "multi").map((d) => d.id);
    assert.deepEqual(multi, ["chat"]);
    const closable = DOCKABLES.filter((d) => isClosableTab(d.id)).map((d) => d.id);
    assert.deepEqual([...closable].sort(), ["chat"]);
  });

  it("the fixed sidebars: sidebar pinned left, the inspector SEVEN pinned right (t_49d0b308)", () => {
    assert.deepEqual(
      DOCKABLES.filter((d) => d.area === "left").map((d) => d.id),
      ["sidebar"],
    );
    // the default order is the ruling's: files, cron, logs, skills,
    // tools, settings, kanban (logs = the http_channel module id)
    assert.deepEqual(
      DOCKABLES.filter((d) => d.area === "right").map((d) => d.id),
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
    );
    // the inspector tab inventory IS the right-area set, in one place
    assert.deepEqual(
      [...INSPECTOR_TAB_COMPONENTS],
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
    );
    assert.ok(
      INSPECTOR_TAB_COMPONENTS.every((c) => !isClosableTab(c)),
      "no X on any of the seven",
    );
  });

  it("settings has NO other entry point (t_49d0b308): the '+' inventory excludes it", () => {
    // settings is pinned right now - it can only be reached by its
    // inspector tab. MAIN_DOCKABLES (both '+' menus' inventory) holds
    // the main-area dockables only
    assert.ok(isPinnedTab("settings"), "settings is pinned chrome");
    assert.deepEqual([...MAIN_DOCKABLES], ["chat"]);
    assert.ok(!MAIN_DOCKABLES.includes("settings"));
    assert.ok(!MAIN_DOCKABLES.includes("kanban"));
  });

  it("legacy ids: pre-reset trees deserialize but nothing may open one", () => {
    assert.deepEqual([...LEGACY_DOCK_COMPONENTS], ["inspector"]);
    assert.ok(!(DOCK_COMPONENTS as readonly string[]).includes("inspector"));
  });

  it("settings + kanban are pinned: openSingleton refuses every open path", async () => {
    for (const id of ["settings", "kanban"] as const) {
      assert.equal(isPinnedTab(id), true, `${id} pinned`);
      const t: DockNode = chatOnlyTree();
      const res = openSingleton(t, panes(t)[0]!.id, id);
      assert.equal(res.action, "noop", `${id} opens nowhere`);
      assert.equal(res.tree, t);
    }
  });

  it("dockableOf resolves every openable component, throws outside the union", () => {
    for (const id of DOCK_COMPONENTS) {
      const d: DockableComponent = dockableOf(id);
      assert.equal(d.id, id);
    }
    assert.throws(() => dockableOf("nonsense" as never), /unknown component/);
  });

  it("derives the tab-title fallback and the main-area '+' inventory", () => {
    assert.deepEqual(
      Object.fromEntries(DOCKABLES.map((d) => [d.id, d.title])),
      { ...COMPONENT_TITLES },
    );
    // the pinned sidebars have no menu-open path (card t_4ae27e0d);
    // settings left the main class (card t_49d0b308) - chat only
    assert.deepEqual([...MAIN_DOCKABLES], ["chat"]);
    const ids = DOCKABLES.map((d) => d.id);
    assert.ok(MAIN_DOCKABLES.every((c) => ids.includes(c)));
  });

  it("chrome is NOT in the registry (MenuBar/StatusBar are the window, not content)", () => {
    const chrome = ["menubar", "statusbar", "menu", "status"];
    for (const d of DOCKABLES) assert.ok(!chrome.includes(d.id), d.id);
  });
});

// --- openSingleton (the engine half of the contract) ------------------------
// card t_49d0b308: no REAL component lives in the main-singleton class
// anymore (settings is pinned in the inspector now), so these tests run
// against the test-only scratch singleton (see scratchDockable.ts) - the
// contract text is unchanged, only the fixture.

describe("openSingleton", () => {
  it("creates in an EMPTY MAIN pane when no instance exists", async () => {
    await withScratchSingleton(async (scratch) => {
      const tree = chatOnlyTree();
      const base = splitPane(tree, panes(tree)[0]!.id, "row", "after");
      const empty = panes(base).find((p) => p.tabs.length === 0)!;
      const res = openSingleton(base, empty.id, scratch);
      assert.equal(res.action, "created");
      const pane = paneById(res.tree, empty.id);
      assert.deepEqual(pane.tabs.map((x) => x.component), [scratch]);
      assert.equal(pane.active, 0);
      assert.equal(instanceCount(res.tree, scratch), 1);
    });
  });

  it("appends + activates in an OCCUPIED main pane when no instance exists", async () => {
    await withScratchSingleton(async (scratch) => {
      const t = chatOnlyTree();
      const target = panes(t)[1]!;
      const res = openSingleton(t, target.id, scratch);
      assert.equal(res.action, "created");
      const pane = paneById(res.tree, target.id);
      assert.deepEqual(pane.tabs.map((x) => x.component), ["chat", scratch]);
      assert.equal(pane.active, 1, "the opened tab takes focus");
    });
  });

  it("FOCUSES the one instance instead of creating a second (another pane)", async () => {
    await withScratchSingleton(async (scratch) => {
      const t = defaultTree();
      const base = splitPane(t, panes(t)[1]!.id, "row", "after");
      const empty = panes(base).find((p) => p.tabs.length === 0)!;
      const withTab = openSingleton(base, empty.id, scratch);
      assert.equal(withTab.action, "created");
      const other = panes(withTab.tree).find((p) => p.id !== empty.id)!;
      const res = openSingleton(withTab.tree, other.id, scratch);
      assert.equal(res.action, "focused");
      assert.equal(instanceCount(res.tree, scratch), 1, "never a second singleton");
      assert.equal(
        paneById(res.tree, empty.id).tabs.length,
        1,
        "no structural change - only focus moved",
      );
    });
  });

  it("focuses the instance even while it sits as an INACTIVE tab", async () => {
    // the singleton parked inactive in a second tab of the first pane
    await withScratchSingleton(async (scratch) => {
    let t: DockNode = chatOnlyTree();
    const a = panes(t)[0]!;
    const b = panes(t)[1]!;
    t = openTab(t, a.id, { component: scratch });
    t = focusTab(t, a.id, 0); // scratch inactive in pane a
      const res = openSingleton(t, b.id, scratch);
      assert.equal(res.action, "focused");
      const pane = paneById(res.tree, a.id);
      assert.equal(instanceCount(res.tree, scratch), 1);
      assert.equal(pane.active, 1, "the parked singleton became active");
    });
  });

  it("re-opening in the same pane still lands on the instance (no duplicate)", async () => {
    await withScratchSingleton(async (scratch) => {
      let t: DockNode = chatOnlyTree();
      const paneA = panes(t)[0]!;
      const first = openSingleton(t, paneA.id, scratch);
      assert.equal(first.action, "created");
      t = first.tree;
      // scratch active in paneA; blur it by focusing chat, then re-open
      t = focusTab(t, paneA.id, 0);
      const res = openSingleton(t, paneA.id, scratch);
      assert.equal(res.action, "focused");
      assert.equal(instanceCount(res.tree, scratch), 1);
      assert.equal(paneById(res.tree, paneA.id).tabs.length, 2);
    });
  });

  it("no-ops when the target pane vanished (concurrent close)", async () => {
    await withScratchSingleton(async (scratch) => {
      const t = chatOnlyTree();
      const res = openSingleton(t, "n999z", scratch);
      assert.equal(res.action, "noop");
      assert.equal(res.tree, t, "the tree object is preserved untouched");
    });
  });

  it("refuses PINNED components and PINNED panes (no open path into the sidebars)", async () => {
    const t = defaultTree();
    for (const c of INSPECTOR_TAB_COMPONENTS) {
      const res = openSingleton(t, firstMainPaneId(t)!, c);
      assert.equal(res.action, "noop", `${c} opens nowhere`);
      assert.equal(res.tree, t);
    }
    await withScratchSingleton(async (scratch) => {
      const side = paneById(t, asSplit(t).children[0].id);
      const res = openSingleton(t, side.id, scratch);
      assert.equal(res.action, "noop", "a pinned target pane takes no tabs");
      assert.equal(res.tree, t);
    });
  });
});

// --- the store (createDock: the reset-era ops over a persisted tree) ---------

describe("dock store: the pane-model reset", () => {
  it("has() reads EVERY tab: a parked inactive singleton exists", async () => {
    await withScratchSingleton(async (scratch) => {
      let t: DockNode = chatOnlyTree();
      const a = panes(t)[0]!;
      t = openTab(t, a.id, { component: scratch });
      t = focusTab(t, a.id, 0);
      const mem = new MemoryStorage();
      mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
      const dock = createDock(mem as unknown as Storage);
      await flush();
      assert.equal(dock.has(scratch), true, "inactive singleton still exists");
      assert.equal(dock.has("chat"), true);
      assert.equal(dock.has("files"), true, "the pinned inspector edge came back");
      // active-only has() would under-report and duplicate singletons
    });
  });

  it("a loaded pre-reset tree modernizes: the inspector tab expands, settings+kanban join (t_49d0b308)", async () => {
    const legacy: DockNode = {
      kind: "split",
      id: "n1",
      direction: "row",
      children: [
        { kind: "pane", id: "n2", tabs: [{ component: "sidebar" }], active: 0, fixed: 240 },
        { kind: "pane", id: "n3", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "n4", tabs: [{ component: "inspector" }], active: 0, fixed: 300 },
      ],
      sizes: [1 / 3, 1 / 3, 1 / 3],
    };
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, JSON.stringify({ v: 1, tree: legacy }));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const insp = paneById(tree, "n4");
    assert.deepEqual(
      insp.tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
      "the chip strip's tabs are real now, and the two new views are in",
    );
    // persisted at once so the next boot never re-modernizes
    const stored = JSON.parse(mem.getItem(DOCK_LAYOUT_KEY) ?? "{}");
    assert.deepEqual(stored.tree, tree as unknown, "the modern tree persisted on boot");
  });

  it("a five-tab-era inspector gains settings + kanban in the canonical order (t_49d0b308)", async () => {
    const oldFive: DockNode = {
      kind: "split",
      id: "n1",
      direction: "row",
      children: [
        { kind: "pane", id: "n2", tabs: [{ component: "sidebar" }], active: 0, fixed: 240 },
        { kind: "pane", id: "n3", tabs: [{ component: "chat" }], active: 0, fixed: null },
        {
          kind: "pane",
          id: "n4",
          tabs: ["files", "cron", "http_channel", "skills", "tools"].map((c) => ({
            component: c as DockPane["tabs"][number]["component"],
          })),
          active: 2,
          fixed: 300,
        },
      ],
      sizes: [1 / 3, 1 / 3, 1 / 3],
    };
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(oldFive));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const insp = paneById(dock.tree.value, "n4");
    assert.deepEqual(
      insp.tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
    );
    assert.equal(insp.active, 2, "the user's active view stays put");
  });

  it("a pre-move settings tab parked in a MAIN pane is stripped on boot (t_49d0b308)", async () => {
    // the old shell allowed settings as a main-pane tab; build the tree
    // persisting EXACTLY that (openTab would refuse a pinned component
    // today, so the literal is constructed by hand)
    const t0: DockNode = {
      kind: "split",
      id: "n1",
      direction: "row",
      children: [
        { kind: "pane", id: "n2", tabs: [{ component: "sidebar" }], active: 0, fixed: 240 },
        {
          kind: "pane",
          id: "n3",
          tabs: [{ component: "chat" }, { component: "settings" }],
          active: 1,
          fixed: null,
        },
        {
          kind: "pane",
          id: "n4",
          tabs: ["files", "cron", "http_channel", "skills", "tools"].map((c) => ({
            component: c as DockPane["tabs"][number]["component"],
          })),
          active: 0,
          fixed: 300,
        },
      ],
      sizes: [1 / 3, 1 / 3, 1 / 3],
    };
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t0));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    // the chat pane is back to chat-only; the inspector pane holds the
    // ONE settings tab (pinned), in the canonical order
    const chatPane = paneById(tree, asSplit(tree).children[1].id);
    assert.deepEqual(
      chatPane.tabs.map((x) => x.component),
      ["chat"],
      "the stray settings tab left the main pane",
    );
    const insp = paneById(tree, asSplit(tree).children[2].id);
    assert.equal(
      insp.tabs.filter((t) => t.component === "settings").length,
      1,
      "exactly one settings tab, in the inspector",
    );
    assert.deepEqual(
      insp.tabs.map((x) => x.component),
      ["files", "cron", "http_channel", "skills", "tools", "settings", "kanban"],
    );
  });

  it("a tree with sidebars toggled away gets its fixed shape back", async () => {
    const t: DockNode = makePane("chat");
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    assert.equal(panes(tree).length, 3, "sidebar + chat + inspector");
    const comps = panes(tree).map((p) => p.tabs[0]?.component);
    assert.deepEqual(comps[0], "sidebar");
    assert.equal(panes(tree)[0]!.fixed, 240);
  });

  it("ensureMainPane returns the first main pane; never inserts when one lives", async () => {
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(defaultTree()));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const before = serialize(dock.tree.value);
    const chatPane = panes(dock.tree.value).find((p) => p.tabs[0]?.component === "chat")!;
    assert.equal(dock.ensureMainPane(), chatPane.id, "no pane inserted");
    assert.equal(serialize(dock.tree.value), before, "the tree is untouched");
  });

  it("ensureMainPane re-inserts a main pane when the user removed the last one", async () => {
    let t: DockNode = defaultTree();
    // strip both main candidates: the chat pane (pinned sidebars refuse,
    // so the tree keeps its shape) - only remove-able pane is the chat
    const chatPane = asSplit(t).children[1];
    t = removePane(t, asPane(chatPane).id);
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    assert.equal(panes(dock.tree.value).length, 2, "sidebars only");
    const id = dock.ensureMainPane();
    const tree = dock.tree.value;
    const pane = paneById(tree, id);
    assert.equal(pane.tabs.length, 0, "a fresh flexible pane");
    assert.equal(panes(tree).length, 3, "the dock has a main pane again");
    assert.equal(panes(tree)[2]!.id, id, "inserted at the root's after-edge");
  });

  it("canRemovePane: pinned never, the last remaining pane never", async () => {
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(defaultTree()));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const side = paneById(tree, asSplit(tree).children[0].id);
    const files = paneById(tree, asSplit(tree).children[2].id);
    const chatPane = paneById(tree, asSplit(tree).children[1].id);
    assert.equal(dock.canRemovePane(side.id), false, "sessions pane unclosable");
    assert.equal(dock.canRemovePane(files.id), false, "inspector pane unclosable");
    assert.equal(dock.canRemovePane(chatPane.id), true, "a main pane may go");
  });

  it("closing every main pane still leaves the dock alive (the pinned two)", async () => {
    const t: DockNode = makePane("chat");
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    // boot repair already has the fixed shape; dropping the chat pane
    // must leave the pinned two (the engine's never-empty guard is what
    // would refuse a zero-pane dock from any other call path)
    const chatPane = panes(dock.tree.value).find((p) => p.tabs[0]?.component === "chat")!;
    dock.remove(chatPane.id);
    await flush();
    const tree = dock.tree.value;
    assert.equal(panes(tree).length, 2, "the pinned sidebars hold the dock");
    assert.equal(dock.ensureMainPane().length > 0, true, "a main pane reopens");
  });

  it("closeTab through the store refuses the pinned sidebars' tabs", async () => {
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(defaultTree()));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const files = paneById(tree, asSplit(tree).children[2].id);
    dock.closeTab(files.id, 0);
    dock.closeTab(files.id, 1);
    await flush();
    assert.equal(
      paneById(dock.tree.value, files.id).tabs.map((x) => x.component as string).join(","),
      "files,cron,http_channel,skills,tools,settings,kanban",
      "none of the seven may close",
    );
    // the main class still closes through the store
    const chatPane = paneById(tree, asSplit(tree).children[1].id);
    dock.closeTab(chatPane.id, 0);
    await flush();
    assert.equal(paneById(dock.tree.value, chatPane.id).tabs.length, 0);
  });
});

// --- the composition-manifest lift ------------------------------------------

describe("dockablesFromPanels", () => {
  // "budget" plays the un-openable module id: kanban IS openable now
  // (card t_49d0b308 gave the board its inspector tab), so it can no
  // longer stand in for a module outside the shell inventory
  const MODULE = "budget";
  it("panels without a declaration stay out of the registry", () => {
    assert.deepEqual(dockablesFromPanels([{ module: MODULE, dockable: null }]), []);
    assert.deepEqual(dockablesFromPanels([{ module: MODULE }]), []);
  });

  it("lifts a declaration with registry defaults", () => {
    const [entry] = dockablesFromPanels([{ module: MODULE, dockable: {} }]);
    assert.ok(entry);
    assert.equal(entry.id, MODULE);
    assert.equal(entry.title, MODULE);
    assert.equal(entry.policy, "singleton");
    assert.ok(entry.icon.paths.length > 0 || entry.icon.rect !== undefined);
  });

  it("honors a full declaration", () => {
    const spec = {
      title: "board",
      icon: { paths: ["M4 4h16v16H4z"] },
      policy: "multi" as const,
    };
    const [entry] = dockablesFromPanels([{ module: MODULE, dockable: spec }]);
    assert.deepEqual(entry, { id: MODULE, title: "board", icon: spec.icon, policy: "multi" });
  });

  it("a module id colliding with the shell inventory is dropped, not shadowed", () => {
    const lifted = dockablesFromPanels([
      { module: "sidebar", dockable: { title: "impostor" } },
      { module: MODULE, dockable: {} },
    ]);
    assert.deepEqual(lifted.map((d) => d.id), [MODULE]);
  });
});