// omega webui: the continuous roster pane (card t_72ecca67, ruling 18)
// - the pure view-model helpers (continuousRows.ts) unit-tested, and
// the panel/registry wiring source-checked: the dockable entry, the
// app outlet, the wired picker entry, and the create flow's REST
// surface. Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import {
  evidenceSummary,
  previewLine,
  rowStatus,
  shortWhen,
  verifierVoice,
  type OmegaContinuousRow,
  type OmegaEvidenceRow,
} from "../app/components/agents/continuousRows.ts";
import { DOCK_COMPONENTS, DOCKABLES, dockableOf } from "../app/composables/dockables.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");
const panel = src("app/components/agents/ContinuousPanel.vue");
const app = src("app/app.vue");

function row(overrides: Partial<OmegaContinuousRow> = {}): OmegaContinuousRow {
  return {
    name: "scout",
    skill_count: 0,
    memory_size: 0,
    dir: "/nonexistent",
    unread: false,
    ...overrides,
  };
}

describe("continuous view-model helpers", () => {
  it("rowStatus never invents a wake: unread, next wake, or sleeping", () => {
    assert.equal(rowStatus(row({ unread: true })), "unread report");
    assert.equal(rowStatus(row({ next_wake: "2026-10-28T12:34:00Z" })), "next wake 12:34");
    assert.equal(rowStatus(row()), "sleeping");
  });

  it("previewLine falls back to the no-report line", () => {
    assert.equal(previewLine(row({ last_report: "patrol complete" })), "patrol complete");
    assert.match(previewLine(row()), /no report yet/);
  });

  it("evidenceSummary counts without fabricating a verdict on zero rows", () => {
    assert.equal(evidenceSummary([]), "no evidence rows");
    assert.equal(evidenceSummary([{ id: 1, ts: "t", run_id: "r", tool: "shell", args_hash: "h", ok: true }]), "1 ok");
    assert.equal(evidenceSummary([
      { id: 1, ts: "t", run_id: "r", tool: "shell", args_hash: "h", ok: true },
      { id: 2, ts: "t", run_id: "r", tool: "read", args_hash: "h", ok: false },
    ]), "1 ok, 1 failed");
  });

  it("verifierVoice renders the cheap verifier or a dash", () => {
    assert.equal(verifierVoice({ id: 1, ts: "t", run_id: "r", tool: "x", args_hash: "h", ok: true, verifier: "file:exists" }), "file:exists");
    assert.equal(verifierVoice({ id: 1, ts: "t", run_id: "r", tool: "x", args_hash: "h", ok: true }), "-");
  });

  it("shortWhen cuts the local time-of-day", () => {
    assert.equal(shortWhen("2026-10-28T12:34:00Z"), "12:34");
    assert.equal(shortWhen(""), "-");
  });
});

describe("the continuous registry + wiring", () => {
  it("the dockable exists: singleton, pinned right, unclosable, Title Case", () => {
    assert.ok((DOCK_COMPONENTS as readonly string[]).includes("continuous"), "the component id exists");
    const d = dockableOf("continuous");
    assert.equal(d.title, "Continuous");
    assert.equal(d.policy, "singleton");
    assert.equal(d.area, "right");
    assert.equal(d.closable, false);
    assert.ok(DOCKABLES.some((x) => x.id === "continuous"), "the registry carries it");
  });

  it("the app outlet exists and mounts the panel", () => {
    assert.match(app, /#pane-continuous/, "the outlet template exists");
    assert.match(app, /<ContinuousPanel/, "the panel mounts in the outlet");
    assert.match(app, /openContinuousTab/, "the opener exists");
    assert.match(app, /openSingleton\(pane\.id, "continuous"\)/, "the opener opens the singleton");
  });

  it("the roster panel rides the REST surface exactly (ruling 5 + 11)", () => {
    assert.match(panel, /fetch\("\.?"?\/continuous\/roster"|fetch\("\/continuous\/roster"\)/, "GET roster");
    assert.match(panel, /method: "POST"/, "the POSTs exist");
    assert.match(panel, /\/continuous\/\$\{encodeURIComponent\(/, "per-agent paths encode the name");
    assert.match(panel, /\/evidence`/, "the evidence read rides the detail strip");
    assert.match(panel, /\/sleep`/, "the sleep button rides POST sleep");
  });

  it("the create flow is the three-field form (name, directive, schedule)", () => {
    assert.match(panel, /createName/, "name field");
    assert.match(panel, /createDirective/, "directive field");
    assert.match(panel, /createSchedule/, "schedule field");
    assert.match(panel, /fetch\("\/continuous",\s*\{[\s\S]*?method: "POST"/, "POST /continuous");
  });
});