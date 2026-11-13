// omega webui: unit tests for the bottom panel (card t_67fc1905) -
// the dock's pinned horizontal child under the main panel: the pane
// tree's bottom slot, the registry's bottom-hosted logs home (the
// dual-host right-shell copy was ruled a duplicate and removed -
// Endo's comment 2026-10-26), close/reopen persistence across
// reloads, and the horizontal divider's drag math
// (+200 snap ladder, grow-direction, drag-past-min close intent).
// Render-free; node's built-in test runner.
import { describe, it, test } from "node:test";
import assert from "node:assert/strict";
import {
  createDock,
  serialize,
  deserialize,
  migrateLegacy,
  makeSplit,
  makePane,
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
  syncBottomHostTabs,
  recalledEdgePx,
  MIN_PANE_PX,
  BOTTOM_DEFAULT_PX,
  BOTTOM_MAX_FRACTION,
  BOTTOM_CLOSED_KEY,
  DOCK_LAYOUT_KEY,
  DOCK_LAYOUT_BAK_KEY,
  type DockNode,
  type DockPane,
  type DockTab,
  type DockSplit,
} from "../app/composables/dock.ts";
import { defaultHostTabs, hostsOf } from "../app/composables/dockables.ts";
import { snapCandidateAt, BOTTOM_SNAP_STEP } from "../app/composables/panes.ts";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const srcPath = (rel: string) =>
  readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

test("fullscreen keeps the sidebars: css owns the takeover (Endo's comment 2026-10-26)", () => {
  const css = srcPath("../app/assets/css/bottompanel.css");
  // the band escapes the flow and spans between the sidebar insets
  assert.match(css, /\.dock-pane\.edge-bottom\.bottom-fullscreen\s*{[^}]*position:\s*absolute/s);
  assert.match(css, /left:\s*var\(--bottom-inset-left/s);
  assert.match(css, /right:\s*var\(--bottom-inset-right/s);
  // the sidebars never hide: only their measured overhang is neutralized
  // (the row stays rendered underneath)
  assert.match(css, /:has\(\.dock-pane\.edge-bottom\.bottom-fullscreen\)[^}]*margin-bottom:\s*0\s*!important/s);
  // the pane class is bound in DockPane (the view rides the class, not
  // inline styles)
  const pane = srcPath("../app/components/DockPane.vue");
  assert.match(pane, /'bottom-fullscreen':\s*node\.edge === 'bottom' && api\.bottomFullscreen\.value/);
  // no row-hiding branch left in the split renderer
  const split = srcPath("../app/components/DockSplit.vue");
  assert.ok(!split.includes('display: "none"'), "the row never hides");
});

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
      ["terminal", "observability"],
      "terminal + the Logs tab are the bottom host's default tabs (card t_55a3fc4b, t_e703180e, t_01c53e78)",
    );
  });

  it("the raw-tail logs pane is GONE: no component seats it anywhere (card t_01c53e78)", () => {
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const inspector = findEdgePane(dock.tree.value, "inspector")!;
    assert.ok(
      !inspector.tabs.some((t) => (t.component as string) === "http_channel"),
      "the inspector never seats the retired pane",
    );
    const bottom = bottomOf(dock.tree.value)!;
    assert.ok(
      !bottom.tabs.some((t) => (t.component as string) === "http_channel"),
      "the bottom panel does not seat it either",
    );
  });

  it("a persisted http_channel tab drops on load with a repair receipt (the deletability proof)", () => {
    // a record persisted before the pane retired (card t_01c53e78): the
    // component left the registry, so the total reader drops the tab and
    // says so - the surviving Logs tab is NOT duplicated or re-seated.
    const store = new MemoryStorage() as unknown as Storage;
    const fresh = createDock(store); // fresh boot (no record to persist yet)
    store.setItem(DOCK_LAYOUT_KEY, serialize(fresh.tree.value, []));
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!);
    let seeded = false;
    for (const n of allNodes(rec.tree as DockNode)) {
      if (!isPane(n) || n.edge !== "bottom") continue;
      n.tabs.unshift({ component: "http_channel" });
      seeded = true;
    }
    assert.ok(seeded, "pre: the record carries the retired pane");
    store.setItem(DOCK_LAYOUT_KEY, JSON.stringify(rec));
    const dock = createDock(store);
    assert.ok(
      ![...allNodes(dock.tree.value)].some(
        (n) => isPane(n) && n.tabs.some((t) => (t.component as string) === "http_channel"),
      ),
      "the retired tab is dropped on load",
    );
    assert.deepEqual(
      bottomOf(dock.tree.value)!.tabs.map((t) => t.component),
      defaultHostTabs("bottom"),
      "the surviving strip is the registry's bottom home",
    );
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
    // Endo ruling 2026-10-25: removePane can no longer strip the last
    // main pane, so the no-main state is built BY HAND - the shape a
    // stale persisted record may still carry (ensureMainPane is the
    // repair path for exactly that record)
    const dock = createDock(new MemoryStorage() as unknown as Storage);
    const root = dock.tree.value as DockSplit;
    const row = root.children[0] as DockSplit;
    const shellRow = makeSplit("row", [row.children[0], row.children[2]]);
    const without = makeSplit("column", [shellRow, root.children[1]]);
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
  it("the menubar toggle closes the bottom panel completely (the never-empty rule is main-only)", async () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    assert.ok(bottomOf(dock.tree.value));
    dock.toggleBottom();
    assert.equal(bottomOf(dock.tree.value), null, "the shell is out of the tree");
    await flush();
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!);
    assert.ok(rec.closedEdges.includes("bottom"), "closure rides the record (t_aa9dbada)");
    assert.equal(paneCount(dock.tree.value) > 0, true, "the dock is never empty");
  });

  it("closure SURVIVES reloads (unlike the sidebars' fixed shape)", async () => {
    const store = new MemoryStorage() as unknown as Storage;
    const first = createDock(store);
    first.toggleBottom();
    assert.equal(bottomOf(first.tree.value), null);
    await flush(); // persisted record now reflects the pruned shell + closed flag
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

  it("a legacy CLOSED key under a PRESENT shell is retired without reopening rules (migration)", async () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    await flush(); // the record now carries closedEdges - the legacy key is dead input
    store.setItem(BOTTOM_CLOSED_KEY, "1"); // corrupt: flag set, record newer
    const again = createDock(store);
    assert.ok(bottomOf(again.tree.value), "the shell stays");
    assert.equal(store.getItem(BOTTOM_CLOSED_KEY), null, "the legacy key retired at boot");
  });

  it("the bottom panel's drag-close (divider push) persists closure too", async () => {
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
    await flush();
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!);
    assert.ok(rec.closedEdges.includes("bottom"), "the push-close rides the record");
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

  it("respects the closed-edge set (never reinserts a closed panel)", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const tree = migrateLegacy((k) => null); // shape with shell, but no store write
    const docked = hideBottomPanel(tree, store); // close: height remembered, shell gone
    const again = ensureBottomPanel(docked, ["bottom"]);
    assert.equal(bottomOf(again), null, "the closed-edge set holds");
    const revealed = ensureBottomPanel(docked, []);
    assert.ok(bottomOf(revealed), "an open set lets the repair reinsert");
  });

  it("revealBottomPanel reopens the shell at the remembered height", () => {
    const store = new MemoryStorage() as unknown as Storage;
    const dock = createDock(store);
    dock.toggleBottom();
    const back = revealBottomPanel(dock.tree.value, store);
    assert.ok(bottomOf(back));
  });
});

describe("the boot repair's strip sync (syncBottomHostTabs, card t_841df625)", () => {
  const bottomDefaults = defaultHostTabs("bottom");

  function rootWithBottomStrip(tabs: DockTab[], active: number | null): DockNode {
    // makePane mints the ids; the caller shapes the shell's strip directly
    const shell = makePane(null, null, "bottom");
    shell.tabs = tabs;
    if (active !== null) shell.active = active;
    return makeSplit("column", [makePane("chat"), shell]);
  }

  const shellOf = (tree: DockNode): DockPane => findEdgePane(tree, "bottom")!;
  const stripOf = (tree: DockNode): string[] =>
    shellOf(tree).tabs.map((t) => t.component);
  // the strip's named residents: the surgery tests below strip or park
  // ONE default and expect the sync to bring it back (card t_e703180e
  // added a third bottom home, so the "last default" read is gone -
  // names are stable where an index into a growing list is not).
  const terminal = "terminal";
  const observability = "observability";
  // what the sync appends: every default the strip lacks, in registry
  // order, at the END
  const appended = (tabs: DockTab[]): string[] =>
    bottomDefaults.filter((d) => !tabs.some((t) => t.component === d));

  it("a persisted pre-observability strip gains the missing bottom homes", () => {
    checkStripSync([{ component: "terminal" }], 0, 0);
  });

  it("the active index is PRESERVED (append at end)", () => {
    checkStripSync([{ component: "terminal" }, { component: "chat" }], 1, 1);
  });

  function checkStripSync(
    tabs: DockTab[],
    activeBefore: number,
    activeAfter: number,
  ): void {
    const tree = rootWithBottomStrip(tabs, activeBefore);
    const out = syncBottomHostTabs(tree);
    assert.deepEqual(
      stripOf(out),
      [...tabs.map((t) => t.component), ...appended(tabs)],
      "the user strip is untouched; the missing defaults appended at END",
    );
    assert.equal(shellOf(out).active, activeAfter, "active index preserved");
    // idempotent: the second run changes NOTHING (same reference)
    const again = syncBottomHostTabs(out);
    assert.strictEqual(again, out);
  }

  it("an empty strip gains ALL the defaults and focuses 0", () => {
    const tree = rootWithBottomStrip([], 5);
    const out = syncBottomHostTabs(tree);
    assert.deepEqual(stripOf(out), bottomDefaults, "the full default strip");
    assert.equal(shellOf(out).active, 0, "an empty strip focuses the first");
  });

  it("a strip that already carries every bottom home is byte-identical (no-op)", () => {
    const tree = rootWithBottomStrip(
      bottomDefaults.map((c) => ({ component: c })),
      bottomDefaults.length - 1,
    );
    assert.deepEqual(stripOf(tree), bottomDefaults);
    assert.strictEqual(syncBottomHostTabs(tree), tree, "same reference");
  });

  it("a default parked ELSEWHERE (a main pane) is never duplicated", () => {
    // the whole-tree read (the restoreInspectorSet precedent): the
    // singleton lives in a main pane, the strip seats the REST -
    // strip-only membership would clone the singleton onto the shell
    const main = makePane(null);
    main.tabs = [{ component: terminal }];
    const shell = makePane(null, null, "bottom");
    shell.tabs = [{ component: observability }];
    const tree = makeSplit("column", [main, shell]);
    const out = syncBottomHostTabs(tree);
    assert.strictEqual(out, tree, "no-op: the default is seated");
    assert.equal(shellOf(out).tabs.length, 1, "the strip gained nothing");
    assert.equal(findComponentTab(out, terminal)!.paneId, main.id,
      "the singleton stays in the main pane");
  });

  it("a GROWN bottom root seats the strip in its hosting leaf (the restoreInspectorSet read)", () => {
    const shell = makePane(null, null, "bottom");
    shell.tabs = []; // the classic bar is empty while grown (by law)
    const leaf = makePane(null); // the shell's original content leaf
    leaf.tabs = [{ component: terminal }];
    shell.grown = makeSplit("row", [leaf, makePane(null)]);
    const tree = makeSplit("column", [makePane("chat"), shell]);
    const out = syncBottomHostTabs(tree);
    assert.equal(shellOf(out).tabs.length, 0, "the shell's own bar stays empty");
    for (const n of allNodes(out)) {
      if (isPane(n) && n.tabs.some((t) => t.component === terminal)) {
        assert.deepEqual(
          n.tabs.map((t) => t.component),
          bottomDefaults,
          "the leaf hosting the bottom-home tabs took the strip",
        );
        return;
      }
    }
    assert.fail("no bottom-hosting leaf survived");
  });

  it("an absent/closed bottom shell is nobody's repair target", () => {
    const tree = makePane("chat");
    assert.strictEqual(syncBottomHostTabs(tree), tree);
  });

  it("a pre-terminal PERSISTED record heals on load, backs up once, and stays healed on reload", async () => {
    const store = new MemoryStorage() as unknown as Storage;
    const fresh = createDock(store); // fresh boot: [logs, terminal] today
    store.setItem(DOCK_LAYOUT_KEY, serialize(fresh.tree.value, []));
    // surgery: a record persisted BEFORE merge c080511 - the shell's
    // strip predates the terminal registration
    const rec = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!);
    let stripped = false;
    for (const n of allNodes(rec.tree as DockNode)) {
      if (!isPane(n) || n.edge !== "bottom") continue;
      n.tabs = n.tabs.filter((t) => t.component !== terminal);
      n.active = Math.min(n.active, n.tabs.length - 1);
      stripped = true;
    }
    assert.ok(stripped, "pre: the shell's strip predates the terminal");
    const raw = JSON.stringify(rec);
    store.setItem(DOCK_LAYOUT_KEY, raw);
    const healed = createDock(store);
    // card t_e703180e: the strip now seats three homes - the healed
    // strip is compared as a SET, because the sync APPENDS at the end
    // (order is the strip-sync tests' business, membership is this one's)
    assert.deepEqual(
      [...(bottomOf(healed.tree.value)?.tabs.map((t) => t.component) ?? [])].sort(),
      [...bottomDefaults].sort(),
      "every bottom home is back, without any storage wipe",
    );
    assert.ok(
      healed.tree.value === undefined ||
        bottomOf(healed.tree.value)?.tabs.every((t) => "key" in t === false),
      "appended tabs carry no chat keys (not chat tabs)",
    );
    // ruling 6: the repaired load backs the RAW record up before re-persisting
    assert.equal(store.getItem(DOCK_LAYOUT_BAK_KEY), raw, "the pre-repair record backed up");
    // stable on reload: a second boot over the healed record changes nothing
    await flush();
    const after = JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!);
    assert.deepEqual(
      ((): string[] => {
        for (const n of allNodes(after.tree as DockNode)) {
          if (isPane(n) && n.edge === "bottom") {
            return (n as DockPane).tabs.map((t) => t.component);
          }
        }
        return [];
      })().sort(),
      [...bottomDefaults].sort(),
      "the healed strip persisted",
    );
    store.setItem(DOCK_LAYOUT_KEY, JSON.stringify(after));
    const stable = createDock(store);
    assert.deepEqual(
      [...(bottomOf(stable.tree.value)?.tabs.map((t) => t.component) ?? [])].sort(),
      [...bottomDefaults].sort(),
      "reload: the strip is the registry's bottom-home strip",
    );
    // and the loaded healed tree is repair-idempotent (same reference)
    assert.strictEqual(syncBottomHostTabs(stable.tree.value), stable.tree.value);
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
    // t_e539ba29: 600 content, not 700 - at the 0.5 max share the share cap
    // (300) binds below the floor cap on a 700 box (350 < 380), so this
    // premise needs a box the share cap leaves alone: share 300 > floor 280
    const { split, startPx, contentPx } = column(200, 600);
    // grow the bottom beyond what a 320 chat floor leaves: 600 - 320 = 280
    const d = dragSplitDivider(split, 1, startPx, -400, contentPx);
    const bottom = (d.split as DockSplit).children[1] as DockPane;
    assert.equal(bottom.fixed, 280, "capped by the chat floor");
    // t_6a74d5a1 wall law: the divider stops at the chat's floor - the chat
    // (and everything the drag did not touch) is bit-identical before/after
    const out = d.split as DockSplit;
    assert.deepEqual(out.sizes, (split as DockSplit).sizes, "the chat's fraction untouched");
    assert.deepEqual(
      (out.children[0] as DockPane).fixed,
      (split.children[0] as DockPane).fixed,
      "the chat's fixed state untouched",
    );
  });

  // Endo's comment (2026-10-26): the max share. A drag past it caps even
  // when the chat floor leaves room - the swallowed window strands the
  // divider offscreen (bottom-anchored geometry), ungrabbable. Lowered to
  // 0.5 by t_e539ba29 (2026-10-28): 0.65 left the chat a cramped strip
  // under the composer in the max-up state
  it("the bottom shell's growth stops at the max share (50%)", () => {
    // 1200 content: the chat floor would allow 880, the max share is 600 -
    // the share cap must bind first
    const { split, startPx, contentPx } = column(200, 1200);
    const d = dragSplitDivider(split, 1, startPx, -1000, contentPx);
    const bottom = (d.split as DockSplit).children[1] as DockPane;
    assert.equal(
      bottom.fixed,
      Math.floor(contentPx * BOTTOM_MAX_FRACTION),
      "capped at the max share, not the chat floor",
    );
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
  it("the Logs tab and the terminal are the bottom host's defaults", () => {
    // card t_55a3fc4b: terminal joins the fresh bottom strip
    // card t_e703180e: the observability tab is the second bottom home
    // card t_01c53e78: the raw-tail logs pane retired - Logs is the
    // surviving observability tab's name
    assert.deepEqual(defaultHostTabs("bottom"), ["terminal", "observability"]);
  });

  it("the Logs tab reports its bottom home; every other component single-hosts", () => {
    assert.deepEqual([...hostsOf("observability")], ["bottom"]);
    assert.deepEqual([...hostsOf("cron")], ["right"]);
    assert.deepEqual([...hostsOf("chat")], ["main"]);
  });

  it("the bottom host's fresh strip comes from the default home mapping", () => {
    const strip = bottomShellPane();
    assert.equal(strip.edge, "bottom");
    assert.equal(strip.tabs[0]!.component, "terminal");
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
