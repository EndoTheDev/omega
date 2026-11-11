// omega webui: unit tests for the settings scope tabs (settings tier 3b,
// card t_04eb11d6) - the per-agent surface the pane renders instead of
// the Main registry.
//
// Two layers, same as the other settings suites:
//   - the PURE helpers in composables/omega.ts (normalizeSettingsPayload's
//     scopes, agentConfigUpdate's wire mapping, scopeConfigDiff's save
//     derivation) - real assertions on real functions;
//   - the component's WIRING (SettingsTab.vue) - source assertions, because
//     the unit suite has no DOM: what is pinned is which list a tab renders,
//     where the strip is gated, how the search and the save route.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  AGENT_KNOB_FIELDS,
  agentConfigUpdate,
  filterSettingGroups,
  normalizeSettingsPayload,
  scopeConfigDiff,
  scopeKnobDiffersFromMain,
} from "../app/composables/omega.ts";

const SETTINGS_TAB = readFileSync(
  fileURLToPath(new URL("../app/components/settings/SettingsTab.vue", import.meta.url)),
  "utf8",
);

// one scope as the server's settingsScopes emits it (card t_22a5f70a):
// four groups in wire order, each knob {key, value, overridden}
function scopePayload(overrides: Record<string, unknown> = {}) {
  const knob = (key: string, value: unknown, overridden: boolean) => ({ key, value, overridden });
  const groups = [
    {
      key: "provider",
      overridden: false,
      knobs: [
        knob("provider.type", overrides["provider.type"] ?? "ollama", "provider.type" in overrides),
        knob("provider.model_name", "llama", "provider.model_name" in overrides),
        knob("provider.host", overrides["provider.host"] ?? "", "provider.host" in overrides),
        knob("provider.api_key", "****", "provider.api_key" in overrides),
      ],
    },
    {
      key: "system_prompt",
      overridden: false,
      knobs: [knob("system_prompt", overrides.system_prompt ?? "", "system_prompt" in overrides)],
    },
    {
      key: "description",
      overridden: false,
      knobs: [knob("description", overrides.description ?? "helper agent", "description" in overrides)],
    },
    {
      key: "max_turns",
      overridden: false,
      knobs: [knob("max_turns", overrides.max_turns ?? 42, "max_turns" in overrides)],
    },
  ];
  return { name: "alpha", groups };
}

// --- normalizeSettingsPayload: the scopes half of the wire ---

test("normalize keeps a well-formed scope: name, group order, overridden flags", () => {
  const out = normalizeSettingsPayload({
    groups: [],
    scopes: [scopePayload({ "provider.model_name": true, max_turns: 7 })],
  });
  assert.equal(out.scopes.length, 1);
  const scope = out.scopes[0];
  assert.equal(scope.name, "alpha");
  assert.deepEqual(
    scope.groups.map((g) => g.key),
    ["provider", "system_prompt", "description", "max_turns"],
  );
  const model = scope.groups[0].knobs.find((k) => k.key === "provider.model_name");
  assert.equal(model?.overridden, true);
  const host = scope.groups[0].knobs.find((k) => k.key === "provider.host");
  assert.equal(host?.overridden, false);
  // the effective value rides through untouched (secrets arrive masked)
  assert.equal(scope.groups[0].knobs.find((k) => k.key === "provider.api_key")?.value, "****");
});

test("normalize ALWAYS answers an array of scopes (R8: missing key = empty world)", () => {
  for (const raw of [{ groups: [] }, { groups: [], scopes: null }, { groups: [], scopes: "nope" }, {}]) {
    assert.deepEqual(normalizeSettingsPayload(raw).scopes, []);
  }
  assert.deepEqual(normalizeSettingsPayload({ groups: [], scopes: [] }).scopes, []);
});

test("normalize drops malformed scopes and keeps the rest", () => {
  const out = normalizeSettingsPayload({
    groups: [],
    scopes: [
      null,
      "nope",
      { groups: [] }, // no name
      { name: "" }, // empty name
      { name: "beta" }, // no groups array
      { name: "gamma", groups: [null, { key: "provider" }, { key: "provider", knobs: [null, { key: "provider.host" }] }] },
    ],
  });
  assert.deepEqual(out.scopes.map((s) => s.name), ["gamma"]);
  // the group with no knobs array drops; the knob with no value drops
  assert.equal(out.scopes[0].groups.length, 1);
  assert.equal(out.scopes[0].groups[0].knobs.length, 0);
});

test("normalize keeps a scope error string and drops an empty one", () => {
  const withError = normalizeSettingsPayload({
    groups: [],
    scopes: [{ name: "alpha", groups: [], error: "parse config.yaml: bad indent" }],
  });
  assert.equal(withError.scopes[0].error, "parse config.yaml: bad indent");
  const empty = normalizeSettingsPayload({
    groups: [],
    scopes: [{ name: "alpha", groups: [], error: "" }],
  });
  assert.equal("error" in empty.scopes[0], false);
});

// --- agentConfigUpdate: the ONE write path's field mapping ---

test("AGENT_KNOB_FIELDS covers exactly the four overridable groups", () => {
  assert.deepEqual(Object.keys(AGENT_KNOB_FIELDS).sort(), [
    "description",
    "max_turns",
    "provider.api_key",
    "provider.host",
    "provider.model_name",
    "provider.type",
    "system_prompt",
  ]);
});

test("agentConfigUpdate maps dotted knobs onto the endpoint's fields", () => {
  const { body, invalid } = agentConfigUpdate(
    {
      "provider.type": "openai",
      "provider.model_name": "gpt-5",
      "provider.host": "http://127.0.0.1:11434",
      "provider.api_key": "sk-new",
      system_prompt: "be brief",
      description: "triage",
      max_turns: 12,
    },
    [],
  );
  assert.equal(invalid, undefined);
  assert.deepEqual(body, {
    provider_type: "openai",
    model_name: "gpt-5",
    host: "http://127.0.0.1:11434",
    api_key: "sk-new",
    system_prompt: "be brief",
    description: "triage",
    max_turns: 12,
  });
});

test("agentConfigUpdate sends max_turns as a NUMBER and refuses a non-whole one", () => {
  assert.equal(agentConfigUpdate({ max_turns: "12" }, []).body.max_turns, 12);
  // 0 is a real cap ("no turn limit"), never folded away
  assert.equal(agentConfigUpdate({ max_turns: 0 }, []).body.max_turns, 0);
  for (const bad of ["", "12abc", 1.5, -1]) {
    const out = agentConfigUpdate({ max_turns: bad }, []);
    assert.equal(out.body.max_turns, undefined);
    assert.match(out.invalid ?? "", /max turns/);
  }
});

test("agentConfigUpdate drops a key that is also being unset (set+unset is a 400)", () => {
  const { body } = agentConfigUpdate({ description: "new" }, ["description"]);
  assert.equal(body.description, undefined);
  assert.deepEqual(body.unset, ["description"]);
});

test("agentConfigUpdate ignores knobs outside the four overridable groups", () => {
  const { body } = agentConfigUpdate(
    { "compaction.enabled": false, "chat.theme": "solarized", "server.bind": "0.0.0.0:80" },
    [],
  );
  assert.deepEqual(body, {});
});

// --- scopeConfigDiff: the save derivation (count and wire are one truth) ---

test("scopeConfigDiff ships only keys that departed from the served truth", () => {
  const loaded = { "provider.host": "http://a", description: "d", max_turns: 42 };
  const draft = { "provider.host": "http://a", description: "d2", max_turns: 42 };
  const { body } = scopeConfigDiff(loaded, draft, [], new Set());
  assert.deepEqual(body, { description: "d2" });
});

test("scopeConfigDiff turns a reset into the unset verb for an owned key", () => {
  const loaded = { "provider.model_name": "llama", max_turns: 42 };
  // reset writes main's value into the draft AND queues the removal
  const draft = { "provider.model_name": "main-model", max_turns: 42 };
  const { body } = scopeConfigDiff(loaded, draft, ["provider.model_name"], new Set(["provider.model_name"]));
  assert.equal(body.model_name, undefined);
  assert.deepEqual(body.unset, ["provider.model_name"]);
});

test("scopeConfigDiff never unsets a key the agent does not own (a no-op write)", () => {
  const loaded = { "provider.host": "" };
  const { body } = scopeConfigDiff(loaded, { "provider.host": "" }, ["provider.host"], new Set());
  assert.deepEqual(body, {});
});

test("scopeConfigDiff treats an emptied max_turns as inherit, never as 0", () => {
  const loaded = { max_turns: 42 };
  const owned = scopeConfigDiff(loaded, { max_turns: "" }, [], new Set(["max_turns"]));
  assert.deepEqual(owned.body, { unset: ["max_turns"] });
  // no override to remove: the empty field is nothing to save
  assert.deepEqual(scopeConfigDiff(loaded, { max_turns: "" }, [], new Set()).body, {});
});

test("scopeConfigDiff surfaces a bad max_turns before the round-trip", () => {
  const out = scopeConfigDiff({ max_turns: 42 }, { max_turns: "12abc" }, [], new Set());
  assert.match(out.invalid ?? "", /max turns/);
  assert.deepEqual(out.body, {});
});

// --- scopeKnobDiffersFromMain: the agent tab's dot predicate (seam ruling) ---

test("an agent tab's dot means differs-from-MAIN, and an override always lights it", () => {
  // one override set (the layer names the key): dot, whatever the values say
  assert.equal(scopeKnobDiffersFromMain("provider.model_name", "llama", "llama", true, []), true);
  // a masked secret: the structural flag is the ONLY honest answer
  assert.equal(scopeKnobDiffersFromMain("provider.api_key", "****", "****", true, []), true);
  // the inherited knob (no override, effective value IS main's): no dot
  assert.equal(scopeKnobDiffersFromMain("provider.host", "http://a", "http://a", false, []), false);
  // empty and unset fold into one value - an untouched blank row never lights
  assert.equal(scopeKnobDiffersFromMain("system_prompt", "", undefined, false, []), false);
  assert.equal(scopeKnobDiffersFromMain("system_prompt", undefined, "", false, []), false);
  // a draft edit departs from main before it is saved: the row will override
  assert.equal(scopeKnobDiffersFromMain("max_turns", 7, 42, false, []), true);
  // a QUEUED removal is on its way back to main: the star marks it, the dot goes out
  assert.equal(scopeKnobDiffersFromMain("max_turns", 42, 42, true, ["max_turns"]), false);
});

test("R7: an agent tab's search can only ever return the scope's four groups", () => {
  const scope = normalizeSettingsPayload({
    groups: [],
    scopes: [scopePayload({ "provider.model_name": true })],
  }).scopes[0];
  // the pane maps each scope knob onto a row (labels come from the main
  // registry / SCOPE_GROUP_LABELS); the GROUP set is the payload's own
  const ROW_LABELS: Record<string, string> = {
    "provider.type": "type",
    "provider.model_name": "model name",
    "provider.host": "base url",
    "provider.api_key": "api key",
    system_prompt: "system prompt",
    description: "description",
    max_turns: "max turns",
  };
  const groups = scope.groups.map((g) => ({
    key: g.key,
    label: g.key,
    knobs: g.knobs.map((k) => ({
      key: k.key,
      label: ROW_LABELS[k.key] ?? k.key,
      type: "text" as const,
      value: k.value,
      default: "",
    })),
  }));
  assert.deepEqual(groups.map((g) => g.key), ["provider", "system_prompt", "description", "max_turns"]);
  const hits = filterSettingGroups(groups, "model");
  assert.deepEqual(hits.map((g) => g.key), ["provider"]);
  assert.deepEqual(hits[0].knobs.map((k) => k.key), ["provider.model_name"]);
  // a main-registry-only knob can never surface on an agent tab: it is not
  // in the payload (the pane renders scopeGroups, never groups.value)
  for (const g of filterSettingGroups(groups, "")) {
    for (const k of g.knobs) {
      assert.ok(
        ["provider.type", "provider.model_name", "provider.host", "provider.api_key", "system_prompt", "description", "max_turns"].includes(k.key),
        `unexpected agent-tab row ${k.key}`,
      );
    }
  }
  assert.equal(filterSettingGroups(groups, "vision").length, 0);
});

// --- SettingsTab.vue wiring: strip, tab source, search, one write path ---

test("R8: the scope strip exists only when the payload carries a scope", () => {
  // scopeTabs is Main + one per scope, so a bare payload (zero scopes)
  // renders the pane with NO strip at all - the tier-2b shape
  assert.match(SETTINGS_TAB, /const scopeTabs = computed<\{ name: string \| null; label: string \}\[\]>\(\(\) => \[\s*\{ name: null, label: "Main" \},\s*\.\.\.scopes\.value\.map/);
  // card t_a8063683: the gate moved to the HEAD WRAPPER - the head is a
  // flex child, so an ungated wrapper would hold an empty seat even with
  // the strip gone.
  assert.match(SETTINGS_TAB, /<div\s+v-if="scopeTabs\.length > 1"\s+class="settings-content-head">/);
  assert.match(SETTINGS_TAB, /class="settings-scopes"/);
  // role=tablist + aria-selected: the strip is one tablist, Main first
  assert.match(SETTINGS_TAB, /role="tablist"[\s\S]*?role="tab"[\s\S]*?:aria-selected="tab\.name === activeScope"/);
});

test("R3: an agent tab's ONE group source is the scope's four groups", () => {
  // allGroups short-circuits to scopeGroups on an agent tab: no Commonly
  // Used, no chat/dock client state, no other registry group
  assert.match(
    SETTINGS_TAB,
    /if \(activeScope\.value !== null\) return scopeGroups\.value;/,
  );
  assert.match(SETTINGS_TAB, /const SCOPE_GROUP_LABELS[\s\S]*?provider: "provider",[\s\S]*?system_prompt: "system prompt",[\s\S]*?description: "description",[\s\S]*?max_turns: "max turns"/);
  // the chat client-state group never renders on an agent tab: the agent
  // branch returns BEFORE chat/dock are concatenated
  const branch = SETTINGS_TAB.indexOf("if (activeScope.value !== null) return scopeGroups.value;");
  assert.ok(branch > 0);
  // the chat/dock concatenation sits AFTER the agent-tab short-circuit
  assert.ok(SETTINGS_TAB.indexOf("chatGroup.value,", branch) > branch);
});

test("R7: the search is per scope and filters that scope's groups", () => {
  // one writable query per tab, keyed by the active scope
  assert.match(SETTINGS_TAB, /const queries = reactive<Record<string, string>>\(\{\}\);/);
  assert.match(SETTINGS_TAB, /get: \(\) => queries\[activeScope\.value \?\? "main"\] \?\? ""/);
  // the filter runs on the tab's OWN group list - no second filter
  assert.match(SETTINGS_TAB, /filterSettingGroups\(allGroups\.value, query\.value\)/);
});

test("R4: agent writes ride the existing agents edit endpoint, one call per save", () => {
  assert.match(SETTINGS_TAB, /await updateAgentConfig\(name, diff\.body\)/);
  // the count and the wire are the same derivation
  assert.match(SETTINGS_TAB, /const diff = scopeConfigDiff\(/);
  // a landed save adopts the draft BEFORE the refresh: a refresh that
  // cannot reach the host never blanks the tab, and no queued removal
  // outlives the key it just deleted
  assert.match(
    SETTINGS_TAB,
    /state\.loaded = \{ \.\.\.state\.draft \};[\s\S]{0,200}?applyScopes\(\(await fetchSettings\(\)\)\.scopes\)/,
  );
  // a refusal pins the endpoint's words and renders the tab read-only
  assert.match(SETTINGS_TAB, /scopeLocks\[name\] = err instanceof Error \? err\.message : String\(err\)/);
  assert.match(SETTINGS_TAB, /v-if="scopeMessage"/);
  assert.match(SETTINGS_TAB, /v-if="dirtyCount > 0 && !scopeReadOnly"/);
});

test("the dot and reset name their base on each tab", () => {
  assert.match(SETTINGS_TAB, /activeScope\.value === null \? "differs from default" : "differs from main"/);
  assert.match(SETTINGS_TAB, /activeScope\.value === null \? "reset to default" : "reset to main"/);
  // reset on an agent tab queues the REMOVAL - it never writes main's
  // value down (the phantom override / persisted mask hazard)
  assert.match(SETTINGS_TAB, /state\.unset = \[\.\.\.state\.unset, knob\.key\]/);
});
