// omega webui: unit tests for the per-session stream busy map (the pure
// busy logic in composables/omega.ts). Run: node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { anyBusy, isBusy, withBusy } from "../app/composables/omega.ts";

describe("per-session busy map", () => {
  it("busy(A) does not affect session B", () => {
    const b = withBusy(new Set(), "A", true);
    assert.equal(isBusy(b, "A"), true);
    assert.equal(isBusy(b, "B"), false);
    assert.equal(anyBusy(b), true);
  });

  it("A and B stream concurrently; stopping A leaves B busy", () => {
    let b = withBusy(new Set(), "A", true);
    b = withBusy(b, "B", true);
    assert.equal(isBusy(b, "A"), true);
    assert.equal(isBusy(b, "B"), true);
    assert.equal(anyBusy(b), true);
    b = withBusy(b, "A", false);
    assert.equal(isBusy(b, "A"), false);
    assert.equal(isBusy(b, "B"), true);
    assert.equal(anyBusy(b), true);
  });

  it("spinner derives from any-busy: false once every stream settles", () => {
    let b = withBusy(new Set(), "A", true);
    b = withBusy(b, "B", true);
    b = withBusy(b, "A", false);
    assert.equal(anyBusy(b), true);
    b = withBusy(b, "B", false);
    assert.equal(anyBusy(b), false);
  });

  it("re-busy and double-stop are idempotent", () => {
    let b = withBusy(new Set(), "A", true);
    b = withBusy(b, "A", true);
    assert.equal(isBusy(b, "A"), true);
    b = withBusy(b, "A", false);
    b = withBusy(b, "A", false);
    assert.equal(isBusy(b, "A"), false);
    assert.equal(anyBusy(b), false);
  });

  it("withBusy never mutates the base set", () => {
    const base: ReadonlySet<string> = new Set(["A"]);
    withBusy(base, "B", true);
    assert.equal(isBusy(base, "B"), false);
    assert.deepEqual([...base], ["A"]);
  });
});