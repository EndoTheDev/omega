// omega webui: card t_210a0d19 - the module toggles MOVE out of settings.
// The MODULES settings group is gone; the EXTENSIONS tab is the module
// lifecycle cockpit (live state + toggle on every row, expansion reveals
// the module's tools, commands and panels). Pins the swap in one pass:
// the settings side must show no module references, the extensions side
// must carry the toggle law the settings group obeyed. Structural source
// assertions (the node suite has no DOM: what is pinned is the wiring,
// not the paint). Run: node --test engine/webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

const tab = read("app/components/settings/SettingsTab.vue");
const extTab = read("app/components/shell/ExtensionsTab.vue");
const rowModel = read("app/components/shell/extensionsRows.ts");

// --- the settings side: MODULES is gone, the knobs stay ---

test("the MODULES section is removed from settings entirely", () => {
  assert.ok(
    !existsSync(`${root}/app/components/settings/ModuleToggleList.vue`),
    "the old settings toggle component is deleted (the surface moved)",
  );
  assert.ok(
    !tab.includes("ModuleToggleList"),
    "SettingsTab no longer mounts the module list",
  );
  assert.ok(
    !tab.includes("modulesVisible"),
    "the empty-state truth helper for the module group is gone",
  );
  assert.ok(
    !read("app/composables/omega.ts").includes("filterModuleRows"),
    "the module search narrowing is gone (the extensions tab has no search)",
  );
});

test("settings keeps only config knobs, and search narrows them", () => {
  // the removal must not have touched the knob surface
  assert.match(tab, /filterSettingGroups\(allGroups\.value, query\.value\)/,
    "the knob groups still narrow under the live query");
  assert.match(tab, /class="settings-group"/, "the knob group renderer stands");
  // the empty state is groups-only again
  assert.match(
    tab,
    /visibleGroups\.length === 0"[\s\S]{0,80}no settings match/,
    "'no settings match' keys off the knob groups alone",
  );
});

test("the handoff note rides the settings source", () => {
  assert.match(
    tab,
    /t_210a0d19: the MODULES group is gone from settings/,
    "the reader learns where the toggles went without the board",
  );
});

// --- the extensions side: the toggle law lives there now ---

test("the extensions tab rows merge the registry introspection", () => {
  // the row model fuses the three sources; the component fetches both
  // endpoints and never rebuilds the mount API client (scope item 4)
  assert.match(
    extTab,
    /fetchModules\(\)/,
    "the shared /modules client serves the cockpit's state",
  );
  assert.match(
    extTab,
    /toggleModule\(row\.name, checked\)/,
    "the toggle round-trips the shared client (no rebuilt mount API)",
  );
  assert.match(
    extTab,
    /seamAvailable/,
    "a 501 toggle seam degrades the rows, never the inventory",
  );
  assert.match(
    rowModel,
    /state: m \? m\.state : "engine"/,
    "an introspection-unknown row is engine ring (tier law)",
  );
});

test("module rows keep the E2 toggle contract (two-step arm, annotations)", () => {
  // the same law the settings group obeyed: the busy gate, the
  // mounted-checked checkbox, the arm-then-apply confirm, the Q12 tail
  assert.match(
    extTab,
    /:disabled="busy !== null"/,
    "the busy gate (one flight at a time)",
  );
  assert.match(
    extTab,
    /:checked="row\.state === 'mounted'"/,
    "the checkbox re-renders from live server state",
  );
  assert.match(extTab, /toggleNote\(/, "the confirm note carries the law");
  assert.match(
    extTab,
    /confirm: apply (disable|enable)\?/,
    "two-step arm: the second click answers a question",
  );
  assert.match(
    extTab,
    /dependents: \$\{row\.dependents\.join/,
    "the dependents annotation rides the resting row",
  );
  assert.match(
    extTab,
    /class="module-refusal"/,
    "the refusal tail rides the body (Q12: rejections visible as events)",
  );
});

test("engine ring rows never toggle (the tier law stays visual)", () => {
  assert.match(extTab, /extension-engine/, "the engine badge exists");
  assert.match(extTab, /row\.state === 'engine'/, "engine rows branch off");
});

test("expansion reveals the module's tools, commands and panels", () => {
  // the aria contract + the detail sections keyed off the row model
  assert.match(
    extTab,
    /:aria-expanded="isExpanded\(row\.name\)"/,
    "the expand button carries the a11y state",
  );
  assert.match(extTab, /detail-label">tools</, "the tools section");
  assert.match(extTab, /detail-label">commands</, "the commands section");
  assert.match(extTab, /detail-label">panels</, "the panels section");
  assert.match(
    extTab,
    /row\.toolNames\.join/,
    "the tool names render verbatim from the row model",
  );
  assert.match(
    extTab,
    /row\.commandNames\.join/,
    "the command names render verbatim from the row model",
  );
  assert.match(
    extTab,
    /row\.panels\.join/,
    "the panel titles render from the composition manifest",
  );
  // a disabled module owns the honest empty detail
  assert.match(extTab, /none \(disabled\)/,
    "an unmounted module says so instead of a bare none");
});

// --- the rename law (Endo's ruling, unpark 2026-10-28): the tab and
// the command are /modules; /extensions stays as an alias ---

test("the inspector tab is labelled Modules, the dock id stays extensions", () => {
  const dockables = read("app/composables/dockables.ts");
  assert.match(
    dockables,
    /id: "extensions",[\s\S]{0,400}title: "Modules"/,
    "the tab label is Modules; the id stays (stored layouts persist ids)",
  );
  assert.ok(
    !dockables.includes('id: "modules"'),
    "no new dock id - the rename is display-only",
  );
});

test("the /modules command is the name, /extensions rides as the alias", () => {
  const commands = read("app/composables/commands.ts");
  const host = read("app/app.vue");
  assert.match(
    commands,
    /name: "\/modules"/,
    "the palette carries /modules as the primary spelling",
  );
  assert.match(
    commands,
    /name: "\/extensions"/,
    "the alias keeps its palette row (no muscle-memory break)",
  );
  // the host dispatches both spellings onto the same cockpit tab
  assert.match(
    host,
    /case "\/modules":\s*\n\s*case "\/extensions":/,
    "the alias routes through the same case arm",
  );
});