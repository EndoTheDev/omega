// omega webui: unit tests for the divider law (card t_bc321318, Endo
// ruling 2026-10-25: structural dividers run THROUGH; amended t_e539ba29,
// Endo ruling 2026-10-28: the COLUMN divider's resting line died - it
// stacked with the chat card's bottom edge and the tab strip's border-top
// into three hairlines that read as "lines going through the divider",
// with the Logs chip sitting on the topmost seam). The render-free
// suite can't measure boxes, so it pins the sheet instead - the family
// maxheight.test.ts established: parse main.css + bottompanel.css and
// make the rules carry the law. Pinned here:
//   1. the resting VERTICAL (row-split) divider paints a 1px centered
//      structural line, full cross-extent
//   2. the resting HORIZONTAL (column-split) divider paints NOTHING -
//      the pane header below owns the seam (t_e539ba29)
//   3. no border-radius on the handle (the hover band runs edge-to-edge)
//   4. hover/snap keep their full-band backgrounds (they ARE the line
//      while the pointer is on the handle)
//   5. the pane-area layout layer owns the frame's horizontal edges (the
//      ::before/::after strokes under the cards, desktop-gated)
//   6. the root row's vertical dividers ride the sidebars' overhang - the
//      hover band reaches the pane bottom row beside the band
//   7. the root column divider reaches 2px past its insets (T-junctions
//      land on the vertical lines' columns), fallback clean in full mode
//   8. the pane header's seam is a REAL border-bottom on .dock-tabbar -
//      opaque children never break it (supersedes the where-chrome-shows
//      inset-shadow read of t_f6c1b5fe)
// Run: node --test engine/webui/test/ (node >= 23)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");
const bottom = readFileSync(
  join(here, "../app/assets/css/bottompanel.css"),
  "utf8",
);

// Depth-aware rule extractor: every `selector { body }` block of a sheet
// as { at: parent at-rule or null, selectors, body }. At-rule wrappers
// stay separate from their inner rules; inner blocks never leak into
// later selector text.
type Rule = {
  at: string | null;
  selectors: string[];
  /** selector text as written (grouped form kept for find()) */
  raw: string;
  body: string;
};
function rulesOf(sheet: string): Rule[] {
  const rules: Rule[] = [];
  const open: {
    sel: string;
    start: number;
    isAt: boolean;
    at: string | null;
  }[] = [];
  let selStart = 0;
  for (let i = 0; i < sheet.length; i++) {
    const ch = sheet[i]!;
    if (ch === "{") {
      // comments ride inside selector text - strip them before any
      // at-rule detection or selector splitting
      const sel = sheet
        .slice(selStart, i)
        .replace(/\/\*[\s\S]*?\*\//g, "")
        .trim();
      const isAt = sel.startsWith("@");
      // a rule's at = the innermost enclosing at-rule (at-rules store
      // themselves; rules store the wrapper that contains them)
      const at = isAt
        ? sel
        : (open.findLast((e) => e.isAt)?.sel ?? null);
      open.push({ sel, start: i + 1, isAt, at });
      selStart = i + 1;
    } else if (ch === "}") {
      const block = open.pop()!;
      if (!block.isAt) {
        rules.push({
          at: block.at,
          selectors: block.sel.split(",").map((s) => s.trim()),
          raw: block.sel,
          body: sheet.slice(block.start, i),
        });
      }
      selStart = i + 1;
    }
  }
  return rules;
}

function find(
  sheet: Rule[],
  selector: string,
  at: string | null = null,
): Rule {
  const norm = (s: string) => s.replace(/\s+/g, " ").trim();
  const all = sheet.filter((r) => r.at === at);
  // exact selector text wins (an offset rule like `.pane-area::before {
  // top: 0 }` must not be shadowed by the grouped rule naming it too)
  const exact = all.filter((r) => norm(r.raw) === norm(selector));
  const hit = exact.length > 0 ? exact : all.filter((r) => r.selectors.includes(selector));
  assert.equal(hit.length, 1, `exactly one rule for ${selector} (at ${at})`);
  return hit[0]!;
}

const MAIN = rulesOf(css);
const BOTTOM = rulesOf(bottom);

// the two resting-line gradients, both orientations (1px centered column
// / row over the transparent gutters, through the whole cross-extent)
function gradientOf(orientation: "to right" | "to bottom"): string {
  return `linear-gradient(${orientation}, transparent 0 1.5px, var(--stroke) 1.5px 2.5px, transparent 2.5px)`.replace(/\s+/g, " ");
}

describe("divider law (t_bc321318 through-lines; t_e539ba29 column rests invisible)", () => {
  it("the vertical divider paints a full-extent 1px structural line", () => {
    const body = find(MAIN, ".dock-divider").body.replace(/\s+/g, " ");
    assert.ok(
      body.includes(gradientOf("to right")),
      "resting .dock-divider carries the centered vertical line gradient",
    );
  });

  it("the horizontal divider rests INVISIBLE - the pane header owns the seam", () => {
    const body = find(MAIN, ".dock-split.column > .dock-divider").body
      .replace(/\s+/g, " ");
    assert.ok(
      !body.includes("linear-gradient"),
      "no resting gradient left on the column divider",
    );
    // the guard rule kills the base (row-line) gradient ONLY at rest -
    // hover/drag/snap must outpaint it (t_e539ba29: the (0,3,0) column
    // rule out-ranked the (0,2,0) bands and the handle never highlighted)
    const guard = find(
      MAIN,
      ".dock-split.column > .dock-divider:not(:hover):not(.active):not(.snap)",
    ).body.replace(/\s+/g, " ");
    assert.match(guard, /background:\s*none/);
  });

  it("the handle has no border-radius - the line runs edge-to-edge", () => {
    assert.ok(
      !/\bborder-radius\b/.test(find(MAIN, ".dock-divider").body),
      "no rounding on the divider handle",
    );
  });

  it("hover/snap keep the full-band backgrounds that replace the line", () => {
    const hover = find(MAIN, ".dock-divider:hover");
    assert.match(hover.body, /background:\s*var\(--stroke\)/);
    const active = find(MAIN, ".dock-divider.active");
    assert.match(active.body, /background:\s*var\(--stroke\)/);
    const snap = find(MAIN, ".dock-divider.snap");
    assert.match(snap.body, /color-mix\(in srgb, var\(--accent\) 45%/);
  });

  it("the pane-area owns the frame's horizontal edges (top + bottom rows)", () => {
    const desktop = "@media (min-width: 769px)";
    // the shared edge rule + the two offset rules in the desktop block
    const edge = find(MAIN, ".pane-area::before, .pane-area::after", desktop);
    const body = edge.body.replace(/\s+/g, " ");
    assert.match(body, /background:\s*var\(--stroke\)/);
    assert.match(body, /height:\s*1px/);
    assert.match(body, /pointer-events:\s*none/);
    const top = find(MAIN, ".pane-area::before", desktop);
    assert.match(top.body.replace(/\s+/g, " "), /top:\s*0/);
    // the 3px padding row = the cards' bottom-border row
    const bottom = find(MAIN, ".pane-area::after", desktop);
    assert.match(bottom.body.replace(/\s+/g, " "), /bottom:\s*3px/);
  });

  it("root-row dividers ride the sidebars' overhang (full window height)", () => {
    const overhang = find(
      BOTTOM,
      ".dock-root > .dock-split.row > .dock-divider",
    ).body.replace(/\s+/g, " ");
    assert.match(
      overhang,
      /margin-bottom:\s*var\(--bottom-sidebars-overhang,\s*0px\)/,
      "same var the pinned panes ride",
    );
    const pane = find(
      BOTTOM,
      ".dock-pane.pinned:not(.edge-bottom)",
    ).body.replace(/\s+/g, " ");
    assert.match(
      pane,
      /var\(--bottom-sidebars-overhang,\s*0px\)/,
      "the pinned-pane rule this mirrors still exists",
    );
  });

  it("the root column divider's line lands on the verticals (T-junctions)", () => {
    const body = find(BOTTOM, ".dock-root > .dock-divider").body.replace(
      /\s+/g,
      " ",
    );
    assert.match(
      body,
      /margin-left:\s*max\(calc\(var\(--bottom-inset-left,\s*0px\)\s*-\s*2px\),\s*0px\)/,
    );
    assert.match(
      body,
      /margin-right:\s*max\(calc\(var\(--bottom-inset-right,\s*0px\)\s*-\s*2px\),\s*0px\)/,
    );
  });

  it("the pane header's seam is a real full-width border-bottom", () => {
    const bar = find(MAIN, ".dock-tabbar").body.replace(/\s+/g, " ");
    assert.match(bar, /border-bottom:\s*1px solid var\(--stroke\)/);
    // the where-chrome-shows inset-shadow seam is gone for good
    assert.ok(
      !bar.includes("box-shadow: inset 0 -1px"),
      "no breakable inset-shadow seam left on the tab bar",
    );
  });
});