// omega webui: unit tests for the URL<->session route helpers in
// composables/omega.ts (parseSessionPath / sessionPath, pure functions).
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  parseSessionPath,
  sessionPath,
} from "../app/composables/omega.ts";

describe("parseSessionPath", () => {
  it("extracts the id from a /chat/<id> path", () => {
    assert.equal(parseSessionPath("/chat/abc-123"), "abc-123");
  });

  it("returns null for everything that is not /chat/<id>", () => {
    assert.equal(parseSessionPath("/"), null);
    assert.equal(parseSessionPath(""), null);
    assert.equal(parseSessionPath("/chat"), null);
    assert.equal(parseSessionPath("/sessions/s1"), null);
    assert.equal(parseSessionPath("/_nuxt/app.js"), null);
  });

  it("rejects empty ids and nested paths", () => {
    assert.equal(parseSessionPath("/chat/"), null);
    assert.equal(parseSessionPath("/chat/a/b"), null);
  });
});

describe("sessionPath", () => {
  it("builds the /chat/<id> URL", () => {
    assert.equal(sessionPath("abc-123"), "/chat/abc-123");
  });

  it("round-trips with parseSessionPath", () => {
    assert.equal(parseSessionPath(sessionPath("abc-123")), "abc-123");
  });
});
