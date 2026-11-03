// omega webui: card t_9ed6cad7 - closing an edge pane HIDES state, it does
// not delete it. The hide stows the pane's full record (tabs, active, fixed,
// grown); the reopen restores THE pane instead of minting a fresh default;
// the layout record carries the stow (`hiddenEdges`), so a reload behaves
// like the live closed window. Render-free; node's built-in test runner.
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import {
  INSPECTOR_TAB_COMPONENTS,
  type DockComponentName,
} from "../app/composables/dockables.ts";
import {
  createDock,
  hideEdgePane,
  serialize,
  deserializeLayout,
  parseAnyLayout,
  revealEdgePane,
  ensurePinnedEdges,
  findEdgePane,
  findComponentTab,
  findPane,
  makeSplit,
  makePane,
  nextNodeId,
  DOCK_LAYOUT_KEY,
  type DockNode,
  type DockPane,
  type DockSplit,
  type HiddenEdgeState,
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
    tabs: INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })),
    active: 0,
    fixed: 300,
  };
  return asSplit(
    makeSplit("row", [
      makePane("sidebar", 240, "left"),
      makePane("chat", null),
      inspector,
    ]),
  );
}

function panesOf(t: DockNode): DockPane[] {
  const out: DockPane[] = [];
  const walk = (n: DockNode): void => {
    if (n.kind === "pane") out.push(n);
    else for (const c of n.children) walk(c);
  };
  walk(t);
  return out;
}

function countTab(t: DockNode, component: string): number {
  return panesOf(t).reduce(
    (n, p) => n + p.tabs.filter((x) => x.component === component).length,
    0,
  );
}

// a stowed pane the tests mint/restore: the left shell after the user
// dragged the kanban tab in (sessions + kanban, kanban focused)
function hiddenSidebar(): DockPane {
  return {
    kind: "pane",
    id: nextNodeId(),
    tabs: [{ component: "sidebar" }, { component: "kanban" }],
    active: 1,
    fixed: 480,
    edge: "left",
  };
}

// tests ---------------------------------------------------------------------

describe("edge pane hide/reveal - the stow (card t_9ed6cad7)", () => {
  /** the realistic reveal context: the shell already pruned from the tree
      (the hide's product) - reveal always runs on the CLOSED tree. The
      kanban tab is REALLY moved (lifted out of the inspector). */
  function closedWithKanbanStowed(): { closed: DockSplit; stow: DockPane } {
    const open = ensurePinnedEdges(shellTree());
    const left = findEdgePane(open, "sidebar")!;
    left.fixed = 480; // the user resized the shell before hiding
    const kbLoc = findComponentTab(open, "kanban")!;
    const kbPane = findPane(open, kbLoc.paneId)!;
    kbPane.tabs = kbPane.tabs.filter((_, i) => i !== kbLoc.index);
    kbPane.active = Math.max(0, Math.min(kbPane.active, kbPane.tabs.length - 1));
    left.tabs = [...left.tabs, { component: "kanban" }];
    left.active = 1;
    const stow = asPane(structuredClone(left));
    return { closed: asSplit(hideEdgePane(open, "sidebar")), stow };
  }

  it("revealEdgePane restores the stowed pane: tabs, active index, fixed px", () => {
    const { closed, stow } = closedWithKanbanStowed();
    const opened = asSplit(revealEdgePane(closed, "sidebar", "before", null, stow));
    const pane = asPane(findEdgePane(opened, "sidebar")!);
    assert.deepEqual(pane.tabs, [{ component: "sidebar" }, { component: "kanban" }]);
    assert.equal(pane.active, 1);
    // the stow's own px wins over the sub-320 reopen clamp
    assert.equal(pane.fixed, 480);
  });

  it("revealEdgePane without a stow mints the defaults (today's semantics unchanged)", () => {
    const open = ensurePinnedEdges(shellTree());
    const closed = hideEdgePane(open, "inspector");
    const opened = revealEdgePane(closed, "inspector", "after", null);
    const pane = asPane(findEdgePane(opened, "inspector")!);
    assert.deepEqual(pane.tabs, INSPECTOR_TAB_COMPONENTS.map((c) => ({ component: c })));
    assert.equal(pane.active, 0);
  });

  it("revealEdgePane re-mints ids (a stow's ids never collide with the live tree)", () => {
    const { closed, stow } = closedWithKanbanStowed();
    const opened = revealEdgePane(closed, "sidebar", "before", null, stow);
    const pane = asPane(findEdgePane(opened, "sidebar")!);
    assert.notEqual(pane.id, stow.id);
    // every id in the restored tree is unique - a live split may have minted
    // over the stow's ids while the shell sat hidden
    const ids = new Set<string>();
    for (const p of panesOf(opened)) {
      assert.ok(!ids.has(p.id), `duplicate id ${p.id}`);
      ids.add(p.id);
    }
  });

  it("the no-duplication law re-checked at reveal: a tab seated elsewhere is not duplicated back in", () => {
    const { closed, stow } = closedWithKanbanStowed();
    // the bogus copy: kanban seated in the chat pane (a boot repair could
    // mint this while the shell sat hidden) - the reveal must never double
    const chat = panesOf(closed).find((p) => !p.edge && p.tabs.some((t) => t.component === "chat"))!;
    chat.tabs = [...chat.tabs, { component: "kanban" }];
    const opened = revealEdgePane(closed, "sidebar", "before", null, stow);
    assert.equal(countTab(opened, "kanban"), 1);
    assert.equal(findComponentTab(opened, "kanban")!.paneId, chat.id);
    const left = asPane(findEdgePane(opened, "sidebar")!);
    assert.deepEqual(left.tabs, [{ component: "sidebar" }]);
  });

  it("serialize/deserializeLayout carries the stow round-trip", () => {
    const { closed, stow } = closedWithKanbanStowed();
    const raw = serialize(closed, ["sidebar"], 1000, { sidebar: stow } satisfies HiddenEdgeState);
    const back = deserializeLayout(raw)!;
    assert.equal(back.version, 3);
    assert.deepEqual(back.closedEdges, ["sidebar"]);
    const stowed = back.hiddenEdges?.sidebar;
    assert.ok(stowed);
    assert.deepEqual(stowed.tabs, [{ component: "sidebar" }, { component: "kanban" }]);
    assert.equal(stowed.active, 1);
    assert.equal(stowed.fixed, 480);
  });

  it("a record WITHOUT hiddenEdges (pre-change shape) loads to today's semantics", () => {
    const tree = ensurePinnedEdges(shellTree());
    // the OLD writer's exact shape: no hiddenEdges field at all
    const old = JSON.stringify({
      v: 3,
      tree: ((): unknown => {
        // prune the sidebar pane by hand (the pre-change hide result)
        const root = asSplit(tree);
        const rest = root.children.filter((c) => (c as DockPane).edge !== "left");
        return { kind: "split", id: root.id, direction: "row", px: [700, 300], children: rest };
      })(),
      closedEdges: ["sidebar"],
    });
    const back = deserializeLayout(old)!;
    assert.deepEqual(back.closedEdges, ["sidebar"]);
    assert.equal(back.hiddenEdges, undefined);
    // the total reader stays null-free with today's semantics: the record's
    // closed edge stays closed, nothing stowed
    const parsed = parseAnyLayout(old, () => tree);
    assert.equal(findEdgePane(parsed.tree, "sidebar"), null);
    assert.equal(parsed.hiddenEdges, undefined);
    assert.deepEqual(parsed.repairs, []);
  });

  it("a CORRUPTED hiddenEdges entry drops (never the record); the tolerant reader receipts it", () => {
    const tree = ensurePinnedEdges(shellTree());
    const raw = JSON.parse(serialize(tree, [], 1000, { sidebar: hiddenSidebar() } satisfies HiddenEdgeState)) as Record<string, unknown>;
    (raw.hiddenEdges as Record<string, unknown>).sidebar = {
      kind: "pane", id: "n42", tabs: [{ component: "not-a-component" }], active: 0, fixed: 100,
    };
    const back = deserializeLayout(JSON.stringify(raw))!;
    assert.ok(back.tree); // the record itself survived
    assert.ok(!back.hiddenEdges?.sidebar);
    const parsed = parseAnyLayout(JSON.stringify(raw), () => tree);
    assert.equal(parsed.hiddenEdges?.sidebar, undefined);
    assert.ok(parsed.repairs.some((r) => r.includes("unreadable")), JSON.stringify(parsed.repairs));
  });
});

describe("createDock: the closed window keeps its composition (card t_9ed6cad7)", () => {
  it("a tab dragged INTO the left sidebar survives close + reopen (+ reload), active preserved", async () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    const boot = dock.tree.value;
    const sidebar = findEdgePane(boot, "sidebar")!;
    assert.ok(sidebar, "the default boot mints the left shell");
    // drag kanban in and focus it
    const kb = findComponentTab(boot, "kanban");
    assert.ok(kb, "the default boot seats kanban in the inspector");
    dock.moveTab(kb.paneId, kb.index, sidebar.id, 1);
    await flush();
    const kb2 = findComponentTab(dock.tree.value, "kanban")!;
    dock.focus(kb2.paneId, kb2.index);
    await flush();
    // close
    dock.toggleEdge("sidebar", "before");
    await flush();
    assert.equal(findEdgePane(dock.tree.value, "sidebar"), null);
    // the record carries the stow
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!) as {
      closedEdges?: string[];
      hiddenEdges?: Record<string, { tabs: { component: string }[]; active: number }>;
    };
    assert.ok(rec.closedEdges?.includes("sidebar"));
    assert.deepEqual(rec.hiddenEdges?.sidebar?.tabs, [
      { component: "sidebar" },
      { component: "kanban" },
    ]);
    assert.equal(rec.hiddenEdges?.sidebar?.active, 1);
    // RELOAD (closed): the record carries the closure AND the stow
    const dock2 = createDock(store);
    assert.equal(findEdgePane(dock2.tree.value, "sidebar"), null, "still closed after reload");
    // reopen: the closed window's composition comes back, active preserved
    dock2.toggleEdge("sidebar", "before");
    await flush();
    const sb2 = asPane(findEdgePane(dock2.tree.value, "sidebar")!);
    assert.equal(countTab(dock2.tree.value, "kanban"), 1);
    assert.equal(findComponentTab(dock2.tree.value, "kanban")!.paneId, sb2.id);
    assert.equal(sb2.active, 1);
    // the in-page cycle again (no reload in between): still the user's state
    dock2.toggleEdge("sidebar", "before");
    await flush();
    dock2.toggleEdge("sidebar", "before");
    await flush();
    const sb3 = asPane(findEdgePane(dock2.tree.value, "sidebar")!);
    assert.equal(countTab(dock2.tree.value, "kanban"), 1);
    assert.equal(sb3.active, 1);
  });

  it("a default inspector tab moved OUT does not duplicate back in (close + reopen + reload)", async () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    const boot = dock.tree.value;
    const files = findComponentTab(boot, "files");
    const chat = findComponentTab(boot, "chat");
    assert.ok(files && chat);
    const chatId = chat.paneId;
    dock.moveTab(files.paneId, files.index, chatId, 1);
    await flush();
    assert.equal(countTab(dock.tree.value, "files"), 1);
    // close + reopen the inspector: files stays OUT
    dock.toggleEdge("inspector", "after");
    await flush();
    dock.toggleEdge("inspector", "after");
    await flush();
    assert.equal(countTab(dock.tree.value, "files"), 1);
    assert.equal(findComponentTab(dock.tree.value, "files")!.paneId, chatId);
    // now close it and RELOAD the closed state, then reopen: still no dup
    dock.toggleEdge("inspector", "after");
    await flush();
    const dock2 = createDock(store);
    assert.equal(findEdgePane(dock2.tree.value, "inspector"), null);
    dock2.toggleEdge("inspector", "after");
    await flush();
    const refetchChat = findComponentTab(dock2.tree.value, "chat");
    assert.ok(refetchChat);
    assert.equal(countTab(dock2.tree.value, "files"), 1);
    assert.equal(findComponentTab(dock2.tree.value, "files")!.paneId, refetchChat.paneId);
  });

  it("empty-state edge: the inspector reduced to its last non-default tab keeps that composition across close + reopen + reload", async () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    // move every default inspector tab out except kanban
    const chat = findComponentTab(dock.tree.value, "chat")!;
    for (const c of INSPECTOR_TAB_COMPONENTS.filter((x) => x !== "kanban")) {
      const loc = findComponentTab(dock.tree.value, c);
      if (loc) dock.moveTab(loc.paneId, loc.index, chat.paneId, 99); // clamps: append
    }
    await flush();
    const insp = findComponentTab(dock.tree.value, "kanban")!; // the inspector hosts kanban only
    const inspPane = asPane(findPane(dock.tree.value, insp.paneId)!);
    assert.ok(inspPane.edge === "right");
    assert.deepEqual(inspPane.tabs, [{ component: "kanban" }]);
    // close, reopen, reload, reopen: the composition survives
    dock.toggleEdge("inspector", "after");
    await flush();
    dock.toggleEdge("inspector", "after");
    await flush();
    let again = findComponentTab(dock.tree.value, "kanban")!;
    assert.deepEqual(asPane(findPane(dock.tree.value, again.paneId)!).tabs, [{ component: "kanban" }]);
    dock.toggleEdge("inspector", "after");
    await flush();
    const dock2 = createDock(store);
    dock2.toggleEdge("inspector", "after");
    await flush();
    again = findComponentTab(dock2.tree.value, "kanban")!;
    assert.deepEqual(asPane(findPane(dock2.tree.value, again.paneId)!).tabs, [{ component: "kanban" }]);
    assert.equal(countTab(dock2.tree.value, "files"), 1, "the out-moved defaults never re-seat");
  });

  it("narrowSqueeze is a hide, not a delete: the toggle re-reveals the squeezed pane's composition", async () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    await flush();
    // starve the window: the inspector gives way first
    dock.narrowSqueeze(800);
    await flush();
    assert.equal(findEdgePane(dock.tree.value, "inspector"), null);
    // the record stowed it (the give-way closure persists across reloads)
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!) as {
      closedEdges?: string[];
      hiddenEdges?: Record<string, { tabs: unknown[] }>;
    };
    assert.ok(rec.closedEdges?.includes("inspector"));
    assert.ok((rec.hiddenEdges?.inspector?.tabs?.length ?? 0) > 0);
    // the user's toggle reopens THE pane
    dock.toggleEdge("inspector", "after");
    await flush();
    const pane = asPane(findEdgePane(dock.tree.value, "inspector")!);
    assert.equal(pane.tabs.length, INSPECTOR_TAB_COMPONENTS.length);
    assert.equal(pane.active, 0);
  });

  it("divider drag-close is a hide too: the stow restores on the toggle", async () => {
    // pure path: hide via the model, stow by the same call the drag intents
    // run (dragSplitDivider's intents funnel through the store's setClosed +
    // hiddenState capture); here the pure reveal side of that capture.
    const store = new MemoryStorage();
    const dock = createDock(store);
    await flush();
    dock.toggleEdge("inspector", "after"); // hide (stows)
    await flush();
    dock.toggleEdge("inspector", "after"); // reopen (restore path)
    await flush();
    const pane = asPane(findEdgePane(dock.tree.value, "inspector")!);
    // the restored pane carries the FULL recorded composition: the default
    // inspector tab set, files active, no duplicates anywhere
    assert.deepEqual(
      pane.tabs.map((t) => t.component),
      INSPECTOR_TAB_COMPONENTS.map((c) => c as string),
    );
    assert.equal(countTab(dock.tree.value, "files"), 1);
    assert.equal(countTab(dock.tree.value, "settings"), 1);
  });

  it("undo of a HIDE restores the pre-hide state, the stow cleared; the next hide re-stows", async () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    await flush();
    dock.toggleEdge("inspector", "after"); // hide
    await flush();
    assert.equal(findEdgePane(dock.tree.value, "inspector"), null);
    assert.equal(dock.undoLayout(), true);
    await flush();
    const pane = asPane(findEdgePane(dock.tree.value, "inspector")!);
    assert.equal(pane.tabs.length, INSPECTOR_TAB_COMPONENTS.length, "the pre-hide composition came back");
    // the restored pane carries the FULL recorded composition, no duplicates
    // the record's stow retired with the open edge (watch fired after undo)
    await flush();
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!) as { closedEdges?: string[] };
    assert.ok(!rec.closedEdges?.includes("inspector"));
  });
});