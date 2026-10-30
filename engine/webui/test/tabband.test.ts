// omega webui: the UNIVERSAL PANEL TOP BAR (card t_308e6d9b, Endo's
// design directive 2026-10-30). Every tab carries the Files-band top
// bar (the shared TabBand component), and the band never scrolls: it
// renders OUTSIDE the tab's scrollport (the t_c252b9c6 lesson - no CSS
// sticky inside a scroll body). The node suite has no DOM, so this is
// pinned on the source, per tab: the <TabBand> usage, its position
// BEFORE the scroll container, and (where the tab owns actions) the
// tab's real buttons living inside the band.
// Run: node --test test/tabband.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

// one entry per tab: file (engine/webui-root relative; ../.. leaves to
// the repo root for module/engine-ring UIs), the band title, the scroll
// container class the band must sit OUTSIDE of, and the tab's own
// action labels that must live inside the band. files is the band's
// birthplace: its band is conditional ONLY on the cwd-fallback ruling.
const TABS: Array<{
  file: string;
  title: string | null;
  scrollClass: string;
  actions: string[];
}> = [
  { file: "app/components/files/FilesTab.vue", title: null, scrollClass: "files-scroll", actions: ["add file", "refresh files"] },
  { file: "../../modules/cron/ui/CronJobs.vue", title: "Cron", scrollClass: "cron-body", actions: [] },
  { file: "../../modules/skills/ui/SkillsList.vue", title: "Skills", scrollClass: "skills-body", actions: ["refresh skills"] },
  { file: "../../modules/tools/ui/ToolsList.vue", title: "Tools", scrollClass: "tools-body", actions: [] },
  { file: "../../engine/http_channel/ui/LogsList.vue", title: "Logs", scrollClass: "logs-body", actions: [] },
  { file: "../../modules/mcp/ui/McpServers.vue", title: "MCP", scrollClass: "mcp-body", actions: ["refresh mcp servers"] },
  { file: "../../modules/kanban/ui/Board.vue", title: "Kanban", scrollClass: "board-scroll", actions: ["board-remove-btn"] },
  { file: "app/components/shell/ExtensionsTab.vue", title: "Modules", scrollClass: "inspector-extensions", actions: [] },
  { file: "app/components/agents/AgentsPanel.vue", title: "Agents", scrollClass: "agents-body", actions: ["scaffold a new agent directory"] },
  { file: "app/components/insights/InsightsTab.vue", title: "Insights", scrollClass: "insights-scroll", actions: ["window in days, 0 = all time"] },
  { file: "app/components/settings/SettingsTab.vue", title: "Settings", scrollClass: "settings-body", actions: ["filter settings"] },
  { file: "app/components/notifications/NotificationsPanel.vue", title: "Notifications", scrollClass: "notif-list", actions: [] },
  { file: "app/components/sessions/AppSidebar.vue", title: "Sessions", scrollClass: "session-list", actions: ["New conversation"] },
];

describe("the universal band (card t_308e6d9b)", () => {
  const band = read("app/components/ui/TabBand.vue");

  it("one shared component encodes the contract", () => {
    assert.match(band, /class="tab-band"/);
    assert.match(band, /flex:\s*none/, "the band never flexes into the scrollport");
    assert.doesNotMatch(band, /position:\s*sticky/, "non-scrolling is STRUCTURE, not CSS sticky");
    assert.match(band, /role="toolbar"/);
    assert.match(band, /\.band-tool/, "one action-button look for every tab");
  });

  for (const t of TABS) {
    it(`${t.file} carries the band OUTSIDE its scrollport`, () => {
      const src = read(t.file);
      assert.match(src, /<TabBand/, "the tab composes the shared band");
      if (t.title) {
        assert.match(src, new RegExp(`title="${t.title}"`), "the band is titled for what the tab is");
      }
      const bandAt = src.indexOf("<TabBand");
      const scrollAt = src.indexOf(`class="${t.scrollClass}"`);
      // the established cwd-fallback ruling is the ONLY conditional band
      if (!src.includes('v-if="headerName"')) {
        const pre = src.slice(Math.max(0, bandAt - 60), bandAt);
        assert.doesNotMatch(pre, /v-if=/, "the band is ALWAYS present");
      }
      assert.ok(bandAt < scrollAt, `the band renders BEFORE "${t.scrollClass}" (never inside the scrollport)`);
      // no floating tab-head rows left in the bodies
      assert.doesNotMatch(
        src,
        /class="(notif-head|mcp-header|skills-header|tools-head|agents-head\b|insights-head|sessions-head|board-head)"/,
        "the old per-tab head rows are gone",
      );
    });
  }

  it("every tab's own actions live INSIDE the band", () => {
    for (const t of TABS) {
      const src = read(t.file);
      for (const action of t.actions) {
        const actionAt = src.indexOf(`"${action}"`);
        assert.ok(actionAt >= 0, `${t.file} still owns its "${action}" action`);
        const bandAt = src.indexOf("<TabBand");
        const bandEndAt = src.indexOf("</TabBand>");
        assert.ok(
          bandAt >= 0 && bandEndAt > 0 && actionAt > bandAt && actionAt < bandEndAt,
          `${t.file}'s "${action}" lives in the band`,
        );
      }
    }
  });
});