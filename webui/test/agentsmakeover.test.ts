// omega webui: the Agents panel MAKE-OVER (card t_df015e66, window
// 0.17.3). Six rulings on one tab, pinned the house way - a pure layer
// the node suite can probe, plus source assertions where only the SFC
// can answer (the suite has no DOM).
//
//   1. ONE create door: one "+", one form, the agent KIND inside it.
//      The "@" duplicate door died.
//   2. Delete = delete the HOME, sessions included; the confirm names
//      the count before the trigger fires.
//   3. Provider renders as a REFERENCED instance from settings - no
//      host, no api key, no type in the panel.
//   4. Toggles are the ALWAYS OFF set; skills are the explicit
//      allowlist (empty = none).
//   5. The inheritance chips: authored vs inherited, the same state
//      machine as the cron chain - the state is PRESENCE.
//   6. "Inherit from agent" at create is a materialized full COPY.
//
// Run: node --test test/agentsmakeover.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  AGENT_KINDS,
  agentDeleteConfirm,
  agentDeleteLabel,
  agentKindLabel,
  agentSkillsNote,
  agentTogglesNote,
  AGENT_KINDS as KINDS,
  chipOf,
  cloneSources,
  createRequest,
  emptyCreateDraft,
  fieldChips,
  INHERITABLE_FIELDS,
  kindNeedsContinuous,
  LOCKED_AGENT_MODULES,
  scopeIndex,
  skillOptions,
  toggleChoices,
  toggleOptions,
  type AgentCreateDraft,
  type OmegaAgent,
  type SettingsScope,
} from "../../harness/continuous/ui/agentsRows.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const panelSrc = readFileSync(
  join(root, "harness", "continuous", "ui", "AgentsPanel.vue"),
  "utf8",
);
const rowsSrc = readFileSync(
  join(root, "harness", "continuous", "ui", "agentsRows.ts"),
  "utf8",
);

function draft(over: Partial<AgentCreateDraft> = {}): AgentCreateDraft {
  return { ...emptyCreateDraft(), name: "scout", ...over };
}

const roster: OmegaAgent[] = [
  { name: "default", dir: "/home/x/.omega", skill_count: 0, memory_size: 0, session_count: 2, continuous: false },
  { name: "scout", dir: "/home/x/.omega/agents/scout", skill_count: 1, memory_size: 0, session_count: 0, continuous: true },
];

// --- ruling 1: one door, the kind inside ------------------------------

test("the kinds are the three the ONE door offers, and only one needs the capsule", () => {
  assert.deepEqual([...AGENT_KINDS], ["scaffold", "continuous", "clone"]);
  assert.deepEqual([...KINDS], ["scaffold", "continuous", "clone"], "one vocabulary, one spelling");
  assert.equal(kindNeedsContinuous("continuous"), true);
  assert.equal(kindNeedsContinuous("scaffold"), false);
  assert.equal(kindNeedsContinuous("clone"), false);
  for (const kind of AGENT_KINDS) {
    assert.ok(agentKindLabel(kind).length > 0, `${kind} has a voice`);
  }
  assert.match(agentKindLabel("clone"), /full copy at birth/, "the clone says COPY, never a live link");
});

test("a scaffold create posts to /agents with its name and description", () => {
  const req = createRequest(draft({ kind: "scaffold", description: "patrols" }));
  assert.ok(req);
  assert.equal(req!.path, "/agents");
  assert.deepEqual(req!.body, { name: "scout", description: "patrols" });
  assert.equal(req!.body.inherit_from, undefined, "no clone source on a scaffold");
  assert.equal(req!.body.directive, undefined, "no wake fields on a scaffold");
});

test("a continuous create posts to /continuous with directive and schedule", () => {
  const req = createRequest(
    draft({ kind: "continuous", directive: "patrol the logs", schedule: " 17 * * * * " }),
  );
  assert.ok(req);
  assert.equal(req!.path, "/continuous");
  assert.deepEqual(req!.body, {
    name: "scout",
    directive: "patrol the logs",
    schedule: "17 * * * *",
  });
});

test("a clone create posts the inherited-create door (ruling 6)", () => {
  const req = createRequest(draft({ kind: "clone", inherit_from: "default" }));
  assert.ok(req);
  assert.equal(req!.path, "/agents");
  assert.deepEqual(req!.body, { name: "scout", description: "", inherit_from: "default" });
});

test("an incomplete form is refused before it reaches the wire", () => {
  assert.equal(createRequest(draft({ name: "   " })), null, "no name, no request");
  assert.equal(createRequest(draft({ kind: "clone", inherit_from: "" })), null, "a clone needs its source");
  assert.equal(createRequest(draft({ kind: "clone", inherit_from: "  " })), null, "whitespace is not a source");
});

test("cloneSources offers every other home, main included, never the name being created", () => {
  const sources = cloneSources(roster, " scout ");
  assert.deepEqual(sources.map((p) => p.name), ["default"], "main is a legal source; the draft name is not");
});

// --- ruling 2: delete = delete the HOME -------------------------------

test("the delete confirm names the session count the delete will take", () => {
  assert.equal(
    agentDeleteConfirm("scout", 3),
    "delete scout? its home and 3 sessions go with it - this cannot be undone",
  );
  assert.equal(
    agentDeleteConfirm("scout", 1),
    "delete scout? its home and 1 session go with it - this cannot be undone",
  );
  assert.equal(
    agentDeleteConfirm("scout", 0),
    "delete scout? its home and no sessions go with it - this cannot be undone",
  );
  assert.match(agentDeleteConfirm("scout", -1), /session count is unknown/);
  assert.equal(agentDeleteLabel("scout"), "delete scout's home - sessions included");
});

// --- ruling 7: the inheritance state machine --------------------------

const scope = (overridden: Record<string, boolean>, values: Record<string, unknown> = {}): SettingsScope => ({
  name: "scout",
  groups: [
    {
      key: "provider",
      overridden: true,
      knobs: [
        { key: "provider.instance", value: values["provider.instance"] ?? "default", overridden: !!overridden["provider.instance"] },
        { key: "provider.model_name", value: values["provider.model_name"] ?? "glm-5.3", overridden: !!overridden["provider.model_name"] },
      ],
    },
    { key: "system_prompt", overridden: !!overridden["system_prompt"], knobs: [{ key: "system_prompt", value: values["system_prompt"] ?? "be brief", overridden: !!overridden["system_prompt"] }] },
    { key: "max_turns", overridden: !!overridden["max_turns"], knobs: [{ key: "max_turns", value: values["max_turns"] ?? 42, overridden: !!overridden["max_turns"] }] },
  ],
});

test("scopeIndex flattens the groups, and no scope is the all-inherited floor", () => {
  assert.equal(Object.keys(scopeIndex(scope({}))).length, 4);
  assert.deepEqual(scopeIndex(undefined), {});
  for (const chip of fieldChips(undefined)) {
    assert.equal(chip.inherited, true, "no scope read = every knob inherits");
    assert.match(chip.note, /inherited from main/);
  }
});

test("an omitted knob is INHERITED and its chip names the source and value", () => {
  const chips = fieldChips(scope({}));
  assert.equal(chips.length, INHERITABLE_FIELDS.length);
  for (const chip of chips) {
    assert.equal(chip.inherited, true);
    assert.equal(chip.chip, "inherited from main");
  }
  const model = chipOf(chips, "provider.model_name")!;
  assert.equal(model.value, "glm-5.3", "an inherited knob still carries main's value");
  assert.equal(model.note, "model inherited from main (glm-5.3)");
  const turns = chipOf(chips, "max_turns")!;
  assert.equal(turns.value, "42", "a numeric knob reads as text for the input");
  assert.equal(turns.note, "max turns inherited from main (42)");
});

test("a knob the layer NAMES is AUTHORED - no chip, and it keeps its own value", () => {
  const chips = fieldChips(
    scope({ "provider.instance": true, system_prompt: true }, { "provider.instance": "work", system_prompt: "" }),
  );
  const provider = chipOf(chips, "provider.instance")!;
  assert.equal(provider.inherited, false);
  assert.equal(provider.chip, "");
  assert.equal(provider.note, "");
  assert.equal(provider.value, "work");
  // an explicitly EMPTY prompt is still an override, not an inherit
  const prompt = chipOf(chips, "system_prompt")!;
  assert.equal(prompt.inherited, false, "empty-and-authored is authored (the layer law)");
  // and the untouched knobs still inherit
  assert.equal(chipOf(chips, "max_turns")!.inherited, true);
});

// --- rulings 4 + 5: always off, allowlist ----------------------------

test("the toggles picker offers mounted, toggleable capsules and never the locked pair", () => {
  const options = toggleOptions([
    { name: "cron", state: "mounted", toggleable: true },
    { name: "kanban", state: "disabled", toggleable: true },
    { name: "store", state: "mounted", toggleable: false },
    { name: "agent-loop", state: "mounted", toggleable: true },
    { name: "provider", state: "mounted", toggleable: true },
    { name: "skills", state: "mounted" },
  ]);
  assert.deepEqual(options, ["cron", "skills"]);
  assert.deepEqual([...LOCKED_AGENT_MODULES], ["agent-loop", "provider"]);
});

test("a capsule the agent enables but the host has since disabled stays VISIBLE", () => {
  // hiding it would leave an invisible enabled entry: the next toggle
  // save carries the name and the subset law answers 400 with a refusal
  // the operator cannot act on
  assert.deepEqual(toggleChoices(["cron", "skills"], ["kanban"]), ["cron", "kanban", "skills"]);
  assert.deepEqual(toggleChoices(["cron"], []), ["cron"]);
  assert.deepEqual(toggleChoices([], ["kanban", "kanban"]), ["kanban"], "a set, not a list");
  assert.ok(panelSrc.includes("agents-check-off"), "the off-host entry renders dim");
  assert.match(panelSrc, /uncheck it here, or re-enable the capsule in settings/, "and says why");
});

test("an empty toggles set is a REAL answer, never main's feature set", () => {
  assert.match(agentTogglesNote([]), /nothing enabled/);
  assert.match(agentTogglesNote([]), /never inherits main's feature set/);
  assert.match(agentTogglesNote(["cron", "kanban"]), /^2 enabled/);
});

test("the skills allowlist is explicit - empty means none", () => {
  assert.match(agentSkillsNote([]), /no skills/);
  assert.match(agentSkillsNote([]), /never main's registry/);
  assert.match(agentSkillsNote(["probe"]), /^1 allowed/);
  assert.deepEqual(skillOptions({ skills: ["probe", "deploy"] }), ["probe", "deploy"]);
  assert.deepEqual(skillOptions({}), []);
});

// --- the SFC's own half (source pins - the node suite has no DOM) -----

test("the band carries exactly ONE create door and no @ duplicate", () => {
  const newButtons = (panelSrc.match(/class="agents-new band-tool"/g) ?? []).length;
  assert.equal(newButtons, 1, "one '+', not two doors");
  const createForms = (panelSrc.match(/class="agents-create"/g) ?? []).length;
  assert.equal(createForms, 1, "one create form, not two near-identical ones");
  assert.ok(!panelSrc.includes(">@</button>"), "the @ duplicate button died");
  assert.ok(!panelSrc.includes("showContCreate"), "the second create form died");
  assert.match(panelSrc, /title="create an agent"/, "the one door says what it does");
  assert.match(panelSrc, /aria-label="agent kind"/, "the kind choice lives inside the one form");
});

test("the panel DELETEs the home and names the count before the trigger", () => {
  assert.match(panelSrc, /method: "DELETE"/, "the delete rides DELETE");
  assert.match(panelSrc, /fetch\(`\/agents\/\$\{encodeURIComponent\(name\)\}`, \{\s*method: "DELETE"/, "the home, not a flag");
  assert.match(panelSrc, /agentDeleteConfirm\(row\.name, sessionCountOf\(row\.name\)\)/, "the confirm names the count");
  assert.match(panelSrc, /session_count/, "the count is the roster row's own fact");
  assert.match(panelSrc, /sessions\?: number/, "the receipt answers the same count");
});

test("the provider renders as a referenced instance picker from settings", () => {
  assert.match(panelSrc, /aria-label="provider instance"/);
  assert.match(panelSrc, /v-for="inst in providerInstances"/, "the options come from settings");
  assert.match(panelSrc, /fetch\("\/settings"\)/, "read from the settings payload");
  assert.match(panelSrc, /k\.key === "provider\.instances"/, "the named instances knob");
  assert.match(panelSrc, /providerInstances\.value = rows/);
  // the secret surface is gone, not hidden: no key input, no password
  // field, no host box anywhere in the panel.
  assert.ok(!panelSrc.includes('aria-label="api key"'), "no api key input in the panel");
  assert.ok(!panelSrc.includes('type="password"'), "no secret input at all");
  assert.ok(!panelSrc.includes('aria-label="provider host"'), "no host field");
  assert.match(rowsSrc, /agentProviderText/, "the row voice names the reference");
});

test("the chips render the inherited state and the reset is the layer's unset verb", () => {
  assert.match(panelSrc, /agents-inherit-chip/, "the chip renders");
  assert.match(panelSrc, /chipInherited\(row\.name, 'system_prompt'\)/, "per-knob state, not a per-form flag");
  assert.match(panelSrc, /chipInherited\(row\.name, 'provider\.instance'\)/);
  assert.match(panelSrc, /chipInherited\(row\.name, 'provider\.model_name'\)/);
  assert.match(panelSrc, /chipInherited\(row\.name, 'max_turns'\)/);
  assert.match(panelSrc, /JSON\.stringify\(\{ unset: \[key\] \}\)/, "reset DELETES the key so it inherits again");
});

test("toggles and the skills allowlist render as explicit sets", () => {
  assert.match(panelSrc, /aria-label="toggles"/);
  assert.match(panelSrc, /aria-label="skills"/);
  assert.match(panelSrc, /agentTogglesNote\(/, "the always-off note is the shared voice");
  assert.match(panelSrc, /agentSkillsNote\(/, "the allowlist note is the shared voice");
  assert.match(panelSrc, /v-for="m in toggleChoicesFor\(row\.name\)"/, "the union set (an off-host entry stays visible)");
  assert.match(panelSrc, /v-for="s in skillOptionsFor\(row\.name\)"/);
});

test("the clone door sends the landed inherit_from create (no duplicate endpoint)", () => {
  assert.match(panelSrc, /createRequest\(create\)/, "the routing is the pure function");
  assert.match(panelSrc, /cloneCandidates/, "the source picker lists the other homes");
  assert.match(rowsSrc, /inherit_from: from/);
  // the old duplicate route never comes back: clone is a CREATE, not an @
  assert.ok(!panelSrc.includes("/duplicate"), "no duplicate endpoint");
  assert.ok(!panelSrc.includes("/clone"), "no clone endpoint either - POST /agents with inherit_from is the door");
});
