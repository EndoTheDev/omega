// omega webui: leaf-contract characterization (v0.14.4 milestone 2).
// Pins TODAY'S divider-drag outcomes that the contract port touches - the
// FIXED absorbing neighbor (128 floor, not the chat's 320), the
// BOTH-FLEXIBLE fraction shift, and the contract factories/resolver.
// dock.test.ts (floors, snap ladder, the divider wall law) and dockbottom.test.ts
// (the bottom's share cap + 200 ladder) already characterize the rest.
// The port's law (ruling 7): defaults reproduce today exactly - a pane
// ships no contract until v:3 serializes one, so these must stay green
// through every port step. Render-free; node's built-in test runner.

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  dragSplitDivider,
  makePane,
  makeSplit,
  edgeShellContract,
  chatPaneContract,
  bottomShellContract,
  contractFloorOf,
  MIN_PANE_PX,
  CHAT_FLOOR_PX,
  type DockPane,
  type DockSplit,
} from "../app/composables/dock.ts";

const asPane = (n: unknown) => n as DockPane;
const asSplit = (n: unknown) => n as DockSplit;

describe("dragSplitDivider characterization: the fixed absorbing neighbor", () => {
  // a row of TWO fixed panes (rare - the contract port's real case is the
  // split pane beside a fixed shell): the absorbing neighbor floors at
  // 128, not 320. room = 540 - 0 others = 540.
  const twoFixed = () => makeSplit("row", [makePane("sidebar", 240), makePane("files", 300)]);

  it("left fixed over a fixed neighbor: the cap reads the neighbor's 128 floor", () => {
    // req 240 + 200 = 440 <= cap (540 - 128 = 412? NO: 440 > 412 ->
    // clamped to the cap; snap 440 -> 320-ladder zone? 440 free (425..455
    // is the 640 rung's neighborhood? no: 429..455 -> 440 is inside
    // 425+15 = 440 edge of the 640 zone? 640's zone is 625..655 - free)
    const out = dragSplitDivider(twoFixed(), 1, [240, 300], 200, 540);
    assert.equal(asPane(out.split.children[0]).fixed, 412, "clamped at room - 128");
  });

  it("left fixed over a fixed neighbor: the drag floor and close intent stand", () => {
    const onFloor = dragSplitDivider(twoFixed(), 1, [240, 300], -112, 540);
    assert.equal(asPane(onFloor.split.children[0]).fixed, MIN_PANE_PX);
    const past = dragSplitDivider(twoFixed(), 1, [240, 300], -113, 540);
    assert.equal(past.collapsedPaneId, asPane(past.split.children[0]).id);
  });

  it("snap ladder still applies above a fixed neighbor (320 rung)", () => {
    // req 320: inside the 320 rung's +-15 zone -> snaps
    const out = dragSplitDivider(twoFixed(), 1, [240, 300], 80, 540);
    assert.equal(asPane(out.split.children[0]).fixed, 320);
  });
});

describe("dragSplitDivider characterization: both panes flexible", () => {
  const flex = () => makeSplit("row", [makePane("chat"), makePane("files")]);
  const CONTENT = 1000;

  it("the drag shifts FRACTIONS against the content box, no fixed appears", () => {
    const out = dragSplitDivider(flex(), 1, [500, 500], 100, CONTENT);
    assert.deepEqual(asSplit(out.split).sizes.map((s) => Math.round(s * 100)), [60, 40]);
    assert.equal(asPane(asSplit(out.split).children[0]).fixed, null);
    assert.equal(out.collapsedPaneId, null);
  });

  it("the flexible drag walls at room - 320 on BOTH sides (t_8bcbe72e: the left floor is the chat's 320, not 128)", () => {
    // huge right drag: finalLeft clamps at pxHi = 1000 - 320 = 680
    const out = dragSplitDivider(flex(), 1, [500, 500], 900, CONTENT);
    assert.deepEqual(asSplit(out.split).sizes.map((s) => Math.round(s * 100)), [68, 32]);
    // huge left drag: finalLeft clamps at pxLo = contractFloorOf(left) = 320 -
    // the old pin here read 13 (128), the asymmetry Endo dragged his chat to
    const back = dragSplitDivider(flex(), 1, [500, 500], -900, CONTENT);
    assert.deepEqual(asSplit(back.split).sizes.map((s) => Math.round(s * 100)), [32, 68]);
  });

  it("the +320 attractor snaps a flexible drag like a fixed one", () => {
    // 500 + 130 = 630 -> inside 640's zone (625..655) -> snaps to 640
    const out = dragSplitDivider(flex(), 1, [500, 500], 130, CONTENT);
    assert.deepEqual(asSplit(out.split).sizes.map((s) => Math.round(s * 100)), [64, 36]);
  });
});

describe("leaf contracts: factories resolve today's constants (ruling 7)", () => {
  it("edgeShell: min 128, preferred 320, leading anchor, no ceiling", () => {
    assert.deepEqual(edgeShellContract(), {
      minPx: 128, maxPx: null, maxFrac: null, preferred: 320, anchor: "leading",
    });
  });

  it("chat pane: the 320 wall, flexible otherwise", () => {
    assert.deepEqual(chatPaneContract(), {
      minPx: 320, maxPx: null, maxFrac: null, preferred: 0, anchor: "leading",
    });
  });

  it("bottom shell: min 128, preferred 200, the max-share ceiling, bottom anchor", () => {
    assert.deepEqual(bottomShellContract(), {
      minPx: 128, maxPx: null, maxFrac: 0.5, preferred: 200, anchor: "bottom",
    });
  });

  it("contractFloorOf: fixed -> the shared 128 floor; flexible -> the 320 wall", () => {
    const fixed = makePane("sidebar", 240);
    const flex = makePane("chat");
    const split = makeSplit("row", [fixed, flex]);
    assert.equal(contractFloorOf(fixed), MIN_PANE_PX);
    assert.equal(contractFloorOf(flex), CHAT_FLOOR_PX);
    assert.equal(contractFloorOf(split), CHAT_FLOOR_PX, "a nested split absorbs like a flexible pane");
    assert.equal(contractFloorOf(makeSplit("row", [])), CHAT_FLOOR_PX);
  });

  it("contractFloorOf: a pane carrying a contract reads ITS floor", () => {
    const stiff = { ...makePane("files"), contract: chatPaneContract() };
    assert.equal(contractFloorOf(stiff), CHAT_FLOOR_PX);
    const stiffFixed = { ...makePane("sidebar", 240), contract: edgeShellContract() };
    assert.equal(contractFloorOf(stiffFixed), MIN_PANE_PX, "fixed identity owns the drag floor");
  });

  it("contractFloorOf: the drag caps read it - a 640-floor pane really walls at 640", () => {
    // chat flexible with a hand-raised contract floor 640: the fixed-left
    // cap becomes room - 640
    const chat = { ...makePane("chat"), contract: { ...chatPaneContract(), minPx: 640 } };
    const sidebar = makePane("sidebar", 240);
    const split = makeSplit("row", [sidebar, chat]);
    // room 1000 - 0 = 1000; cap = max(128, 1000 - 640) = 360
    const out = dragSplitDivider(split, 1, [240, 760], 900, 1000);
    assert.equal(asPane(out.split.children[0]).fixed, 360, "cap partitions by the contract floor");
    const snap = dragSplitDivider(split, 1, [240, 760], 120, 1000);
    assert.equal(asPane(snap.split.children[0]).fixed, 360, "req 360 == cap");
  });

  it("MIN_PANE_PX/CHAT_FLOOR_PX keep their values (the defaults cite them)", () => {
    assert.equal(MIN_PANE_PX, 128);
    assert.equal(CHAT_FLOOR_PX, 320);
  });
});