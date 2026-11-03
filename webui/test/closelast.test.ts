// omega webui: the last-main-pane law (Endo ruling 2026-10-25,
// corrected t_b620f99d: the law protects the PANE, not the tabs).
//
// The LAST main pane never closes its PANE: `removePane` refuses it as a
// no-op whatever carried the request in - button, shortcut, stale state
// (the guards read the LIVE tree). The main area never reaches zero
// panes; the empty launcher pane IS a main pane and stays.
//
// TAB close is a separate computation (t_b620f99d): every closable
// tab's X renders whatever the census says, and closing the LAST tab in
// the last main pane empties the pane into its launcher state - the
// pane never leaves the tree, so the law still holds.
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
  removePane,
  serialize,
  splitPane,
  DOCK_LAYOUT_KEY,
  paneCount,
  type DockNode,
  type DockPane,
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

describe("case 1: a single main pane - pane X hidden, tab X present", () => {
  it("removePane refuses it even with shells present (the old guard counted them)", () => {
    const { t, main } = shellTree();
    assert.equal(removePane(t, main.id), t, "the close request is a no-op");
    assert.ok(findPane(t, main.id), "the pane never left");
  });
  it("closeTab works on the LAST main pane: the law protects the pane, not the tabs", () => {
    const { t, main } = shellTree();
    const twoTabs: DockPane = {
      ...main,
      tabs: [{ component: "chat" }, { component: "chat" }],
    };
    const withTab: DockNode = makeSplit("row", [
      (t as { children: DockNode[] }).children[0]!,
      twoTabs,
      (t as { children: DockNode[] }).children[2]!,
    ]);
    const after = closeTab(withTab, twoTabs.id, 0);
    assert.notEqual(after, withTab, "tab 0 closes");
    assert.equal(findPane(after, twoTabs.id)!.tabs.length, 1, "one tab stays");
    const after2 = closeTab(after, twoTabs.id, 0);
    assert.equal(
      findPane(after2, twoTabs.id)!.tabs.length,
      0,
      "the LAST tab closes too",
    );
    assert.ok(findPane(after2, twoTabs.id), "the emptied pane never left the tree");
    assert.equal(mainPaneCount(after2), 1, "the launcher state is still a main pane");
  });
  it("the store still hides ONLY the pane X: canRemovePane false, tab close goes through", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(t);
    assert.equal(dock.canRemovePane(main.id), false, "no remove X on the last main pane");
    assert.equal(dock.isLastMainPane(main.id), true, "the pane guards on the census");
    dock.closeTab(main.id, 0);
    await flush();
    assert.equal(findPane(dock.tree.value, main.id)!.tabs.length, 0, "the tab closed");
    assert.ok(findPane(dock.tree.value, main.id), "the pane never left");
    assert.equal(mainPaneCount(dock.tree.value), 1, "the launcher stays a main pane");
  });
});

describe("case 2: two main panes - close on both, then the survivor loses it", () => {
  it("each pane may close while the other lives; closing one strips the survivor", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(splitPane(t, main.id, "row", "after"));
    const mains = panes(dock.tree.value).filter((p) => p.edge === undefined);
    assert.equal(mains.length, 2, "the split minted a second main pane");
    for (const p of mains) {
      assert.equal(dock.canRemovePane(p.id), true, "both carry the pane X");
      assert.equal(dock.isLastMainPane(p.id), false, "both carry the tab X");
    }
    // closing one pane: the survivor becomes the last main pane - its
    // pane X goes with the same tree swap; the TAB X never was
    // census-gated (t_b620f99d: the law protects the pane, not the tabs)
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
    const { t, main } = shellTree();
    const dock = await dockOver(splitPane(t, main.id, "row", "after"));
    const mains = panes(dock.tree.value).filter((p) => p.edge === undefined);
    dock.closeTab(main.id, 0);
    await flush();
    // the pane emptied but never left: the census still reads two
    assert.equal(mainPaneCount(dock.tree.value), 2, "the emptied pane is still a main pane");
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
  it("dock.closeTab on the last main pane closes the tab but never the pane", async () => {
    const { t, main } = shellTree();
    const dock = await dockOver(t);
    dock.closeTab(main.id, 0);
    await flush();
    assert.equal(findPane(dock.tree.value, main.id)!.tabs.length, 0, "the last tab closed");
    assert.ok(findPane(dock.tree.value, main.id), "the pane survives as the launcher");
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
  it("the emptied source stays as the launcher when it is the last main pane", () => {
    const { t, main } = shellTree();
    const shell = panes(t).find((p) => p.edge === "right")!;
    // the chat tab walks into the pinned shell: the source empties
    const out = moveTab(t, main.id, 0, shell.id, shell.tabs.length);
    const srcPane = findPane(out, main.id);
    assert.ok(srcPane, "the last main pane never closes (t_aa9dbada exception)");
    assert.equal(srcPane!.tabs.length, 0, "it sits empty as the launcher");
    assert.equal(mainPaneCount(out), 1, "the main area never reaches zero panes");
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

describe("the chrome wiring (DockPane source pins)", () => {
  it("the tab X rides component closability only; the pane X rides canRemovePane", () => {
    const pane = src("app/components/DockPane.vue");
    // the tab close button renders behind tabClosable - NO census term
    // (t_b620f99d: the pane law must not feed the tab-close rendering)
    assert.match(pane, /v-if="tabClosable\(t\)"/);
    assert.doesNotMatch(
      pane,
      /tabClosable.*isLastMainPane/,
      "the census never feeds the tab X",
    );
    // the pane remove paths ride the store's census-guarded read
    assert.match(pane, /v-if="api\.canRemovePane\(node\.id\)"/);
  });
});