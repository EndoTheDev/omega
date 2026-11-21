// omega webui: the sidebar tab's Hide and the strip's restore list.
//
// Hiding is a VISIBILITY FLAG, never a close: the tab keeps its seat and its pane, the chip
// stops rendering, and the flag persists in the layout record beside the tree. The strip is
// the surface that stays visible, so it owns the restore list, and it always keeps one chip.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  dockTabMenuItems,
  hiddenTabMenuItems,
} from "../app/composables/dockables.ts";
import {
  createDock,
  deserializeLayout,
  findEdgePane,
  serialize,
  type DockPane,
} from "../app/composables/dock.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

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
  key(i: number): string | null {
    return [...this.m.keys()][i] ?? null;
  }
  get length(): number {
    return this.m.size;
  }
}

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

function noopDeps() {
  const calls: string[] = [];
  return {
    calls,
    close: () => calls.push("close"),
    closeOthers: () => calls.push("close-others"),
    hide: () => calls.push("hide"),
  };
}

describe("the tab menu's Hide row", () => {
  it("is absent when the strip would be left with no chip", () => {
    const items = dockTabMenuItems(
      { tabs: [{ component: "files" }] },
      0,
      { ...noopDeps(), visibleCount: 1 },
    );
    assert.deepEqual(items, [], "the last visible chip cannot hide");
  });

  it("is offered on a secondary-sidebar chip while another chip stays", () => {
    const d = noopDeps();
    const items = dockTabMenuItems(
      { tabs: [{ component: "files" }, { component: "skills" }] },
      0,
      { ...d, visibleCount: 2 },
    );
    assert.deepEqual(ids(items), ["tab:hide"]);
    (items[0] as { run: () => void }).run();
    assert.deepEqual(d.calls, ["hide"]);
  });

  it("never appears where the surface passes no hide writer (main pane, bottom, primary)", () => {
    const items = dockTabMenuItems(
      { tabs: [{ component: "chat" }, { component: "viewer" }] },
      0,
      noopDeps(),
    );
    assert.deepEqual(ids(items), ["tab:close", "tab:close-others"]);
  });
});

describe("the strip's restore list", () => {
  it("names each hidden tab of this pane with Show <title>", () => {
    const shown: string[] = [];
    const rows = hiddenTabMenuItems(
      { tabs: [{ component: "files" }, { component: "skills" }, { component: "tools" }] },
      ["files", "tools"],
      (c) => c.toUpperCase(),
      (c) => shown.push(c),
    );
    assert.deepEqual(ids(rows), ["tab:show:files", "tab:show:tools"]);
    assert.deepEqual(
      rows.map((r) => (r as { label: string }).label),
      ["Show FILES", "Show TOOLS"],
    );
    (rows[1] as { run: () => void }).run();
    assert.deepEqual(shown, ["tools"]);
  });

  it("is empty when nothing is hidden, so the right-click stays the browser's", () => {
    assert.deepEqual(
      hiddenTabMenuItems({ tabs: [{ component: "files" }] }, [], (c) => c, () => {}),
      [],
    );
  });
});

describe("the store: hide, keep the seat, persist", () => {
  it("flips one flag and writes it into the layout record", () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    assert.deepEqual([...dock.hiddenTabs.value], []);
    dock.setTabHidden("files", true);
    assert.deepEqual([...dock.hiddenTabs.value], ["files"]);
    assert.deepEqual(
      JSON.parse(store.getItem("omega.dock-layout")!).hiddenTabs,
      ["files"],
    );
    // the tab KEEPS its seat: hiding never touched the tree
    const pane = findEdgePane(dock.tree.value, "inspector") as DockPane;
    assert.ok(pane.tabs.some((t) => t.component === "files"), "the seat survives the hide");
    dock.setTabHidden("files", false);
    assert.deepEqual([...dock.hiddenTabs.value], []);
  });

  it("survives a reload: the second store boots with the chip still hidden", () => {
    const store = new MemoryStorage();
    createDock(store).setTabHidden("skills", true);
    const second = createDock(store);
    assert.deepEqual([...second.hiddenTabs.value], ["skills"]);
  });

  it("opening a view from the outside reveals its chip", () => {
    const dock = createDock(new MemoryStorage());
    const pane = findEdgePane(dock.tree.value, "inspector") as DockPane;
    dock.setTabHidden("files", true);
    dock.openSingleton(pane.id, "files");
    assert.deepEqual([...dock.hiddenTabs.value], [], "opening a view asks to see it");
  });

  it("the record round-trips a hidden-tab set", () => {
    const store = new MemoryStorage();
    const dock = createDock(store);
    const raw = serialize(dock.tree.value, [], 1200, undefined, ["files"]);
    assert.deepEqual(deserializeLayout(raw)?.hiddenTabs, ["files"]);
  });
});

describe("the wiring: the chip renders only when visible, the strip holds the restore", () => {
  const pane = read("webui", "app", "components", "DockPane.vue");

  it("a hidden chip does not render, and the body follows the active VISIBLE chip", () => {
    assert.match(pane, /v-if="!tabHidden\(t\)"/);
    assert.match(pane, /function tabHidden\(tab: DockTab\): boolean \{/);
    assert.match(pane, /const activeIndex = computed\(\(\) => \{/);
    assert.match(pane, /:tab="activeTab"/);
  });

  it("the strip right-click is the restore door, registered into the one shared menu", () => {
    assert.match(pane, /@contextmenu="onStripContextMenu"/);
    assert.match(pane, /hiddenTabMenuItems\(/);
    assert.match(
      pane,
      /if \(paneZone\.value !== "right"\) return;/,
      "the rule is the secondary sidebar's strip",
    );
  });

  it("Hide is offered only on the secondary sidebar's chips", () => {
    assert.match(pane, /const secondary = paneZone\.value === "right";/);
    assert.match(pane, /hide: \(\) => api\.setTabHidden\(/);
  });
});
