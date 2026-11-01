// omega webui: the registry-browser AGENTS tab toggle behavior (v0.14.1,
// card t_372b9e9a). Pure-surface test: the row model + the soft-toggle
// law + the source greps (the composition-test convention).
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
} from "../app/components/agents/agentsRows.ts";
import type { OmegaContinuousRow } from "../app/components/agents/continuousRows.ts";

// the AgentsPanel source is three levels up from test/ (the repo root)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

// --- agent rows: the config summary is the expandable detail ---

test("agentDetailLines renders the config summary honestly", () => {
  const a: OmegaAgent = {
    name: "bots/patrol",
    dir: "/home/x/.omega-dev/agents/bots_patrol",
    description: "patrol bot",
    provider_type: "openai",
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
    "provider: openai",
    `dir: ${a.dir}`,
  ]);
  assert.ok(lines.includes("skills: a, b"));
  assert.ok(lines.includes("memory: 2kb"));
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

// --- the soft toggle: the fold flags the row, the note says the consequence ---

test("the agents fold merges by name; the disable note names roster+wake", () => {
  const fold = normalizeDisabledPayload({ agents: ["bots/patrol"] }).agents;
  assert.deepEqual(fold, ["bots/patrol"]);
  assert.equal(registryNote("agents", "bots/patrol", false), "disable bots/patrol? roster row and wake triggers off; home untouched");
  assert.equal(registryNote("agents", "bots/patrol", true), "");
});

test("AgentsPanel renders the shared pattern and the SOFT toggle wire", () => {
  const src = readFileSync(join(root, "engine", "webui", "app", "components", "agents", "AgentsPanel.vue"), "utf8");
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes('kind="agents"'), "the agents namespace rides to the note law");
  assert.ok(src.includes("/disabled/agents"), "the soft toggle PUTs the agents namespace");
});

// --- card t_2d8deb8c: the locked default agent + details-not-sessions ---

// the status vocabulary (Endo's ruling): on = green (any pulse),
// sleeping = dim. A config-only agent renders no invented state.
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

// the locked law: the default row never renders a disable affordance.
test("AgentsPanel locks the default agent (no disabled render, no arm path)", () => {
  const panel = readFileSync(join(root, "engine", "webui", "app", "components", "agents", "AgentsPanel.vue"), "utf8");
  const list = readFileSync(join(root, "engine", "webui", "app", "components", "RegistryList.vue"), "utf8");
  assert.ok(panel.includes("locked: a.name === DefaultAgentName"), "the default row is locked");
  assert.ok(panel.includes('a.name === DefaultAgentName ? false : set.has(a.name)'), "the default row never renders disabled");
  assert.ok(list.includes("r.locked"), "RegistryList seats the locked flag");
  assert.ok(list.includes("if (r.locked) return;"), "the arm step never arms a locked row");
  assert.ok(list.includes("!!r.locked"), "the pill renders non-interactive");
  // the backend mirror: the handler refuses a curl too (the Go gate
  // pins it; this is the UI-side anchor to the same law text).
  assert.ok(list.includes("cannot be disabled"), "the locked aria states the law");
});

// ruling 3: the expansion is details + edit; zero session rows.
test("AgentsPanel expansion: config edit wire, no session surface", () => {
  const panel = readFileSync(join(root, "engine", "webui", "app", "components", "agents", "AgentsPanel.vue"), "utf8");
  assert.ok(panel.includes(`fetch(\`/agents/\${`), "the edit writes through the agents API");
  assert.ok(panel.includes('"PUT"'), "the edit is a PUT");
  assert.ok(panel.includes("saveAgentEdit"), "the save affordance exists");
  for (const field of ["description", "provider_type", "model_name", "api_key", "system_prompt"]) {
    assert.ok(panel.includes(`agents-edit-input`), `the ${field} affordance rides the draft form`);
  }
  assert.ok(!panel.includes("agents-sessions"), "the session list died");
  assert.ok(!panel.includes("new session as this agent"), "the new-session button died");
  assert.ok(!panel.includes("/sessions`"), "no session fetch rides the tab");
  // the detail facts lost the sessions line (ruling 3)
  const rows = readFileSync(join(root, "engine", "webui", "app", "components", "agents", "agentsRows.ts"), "utf8");
  assert.ok(!rows.includes("`sessions: ${"), "the sessions count line died from the detail");
});