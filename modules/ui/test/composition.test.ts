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

test("the inspector mounts the generated module panels", () => {
  const sidebar = readFileSync(
    `${root}/app/components/files/RightSidebar.vue`,
    "utf8",
  );
  assert.ok(sidebar.includes("module-panels.generated"), "imports the generated list");
  assert.ok(sidebar.includes('class="module-panels"'), "has the mount section");
  // main-pane views are a switcher destination, not inspector content -
  // the mount v-fors the PANEL subset (omega.ts owns mainViewModules)
  assert.ok(sidebar.includes('v-if="mountedPanels.length"'), "renders nothing when no module ships a panel");
  assert.ok(sidebar.includes("!mainViewModules.has(p.module)"), "main-view modules stay out of the inspector");
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