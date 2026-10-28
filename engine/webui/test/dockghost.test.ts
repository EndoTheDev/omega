// omega webui: unit tests for the ghost preview (v0.14.4 milestone 5,
// ruling 5). The load-bearing law already pins prediction == commit in
// dockdnd.test.ts; these pin the ghost's OWN read: ghostPreview delegates
// to predictDockDrop (its refusal is the commit's refusal, verbatim), the
// split halves are the insert law's exact 50/50 of the measured axis px,
// and the divider readout's numerals/rung math. Render-free.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makePane,
  makeSplit,
  predictDockDrop,
  type DockNode,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";
import {
  ghostPreview,
  resizeReadout,
  ghostRegistry,
} from "../app/composables/dockghost.ts";

const asPane = (n: DockNode) => n as DockPane;
const asSplit = (n: DockNode) => n as DockSplit;

function rowRoot(): { root: DockSplit; a: DockPane; b: DockPane } {
  const a = makePane("chat");
  const b = makePane("canvas");
  const root = makeSplit("row", [a, b]);
  return { root, a, b };
}

describe("ghostPreview (the exact before-drop read)", () => {
  it("a split landing: halves are the measured axis px at exactly 50/50", () => {
    const { root, a, b } = rowRoot();
    const g = ghostPreview(root, a.id, 0, b.id, "left", 480);
    assert.equal(g.refused, null);
    assert.equal(g.direction, "row");
    assert.deepEqual(g.halves, [
      { px: 240, pct: 0.5 },
      { px: 240, pct: 0.5 },
    ]);
  });

  it("the halves split ODD px into round(floor/ceil) without drift", () => {
    const { root, a, b } = rowRoot();
    const g = ghostPreview(root, a.id, 0, b.id, "top", 481);
    assert.ok(g.halves);
    assert.equal(g.halves!.reduce((s, h) => s + h.px, 0), 481, "no px lost");
    assert.equal(g.halves![0]!.px, 241, "first half = round(481/2)");
    assert.equal(g.halves![1]!.px, 240, "second half = the remainder");
  });

  it("a refusal returns the commit's own reason verbatim", () => {
    const { root, a, b } = rowRoot();
    const same = predictDockDrop(root, a.id, 0, a.id, "left");
    assert.ok("refused" in same);
    const g = ghostPreview(root, a.id, 0, a.id, "left", 480);
    assert.equal(g.refused, same.reason);
    assert.equal(g.halves, null);
  });

  it("a center drop previews the group (no direction, no halves)", () => {
    const { root, a, b } = rowRoot();
    asPane(asSplit(root).children[0]).tabs = [{ component: "files" }, { component: "chat" }];
    const g = ghostPreview(root, a.id, 1, b.id, "center", 480);
    assert.equal(g.refused, null);
    assert.equal(g.direction, null);
    assert.equal(g.halves, null);
  });

  it("an unmeasured pane omits the halves instead of guessing", () => {
    const { root, a, b } = rowRoot();
    const g = ghostPreview(root, a.id, 0, b.id, "right", null);
    assert.equal(g.refused, null);
    assert.equal(g.halves, null);
    assert.equal(g.direction, "row");
  });

  it("the ghost's accepted read equals the predictor's accepted read (one formula)", () => {
    for (const zone of ["left", "right", "top", "bottom"] as const) {
      const { root, a, b } = rowRoot();
      asPane(asSplit(root).children[0]).tabs = [{ component: "files" }, { component: "chat" }];
      const p = predictDockDrop(root, a.id, 0, b.id, zone);
      const g = ghostPreview(root, a.id, 0, b.id, zone, 600);
      if ("refused" in p) {
        assert.equal(g.refused, p.reason, `${zone}: refusals agree verbatim`);
        continue;
      }
      assert.equal(g.refused, null, `${zone}: the predictor accepts, the ghost accepts`);
    }
  });
});

describe("resizeReadout (the divider's snap numerals)", () => {
  it("formats px + % of the content box", () => {
    const r = resizeReadout(320, 960, 320);
    assert.equal(r.text, "320px (33%) \u2192 snap 320px", "320 IS a rung of its own ladder");
    assert.equal(r.rung, 320);
    assert.equal(resizeReadout(310, 960, 320).text, "310px (32%) \u2192 snap 320px");
    assert.equal(resizeReadout(350, 960, 320).text, "350px (36%)", "350 is 30 off the rung: free");
    assert.equal(r.pct > 0.33 && r.pct < 0.34, true);
  });

  it("names the rung inside the 15px attract zone, free drags outside", () => {
    assert.equal(resizeReadout(305, 960, 320).rung, 320, "305 is inside the zone (15 from the rung)");
    assert.equal(resizeReadout(335, 960, 320).rung, 320);
    assert.equal(resizeReadout(289, 960, 320).rung, null, "289 is 31 from the rung - free");
    assert.equal(resizeReadout(0, 960, 320).rung, null, "no rung below one step");
  });

  it("degenerates honestly (zero content)", () => {
    const r = resizeReadout(0, 0, 320);
    assert.equal(r.pct, 0);
    assert.equal(r.text, "0px (0%)");
  });
});

describe("ghostRegistry (geometry snapshotted at dragstart)", () => {
  it("records per gesture: first stamp wins, end clears", () => {
    assert.equal(ghostRegistry.current(), null);
    ghostRegistry.begin("p1", 0);
    ghostRegistry.record("p1", { x: 0, y: 0, w: 100, h: 50 });
    ghostRegistry.record("p1", { x: 9, y: 9, w: 9, h: 9 }); // stale stamp refused
    ghostRegistry.record("p2", { x: 100, y: 0, w: 100, h: 50 });
    const cur = ghostRegistry.current();
    assert.equal(cur!.source!.paneId, "p1");
    assert.deepEqual(cur!.rects.get("p1"), { x: 0, y: 0, w: 100, h: 50 });
    assert.deepEqual(cur!.rects.get("p2"), { x: 100, y: 0, w: 100, h: 50 });
    ghostRegistry.end();
    assert.equal(ghostRegistry.current(), null);
    ghostRegistry.record("p3", { x: 0, y: 0, w: 1, h: 1 }); // no gesture: ignored
    assert.equal(ghostRegistry.current(), null);
  });
});