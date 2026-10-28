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
  type OmegaAgent,
} from "../app/components/agents/agentsRows.ts";

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