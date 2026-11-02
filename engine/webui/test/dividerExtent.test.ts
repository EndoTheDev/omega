// omega webui: unit tests for the divider law (card t_bc321318, Endo
// ruling 2026-10-25: structural dividers run THROUGH; t_e539ba29,
// Endo ruling 2026-10-28: the COLUMN divider's resting line died - it
// stacked with the chat card's bottom edge and the tab strip's border-top
// into three hairlines that read as "lines going through the divider",
// with the Logs chip sitting on the topmost seam; t_4e2ceca1, Endo
// ruling 2026-10-28 follow-up: the law is UNIFORM - BOTH orientations
// rest invisible, the row splits' verticals included). The render-free
// suite can't measure boxes, so it pins the sheet instead - the family
// maxheight.test.ts established: parse main.css + bottompanel.css and
// make the rules carry the law. Pinned here:
//   1. the base .dock-divider rule still carries the centered 1px
//      structural-line gradient (the hover band outpaints it, and the
//      sheet keeps one source of truth for the geometry)
//   2. BOTH orientations rest INVISIBLE: the generic (0,4,0) guard
//      kills the base gradient for every .dock-divider at rest -
//      hover/drag/snap are outside its guards and outpaint it
//   3. no border-radius on the handle (the hover band runs edge-to-edge)
//   4. hover/snap keep their full-band backgrounds (they ARE the line
//      while the pointer is on the handle)
//   5. the gutter bridge is GONE (t_9846a12e, Endo stroke-cleanup ruling
//      2026-11-02): the ROW divider's ::before/::after top/bottom strokes
//      (t_97cece1e) read as dashes at the divider's ends - zero rules for
//      them in the sheet; the cards' own borders carry the seams alone.
//      The pane-area's unconditional ::before/::after rails stay dead
//      too (t_97cece1e); single card = no divider = zero strokes
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

describe("divider law (t_bc321318 through-lines; t_e539ba29 + t_4e2ceca1 BOTH orientations rest invisible)", () => {
  it("the base .dock-divider rule carries the centered 1px line gradient", () => {
    const body = find(MAIN, ".dock-divider").body.replace(/\s+/g, " ");
    assert.ok(
      body.includes(gradientOf("to right")),
      "the base rule keeps the structural-line gradient (the guard, not deletion, kills its resting paint)",
    );
  });

  it("BOTH orientations rest INVISIBLE - the generic guard kills the line - the hover band owns the seam", () => {
    const body = find(MAIN, ".dock-split.column > .dock-divider").body
      .replace(/\s+/g, " ");
    assert.ok(
      !body.includes("linear-gradient"),
      "no resting gradient left on the column divider",
    );
    // the guard rule kills the base (line) gradient for EVERY divider at
    // rest - generic, both orientations (t_4e2ceca1: the row splits'
    // verticals kept painting their resting stroke after e36e555) -
    // hover/drag/snap must outpaint it (t_e539ba29: the (0,3,0) column
    // rule out-ranked the (0,2,0) bands and the handle never highlighted;
    // the generic form counts three :not() pseudo-classes: (0,4,0))
    const guard = find(
      MAIN,
      ".dock-divider:not(:hover):not(.active):not(.snap)",
    ).body.replace(/\s+/g, " ");
    assert.match(guard, /background:\s*none/);
    // a scoped kill is a regression: the law is uniform, no orientation
    // scoping left in the selector
    assert.ok(
      !guard.includes(".dock-split"),
      "the rest-guard is generic - it must not be re-scoped to one orientation",
    );
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

  it("the rail law: a rail renders ONLY across a gutter - the pane-area strokes are gone (single card = zero)", () => {
    // the unconditional whole-width strokes (t_bc321318, amended twice)
    // died: in single-pane view they sat ON the card's own border row and
    // duplicated it (Endo 2026-11-01: "i cant think of it being intended")
    const railRules = MAIN.filter((r) =>
      r.selectors.some((s) => /^\.pane-area::(before|after)$/.test(s)),
    );
    assert.equal(
      railRules.length,
      0,
      "no pane-area ::before/::after rail rule left anywhere in the sheet",
    );
    // the no-gap case pinned structurally: DockSplit renders a divider
    // only BETWEEN two children (idx < children.length - 1) - a
    // single-card tree renders no divider, so zero rail elements exist;
    // the card's own border is the whole edge
    const split = readFileSync(
      join(here, "../app/components/DockSplit.vue"),
      "utf8",
    );
    assert.match(
      split,
      /node\.children\.length - 1/,
      "dividers render only between two children - keep this guard or the no-gap pin breaks",
    );
  });

  it("the gutter bridge is gone: zero rules for the row divider's ::before/::after (and the sheet has no strays)", () => {
    // t_9846a12e (Endo stroke-cleanup ruling 2026-11-02): the bridge's
    // 1px dashes at the row divider's top/bottom ends are strokes the
    // frame layer paints - dead by the ruling, in ANY at-rule (desktop
    // or otherwise). Nothing anchors absolute strokes to the row
    // divider's box anymore either: the position:relative anchor died
    // with them (bottompanel's row-divider rule uses margins only).
    const bridgeRules = MAIN.filter((r) =>
      r.selectors.some((s) =>
        /^\.dock-split\.row > \.dock-divider::(before|after)$/.test(s),
      ),
    );
    assert.equal(
      bridgeRules.length,
      0,
      "no gutter-bridge pseudo-element rules left anywhere in the sheet",
    );
    const anchored = MAIN.filter(
      (r) =>
        r.selectors.includes(".dock-split.row > .dock-divider") &&
        r.body.includes("position"),
    );
    assert.equal(
      anchored.length,
      0,
      "no position anchor on the row divider's box - its absolutes are gone",
    );
    // regression guard, not the ruling: the COLUMN grab band (8px hover
    // band) keeps its own ::before overlay
    const grab = find(MAIN, ".dock-split.column > .dock-divider::before");
    assert.match(grab.body.replace(/\s+/g, " "), /top:\s*-8px/);
    assert.match(grab.body.replace(/\s+/g, " "), /bottom:\s*-8px/);
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