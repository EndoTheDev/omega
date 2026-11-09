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

test("a persisted 'default' value renders as the chip text", () => {
  // BandSelect falls back to the raw value when no option matches; the
  // prepended entry is what turns the stored word into the label
  const options = withAuxDefault([], true);
  const current = options.find((o) => o.value === "default")?.label;
  assert.equal(current, "default (the chat's model)");
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
