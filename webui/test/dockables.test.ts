// omega webui: unit tests for THE DOCKABLE CONTRACT (card t_30ee04fb) -
// the registry (dockables.ts), the singleton opener (openSingleton in the
// webui), the store-level semantics (has / openSingleton / the reset-era
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
  EDGE_DEFAULT_PX,
  createDock,
  focusTab,
  openTab,
  makePane,
  makeSplit,
  nextNodeId,
  openSingleton,
  moveTab,
  findComponentTab,
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
    like defaultTree below still BUILD row trees for the webui tests -
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

  it("tab labels ride the Title Case siblings (card t_556ee727)", () => {
    // every shell tab renders Title Case today: Files, Settings, Insights,
    // Agents. The module tabs override via dockable.json; `agents` is the
    // only registry title the tab bar prints raw (card t_210a0d19's
    // "Modules" label died with that tab - card t_81d0901b moved the
    // module surface into settings). Display labels ONLY - ids, routes and
    // module names stay lowercase.
    assert.equal(COMPONENT_TITLES.agents, "Agents");
    for (const d of DOCKABLES) {
      if (d.id === "agents") continue;
      assert.ok(
        d.title === "Sessions" && d.id === "sidebar" ||
          d.title === "Files" && d.id === "files" ||
          d.title === "Settings" && d.id === "settings" ||
          d.title === "Insights" && d.id === "insights" ||
          d.title === "MCP" && d.id === "mcp" ||
          d.title === "Browser" && d.id === "browser" ||
          d.title === "Notifications" && d.id === "notifications" ||
          // card t_55a3fc4b: the integrated terminal's label
          d.title === "Terminal" && d.id === "terminal" ||
          // card t_e703180e: the observability tab's label
          // card t_01c53e78: renamed to "Logs" - the one logs surface
          d.title === "Logs" && d.id === "observability" ||
          d.title === d.id,
        `${d.id}: unexpected label "${d.title}"`,
      );
    }
  });

  it("carries the contract fields: non-empty title, an iconset name, one policy, an area", () => {
    for (const d of DOCKABLES) {
      assert.ok(d.title.length > 0, `${d.id}: title`);
      // card t_502b8e81: the registry icon is a NAME in the iconset -
      // the iconset test pins the union; here only the shape is contract
      assert.equal(typeof d.icon, "string", `${d.id}: icon is a name`);
      assert.ok((d.icon as string).length > 0, `${d.id}: icon non-empty`);
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
      // card t_36ceb99b: chat AND viewer are the closable multi class.
      // card t_95c91f9c: browser joins the closable class - a general
      // browser is closable like the content tabs, just singleton.
      // card t_00e4a784: settings joins too - the menubar gear opens it
      // as a closable main tab (VSCode's settings tab).
      assert.equal(
        isClosableTab(d.id),
        d.id === "chat" || d.id === "viewer" || d.id === "browser" || d.id === "settings",
        `${d.id}: closable == chat|viewer|browser|settings`,
      );
    }
  });

  it("policy split: chat + viewer are multi (t_36ceb99b); the closable class adds browser + settings singletons (t_95c91f9c, t_00e4a784)", () => {
    const multi = DOCKABLES.filter((d) => d.policy === "multi").map((d) => d.id);
    // card t_9855a20e: the new-tab placeholder is retired - the multi class
    // is the two per-instance content tabs again
    assert.deepEqual(multi, ["chat", "viewer"]);
    const closable = DOCKABLES.filter((d) => isClosableTab(d.id)).map((d) => d.id);
    assert.deepEqual([...closable].sort(), ["browser", "chat", "settings", "viewer"]);
  });

  it("the fixed sidebars: sidebar pinned left, the inspector NINE pinned right (t_49d0b308 + t_d7fd3ad3 + arc 3c canvas + t_c7136c50 mcp + t_180db2cb notifications; t_00e4a784 retired settings; t_81d0901b retired the modules cockpit tab)", () => {
    assert.deepEqual(
      DOCKABLES.filter((d) => d.area === "left").map((d) => d.id),
      ["sidebar"],
    );
    // the default order is the ruling's: files, cron, skills, tools,
    // agents, kanban, insights, mcp, notifications (extensions left the
    // strip in card t_81d0901b - its surface is the settings MODULES
    // section; insights joined the right shell at t_d7fd3ad3; mcp joined
    // last at t_c7136c50; notifications joined last at t_180db2cb;
    // settings left for the on-demand main tab at t_00e4a784)
    assert.deepEqual(
      DOCKABLES.filter((d) => d.area === "right").map((d) => d.id),
      ["files", "cron", "skills", "tools", "agents", "kanban", "insights", "mcp", "notifications"],
    );
    // the inspector tab inventory IS the right-area set, in one place
    assert.deepEqual(
      [...INSPECTOR_TAB_COMPONENTS],
      ["files", "cron", "skills", "tools", "agents", "kanban", "insights", "mcp", "notifications"],
    );
    assert.ok(
      INSPECTOR_TAB_COMPONENTS.every((c) => !isClosableTab(c)),
      "no X on any of the nine",
    );
  });

  it("settings: NO inspector seat, NO '+' entry - the menubar gear is the entry point (t_00e4a784)", () => {
    // the ruling's three facts on the registry: main area, closable,
    // absent from both the fixed inspector set and the refill inventory.
    const settings = DOCKABLES.find((d) => d.id === "settings")!;
    assert.equal(settings.area, "main", "settings is a main-area tab");
    assert.equal(settings.closable, true, "and closes like any tab");
    assert.equal(settings.policy, "singleton", "one settings tab, ever");
    assert.ok(!INSPECTOR_TAB_COMPONENTS.includes("settings"), "never a pinned inspector view");
    // MAIN_DOCKABLES (the main-area list) carries it; MENU_DOCKABLES (the
    // '+' refill inventory) does not - the menubar button is the door.
    // card t_d7fd3ad3: insights LEFT the main area (its home is the right
    // shell). card t_36ceb99b: viewer joins the MAIN class (multi,
    // row-click opened) but not the MENUS - an empty viewer tab is
    // meaningless, content IS the tab.
    assert.deepEqual([...MAIN_DOCKABLES], ["chat", "viewer", "settings", "browser"]);
    // card t_95c91f9c: the browser joins the main class. It IS
    // menu-fillable: the empty-pane '+' menu is a real entry point
    // (a browser tab is meaningful with no site yet - the bar IS the
    // content, opposite ruling from the viewer).
    assert.ok(MAIN_DOCKABLES.includes("browser"), "browser IS a main-area tab");
    assert.ok(!MENU_DOCKABLES.includes("settings"), "no settings menu entry");
    assert.ok(!MENU_DOCKABLES.includes("viewer"), "no empty viewer menu entry");
    assert.ok(!MENU_DOCKABLES.includes("sidebar"), "sessions never opens by menu");
    assert.ok(MENU_DOCKABLES.includes("kanban"), "kanban IS refillable");
    // card t_81d0901b: the modules cockpit is NOT a dockable any more - it
    // has no menu entry to check (the settings MODULES section is the
    // surface, and the /modules command jumps there).
    // card t_d7fd3ad3: insights stays menu-fillable - the singleton
    // opener FOCUSES its inspector instance (clean coexistence with the
    // default sidebar home)
    assert.ok(MENU_DOCKABLES.includes("insights"), "insights IS refillable");
  });

  it("legacy ids: pre-reset trees deserialize but nothing may open one", () => {
    // card t_9855a20e: "newtab" joined "inspector" as legacy-only - records
    // persisted while the never-zero placeholder lived still parse, and the
    // boot's repairPaneSeats retires the seat onto a fresh new chat
    assert.deepEqual([...LEGACY_DOCK_COMPONENTS], ["inspector", "newtab"]);
    assert.ok(!(DOCK_COMPONENTS as readonly string[]).includes("inspector"));
    assert.ok(!(DOCK_COMPONENTS as readonly string[]).includes("newtab"));
  });

  it("card t_81d0901b: the modules cockpit is NOT a dockable any more", () => {
    // module toggles are configuration, not content - the surface moved into
    // the settings tab's MODULES section and the secondary-pane tab left the
    // registry entirely. No id, no inventory entry, no title, no floor line:
    // a leftover of any of them would let the registry advertise a tab the
    // shell no longer has an outlet for.
    assert.ok(
      !(DOCK_COMPONENTS as readonly string[]).includes("extensions"),
      "the removed tab id left the openable component universe",
    );
    assert.equal(
      DOCKABLES.find((d) => (d.id as string) === "extensions"),
      undefined,
      "no inventory entry survives",
    );
    assert.equal(
      (COMPONENT_TITLES as Record<string, string>).extensions,
      undefined,
      "no stale \"Modules\" title",
    );
    assert.ok(!MENU_DOCKABLES.includes("extensions" as never), "no '+' menu entry");
    assert.ok(!INSPECTOR_TAB_COMPONENTS.includes("extensions" as never), "no inspector seat");
    // and it is NOT a legacy id either: a legacy id would DESERIALIZE (a
    // phantom tab in a live pane); the removed one must DROP on load
    assert.ok(
      !(LEGACY_DOCK_COMPONENTS as readonly string[]).includes("extensions"),
      "the removed id is not resurrected through the legacy door",
    );
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
    // insights left the main class (card t_d7fd3ad3: its home is the
    // right shell). card t_36ceb99b: the viewer joins the main class.
    // card t_95c91f9c: the browser joins the main class (singleton, the
    // '+' menu opener focuses it like every singleton). card t_00e4a784:
    // settings is BACK in the main class (the menubar gear's tab).
    const ids = DOCKABLES.map((d) => d.id);
    assert.ok(MAIN_DOCKABLES.every((c) => ids.includes(c)));
    assert.deepEqual([...MAIN_DOCKABLES], ["chat", "viewer", "settings", "browser"]);
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

// --- openSingleton (the webui half of the contract) ------------------------
// card t_49d0b308: no REAL component lives in the main-singleton class
// anymore (settings is pinned in the inspector now), so these tests run
// against the test-only scratch singleton (see scratchDockable.ts) - the
// contract text is unchanged, only the fixture.

describe("openSingleton", () => {
  it("creates in an EMPTY MAIN pane when no instance exists", async () => {
    await withScratchSingleton(async (scratch) => {
      const tree = chatOnlyTree();
      const base = splitPane(tree, panes(tree)[0]!.id, "row", "after");
      // card t_9855a20e: the split mints an EMPTY pane - the open fills it
      const seat = panes(base).find((p) => p.tabs.length === 0)!;
      const res = openSingleton(base, seat.id, scratch);
      assert.equal(res.action, "created");
      const pane = paneById(res.tree, seat.id);
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
      // card t_9855a20e: the split's fresh pane is EMPTY
      const seat = panes(base).find((p) => p.tabs.length === 0)!;
      const withTab = openSingleton(base, seat.id, scratch);
      assert.equal(withTab.action, "created");
      const other = panes(withTab.tree).find((p) => p.id !== seat.id)!;
      const res = openSingleton(withTab.tree, other.id, scratch);
      assert.equal(res.action, "focused");
      assert.equal(instanceCount(res.tree, scratch), 1, "never a second singleton");
      assert.equal(
        paneById(res.tree, seat.id).tabs.length,
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
    // ...but kanban/files DO (they open wherever the user aims); settings
    // no longer lives in the tree (card t_00e4a784) - the opener CREATES
    // its tab on first click, the focus arm covers the second
    for (const c of ["kanban"] as const) {
      const res = openSingleton(t, firstMainPaneId(t)!, c);
      assert.equal(res.action, "focused", `${c} already lives - focus wins`);
    }
    const fresh = openSingleton(t, firstMainPaneId(t)!, "settings");
    assert.equal(fresh.action, "created", "settings opens on demand");
    const again = openSingleton(fresh.tree, firstMainPaneId(t)!, "settings");
    assert.equal(again.action, "focused", "a second click focuses the one tab");
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

  it("a loaded pre-reset tree modernizes: the inspector tab expands, settings+kanban join (t_49d0b308; settings retired again by t_00e4a784)", async () => {
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
      // t_c7136c50 note: an era tree whose insights tab gets lifted
      // (rehomeInsightsTab runs before the right edge lands) re-seats
      // insights AFTER the restored set - the modernized v1 strip ends
      // [mcp, notifications, insights]. The fresh-tree default stays
      // canonical (dock.test.ts). Card t_81d0901b: the extensions cockpit
      // left the strip entirely (its surface is the settings MODULES
      // section), so the tail lost that id too.
      ["files", "cron", "skills", "tools", "agents", "kanban", "mcp", "notifications", "insights"],
      "the chip strip's tabs are real now, and the new views are in",
    );
    // card t_00e4a784: settings is NOT re-seated by the modernizer - the
    // menubar gear owns it as an on-demand main tab
    assert.equal(findComponentTab(tree, "settings"), null);
    // persisted at once so the next boot never re-modernizes; the record
    // shape is v:3 (px shares): the same RENDER, but fixed children's
    // fractions re-derive from their px, so compare through the shape law
    const back = deserializeLayout(mem.getItem(DOCK_LAYOUT_KEY)!);
    assert.ok(back && treeShapesMatch(back.tree, tree), "the modern tree persisted on boot (same render)");
  });

  it("a five-tab-era inspector gains kanban + friends in the canonical order, settings stays out (t_49d0b308, t_00e4a784)", async () => {
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
      ["files", "cron", "skills", "tools", "agents", "kanban", "insights", "mcp", "notifications"],
    );
    assert.equal(insp.active, 2, "the user's active view stays put");
  });

  it("a v1-era tree with settings parked in a MAIN pane KEEPS it - the main class is its home now (t_00e4a784)", async () => {
    // the t_49d0b308 era stripped this seat; the 2026-11-09 ruling
    // REVERSED it - settings opens as a main tab, so a persisted
    // main-pane copy is the user's own tab and survives the load
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
    const row = mainRowOf(tree); // t_67fc1905: the column root holds the row + the bottom shell
    const chatPane = paneById(tree, row.children[1].id);
    assert.deepEqual(
      chatPane.tabs.map((x) => x.component),
      ["chat", "settings"],
      "the main-pane settings tab is the new law's normal home",
    );
    const insp = paneById(tree, row.children[2].id);
    assert.equal(
      insp.tabs.filter((t) => t.component === "settings").length,
      0,
      "never a second, pinned copy in the inspector",
    );
    assert.deepEqual(
      insp.tabs.map((x) => x.component),
      // same lift-and-reseat shape as the pre-reset modernize above: the
      // insights copy lifts back to the right shell
      ["files", "cron", "skills", "tools", "agents", "kanban", "mcp", "notifications", "insights"],
    );
  });

  it("a v2 tree with a right-area tab dragged into a main pane KEEPS it (free content)", async () => {
    let t: DockNode = defaultTree();
    // kanban is tab 5 of the inspector bar (settings left the strip -
    // t_00e4a784; continuous folded away - t_c7222095): drag it into the
    // main pane
    t = moveTab(t, asSplit(t).children[2].id, 5, asSplit(t).children[1].id, 0);
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const tree = dock.tree.value;
    const main = paneById(tree, mainRowOf(tree).children[1].id);
    assert.deepEqual(
      main.tabs.slice(0, 2).map((x) => x.component),
      ["kanban", "chat"],
      "the user's drag survives the reload verbatim",
    );
    // no second kanban: the fallback only re-adds MISSING singletons
    assert.equal(
      [...allNodes(tree)].reduce(
        (n, p) => n + (p.kind === "pane" ? p.tabs.filter((x) => x.component === "kanban").length : 0),
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
    assert.equal(panes(tree)[0]!.fixed, EDGE_DEFAULT_PX.sidebar);
    assert.deepEqual(comps[3], "terminal", "the bottom shell holds the terminal");
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
    // card t_9855a20e: the recovered pane is EMPTY - the opener that asked
    // for it mints the content (here: openChatTab in app.vue), and no
    // placeholder sticker is seated
    assert.deepEqual(pane.tabs.map((x) => x.component), [], "a fresh, empty main pane");
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
    // card t_9855a20e: the split's fresh pane is EMPTY (no seat is minted)
    const fresh = panes(dock.tree.value).find(
      (p) => p.edge === undefined && p.tabs.length === 0,
    )!;
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
      "files,cron,skills,tools,agents,kanban,insights,mcp,notifications",
      "none of them may close (logs left the strip - Endo's comment 2026-10-26)",
    );
    // the main class still closes through the store on ANY census read
    // (t_b620f99d: the last-main-pane law protects the pane, not tabs) -
    // but card t_9855a20e locks the pane's LAST tab: give it company first
    const chatPane = paneById(tree, row.children[1].id);
    dock.split(chatPane.id, "row", "after");
    dock.openSingleton(chatPane.id, "settings");
    dock.closeTab(chatPane.id, 1);
    await flush();
    assert.deepEqual(
      paneById(dock.tree.value, chatPane.id).tabs.map((x) => x.component),
      ["chat"],
      "the tab with company closed, the lone survivor is locked",
    );
    const before = serialize(dock.tree.value);
    dock.closeTab(chatPane.id, 0);
    await flush();
    assert.equal(serialize(dock.tree.value), before, "the lone tab refuses the close");
  });

  it("a settings tab closes through the store like chat (t_00e4a784)", async () => {
    const mem = new MemoryStorage();
    mem.setItem(DOCK_LAYOUT_KEY, serialize(defaultTree()));
    const dock = createDock(mem as unknown as Storage);
    await flush();
    const paneId = dock.ensureMainPane();
    assert.equal(dock.openSingleton(paneId, "settings"), "created", "the gear's open mints the tab");
    await flush();
    let pane = paneById(dock.tree.value, paneId);
    assert.ok(pane.tabs.some((t) => t.component === "settings"), "settings opened into the main pane");
    const idx = pane.tabs.findIndex((t) => t.component === "settings");
    dock.closeTab(paneId, idx);
    await flush();
    pane = paneById(dock.tree.value, paneId);
    assert.equal(pane.tabs.some((t) => t.component === "settings"), false, "closes like any tab");
    // and the gear's opener brings it back, never a duplicate
    assert.equal(dock.openSingleton(paneId, "settings"), "created", "re-opens after a close");
    assert.equal(dock.openSingleton(paneId, "settings"), "focused", "the second click focuses");
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
    assert.equal(typeof entry.icon, "string", "module default icon is a name");
    assert.ok(entry.icon.length > 0);
  });

  it("honors a full declaration", () => {
    const spec = {
      title: "board",
      icon: "module",
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

  it("the empty-pane component menu is GONE (card t_d04a1c80)", () => {
    // DOCK-EMPTY-ACTIONS removal: the 15-button inventory the empty pane
    // rendered is deleted - no menu markup, no menu state, no open path
    // left in the pane. The placeholders are pinned as ABSENCES so a
    // reintroduced menu reds here (the 0.17.3 canvas menus are the
    // future home for adding views; a replacement built now is out of
    // scope by ruling).
    assert.doesNotMatch(pane, /dock-empty-actions/, "the menu wrapper is gone");
    assert.doesNotMatch(pane, /v-for="d in menuDockables"/);
    assert.doesNotMatch(pane, /function openDockable\(/);
    // the blank new-tab body is the only thing the else-branch renders
    assert.match(pane, /<div\s+v-else\s+class="dock-empty"\s*\/>/);
  });
});