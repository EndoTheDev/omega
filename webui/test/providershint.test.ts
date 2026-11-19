// omega webui: - the provider instances editor explains itself.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

const field = read("app/components/settings/ProvidersField.vue");
const settings = read("../cmd/omega/settings.go");

// the editor states the flow before the first row

test("the editor opens with the visible row explainer", () => {
  assert.match(field, /class="provider-hint"/,
    "the explainer paragraph stands above the rows");
  assert.match(field,
    /every provider is a named instance[\s\S]*?api key[\s\S]*?model pickers[\s\S]*?under its name/,
    "the explainer names what a row IS (named endpoint - name, type, base url, api key) and how the models reach the pickers");
  assert.match(field, /row\s+named "default" is the provider the agent serves/,
    "the explainer names the primary row (no more flat provider above the list)");
  // the explainer renders BEFORE the v-for rows (visible without scrolling)
  const hintAt = field.indexOf('class="provider-hint"');
  const rowsAt = field.indexOf("v-for=");
  assert.ok(hintAt >= 0 && rowsAt > hintAt,
    "the explainer precedes the row list in template order");
});

// a fresh row shows what to type

test("a blank row carries a name placeholder - and NO model column", () => {
  assert.match(field, /placeholder="my-ollama"/, "the name cell shows a sample id");
  assert.doesNotMatch(field, /placeholder="llama3"/,
    "the model column is GONE (t_1914a8c9): instances carry no model - the pickers offer the endpoint's catalog");
});

// --- card t_4924df4c (new-row half) / t_0346201c (stored-row half) ------
//
// A submitted host is accepted only when it ECHOES the stored host of the
// row the list already names: applyProviderInstances answers "config.yaml
// is the only door" for `inst.Host != existing.Host` on a stored row, and
// a row the stored list does not name has no stored host to echo at all.
// So the base url cell is a STATIC gate - disabled with the door named at
// the field on every row, no input handler, no predicate that could ever
// re-open it after the save. The api key cell stays on the new-row
// predicate: a stored row's masked key round-trips legally.
//
// A new row is legal with its name and type alone, so its key cell is
// disabled too - and the host placeholder is a hint, never the
// http://localhost:11434 example that was itself an illegal value.

test("the base url cell is config.yaml's on EVERY row - stored or new", () => {
  const at = field.indexOf("provider-cell provider-host");
  assert.ok(at > 0, "the host cell renders");
  const hostCell = field.slice(at, field.indexOf("api key", at));
  assert.match(hostCell, /\sdisabled\s/,
    "the host input is gated unconditionally");
  assert.doesNotMatch(hostCell, /:disabled="isNew\(inst\)"/,
    "the gate cannot be the new-row predicate - a STORED row's host must echo the stored one, so a live cell there only ever produces a save error");
  assert.doesNotMatch(hostCell, /@input=/,
    "a gated cell carries no write path");
  assert.match(hostCell, /<em class="provider-door">config\.yaml-only<\/em>/,
    "the host cell always names the door, before any save click");
  assert.match(hostCell, /placeholder="config\.yaml-only"/,
    "the host placeholder states the door - 'provider default' invited an edit");
  assert.match(hostCell, /:value="inst\.host"/,
    "the stored host still renders (read-only)");
});

test("a NEW row disables the api key cell and names the door at the field", () => {
  assert.match(field, /const isNew = \(row: ProviderInstanceRow\): boolean =>/,
    "the editor knows which rows the server would treat as new");
  // the predicate reads the STORED list off the knob, not the draft
  assert.match(field, /const stored = props\.knob\.value;/,
    "new-ness is measured against the last payload the server sent");
  const gated = field.match(/:disabled="isNew\(inst\)"/g) ?? [];
  assert.equal(gated.length, 1,
    "the api key is disabled on a new row (name + type stay live; the host cell is a static gate)");
  const doors = field.match(/<em v-if="isNew\(inst\)" class="provider-door"/g) ?? [];
  assert.equal(doors.length, 1,
    "the api key cell names config.yaml in its label on a new row");
  assert.match(field, />config\.yaml-only</,
    "the marker says the door: config.yaml-only");
  // the host placeholder is a hint, never an example endpoint
  assert.doesNotMatch(field, /placeholder="http:\/\/localhost:11434"/,
    "an example host on a new row invites the exact value the API refuses");
});

// --- the knob help and the editor copy agree ---

test("the provider.instances knob help carries the same flow facts", () => {
  assert.match(settings,
    /Key: "provider\.instances"[\s\S]{0,600}Help: +?"every provider is a named instance: the name is the instance id/,
    "the server-side help keeps saying the name is the instance id");
  assert.match(settings,
    /the row named .{0,18}default.{0,18} is the provider the agent serves[\s\S]{0,400}apply live/,
    "the help names the primary row and that the list applies live");
});

// ONE section: every instance renders, the primary included -----

test("the providers section renders every instance, the primary included", () => {
  // the old connection form and the instances list merged into ONE list.
  assert.match(field, /v-for="\(inst, i\) in rows"/,
    "one row per instance, the primary included (there is no separate connection form)");
  assert.doesNotMatch(field, /provider\.(type|host|api_key)/,
    "the flat connection trio is gone from the editor");
  // the primary row is found by NAME - no flag, no index special case
  assert.match(field, /const PRIMARY = "default"/,
    "the primary is the row named default, resolved by name");
  assert.match(field, /:readonly="isPrimary\(inst\)"/,
    "the primary row's name is the contract: not renamable from the pane");
  assert.match(field, /:disabled="isPrimary\(inst\)"/,
    "the primary row cannot be removed: the agent would have no provider");
});

test("editing the primary row writes the default row", () => {
  // the type/api_key cells of the primary row emit their row like any other.
  assert.match(field,
    /if \(key === "name" && isPrimary\(rows\.value\[i\]!\)\) return;/,
    "the primary's name edit is refused, every other field writes through");
  assert.match(field,
    /rows\.value\.map\(\(row, j\) => \(j === i \? \{ \.\.\.row, \[key\]: value \} : row\)\)/,
    "an edit replaces exactly the edited row - the primary edits its own row");
  assert.match(field, /function removeRow\(i: number\) \{\s*\n\s*if \(isPrimary\(rows\.value\[i\]!\)\) return;/,
    "remove refuses the primary row before it filters anything");
});

test("the type ladder is the listbox's {value,label} shape", () => {
  // a bare string array renders an EMPTY popup.
  assert.match(field, /const types: \{ value: string; label: string \}\[\] = \[/,
    "the type ladder is object-shaped, or the popup renders blank rows");
  for (const type of ["ollama", "openai", "anthropic"]) {
    assert.match(field, new RegExp(`\{ value: "${type}", label: "${type}" \}`),
      `the ladder offers ${type}`);
  }
});
