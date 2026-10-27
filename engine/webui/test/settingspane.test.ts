// omega webui: unit tests for the settings-pane pure helpers
// (normalizeSettingsPayload, filterSettingGroups, settingsDiff).
// Run: node --test engine/webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  filterSettingGroups,
  normalizeSettingsPayload,
  settingsDiff,
  type SettingGroup,
} from "../app/composables/omega.ts";

// helper builds one group with one knob of the given shape
function group(key: string, knobs: unknown[], overrides: object = {}): SettingGroup {
  return { key, label: key, knobs: knobs as SettingGroup["knobs"], ...overrides };
}

function knob(key: string, label: string, value: unknown, help?: string): object {
  return { key, label, type: "text", value, ...(help ? { help } : {}) };
}

// --- normalizeSettingsPayload: a malformed response never kills the pane ---

test("normalize keeps well-formed groups and knobs", () => {
  const out = normalizeSettingsPayload({
    groups: [
      {
        key: "compaction",
        label: "compaction",
        knobs: [
          { key: "compaction.enabled", label: "enabled", type: "toggle", value: true },
        ],
      },
    ],
  });
  assert.equal(out.groups.length, 1);
  assert.equal(out.groups[0].knobs[0].key, "compaction.enabled");
});

test("normalize drops non-object entries and unnamed groups", () => {
  const out = normalizeSettingsPayload({
    groups: [null, "nope", { knobs: [] }, { key: "g", label: "g" }, {
      key: "ok",
      label: "ok",
      knobs: [null, { key: "k", label: "k", type: "toggle" }, {
        key: "k2",
        label: "k2",
        type: "bananas",
        value: 1,
      }],
    }],
  });
  // only the "ok" group survives; its missing-value and unknown-type
  // knobs fall out (a knob with no control to render is stale data)
  assert.equal(out.groups.length, 1);
  assert.equal(out.groups[0].knobs.length, 0);
});

test("normalize tolerates junk root payloads", () => {
  for (const junk of [null, 42, "x", {}, { groups: "nope" }, { groups: [5] }]) {
    assert.deepEqual(normalizeSettingsPayload(junk), { groups: [] });
  }
});

test("normalize keeps optional fields typed", () => {
  const out = normalizeSettingsPayload({
    groups: [{
      key: "g",
      label: "g",
      knobs: [{
        key: "n",
        label: "n",
        type: "number",
        value: 3,
        min: "junk",
        max: 10,
        step: 1,
        options: ["a", "b"],
        help: "h",
        live: true,
      }],
    }],
  });
  const k = out.groups[0].knobs[0];
  assert.equal(k.min, undefined); // non-number min is dropped
  assert.equal(k.max, 10);
  assert.equal(k.step, 1);
  assert.deepEqual(k.options, ["a", "b"]);
  assert.equal(k.help, "h");
  assert.equal(k.live, true);
});

// --- filterSettingGroups: the search box narrows label + help ---

const DEMO: SettingGroup[] = [
  group("compaction", [
    knob("compaction.enabled", "enabled", true, "summarize history when the budget is exceeded"),
    knob("compaction.keep_last", "keep last", 10),
  ]),
  group("memory", [knob("memory.enabled", "enabled", true)]),
];

test("empty query returns every group unchanged", () => {
  assert.deepEqual(filterSettingGroups(DEMO, ""), DEMO);
  assert.deepEqual(filterSettingGroups(DEMO, "   "), DEMO);
});

test("query matches label case-insensitively and drops empty groups", () => {
  const out = filterSettingGroups(DEMO, "KEEP LAST");
  assert.equal(out.length, 1);
  assert.equal(out[0].key, "compaction");
  assert.equal(out[0].knobs.length, 1);
});

test("query matches the help text (description) too", () => {
  const out = filterSettingGroups(DEMO, "summarize");
  assert.equal(out.length, 1);
  assert.equal(out[0].knobs[0].key, "compaction.enabled");
});

test("no match anywhere yields zero groups (the empty state)", () => {
  assert.deepEqual(filterSettingGroups(DEMO, "zzz"), []);
});

test("filtering never mutates the caller's groups", () => {
  filterSettingGroups(DEMO, "keep last");
  assert.equal(DEMO[0].knobs.length, 2);
});

// --- settingsDiff: only changed knobs go on the wire ---

test("diff lists only keys whose draft differs from loaded", () => {
  const out = settingsDiff(
    { a: 1, b: true, c: "x" },
    { a: 2, b: true, c: "x", d: "new" },
  );
  assert.deepEqual(out, { a: 2, d: "new" });
});

test("identical draft and loaded diff to an empty map", () => {
  assert.deepEqual(settingsDiff({ a: 1 }, { a: 1 }), {});
});