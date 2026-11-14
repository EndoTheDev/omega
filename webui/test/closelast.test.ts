// omega webui: the last-main-pane law (Endo ruling 2026-10-25,
// corrected t_b620f99d: the law protects the PANE, not the tabs) plus THE
// LOCKED LAST TAB (card t_9855a20e, Endo ruling 2026-11-14).
//
// The LAST main pane never closes its PANE: `removePane` refuses it as a
// no-op whatever carried the request in - button, shortcut, stale state
// (the guards read the LIVE tree). The main area never reaches zero
// panes; the empty launcher pane IS a main pane and stays.
//
// TAB close is a separate computation: a pane's LAST tab is LOCKED. It
// renders no X (DockPane's tabClosable) and `closeTab` refuses the
// request, so no close can empty a pane - which is exactly how the pane
// stays non-empty. Nothing is seated in its place: the never-zero
// placeholder of commit 8eabefc7 (card t_d04a1c80) is retired as the
// blank "empty bad looking space" Endo closed his last chat into. The two
// dead states it can leave on disk - a persisted placeholder seat, a
// zero-tab pane from an older record - are repaired at boot onto a fresh
// new-chat tab (`repairPaneSeats`).
//
// Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import {
  createDock,
  closeTab,
  findPane,
  isLastMainPane,
  mainPaneCount,
  makePane,
  makeSplit,
  movePane,
  moveTab,
  openTab,
  removePane,
  repairPaneSeats,
  serialize,
  splitPane,
  DOCK_LAYOUT_KEY,
  paneCount,
  type DockNode,
  type DockPane,
  type DockTab,
} from "../app/composables/dock.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

// minified in-memory Storage (same shape as the sibling suites)
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

function panes(t: DockNode): DockPane[] {
  const out: DockPane[] = [];
  const walk = (n: DockNode): void => {
    if (n.kind === "pane") out.push(n);
    else n.children.forEach(walk);
  };
  walk(t);
  return out;
}

/** the pane with this id (asserting it exists) */
function asPaneOf(t: DockNode, id: string): DockPane {
  const p = panes(t).find((x) => x.id === id);
  assert.ok(p, `expected pane ${id}`);
  return p!;
}

function comps(p: DockPane): string[] {
  return p.tabs.map((t) => t.component);
}

/** a persisted never-zero placeholder tab (card t_d04a1c80) - "newtab" is a
    LEGACY id now (dockables.ts), so records still carry it even though the
    live component union no longer does */
function legacySeat(): DockTab {
  return { component: "newtab" } as unknown as DockTab;
}

/** the fixed shell shape: left sessions + the single main pane + right
    inspector (the census reads 1 - the shells never count) */
function shellTree(): { t: DockNode; main: DockPane } {
  const t: DockNode = makeSplit("row", [
    makePane("sidebar", 240, "left"),
    makePane("chat", null),
    makePane("files", 300, "right"),
  ]);
  return { t, main: panes(t).find((p) => p.edge === undefined)! };
}

/** the same shell with the main pane hosting TWO chat tabs (the only shape
    a close is allowed to touch) */
function shellTreeTwoTabs(): { t: DockNode; main: DockPane } {
  const { t, main } = shellTree();
  const twoTabs: DockPane = { ...main, tabs: [{ component: "chat" }, { component: "chat" }] };
  const out: DockNode = makeSplit("row", [
    (t as { children: DockNode[] }).children[0]!,
    twoTabs,
    (t as { children: DockNode[] }).children[2]!,
  ]);
  return { t: out, main: twoTabs };
}

/** a fresh dock over the persisted shell shape (boot repair grafts the
    missing bottom shell; ids survive, so `main.id` still resolves) */
async function dockOver(t: DockNode): Promise<ReturnType<typeof createDock>> {
  const mem = new MemoryStorage() as unknown as Storage;
  mem.setItem(DOCK_LAYOUT_KEY, serialize(t));
  const dock = createDock(mem);
  await flush();
  return dock;
}

describe("the census (mainPaneCount / isLastMainPane)", () => {
  it("shells don't count: the fixed shape reads ONE main pane", () => {
    const { t, main } = shellTree();
    assert.equal(paneCount(t), 3, "three panes total...");
    assert.equal(mainPaneCount(t), 1, "...but one MAIN pane");
    assert.equal(isLastMainPane(t, main), true, "the chat pane is the last main pane");
    const shell = panes(t).find((p) => p.edge === "left")!;
    assert.equal(isLastMainPane(t, shell), false, "a pinned shell is never the last main pane");
  });
  it("a split flips the read: two main panes, neither is last", () => {
    const { t, main } = shellTree();
    const out = splitPane(t, main.id, "row", "after");
    assert.equal(mainPaneCount(out), 2);
    for (const p of panes(out)) {
      if (p.edge === undefined) assert.equal(isLastMainPane(out, p), false);
    }
  });
});

describe("case 1: a single main pane - pane X hidden, its tabs locked", () => {
  it("removePane refuses it even with shells present (the old guard counted them)", () => {
    const { t, main } = shellTree();
    assert.equal(removePane(t, main.id), t, "the close request is a no-op");
    assert.ok(findPane(t, main.id), "the pane never left");
  });
  it("the pane's LONE tab is LOCKED: closeTab refuses, nothing is seated", () => {
    const { t, main } = shellTree();
    assert.equal(main.tabs.length, 1);
    assert.equal(closeTab(t, main.id, 0), t, "the last tab never closes");
    assert.deepEqual(comps(asPaneOf(t, main.id)), ["chat"], "the tab is still there");
  });
  it("a tab WITH company still closes; the survivor is then locked", () => {
    const { t, main } = shellTreeTwoTabs();
    const after = closeTab(t, main.id, 0);
    assert.notEqual(after, t, "tab 0 closes");
    assert.deepEqual(comps(asPaneOf(after, main.id)), ["chat"], "one tab stays");
    assert.equal(asPaneOf(after, main.id).active, 0);
    // card t_9855a20e (THE LOCKED LAST TAB): the survivor is the pane's
    // LAST tab - the close that used to seat a placeholder is refused, so
    // the pane is never empty and no "New Tab" sticker appears
    const after2 = closeTab(after, main.id, 0);
    assert.equal(after2, after, "the last tab refuses - the tree is untouched");
    assert.deepEqual(comps(asPaneOf(after2, main.id)), ["chat"]);
    assert.ok(findPane(after2, main.id), "the pane never left the tree");
    assert.equal(mainPaneCount(after2), 1, "the pane is still a main pane");
  });
  it("an unclosable component's tab refuses whatever the tab count", () => {
    const { t, main } = shellTree();
    const files: DockPane = { ...main, tabs: [{ component: "chat" }, { component: "files" }] };
    const out: DockNode = makeSplit("row", [
      (t as { children: DockNode[] }).children[0]!,
      files,
      (t as { children: DockNode[] }).children[2]!,
    ]);
    assert.equal(closeTab(out, files.id, 1), out, "files carries no X (registry)");
  });
  it("the store still hides ONLY the pane X: canRemovePane false, the lone tab refuses", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(t);
    assert.equal(dock.canRemovePane(main.id), false, "no remove X on the last main pane");
    assert.equal(dock.isLastMainPane(main.id), true, "the pane guards on the census");
    const before = serialize(dock.tree.value);
    dock.closeTab(main.id, 0);
    await flush();
    assert.equal(serialize(dock.tree.value), before, "the locked tab changed nothing");
    assert.deepEqual(comps(findPane(dock.tree.value, main.id)!), ["chat"]);
    assert.ok(findPane(dock.tree.value, main.id), "the pane never left");
    assert.equal(mainPaneCount(dock.tree.value), 1);
  });
  it("a store close WITH company lands and leaves the survivor locked", async () => {
    const { t, main } = shellTreeTwoTabs();
    const dock = await dockOver(t);
    dock.closeTab(main.id, 0);
    await flush();
    assert.deepEqual(comps(findPane(dock.tree.value, main.id)!), ["chat"], "one tab closed");
    assert.equal(dock.canRemovePane(main.id), false, "still the last main pane");
  });
});

describe("case 2: two main panes - close on both, then the survivor loses it", () => {
  it("each pane may close while the other lives; the survivor's pane X goes", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(splitPane(t, main.id, "row", "after"));
    const mains = panes(dock.tree.value).filter((p) => p.edge === undefined);
    assert.equal(mains.length, 2, "the split minted a second main pane");
    for (const p of mains) {
      assert.equal(dock.canRemovePane(p.id), true, "both carry the pane X");
      assert.equal(dock.isLastMainPane(p.id), false, "neither is the last main pane");
    }
    // closing one pane: the survivor becomes the last main pane - its
    // pane X goes with the same tree swap
    const gone = mains.find((p) => p.id !== main.id)!;
    dock.remove(gone.id);
    await flush();
    const survivor = findPane(dock.tree.value, main.id)!;
    assert.ok(survivor, "the survivor stays");
    assert.equal(dock.canRemovePane(survivor.id), false, "the survivor's pane X is gone");
    assert.equal(dock.isLastMainPane(survivor.id), true, "its pane close path refuses");
    assert.equal(mainPaneCount(dock.tree.value), 1);
  });
  it("closing a TAB leaves the other pane's chrome alone (the pane stays)", async () => {
    const { t, main } = shellTreeTwoTabs();
    const dock = await dockOver(splitPane(t, main.id, "row", "after"));
    const mains = panes(dock.tree.value).filter((p) => p.edge === undefined);
    dock.closeTab(main.id, 0);
    await flush();
    // one tab left, the pane never left: the census still reads two
    assert.equal(mainPaneCount(dock.tree.value), 2, "the closed pane is still a main pane");
    const other = mains.find((p) => p.id !== main.id)!;
    assert.equal(dock.canRemovePane(other.id), true, "the neighbor keeps its X");
  });
});

describe("case 3: close-last via direct action call never removes the pane", () => {
  it("dock.remove on the last main pane changes nothing", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(t);
    const before = serialize(dock.tree.value);
    dock.remove(main.id);
    await flush();
    assert.equal(serialize(dock.tree.value), before, "the tree is untouched");
    assert.ok(findPane(dock.tree.value, main.id), "the pane never left");
  });
  it("dock.closeTab on the last main pane's lone tab is a refused no-op", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(t);
    const before = serialize(dock.tree.value);
    dock.closeTab(main.id, 0);
    await flush();
    assert.equal(serialize(dock.tree.value), before, "the locked tab changed nothing");
    assert.deepEqual(comps(findPane(dock.tree.value, main.id)!), ["chat"]);
    assert.equal(mainPaneCount(dock.tree.value), 1, "the main area still reads one");
  });
  it("movePane of the last main pane is a no-op, never a duplicate", () => {
    const { t, main } = shellTree();
    const shell = panes(t).find((p) => p.edge === "right")!;
    const out = movePane(t, main.id, shell.id, "before");
    assert.equal(out, t, "the refused detach never reaches the insert");
    assert.equal(
      panes(t).filter((p) => p.id === main.id).length,
      1,
      "no clone of the pane beside itself",
    );
  });
});

describe("the move-out exception: the last main pane survives its own last tab", () => {
  it("the drained source sits EMPTY (no placeholder is seated)", () => {
    const { t, main } = shellTree();
    const shell = panes(t).find((p) => p.edge === "right")!;
    // the chat tab walks into the pinned shell: the source empties
    const out = moveTab(t, main.id, 0, shell.id, shell.tabs.length);
    const srcPane = findPane(out, main.id);
    assert.ok(srcPane, "the last main pane never closes (t_aa9dbada exception)");
    assert.deepEqual(
      comps(srcPane!),
      [],
      "nothing is minted to fill it (card t_9855a20e: the seat retired)",
    );
    assert.equal(mainPaneCount(out), 1, "the main area never reaches zero panes");
    assert.deepEqual(comps(findPane(out, shell.id)!), ["files", "chat"], "the tab landed");
  });
  it("a non-last main pane still closes on its last tab out (the law unchanged)", () => {
    const a = makePane("chat");
    const b = makePane("chat", null);
    const t: DockNode = makeSplit("row", [a, b]);
    const out = moveTab(t, a.id, 0, b.id, 0);
    assert.ok(!findPane(out, a.id), "the emptied source closed");
    assert.equal(mainPaneCount(out), 1, "the survivor is the last main pane now");
  });
});

describe("the pane-seat repair through reload (card t_9855a20e)", () => {
  it("a persisted placeholder seat becomes a fresh new chat (not a blank pane)", async () => {
    const t: DockNode = makeSplit("row", [
      makePane("sidebar", 240, "left"),
      { kind: "pane", id: "n900", tabs: [legacySeat()], active: 0, fixed: null },
      makePane("files", 300, "right"),
    ]);
    const dock = await dockOver(t);
    const main = panes(dock.tree.value).find((p) => p.edge === undefined)!;
    assert.deepEqual(comps(main), ["chat"], "the retired seat lands as a new chat");
    assert.ok(main.tabs[0]!.key, "the chat tab carries its instance key");
    assert.ok(
      dock.bootRepairs.some((r) => r.includes("no live tab")),
      "the boot reports the seat repair",
    );
  });
  it("a persisted ZERO-tab pane gets the same repair", async () => {
    const t: DockNode = makeSplit("row", [
      makePane("sidebar", 240, "left"),
      { kind: "pane", id: "n901", tabs: [], active: 0, fixed: null },
      makePane("files", 300, "right"),
    ]);
    const dock = await dockOver(t);
    const main = panes(dock.tree.value).find((p) => p.edge === undefined)!;
    assert.deepEqual(comps(main), ["chat"], "a zero-tab record boots a chat, never a blank pane");
  });
  it("a persisted seat beside real content is simply dropped", async () => {
    const t: DockNode = makeSplit("row", [
      makePane("sidebar", 240, "left"),
      {
        kind: "pane",
        id: "n902",
        tabs: [legacySeat(), { component: "chat" }],
        active: 0,
        fixed: null,
      },
      makePane("files", 300, "right"),
    ]);
    const dock = await dockOver(t);
    const main = panes(dock.tree.value).find((p) => p.edge === undefined)!;
    assert.deepEqual(comps(main), ["chat"], "the stale seat retired, the content stayed");
    assert.equal(main.active, 0, "focus follows the content it held");
  });
  it("repairPaneSeats is idempotent (same object when settled)", () => {
    const t = makeSplit("row", [makePane("chat"), makePane("sidebar", 240, "left")]);
    assert.equal(repairPaneSeats(t), t, "a settled tree is returned as-is");

    const seated = makeSplit("row", [
      { kind: "pane", id: "n903", tabs: [legacySeat()], active: 0, fixed: null },
      makePane("sidebar", 240, "left"),
    ]);
    const repaired = repairPaneSeats(seated);
    assert.notEqual(repaired, seated, "the repair changed the tree");
    assert.deepEqual(comps(asPaneOf(repaired, "n903")), ["chat"]);
    assert.equal(repairPaneSeats(repaired), repaired, "idempotent on the repaired tree");
  });
  it("real content arriving on a repaired pane keeps the pane honest", () => {
    const seated = makeSplit("row", [
      { kind: "pane", id: "n904", tabs: [], active: 0, fixed: null },
      makePane("sidebar", 240, "left"),
    ]);
    const out = openTab(seated, "n904", { component: "settings" });
    assert.deepEqual(comps(asPaneOf(out, "n904")), ["settings"]);
  });
});

describe("the chrome wiring (DockPane source pins)", () => {
  it("the tab X rides component closability AND the pane's tab count", () => {
    const pane = src("app/components/DockPane.vue");
    // the tab close button renders behind tabClosable - NO census term
    // (t_b620f99d: the pane law must not feed the tab-close rendering)
    assert.match(pane, /v-if="tabClosable\(t\)"/);
    assert.doesNotMatch(
      pane,
      /tabClosable.*isLastMainPane/,
      "the census never feeds the tab X",
    );
    // card t_9855a20e: THE LOCKED LAST TAB - the lone tab renders no X
    assert.match(
      pane,
      /function tabClosable\(t: DockTab\): boolean \{[\s\S]*?isClosableTab\(t\.component\) && props\.node\.tabs\.length > 1/,
      "a pane's last tab carries no close X",
    );
    // the pane remove paths ride the store's census-guarded read
    assert.match(pane, /v-if="api\.canRemovePane\(node\.id\)"/);
  });
});
