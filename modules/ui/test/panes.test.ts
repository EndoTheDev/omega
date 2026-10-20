// omega webui: unit tests for the pane-width math in composables/panes.ts
// (the +320 magnetic snap ladder, open-clamp floor). Pure functions, no DOM.
// Run: node --test "test/*.test.ts"
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  PANE_OPEN_MIN,
  SNAP_STEP,
  SNAP_ZONE,
  openClamp,
  snapCandidate,
  snapLadder,
} from "../app/composables/panes.ts";

describe("snapCandidate", () => {
  it("sticks exactly at every +320 rung (320, 640, 960, 1280, 1600)", () => {
    for (const p of snapLadder(1600)) {
      assert.equal(snapCandidate(p), p);
    }
  });

  it("attracts the whole 15px zone around each rung", () => {
    for (const p of snapLadder(1600)) {
      assert.equal(snapCandidate(p - SNAP_ZONE), p, `${p - SNAP_ZONE} should stick to ${p}`);
      assert.equal(snapCandidate(p + SNAP_ZONE), p, `${p + SNAP_ZONE} should stick to ${p}`);
      assert.equal(snapCandidate(p - 1), p);
      assert.equal(snapCandidate(p + 1), p);
    }
  });

  it("ignores just outside the zone - the drag stays free there", () => {
    for (const p of snapLadder(1600)) {
      assert.equal(snapCandidate(p - SNAP_ZONE - 1), null);
      assert.equal(snapCandidate(p + SNAP_ZONE + 1), null);
    }
  });

  it("stays null through the gaps between zones and at the edges", () => {
    assert.equal(snapCandidate(200), null);
    assert.equal(snapCandidate(480), null);
    assert.equal(snapCandidate(800), null);
    assert.equal(snapCandidate(1120), null);
    assert.equal(snapCandidate(0), null);
    assert.equal(snapCandidate(128), null);
    assert.equal(snapCandidate(1440), null); // halfway to 1600
  });

  it("does not clamp - far positions return null, not the nearest snap", () => {
    assert.notEqual(snapCandidate(321), 640);
    assert.notEqual(snapCandidate(600), 320);
    assert.notEqual(snapCandidate(14000), 960);
  });

  it("zone is 15px and the ladder step is 320 as specified", () => {
    assert.equal(SNAP_ZONE, 15);
    assert.equal(SNAP_STEP, 320);
  });
});

describe("snapLadder", () => {
  it("generates 320 then every +320, capped", () => {
    assert.deepEqual(snapLadder(320), [320]);
    assert.deepEqual(snapLadder(640), [320, 640]);
    assert.deepEqual(snapLadder(1280), [320, 640, 960, 1280]);
    assert.deepEqual(snapLadder(1200), [320, 640, 960]);
  });

  it("is the set snapCandidate answers from", () => {
    for (const p of snapLadder(2560)) assert.equal(snapCandidate(p), p);
  });
});

describe("openClamp", () => {
  it("lifts a stale sub-floor width to the floor on open", () => {
    assert.equal(openClamp(180), 320);
    assert.equal(openClamp(1), PANE_OPEN_MIN);
    assert.equal(openClamp(0), PANE_OPEN_MIN);
  });

  it("passes widths at or above the floor through unchanged", () => {
    assert.equal(openClamp(320), 320);
    assert.equal(openClamp(321), 321);
    assert.equal(openClamp(640), 640);
    assert.equal(openClamp(1200), 1200);
  });

  it("floor is the 320 dock constant", () => {
    assert.equal(PANE_OPEN_MIN, 320);
  });
});