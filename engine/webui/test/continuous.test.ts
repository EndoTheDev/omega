// omega webui: the continuous wake-state surface (card t_72ecca67,
// ruling 18; card t_c7222095 merged the roster into the Agents tab) -
// the pure view-model helpers (continuousRows.ts) unit-tested, and the
// MERGE source-checked: the dock vocabulary carries no "continuous"
// entry (deletability law), the AgentsPanel carries the roster/wake
// REST surface, and the merged row renders config + status. Render-
// free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
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
import {
  continuousIndex,
  mergedRowDetail,
} from "../app/components/agents/agentsRows.ts";
import { DOCK_COMPONENTS, DOCKABLES } from "../app/composables/dockables.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");
const agentsPanel = src("app/components/agents/AgentsPanel.vue");
const registryList = src("app/components/RegistryList.vue");
const app = src("app/app.vue");
const panelGone = !existsSync(`${root}app/components/agents/ContinuousPanel.vue`);

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

describe("the merge: one Agents tab (card t_c7222095, deletability law)", () => {
  it("the dock registry carries NO continuous entry - no alias, no hidden tab", () => {
    const ids: readonly string[] = DOCK_COMPONENTS;
    assert.ok(!ids.includes("continuous"), "the component id left DOCK_COMPONENTS");
    assert.ok(!DOCKABLES.some((x) => x.id === "continuous"), "the registry carries no dockable");
  });

  it("ContinuousPanel.vue is deleted and app.vue has zero continuous vocabulary", () => {
    assert.equal(panelGone, true, "the panel file is gone");
    assert.doesNotMatch(app, /ContinuousPanel/, "no mount, no import");
    assert.doesNotMatch(app, /#pane-continuous/, "the outlet dropped");
    assert.doesNotMatch(app, /openContinuousTab/, "the opener dropped");
    assert.doesNotMatch(app, /new-continuous/, "the sidebar emit wiring dropped");
    assert.doesNotMatch(src("app/components/sessions/AppSidebar.vue"), /new-continuous|"Continuous"/, "the picker entry dropped");
    assert.doesNotMatch(src("app/composables/icons.ts"), /continuous: CircleDot/, "the icon vocabulary entry dropped");
  });

  it("the merged row renders config + runtime status + preview in one row", () => {
    assert.match(agentsPanel, /agents-badge/, "the inline status badge renders");
    assert.match(agentsPanel, /rowStatus/, "the badge speaks the roster status voice");
    assert.match(agentsPanel, /preview:\s*previewLine\(/, "the row carries the last-report preview");
    assert.match(registryList, /registry-preview/, "RegistryList renders the under-row preview");
    assert.match(registryList, /<span[\s\S]{0,40}v-if="r\.preview"/, "the preview renders only when set");
  });

  it("the merged AgentsPanel rides the continuous REST surface exactly (ruling 5 + 11)", () => {
    assert.match(agentsPanel, /fetch\("\/continuous\/roster"\)/, "GET roster");
    assert.match(agentsPanel, /method: "POST"/, "the POSTs exist");
    assert.match(agentsPanel, /\/continuous\/\$\{encodeURIComponent\(/, "per-agent paths encode the name");
    assert.match(agentsPanel, /\/evidence`/, "the evidence read rides the wake strip");
    assert.match(agentsPanel, /\/sleep`/, "the sleep button rides POST sleep");
  });

  it("the create flow is the three-field form (name, directive, schedule) on the Agents tab", () => {
    assert.match(agentsPanel, /contDirective/, "directive field");
    assert.match(agentsPanel, /contSchedule/, "schedule field");
    assert.match(agentsPanel, /fetch\("\/continuous",\s*\{[\s\S]*?method: "POST"/, "POST /continuous");
  });

  it("a rostered agent's wake strip keeps the wake/sleep + unread behavior", () => {
    assert.match(agentsPanel, /wake sent - the report lands in the canonical chat/, "ruling 8 wake voice");
    assert.match(agentsPanel, /sleep sent/, "sleep feedback");
    assert.match(agentsPanel, /agents-badge\.unread|\.unread/, "the unread state is visible");
    // the chat fetch in the merge marks the canonical session read (the
    // roster's unread flag clears on the reload) - the expand fetches it
    assert.match(agentsPanel, /\/chat`\)/, "the expand fetches the canonical chat (mark-read parity)");
  });

  it("a host without the module keeps the tab honest: no badge, no create form", () => {
    assert.match(agentsPanel, /continuousAvailable/, "the availability gate exists");
    assert.match(agentsPanel, /v-if="continuousAvailable"[\s\S]{0,160}class="agents-new band-tool"/, "the + hides without the module");
    assert.match(agentsPanel, /return c \? rowStatus\(c\) : ""/, "a config-only agent renders no badge");
  });
});

describe("the merge, pure: joined row model (agentsRows.ts)", () => {
  it("continuousIndex joins the roster by name", () => {
    const idx = continuousIndex([row(), row({ name: "coder", unread: true })]);
    assert.equal(idx["scout"]?.name, "scout");
    assert.equal(idx["coder"]?.unread, true);
  });

  it("mergedRowDetail appends the wake line only for rostered agents", () => {
    const a = { name: "scout", skill_count: 0, memory_size: 0, dir: "/x" } as any;
    const bare = mergedRowDetail(a, null);
    assert.ok(bare.every((l) => !/wake: /.test(l)), "no phantom wake for a config-only agent");
    assert.ok(bare.length >= 7, "the config summary stands alone");
    const merged = mergedRowDetail(a, row({ next_wake: "2026-10-28T12:34:00Z" }));
    assert.equal(merged[merged.length - 1], "wake: next wake 12:34");
    assert.equal(merged.length, bare.length + 1);
  });
});