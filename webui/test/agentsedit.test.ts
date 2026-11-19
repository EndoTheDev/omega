// omega webui: the agents panel's EDIT WRITE - the pure diff behind PUT /agents/{name}.
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  AGENT_INHERIT_KINDS,
  editRequest,
  fieldChips,
  type AgentEditDraft,
  type OmegaAgent,
  type SettingsScope,
} from "../../harness/continuous/ui/agentsRows.ts";

const scope: SettingsScope = {
  name: "scout",
  groups: [
    {
      key: "provider",
      overridden: false,
      knobs: [
        { key: "provider.instance", value: "default", overridden: false },
        { key: "provider.model_name", value: "glm-5.3", overridden: false },
      ],
    },
    { key: "system_prompt", overridden: false, knobs: [{ key: "system_prompt", value: "main prompt", overridden: false }] },
    { key: "max_turns", overridden: false, knobs: [{ key: "max_turns", value: 100, overridden: false }] },
  ],
};

const chips = fieldChips(scope);

const saved: OmegaAgent = {
  name: "scout",
  dir: "/home/x/.omega/agents/scout",
  description: "probe duty",
  skill_count: 0,
  memory_size: 0,
  session_count: 0,
};

function draft(over: Partial<AgentEditDraft> = {}): AgentEditDraft {
  const inherit = {} as Record<(typeof AGENT_INHERIT_KINDS)[number], boolean>;
  for (const kind of AGENT_INHERIT_KINDS) inherit[kind] = false;
  return {
    description: "probe duty",
    system_prompt: "main prompt",
    provider_instance: "default",
    model_name: "glm-5.3",
    max_turns: "100",
    toggles: [],
    skills_allow: [],
    inherit,
    ...over,
  };
}

test("an untouched draft writes nothing at all (the no-changes beat)", () => {
  assert.equal(editRequest(draft(), saved, chips), null);
});

test("a NUMBER from the number input does not kill the save (the live repro)", () => {
  const body = editRequest(draft({ max_turns: 7 }), saved, chips);
  assert.ok(body, "the save still builds a body");
  assert.equal(body!.max_turns, 7, "the typed cap is the one number that changed");
  assert.equal(Object.keys(body!).length, 1, "nothing else rode along");
});

test("clearing the number box is the reset verb, not a zero cap", () => {
  const body = editRequest(draft({ max_turns: "" }), saved, chips);
  assert.deepEqual(body, { unset: ["max_turns"] });
});

test("the diff carries only what changed, and sets compare as sets", () => {
  const body = editRequest(
    draft({ toggles: ["cron"], skills_allow: ["probe"], provider_instance: "work" }),
    { ...saved, toggles: [], skills_allow: ["probe"] },
    chips,
  );
  assert.deepEqual(body, { provider_instance: "work", toggles: ["cron"] });
  // the same set in another order is NOT a change
  assert.equal(
    editRequest(draft({ toggles: ["b", "a"] }), { ...saved, toggles: ["a", "b"] }, chips),
    null,
  );
});

test("an inherited knob that was typed over is written; an equal value is not", () => {
  assert.deepEqual(
    editRequest(draft({ system_prompt: "agent prompt" }), saved, chips),
    { system_prompt: "agent prompt" },
  );
  // typing main's own value leaves the knob INHERITED (no phantom override)
  assert.equal(editRequest(draft({ system_prompt: "main prompt" }), saved, chips), null);
  // and an explicitly empty prompt IS an authored value (the layer law)
  assert.deepEqual(editRequest(draft({ system_prompt: "" }), saved, chips), { system_prompt: "" });
});

test("the inherit flags travel as one block when any kind flips", () => {
  const d = draft();
  d.inherit.skills = true;
  const body = editRequest(d, saved, chips);
  assert.deepEqual(body, { inherit: { skills: true, soul: false, user: false, memory: false } });
  assert.equal(editRequest(d, { ...saved, inherits: ["skills"] }, chips), null);
});
