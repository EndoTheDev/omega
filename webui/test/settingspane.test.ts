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
import {
  DESKTOP_AUTOSTART_HELP,
  DESKTOP_AUTOSTART_LABEL,
  DESKTOP_AUTOSTART_ROW,
  DESKTOP_CONFIG_ROWS,
  DESKTOP_WINDOW_ROWS,
  desktopWindowChecked,
  desktopWindowState,
} from "../app/composables/desktopWindow.ts";

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

// --- the desktop window's four rows: one section, the app's answer in the window ------

const desktopWindowSource = readFileSync(
  `${repoRoot}/app/composables/desktopWindow.ts`,
  "utf8",
);
const desktopWindowField = readFileSync(
  `${repoRoot}/app/components/settings/DesktopWindowField.vue`,
  "utf8",
);

test("the desktop section carries four rows in a window and three in a browser tab", () => {
  assert.equal(DESKTOP_WINDOW_ROWS.length, 4, "the embedded window's section has four rows");
  assert.deepEqual(
    DESKTOP_WINDOW_ROWS,
    ["start_with_system", "start_minimized", "minimize_to_tray", "close_to_tray"],
    "the rows are the registration then the app's three config toggles",
  );
  assert.equal(DESKTOP_CONFIG_ROWS.length, 3, "a browser tab edits three config toggles");
  assert.equal(DESKTOP_AUTOSTART_ROW, "start_with_system");
  // the autostart row is the app's alone: it is not one of the config keys, so a tab never
  // renders it
  assert.ok(
    !(DESKTOP_CONFIG_ROWS as readonly string[]).includes(DESKTOP_AUTOSTART_ROW),
    "the autostart row must not ride the config keys a browser tab can write",
  );
  assert.match(DESKTOP_AUTOSTART_LABEL, /start with system/);
  assert.match(DESKTOP_AUTOSTART_HELP, /registration/);
});

test("the autostart row's checked state follows the door's answer", () => {
  const state = desktopWindowState({
    start_with_system: true,
    start_minimized: true,
    minimize_to_tray: false,
    close_to_tray: true,
    error: "",
  });
  assert.equal(desktopWindowChecked(state, "start_with_system"), true);
  assert.equal(desktopWindowChecked(state, "start_minimized"), true);
  assert.equal(desktopWindowChecked(state, "minimize_to_tray"), false);
  assert.equal(desktopWindowChecked(state, "close_to_tray"), true);

  // the tick follows the next answer: nothing is cached
  const after = desktopWindowState({ start_with_system: false, close_to_tray: true });
  assert.equal(desktopWindowChecked(after, "start_with_system"), false);

  // a refused or junk answer never guesses a row on
  for (const junk of [null, 42, "x", {}, { error: "config.yaml is read-only" }]) {
    const off = desktopWindowState(junk);
    for (const key of DESKTOP_WINDOW_ROWS) {
      assert.equal(desktopWindowChecked(off, key), false, `${key} read on from ${JSON.stringify(junk)}`);
    }
  }
});

test("the pane hands the desktop section to the field, so the window's rows are the app's", () => {
  assert.match(
    paneSource,
    /<DesktopWindowField\s+v-if="section\.id === 'desktop'"[\s\S]{0,120}:app-owned="desktopWindowOwned"[\s\S]{0,80}:knobs="desktopWindowKnobs"/,
    "the desktop section renders the field with the shell verdict and the schema's words",
  );
  // the three config toggles stand down in the window and keep the generic renderer in a tab
  assert.match(paneSource, /!desktopWindowOwnedKnob\(knob\.key\)/, "the owned toggles are suppressed");
  assert.match(
    paneSource,
    /v-else-if="knob\.type !== 'toggle'" class="setting-item"/,
    "the else chain never renders an owned toggle as a text row",
  );
  assert.match(
    paneSource,
    /whenShellDecided\(\)\.then/,
    "the verdict lands after the first mount; the pane watches it instead of guessing at setup",
  );

  // the field reads the host on mount and writes every row through the same door
  assert.match(desktopWindowField, /requestDesktopWindowOptions\(\)/, "one read on mount");
  assert.match(desktopWindowField, /setDesktopWindowOption\(key, on\)/, "one write per row");
  assert.match(desktopWindowField, /v-for="row in rows"/, "one row per DESKTOP_WINDOW_ROWS entry");
  assert.match(desktopWindowField, /role="alert"/, "a refusal is the field's own alert line");
  assert.match(desktopWindowField, /v-if="!appOwned"/, "a browser tab gets the one-line reason");

  // the door's bodies: the same names the Go host decodes
  assert.match(desktopWindowSource, /omega: "window:get"/);
  assert.match(desktopWindowSource, /omega: "window:set", key, value: on/);
  assert.match(desktopWindowSource, /WINDOW_OPTIONS_EVENT = "omega:window-options"/);
});
