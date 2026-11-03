// omega webui: the divider drag's pointer reduction (card t_38c0e960).
// The store math's deltaPx is CUMULATIVE from the measured baseline
// (dragSplitDivider: req = startPx[k] + delta) while the divider emits a
// delta cumulative from its pointerdown origin plus the raw position.
// The v0.9.0 rework joined them with per-event increments - every move
// reset the pane to baseline + one increment and the divider never moved
// more than ~10px in a full drag (live-proven regression). These tests
// pin the reduction shape production actually sends. The old rebase arm
// died with the squeeze push-back chain (t_6a74d5a1 wall law: a cap is a
// stop, not a push) - the reducer takes no rebase flag anymore.

import { test } from "node:test";
import assert from "node:assert/strict";
import { dividerDelta, type DividerPointerState } from "../app/composables/dividerGesture.ts";
import {
  dragSplitDivider,
  makePane,
  makeSplit,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

const fresh = (): DividerPointerState => ({ base: null });

// shell: same shape as dock.test.ts's divider describe block
const shell = (): DockSplit =>
  makeSplit("row", [makePane("sidebar", 240), makePane("chat", null), makePane("files", 300)]);

test("dividerDelta: gesture start adopts the divider's pointerdown origin", () => {
  const s = fresh();
  // pointerdown at raw 100, first move arrives at raw 110
  const d = dividerDelta(s, 10, 110);
  assert.equal(d, 10);
  assert.equal(s.base, 100, "base becomes the pointerdown origin");
});

test("dividerDelta: a full drag accumulates - 15 moves send a growing cumulative delta", () => {
  const s = fresh();
  // divider emit per move: delta = raw - pointerdown origin (100)
  let last = 0;
  for (let i = 1; i <= 15; i++) {
    const raw = 100 + i * 10;
    const d = dividerDelta(s, i * 10, raw);
    assert.ok(d !== null && d > last, `moves accumulate (event ${i}: ${d} > ${last})`);
    last = d!;
  }
  assert.equal(last, 150, "final delta = pointer travel from the origin");
});

test("dividerDelta + dragSplitDivider: the reduced stream tracks the pointer (the regression shape)", () => {
  // the store math sees the SAME startPx every event plus the reducer's
  // cumulative delta. Baseline 240 + 150 travel = 390 wanted, but the wall
  // law (t_6a74d5a1) caps the sidebar at room - chat floor = 700 - 320:
  // the divider STOPS at 380, nothing beyond the chat moves.
  const CONTENT = 1000;
  const s = fresh();
  let t = shell();
  for (let i = 1; i <= 15; i++) {
    const raw = 100 + i * 10;
    const d = dividerDelta(s, i * 10, raw)!;
    t = dragSplitDivider(t, 1, [240, 460, 300], d, CONTENT).split;
  }
  assert.equal((t.children[0] as DockPane).fixed, 380, "sidebar rides the pointer to the wall (240 + 150, capped)");
});

// t_be305778: the column orientation joins the pointer contract. The
// bottom shell anchors to the viewport's BOTTOM - the divider is its TOP
// edge (y = contentPx - height) - so a drag UP (delta < 0) GROWS it and
// drag down shrinks it: the mirror of the row's fixed-right-sidebar read.
// The old "+delta grows it" left-child read moved the divider AGAINST the
// pointer (drag up -> divider down); this test pins BOTH ways so the
// orientation cannot regress silently again.
const columnShell = () => makeSplit("column", [makePane("chat", null), makePane("http_channel", 200)]);

test("dividerDelta + dragSplitDivider (column): drag UP tracks the pointer up (the shell GROWS)", () => {
  const CONTENT = 1000; // mainRow 800 + bottom 200; divider starts at y 800
  const s = fresh();
  let t = columnShell();
  for (let i = 1; i <= 15; i++) {
    const raw = 800 - i * 10;
    const d = dividerDelta(s, -i * 10, raw)!;
    t = dragSplitDivider(t, 1, [800, 200], d, CONTENT).split;
  }
  const b = (t.children[1] as DockPane).fixed;
  assert.ok(b !== null);
  assert.equal(b, 350, "drag up 150 grows the shell 200 -> 350");
  assert.equal(CONTENT - b, 650, "the divider's y rode UP with the pointer (800 - 150)");
});

test("dividerDelta + dragSplitDivider (column): drag DOWN tracks the pointer down (the shell SHRINKS)", () => {
  const CONTENT = 1000;
  const s = fresh();
  let t = columnShell();
  // 50px of travel (5 moves): further travel hits the 128 close floor -
  // the shell's own divider push - which is covered in dockbottom.test.ts
  for (let i = 1; i <= 5; i++) {
    const raw = 800 + i * 10;
    const d = dividerDelta(s, i * 10, raw)!;
    t = dragSplitDivider(t, 1, [800, 200], d, CONTENT).split;
  }
  const b = (t.children[1] as DockPane).fixed;
  assert.ok(b !== null);
  assert.equal(b, 150, "drag down 50 shrinks the shell 200 -> 150");
  assert.equal(CONTENT - b, 850, "the divider's y rode DOWN with the pointer (800 + 50)");
});

test("dividerDelta: the reworked shape (per-event increments) fails loudly", () => {
  // the 6a90f3a bug: deltas stuck at one increment - the reduction must
  // NOT produce that stream
  const s = fresh();
  const got: number[] = [];
  for (let i = 1; i <= 15; i++) got.push(dividerDelta(s, 10, 100 + i * 10)!);
  assert.equal(got.filter((d) => d > 10).length, 14, "every event after the first moves past one increment");
});

test("dividerDelta: no rebase arm - the baseline is frozen for the whole gesture", () => {
  // t_6a74d5a1 wall law: a cap is a stop, not a push, so nothing ever
  // re-measures mid-gesture; the reducer ignores extra call shapes and
  // deltas keep growing from the one pointerdown origin
  const s = fresh();
  dividerDelta(s, 40, 140); // base = 100
  assert.equal(dividerDelta(s, 60, 200), 100);
  assert.equal(s.base, 100, "the base never re-adopts a mid-gesture raw");
});