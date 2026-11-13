// omega webui: card t_81d0901b - the MODULES surface is CONFIGURATION.
//
// This file SUPERSEDES the t_210a0d19 pin that lived here (module toggles
// OUT of settings, into the extensions cockpit tab): the ruling of
// 2026-11-12 reverses the placement, so the toggles are back in the settings
// tab as its own MODULES section and the secondary-pane tab is GONE. The
// pins below hold the new law:
//
//   1. the settings nav carries the MODULES section (settingsNav data);
//   2. the settings pane renders it and mounts the moved surface;
//   3. the row law survived the move intact - one-click toggle, busy gate,
//      consequence hint, refusal tail, the tools/commands/panels expansion;
//   4. the always-on pair renders as a VISIBLE non-toggleable CHIP row (the
//      refusal itself is the Go pin: api/server_modules_test.go's
//      TestModulesAlwaysOnPairRejectedAtTheSeam - the row only mirrors it);
//   5. module rows get NO settings-native row semantics (no modified dot, no
//      per-row reset, no scaffold-set flags): plain toggle + description;
//   6. /modules, /extensions and /panels open the settings pane and land on
//      that section;
//   7. the removed tab left no living reference behind.
//
// Structural source assertions: the node suite has no DOM (what is pinned is
// the wiring, not the paint). Run: node --test "test/*.test.ts"
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  MODULES_CATEGORY,
  buildNav,
  categoryCount,
  documentFor,
  modulesNavCategory,
} from "../app/components/settings/settingsNav.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

const settingsTab = read("app/components/settings/SettingsTab.vue");
const section = read("app/components/settings/ModulesSection.vue");
const rowModel = read("app/components/shell/extensionsRows.ts");
const app = read("app/app.vue");

// --- 1. the nav data --------------------------------------------------------

test("settingsNav carries the MODULES section (card t_81d0901b)", () => {
  assert.equal(MODULES_CATEGORY, "modules");
  const cat = modulesNavCategory(7);
  assert.equal(cat.id, MODULES_CATEGORY, "the category is the jump target's id");
  assert.equal(cat.label, "modules");
  assert.equal(cat.flat, true, "no child jump links: the section has no subgroups");
  assert.deepEqual(cat.knobs, [], "its rows are NOT knobs");
  assert.deepEqual(cat.subgroups, []);
  assert.equal(categoryCount(cat), 7, "the nav prints the live module-row count");
});

test("the nav counts knobs for knob categories and rows for the MODULES entry", () => {
  const knobs = [
    { key: "provider.host", label: "host", type: "text" as const, value: "" },
    { key: "provider.api_key", label: "api key", type: "secret" as const, value: "" },
  ];
  const nav = buildNav([{ key: "provider", label: "provider", knobs }]);
  assert.equal(categoryCount(nav[0]!), 2, "a knob category keeps counting knobs");
  assert.equal(categoryCount(modulesNavCategory(9)), 9);
});

test("the MODULES entry is a jump target, never a knob block in the document", () => {
  // documentFor walks KNOBS; the modules body is the component, so the
  // document must not grow an empty "modules" block (that would render a
  // heading with nothing under it, and the search filter would own it)
  const nav = buildNav([
    {
      key: "provider",
      label: "provider",
      knobs: [{ key: "provider.host", label: "host", type: "text" as const, value: "" }],
    },
  ]);
  const sections = documentFor([modulesNavCategory(4), ...nav]);
  assert.ok(!sections.some((s) => s.id === MODULES_CATEGORY), "no knob section for modules");
  assert.deepEqual(sections.map((s) => s.id), ["provider"], "the knob document is intact");
});

// --- 2. the settings pane renders the section ------------------------------

test("the settings pane renders the MODULES section and mounts the moved surface", () => {
  assert.match(settingsTab, /import ModulesSection from "\.\/ModulesSection\.vue"/);
  assert.match(settingsTab, /<ModulesSection/, "the section body is mounted");
  // the section is a real jump/anchor/spy target - the machinery reads these
  assert.match(
    settingsTab,
    /class="settings-category settings-modules-category"[\s\S]{0,120}data-section="modules"/,
    "the section carries the category + data-section the spy and the clamp read",
  );
  // the nav entry is PREPENDED, so the retired tab's reach survives the move
  assert.match(
    settingsTab,
    /modulesNavCategory\(moduleCount\.value\)/,
    "the MODULES entry heads the nav",
  );
  assert.match(settingsTab, /moduleCount = \$event/, "the section reports its row count");
  assert.match(settingsTab, /@open-module=/, "a has-UI row still opens its module tab");
});

test("the section follows the pane's own narrow measurement", () => {
  assert.match(settingsTab, /<ModulesSection[\s\S]{0,120}:narrow="narrow"/, "the pane passes its measurement down");
  assert.match(section, /class="settings-modules" :class="\{ narrow: narrow \}"/, "the section takes no second observer");
  assert.doesNotMatch(section, /useNarrow/, "no resize observer of its own");
});

test("a knob filter never pretends a module row is a hit", () => {
  // a live query and the modified-only switch narrow the KNOB document; the
  // module section steps aside, and the empty state never claims the pane is
  // empty while modules render
  assert.match(settingsTab, /const modulesShown = computed\(/, "one truth for the section's visibility");
  assert.match(settingsTab, /v-if="modulesShown"/, "the section is gated on it");
  assert.match(
    settingsTab,
    /totalCount === 0 && !modulesShown/,
    "the empty state accounts for the module rows",
  );
});

// --- 3. the row law survived the move ---------------------------------------

test("the rows still merge the registry introspection (GET /modules)", () => {
  assert.match(section, /fetchModules\(\)/, "the shared /modules client serves the live state");
  assert.match(section, /toggleModule\(row\.name, checked\)/, "the toggle round-trips the shared client");
  assert.match(section, /seamAvailable/, "a 501 toggle seam degrades the rows, never the inventory");
  assert.match(
    rowModel,
    /state: m \? m\.state : "shell"/,
    "an introspection-unknown row is the shell ring (tier law)",
  );
});

test("module rows keep the one-click toggle contract (single write, annotations)", () => {
  // card t_5051d523: the arm/confirm two-step is dead - one click commits.
  assert.match(section, /:disabled="busy !== null"/, "the busy gate (one flight at a time)");
  assert.match(
    section,
    /<TogglePill[\s\S]*?:state="row\.state === 'mounted'"/,
    "the shared TogglePill renders from live server state",
  );
  assert.doesNotMatch(section, /type="checkbox"/, "no checkbox (the :checked-prop trap stays dead)");
  assert.match(section, /toggleNote\(/, "the consequence text carries the law");
  assert.match(section, /:hint="rowHint\(row\)"/, "one click commits: the hint is a hover text, never a gate");
  assert.doesNotMatch(section, /:armed=|confirm-label|confirm: apply/, "no arm state, no confirm relabel");
  assert.match(section, /dependents: \$\{row\.dependents\.join/, "the dependents annotation rides the resting row");
  assert.match(section, /class="module-refusal"/, "the refusal tail rides the body (Q12)");
});

test("expansion reveals the module's tools, commands and panels", () => {
  assert.match(section, /:aria-expanded="isExpanded\(row\.name\)"/, "the expand button carries the a11y state");
  assert.match(section, /detail-label">tools</);
  assert.match(section, /detail-label">commands</);
  assert.match(section, /detail-label">panels</);
  assert.match(section, /row\.toolNames\.join/, "tool names render verbatim from the row model");
  assert.match(section, /row\.commandNames\.join/, "command names render verbatim from the row model");
  assert.match(section, /row\.panels\.join/, "panel titles render from the composition manifest");
  assert.match(section, /none \(disabled\)/, "an unmounted module says so instead of a bare none");
});

// --- 4. the always-on pair is a VISIBLE mirror ------------------------------

test("the always-on rows carry a chip, never a disabled switch", () => {
  // the refusal is the Go seam's (api/server_modules.go "module <name> is
  // always on"); this row only mirrors the registry's toggleable flag
  assert.match(section, /class="module-alwayson/, "the always-on chip exists");
  assert.match(section, />always on</, "the chip says what it means");
  assert.match(section, /v-else-if="row\.state === 'shell'"/, "the shell ring keeps its own badge");
  assert.match(
    section,
    /v-if="seamAvailable && row\.toggleable"/,
    "a non-toggleable row renders NO switch at all",
  );
  assert.doesNotMatch(
    section,
    /alwaysOnModules|ALWAYS_ON/,
    "the pair is not re-declared client-side - the seam owns the law",
  );
});

// --- 5. plain rows: no settings-native semantics ----------------------------

test("module rows carry no settings-native row semantics", () => {
  // NO modified dot, NO per-row reset, NO scaffold-set override flags: a
  // module row is a plain toggle + description over a persisted config write
  assert.doesNotMatch(section, /class="setting-row/, "the settings knob row class is not used");
  assert.doesNotMatch(section, /class="setting-dot/, "no modified dot");
  assert.doesNotMatch(section, /class="setting-reset/, "no per-row reset");
  assert.doesNotMatch(section, /isModified|canReset|serverKnobDirty/, "no settings-diff machinery");
  assert.doesNotMatch(section, /scaffold-row|overrideRow|override-row/i, "no scaffold-set override flags");
  assert.match(section, /class="extension-row reg-row"/, "the shared registry row is the row law");
});

// --- 6. the palette lands on the section ------------------------------------

test("the /modules command opens settings and jumps to the MODULES section", () => {
  assert.match(
    app,
    /case "\/modules":\s*\n\s*case "\/extensions":/,
    "the alias keeps riding the same case arm",
  );
  assert.match(
    app,
    /openSettingsTab\(\);/,
    "the command opens the settings tab",
  );
  assert.match(
    app,
    /jumpToSettingsSection\(MODULES_CATEGORY\)/,
    "and asks for the MODULES section",
  );
  assert.match(settingsTab, /applyJump\(\)/, "the pane consumes the pending jump");
  assert.match(
    settingsTab,
    /restoreAnchor\(\);[\s\S]{0,200}?syncSpy\(\);[\s\S]{0,400}?applyJump\(\)/,
    "a jump that arrived before the payload landed applies last, after the anchor",
  );
});

// --- 7. the removed tab left nothing behind ---------------------------------

test("the secondary-pane modules tab is gone from the shell", () => {
  assert.ok(
    !existsSync(`${root}/app/components/shell/ExtensionsTab.vue`),
    "the tab component is deleted (its surface moved, it was not copied)",
  );
  assert.doesNotMatch(app, /#pane-extensions/, "no outlet slot");
  assert.doesNotMatch(app, /ExtensionsTab/, "no import, no drawer branch");
  assert.doesNotMatch(app, /inspectorDrawerActiveId === 'extensions'/, "no drawer branch");
  assert.doesNotMatch(app, /openModuleTab\("extensions"\)/, "no dispatch onto the dead id");
  // the superseded placement comments must not survive as law
  assert.doesNotMatch(
    settingsTab,
    /t_210a0d19: the MODULES group is gone from settings/,
    "the reversed note is gone",
  );
  assert.doesNotMatch(
    settingsTab,
    /module toggles live in the extensions tab/,
    "no contradiction left in the tree",
  );
});
