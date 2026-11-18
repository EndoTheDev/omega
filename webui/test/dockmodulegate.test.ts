// omega webui: THE RUNTIME MODULE GATE - the dock half. A module toggled off in
// settings dismounts on the server live; its dockable's chip must leave the strip with
// it and return on remount. These tests pin the pure mechanics: the gate predicate
// (owned AND off), the parking (tab out of the tree, seat recorded, shell tabs
// untouched), the re-seat (same pane/seat on remount, no duplicate singleton), the
// stash reader, and the createDock wiring (a boot with a module off carries no ghost
// chip, and the parked seat survives the reload).

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  DOCKABLES,
  EMPTY_MODULE_GATE,
  compositionModuleIds,
  moduleGated,
  type ModuleGate,
} from "../app/composables/dockables.ts";
import {
  DOCK_LAYOUT_KEY,
  MODULE_STASH_KEY,
  createDock,
  findComponentTab,
  findEdgePane,
  findPane,
  makeSplit,
  parseParkedTabs,
  parkGatedTabs,
  reseatParkedTabs,
  serialize,
  type DockComponentName,
  type DockNode,
  type DockPane,
  type ParkedTab,
} from "../app/composables/dock.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..");
const read = (...p: string[]) => readFileSync(join(repoRoot, ...p), "utf8");

/** the gate under test: these ids are module-owned, these modules are off */
function gateOf(owned: string[], off: string[]): ModuleGate {
  return { owned: new Set(owned), off: new Set(off) };
}

/** a pane literal with the given seats (tests name their own pane ids) */
function paneOf(
  id: string,
  tabs: string[],
  active = 0,
  edge?: "left" | "right" | "bottom",
): DockPane {
  return {
    kind: "pane",
    id,
    tabs: tabs.map((component) => ({ component: component as DockComponentName })),
    active,
    fixed: null,
    ...(edge ? { edge } : {}),
  } as DockPane;
}

/** the inspector shell: [files, cron, skills] with cron active in the middle */
function inspectorPane(extra: Partial<DockPane> = {}): DockPane {
  return { ...paneOf("p-right", ["files", "cron", "skills"], 1, "right"), ...extra };
}

/** main pane + the inspector shell, the shipped shape in miniature */
function shell(pane: DockPane = inspectorPane()): DockNode {
  return makeSplit("row", [paneOf("p-main", ["chat"]), pane]);
}

function tabIds(tree: DockNode, paneId: string): string[] {
  const pane = findPane(tree, paneId);
  return pane ? pane.tabs.map((t) => t.component) : [];
}

// -- 1.

describe("the gate predicate: module-owned AND off", () => {
  it("hides a module-owned dockable whose module is off", () => {
    assert.equal(moduleGated("cron", gateOf(["cron"], ["cron"])), true);
  });

  it("keeps a module-owned dockable whose module runs", () => {
    assert.equal(moduleGated("cron", gateOf(["cron"], [])), false);
  });

  it("keeps a shell-owned pane even when its NAME sits in the off set", () => {
    // the runtime never owned "chat"/"agents"/"insights".
    const gate = gateOf(["cron"], ["chat", "agents", "insights"]);
    for (const id of ["chat", "agents", "insights"]) {
      assert.equal(moduleGated(id, gate), false, `${id} is shell-owned`);
    }
  });

  it("hides nothing under the empty gate", () => {
    for (const d of DOCKABLES) {
      assert.equal(moduleGated(d.id, EMPTY_MODULE_GATE), false, d.id);
    }
  });
});

// -- 2.

describe("the owned set is DERIVED from the composition manifests", () => {
  it("unions modulePanels + moduleTabs and invents no id", () => {
    const panels = [{ module: "cron" }, { module: "files" }, { module: "store" }];
    const tabs = [{ module: "terminal" }, { module: "browser" }];
    const owned = compositionModuleIds(panels, tabs);
    assert.deepEqual([...owned].sort(), ["browser", "cron", "files", "store", "terminal"]);
  });

  it("the real manifests own the capsule panes and NOT the shell's", () => {
    // the generated files on disk (nuxt prepare wrote them) - the gate must read the.
    const panelsSrc = read("webui", "app", "composables", "module-panels.generated.ts");
    const tabsSrc = read("webui", "app", "composables", "module-tabs.generated.ts");
    const modulesOf = (src: string) =>
      [...src.matchAll(/\{ module: "([^"]+)"/g)].map((m) => m[1]!);
    const owned = compositionModuleIds(
      modulesOf(panelsSrc).map((module) => ({ module })),
      modulesOf(tabsSrc).map((module) => ({ module })),
    );
    for (const capsule of ["cron", "skills", "tools", "kanban", "mcp", "terminal", "observability"]) {
      assert.equal(owned.has(capsule), true, `${capsule} ships its pane from a module`);
    }
    for (const shellOwned of ["chat", "viewer", "sidebar", "agents", "insights", "settings"]) {
      assert.equal(owned.has(shellOwned), false, `${shellOwned} is the shell's own pane`);
    }
  });

  it("tolerates a junk manifest entry instead of throwing", () => {
    const owned = compositionModuleIds([
      { module: "" },
      { module: "cron" },
    ] as { module: string }[]);
    assert.deepEqual([...owned], ["cron"]);
  });
});

// -- 3.

describe("parkGatedTabs: the chip leaves, the seat is remembered", () => {
  it("removes the gated tab and keeps the rest in order", () => {
    const res = parkGatedTabs(shell(), gateOf(["cron"], ["cron"]));
    assert.equal(res.changed, true);
    assert.deepEqual(tabIds(res.tree, "p-right"), ["files", "skills"]);
    assert.deepEqual(res.parked, [{ component: "cron", pane: "p-right", index: 1 }]);
  });

  it("moves the focus off a gated ACTIVE tab, never past the strip", () => {
    const res = parkGatedTabs(shell(), gateOf(["cron"], ["cron"]));
    const pane = findPane(res.tree, "p-right") as DockPane;
    assert.notEqual(pane.tabs[pane.active]?.component, "cron");
    assert.ok(pane.active >= 0 && pane.active < pane.tabs.length);
  });

  it("keeps a kept active tab active", () => {
    const pane = inspectorPane({ active: 2 }); // skills
    const res = parkGatedTabs(shell(pane), gateOf(["cron"], ["cron"]));
    const after = findPane(res.tree, "p-right") as DockPane;
    assert.equal(after.tabs[after.active]?.component, "skills");
  });

  it("is the SAME tree object when nothing is gated (no churn)", () => {
    const tree = shell();
    const res = parkGatedTabs(tree, gateOf(["cron"], []));
    assert.equal(res.changed, false);
    assert.equal(res.tree, tree);
    assert.deepEqual(res.parked, []);
  });

  it("parks across every pane, main ones included", () => {
    const tree = makeSplit("row", [paneOf("p-main", ["browser", "chat"]), inspectorPane()]);
    const res = parkGatedTabs(tree, gateOf(["cron", "browser"], ["cron", "browser"]));
    assert.deepEqual(tabIds(res.tree, "p-main"), ["chat"]);
    assert.deepEqual(
      res.parked.map((p) => p.component).sort(),
      ["browser", "cron"],
    );
  });
});

// -- 4.

describe("reseatParkedTabs: the chip comes back where it sat", () => {
  it("re-seats into the recorded pane and index on remount", () => {
    const parked: ParkedTab[] = [{ component: "cron", pane: "p-right", index: 1 }];
    const gated = parkGatedTabs(shell(), gateOf(["cron"], ["cron"]));
    const back = reseatParkedTabs(gated.tree, parked, gateOf(["cron"], []));
    assert.deepEqual(tabIds(back.tree, "p-right"), ["files", "cron", "skills"]);
    assert.deepEqual(back.parked, [], "the seat is consumed");
  });

  it("keeps a tab parked while its module is still off", () => {
    const parked: ParkedTab[] = [{ component: "cron", pane: "p-right", index: 1 }];
    const res = reseatParkedTabs(shell(inspectorPane({ tabs: [{ component: "files" }], active: 0 })), parked, gateOf(["cron"], ["cron"]));
    assert.deepEqual(tabIds(res.tree, "p-right"), ["files"]);
    assert.deepEqual(res.parked, parked);
  });

  it("falls back to the registry area when the old pane is gone", () => {
    // canvas C1: the fallback reads the registry HOME, which is the class's zone.
    const parked: ParkedTab[] = [{ component: "skills", pane: "p-gone", index: 0 }];
    const tree = shell(inspectorPane({ tabs: [{ component: "files" }], active: 0 }));
    const res = reseatParkedTabs(tree, parked, gateOf(["skills"], []));
    assert.deepEqual(tabIds(res.tree, "p-right"), ["skills", "files"]);
    // no home pane in the tree: the seat stays parked, never minted into a pane the.
    const noLeft = reseatParkedTabs(
      shell(inspectorPane({ tabs: [{ component: "files" }], active: 0 })),
      [{ component: "cron", pane: "p-gone", index: 0 }],
      gateOf(["cron"], []),
    );
    assert.deepEqual(noLeft.parked.map((p) => p.component), ["cron"]);
  });

  it("never duplicates a singleton that is already hosted", () => {
    const parked: ParkedTab[] = [{ component: "cron", pane: "p-right", index: 2 }];
    const tree = shell(); // cron is already there (re-opened from a menu)
    const res = reseatParkedTabs(tree, parked, gateOf(["cron"], []));
    assert.deepEqual(tabIds(res.tree, "p-right"), ["files", "cron", "skills"]);
    assert.deepEqual(res.parked, []);
  });

  it("drops a seat whose component the registry no longer hosts", () => {
    const parked: ParkedTab[] = [{ component: "gone-module", pane: "p-right", index: 0 }];
    const res = reseatParkedTabs(shell(), parked, gateOf([], []));
    assert.deepEqual(res.parked, []);
    assert.deepEqual(tabIds(res.tree, "p-right"), ["files", "cron", "skills"]);
  });
});

// -- 5.

describe("parseParkedTabs: a lenient reader", () => {
  it("reads the seats it wrote", () => {
    const raw = JSON.stringify([{ component: "cron", pane: "p1", index: 2 }]);
    assert.deepEqual(parseParkedTabs(raw), [{ component: "cron", pane: "p1", index: 2 }]);
  });

  it("returns nothing for a corrupt or empty record", () => {
    assert.deepEqual(parseParkedTabs(null), []);
    assert.deepEqual(parseParkedTabs("{ not json"), []);
    assert.deepEqual(parseParkedTabs('{"component":"cron"}'), []);
  });

  it("drops junk entries and repairs a bad index", () => {
    const raw = JSON.stringify([
      null,
      { component: "" },
      { component: "cron", pane: 7, index: -3 },
    ]);
    assert.deepEqual(parseParkedTabs(raw), [{ component: "cron", pane: "", index: 0 }]);
  });
});

// -- 6.

class MemoryStorage {
  private m = new Map<string, string>();
  getItem(k: string): string | null {
    return this.m.get(k) ?? null;
  }
  setItem(k: string, v: string): void {
    this.m.set(k, v);
  }
  removeItem(k: string): void {
    this.m.delete(k);
  }
}

describe("createDock: the gate is part of the store", () => {
  it("a boot with a module off carries no chip and parks the seat", async () => {
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, serialize(shell(), []));
    const dock = createDock(store as unknown as Storage);
    dock.setModuleGate(gateOf(["cron"], ["cron"]));
    assert.equal(findComponentTab(dock.tree.value, "cron"), null, "no ghost chip");
    assert.equal(JSON.parse(store.getItem(MODULE_STASH_KEY) ?? "[]").length, 1, "the seat is stashed");
    // the tree watch is the ONE writer - it lands on the next tick
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(
      (store.getItem(DOCK_LAYOUT_KEY) ?? "").includes('"cron"'),
      false,
      "the layout record carries no ghost pane",
    );
  });

  it("a remount re-seats the stashed tab in the same session", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    dock.setModuleGate(gateOf(["cron"], ["cron"]));
    assert.equal(findComponentTab(dock.tree.value, "cron"), null);
    dock.setModuleGate(gateOf(["cron"], []));
    assert.notEqual(findComponentTab(dock.tree.value, "cron"), null, "the chip is back");
    assert.equal(JSON.parse(store.getItem(MODULE_STASH_KEY) ?? "[]").length, 0);
  });

  it("a reload while off re-seats from the stash when the module returns", () => {
    const store = new MemoryStorage();
    const first = createDock(store as unknown as Storage);
    first.setModuleGate(gateOf(["cron"], ["cron"]));
    assert.equal(findComponentTab(first.tree.value, "cron"), null);

    // same browser, same localStorage: boot again with the module still off
    const second = createDock(store as unknown as Storage);
    second.setModuleGate(gateOf(["cron"], ["cron"]));
    assert.equal(findComponentTab(second.tree.value, "cron"), null);
    // ..
    second.setModuleGate(gateOf(["cron"], []));
    assert.notEqual(findComponentTab(second.tree.value, "cron"), null);
  });

  it("ignores an empty gate (nothing hidden, nothing stashed)", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    dock.setModuleGate(EMPTY_MODULE_GATE);
    assert.equal(JSON.parse(store.getItem(MODULE_STASH_KEY) ?? "[]").length, 0);
  });
});

// -- 6b.

describe("a reveal reintroduces a strip: the gate re-runs there too", () => {
  it("a seat parked behind a CLOSED edge reseats on reveal", () => {
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, serialize(shell(), []));
    const dock = createDock(store as unknown as Storage);
    const strip = findEdgePane(dock.tree.value, "inspector")!;
    assert.ok(strip, "boot seats the inspector shell");
    const before = tabIds(dock.tree.value, strip.id);

    // 1.
    dock.setModuleGate(gateOf(["skills"], ["skills"]));
    assert.equal(findComponentTab(dock.tree.value, "skills"), null);
    // 2.
    dock.toggleEdge("inspector", "after");
    assert.equal(findEdgePane(dock.tree.value, "inspector"), null, "the edge is closed");
    // 3. skills comes back while the strip is hidden: no live pane hosts it,
    //    so the seat STAYS parked rather than being dropped
    dock.setModuleGate(gateOf(["skills"], []));
    assert.equal(findComponentTab(dock.tree.value, "skills"), null, "still parked behind the closed edge");
    assert.deepEqual(
      parseParkedTabs(store.getItem(MODULE_STASH_KEY)).map((p) => p.component),
      ["skills"],
      "the seat survived the while-hidden remount",
    );
    // 4. the inspector reopens: the reveal re-runs the live gate, so the
    //    live module's chip is back in the strip in the same tick
    dock.toggleEdge("inspector", "after");
    const back = findEdgePane(dock.tree.value, "inspector")!;
    assert.ok(back, "the panel is back");
    assert.deepEqual(tabIds(dock.tree.value, back.id), before, "the chip is back in its old seat");
    assert.deepEqual(parseParkedTabs(store.getItem(MODULE_STASH_KEY)), [], "the seat is consumed");
  });

  it("a seat parked while the BOTTOM panel was closed reseats on reopen", () => {
    const store = new MemoryStorage();
    const dock = createDock(store as unknown as Storage);
    const strip = findEdgePane(dock.tree.value, "bottom")!;
    assert.ok(strip, "boot grafts the bottom shell");
    const before = tabIds(dock.tree.value, strip.id);
    assert.ok(before.includes("terminal"), "the bottom home hosts the terminal chip");

    dock.setModuleGate(gateOf(["terminal"], ["terminal"]));
    assert.equal(findComponentTab(dock.tree.value, "terminal"), null);
    dock.toggleBottom();
    assert.equal(findEdgePane(dock.tree.value, "bottom"), null, "the panel is closed");

    dock.setModuleGate(gateOf(["terminal"], []));
    assert.equal(findComponentTab(dock.tree.value, "terminal"), null, "parked while hidden");

    dock.toggleBottom();
    const back = findEdgePane(dock.tree.value, "bottom")!;
    assert.ok(back, "the panel is back");
    assert.deepEqual(tabIds(dock.tree.value, back.id), before, "the chip is back in its seat");
    assert.deepEqual(parseParkedTabs(store.getItem(MODULE_STASH_KEY)), []);
  });
});

// -- 7. the surfaces that must NOT be gated --------------------------------

describe("the shell's own panes stay outside the gate", () => {
  it("app.vue derives the owned set from the manifests and passes the live off set", () => {
    const src = read("webui", "app", "app.vue");
    assert.match(src, /compositionModuleIds\(modulePanels, moduleTabs\)/, "derived, never a hand list");
    assert.match(src, /rawDock\.setModuleGate\(currentModuleGate\(\)\)/, "applied before the first paint");
    assert.match(src, /hydrateModuleState\(window\.localStorage\)/, "the cache hydrates synchronously");
  });

  it("the settings modules section and the dock drink from ONE store", () => {
    const section = read("webui", "app", "components", "settings", "ModulesSection.vue");
    const state = read("webui", "app", "composables", "moduleState.ts");
    assert.match(section, /from "\.\.\/\.\.\/composables\/moduleState"/, "the store is the source");
    assert.match(state, /export async function commitModuleToggle/, "one write path");
    assert.match(state, /export async function loadModules/, "one read path");
    assert.doesNotMatch(state, /setInterval|setTimeout/, "no polling timer");
  });
});
