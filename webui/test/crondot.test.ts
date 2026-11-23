// omega webui: the cron console row's status dot - the last fire's outcome, one word per
// color. Green = a clean fire, red = a failed one, orange = never fired (an unknown
// outcome is not a success).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { cronDotLabel, cronDotOf } from "../../harness/cron/ui/statusDot.ts";

describe("cron status dot", () => {
  it("reads the three outcomes", () => {
    assert.equal(cronDotOf({ status: "ok" }), "ok");
    assert.equal(cronDotOf({ status: "error" }), "error");
    assert.equal(cronDotOf({ status: "" }), "never");
    assert.equal(cronDotOf({}), "never", "an absent status is never-fired, not ok");
  });

  it("an unknown status is never-fired, never a fake success", () => {
    assert.equal(cronDotOf({ status: "skipped" }), "never");
  });

  it("the label says what the color claims", () => {
    assert.match(cronDotLabel({ status: "ok", last: "2026-11-23T08:00:00Z" }), /last fire ok/);
    assert.match(
      cronDotLabel({ status: "error", status_note: "provider exploded" }),
      /failed: provider exploded/,
    );
    assert.equal(cronDotLabel({ status: "" }), "never fired");
  });
});
