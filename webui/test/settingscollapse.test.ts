// omega webui: unit tests for the settings-group collapse persistence
// helpers (parseCollapsedKeys/serializeCollapsedKeys in
// composables/omega.ts) and the SHARED search-input contract
// (SearchInput.vue consumed by AppSidebar AND SettingsTab - only the
// look is shared; filter/union behaviors stay with the consumers).
// The component tests are structural (source assertions): the unit suite
// has no DOM, so what is pinned is the wiring, not the paint.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  parseCollapsedKeys,
  serializeCollapsedKeys,
} from "../app/composables/omega.ts";

// --- parseCollapsedKeys: persisted collapsed GROUP KEYS ---

test("parseCollapsedKeys round-trips through serializeCollapsedKeys", () => {
  const set = new Set(["compaction", "memory"]);
  const back = parseCollapsedKeys(serializeCollapsedKeys(set));
  assert.deepEqual(back, set);
});

test("parseCollapsedKeys accepts any string key (groups are dynamic server data)", () => {
  // unlike the sidebar buckets there is no allowlist: a group that exists
  // today but not at parse time must still survive the trip
  assert.deepEqual(parseCollapsedKeys('["compaction","later-added-group"]'), new Set([
    "compaction",
    "later-added-group",
  ]));
});

test("parseCollapsedKeys drops non-string entries", () => {
  assert.deepEqual(
    parseCollapsedKeys('["compaction",42,null,["memory"],true]'),
    new Set(["compaction"]),
  );
});

test("parseCollapsedKeys tolerates malformed and empty payloads", () => {
  for (const raw of [null, "", "not-json{", '{"compaction":true}', "[]"]) {
    assert.deepEqual(parseCollapsedKeys(raw), new Set());
  }
});

test("defaults stay expanded: null storage parses empty", () => {
  assert.equal(parseCollapsedKeys(null).size, 0);
});

// --- the shared SearchInput: one component, two consumers ---

const root = fileURLToPath(new URL("..", import.meta.url));
const comp = (name: string, area: string) =>
  readFileSync(`${root}/app/components/${area}/${name}.vue`, "utf8");

test("SearchInput owns exactly one input (no duplicated look)", () => {
  const src = comp("SearchInput", "shell");
  assert.equal((src.match(/<input/g) ?? []).length, 1);
  assert.ok(src.includes('class="search-clear"'), "has its clear affordance");
  assert.ok(
    src.includes('keydown.esc.prevent'),
    "Escape clears, matching the settings contract",
  );
});

test("AppSidebar and SettingsTab both consume SearchInput via v-model", () => {
  const consumers = [
    ["AppSidebar", "sessions"],
    ["SettingsTab", "settings"],
  ] as const;
  for (const [name, area] of consumers) {
    const src = comp(name, area);
    assert.ok(
      src.includes("<SearchInput"),
      `${name} renders the shared component`,
    );
    assert.ok(
      src.includes('v-model="query"'),
      `${name} binds it to its own query ref (behavior stays put)`,
    );
  }
});

test("neither consumer re-declares the search-input look in CSS", () => {
  const css = readFileSync(`${root}/app/assets/css/main.css`, "utf8");
  assert.ok(css.includes(".search-input input"), "shared look present");
  assert.ok(!/\.session-search input\b/.test(css), "sidebar override gone");
  assert.ok(!/\.settings-search \{/.test(css), "settings override gone");
});