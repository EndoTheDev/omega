// omega webui: card t_fec41e66 - the provider instances editor explains
// itself. Endo could not tell what a row IS or how a run reaches it: the
// rows carried faint cell labels while the only flow explanation lived in
// the knob help (easy to miss) and the source comment (invisible). Pins
// the visible flow: the in-editor explainer, the placeholders on a fresh
// row, and the knob help that says the same thing server-side. Structural
// source assertions (the node suite has no DOM: what is pinned is the
// copy that renders, not the paint). Run: node --test webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

const field = read("app/components/settings/ProvidersField.vue");
const settings = read("../cmd/omega/settings.go");

// --- the editor states the flow before the first row ---

test("the editor opens with the visible row explainer", () => {
  assert.match(field, /class="provider-hint"/,
    "the explainer paragraph stands above the rows");
  assert.match(field,
    /each row is one account of a provider type[\s\S]*?instance id[\s\S]*?instance:model/,
    "the explainer names what a row IS and how a run reaches it: instance:model");
  // the explainer renders BEFORE the v-for rows (visible without scrolling)
  const hintAt = field.indexOf('class="provider-hint"');
  const rowsAt = field.indexOf("v-for=");
  assert.ok(hintAt >= 0 && rowsAt > hintAt,
    "the explainer precedes the row list in template order");
});

// --- a fresh row shows what to type ---

test("a blank row carries placeholders for name, model and host", () => {
  assert.match(field, /placeholder="my-ollama"/, "the name cell shows a sample id");
  assert.match(field, /placeholder="llama3"/, "the model cell shows a sample model");
  assert.match(field, /placeholder="http:\/\/localhost:11434"/,
    "the host cell shows the ollama default");
});

// --- the knob help and the editor copy agree ---

test("the provider.instances knob help carries the same flow facts", () => {
  assert.match(settings,
    /Key: "provider\.instances"[\s\S]{0,200}Help: +?"named provider instances: the name is the instance id/,
    "the server-side help keeps saying the name is the instance id");
});
