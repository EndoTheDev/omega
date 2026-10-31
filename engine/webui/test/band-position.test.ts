// omega webui: the band POSITION contract (card t_ffab9823). t_f1a034dc
// fixed what the band CONTAINS; this pins where it MOUNTS. Live
// measurement 2026-10-31 (both axes, Files as the reference):
//   vertical   title/band-bottom/next-row: 81/99/99 everywhere - Tools
//              +6px (a stray duplicate `.tools-list.narrow` root rule
//              re-adding 6px 8px padding), Settings +10px AND a 12px
//              dead gap (the root's 10px padding + flex gap);
//   horizontal band-left: Files = x, Tools +8 (same stray rule), Settings
//              +10 (the root's 10px padding), Insights +14 (the root's
//              `padding: 0 14px`).
// The contract: the root the band mounts in carries NO padding - the
// band anchors the pane's top-left edge on both axes, and the band's
// first row starts at the band's own bottom edge (the row carries its
// own seat, the sessions new-row pattern). The node suite has no DOM,
// so this is pinned structurally on the source: the root rule blocks
// and the row-seat rules.
// Run: node --test test/band-position.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}/${p}`, "utf8");

const maincss = src("app/assets/css/main.css");
const tools = src("../../modules/tools/ui/ToolsList.vue");
const insights = src("app/components/insights/InsightsTab.vue");

// ...a rule block: selector ... { ... } - the FIRST `}` closing it.
function ruleBlock(css: string, selector: string): string {
  const start = css.indexOf(selector);
  assert.ok(start >= 0, `the ${selector.trim()} rule exists`);
  const open = css.indexOf("{", start);
  const close = css.indexOf("}", open);
  return css.slice(open + 1, close);
}

describe("settings: the band mounts flush (card t_ffab9823)", () => {
  it("the .settings root carries no padding and no flex gap", () => {
    const block = ruleBlock(maincss, ".settings {");
    assert.doesNotMatch(block, /padding:\s*[1-9]/, "no root padding");
    assert.doesNotMatch(block, /gap:\s*[1-9]/, "no root gap");
  });

  it("no narrow root padding survives", () => {
    assert.doesNotMatch(
      maincss,
      /\.settings\.narrow\s*\{[^}]*padding/,
      "the 10px narrow root padding is gone",
    );
    assert.doesNotMatch(
      maincss,
      /\.settings\.narrow\s*\{[^}]*gap/,
      "the 12px narrow root gap is gone",
    );
  });

  it("the filter row carries its own seat (the sessions pattern)", () => {
    const block = ruleBlock(maincss, ".settings-filter {");
    assert.match(block, /padding:\s*8px\s+10px/);
  });

  it("the body's inset rides its own padding, not the root", () => {
    const block = ruleBlock(maincss, ".settings-body {");
    assert.match(block, /padding:\s*0\s+10px/);
  });
});

describe("tools: no root padding above the band (card t_ffab9823)", () => {
  it("the root rules set no padding in wide OR narrow", () => {
    const rootBlock = ruleBlock(tools, ".tools-list {");
    assert.doesNotMatch(rootBlock, /padding/, "the wide root has no padding");
    const narrowRoots = tools.match(/\.tools-list\.narrow\s*\{[^}]*\}/g) ?? [];
    assert.equal(
      narrowRoots.length,
      0,
      "no .tools-list.narrow ROOT rules survive (the stray +6/+8px duplicate is deleted; narrow padds the BODY only)",
    );
  });

  it("the narrow body keeps its own inset", () => {
    const block = ruleBlock(tools, ".tools-list.narrow .tools-body {");
    assert.match(block, /padding:\s*4px\s+8px\s+10px/);
  });
});

describe("insights: the band's left edge matches Files (card t_ffab9823)", () => {
  it("the .insights root carries no padding", () => {
    const block = ruleBlock(insights, ".insights {");
    assert.doesNotMatch(block, /padding/, "no root padding");
  });

  it("the scroll wrapper carries the seat (sides + bottom)", () => {
    const block = ruleBlock(insights, ".insights-scroll {");
    assert.match(block, /padding:\s*4px\s+14px\s+10px/);
  });
});

describe("the universal contract: band FIRST in every root", () => {
  it("the band is the root's first child in all three components", () => {
    for (const [name, text, rootAt, bandAt] of [
      ["settings", src("app/components/settings/SettingsTab.vue"), 'class="settings aside pane inspector-panel"', /<TabBand[^>]*title="Settings"/],
      ["tools", tools, 'class="tools-list"', /<TabBand[^>]*title="Tools">/],
      ["insights", insights, 'class="pane insights"', /<TabBand[^>]*title="Insights"/],
    ] as const) {
      const rootStart = text.indexOf(rootAt);
      assert.ok(rootStart >= 0, `${name}: the root element found`);
      const bandStart = text.search(bandAt);
      assert.ok(bandStart > rootStart, `${name}: the band mounts inside its root`);
      const between = text.slice(rootStart, bandStart);
      assert.doesNotMatch(
        between,
        /<(?!\/?!?!--)[a-zA-Z]/,
        `${name}: nothing renders between the root and the band`,
      );
    }
  });
});
