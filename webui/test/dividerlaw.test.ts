// omega webui: t_f7201e02 - the both-flexible divider drag law.
//
// The original body shifted the CURRENT sizes by (finalLeft - pxOf)/content,
// but the store grafts the moved split back and every later move event
// re-derives from the FROZEN startPx: the full cumulative delta was applied
// on top of all earlier events' shifts, so the divider compounded and raced
// to a wall on the first moves (Endo's "jumps to the next wall"). These
// tests run the loop the component actually drives - the same frozen
// baseline, the grafted (already moved) split as the next event's input -
// and pin:
//   - idempotence: event n re-computes the pair's shares from the frozen
//     baseline; small deltas produce small moves from any start width.
//   - grab guard: a start already inside a rung's attract zone (a passive
//     hold the gesture did not travel into) must not teleport on grab -
//     snap engages only when the drag BRINGS the intended px into a zone
//     (travel magnet), the fixed lanes' no-jump-on-grab feel.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makePane,
  makeSplit,
  dragSplitDivider,
  type DockSplit,
} from "../app/composables/dock.ts";

// a 2-child all-flexible split (grown-root leaves: both makePane(null)) whose
// sizes match the measured gesture-start geometry (the component's baseline:
// rendered flexible px = contentPx * size / totalFlex)
const freshPair = (leftPx: number, rightPx: number, contentPx: number): DockSplit =>
  ({
    ...makeSplit("row", [makePane(null), makePane(null)]),
    sizes: [leftPx / contentPx, rightPx / contentPx],
  });

// the loop DockSplit.vue drives: frozen startPx + the grafted split each event
function feed(
  split: DockSplit,
  dividerIndex: number,
  startPx: number[],
  contentPx: number,
  cumulatives: number[],
): DockSplit {
  let node = split;
  for (const c of cumulatives) {
    node = dragSplitDivider(node, dividerIndex, startPx, c, contentPx).split as DockSplit;
  }
  return node;
}

/** the rendered left px the render math derives from the pair's sizes:
    flexible cells share the content box proportionally (cellStyleFor) */
function impliedLeftPx(split: DockSplit, contentPx: number): number {
  const s0 = split.sizes[0]!;
  const s1 = split.sizes[1]!;
  return contentPx * (s0 / (s0 + s1));
}

describe("dragSplitDivider both-flexible drag law (t_f7201e02)", () => {
  it("multi-event loop: each event lands on baseline+cumulative, no runaway", () => {
    const node = feed(freshPair(498, 498, 996), 1, [498, 498], 996, [2, 5, 10, 18]);
    // the last event's cumulative is 18: intended left = 516. The compounded
    // body re-applied every event's shift (2+5+10+18) and landed 533.
    const p = impliedLeftPx(node, 996);
    assert.ok(Math.abs(p - 516) < 0.01, `left implied ${p}, want 516`);
    assert.ok(Math.abs(node.sizes[0]! + node.sizes[1]! - 1) < 1e-9, "pair sum stays normalized");
  });

  it("second event moves by ITS delta, not the full cumulative (the first-move jump)", () => {
    const one = feed(freshPair(498, 498, 996), 1, [498, 498], 996, [2]);
    const two = feed(one, 1, [498, 498], 996, [5]);
    // event 2 intends 503; the compounded body landed 505 (event 1's shift re-applied)
    const p = impliedLeftPx(two, 996);
    assert.ok(Math.abs(p - 503) < 0.01, `got ${p}, want 503`);
  });

  it("near-rung passive start: a 2px delta moves ~2px, NOT to the rung", () => {
    // 645 sits inside rung 640's 15px zone without the gesture traveling there
    const out = dragSplitDivider(freshPair(645, 355, 1000), 1, [645, 355], 2, 1000);
    const p = impliedLeftPx(out.split as DockSplit, 1000);
    assert.ok(Math.abs(p - 647) < 0.01, `got ${p}, want 647 (no grab jump)`);
  });

  it("on-rung start: small deltas keep following the cursor", () => {
    const out = dragSplitDivider(freshPair(640, 360, 1000), 1, [640, 360], 3, 1000);
    const p = impliedLeftPx(out.split as DockSplit, 1000);
    assert.ok(Math.abs(p - 643) < 0.01, `got ${p}, want 643`);
  });

  it("travel magnet: the drag BRINGING the intended px into a zone attracts, both directions", () => {
    const fwd = dragSplitDivider(freshPair(520, 480, 1000), 1, [520, 480], 110, 1000);
    assert.equal(impliedLeftPx(fwd.split as DockSplit, 1000), 640, "forward travel magnet");
    const back = dragSplitDivider(freshPair(840, 160, 1000), 1, [840, 160], -200, 1000);
    assert.equal(impliedLeftPx(back.split as DockSplit, 1000), 640, "backward travel magnet");
  });

  it("traveling out of a zone escapes free (no re-snap)", () => {
    const out = dragSplitDivider(freshPair(520, 480, 1000), 1, [520, 480], 160, 1000);
    const p = impliedLeftPx(out.split as DockSplit, 1000);
    assert.ok(Math.abs(p - 680) < 0.01, `got ${p}, want 680`);
  });
});