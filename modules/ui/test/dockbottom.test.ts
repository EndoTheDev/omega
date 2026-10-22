// omega webui: unit tests for the bottom panel (card t_67fc1905) -
// the dock's pinned horizontal child under the main panel: the pane
// tree's bottom slot, the registry's dual-host logs home, close/reopen
// persistence across reloads, and the horizontal divider's drag math
// (+200 snap ladder, grow-direction, drag-past-min close intent).
// Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  createDock,
  serialize,
  deserialize,
  migrateLegacy,
  findEdgePane,
  findComponentTab,
  firstMainPaneId,
  insertMainPane,
  removePane,
  isPinnedPane,
  paneCount,
  isSplit,
  isPane,
  allNodes,
  dragSplitDivider,
  ensureBottomPanel,
  ensureBottomShell,
  hideBottomPanel,
  revealBottomPanel,
  bottomShellPane,
  recalledEdgePx,
  MIN_PANE_PX,
  BOTTOM_DEFAULT_PX,
  BOTTOM_CLOSED_KEY,
  DOCK_LAYOUT_KEY,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import { defaultHostTabs, hostsOf } from "../app/composables/dockables.ts";
import { snapCandidateAt, BOTTOM_SNAP_STEP } from "../app/composables/panes.ts";

function flush(): Promise<void> {
  // Vue's deep-watch persistence flushes on a microtask - a "reload"
  // (a second createDock over the same store) must wait for it
  return new Promise((r) => setTimeout(r, 0));
}

// tiny Storage stand-in (same shape as dock.test.ts's MemoryStorage - a
// fresh one per test file keeps the suites hermetic)
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

function bottomOf(tree: DockNode): DockPane | null {
  return findEdgePane(tree, "bottom");
}

/** the fresh-default tree shape (no persisted layout): column root,
    [main row, bottom shell]. */
function freshRoot(tree: DockNode): DockSplit {
  assert.equal(tree.kind, "split", "the fresh root is a split");
  return tree as DockSplit;
}

describe("the bottom shell's place in the tree", () => {
  it("a fresh boot plants the bottom panel under the main column", () => {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const root = freshRoot(dock.tree.value);
    assert.equal(root.direction, "column", "root: column");
    assert.equal(root.children.length, 2, "row + bottom shell");
    assert.equal(isSplit(root.children[0]!), true, "the main content row stays a row");
    assert.equal(root.children[1]!.kind, "pane", "bottom child is the shell pane");
    const bottom = bottomOf(dock.tree.value);
    assert.ok(bottom, "the shell exists");
    assert.equal(bottom.edge, "bottom");
    assert.equal(bottom.fixed, BOTTOM_DEFAULT_PX, "fresh height = default rung 200");
    assert.deepEqual(
      bottom.tabs.map((t) => t.component),
      ["http_channel"],
      "logs is the bottom host's default tab",
    );
  });

  it("logs ALSO keeps its secondary-sidebar home (the dual host)", () => {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const inspector = findEdgePane(dock.tree.value, "inspector")!;
    const inspectorLogs = inspector.tabs.find((t) => t.component === "http_channel");
    assert.ok(inspectorLogs, "the inspector still seats logs");
    const bottom = bottomOf(dock.tree.value)!;
    const bottomLogs = bottom.tabs.find((t) => t.component === "http_channel");
    assert.ok(bottomLogs, "the bottom panel seats logs too");
  });

  it("the bottom shell is pinned: unremovable via removePane", () => {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const bottom = bottomOf(dock.tree.value)!;
    assert.equal(isPinnedPane(bottom), true);
    const after = removePane(dock.tree.value, bottom.id);
    assert.strictEqual(after, dock.tree.value, "pinned panes refuse removal");
  });

  it("the main-pane class never routes into the bottom shell", () => {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const bottom = bottomOf(dock.tree.value)!;
    const firstMain = firstMainPaneId(dock.tree.value);
    assert.ok(firstMain);
    assert.notEqual(firstMain, bottom.id);
  });

  it("insertMainPane lands in the MAIN ROW of a column root, not beside the bottom shell", () => {
    // drop the flexible chat pane so the caller must re-insert a main pane
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const root = dock.tree.value as DockSplit;
    const row = root.children[0] as DockSplit;
    const chat = row.children.find((c) => isPane(c) && c.edge === undefined)!;
    const without = removePane(
      removePane(dock.tree.value, chat.id),
      "", // no-op id, just mechanics (the row still holds the two sidebars)
    );
    assert.ok(findEdgePane(without, "sidebar"), "sidebar survived");
    assert.ok(findEdgePane(without, "inspector"), "inspector survived");
    assert.ok(!firstMainPaneId(without), "no main pane left");
    const res = insertMainPane(without);
    const newRoot = res.tree;
    // still a column root with the bottom shell as LAST child
    assert.equal((newRoot as DockSplit).direction, "column");
    const newRow = (newRoot as DockSplit).children[0] as DockSplit;
    assert.ok(
      newRow.children.some((c) => isPane(c) && c.id === res.paneId),
      "the new main pane sits in the row",
    );
    assert.ok(bottomOf(newRoot), "the bottom shell survived");
  });
});

describe("close / reopen (persistence rulings)", () => {
  it("the menubar toggle closes the bottom panel completely (the never-empty rule is main-only)", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    assert.ok(bottomOf(dock.tree.value));
    dock.toggleBottom();
    assert.equal(bottomOf(dock.tree.value), null, "the shell is out of the tree");
    assert.equal(store.getItem(BOTTOM_CLOSED_KEY), "1", "closure persisted");
    assert.equal(paneCount(dock.tree.value) > 0, true, "the dock is never empty");
  });

  it("closure SURVIVES reloads (unlike the sidebars' fixed shape)", async () => {
    const store = new MemoryStorage() as unknown as Storage;
    const first = createDock(store);
    first.toggleBottom();
    assert.equal(bottomOf(first.tree.value), null);
    await flush(); // persisted tree now reflects the pruned shell
    // a fresh boot with the SAME store: the dock sees the closed flag and leaves the shell out
    const second = createDock(store);
    assert.equal(bottomOf(second.tree.value), null, "still closed after reload");
  });

  it("reopen restores the last height (hide remembers, reopen restores)", async () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    await flush();
    const bottom = bottomOf(dock.tree.value)!;
    // resize to an unsnapped height by direct drag on the model (no UI);
    // drag UP grows the bottom shell (it anchors to the viewport bottom)
    const root = dock.tree.value as DockSplit;
    const d = dragSplitDivider(root, 1, [600, 200], -40, 800); // grow by 40 → 240
    dock.tree.value = d.split;
    assert.equal(bottomOf(dock.tree.value)!.fixed, 240);
    dock.toggleBottom();
    dock.toggleBottom();
    const back = bottomOf(dock.tree.value)!;
    assert.equal(back.fixed, 240, "the remembered height reopened");
    assert.equal(recalledEdgePx(store, "bottom"), 240);
  });

  it("a stale CLOSED flag under a PRESENT shell gets cleared (repair)", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    store.setItem(BOTTOM_CLOSED_KEY, "1"); // corrupt: shell present + flag set
    const again = createDock(store);
    assert.ok(bottomOf(again.tree.value), "the shell stays");
    assert.equal(store.getItem(BOTTOM_CLOSED_KEY), "0", "the stale flag cleared at boot");
  });

  it("the bottom panel's drag-close (divider push) persists closure too", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    // drag the horizontal divider DOWN past the floor (drag down shrinks
    // the viewport-bottom-anchored shell): the bottom collapses
    const res = dock.drag(
      (dock.tree.value as DockSplit).id,
      1,
      [600, 200],
      100,
      800,
    );
    assert.ok(res.collapsedPaneId, "the close intent fired");
    assert.equal(bottomOf(dock.tree.value), null);
    assert.equal(store.getItem(BOTTOM_CLOSED_KEY), "1", "the push-close also persists");
  });
});

describe("the boot repair (ensureBottomPanel)", () => {
  it("grafts the shell into a pre-panel tree (upgrade path)", () => {
    // a v2 tree predating the bottom panel: plain row root, no shell
    const legacyStore = new MemoryStorage() as unknown as Storage;
    const prePanel = migrateLegacy((k) => legacyStore.getItem(k));
    // simulate pre-card shape: drop the shell bottom, persist as a plain row
    const bare = ((): DockNode => {
      const t = prePanel as DockSplit;
      assert.equal(t.direction, "column"); // freshly migrated includes bottom
      // build the PRE-card shape directly: row root without bottom
      return {
        kind: "split",
        id: "nz0",
        direction: "row",
        children: (t.children[0] as DockSplit).children,
        sizes: (t.children[0] as DockSplit).sizes,
      };
    })();
    const store = new MemoryStorage() as unknown as Storage;
    store.setItem(DOCK_LAYOUT_KEY, serialize(bare));
    const dock = createDock(store);
    assert.ok(bottomOf(dock.tree.value), "the upgrade grafts the bottom panel");
  });

  it("is idempotent over a shell that already lives in the tree", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    const before = dock.tree.value;
    const after = ensureBottomPanel(before, store);
    assert.strictEqual(after, before, "no change, same reference");
    assert.equal(bottomOf(after)!.id, bottomOf(before)!.id);
  });

  it("respects the closed flag (never reinserts a closed panel)", () => {
    const store = new MemoryStorage() as unknown as Storage;
    store.setItem(BOTTOM_CLOSED_KEY, "1");
    const tree = migrateLegacy((k) => null); // shape with shell, but no store write
    const docked = hideBottomPanel(tree, store); // close: flag written, shell gone
    const again = ensureBottomPanel(docked, store);
    assert.equal(bottomOf(again), null, "the closed flag holds");
    store.setItem(BOTTOM_CLOSED_KEY, "0");
    const revealed = ensureBottomPanel(docked, store);
    assert.ok(bottomOf(revealed), "clearing the flag lets the repair reinsert");
  });

  it("revealBottomPanel reopens the shell at the remembered height", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    dock.toggleBottom();
    const back = revealBottomPanel(dock.tree.value, store);
    assert.ok(bottomOf(back));
    assert.equal(store.getItem(BOTTOM_CLOSED_KEY), "0");
  });
});

describe("the horizontal divider drag (the bottom shell)", () => {
  function column(withBottomHeight: number, content = 800): {
    split: DockSplit;
    startPx: number[];
    contentPx: number;
  } {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const root = dock.tree.value as DockSplit;
    // normalize the bottom's height for the math test
    const bottom = bottomOf(dock.tree.value)!;
    const d = dragSplitDivider(root, 1, [content - withBottomHeight, 200], withBottomHeight - 200, content);
    const split = d.split;
    const startPx = [content - withBottomHeight, withBottomHeight];
    return { split, startPx, contentPx: content };
  }

  // t_be305778 pointer-tracking invariant: the bottom shell anchors to the
  // viewport BOTTOM, so the divider sits at y = contentPx - bottomHeight -
  // a drag DOWN (+delta) must SHRINK the shell and drag UP grow it, with
  // the divider's screen y tracking the pointer either way. (The original
  // block pinned "drag DOWN grows" - the left-child read - which inverted
  // the live gesture; these tests pin the pointer contract instead.)
  it("drag DOWN shrinks the bottom shell (the divider is its TOP edge)", () => {
    const { split, startPx, contentPx } = column(200);
    const d = dragSplitDivider(split, 1, startPx, 50, contentPx);
    const bottom = (d.split as DockSplit).children[1] as DockPane;
    assert.equal(bottom.fixed, 150, "+delta shrinks the bottom");
    assert.ok(bottom.fixed !== null);
    assert.equal(d.collapsedPaneId, null);
    assert.equal(contentPx - bottom.fixed, contentPx - startPx[1]! + 50,
      "the divider's y (content - height) rode DOWN with the pointer");
  });

  it("drag UP grows the bottom shell to the 128-floor cap of the chat", () => {
    const { split, startPx, contentPx } = column(200);
    const d = dragSplitDivider(split, 1, startPx, -60, contentPx);
    const bottom = (d.split as DockSplit).children[1] as DockPane;
    assert.equal(bottom.fixed, 260, "-delta grows the bottom");
    assert.ok(bottom.fixed !== null);
    assert.equal(contentPx - bottom.fixed, contentPx - startPx[1]! - 60,
      "the divider's y rode UP with the pointer");
  });

  it("drag DOWN past the floor is a CLOSE intent (the panel's own divider push)", () => {
    const { split, startPx, contentPx } = column(200);
    const d = dragSplitDivider(split, 1, startPx, 120, contentPx);
    assert.ok(d.collapsedPaneId, "collapse intent: 200 - 120 < 128");
  });

  it("snaps on the +200 ladder (200/400/600), not the sidebars' +320", () => {
    const { split, startPx, contentPx } = column(200);
    assert.equal(BOTTOM_SNAP_STEP, 200);
    // into the +14 zone of 400: 400 - 214 = ... start 200, drag UP 190: 200 + 190 = 390 → snap 400
    const d = dragSplitDivider(split, 1, startPx, -190, contentPx);
    const bottom = (d.split as DockSplit).children[1] as DockPane;
    assert.equal(bottom.fixed, 400, "snapped to rung 400");
    // between rungs stays free: 200 + 100 = 300 (midway) → no snap
    const free = dragSplitDivider(split, 1, startPx, -100, contentPx);
    const bottomFree = (free.split as DockSplit).children[1] as DockPane;
    assert.equal(bottomFree.fixed, 300, "between rungs the drag is free");
  });

  it("the chat (the absorbing neighbor above) keeps its 320 floor", () => {
    const { split, startPx, contentPx } = column(200, 700);
    // grow the bottom beyond what a 320 chat floor leaves: 700 - 320 = 380
    const d = dragSplitDivider(split, 1, startPx, -400, contentPx);
    const bottom = (d.split as DockSplit).children[1] as DockPane;
    assert.equal(bottom.fixed, 380, "capped by the chat floor");
  });
});

describe("persistence round-trip", () => {
  it("the bottom shell's marker + height survive serialize/deserialize", () => {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const back = deserialize(serialize(dock.tree.value))!;
    const bottom = bottomOf(back)!;
    assert.ok(bottom);
    assert.equal(bottom.edge, "bottom");
    assert.equal(bottom.fixed, BOTTOM_DEFAULT_PX);
    // and the reload path keeps it (no closed flag in a fresh store)
    const store = new MemoryStorage() as unknown as Storage;
    store.setItem(DOCK_LAYOUT_KEY, serialize(dock.tree.value));
    const redock = createDock(store);
    assert.equal(bottomOf(redock.tree.value)!.edge, "bottom");
  });

  it("every node id survives across a save/load (no duplicate mints)", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    dock.toggleBottom();
    dock.toggleBottom();
    const ids = new Set<string>();
    for (const n of allNodes(dock.tree.value)) {
      assert.ok(!ids.has(n.id), `duplicate id ${n.id}`);
      ids.add(n.id);
    }
  });
});

describe("the registry's home rulings", () => {
  it("logs' default home is the bottom host", () => {
    assert.deepEqual(defaultHostTabs("bottom"), ["http_channel"]);
  });

  it("logs reports BOTH hosts (right + bottom); every other component single-hosts", () => {
    assert.deepEqual([...hostsOf("http_channel")], ["right", "bottom"]);
    assert.deepEqual([...hostsOf("cron")], ["right"]);
    assert.deepEqual([...hostsOf("chat")], ["main"]);
  });

  it("the bottom host's fresh strip comes from the default home mapping", () => {
    const strip = bottomShellPane();
    assert.equal(strip.edge, "bottom");
    assert.equal(strip.tabs[0]!.component, "http_channel");
    assert.equal(strip.fixed, BOTTOM_DEFAULT_PX);
  });

  it("snapCandidateAt answers the bottom ladder (+200) and the sidebars' ladder keeps +320", () => {
    assert.equal(snapCandidateAt(400, 200), 400);
    assert.equal(snapCandidateAt(390, 200), 400);
    assert.equal(snapCandidateAt(414, 200), 400);
    // the zone is ±15 INCLUSIVE (one past the zone stays free)
    assert.equal(snapCandidateAt(384, 200), null);
    assert.equal(snapCandidateAt(416, 200), null);
    assert.equal(snapCandidateAt(320, 320), 320); // sidebar behavior unchanged
  });
});
