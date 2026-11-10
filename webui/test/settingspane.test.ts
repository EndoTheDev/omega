// omega webui: unit tests for the settings-pane pure helpers
// (normalizeSettingsPayload, filterSettingGroups, settingsDiff).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  filterSettingGroups,
  knobDiffersFromDefault,
  knobHasDefault,
  normalizeSettingsPayload,
  settingsDiff,
  valuesEqual,
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
    const out = normalizeSettingsPayload(junk);
    assert.deepEqual(out.groups, []);
    // settings tier 3b: scopes is part of the payload and is ALWAYS an
    // array - a junk root answers the R8 empty world, never undefined
    assert.deepEqual(out.scopes, []);
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
// --- card t_c04c959e: the modified dot + reset-to-default pure helpers ---

test("normalize carries the knob default through, null included", () => {
  const out = normalizeSettingsPayload({
    groups: [{
      key: "g",
      label: "g",
      knobs: [
        { key: "a", label: "a", type: "number", value: 3, default: 7 },
        { key: "b", label: "b", type: "text", value: "x", default: "" },
        { key: "c", label: "c", type: "text", value: "m", default: null },
        { key: "d", label: "d", type: "text", value: "s" },
      ],
    }],
  });
  const knobs = out.groups[0].knobs;
  assert.equal(knobs[0].default, 7);
  assert.equal(knobs[1].default, ""); // an empty default is a REAL default
  assert.equal(knobs[2].default, null); // the server's no-default sentinel
  assert.equal(knobs[3].default, undefined); // an older server sends no field
});

test("knobDiffersFromDefault: identity, the empty/unset pair, structural content", () => {
  assert.equal(knobDiffersFromDefault(3, 3), false);
  assert.equal(knobDiffersFromDefault(3, 7), true);
  assert.equal(knobDiffersFromDefault(false, false), false);
  assert.equal(knobDiffersFromDefault(true, false), true);
  // "" default vs an unset/absent value is ONE state: no dot
  assert.equal(knobDiffersFromDefault("", ""), false);
  assert.equal(knobDiffersFromDefault(undefined, ""), false);
  assert.equal(knobDiffersFromDefault(null, ""), false);
  assert.equal(knobDiffersFromDefault("x", ""), true);
  // structural values (the providers list) compare by content
  assert.equal(knobDiffersFromDefault([], []), false);
  assert.equal(knobDiffersFromDefault([{ name: "a" }], [{ name: "a" }]), false);
  assert.equal(knobDiffersFromDefault([{ name: "a" }], []), true);
});

test("a knob with no known default is never modified (no dot, no reset)", () => {
  assert.equal(knobHasDefault(null), false);
  assert.equal(knobHasDefault(undefined), false);
  // the falsy defaults are REAL defaults, never the sentinel
  assert.equal(knobHasDefault(0), true);
  assert.equal(knobHasDefault(""), true);
  assert.equal(knobHasDefault(false), true);
  assert.equal(knobDiffersFromDefault("glm-5.3", null), false);
  assert.equal(knobDiffersFromDefault("glm-5.3", undefined), false);
});

test("normalize keeps the providers knob type", () => {
  const knob = {
    key: "provider.instances",
    label: "provider instances",
    type: "providers",
    value: [{ name: "backup", type: "openai", model_name: "gpt-4o", host: "", api_key: "****" }],
  };
  const out = normalizeSettingsPayload({
    groups: [{ key: "provider", label: "provider", knobs: [knob] }],
  });
  assert.equal(out.groups.length, 1);
  assert.equal(out.groups[0].knobs.length, 1);
  assert.equal(out.groups[0].knobs[0].type, "providers");
});

// valuesEqual: structural equality for object/array knob values - the
// providers list round-trips JSON-equal but never identity-equal.
test("valuesEqual compares structural values by content", () => {
  const row = { name: "backup", type: "openai", model_name: "gpt-4o", host: "", api_key: "****" };
  assert.equal(valuesEqual([row], JSON.parse(JSON.stringify([row]))), true);
  assert.equal(valuesEqual([row], [{ ...row, model_name: "o3" }]), false);
  assert.equal(valuesEqual([row], []), false);
  // scalars keep the identity path
  assert.equal(valuesEqual("a", "a"), true);
  assert.equal(valuesEqual("a", "b"), false);
  assert.equal(valuesEqual(1, 1), true);
  assert.equal(valuesEqual(null, null), true);
  assert.equal(valuesEqual(null, {}), false);
  assert.equal(valuesEqual(undefined, null), false);
});

test("settingsDiff ships an edited providers list and skips a JSON-equal one", () => {
  const row = { name: "backup", type: "openai", model_name: "gpt-4o", host: "", api_key: "****" };
  const loaded = { provider_instances: [row] };
  // round-tripped copy (fresh object identity) is NOT dirty
  assert.deepEqual(settingsDiff(loaded, { provider_instances: JSON.parse(JSON.stringify([row])) }), {});
  // a real edit IS dirty - removal and rename alike
  assert.deepEqual(settingsDiff(loaded, { provider_instances: [] }), { provider_instances: [] });
  const renamed = JSON.parse(JSON.stringify([row]));
  renamed[0].name = "second";
  assert.deepEqual(settingsDiff(loaded, { provider_instances: renamed }), { provider_instances: renamed });
});
