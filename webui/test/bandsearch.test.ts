// omega webui: the in-band search across the registry tabs (card
// t_529628d5, Endo's band-search ruling 2026-11-01): EVERY registry tab
// - cron, skills, tools, mcp, settings - carries a compact search input
// in the band's band-tool slot (the sessions sidebar's t_7ee0a673 seat,
// promoted into TabBand's shared vocabulary), cron's band carries a
// refresh tool, and the settings filter MOVED inside the band (its old
// own-row seat died with the ruling). The node suite has no DOM, so:
//   - the shared SEAT is pinned on TabBand's source (the geometry laws);
//   - the per-tab SEATS are structurally pinned: the .band-search
//     wrapper + the shared SearchInput live INSIDE the band's chunk,
//     BEFORE the scrollport;
//   - the per-tab FILTERS are unit-pinned at the pure-module level
//     (the toggle.ts / toolRows.ts recipe - no SFC compile).
// Run: node --test test/bandsearch.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

// the shared component + the five SFC seats (paths root-relative; the
// modules' paths leave webui/ - the repo-relative convention)
const TABS: Array<{ file: string; placeholder: string }> = [
  { file: "../harness/cron/ui/CronJobs.vue", placeholder: "Search cron jobs" },
  { file: "../harness/skills/ui/SkillsList.vue", placeholder: "Search skills" },
  { file: "../harness/tools/ui/ToolsList.vue", placeholder: "Search tools" },
  { file: "../harness/mcp/ui/McpServers.vue", placeholder: "Search MCP servers" },
  { file: "app/components/settings/SettingsTab.vue", placeholder: "filter settings" },
];

// the shared seat's one definition lives in TabBand's scoped stylesheet
const tabband = read("app/components/ui/TabBand.vue");

// the band chunk of one SFC: from <TabBand to its closed tag
function bandChunk(src: string): string {
  const start = src.indexOf("<TabBand");
  assert.ok(start >= 0, "the band renders");
  const end = src.indexOf("</TabBand>", start);
  assert.ok(end > start, "the band closes");
  return src.slice(start, end);
}

describe("TabBand: the shared compact search seat (card t_529628d5)", () => {
  it("the band-tool vocabulary owns ONE seat definition", () => {
    assert.match(tabband, /\.tab-band-actions :deep\(\.band-search\)/, "the seat lives in TabBand");
    const seat = tabband.match(/\.tab-band-actions :deep\(\.band-search\)\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(seat, /width:\s*128px/, "the comfortable fixed width (the sessions seat)");
    assert.match(seat, /min-width:\s*96px/, "the floor");
  });

  it("the seat's input is compact - the shared 5px vertical padding would grow the band", () => {
    const compact = tabband.match(/[^\n]*\.band-search \.search-input input\)\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(compact, /padding:\s*2px\s+20px\s+2px\s+6px/, "the 2px vertical rhythm holds the 22px law");
  });

  it("the seat is the INPUT ITSELF in-band - no icon-expander", () => {
    // the wrap (the sanctioned narrow adaptation) only fires when the
    // band carries a search seat; wide panes keep the one-row geometry
    assert.match(tabband, /\.tab-band:has\(\.band-search\)/, "the wrap is seat-gated");
    const wrap = tabband.match(/\.tab-band:has\(\.band-search\)\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(wrap, /row-gap:\s*4px/, "the stacked rows keep breathing room");
    // the 22px contract is untouched
    assert.match(tabband, /min-height:\s*22px/);
  });

  it("the sidebar consumes the shared seat - no second definition", () => {
    const sidebar = read("../harness/store/ui/AppSidebar.vue");
    const chunk = bandChunk(sidebar);
    assert.match(chunk, /class="band-search"/, "the sessions seat is the same wrapper");
    // the sidebar keeps only its STACK rules; the seat geometry is gone
    assert.doesNotMatch(sidebar, /^\.band-search \{/m, "the old per-tab seat copy is deleted");
  });
});

describe("every registry tab seats its search in the band", () => {
  for (const t of TABS) {
    it(`${t.file}: the compact seat rides the band, before the scrollport`, () => {
      const src = read(t.file);
      const chunk = bandChunk(src);
      assert.match(chunk, /class="band-search"/, `.band-search inside the band chunk (${t.file})`);
      assert.match(chunk, /<SearchInput/, "the shared SearchInput inside the band chunk");
      assert.match(chunk, /v-model="query"/, "the behavior stays the consumer's");
      assert.match(chunk, new RegExp(`placeholder="${t.placeholder}"`), "the tab's own search voice");
      // the seat is NOT an own row below the band (the move law): the
      // band chunk closes BEFORE the scrollport
      const scrollClass = t.file.endsWith("CronJobs.vue")
        ? "cron-body"
        : t.file.endsWith("SkillsList.vue")
          ? "skills-body"
          : t.file.endsWith("ToolsList.vue")
            ? "tools-body"
            : t.file.endsWith("McpServers.vue")
              ? "mcp-body"
              : "settings-body";
      const closeAt = src.indexOf("</TabBand>");
      const searchClassAt = src.indexOf(`class="${scrollClass}"`);
      assert.ok(closeAt < searchClassAt, "the band (with the seat) closes BEFORE the scrollport");
      // the filter's own row is dead in every tab that had one
      assert.doesNotMatch(src, /class="settings-filter"/, "no filter row below the band");
    });
  }
});

// --- the pure filters (the node suite runs them without compiling SFCs)

import { filterCronJobs } from "../../harness/cron/ui/search.ts";
import { filterSearchableRows } from "../../harness/skills/ui/search.ts";
import { filterToolRows } from "../../harness/tools/ui/search.ts";
import { filterMcpServers } from "../../harness/mcp/ui/search.ts";

describe("cron: the band search filters by name / schedule", () => {
  const jobs = [
    { name: "wake", schedule: "0 5 * * *", enabled: true, next: "" },
    { name: "backup", schedule: "30 3 * * 0", enabled: false, next: "" },
  ];

  it("an empty (or whitespace) query returns the FULL list unchanged", () => {
    assert.equal(filterCronJobs(jobs, "").length, 2);
    assert.equal(filterCronJobs(jobs, "   ").length, 2);
  });

  it("name and schedule both match, case-insensitively", () => {
    assert.deepEqual(filterCronJobs(jobs, "WAKE").map((j) => j.name), ["wake"]);
    assert.deepEqual(filterCronJobs(jobs, "3").map((j) => j.name), ["backup"]);
    assert.deepEqual(filterCronJobs(jobs, "0 5").map((j) => j.name), ["wake"]);
    assert.deepEqual(filterCronJobs(jobs, "zzz"), []);
  });

  it("the ROW ANATOMY law: survivors are the same objects, unfound rows are HIDDEN", () => {
    const out = filterCronJobs(jobs, "wake");
    assert.equal(out[0], jobs[0], "the identical row object, not a re-shape");
    assert.equal(out.length, 1, "the unfound row is hidden, not altered");
  });
});

describe("skills: the band search filters by name / description", () => {
  const rows = [
    { name: "omega-dev", description: "the omega repo", disabled: false },
    { name: "ponytail", description: "lazy solutions only", disabled: true },
  ];

  it("an empty query returns the full list; name AND description match", () => {
    assert.equal(filterSearchableRows(rows, "").length, 2);
    assert.deepEqual(filterSearchableRows(rows, "pony").map((r) => r.name), ["ponytail"]);
    assert.deepEqual(
      filterSearchableRows(rows, "LAZY").map((r) => r.name),
      ["ponytail"],
      "the description is searchable too",
    );
    assert.deepEqual(filterSearchableRows(rows, "zzz"), []);
  });

  it("hide-rows-only: the disabled fold flag survives inside the survivors", () => {
    const out = filterSearchableRows(rows, "omega");
    assert.equal((out[0] as { disabled: boolean }).disabled, false);
    assert.equal(out.length, 1);
  });
});

describe("tools: the band search filters name / description on the GROUPED rows", () => {
  const rows = [
    { name: "files", description: "the files domain", detail: ["files.read - read a file"], disabled: false },
    { name: "shell", description: "run a shell command", detail: ["shell.run - run the command"], disabled: false },
  ];

  it("an empty query returns the full list; name / description match", () => {
    assert.equal(filterToolRows(rows, "").length, 2);
    assert.deepEqual(filterToolRows(rows, "SHELL").map((r) => r.name), ["shell"]);
    assert.deepEqual(filterToolRows(rows, "RUN A").map((r) => r.name), ["shell"]);
    assert.deepEqual(filterToolRows(rows, "zzz"), []);
  });

  it("the row lands WHOLE: a surviving group keeps its detail shape (no re-grouping)", () => {
    const out = filterToolRows(rows, "files");
    assert.deepEqual(out[0], rows[0], "the identical row object - detail intact");
  });
});

describe("mcp: the band search filters name / transport, tools included", () => {
  const servers = [
    { name: "files", transport: "stdio", tools: ["mcp__files__read"] },
    { name: "browser", transport: "http", tools: [] },
  ];

  it("an empty query returns the full list", () => {
    assert.equal(filterMcpServers(servers, "").length, 2);
    assert.equal(filterMcpServers(servers, "  ").length, 2);
  });

  it("server name / transport match", () => {
    assert.deepEqual(filterMcpServers(servers, "browser").map((s) => s.name), ["browser"]);
    assert.deepEqual(filterMcpServers(servers, "STDIO").map((s) => s.name), ["files"]);
    assert.deepEqual(filterMcpServers(servers, "zzz"), []);
  });

  it("a TOOL name shown in the expansion keeps its server row visible", () => {
    assert.deepEqual(filterMcpServers(servers, "read").map((s) => s.name), ["files"]);
    assert.deepEqual(filterMcpServers(servers, "fetch"), []);
  });

  it("hide-rows-only: survivors are the same wire objects", () => {
    const out = filterMcpServers(servers, "browser");
    assert.equal(out[0], servers[1]);
  });
});