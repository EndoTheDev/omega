// omega webui: unit tests for the auxiliary "default" entry of the model
// pickers (card t_df46c8cf). The word is the WIRE CONTRACT: the settings
// schema advertises default_option on the aux knobs, the picker renders
// the entry, and the value persists as the literal "default" (the server
// resolves it per call to the model the chat runs).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";

import { normalizeSettingsPayload } from "../app/composables/omega.ts";
import {
  AUX_DEFAULT_VALUE,
  filterModelEntries,
  pickerNeedsFreeText,
  withAuxDefault,
} from "../app/composables/modelCatalog.ts";

test("the aux sentinel is the literal word the server resolves", () => {
  assert.equal(AUX_DEFAULT_VALUE, "default");
});

test("withAuxDefault prepends the entry ahead of the named models", () => {
  const options = withAuxDefault(
    [
      { value: "gemma4:31b", label: "gemma4:31b" },
      { value: "clef:27b", label: "clef:27b" },
    ],
    true,
  );
  assert.deepEqual(options, [
    { value: "default", label: "default (the chat's model)" },
    { value: "gemma4:31b", label: "gemma4:31b" },
    { value: "clef:27b", label: "clef:27b" },
  ]);
});

test("withAuxDefault leaves non-aux knobs untouched", () => {
  const named = [{ value: "gemma4:31b", label: "gemma4:31b" }];
  assert.deepEqual(withAuxDefault(named, false), named);
});

test("the default entry survives an empty catalog - the one live choice", () => {
  // a vision-filtered list that matched nothing (or a listing that never
  // landed) still offers "default": following the chat's model needs no
  // provider catalog at all
  const empty = filterModelEntries([], "vision");
  const options = withAuxDefault(
    empty.map((e) => ({ value: e.name, label: e.name })),
    true,
  );
  assert.equal(options.length, 1);
  assert.equal(options[0].value, "default");
});

test("an aux knob still degrades to free text when no NAMED model matched", () => {
  // the sentinel is a rendering entry, never a model: a vision-filtered
  // catalog of openai/anthropic ids matches nothing, and the operator
  // must keep the typed escape hatch (the file's contract: never a
  // silent dead dropdown) - the prepended "default" must not mask it
  const named = filterModelEntries(
    [{ name: "gpt-4o", capabilities: [] }],
    "vision",
  );
  assert.equal(named.length, 0);
  const options = withAuxDefault(
    named.map((e) => ({ value: e.name, label: e.name })),
    true,
  );
  assert.equal(options.length, 1, "the sentinel is still offered for rendering");
  assert.equal(
    pickerNeedsFreeText(named.length, false, null),
    true,
    "no named model -> free text, sentinel or not",
  );
});

test("pickerNeedsFreeText: the listbox renders only with named models (or mid-flight)", () => {
  // landed, named models present -> the listbox
  assert.equal(pickerNeedsFreeText(2, false, null), false);
  // landed, nothing matched -> free text (with or without a sentinel)
  assert.equal(pickerNeedsFreeText(0, false, null), true);
  // a failed fetch -> free text, named entries or not
  assert.equal(pickerNeedsFreeText(3, false, "boom"), true);
  // in flight -> the listbox shows the loading hint rather than flashing
  // the fallback input
  assert.equal(pickerNeedsFreeText(0, true, null), false);
});

test("a persisted 'default' value renders as the chip text", () => {
  // BandSelect falls back to the raw value when no option matches; the
  // prepended entry is what turns the stored word into the label
  const options = withAuxDefault(
    [{ value: "gemma4:31b", label: "gemma4:31b" }],
    true,
  );
  const current = options.find((o) => o.value === "default")?.label;
  assert.equal(current, "default (the chat's model)");
  assert.notEqual(current, options[1].label, "the label is not just the value echoed back");
});

test("normalizeSettingsPayload carries default_option through", () => {
  const out = normalizeSettingsPayload({
    groups: [
      {
        key: "provider",
        label: "provider",
        knobs: [
          {
            key: "provider.vision_model",
            label: "vision model",
            type: "model",
            value: "default",
            filter: "vision",
            default_option: true,
          },
          {
            key: "provider.title_model",
            label: "title model",
            type: "model",
            value: "",
          },
          {
            key: "provider.vision_model_bogus",
            label: "bogus",
            type: "model",
            value: "",
            default_option: "yes",
          },
        ],
      },
    ],
  });
  const knobs = out.groups[0].knobs;
  assert.equal(knobs[0].defaultOption, true);
  // absent stays absent (the picker adds no entry to a plain model knob)
  assert.equal(knobs[1].defaultOption, undefined);
  // a non-boolean never turns the entry on
  assert.equal(knobs[2].defaultOption, undefined);
});
