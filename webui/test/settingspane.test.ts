// omega webui: unit tests for the settings-pane pure helpers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

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

// normalizeSettingsPayload: a malformed response never kills the pane

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

// filterSettingGroups: the search box narrows label + help

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

// settingsDiff: only changed knobs go on the wire

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
// the modified dot + reset-to-default pure helpers

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

// --- the desktop instances editor: the panel's write surface moved here -----
//
// The saved-connection list has ONE write surface: this pane. The editor emits the
// WHOLE array into the pane's draft, the pane's existing save path submits it, and the
// server's refusal (config.yaml desktop.instances -> core.NormalizeInstances) renders
// as written. These are source pins: node --test has no DOM and no SFC compiler.

const repoRoot = fileURLToPath(new URL("..", import.meta.url));
const paneSource = readFileSync(`${repoRoot}/app/components/settings/SettingsTab.vue`, "utf8");
const instancesField = readFileSync(
  `${repoRoot}/app/components/settings/InstancesField.vue`,
  "utf8",
);

test("normalize keeps the instances knob type", () => {
  const out = normalizeSettingsPayload({
    groups: [
      {
        key: "desktop",
        label: "desktop",
        knobs: [
          {
            key: "desktop.instances",
            label: "instances",
            type: "instances",
            value: [{ name: "laptop", url: "http://127.0.0.1:8099" }],
            default: [],
          },
        ],
      },
    ],
  });
  assert.equal(out.groups.length, 1);
  assert.equal(out.groups[0]!.knobs.length, 1, "an unlisted type would drop the knob");
  assert.equal(out.groups[0]!.knobs[0]!.type, "instances");
});

test("settingsDiff ships an edited connection list and skips a JSON-equal one", () => {
  const row = { name: "laptop", url: "http://127.0.0.1:8099" };
  const loaded = { "desktop.instances": [row] };
  // a round-tripped copy (fresh object identity) is NOT dirty
  assert.deepEqual(
    settingsDiff(loaded, { "desktop.instances": JSON.parse(JSON.stringify([row])) }),
    {},
  );
  // add, rename and remove are all real edits
  assert.deepEqual(settingsDiff(loaded, { "desktop.instances": [] }), {
    "desktop.instances": [],
  });
  const renamed = [{ ...row, name: "desk" }];
  assert.deepEqual(settingsDiff(loaded, { "desktop.instances": renamed }), {
    "desktop.instances": renamed,
  });
});

test("the pane dispatches the instances knob type to its editor", () => {
  assert.match(
    paneSource,
    /<InstancesField\s+v-else-if="knob\.type === 'instances'"/,
    "the instances knob type renders InstancesField",
  );
  assert.match(paneSource, /:value="instancesValue\(knob\)"/);
  assert.match(paneSource, /@update="setServer\(knob, \$event\)"/, "one write path: the pane's draft");
  assert.match(
    paneSource,
    /:error="fieldError\(knob\)"/,
    "the server's refusal reaches the field as written",
  );
  // the foot line yields to the field's own alert line
  assert.match(paneSource, /!inlineErrorShown/, "no second copy of the same sentence");
});

test("instancesValue coerces the rows tolerantly", () => {
  const at = paneSource.indexOf("const instancesValue");
  assert.ok(at > 0, "the pane carries instancesValue");
  const body = paneSource.slice(at, paneSource.indexOf("// fieldError", at));
  assert.match(body, /if \(!Array\.isArray\(v\)\) return \[\];/, "an unexpected kind degrades to no rows");
  assert.match(body, /typeof r\["name"\] === "string"/);
  assert.match(body, /typeof r\["url"\] === "string"/);
});

test("adding a row goes through the probe, and the hatch stamps the flag", () => {
  assert.match(instancesField, /@click="addRow"/, "one add row");
  assert.match(instancesField, /ref\(""\)/, "the add row is its own draft, never a blank submitted row");
  // the add action probes first and emits the list ONLY when one row came back added
  assert.match(instancesField, /await addInstanceRow\(rows\.value/, "the add proves the address first");
  assert.match(instancesField, /if \(!outcome\.added\)/, "a refusal appends nothing");
  assert.match(instancesField, /emit\("update", outcome\.list\)/, "the passing probe's list is the write");
  // the refusal renders inline, and the hatch rides beside it
  assert.match(instancesField, /class="instance-error instance-probe-error"/, "the refusal line is the field's own alert");
  assert.match(instancesField, /role="alert">\s*<span class="instance-probe-reason">/, "the reason prints as the serve wrote it");
  assert.match(instancesField, /class="instance-anyway"/, "the hatch sits beside the reason");
  assert.match(instancesField, /@click="addAnyway"/);
  assert.match(instancesField, /hatchInstanceRow\(/, "the hatch lands the row stamped");
  assert.match(
    instancesField,
    /refusal\.value\.failure !== "syntax"/,
    "a shape the file refuses gets no hatch - it could never be saved",
  );
  // the busy state and the marker
  assert.match(instancesField, /checking \? "checking\.\.\." : "add connection"/);
  assert.match(instancesField, /v-if="row\.unverified"/, "the row says it was added anyway");
});

test("removing a row emits the whole list", () => {
  assert.match(
    instancesField,
    /emit\("update", removeInstance\(rows\.value, row\.url\)\)/,
    "remove emits the WHOLE array through the shared remover",
  );
  assert.match(instancesField, /@click="removeRow\(i\)"/, "one remove button per row");
});

test("the editor re-validates nothing the server would refuse", () => {
  assert.doesNotMatch(instancesField, /normalizeInstanceName|normalizeInstanceURL/);
  assert.doesNotMatch(instancesField, /throw new Error/);
});

test("the refusal renders in the alert line, as the server wrote it", () => {
  assert.match(instancesField, /role="alert">\{\{ error \}\}/, "the alert line prints the prop verbatim");
  assert.match(instancesField, /v-if="error"/, "and only when a refusal exists");
});

// --- the desktop window routes the list to the APP, the browser tab to its server ----

test("the pane's save sends desktop.instances to the app inside a desktop window", () => {
  assert.match(
    paneSource,
    /instancesAreAppOwned\(\) && "desktop\.instances" in changes/,
    "the list is the app's knob when a desktop window is driving the page",
  );
  assert.match(paneSource, /hostSaved = await saveInstancesInApp\(appRows\)/, "and it rides the host's own door");
  assert.match(
    paneSource,
    /setSaveError\(err instanceof Error \? err\.message : String\(err\), "desktop\.instances"\)/,
    "the host's refusal lands on the field's alert line, keyed to this knob",
  );
  // the host's answer replaces the ACTIVE server's copy of the list in the pane's truth
  assert.equal(
    (paneSource.match(/"desktop\.instances": hostSaved/g) ?? []).length,
    2,
    "loaded and draft both adopt the host's rows",
  );
});

test("every other knob keeps the /settings door in both", () => {
  assert.match(paneSource, /applyPayload\(await saveSettings\(changes\)\)/);
  const at = paneSource.indexOf("async function save()");
  const body = paneSource.slice(at, paneSource.indexOf("async function saveScope", at));
  assert.match(body, /delete changes\["desktop\.instances"\]/, "only that one key leaves the /settings body");
  assert.doesNotMatch(body, /"desktop\.instances" in changes \? .* delete changes\["server\./);
});

test("a save that landed the connection list announces it in both modes", () => {
  const at = paneSource.indexOf("async function save()");
  const body = paneSource.slice(at, paneSource.indexOf("async function saveScope", at));
  assert.match(
    body,
    /if \("desktop\.instances" in changes\) \{\s*window\.dispatchEvent\(new CustomEvent\(INSTANCE_CHANGED_EVENT\)\);\s*\}/,
    "the bar re-reads the list and the open panel re-reads its states, app-owned or not",
  );
  assert.match(body, /window\.dispatchEvent\(new CustomEvent\(INSTANCE_CHANGED_EVENT\)\)/, "one window event, no reload");
});
