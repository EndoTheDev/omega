// omega webui: TYPED MODEL PICKERS (card t_70c28128). Unit tests for
// the model-catalog pure helpers - the capability-filter rules the
// settings model fields rule on:
//   - the decision slot lists decision models ONLY (a chat model is
//     never on the list);
//   - the vision slot lists vision-capable models ONLY (a blind model
//     is never on the list);
//   - chat slots see the full available list;
//   - the curated sets supply capabilities where the wire doesn't.
// Plus the settings-seam wiring pins: type "model" survives the
// normalizer, and the pane renders it through the shared BandSelect
// via ModelField. Run: node --test webui/test/modelcatalog.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  effectiveCapabilities,
  filterModelEntries,
  KNOWN_DECISION_MODELS,
  KNOWN_VISION_MODELS,
  refreshModelCatalog,
  type ModelEntry,
} from "../app/composables/modelCatalog.ts";
import { normalizeSettingsPayload } from "../app/composables/omega.ts";

const here = dirname(fileURLToPath(import.meta.url));
const webui = join(here, "..", "..");
const webuiApp = join(webui, "webui", "app");

const read = (...parts: string[]): string =>
  readFileSync(join(...parts), "utf8");

// the catalog fixture mirrors the LIVE keypool /api/tags answer
// (grounded 2026-11-01): wire capability arrays, mixed families.
const catalog: ModelEntry[] = [
  { name: "clef:27b", capabilities: ["decision", "vision"] },
  { name: "glm-5.3", capabilities: ["completion", "thinking", "tools"] },
  { name: "gpt-oss:20b", capabilities: ["completion", "tools", "thinking"] },
  { name: "kimi-k2.6", capabilities: ["vision", "thinking", "completion", "tools"] },
  { name: "minimax-m3", capabilities: ["completion", "tools", "thinking", "vision"] },
];

test("decision filter lists decision models ONLY - no chat model rides it", () => {
  const got = filterModelEntries(catalog, "decision").map((e) => e.name);
  assert.deepEqual(got, ["clef:27b"]);
  // the wire's blind chat models are excluded
  assert.ok(!got.includes("glm-5.3"), "chat model leaked into the decision slot");
  assert.ok(!got.includes("gpt-oss:20b"), "chat model leaked into the decision slot");
});

test("vision filter lists vision-capable models ONLY - no blind model rides it", () => {
  const got = filterModelEntries(catalog, "vision").map((e) => e.name);
  assert.deepEqual(got, ["clef:27b", "kimi-k2.6", "minimax-m3"]);
  // the blind chat models are excluded
  assert.ok(!got.includes("glm-5.3"), "blind model leaked into the vision slot");
  assert.ok(!got.includes("gpt-oss:20b"), "blind model leaked into the vision slot");
});

test("chat slots (empty filter) see the FULL list, unfiltered", () => {
  assert.deepEqual(filterModelEntries(catalog, ""), catalog);
});

test("curated decision set supplies the flag where the wire doesn't", () => {
  // a name-only listing (openai/anthropic): the family prefixes fire
  // from the curated map alone
  const wireless: ModelEntry[] = [
    { name: "clef-flash", capabilities: [] },
    { name: "nimble:7b", capabilities: [] },
    { name: "tev1", capabilities: [] },
    { name: "gpt-4o", capabilities: [] },
  ];
  const got = filterModelEntries(wireless, "decision").map((e) => e.name);
  assert.deepEqual(got.sort(), ["clef-flash", "nimble:7b", "tev1"]);
  for (const family of KNOWN_DECISION_MODELS)
    assert.ok(family.length > 0, "decision family entry is non-empty");
});

test("curated vision set supplies the flag where the wire doesn't", () => {
  const wireless: ModelEntry[] = [
    { name: "gemma4:31b", capabilities: [] },
    { name: "llava:13b", capabilities: [] },
    { name: "gpt-4o", capabilities: [] },
  ];
  const got = filterModelEntries(wireless, "vision").map((e) => e.name);
  assert.deepEqual(got.sort(), ["gemma4:31b", "llava:13b"]);
});

test("effectiveCapabilities never drops a wire flag the wire DID say", () => {
  assert.deepEqual(
    effectiveCapabilities("kimi-k2.6", ["vision", "tools"]),
    ["vision", "tools"],
  );
  // and never re-writes the wire's list position
});

test("effectiveCapabilities unions wire flags with curated family matches", () => {
  // wire says nothing, the family maps fire
  assert.ok(effectiveCapabilities("clef-flash", []).includes("decision"));
  assert.ok(effectiveCapabilities("gemma3:12b", undefined).includes("vision"));
  // a wire "decision" flag stands on its own even with no curated hit
  assert.ok(effectiveCapabilities("mystery:8b", ["decision"]).includes("decision"));
  // blank capabilities + no family hit stays clean
  assert.deepEqual(effectiveCapabilities("gpt-4o", null), []);
});

test("settings normalization: a model knob survives with its filter", () => {
  const out = normalizeSettingsPayload({
    groups: [
      {
        key: "provider",
        label: "provider",
        knobs: [
          { key: "provider.model_name", label: "model name", type: "model", value: "glm-5.3" },
          {
            key: "provider.vision_model",
            label: "vision model",
            type: "model",
            filter: "vision",
            value: "",
          },
          // an unknown filter value falls out like any stale entry data
          {
            key: "provider.title_model",
            label: "title model",
            type: "model",
            filter: "telepathy",
            value: "",
          },
        ],
      },
    ],
  });
  const knobs = out.groups[0].knobs;
  assert.equal(knobs.length, 3, "type model is a known knob type now");
  assert.equal(knobs[0].type, "model");
  assert.equal(knobs[0].filter, undefined, "unfiltered slots carry no filter");
  assert.equal(knobs[1].filter, "vision");
  assert.equal(knobs[2].filter, undefined, "an unknown filter value never survives");
});

// --- component wiring pins: ModelField is BandSelect, and degrade ---
const settingsTab = read(
  webuiApp,
  "components",
  "settings",
  "SettingsTab.vue",
);
const modelField = read(
  webuiApp,
  "components",
  "settings",
  "ModelField.vue",
);

test("settings normalization: the per-subsystem slot flag survives exactly as a boolean", () => {
  const out = normalizeSettingsPayload({
    groups: [
      {
        key: "provider",
        label: "provider",
        knobs: [
          {
            key: "provider.compaction_model",
            label: "compaction model",
            type: "model",
            slot: true,
            value: "",
          },
          // a truthy-but-not-true flag falls out (the normalizer
          // reads === true, never truthiness)
          {
            key: "provider.summarizer_model",
            label: "summarizer model",
            type: "model",
            slot: "yes",
            value: "",
          },
          // plain model knobs keep slot undefined (the chat picker
          // never lists instances)
          {
            key: "provider.model_name",
            label: "model name",
            type: "model",
            value: "glm-5.3",
          },
        ],
      },
    ],
  });
  const knobs = out.groups[0].knobs;
  assert.equal(knobs[0].slot, true);
  assert.equal(knobs[1].slot, undefined, "non-boolean slot flags never survive");
  assert.equal(knobs[2].slot, undefined);
});

test("the model knob renders ModelField, and the field IS the composer's picker", () => {
  assert.match(settingsTab, /v-else-if="knob\.type === 'model'"/);
  assert.match(settingsTab, /<ModelField[\s\S]*?:knob="knob"/);
  assert.doesNotMatch(settingsTab, /knob\.type === 'model'[\s\S]{0,200}<input/);
  // card t_dac84a33: the settings field renders ModelSelect - the SAME
  // grouped popover the composer rides - with the catalog as groups. The
  // old flat BandSelect list is gone, and no native select ever appears.
  assert.match(modelField, /<ModelSelect[\s\S]*?:groups="groups"/);
  assert.match(
    modelField,
    /import ModelSelect from "\.\.\/chat\/ModelSelect\.vue"/,
  );
  assert.doesNotMatch(modelField, /<BandSelect/);
  assert.doesNotMatch(modelField, /<select[\s>]/);
});

test("the degrade path is decided by the PICKABLE named entries, never the inherit row", () => {
  // the wiring pin for the helper above (no component mounter in this
  // suite): mode feeds off the count of rows the slot actually accepts,
  // while the inherit row is a flag the picker renders, never a model
  assert.match(
    modelField,
    /pickerNeedsFreeText\(\s*pickableCount\.value/,
    "mode must count PICKABLE named models - the inherit row is not a model",
  );
  assert.match(modelField, /const pickableCount = computed/);
  assert.match(modelField, /:inherit-row="inheritRow"/);
  assert.doesNotMatch(
    modelField,
    /pickableCount\.value > 0/,
    "the count reaches the degrade rule, never a hand-rolled threshold",
  );
});

test("the degrade path: fetch failure or empty filtered list yields text + hint", () => {
  // the fallback branch is a real input with the hint spelled out
  assert.match(modelField, /setting-model-hint/);
  assert.match(modelField, /provider model list unavailable/);
  assert.match(modelField, /type the model name/);
  // an aux knob says the word out loud in the empty-list hint too
  assert.match(modelField, /type "default" to follow the chat's model/);
  // the pane refreshes the shared catalog (one request per settings open)
  assert.equal(typeof refreshModelCatalog, "function");
});

// --- the per-subsystem slots (card t_fc0b2f2d, reshaped t_1914a8c9 and
// again t_dac84a33): the picker reads the GROUPED CATALOG whole ---

test("the slot mode: ModelField hands the grouped catalog to the composer picker verbatim", () => {
  // the component reads the grouped /models sections, not a prop:
  // t_1914a8c9 removed the bound-model column from the instance rows, so
  // the OLD ProviderInstanceRow prop is gone; t_dac84a33 then stopped
  // flattening anything - ModelSelect renders every section itself
  assert.doesNotMatch(modelField, /instances\?: ProviderInstanceRow\[\]/,
    "the instance-row prop is gone - slots read the grouped catalog");
  assert.match(modelField, /catalog\.value\.sections \?\? \[\]/);
  assert.match(
    modelField,
    /sections\.value\.map\(\(s\) => \(\{/,
    "every section reaches the picker as a group",
  );
  assert.doesNotMatch(
    modelField,
    /filterModelEntries/,
    "no model is dropped from the picker - the slot rule lands as a REASON",
  );
  assert.match(modelField, /slotRejectReason\(filter, model, wire\)/);
  // the pane no longer hands ModelField instance rows
  assert.doesNotMatch(settingsTab, /:instances="instanceRows"/);
});