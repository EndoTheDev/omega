// omega webui: unit tests for the session-row actions batch
// (t_6bf696e4) - branch/rename/export parity actions. Run:
// node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  commitSessionRename,
  exportSessionUrl,
  sessionRowBadge,
} from "../app/composables/omega.ts";

describe("sessionRowBadge", () => {
  it("marks parented rows with the branch badge", () => {
    assert.equal(sessionRowBadge({ parent_id: "abc" }), "↳ branch");
  });

  it("leaves plain and root rows badge-less", () => {
    assert.equal(sessionRowBadge({}), "");
    assert.equal(sessionRowBadge({ parent_id: "" }), "");
  });
});

describe("commitSessionRename", () => {
  it("commits a real edit as the trimmed label", () => {
    assert.equal(commitSessionRename("  new name  ", "old"), "new name");
  });

  it("drops no-op drafts (empty or unchanged) - no PUT", () => {
    assert.equal(commitSessionRename("   ", "old"), null);
    assert.equal(commitSessionRename("old", "old"), null);
    assert.equal(commitSessionRename("", undefined), null);
  });

  it("treats whitespace-only variants of the current label as a no-op", () => {
    assert.equal(commitSessionRename(" old ", "old"), null);
  });
});

describe("exportSessionUrl", () => {
  it("builds the /export href", () => {
    assert.equal(exportSessionUrl("a b"), "/sessions/a%20b/export");
  });
});