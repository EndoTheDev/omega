// omega webui: the three band-contract breaks (card t_f1a034dc). The
// t_acad02ec rework converged kanban + the selects but missed the three
// outliers: the settings band was deformed (the filter seated IN it -
// 26px band, 0px lead), the tools band stayed bare (no refresh tool,
// the skill-refresh rollout t_ec57d5b8 missed the sibling), and the
// insights band showed the SAME window value TWICE (plain-text period
// label beside the BandSelect chip). The node suite has no DOM, so the
// convergence is pinned structurally on the source - the same
// contract style tabband.test.ts / sidebar-order.test.ts use:
//   - settings: card t_529628d5 superseded the own-row shape with an
//     in-band seat, then card t_6bebff18 (Endo ruling 2026-11-10,
//     option B) moved the filter OUT of the band to a pane-top
//     full-width bar (the VSCode shape), and card t_1cfb080a (Endo,
//     2026-11-11) removed the band itself - the dock tab already says
//     "Settings", so the pane renders NO band and the bar heads it
//     (this file pins that ABSENCE; bandsearch.test.ts owns the
//     band-search roster, which now scopes to the four module tabs);
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
const insights = src("../harness/store/ui/InsightsTab.vue");
const skills = src("../harness/skills/ui/SkillsList.vue");
const maincss = src("app/assets/css/main.css");

describe("settings: NO tab band - the dock tab already says Settings (card t_1cfb080a)", () => {
  // card t_308e6d9b put the universal band in this pane on purpose; card
  // t_1cfb080a (Endo, 2026-11-11) SUPERSEDES it. The dock tab carries the
  // name, so the in-pane band only repeated it while spending a 22px row
  // plus the band's 14px lead. The pin below is the ABSENCE: a later edit
  // must not quietly put it back (band-breaks is where the settings band
  // shape has always been pinned).
  it("the pane composes no tab band at all", () => {
    assert.ok(!/<TabBand\b/.test(settings), "no <TabBand> element in the settings pane");
    assert.doesNotMatch(settings, /class="tab-band/, "no band markup");
    assert.doesNotMatch(settings, /from\s+["'][^"']*TabBand["']/, "no TabBand import");
  });

  it("the filter bar is the pane's FIRST row - nothing renders above it", () => {
    const rootAt = settings.indexOf('class="settings aside pane inspector-panel"');
    assert.ok(rootAt >= 0, "the pane root renders");
    const rootTagEnd = settings.indexOf(">", rootAt);
    const barAt = settings.indexOf('class="settings-filterbar"');
    const bodyAt = settings.indexOf('class="settings-body"');
    assert.ok(rootTagEnd > rootAt, "the pane root's open tag closes");
    assert.ok(barAt > rootTagEnd, "the bar mounts INSIDE the pane root");
    // slice to the bar's OWN open tag, so the assertion is about markup
    // between the root and the bar, never about the bar's attributes
    const barTagAt = settings.lastIndexOf("<", barAt);
    const between = settings.slice(rootTagEnd, barTagAt);
    assert.doesNotMatch(
      between,
      /<(?!\/?!?!--)[a-zA-Z]/,
      "nothing renders between the root and the bar (the band's old seat)",
    );
    assert.ok(barAt < bodyAt, "the bar heads the body, never inside the scrollport");
  });

  it("the bar carries the pane's own top inset - the band's 14px lead", () => {
    // the 22px title row is NOT recovered (the name is gone); the lead the
    // band's margin-top supplied is, on the row that now heads the pane.
    // Sides stay 10px = the body's inset, so the horizontal geometry is
    // identical to the band era.
    const block = settings.match(/\.settings-filterbar\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(block, /padding:\s*14px\s+10px/, "14px top lead, 10px sides unchanged");
    assert.doesNotMatch(block, /padding:\s*6px\s+10px/, "the flush 6px seat is gone");
  });

  it("the old filter rows are GONE - source and shared sheet", () => {
    assert.ok(!settings.includes('class="settings-filter"'), "no settings-filter row in the SFC");
    assert.ok(!maincss.includes(".settings-filter"), "no settings-filter rule in main.css");
    assert.ok(!maincss.includes(".settings-search"), "no settings-search rule in main.css");
  });

  it("no flush seating remains - no band overrides survive either", () => {
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
  });

  it("the shared SearchInput still carries the behavior via v-model", () => {
    assert.match(settings, /<SearchInput[^>]*v-model="query"/);
    assert.equal((settings.match(/<SearchInput/g) ?? []).length, 1, "ONE input, moved - not a second");
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