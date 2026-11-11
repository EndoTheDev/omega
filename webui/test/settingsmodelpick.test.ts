// omega webui: the SETTINGS model knobs ride the composer's picker (card
// t_dac84a33). Two Endo rulings and one collision are pinned here:
//   - the aux inherit entry is a PINNED TOP ROW labelled
//     "default (inherit from the main model)", never a provider section;
//   - a model the knob's slot refuses is SHOWN DISABLED WITH ITS REASON
//     ("not a vision model"), never hidden - the catalog reads complete;
//   - the word "default" keeps its two meanings apart: the composer
//     grammar's "default:model" is the BASE PROVIDER's section, while the
//     aux word "default" means "inherit the chat's model" and is the only
//     value the inherit row stores.
// Plus the composer-unchanged proof: both new props are optional and every
// new pixel is gated on them, so the chat picker renders as before.
// Run: node --test test/settingsmodelpick.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";

import {
  AUX_DEFAULT_VALUE,
  knobModelValue,
  slotRejectReason,
} from "../app/composables/modelCatalog.ts";
import { normalizeSettingsPayload } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (...parts: string[]) => readFileSync(join(root, ...parts), "utf8");

const picker = read("app", "components", "chat", "ModelSelect.vue");
const field = read("app", "components", "settings", "ModelField.vue");
const css = read("app", "assets", "css", "main.css");

// the live-ish catalog: one base provider, one named instance, wire flags
// plus the curated families where the wire is silent.
const sections = ["default", "backup"];

describe("the knob's persisted value: slot vs non-slot vs inherit", () => {
  it("a SLOT knob keeps the composer grammar verbatim", () => {
    // the server resolves both forms on its slot lane
    assert.equal(knobModelValue("default:llama3", true, sections), "default:llama3");
    assert.equal(knobModelValue("backup:llama3", true, sections), "backup:llama3");
  });

  it("a non-slot knob stores the BARE model name", () => {
    // provider.vision_model / provider.title_model / provider.model_name
    // feed a provider directly - a section prefix is not part of the value
    assert.equal(knobModelValue("default:llama3", false, sections), "llama3");
    assert.equal(knobModelValue("backup:llama3", false, sections), "llama3");
  });

  it("a model's OWN colon survives the strip (clef:27b stays clef:27b)", () => {
    assert.equal(knobModelValue("default:clef:27b", false, sections), "clef:27b");
    assert.equal(knobModelValue("backup:clef:27b", false, sections), "clef:27b");
  });

  it("the inherit row's literal 'default' rides through untouched", () => {
    assert.equal(AUX_DEFAULT_VALUE, "default");
    assert.equal(knobModelValue("default", false, sections), "default");
    // a bare name is already bare - nothing to unwrap
    assert.equal(knobModelValue("llama3", false, sections), "llama3");
  });

  it("a pick that collides with a section NAME still leaves a model name", () => {
    // an instance named like a model family must not eat the model's colon
    assert.equal(knobModelValue("backup:gpt-oss:20b", false, sections), "gpt-oss:20b");
  });
});

describe("the slot filter is a REASON, not a filter", () => {
  it("the vision slot rejects a blind model with 'not a vision model'", () => {
    assert.equal(
      slotRejectReason("vision", "glm-5.3", ["completion", "tools"]),
      "not a vision model",
    );
    assert.equal(slotRejectReason("vision", "clef:27b", ["decision", "vision"]), null);
  });

  it("the curated families qualify a wire-silent listing", () => {
    // openai/anthropic id lists carry no capabilities: the curated sets decide
    assert.equal(slotRejectReason("vision", "gemma4:31b", []), null);
    assert.equal(slotRejectReason("vision", "gpt-4o", []), "not a vision model");
  });

  it("the approval slot rejects everything outside the decision family", () => {
    assert.equal(
      slotRejectReason("decision", "llama3", ["completion"]),
      "not an approval model",
    );
    assert.equal(slotRejectReason("decision", "clef-flash", []), null);
    assert.equal(slotRejectReason("decision", "mystery:8b", ["decision"]), null);
  });

  it("an unfiltered knob (the main model) rejects nothing", () => {
    assert.equal(slotRejectReason("", "gpt-4o", []), null);
    assert.equal(slotRejectReason("", "glm-5.3", null), null);
  });
});

describe("ModelSelect: the two new props are OFF unless passed", () => {
  it("both are optional, so the composer passes neither and renders as before", () => {
    assert.match(picker, /inheritRow\?: boolean;/);
    assert.match(picker, /qualify\?: \(section: string, model: string\) => string \| null;/);
    // no qualify hook = every row pickable, no reason span, no title
    assert.match(picker, /return props\.qualify \? props\.qualify\(section, model\) : null;/);
    // the chip title still defaults to the composer's own word
    assert.match(picker, /props\.label \?\? "Model"/);
    assert.match(picker, /:title="chipTitle"/);
    // the composer's pop still opens UP: the drop prop defaults to it and
    // the settings field is the only caller that asks for "down"; a "down"
    // seat measures its scrolling container at open, takes the roomier
    // side, and caps the section body to what is left (the settings body
    // clips its overflow - an unmeasured pop vanishes behind the clip)
    assert.match(picker, /drop\?: "up" \| "down";/);
    assert.match(picker, /if \(props\.drop !== "down"\) return;/);
    assert.match(picker, /flipDown\.value = below >= above;/);
    assert.match(picker, /"--model-pop-body":/);
    assert.match(picker, /class="model-select"[\s\S]{0,120}measured: measured/);
    assert.match(picker, /\.model-select\.measured \.model-body \{/);
  });

  it("the inherit row is gated, emits the literal word, and sits OUTSIDE the sections", () => {
    assert.match(picker, /v-if="inheritRow"/);
    assert.match(picker, /default \(inherit from the main model\)/);
    // the literal word, never a section prefix
    assert.match(picker, /function pickInherit\(\) \{\s*emit\("update:model", AUX_DEFAULT_VALUE\);/);
    // pinned ABOVE .model-body: the sections scroll, the row does not
    assert.ok(
      picker.indexOf("model-inherit") < picker.indexOf('class="model-body"'),
      "the inherit row must render above the section body",
    );
    assert.doesNotMatch(
      picker,
      /v-for="g in visibleGroups"[\s\S]{0,400}inheritRow/,
      "the inherit row is never folded into a provider section",
    );
  });

  it("a disqualified row renders disabled with its reason and never picks", () => {
    assert.match(picker, /:aria-disabled="reasonFor\(g\.name, m\) !== null/);
    assert.match(picker, /:title="reasonFor\(g\.name, m\) \?\? undefined"/);
    assert.match(picker, /class="row-reason"/);
    assert.match(picker, /v-if="reasonFor\(g\.name, m\)"/);
    // the pick guard: a disabled row is information, never an override
    assert.match(
      picker,
      /function pick\(g: ModelProviderSection, m: string\) \{\s*\/\/[\s\S]{0,400}if \(reasonFor\(g\.name, m\) !== null\) return;/,
    );
  });
});

describe("ModelField: the knobs' wiring on the composer picker", () => {
  it("the slot knobs persist the grammar, the rest the bare name", () => {
    assert.match(
      field,
      /knobModelValue\(raw, props\.knob\.slot === true, sectionNames\.value\)/,
    );
    // the section names are what makes a colon-bearing model name safe
    assert.match(field, /const sectionNames = computed/);
  });

  it("the inherit row follows default_option, and the approval slot is exempt", () => {
    assert.match(
      field,
      /const inheritRow = computed\(\s*\(\) => props\.knob\.defaultOption === true && filter !== "decision",/,
      "an approval decision is deliberate - no inherit row, ever",
    );
    assert.match(field, /:inherit-row="inheritRow"/);
    // and the row opens DOWN: a settings row sits mid-pane, not on the
    // composer's floor where the shared pop anchors upward
    assert.match(field, /drop="down"/);
  });

  it("the filters ride qualify with the ruling's exact reasons", () => {
    assert.match(field, /return slotRejectReason\(filter, model, wire\);/);
    // and the reasons are the ruling's words
    const catalogSrc = read("app", "composables", "modelCatalog.ts");
    assert.match(catalogSrc, /return "not a vision model";/);
    assert.match(catalogSrc, /return "not an approval model";/);
  });

  it("the approval knob carries no inherit row and no 'default' hint", () => {
    // the hint mentions the word only where the row exists
    assert.match(field, /return inheritRow\.value\s*\?\s*`\$\{lead\} - type "default"/);
    // the knob inventory: approval is a decision slot, vision is the one
    // default_option knob today (t_b403ad88 may add more - the exemption
    // above keeps approval out regardless)
    const out = normalizeSettingsPayload({
      groups: [
        {
          key: "provider",
          label: "provider",
          knobs: [
            {
              key: "provider.approvals_model",
              label: "approval decision model",
              type: "model",
              slot: true,
              filter: "decision",
              default_option: true,
              value: "",
            },
            {
              key: "provider.vision_model",
              label: "vision model",
              type: "model",
              filter: "vision",
              default_option: true,
              value: "default",
            },
          ],
        },
      ],
    });
    const [approval, vision] = out.groups[0].knobs;
    assert.equal(approval.defaultOption, true, "the flag may land on it");
    assert.equal(approval.filter, "decision", "and the exemption reads the filter");
    assert.equal(vision.defaultOption, true);
    assert.equal(vision.filter, "vision");
  });

  it("styles exist for the pinned row and the disabled rows", () => {
    assert.match(css, /\.model-inherit \{/);
    assert.match(css, /\.model-list li\.off \{/);
    assert.match(css, /\.model-list li \.row-reason \{/);
  });
});
