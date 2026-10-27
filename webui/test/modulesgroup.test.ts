// omega webui: card t_8ac225f1 - the MODULES settings group collapses
// like the other ten. Pins the pure search narrowing (filterModuleRows
// in composables/omega.ts, same contract as filterSettingGroups) and
// the component wiring (structural source assertions: the node suite
// has no DOM, so what is pinned is the wiring, not the paint).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  filterModuleRows,
  type ModuleRow,
} from "../app/composables/omega.ts";

const row = (over: Partial<ModuleRow>): ModuleRow => ({
  name: "kanban",
  state: "mounted",
  toggleable: true,
  ...over,
});

const ROWS: ModuleRow[] = [
  row({ name: "kanban", description: "the task board" }),
  row({ name: "cron", state: "disabled" }),
  row({ name: "tools", description: "tool listing" }),
];

// --- filterModuleRows: the search box narrows the module rows ---

test("empty query returns every row unchanged", () => {
  assert.deepEqual(filterModuleRows(ROWS, ""), ROWS);
  assert.deepEqual(filterModuleRows(ROWS, "   "), ROWS);
});

test("query matches the row name case-insensitively and drops the rest", () => {
  const out = filterModuleRows(ROWS, "CRON");
  assert.equal(out.length, 1);
  assert.equal(out[0].name, "cron");
});

test("query matches the description too", () => {
  const out = filterModuleRows(ROWS, "task board");
  assert.equal(out.length, 1);
  assert.equal(out[0].name, "kanban");
});

test("no match anywhere yields zero rows (the group hides)", () => {
  assert.deepEqual(filterModuleRows(ROWS, "zzz"), []);
});

test("filtering never mutates the caller's rows", () => {
  filterModuleRows(ROWS, "cron");
  assert.equal(ROWS.length, 3);
});

// --- the component wiring: MODULES is a first-class settings group ---

const root = fileURLToPath(new URL("..", import.meta.url));
const list = readFileSync(
  `${root}/app/components/settings/ModuleToggleList.vue`,
  "utf8",
);
const tab = readFileSync(
  `${root}/app/components/settings/SettingsTab.vue`,
  "utf8",
);

test("MODULES collapses like other groups: same head button as the knob groups", () => {
  // byte-for-byte the SettingsTab group head's interaction: the shared
  // button class, the chevron prefix, aria-expanded, click toggles
  assert.ok(
    list.includes('class="settings-group-head"'),
    "the head rides button.settings-group-head (shared rules + styling)",
  );
  assert.match(list, /class="bucket-chevron"/, "the shared chevron prefix");
  assert.match(list, /:aria-expanded="!isCollapsed"/, "the a11y state");
  assert.match(list, /@click="toggleCollapsed"/, "whole header toggles");
  // body hidden while collapsed, exactly like the knob groups' loop
  assert.match(list, /v-if="!isCollapsed"/, "collapsed body hidden");
  // and the dead div-head is gone from the stylesheet
  assert.ok(
    !readFileSync(`${root}/app/assets/css/main.css`, "utf8").includes(
      ".modules-head",
    ),
    "no modules-specific head styles left",
  );
});

test("collapse state persists in the SHARED settings collapse storage", () => {
  assert.match(
    list,
    /COLLAPSED_KEY = "omega\.settings-collapsed"/,
    "the same localStorage key SettingsTab persists its groups under",
  );
  assert.match(
    list,
    /parseCollapsedKeys[\s\S]*serializeCollapsedKeys/,
    "the same round-trip helpers, so other groups' keys survive",
  );
  // the group's key in that shared set
  assert.match(
    list,
    /has\("modules"\)/,
    "restores from the shared set under key modules",
  );
  assert.match(list, /next\.add\("modules"\)/, "toggle writes key modules");
  assert.match(list, /next\.delete\("modules"\)/, "untoggle drops key modules");
});

test("search wins over collapse and narrows the module rows", () => {
  assert.match(
    list,
    /filterModuleRows/,
    "the shared narrowing helper narrows the rows",
  );
  assert.match(
    list,
    /v-if="loaded && \(!searching \|\| visibleRows\.length > 0\)"/,
    "a search that matched no rows hides the group (like the knob groups)",
  );
  assert.match(
    list,
    /collapsed\.value && !searching\.value/,
    "a live query forces the group open (search-wins rule)",
  );
});

test("SettingsTab hands the query down and counts MODULES in its empty state", () => {
  assert.match(
    tab,
    /<ModuleToggleList :query="query" @visible="onModulesVisible" \/>/,
    "the parent passes its live query and tracks the section's visibility",
  );
  assert.match(
    tab,
    /visibleGroups\.length === 0 && !modulesVisible/,
    "'no settings match' shows only when neither groups nor modules matched",
  );
});

test("neither writer clobbers the shared collapse set (rmw on both sides)", () => {
  // the persist is one localStorage key; a toggle that wrote from its
  // own in-memory set once wiped the other group's key between two
  // toggles (verify lane, card t_8ac225f1)
  assert.match(
    tab,
    /const stored =[\s\S]{0,80}parseCollapsedKeys\(localStorage\.getItem\(COLLAPSED_KEY\)\)/,
    "SettingsTab toggles start from the shared storage set",
  );
  const listToggle =
    list.split("function toggleCollapsed")[1]?.split("}")[0] ?? "";
  assert.match(
    listToggle,
    /parseCollapsedKeys\(localStorage\.getItem\(COLLAPSED_KEY\)\)/,
    "ModuleToggleList toggles read-modify-write the shared set too",
  );
});

test("module rows keep the E2 toggle contract (checkbox + annotations)", () => {
  // the collapse re-wrap must not have touched the row surface: the
  // disabled gate, the mounted-checked checkbox, always-on + dependents
  assert.match(
    list,
    /:disabled="busy !== null \|\| !m\.toggleable"/,
    "busy + always-on gate intact",
  );
  assert.match(list, /:checked="m\.state === 'mounted'"/, "live state checkbox");
  assert.match(list, /always on/, "the locked annotation");
  assert.match(
    list,
    /dependents: \$\{m\.dependents\.join/, 
    "the dependents annotation",
  );
  assert.match(list, /class="module-refusal"/, "the refusal tail rides the body");
});