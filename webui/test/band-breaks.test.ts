// omega webui: the three band-contract breaks (card t_f1a034dc). The
// t_acad02ec rework converged kanban + the selects but missed the three
// outliers: the settings band was deformed (the filter seated IN it -
// 26px band, 0px lead), the tools band stayed bare (no refresh tool,
// the skill-refresh rollout t_ec57d5b8 missed the sibling), and the
// insights band showed the SAME window value TWICE (plain-text period
// label beside the BandSelect chip). The node suite has no DOM, so the
// convergence is pinned structurally on the source - the same
// contract style tabband.test.ts / sidebar-order.test.ts use:
//   - settings: card t_529628d5 SUPERSEDED the own-row shape - the
//     filter seats IN the band again, the LEGAL way (the shared
//     compact .band-search seat that holds the 22px row); the old
//     filter row is gone;
//   - tools: the Skills refresh band-tool pattern in the band;
//   - insights: ONE window control, no duplicate period label.
// Run: node --test test/band-breaks.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}/${p}`, "utf8");

const settings = src("app/components/settings/SettingsTab.vue");
const tools = src("../harness/tools/ui/ToolsList.vue");
const insights = src("app/components/insights/InsightsTab.vue");
const skills = src("../harness/skills/ui/SkillsList.vue");
const tabband = src("app/components/ui/TabBand.vue");
const maincss = src("app/assets/css/main.css");

describe("settings: the filter seats IN the band (card t_529628d5)", () => {
  it("the band carries the filter - the shared compact seat, title + input on one row", () => {
    const bandStart = settings.indexOf('<TabBand title="Settings">');
    const bandEnd = settings.indexOf("</TabBand>", bandStart);
    assert.ok(bandStart >= 0, "the band renders");
    assert.ok(bandEnd > bandStart, "the band closes");
    const chunk = settings.slice(bandStart, bandEnd);
    assert.match(chunk, /class="band-search"/, "the filter rides the shared seat");
    assert.match(chunk, /<SearchInput/, "the filter's input seats inside the band");
    assert.match(chunk, /placeholder="filter settings"/, "same voice, same filter");
  });

  it("the old filter row below the band is GONE - source and shared sheet", () => {
    assert.ok(!settings.includes('class="settings-filter"'), "no settings-filter row in the SFC");
    assert.ok(!maincss.includes(".settings-filter"), "no settings-filter rule in main.css");
  });

  it("no flush seating remains - the band rides the shared 22/14 contract", () => {
    assert.doesNotMatch(
      settings,
      /:lead="false"/,
      "lead=false (the lost 14px top inset) is gone",
    );
    assert.doesNotMatch(
      settings,
      /\.settings :deep\(\.settings-search\)|\.settings :deep\(\.tab-band-actions\)/,
      "the band seating overrides are gone",
    );
    assert.doesNotMatch(
      settings,
      /class="settings-search"/,
      "the in-band filter class is gone",
    );
    // the component's own measurements are the contract
    assert.match(tabband, /min-height:\s*22px/);
    assert.match(tabband, /margin-top:\s*14px/);
  });

  it("the shared SearchInput still carries the behavior via v-model", () => {
    assert.match(settings, /<SearchInput[^>]*v-model="query"/);
  });
});

describe("tools: the band carries the refresh tool (card t_f1a034dc)", () => {
  it("the band holds the refresh button inside it", () => {
    const bandStart = tools.indexOf('<TabBand title="Tools">');
    const bandEnd = tools.indexOf("</TabBand>", bandStart);
    assert.ok(bandStart >= 0, "the Tools band renders");
    assert.ok(bandEnd > bandStart, "the Tools band closes");
    const bandChunk = tools.slice(bandStart, bandEnd);
    assert.ok(
      bandChunk.includes('aria-label="refresh tools"'),
      "the refresh tool lives INSIDE the band",
    );
    assert.match(
      bandChunk,
      /class="band-tool"/,
      "it uses the one action-button look",
    );
    assert.match(bandChunk, /name="refresh"/, "the Icon vocabulary glyph");
  });

  it("the refresh re-runs the fetch chain without a remount", () => {
    const refreshAt = tools.indexOf("async function refresh(): Promise<void>");
    assert.ok(refreshAt > 0, "a refresh handler exists");
    // the in-flight lock: a second click is dropped (the Skills pattern)
    assert.match(tools, /if \(refreshing\.value\) return/);
    assert.match(tools, /await load\(\)/, "the refresh re-runs load()");
    assert.match(tools, /:disabled="refreshing"/, "the button shows the lock");
  });

  it("the Skills sibling stays the reference shape - Tools is its twin", () => {
    const skBandAt = skills.indexOf('<TabBand title="Skills">');
    const skBandEnd = skills.indexOf("</TabBand>", skBandAt);
    const skChunk = skills.slice(skBandAt, skBandEnd);
    for (const marker of ['aria-label="refresh skills"', 'class="band-tool"', "name=\"refresh\""]) {
      assert.ok(skChunk.includes(marker));
      assert.ok(tools.includes(marker.replace("skills", "tools")), `tools carries ${marker}`);
    }
  });
});

describe("insights: ONE window control (card t_f1a034dc)", () => {
  it("the duplicate plain-text period label is gone", () => {
    const bandStart = insights.indexOf("<TabBand");
    const bandEnd = insights.indexOf("</TabBand>", bandStart);
    const bandChunk = insights.slice(bandStart, bandEnd);
    assert.doesNotMatch(bandChunk, /<span\b/, "no label span renders in the band");
    assert.doesNotMatch(insights, /class="insights-period"/, "the label classes are gone");
    assert.doesNotMatch(insights, /\.insights-period\s*\{/, "its CSS rule is gone");
  });

  it("the window value still renders exactly once - the BandSelect chip", () => {
    const bandStart = insights.indexOf("<TabBand");
    const bandEnd = insights.indexOf("</TabBand>", bandStart);
    const bandChunk = insights.slice(bandStart, bandEnd);
    assert.equal((bandChunk.match(/<BandSelect/g) ?? []).length, 1, "exactly one window control");
    assert.ok(bandChunk.includes('label="window in days, 0 = all time"'));
    assert.ok(bandChunk.includes('aria-label="recompute insights"'), "the refresh icon stays");
    // the period still travels: the band's hover tooltip + the body
    assert.match(insights, /title-attr=/);
  });
});