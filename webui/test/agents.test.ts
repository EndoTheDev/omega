// omega webui: unit tests for the agents surface (cards t_7e2b7002 +
// t_c7222095 + t_2d8deb8c). Two layers, matching the house patterns:
// - agentsRows.ts (PURE): the display contracts - model voice, honest
//   memory (0 = "-", never "0b"), empty-state rules, the status
//   vocabulary (dot + word);
// - AgentsPanel.vue: the wire contract - the panel fetches /agents
//   and PUT/POST /agents ({name}?), and (t_2d8deb8c) RENDERS NO
//   SESSION SURFACE: the expansion is details + edit.
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
} from "../../harness/continuous/ui/agentsRows.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..");

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

// --- AgentsPanel.vue: the wire contract ---

test("AgentsPanel fetches the real endpoints (the wiring pass folded omega.ts in)", () => {
  const vue = read("webui", "..", "harness", "continuous", "ui", "AgentsPanel.vue");
  assert.match(vue, /["'`]\/agents["'`]/, "GET inventory");
  // card t_2d8deb8c: the edit affordance replaces the session surface -
  // the tab's agent-scoped write is PUT /agents/{name}; the sessions
  // route stays in the API but the TAB no longer fetches it.
  assert.match(vue, /\/agents\/\$\{[^}]+\}["'`]/, "the agent-scoped PUT route");
  assert.match(vue, /method: ["'`]PUT["'`]/, "the edit is a PUT");
  assert.match(vue, /method: ["'`]POST["'`]/, "scaffold POST");
  // t_372b9e9a: the wiring pass landed - the panel binds the registry
  // fold helpers through composables/omega (RegistryRow type at least)
  assert.match(vue, /composables\/omega/, "the shared registry helpers are the wire");
  assert.match(vue, /\/disabled\/agents/, "and the fold wire is the agents namespace");
});

// --- card t_2d8deb8c, ruling 3: the expansion is details + edit ---

test("AgentsPanel expansion carries the edit affordance, not sessions", () => {
  const vue = read("webui", "..", "harness", "continuous", "ui", "AgentsPanel.vue");
  assert.match(vue, /class="agents-edit"/, "the edit form renders");
  assert.match(vue, /saveAgentEdit/, "the save affordance exists");
  for (const field of ["agent description", "provider type", "model name", "api key", "system prompt"]) {
    assert.match(vue, new RegExp(`aria-label="${field.replace(/ /g, "\\s")}"`), `the ${field} field is reachable`);
  }
  // DEAD surfaces: the tab renders no session rows, no session
  // button, no session fetch - the API keeps /agents/{name}/sessions,
  // the surfaces that still want it own it later.
  assert.doesNotMatch(vue, /agents-sessions/, "the session list died");
  assert.doesNotMatch(vue, /new session as this agent/, "the new-session button died");
  assert.doesNotMatch(vue, /fetch\([^)]*\/sessions/, "no session fetch rides the tab");
  assert.doesNotMatch(vue, /loadSessions|openAgentSession|newSessionAs/, "the session code died");
});

test("the default agent is read-only in the expansion (its config is the home config)", () => {
  const vue = read("webui", "..", "harness", "continuous", "ui", "AgentsPanel.vue");
  assert.match(vue, /row\.name === DefaultAgentName/, "the default agent branches off");
  assert.match(vue, /agents-readonly-note/, "the read-only note explains why");
});

// --- the list contract (card t_619832ee) ---

test("AgentsPanel coerces a 200/null body, never crashes the render", () => {
  const vue = read("webui", "..", "harness", "continuous", "ui", "AgentsPanel.vue");
  // Trust boundary: the backend used to answer a zero-session agent with
  // the literal body `null`, detailSessions.length threw, and the whole
  // sidebar went blank. Every list fetch carries the ?? [] guard; the
  // merge (t_c7222095) kept the roster/chat/evidence reads (t_2d8deb8c
  // removed the sessions read + its guard): five guards now.
  const guardCount = (src: string): number => (src.match(/\?\? \[\]/g) ?? []).length;
  assert.equal(guardCount(vue), 5, "one ?? [] guard per list fetch (agents, roster, chat, evidence fetch + summary read)");
});

// --- t_c7222095: the Agents/Continuous merge (one tab) ---

test("AgentsPanel merges the continuous roster into the rows (status word, no preview)", () => {
  const vue = read("webui", "..", "harness", "continuous", "ui", "AgentsPanel.vue");
  assert.match(vue, /fetch\("\/continuous\/roster"\)/, "the roster loads with the agents");
  assert.match(vue, /continuousIndex\(roster\.value\)/, "the roster joins by name");
  assert.match(vue, /mergedRowDetail\(a, c\)/, "the detail carries the wake line");
  // t_2d8deb8c: the status vocabulary - the dot+word rides the row
  // law's SUBTITLE line through RegistryRow.status; the old inline
  // badge and the under-row preview line died with ruling 3.
  assert.doesNotMatch(vue, /agents-badge/, "the inline badge died");
  assert.doesNotMatch(vue, /preview:/, "the under-row preview died");
  assert.match(vue, /agentStatusFor\(/, "the status word is the roster voice");
  assert.doesNotMatch(vue, /import ContinuousPanel|<ContinuousPanel/, "no panel import or mount survives");
});