// omega webui: unit tests for the agents surface (card t_7e2b7002).
// Two layers, matching the house patterns:
// - agentsRows.ts (PURE): the display contracts - model voice, honest
//   memory (0 = "-", never "0b"), empty-state rules, session titles;
// - AgentsPanel.vue: the wire contract the deferred mount will serve -
//   the panel fetches /agents, GET/POST /agents/{name}/sessions, and
//   POST /agents, WITHOUT touching omega.ts (same-file ruling) until
//   the wiring pass folds it in.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  agentMemoryText,
  agentModelText,
  agentSessionTitle,
  agentSkillsText,
  agentsListIsEmpty,
} from "../app/components/agents/agentsRows.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..", "..");

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

// --- agentsRows.ts: display contracts ---

test("agent model voice: model_name wins, default falls back to home-config voice", () => {
  const scout: any = { name: "scout", skill_count: 0, memory_size: 0, model_name: "qwen3" };
  assert.equal(agentModelText(scout), "qwen3");
  const bare: any = { name: "default", skill_count: 0, memory_size: 0 };
  assert.equal(agentModelText(bare), "(home config)");
  const orphan: any = { name: "newborn", skill_count: 0, memory_size: 0 };
  assert.equal(agentModelText(orphan), "", "a non-default agent with no model shows nothing (its layer decides)");
});

test("agent memory text: honest zero, never a phantom 0b", () => {
  assert.equal(agentMemoryText({ name: "a", skill_count: 0, memory_size: 0 } as any), "-");
  assert.equal(agentMemoryText({ name: "a", skill_count: 0, memory_size: 12 } as any), "12b");
  assert.equal(agentMemoryText({ name: "a", skill_count: 0, memory_size: 4096 } as any), "4kb");
  assert.equal(agentMemoryText({ name: "a", skill_count: 0, memory_size: 2500 } as any), "2kb");
});

test("agent skills text: names when present, empty string when none", () => {
  assert.equal(agentSkillsText({ name: "a", skill_count: 2, memory_size: 0, skills: ["deploy", "probe"] } as any), "deploy, probe");
  assert.equal(agentSkillsText({ name: "a", skill_count: 0, memory_size: 0 } as any), "");
});

test("agent session title: label, else truncated id (sessions-sidebar voice)", () => {
  assert.equal(agentSessionTitle({ id: "abcdef1234567890", label: "nightly probe" }), "nightly probe");
  assert.equal(agentSessionTitle({ id: "abcdef1234567890" }), "abcdef123456...");
});

test("agents list is never empty by construction: default exists", () => {
  const one = [{ name: "default", skill_count: 0, memory_size: 0 }];
  assert.equal(agentsListIsEmpty(one), false, "a default-only home renders one row");
  assert.equal(agentsListIsEmpty([]), true);
});

// --- AgentsPanel.vue: the wire contract the deferred mount serves ---

test("AgentsPanel fetches the real endpoints, no omega.ts dependency", () => {
  const vue = read("modules", "ui", "app", "components", "agents", "AgentsPanel.vue");
  assert.match(vue, /["'`]\/agents["'`]/, "GET inventory");
  assert.match(vue, /agents\/\$\{[^}]+\}\/sessions/, "agent-scoped sessions route");
  assert.match(vue, /method: ["'`]POST["'`]/, "scaffold + create-as-agent POSTs");
  assert.ok(!/composables\/omega/.test(vue), "no omega.ts import (same-file ruling: owned by t_14f73742; the wiring pass folds it)");
});

test("AgentsPanel exposes the card's primary action and honest empty states", () => {
  const vue = read("modules", "ui", "app", "components", "agents", "AgentsPanel.vue");
  assert.match(vue, /new session as this agent/, "the primary action button");
  assert.match(vue, /no sessions as this agent/, "honest session-list empty state");
  assert.match(vue, /agentsRows/, "renders from the pure model");
});