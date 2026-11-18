// omega webui: the Cron tab's INHERITANCE CHAIN. The chain's display is pure data: this
// suite pins the resolution and the editor's own law (a wake may not lean on the main
// agent, which never wakes as a subprocess - harness/cron/save.go refuses the same
// shape on the write face).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  MAIN_AGENT_FALLBACK,
  agentChain,
  chainViolation,
  emptyDraft,
  mainAgentOf,
  modelChain,
  validateDraft,
  type ChainDraft,
  type FieldOption,
  type JobDraft,
} from "../../harness/cron/ui/edit.ts";

// the roster as GET /agents answers it.
const ROSTER: FieldOption[] = [
  {
    value: "default",
    label: "default",
    hint: "the main agent - in-process, never wakes",
    group: "agents",
    model: "",
    continuous: false,
  },
  {
    value: "scout",
    label: "scout",
    hint: "patrols",
    group: "agents",
    model: "glm-5.3",
    continuous: true,
  },
  {
    value: "scribe",
    label: "scribe",
    hint: "writes it down",
    group: "agents",
    model: "",
    continuous: true,
  },
];

const MAIN_MODEL = "deepseek-v4.1-flash";

function chain(over: Partial<ChainDraft> = {}): ChainDraft {
  return { agent: "", model: "", deliver: "new-session", ...over };
}

// -- the two links --

test("mainAgentOf reads the roster's main mark, and falls back to the Go constant", () => {
  assert.equal(mainAgentOf(ROSTER), "default");
  // an unanswered GET /agents must still render the chain honestly
  assert.equal(mainAgentOf([]), MAIN_AGENT_FALLBACK);
  assert.equal(mainAgentOf([]), "default", "mirrors agent.DefaultAgentName");
});

test("a job with no agent INHERITS the main agent", () => {
  const c = agentChain(chain(), ROSTER);
  assert.equal(c.inherited, true);
  assert.equal(c.value, "default", "the fire resolves to the main agent");
  assert.equal(c.source, "the main agent");
  assert.equal(c.note, "inherits the main agent (default)");
  assert.equal(c.blocked, false);
});

test("an authored agent is AUTHORED - no chip, no note, no resolution", () => {
  const c = agentChain(chain({ agent: "scout" }), ROSTER);
  assert.equal(c.inherited, false);
  assert.equal(c.value, "scout");
  assert.equal(c.source, "");
  assert.equal(c.note, "");
  assert.equal(c.blocked, false);
});

test("a job with no model INHERITS from the agent the fire runs under", () => {
  // in-process fires run as the main agent (runner.go's wrong-target law), so the.
  const inproc = modelChain(chain(), ROSTER, MAIN_MODEL);
  assert.equal(inproc.inherited, true);
  assert.equal(inproc.value, MAIN_MODEL);
  assert.equal(inproc.source, "the main agent");
  assert.equal(inproc.note, `inherits the main agent (${MAIN_MODEL})`);

  // a wake runs inside the agent's own home: the agent's own config layer answers first
  const wakeOwn = modelChain(
    chain({ agent: "scout", deliver: "continuous-agent" }),
    ROSTER,
    MAIN_MODEL,
  );
  assert.equal(wakeOwn.inherited, true);
  assert.equal(wakeOwn.value, "glm-5.3", "the agent's own model layer answers");
  assert.equal(wakeOwn.source, "agent scout");
  assert.equal(wakeOwn.note, "inherits agent scout (glm-5.3)");
});

test("the model link's middle link can itself inherit the main agent", () => {
  const c = modelChain(
    chain({ agent: "scribe", deliver: "continuous-agent" }),
    ROSTER,
    MAIN_MODEL,
  );
  assert.equal(c.inherited, true);
  assert.equal(c.value, MAIN_MODEL, "a layer with no model_name inherits the main");
  assert.equal(c.source, "agent scribe, which inherits the main agent");
  assert.equal(
    c.note,
    `inherits agent scribe, which inherits the main agent (${MAIN_MODEL})`,
    "the note names the whole chain, not just the last hop",
  );
});

test("an authored model is AUTHORED whatever the agent says", () => {
  const c = modelChain(
    chain({ agent: "scout", model: "clef:27b", deliver: "continuous-agent" }),
    ROSTER,
    MAIN_MODEL,
  );
  assert.equal(c.inherited, false);
  assert.equal(c.value, "clef:27b");
  assert.equal(c.note, "");
});

test("an agent the registry does not list leaves the model chain unknown, never invented", () => {
  const c = modelChain(
    chain({ agent: "ghost", deliver: "continuous-agent" }),
    ROSTER,
    MAIN_MODEL,
  );
  assert.equal(c.inherited, true);
  assert.equal(c.value, "", "the value is unknown - the chain does not guess");
  assert.equal(c.source, "agent ghost");
  assert.match(c.note, /not in the registry/);
});

test("a degraded catalog names the source without a value", () => {
  const c = modelChain(chain(), ROSTER, "");
  assert.equal(c.inherited, true);
  assert.equal(c.value, "");
  assert.equal(c.note, "inherits the main agent");
});

// -- the state machine's state is PRESENCE, not a value --

test("authored and inherited are distinct states even when they resolve alike", () => {
  // the same resolved agent, two different states: one authored, one inherited. This is
  // the pencil fix's law applied to the chain - one field must never be read as both
  const authored = agentChain(chain({ agent: "default" }), ROSTER);
  const inherited = agentChain(chain(), ROSTER);
  assert.equal(authored.value, inherited.value, "they resolve to the same agent");
  assert.notEqual(
    authored.inherited,
    inherited.inherited,
    "and yet they are different states - the chip is the only tell",
  );
});

// -- the chain's law: a wake cannot lean on the main agent --

test("a wake inheriting the main agent is BLOCKED, and says why", () => {
  const c = agentChain(chain({ deliver: "continuous-agent" }), ROSTER);
  assert.equal(c.inherited, true);
  assert.equal(c.value, "default");
  assert.equal(c.blocked, true);
  assert.equal(c.note, "inherits the main agent (default), which never wakes");
  const m = modelChain(chain({ deliver: "continuous-agent" }), ROSTER, MAIN_MODEL);
  assert.equal(m.blocked, true, "the whole chain reads as blocked");
  // ..
  assert.equal(agentChain(chain(), ROSTER).blocked, false);
});

test("chainViolation refuses exactly the blocked chain", () => {
  const msg = chainViolation(chain({ deliver: "continuous-agent" }), "default");
  assert.match(msg, /continuous-agent needs its own agent/);
  assert.match(msg, /never wakes/);
  // an authored agent is the operator's call (a ghost agent still saves - the
  // never-strand law.
  assert.equal(
    chainViolation(chain({ agent: "scout", deliver: "continuous-agent" }), "default"),
    "",
  );
  // an AUTHORED reference to the main agent IS the absence (save.go normalizes it.
  assert.notEqual(
    chainViolation(chain({ agent: "default", deliver: "continuous-agent" }), "default"),
    "",
    "picking the main row is the same non-answer as an empty box",
  );
  assert.equal(chainViolation(chain(), "default"), "");
});

test("validateDraft carries the chain's law last and takes the roster's main agent", () => {
  const base = { ...emptyDraft(), name: "probe", schedule: "0 8 * * *", prompt: "p" };
  assert.equal(validateDraft(base), "", "the default draft is a valid in-process job");
  const wake: JobDraft = { ...base, deliver: "continuous-agent" };
  assert.match(validateDraft(wake), /needs its own agent/);
  assert.equal(
    validateDraft({ ...wake, agent: "scout" }),
    "",
    "a wake with its own agent saves",
  );
  assert.equal(validateDraft({ ...wake, prompt: " " }), "prompt is required");
  assert.equal(
    validateDraft({ ...wake, agent: "default" }, "main-2"),
    "",
    "a name that is not THIS lane's main agent is an ordinary agent",
  );
  assert.match(
    validateDraft({ ...wake, agent: "main-2" }, "main-2"),
    /needs its own agent/,
    "the lane's own main agent is the chain's absence",
  );
});

// -- the SFC wiring (source pins: the node suite has no DOM) --------

test("CronJobs.vue renders the chip and the chain note, and keeps the draft empty", () => {
  const src = readFileSync(
    fileURLToPath(new URL("../../harness/cron/ui/CronJobs.vue", import.meta.url)),
    "utf8",
  );
  // ONE chip + ONE note, driven by the ONE chain function over COMBO_FIELDS.
  assert.match(src, /v-for="f in COMBO_FIELDS"/);
  assert.match(src, /v-if="chainOf\(f\.field\)\.inherited"/);
  assert.match(src, /class="cron-inherited-chip"/);
  assert.match(src, /class="cron-field-note cron-chain-note"/);
  assert.match(src, /\{\{ chainOf\(f\.field\)\.note \}\}/);
  // the blocked chain speaks in the danger voice on BOTH halves
  assert.match(src, /:class="\{ blocked: chainOf\(f\.field\)\.blocked \}"/);
  // the resolution is DISPLAY ONLY: the field still binds the draft
  // (empty = inherit), never the resolved value
  assert.match(src, /v-model="editor\.draft\[f\.field\]"/);
  assert.ok(
    !/v-model="chainOf\(/.test(src),
    "the chain must never write a resolved value back into the field",
  );
  assert.match(src, /modelChain\(chain, agentOptions\.value, modelCurrent\.value\)/);
  assert.match(src, /validateDraft\(\s*editor\.value\.draft,\s*mainAgentOf\(agentOptions\.value\),?\s*\)/);
});
