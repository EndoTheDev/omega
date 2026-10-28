// omega webui: the registry-browser SKILLS tab toggle behavior (v0.14.1,
// card t_372b9e9a). Pure-surface test like moduletoggle.test.ts: the
// helpers bind at the wire (GET /skills + GET/PUT /disabled), the
// source greps pin the component wiring (the composition-test
// convention - node --test has no DOM).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  normalizeDisabledPayload,
  withDisabledFlags,
  registryNote,
  filterRegistryRows,
  type RegistryRow,
} from "../app/composables/omega.ts";

// module UI source lives at the repo root (engine/webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
const read = (...p: string[]) => readFileSync(join(root, "engine", "webui", ...p), "utf8");

// --- normalizeDisabledPayload: a junk fold never kills the tab ---

test("disabled fold normalizer: lists pass through, junk collapses to []", () => {
  assert.deepEqual(
    normalizeDisabledPayload({ skills: ["a", "b"], tools: [], agents: ["main"] }),
    { skills: ["a", "b"], tools: [], agents: ["main"] },
  );
  // nulls and non-strings never ride the list
  assert.deepEqual(
    normalizeDisabledPayload({ skills: [null, 5, "a"] }),
    { skills: ["a"], tools: [], agents: [] },
  );
  // a junk payload is all-[]: the tab renders an empty fold, not a crash
  for (const junk of [null, 42, "x", [], { skills: "nope" }, { skills: [5] }]) {
    assert.deepEqual(normalizeDisabledPayload(junk), { skills: [], tools: [], agents: [] });
  }
});

// --- withDisabledFlags: the fold merges by name, fold-only names are ignored ---

test("skill rows merge the fold: disabled states land, inputs untouched", () => {
  const rows = withDisabledFlags(
    [
      { name: "skills.read", description: "read a skill" },
      { name: "dj-music", description: "manage the library" },
    ],
    ["dj-music"],
  );
  assert.equal(rows[0].disabled, false);
  assert.equal(rows[1].disabled, true);
  // the fold entry no row names is ignored (the server refuses
  // unknown-name disables, so this is dead fold state, not a phantom row)
  assert.equal(withDisabledFlags(rows, ["dj-music", "ghost"])[0].disabled, false);
});

// --- registryNote: the skills consequence names the prompt build ---

test("arming a skill disable names the prompt-build consequence; enable is silent", () => {
  assert.equal(registryNote("skills", "dj-music", false), "disable dj-music? excluded from the session prompt build");
  assert.equal(registryNote("skills", "dj-music", true), "");
});

// --- the tab renders the shared pattern and writes the shared wire ---

test("SkillsList consumes the shared component and the fold wire", () => {
  // module UI source lives at the repo root, engine/webui/test is three
  // levels under it
  const src = readFileSync(join(root, "modules", "skills", "ui", "SkillsList.vue"), "utf8");
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes('kind="skills"'), "the skills namespace rides to the note law");
  assert.ok(src.includes('fetch("/skills")'), "self-contained inventory fetch");
  assert.ok(src.includes("/disabled/skills"), "the toggle PUTs the skills namespace");
  assert.ok(src.includes("new Set(fold.value)"), "flags merge by name");
  assert.ok(!src.includes("composables/omega"), "no shell client dependency - module UI carries its own wiring");
  assert.ok(src.includes("status === 501") && src.includes("toggleable.value = false"), "a missing seam renders read-only");
});

// t_504a78ca regression: RegistryList gates with `v-if="rows.length"`.
// rows as a plain function makes .length the ARITY (0) - data loads,
// the tab renders empty. The rows binding must hand RegistryList an
// array (computed, the AgentsPanel shape), not a function.
test("SkillsList builds rows as a computed (arity trap: a plain function rows() renders empty)", () => {
  const src = readFileSync(join(root, "modules", "skills", "ui", "SkillsList.vue"), "utf8");
  assert.ok(src.includes("const rows = computed("), "rows computed, so :rows unwraps to the array");
  assert.ok(!src.includes("function rows()"), "no plain-function row builder under :rows");
  assert.ok(src.includes(':rows="rows"'), "the shared component gets the rows binding");
});

// --- filterRegistryRows: the shared narrowing contract ---

test("the row filter narrows by name or description", () => {
  const rows: RegistryRow[] = [
    { name: "skills.read", description: "read a skill", disabled: false },
    { name: "dj-music", description: "Library management", disabled: true },
  ];
  assert.deepEqual(
    filterRegistryRows(rows, "dj").map((r) => r.name),
    ["dj-music"],
  );
  assert.deepEqual(
    filterRegistryRows(rows, "LIBRARY").map((r) => r.name),
    ["dj-music"],
  );
  assert.equal(filterRegistryRows(rows, "  ").length, 2);
});