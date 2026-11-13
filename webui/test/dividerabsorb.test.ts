// omega webui: card t_8098aa06 - the fixed lanes' absorb law.
//
// A divider with a FIXED pane on one side controls that pane and the
// ADJACENT neighbor absorbs 1:1. The old body wrote only the fixed pane's
// px and let the flex reflow spread the delta over every flexible child:
// the pane the divider touches tracked the pointer at its SHARE of the flex
// space (Endo 2026-11-13: a small terminal pane beside the chat shrank at a
// fraction of the drag - "the terminal shrinks SLOW", right divider only,
// because the left divider is the both-flexible lane and already 1:1),
// and a pane the user never touched moved too (the wall law).
//
// These tests drive the loop DockSplit.vue drives (frozen startPx, the
// grafted split as the next event's input) and render the result through
// the cellStyleFor law, pinning:
//   - every leg of a terminal pane's two dividers tracks the pointer 1:1,
//   - the panes the gesture did not touch hold their measured px,
//   - the sizes sum survives (the record's px shares re-derive it).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makePane,
  makeSplit,
  dragSplitDivider,
  type DockSplit,
} from "../app/composables/dock.ts";
import type { DockComponentName } from "../app/composables/dockables.ts";

interface Child {
  comp: DockComponentName;
  px: number;
  fixed?: number;
  edge?: "left" | "right" | "bottom";
}

/** a split whose fractions are the children's own px (the render reads
    only the RATIO, and the drag treats the fractions as the baseline) */
function tree(direction: "row" | "column", spec: Child[]): DockSplit {
  const children = spec.map((c) => makePane(c.comp, c.fixed ?? null, c.edge));
  const split = makeSplit(direction, children);
  split.sizes = spec.map((c) => (c.fixed === undefined ? c.px : 0));
  return split;
}

/** the pane's fixed px (null = flexible; a nested split is never fixed) */
const fixedOf = (c: DockSplit["children"][number]): number | null =>
  c.kind === "pane" ? c.fixed : null;

/** the render law (DockSplit.vue cellStyleFor): fixed panes keep their px,
    flexible cells share the remaining box by their fraction
    (px is only a ratio against the flex sum) */
function render(split: DockSplit, contentPx: number): number[] {
  let fixedSum = 0;
  let flexSum = 0;
  split.children.forEach((c, k) => {
    const f = fixedOf(c);
    if (f !== null) fixedSum += f;
    else flexSum += split.sizes[k] ?? 0;
  });
  const free = contentPx - fixedSum;
  return split.children.map((c, k) => {
    const f = fixedOf(c);
    return f !== null ? f : ((split.sizes[k] ?? 0) / flexSum) * free;
  });
}

const startPx = (split: DockSplit, contentPx: number): number[] =>
  render(split, contentPx).map((p) => Math.round(p));

/** the component's loop: frozen startPx, one call per move event, the
    grafted split carried into the next event */
function feed(
  split: DockSplit,
  dividerIndex: number,
  px: number[],
  contentPx: number,
  cumulatives: number[],
): DockSplit {
  let node = split;
  for (const c of cumulatives) {
    node = dragSplitDivider(node, dividerIndex, px, c, contentPx).split as DockSplit;
  }
  return node;
}

describe("the fixed lanes' adjacent absorb (card t_8098aa06)", () => {
  // row: sidebar 240 fixed, chat 600 flex, TERMINAL 500 flex, inspector 560 fixed
  const CONTENT = 1900;
  const mk = (): DockSplit =>
    tree("row", [
      { comp: "sidebar", px: 240, fixed: 240, edge: "left" },
      { comp: "chat", px: 600 },
      { comp: "terminal", px: 500 },
      { comp: "files", px: 560, fixed: 560, edge: "right" },
    ]);

  it("the reported leg: the right divider's inward drag shrinks the terminal 1:1", () => {
    const t = mk();
    const px = startPx(t, CONTENT);
    assert.deepEqual(px, [240, 600, 500, 560], "the gesture-start measurement");
    // drag LEFT 20: the inspector grows to 580, the terminal gives exactly 20
    const out = render(feed(t, 3, px, CONTENT, [-20]), CONTENT);
    assert.equal(Math.round(out[3]!), 580, "the controlled pane follows the pointer");
    assert.equal(Math.round(out[2]!), 480, "the neighbor gives exactly the drag (was: 490)");
    assert.equal(Math.round(out[1]!), 600, "the chat is not adjacent - bit-identical");
  });

  it("the same divider's outward drag grows the terminal 1:1 (no direction asymmetry)", () => {
    const t = mk();
    const px = startPx(t, CONTENT);
    const out = render(feed(t, 3, px, CONTENT, [20]), CONTENT);
    assert.equal(Math.round(out[3]!), 540);
    assert.equal(Math.round(out[2]!), 520, "the neighbor grows exactly the drag (was: 510)");
    assert.equal(Math.round(out[1]!), 600, "the chat holds");
  });

  it("the terminal's LEFT divider (both flexible) keeps its 1:1 law", () => {
    const t = mk();
    const px = startPx(t, CONTENT);
    const inward = render(feed(t, 2, px, CONTENT, [-20]), CONTENT);
    assert.equal(Math.round(inward[1]!), 580, "the chat is the controlled pane");
    assert.equal(Math.round(inward[2]!), 520, "the terminal takes the complement, 1:1");
    const outward = render(feed(t, 2, px, CONTENT, [20]), CONTENT);
    assert.equal(Math.round(outward[2]!), 480, "1:1 both ways");
  });

  it("a fixed pane on the LEFT: the terminal neighbor absorbs on both legs", () => {
    const CONTENT2 = 1900;
    const t = tree("row", [
      { comp: "sidebar", px: 240, fixed: 240, edge: "left" },
      { comp: "terminal", px: 500 },
      { comp: "chat", px: 700 },
      { comp: "files", px: 460, fixed: 460, edge: "right" },
    ]);
    const px = startPx(t, CONTENT2);
    assert.deepEqual(px, [240, 500, 700, 460]);
    // dragging right grows the sidebar over the terminal
    const grow = render(feed(t, 1, px, CONTENT2, [20]), CONTENT2);
    assert.equal(Math.round(grow[0]!), 260);
    assert.equal(Math.round(grow[1]!), 480, "the terminal gives exactly 20 (was: 490)");
    assert.equal(Math.round(grow[2]!), 700, "the chat beyond never moves");
    // and dragging left shrinks the sidebar, handing the px back 1:1
    const shrink = render(feed(t, 1, px, CONTENT2, [-20]), CONTENT2);
    assert.equal(Math.round(shrink[0]!), 220);
    assert.equal(Math.round(shrink[1]!), 520);
    assert.equal(Math.round(shrink[2]!), 700);
  });

  it("the column lane (bottom shell): the pane above absorbs 1:1, others hold", () => {
    const CONTENT3 = 900;
    const t = tree("column", [
      { comp: "chat", px: 400 },
      { comp: "terminal", px: 300 },
      { comp: "observability", px: 200, fixed: 200, edge: "bottom" },
    ]);
    const px = startPx(t, CONTENT3);
    assert.deepEqual(px, [400, 300, 200]);
    // drag UP 20 grows the shell: the terminal above gives 20, the chat holds
    const up = render(feed(t, 2, px, CONTENT3, [-20]), CONTENT3);
    assert.equal(Math.round(up[2]!), 220);
    assert.equal(Math.round(up[1]!), 280, "the neighbor gives exactly the drag");
    assert.equal(Math.round(up[0]!), 400, "the chat above never moves");
    const down = render(feed(t, 2, px, CONTENT3, [20]), CONTENT3);
    assert.equal(Math.round(down[2]!), 180);
    assert.equal(Math.round(down[1]!), 320);
    assert.equal(Math.round(down[0]!), 400);
  });

  it("a single flexible child keeps today's exact behavior (no size churn)", () => {
    const CONTENT4 = 1500;
    const t = tree("row", [
      { comp: "sidebar", px: 240, fixed: 240, edge: "left" },
      { comp: "chat", px: 800 },
      { comp: "files", px: 460, fixed: 460, edge: "right" },
    ]);
    const px = startPx(t, CONTENT4);
    assert.deepEqual(px, [240, 800, 460]);
    const before = [...t.sizes];
    const out = dragSplitDivider(t, 1, px, 40, CONTENT4).split as DockSplit;
    assert.equal(fixedOf(out.children[0]!), 280);
    assert.deepEqual(out.sizes, before, "one flexible child: nothing to freeze, sizes untouched");
  });

  it("multi-event loop: each event lands on baseline + cumulative (no compounding)", () => {
    const t = mk();
    const px = startPx(t, CONTENT);
    const out = render(feed(t, 3, px, CONTENT, [-4, -9, -12, -20]), CONTENT);
    assert.equal(Math.round(out[2]!), 480, "the last cumulative, applied once");
    assert.equal(Math.round(out[3]!), 580);
    const sum = (feed(t, 3, px, CONTENT, [-20]) as DockSplit).sizes.reduce((a, s) => a + s, 0);
    assert.ok(Math.abs(sum - t.sizes.reduce((a, s) => a + s, 0)) < 1e-9, "the sizes sum survives");
  });

  it("the absorber's floor still walls the drag (the terminal parks on 128)", () => {
    const t = mk();
    const px = startPx(t, CONTENT);
    // room = 500 + 560 = 1060: the inspector stops at 1060 - 128 (the
    // terminal's own drag floor), so the terminal never starves and the
    // chat 600px away never donates a byte
    const out = render(feed(t, 3, px, CONTENT, [-900]), CONTENT);
    assert.equal(Math.round(out[2]!), 128, "the terminal parks on its floor");
    assert.equal(Math.round(out[1]!), 600, "the chat never donates");
  });
});
