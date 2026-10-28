// omega webui: unit tests for THE DOCKABLE CONTRACT (card t_30ee04fb) -
// the registry (dockables.ts), the singleton opener (openSingleton in the
// engine), the store-level semantics (has / openSingleton / the reset-era
// main-pane ops), and the composition-manifest lift (dockablesFromPanels).
// card t_4ae27e0d rewrote the identities: sidebar-pinned sidebars
// (unclosable), the closable main class, the fixed five-tab inspector.
// Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  COMPONENT_TITLES,
  INSPECTOR_TAB_COMPONENTS,
  LEGACY_DOCK_COMPONENTS,
  MAIN_DOCKABLES,
  MENU_DOCKABLES,
  isClosableTab,
  isLockedTab,
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
  makePane,
  makeSplit,
  nextNodeId,
  openSingleton,
  moveTab,
  serialize,
  deserializeLayout,
  treeShapesMatch,
  splitPane,
  allNodes,
  firstMainPaneId,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

// helpers ------------------------------------------------------------------

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
/** card t_67fc1905: the boot grafts the bottom panel - a createDock'd tree's
    root is a column now; the main row is its FIRST child. (Pure helpers
    like defaultTree below still BUILD row trees for the engine tests -
    the graft happens in the loader.) */
function mainRowOf(t: DockNode): DockSplit {
  const root = asSplit(t);
  assert.equal(root.direction, "column", "a dock store tree's root is a column (bottom-panel era)");
  return asSplit(root.children[0]);
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
    edge: "right",
  };
  return asSplit(
    makeSplit("row", [
      makePane("sidebar", 240, "left"),
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

  it("tab labels ride the Title Case siblings (card t_556ee727 + t_210a0d19 rename)", () => {
    // every shell tab renders Title Case today: Files, Settings,
    // Insights, Agents, and (card t_210a0d19's rename law) Modules -
    // the extensions inventory tab's label is "Modules" while its dock
    // id stays "extensions" (stored layouts persist ids). The module
    // tabs override via dockable.json; `agents`/`extensions` are the
    // only registry titles the tab bar prints raw. Display labels
    // ONLY - ids, routes and module names stay lowercase.
    assert.equal(COMPONENT_TITLES.agents, "Agents");
    assert.equal(COMPONENT_TITLES.extensions, "Modules");
    for (const d of DOCKABLES) {
      if (d.id === "agents" || d.id === "extensions") continue;
      assert.ok(
        d.title === "Sessions" && d.id === "sidebar" ||
          d.title === "Files" && d.id === "files" ||
          d.title === "Continuous" && d.id === "continuous" ||
          d.title === "Settings" && d.id === "settings" ||
          d.title === "Insights" && d.id === "insights" ||
          d.title === d.id,
        `${d.id}: unexpected label "${d.title}"`,
      );
    }
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
        d.area === "left" || d.area === "right" || d.area === "main" || d.area === "bottom",
        `${d.id}: area is one of the four`,
      );
      assert.equal(typeof d.closable, "boolean", `${d.id}: closable flag`);
      // the lock is narrow now (t_d6b657a7): only sessions is locked;
      // unclosable is the wider property - chat is the only closable
      // component left (card t_d7fd3ad3: insights' home moved to the
      // right shell; unclosable like every inspector view)
      assert.equal(isLockedTab(d.id), d.id === 'sidebar', `${d.id}: lock == sessions`);
      assert.equal(isClosableTab(d.id), d.id === "chat", `${d.id}: closable == chat`);
    }
  });

  it("policy split (card t_4ae27e0d + t_49d0b308 + t_d7fd3ad3): chat the only multi; chat the only closable", () => {
    const multi = DOCKABLES.filter((d) => d.policy === "multi").map((d) => d.id);
    assert.deepEqual(multi, ["chat"]);
    const closable = DOCKABLES.filter((d) => isClosableTab(d.id)).map((d) => d.id);
    assert.deepEqual([...closable].sort(), ["chat"]);
  });

  it("the fixed sidebars: sidebar pinned left, the inspector TEN pinned right (t_49d0b308 + t_14f73742 + t_d7fd3ad3 + arc 3c canvas)", () => {
    assert.deepEqual(
      DOCKABLES.filter((d) => d.area === "left").map((d) => d.id),
      ["sidebar"],
    );
    // the default order is the ruling's: files, cron, logs, skills,
    // tools, settings, kanban, extensions, insights (logs = the
    // http_channel module id; extensions joined last at t_14f73742;
    // insights joined the right shell at t_d7fd3ad3)
    assert.deepEqual(
      DOCKABLES.filter((d) => d.area === "right").map((d) => d.id),
      ["files", "cron", "skills", "tools", "agents", "continuous", "settings", "kanban", "extensions", "insights"],
    );
    // the inspector tab inventory IS the right-area set, in one place
    assert.deepEqual(
      [...INSPECTOR_TAB_COMPONENTS],
      ["files", "cron", "skills", "tools", "agents", "continuous", "settings", "kanban", "extensions", "insights"],
    );
    assert.ok(
      INSPECTOR_TAB_COMPONENTS.every((c) => !isClosableTab(c)),
      "no X on any of the ten",
    );
  });

  it("settings has NO other entry point (t_49d0b308): the '+' inventory excludes it", () => {
    // settings has no menu path (ruling, t_d6b657a7): persistence and the
    // default-arrangement fallback are its only entry. MAIN_DOCKABLES (the
    // main-area list) and MENU_DOCKABLES (the refill inventory) both
    // exclude it. card t_d7fd3ad3: insights LEFT the main area (its home
    // is the right shell) - chat is the main class's only member again.
    assert.deepEqual([...MAIN_DOCKABLES], ["chat"]);
    assert.ok(!MENU_DOCKABLES.includes("settings"), "no settings menu entry");
    assert.ok(!MENU_DOCKABLES.includes("sidebar"), "sessions never opens by menu");
    assert.ok(MENU_DOCKABLES.includes("kanban"), "kanban IS refillable");
    // card t_14f73742: extensions IS refillable - the '+' menu is a real
    // entry point for the inventory tab (unlike settings, it has a
    // routeable panel body over GET /extensions)
    assert.ok(MENU_DOCKABLES.includes("extensions"), "extensions IS refillable");
    // card t_d7fd3ad3: insights stays menu-fillable - the singleton
    // opener FOCUSES its inspector instance (clean coexistence with the
    // default sidebar home)
    assert.ok(MENU_DOCKABLES.includes("insights"), "insights IS refillable");
  });

  it("legacy ids: pre-reset trees deserialize but nothing may open one", () => {
    assert.deepEqual([...LEGACY_DOCK_COMPONENTS], ["inspector"]);
    assert.ok(!(DOCK_COMPONENTS as readonly string[]).includes("inspector"));
  });

  it("Sessions is locked: openSingleton refuses its only open path (t_d6b657a7)", async () => {
    assert.equal(isLockedTab("sidebar"), true, "sessions is the locked component");
    for (const id of ["sidebar"] as const) {
      const t: DockNode = chatOnlyTree();
      const res = openSingleton(t, panes(t)[0]!.id, id);
      assert.equal(res.action, "noop", `${id} opens nowhere`);
      assert.equal(res.tree, t);
    }
  });

  it("free content: singletons open INTO an empty edge shell (t_d6b657a7)", async () => {
    await withScratchSingleton(async (scratch) => {
      const t = defaultTree();
      const side = paneById(t, asSplit(t).children[0].id);
      const res = openSingleton(t, side.id, scratch);
      assert.equal(res.action, "created", "an edge shell takes tabs now");
      assert.equal(instanceCount(res.tree, scratch), 1);
      assert.deepEqual(
        paneById(res.tree, side.id).tabs.map((x) => x.component),
        ["sidebar", scratch],
      );
    });
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
    // settings left the main class (card t_49d0b308) and insights left
    // with it (card t_d7fd3ad3: its home is the right shell) - chat is
    // the main class's only member.
    assert.deepEqual([...MAIN_DOCKABLES], ["chat"]);
    const ids = DOCKABLES.map((d) => d.id);
    assert.ok(MAIN_DOCKABLES.every((c) => ids.includes(c)));
    // card t_d7fd3ad3 items 1+3: the two header voices go capital -
    // "Sessions" (primary) and "Insights" (secondary); the module-tab
    // and inspector-voice titles stay lowercase by Endo's call
    assert.equal(COMPONENT_TITLES.sidebar, "Sessions");
    assert.equal(COMPONENT_TITLES.insights, "Insights");
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

  it("the locked component refuses every open path; free singletons open anywhere", async () => {
    const t = defaultTree();
    // sessions has NO open path (the loader owns it)...
    for (const c of ["sidebar"] as const) {
      const res = openSingleton(t, firstMainPaneId(t)!, c);
      assert.equal(res.action, "noop", `${c} opens nowhere`);
      assert.equal(res.tree, t);
    }
    // ...but settings/kanban/files DO (they open wherever the user aims)
    for (const c of ["settings", "kanban"] as const) {
      const res = openSingleton(t, firstMainPaneId(t)!, c);
      assert.equal(res.action, "focused", `${c} already lives - focus wins`);
    }
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
      ["files", "cron", "skills", "tools", "agents", "continuous", "settings", "kanban", "extensions", "insights"],
      "the chip strip's tabs are real now, and the two new views are in",
    );
    // persisted at once so the next boot never re-modernizes; the record
    // shape is v:3 (px shares): the same RENDER, but fixed children's
    // fractions re-derive from their px, so compare through the shape law
    const back = deserializeLayout(mem.getItem(DOCK_LAYOUT_KEY)!);
    assert.ok(back && treeShapesMatch(back.tree, tree), "the modern tree persisted on boot (same render)");
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
          tabs: ["files", "cron", "skills", "tools"].map((c) => ({
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
      ["files", "cron", "skills", "tools", "agents", "continuous", "settings", "kanban", "extensions", "insights"],
    );
    assert.equal(insp.active, 2, "the user's active view stays put");
  });

  it("a v1-era tree with settings parked in a MAIN pane strips it back to the era law", async () => {
    // the t_49d0b308 era: settings lived in the inspector only. A v1
    // persisted literal still gets that migration (openTab would refuse
    // building it today, so the literal is constructed by hand)
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
          tabs: ["files", "cron", "skills", "tools"].map((c) => ({
            component: c as DockPane["tabs"][number]["component"],
          })),
          active: 0,
          fixed: 300,
        },
      ],
      sizes: [1 / 3, 1 / 3, 1 / 3],
    };
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, JSON.stringify({ v: 1, tree: t0 }));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    // the chat pane is back to chat-only; the inspector pane holds the
    // ONE settings tab (pinned), in the canonical order
    const row = mainRowOf(tree); // t_67fc1905: the column root holds the row + the bottom shell
    const chatPane = paneById(tree, row.children[1].id);
    assert.deepEqual(
      chatPane.tabs.map((x) => x.component),
      ["chat"],
      "the stray settings tab left the main pane",
    );
    const insp = paneById(tree, row.children[2].id);
    assert.equal(
      insp.tabs.filter((t) => t.component === "settings").length,
      1,
      "exactly one settings tab, in the inspector",
    );
    assert.deepEqual(
      insp.tabs.map((x) => x.component),
      ["files", "cron", "skills", "tools", "agents", "continuous", "settings", "kanban", "extensions", "insights"],
    );
  });

  it("a v2 tree with settings dragged into a main pane KEEPS it (free content)", async () => {
    let t: DockNode = defaultTree();
    // settings is tab 6 of the inspector bar (continuous joined after
    // agents, t_72ecca67; logs left the strip - Endo's 2026-10-26):
    // drag it into the main pane
    t = moveTab(t, asSplit(t).children[2].id, 6, asSplit(t).children[1].id, 0);
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const main = paneById(tree, mainRowOf(tree).children[1].id);
    assert.deepEqual(
      main.tabs.slice(0, 2).map((x) => x.component),
      ["settings", "chat"],
      "the user's drag survives the reload verbatim",
    );
    // no second settings: the fallback only re-adds MISSING singletons
    assert.equal(
      [...allNodes(tree)].reduce(
        (n, p) => n + (p.kind === "pane" ? p.tabs.filter((x) => x.component === "settings").length : 0),
        0,
      ),
      1,
    );
  });

  it("a tree with sidebars toggled away gets its fixed shape back", async () => {
    const t: DockNode = makePane("chat");
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    // t_67fc1905: the boot graft adds the bottom shell (logs) - the fixed
    // shape is now sidebar + chat + inspector + bottom
    assert.equal(panes(tree).length, 4, "sidebar + chat + inspector + bottom");
    const comps = panes(tree).map((p) => p.tabs[0]?.component);
    assert.deepEqual(comps[0], "sidebar");
    assert.equal(panes(tree)[0]!.fixed, 240);
    assert.deepEqual(comps[3], "http_channel", "the bottom shell holds logs");
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

  it("ensureMainPane re-inserts a main pane when the record lost the last one (stale state)", async () => {
    // Endo ruling 2026-10-25: removePane refuses the last main pane now,
    // so the zero-main record is built BY HAND - the stale shape a
    // pre-ruling session may still have persisted (ensureMainPane is
    // the repair path for exactly that record)
    const full = defaultTree();
    const row = asSplit(full);
    const t: DockNode = makeSplit("row", [row.children[0], row.children[2]]);
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    // t_67fc1905: the bottom shell joins the pinned set on boot
    assert.equal(panes(dock.tree.value).length, 3, "sidebars + the bottom shell");
    const id = dock.ensureMainPane();
    const tree = dock.tree.value;
    const pane = paneById(tree, id);
    assert.equal(pane.tabs.length, 0, "a fresh flexible pane");
    assert.equal(panes(tree).length, 4, "the dock has a main pane again");
    // the new pane lands inside the MAIN ROW, at its after-edge (never
    // beside the column's bottom child)
    assert.deepEqual(
      panes(tree).map((p) => p.id),
      [
        mainRowOf(tree).children[0].id, // sidebar
        mainRowOf(tree).children[1].id, // inspector
        id, // the fresh main pane, after-edge of the row
        asSplit(tree).children[1].id, // the bottom shell
      ],
      "inserted at the row's after-edge, the bottom shell last",
    );
  });

  it("canRemovePane: pinned never, the last MAIN pane never (Endo ruling 2026-10-25)", async () => {
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(defaultTree()));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const row = mainRowOf(tree); // t_67fc1905: row indexes live one level down now
    const side = paneById(tree, row.children[0].id);
    const files = paneById(tree, row.children[2].id);
    const chatPane = paneById(tree, row.children[1].id);
    assert.equal(dock.canRemovePane(side.id), false, "sessions pane unclosable");
    assert.equal(dock.canRemovePane(files.id), false, "inspector pane unclosable");
    assert.equal(
      dock.canRemovePane(paneById(tree, asSplit(tree).children[1].id).id),
      false,
      "the bottom shell is unclosable too",
    );
    // the chat pane is the ONLY main pane: the shells don't count toward
    // the census, so its X hides (the old all-pane count let it go)
    assert.equal(dock.canRemovePane(chatPane.id), false, "the last main pane is unclosable");
    assert.equal(dock.isLastMainPane(chatPane.id), true, "the census read agrees");
    // a second main pane flips the read: the X comes back on EACH of them
    dock.split(chatPane.id, "row", "after");
    const fresh = panes(dock.tree.value).find((p) => p.edge === undefined && p.tabs.length === 0)!;
    assert.ok(fresh, "the split minted a second main pane");
    assert.equal(dock.canRemovePane(chatPane.id), true, "a main pane may go once another lives");
    assert.equal(dock.canRemovePane(fresh.id), true, "the fresh pane may go too");
    assert.equal(dock.isLastMainPane(chatPane.id), false);
  });

  it("the last main pane refuses removal through the store (Endo ruling 2026-10-25)", async () => {
    const t: DockNode = makePane("chat");
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    // boot repair already has the fixed shape; the chat pane is the only
    // MAIN pane, so the store's remove is a no-op whatever called it
    const before = serialize(dock.tree.value);
    const chatPane = panes(dock.tree.value).find((p) => p.tabs[0]?.component === "chat")!;
    dock.remove(chatPane.id);
    await flush();
    assert.equal(serialize(dock.tree.value), before, "the close request changed nothing");
    assert.ok(panes(dock.tree.value).some((p) => p.id === chatPane.id), "the pane never left");
    assert.equal(dock.ensureMainPane(), chatPane.id, "a main pane still answers opens");
  });

  it("closeTab through the store refuses the pinned sidebars' tabs", async () => {
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(defaultTree()));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const row = mainRowOf(tree); // t_67fc1905
    const files = paneById(tree, row.children[2].id);
    dock.closeTab(files.id, 0);
    dock.closeTab(files.id, 1);
    await flush();
    assert.equal(
      paneById(dock.tree.value, files.id).tabs.map((x) => x.component as string).join(","),
      "files,cron,skills,tools,agents,continuous,settings,kanban,extensions,insights",
      "none of the ten may close (logs left the strip - Endo's comment 2026-10-26)",
    );
    // the main class still closes through the store on ANY census read
    // (t_b620f99d: the last-main-pane law protects the pane, not tabs)
    const chatPane = paneById(tree, row.children[1].id);
    dock.split(chatPane.id, "row", "after");
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

// --- the main-pane '+' source pins (Endo amendment, 2026-10-26) ------------

describe("the main-pane '+' mints a session, no menu", () => {
  const root = fileURLToPath(new URL("..", import.meta.url));
  const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");
  const pane = src("app/components/DockPane.vue");

  it("the '+' body mints a session through newSession() - no dropdown wiring survives", () => {
    assert.match(pane, /aria-label="new session"/);
    assert.match(pane, /@click\.stop="newSession\(\)"/);
    // the old inventory-menu shape is OUT of the '+': no state, no toggle,
    // no popup markup (Endo owns the menu's return later)
    assert.doesNotMatch(pane, /newTabMenu/, "the newTabMenu state must be gone");
    assert.doesNotMatch(pane, /new tab components/, "the '+' popup markup must be gone");
  });

  it("the '+' still carries the drop-caret affordance for the bar's end", () => {
    assert.match(
      pane,
      /dock-tab-new"[\s\S]*?caret-before[\s\S]*?dropIdx === node\.tabs\.length/,
      "dropping a tab at the pane end still lands after the last tab",
    );
  });

  it("the empty-pane watermark KEEPS the full inventory menu", () => {
    assert.match(pane, /dock-empty[\s\S]*?role="menu"/);
    assert.match(pane, /v-for="d in menuDockables"/);
    assert.match(pane, /function openDockable\(/);
  });
});