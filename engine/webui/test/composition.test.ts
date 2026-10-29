// omega webui: unit tests for the module-UI composition - the scan that
// turns modules/<name>/ui/ into Nuxt component dirs with a namespace
// prefix, the generated panel list the inspector mounts, and (structurally,
// like the other component tests here) the canary's wiring. Run:
// node --test engine/webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { scanModuleUIs } from "../app/composables/compose.ts";

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

test("scanModuleUIs covers the engine ring: second root registers engine ui dirs, never the shell tree", () => {
  const scratch = makeScratch();
  const engine = makeScratch();
  try {
    mkdirSync(join(scratch, "tools", "ui"), { recursive: true });
    writeFileSync(join(scratch, "tools", "ui", "ToolsList.vue"), "<template/>");
    // engine ring: the second root is engine/; http_channel ships ui/ in it
    mkdirSync(join(engine, "http_channel", "ui"), { recursive: true });
    writeFileSync(join(engine, "http_channel", "ui", "LogsList.vue"), "<template/>");
    // the shell dir never registers - the shell ships its UI in place
    // (engine/webui/app); an engine-root scan must not re-register it
    mkdirSync(join(engine, "webui", "app"), { recursive: true });
    writeFileSync(join(engine, "webui", "app", "Shell.vue"), "<template/>");
    const found = scanModuleUIs(scratch, engine);
    assert.deepEqual(found.map((f) => f.module), ["http_channel", "tools"]);
  } finally {
    rm(scratch);
    rm(engine);
  }
});

test("scanModuleUIs skips empty ui dirs, dot dirs, and the shell dir", () => {
  const scratch = makeScratch();
  try {
    // a module with an empty ui/ ships no UI - not a registration
    mkdirSync(join(scratch, "cron", "ui"), { recursive: true });
    // the shell dir never registers under a module name - the shell
    // ships its UI in place (engine/webui/app), registering "webui" would
    // double-register the shell inside its own shell
    mkdirSync(join(scratch, "webui", "ui"), { recursive: true });
    writeFileSync(join(scratch, "webui", "ui", "Loop.vue"), "<template/>");
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
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "icon not a name (shape)");

    writeFileSync(
      join(scratch, "a", "ui", "dockable.json"),
      JSON.stringify({ icon: "not-a-name" }),
    );
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "icon outside the iconset");

    writeFileSync(join(scratch, "a", "ui", "dockable.json"), JSON.stringify(null));
    assert.equal(scanModuleUIs(scratch)[0]!.dockable, null, "null body");
  } finally {
    rm(scratch);
  }
});

// --- the canary wiring (structural): ToolsList ships from the tools
// module dir and the shell mounts whatever the generated list carries ---

test("the canary component lives in the tools module, not the shell", () => {
  const src = readFileSync(`${root}/../../modules/tools/ui/ToolsList.vue`, "utf8");
  assert.ok(src.includes('fetch("/tools")'), "self-contained GET /tools fetch");
  // card t_73994e55 -> t_372b9e9a: a domain tool's actions[] renders as
  // the row's EXPANDABLE detail lines (memory.add - description), the
  // RegistryList contract; the list itself is the shared component
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes('from "./toolRows"'), "the dotted action detail naming lives in the pure row model (t_e06b38bc grouping)");
  assert.ok(
    !src.includes("composables/omega"),
    "no shell client dependency - module UI carries its own wiring",
  );
  // the shell's inspector host must stay GENERIC - it resolves the panel
  // by module NAME (the dock tab id), never a module component by name
  assert.ok(
    !readFileSync(`${root}/app/components/files/InspectorPanel.vue`, "utf8")
      .includes("ToolsList"),
  );
});

test("the inspector's module tab hosts only the active panel's module", () => {
  const panelVue = readFileSync(
    `${root}/app/components/files/InspectorPanel.vue`,
    "utf8",
  );
  assert.ok(panelVue.includes("module-panels.generated"), "imports the generated list");
  assert.ok(panelVue.includes('class="module-panels"'), "has the mount section");
  // card t_4ae27e0d: the tab bar lives in the dock tabbar; the host only
  // resolves THIS tab's module body. The dock unmounts inactive tabs'
  // outlets, so lazy (fetch/poll while up) carries over.
  assert.ok(panelVue.includes('v-if="panel"'), "renders the resolved panel only");
  // the module tab ids ARE the composition's module names (no alias map)
  assert.ok(panelVue.includes("module === props.module"));
});

test("nuxt.config wires the composition in", () => {
  const config = readFileSync(`${root}/nuxt.config.ts`, "utf8");
  assert.ok(config.includes("scanModuleUIs(modulesRoot, engineRoot)"), "scans the modules tree + the engine ring");
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