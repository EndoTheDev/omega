// omega webui: unit tests for the module-UI composition - the scan that
// turns modules/<name>/ui/ into Nuxt component dirs with a namespace
// prefix, the generated panel list the inspector mounts, and (structurally,
// like the other component tests here) the canary's wiring. Run:
// node --test modules/ui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { inspectorPanelTabs, scanModuleUIs } from "../app/composables/compose.ts";
import { mainViewModules } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));

// --- scanModuleUIs: a directory is the registration ---

test("scanModuleUIs finds module ui dirs with their components", () => {
  const scratch = makeScratch();
  try {
    mkdirSync(join(scratch, "tools", "ui"), { recursive: true });
    writeFileSync(join(scratch, "tools", "ui", "ToolsList.vue"), "<template/>");
    const found = scanModuleUIs(scratch);
    assert.equal(found.length, 1);
    assert.equal(found[0].module, "tools");
    assert.deepEqual(found[0].components, ["ToolsList.vue"]);
  } finally {
    rm(scratch);
  }
});

test("scanModuleUIs skips empty ui dirs, dot dirs, and ui itself", () => {
  const scratch = makeScratch();
  try {
    // a module with an empty ui/ ships no UI - not a registration
    mkdirSync(join(scratch, "cron", "ui"), { recursive: true });
    // the shell module never registers inside its own shell
    mkdirSync(join(scratch, "ui", "ui"), { recursive: true });
    writeFileSync(join(scratch, "ui", "ui", "Loop.vue"), "<template/>");
    // tooling junk dirs are invisible
    mkdirSync(join(scratch, "node_modules", "ui"), { recursive: true });
    writeFileSync(join(scratch, "node_modules", "ui", "Junk.vue"), "<template/>");
    assert.deepEqual(scanModuleUIs(scratch), []);
  } finally {
    rm(scratch);
  }
});

test("scanModuleUIs results are sorted by module name", () => {
  const scratch = makeScratch();
  try {
    for (const name of ["tools", "kanban"]) {
      mkdirSync(join(scratch, name, "ui"), { recursive: true });
      writeFileSync(join(scratch, name, "ui", `${name}.vue`), "<template/>");
    }
    const found = scanModuleUIs(scratch);
    assert.deepEqual(found.map((m) => m.module), ["kanban", "tools"]);
  } finally {
    rm(scratch);
  }
});

test("scanModuleUIs tolerates a missing or unreadable modules root", () => {
  assert.deepEqual(scanModuleUIs("/definitely/not/here/o/omega"), []);
});

// --- ui/dockable.json: the module's dockable declaration (card t_30ee04fb) ---

test("scanModuleUIs reads a valid dockable.json alongside the components", () => {
  const scratch = makeScratch();
  try {
    mkdirSync(join(scratch, "kanban", "ui"), { recursive: true });
    writeFileSync(join(scratch, "kanban", "ui", "Board.vue"), "<template/>");
    writeFileSync(
      join(scratch, "kanban", "ui", "dockable.json"),
      JSON.stringify({ title: "board", policy: "singleton" }),
    );
    const found = scanModuleUIs(scratch);
    assert.equal(found.length, 1);
    assert.deepEqual(found[0].dockable, { title: "board", policy: "singleton" });
    assert.deepEqual(found[0].components, ["Board.vue"], "the declaration is not a component");
  } finally {
    rm(scratch);
  }
});

test("scanModuleUIs yields null dockable for absent, malformed or wrong-shaped declarations", () => {
  const scratch = makeScratch();
  try {
    mkdirSync(join(scratch, "a", "ui"), { recursive: true });
    writeFileSync(join(scratch, "a", "ui", "A.vue"), "<template/>");
    // absent -> null
    const a = scanModuleUIs(scratch)[0]!;
    assert.equal(a.dockable, null);

    writeFileSync(join(scratch, "a", "ui", "dockable.json"), "{not json");
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "malformed JSON");

    writeFileSync(join(scratch, "a", "ui", "dockable.json"), JSON.stringify({ policy: "weird" }));
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "unknown policy");

    writeFileSync(join(scratch, "a", "ui", "dockable.json"), JSON.stringify([]));
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "not an object");

    writeFileSync(
      join(scratch, "a", "ui", "dockable.json"),
      JSON.stringify({ icon: { paths: [] } }),
    );
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "icon with no ink");

    writeFileSync(join(scratch, "a", "ui", "dockable.json"), JSON.stringify(null));
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "null body");
  } finally {
    rm(scratch);
  }
});

// --- the canary wiring (structural): ToolsList ships from the tools
// module dir and the shell mounts whatever the generated list carries ---

test("the canary component lives in the tools module, not the shell", () => {
  const src = readFileSync(`${root}/../tools/ui/ToolsList.vue`, "utf8");
  assert.ok(src.includes('fetch("/tools")'), "self-contained GET /tools fetch");
  assert.ok(
    !src.includes("composables/omega"),
    "no shell client dependency - module UI carries its own wiring",
  );
  // the shell's inspector must stay GENERIC - it mounts the generated
  // list, never a module component by name
  assert.ok(
    !readFileSync(`${root}/app/components/files/RightSidebar.vue`, "utf8")
      .includes("ToolsList"),
  );
});

test("the inspector mounts the active tab's module panel", () => {
  const sidebar = readFileSync(
    `${root}/app/components/files/RightSidebar.vue`,
    "utf8",
  );
  assert.ok(sidebar.includes("module-panels.generated"), "imports the generated list");
  assert.ok(sidebar.includes('class="module-panels"'), "has the mount section");
  // the tab card (t_db430fea): the tab model comes from compose, only
  // the ACTIVE panel renders (lazy: fetch/poll while the tab is up)
  assert.ok(sidebar.includes("inspectorPanelTabs("), "tab model is the compose helper");
  assert.ok(sidebar.includes('v-if="activePanel"'), "renders the active panel only");
});

test("inspectorPanelTabs: labels, main-view filter, stable order", () => {
  const panels = [
    { module: "cron", dockable: { title: "Cron" } },
    { module: "http_channel", dockable: { title: "Logs" } },
    { module: "kanban", dockable: { title: "Board" } },
    { module: "tools", dockable: null },
  ];
  const tabs = inspectorPanelTabs(panels, mainViewModules);
  // main-view modules stay out of the inspector (card 084664b ruling)
  assert.deepEqual(
    tabs.map((t) => t.module),
    ["cron", "http_channel", "tools"],
  );
  // a dockable.json title wins, the module name is the fallback
  assert.deepEqual(
    tabs.map((t) => t.title),
    ["Cron", "Logs", "tools"],
  );
});

test("inspectorPanelTabs: empty module list, missing dockables", () => {
  assert.deepEqual(inspectorPanelTabs([], mainViewModules), []);
  assert.deepEqual(
    inspectorPanelTabs([{ module: "cron" }], mainViewModules),
    [{ module: "cron", title: "cron" }],
  );
});

test("nuxt.config wires the composition in", () => {
  const config = readFileSync(`${root}/nuxt.config.ts`, "utf8");
  assert.ok(config.includes("scanModuleUIs(modulesRoot)"), "scans the modules tree");
  assert.ok(config.includes("prefix: ui.module"), "registers module dirs with a namespace prefix");
  assert.ok(config.includes('"/tools"'), "dev-proxies the tools endpoint to the dev instance");
  assert.ok(config.includes("writeFileSync"), "generates the panel list at config-eval time");
});

// helpers
function makeScratch(): string {
  const dir = `${root}/test/.scratch-${Math.random().toString(36).slice(2)}`;
  mkdirSync(dir, { recursive: true });
  return dir;
}
function rm(dir: string) {
  rmSync(dir, { recursive: true, force: true });
}