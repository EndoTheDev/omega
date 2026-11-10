// omega webui: card t_fec41e66 - the provider instances editor explains
// itself (copy reshaped t_1914a8c9: the Hermes shape - a row is a named
// ENDPOINT, no model column; models come from the instance's catalog
// through the model pickers). Pins the visible flow: the in-editor explainer, the placeholders on a fresh
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
    /each row is one provider account[\s\S]*?api key[\s\S]*?model pickers[\s\S]*?under its name/,
    "the explainer names what a row IS (named endpoint - name, type, base url, api key) and how the models reach the pickers");
  // the explainer renders BEFORE the v-for rows (visible without scrolling)
  const hintAt = field.indexOf('class="provider-hint"');
  const rowsAt = field.indexOf("v-for=");
  assert.ok(hintAt >= 0 && rowsAt > hintAt,
    "the explainer precedes the row list in template order");
});

// --- a fresh row shows what to type ---

test("a blank row carries placeholders for name and host - and NO model column", () => {
  assert.match(field, /placeholder="my-ollama"/, "the name cell shows a sample id");
  assert.match(field, /placeholder="http:\/\/localhost:11434"/,
    "the host cell shows the ollama default");
  assert.doesNotMatch(field, /placeholder="llama3"/,
    "the model column is GONE (t_1914a8c9): instances carry no model - the pickers offer the endpoint's catalog");
});

// --- the knob help and the editor copy agree ---

test("the provider.instances knob help carries the same flow facts", () => {
  // the window is generous on purpose: the knob literal carries comments
  // between Value and Help (the default-source note of card t_d13c7264),
  // so a tight window reds on comments, not on drift
  assert.match(settings,
    /Key: "provider\.instances"[\s\S]{0,600}Help: +?"named provider endpoints: the name is the instance id/,
    "the server-side help keeps saying the name is the instance id (now: named provider ENDPOINTS)");
});
