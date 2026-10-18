// omega webui: unit tests for the pane-width math in composables/panes.ts
// (magnetic snap attractor around 320/640, open-clamp floor). Pure functions,
// no DOM. Run: node --test "test/*.test.ts"
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  PANE_OPEN_MIN,
  SNAP_POINTS,
  SNAP_ZONE,
  openClamp,
  snapCandidate,
} from "../app/composables/panes.ts";

describe("snapCandidate", () => {
  it("sticks exactly at both snap points", () => {
    assert.equal(snapCandidate(320), 320);
    assert.equal(snapCandidate(640), 640);
  });

  it("attracts the whole 15px zone around each point", () => {
    for (const p of SNAP_POINTS) {
      assert.equal(snapCandidate(p - SNAP_ZONE), p, `${p - SNAP_ZONE} should stick to ${p}`);
      assert.equal(snapCandidate(p + SNAP_ZONE), p, `${p + SNAP_ZONE} should stick to ${p}`);
      assert.equal(snapCandidate(p - 1), p);
      assert.equal(snapCandidate(p + 1), p);
    }
  });

  it("ignores just outside the zone - the drag stays free there", () => {
    for (const p of SNAP_POINTS) {
      assert.equal(snapCandidate(p - SNAP_ZONE - 1), null);
      assert.equal(snapCandidate(p + SNAP_ZONE + 1), null);
    }
  });

  it("stays null through the gap between zones and at the edges", () => {
    assert.equal(snapCandidate(200), null);
    assert.equal(snapCandidate(480), null);
    assert.equal(snapCandidate(800), null);
    assert.equal(snapCandidate(0), null);
    assert.equal(snapCandidate(128), null);
  });

  it("does not clamp - far positions return null, not the nearest snap", () => {
    assert.notEqual(snapCandidate(321), 640);
    assert.notEqual(snapCandidate(600), 320);
  });

  it("zone is 15px as specified", () => {
    assert.equal(SNAP_ZONE, 15);
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