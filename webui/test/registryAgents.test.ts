// omega webui: the registry-browser AGENTS tab toggle behavior.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  normalizeDisabledPayload,
  registryNote,
  type RegistryRow,
} from "../app/composables/omega.ts";
import {
  agentDetailLines,
  agentStatusFor,
  type OmegaAgent,
} from "../../harness/continuous/ui/agentsRows.ts";
import type { OmegaContinuousRow } from "../../harness/continuous/ui/continuousRows.ts";

// the AgentsPanel source is three levels up from test/ (the repo root)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

// -- agent rows: the config summary is the expandable detail --

test("agentDetailLines renders the config summary honestly", () => {
  const a: OmegaAgent = {
    name: "bots/patrol",
    dir: "/home/x/.omega-dev/agents/bots_patrol",
    description: "patrol bot",
    provider_instance: "work",
    model_name: "gpt-5.x",
    skill_count: 2,
    skills: ["a", "b"],
    memory_size: 2048,
    session_count: 3,
    last_activity: "2026-10-28T12:00:00Z",
  };
  const lines = agentDetailLines(a);
  assert.deepEqual(lines.slice(0, 3), [
    "model: gpt-5.x",
    "provider: work",
    `dir: ${a.dir}`,
  ]);
  assert.ok(lines.includes("skills: a, b"));
  assert.ok(lines.includes("memory: 2kb"));
  // 0.17.3: an agent with no instance reference runs on main's provider -
  // the line says which, never a phantom type (the row is secret-free)
  const inherited: OmegaAgent = {
    name: "newborn",
    dir: "/home/x/.omega/agents/newborn",
    skill_count: 0,
    memory_size: 0,
    session_count: 0,
  };
  assert.ok(agentDetailLines(inherited).includes("provider: (main's provider)"));
  // missing facts render "-": never a phantom empty row voice
  const bare: OmegaAgent = {
    name: "bots/patrol",
    dir: "/home/x/.omega-dev/agents/bots_patrol",
    skill_count: 0,
    memory_size: 0,
    session_count: 0,
  };
  assert.ok(
    agentDetailLines(bare).filter((l) => l.endsWith(": -")).length >= 2,
    "missing facts say -",
  );
});

// -- the soft toggle: the fold flags the row, the note says the consequence --

test("the agents fold merges by name; the disable note names roster+wake", () => {
  const fold = normalizeDisabledPayload({ agents: ["bots/patrol"] }).agents;
  assert.deepEqual(fold, ["bots/patrol"]);
  assert.equal(registryNote("agents", false), "roster row and wake triggers off; home untouched");
  assert.equal(registryNote("agents", true), "");
});

test("AgentsPanel renders the shared pattern and the SOFT toggle wire", () => {
  const src = readFileSync(join(root, "harness", "continuous", "ui", "AgentsPanel.vue"), "utf8");
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes('kind="agents"'), "the agents namespace rides to the note law");
  assert.ok(src.includes("/disabled/agents"), "the soft toggle PUTs the agents namespace");
});

// --: the locked default agent + details-not-sessions --

// the status vocabulary: on = green (any pulse), sleeping = dim.
test("agentStatusFor: pulse is on, sleeping is dim, config-only is undefined", () => {
  const pulse: OmegaContinuousRow = { name: "coder", next_wake: "2026-11-01T17:00:00Z", unread: false } as unknown as OmegaContinuousRow;
  const sleeping: OmegaContinuousRow = { name: "hello", unread: false } as unknown as OmegaContinuousRow;
  const unread: OmegaContinuousRow = { name: "coder", unread: true } as unknown as OmegaContinuousRow;
  const pulseStatus = agentStatusFor(pulse);
  assert.ok(pulseStatus?.on, "a scheduled wake is on");
  assert.match(pulseStatus!.label, /^next wake/);
  const sleepStatus = agentStatusFor(sleeping);
  assert.equal(sleepStatus!.on, false, "sleeping is dim");
  assert.equal(sleepStatus!.label, "sleeping");
  assert.ok(agentStatusFor(unread)?.on, "an unread report pulses");
  assert.equal(agentStatusFor(null), undefined, "config-only: no invented state");
});

// the no-toggle law: the default row never renders a disable affordance.
test("AgentsPanel gives the default agent no toggle (no disabled render, no write path)", () => {
  const panel = readFileSync(join(root, "harness", "continuous", "ui", "AgentsPanel.vue"), "utf8");
  const list = readFileSync(join(root, "webui", "app", "components", "RegistryList.vue"), "utf8");
  assert.ok(panel.includes("noToggle: a.name === DefaultAgentName"), "the default row is the no-toggle row");
  assert.ok(panel.includes('a.name === DefaultAgentName ? false : set.has(a.name)'), "the default row never renders disabled");
  assert.ok(list.includes("r.noToggle"), "RegistryList seats the no-toggle flag");
  assert.ok(list.includes("if (r.noToggle) return;"), "the write path refuses a no-toggle row by another door");
  assert.ok(list.includes('v-if="toggleable && !r.noToggle"'), "the pill never renders for it");
  // the backend mirror: the handler refuses a curl too (the Go gate pins it.
  assert.ok(panel.includes("if (name === DefaultAgentName && !enabled) return;"), "the toggle handler refuses the default agent");
});

// the expansion is details + edit. ADDS one section and reverses the sessions half: an
// agent's WORK sessions are inspected HERE now (they left the human's sessions
// sidebar), so the tab reads `/agents/<name>/sessions` - the agent's own store - and
// never the human's `/sessions` list.
test("AgentsPanel expansion: config edit wire, plus the agent's OWN sessions", () => {
  const panel = readFileSync(join(root, "harness", "continuous", "ui", "AgentsPanel.vue"), "utf8");
  assert.ok(panel.includes(`fetch(\`/agents/\${`), "the edit writes through the agents API");
  assert.ok(panel.includes('"PUT"'), "the edit is a PUT");
  assert.ok(panel.includes("saveAgentEdit"), "the save affordance exists");
  // the definition layer's field set (0.17.3): every knob rides the draft
  // form; the retired secret knobs are GONE, not merely empty.
  for (const field of ["provider instance", "model name", "max turns", "system prompt", "agent description"]) {
    assert.ok(panel.includes(`aria-label="${field}"`), `the ${field} affordance rides the draft form`);
  }
  assert.ok(!panel.includes('aria-label="api key"'), "no api key input survives the secret-free law");
  assert.ok(!panel.includes('aria-label="provider host"'), "no host input survives the secret-free law");
  assert.ok(!panel.includes("agents-sessions"), "the old embedded session list stays dead");
  assert.ok(!panel.includes("new session as this agent"), "the new-session button stays dead");
  // card t_10ac04f0: the per-agent sessions read is the inspection surface
  assert.ok(
    panel.includes("`/agents/${encodeURIComponent(name)}/sessions`"),
    "the expansion reads the agent's own sessions",
  );
  assert.ok(
    !panel.includes('fetch("/sessions'),
    "the human's own session list is never fetched from this tab",
  );
  // the detail facts lost the sessions line (ruling 3)
  const rows = readFileSync(join(root, "harness", "continuous", "ui", "agentsRows.ts"), "utf8");
  assert.ok(!rows.includes("`sessions: ${"), "the sessions count line died from the detail");
});