// omega webui: unit tests for the archived-session filter logic
// (filterArchived in composables/omega.ts). Run: node --test
// "test/*.test.ts" (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  filterArchived,
  bucketSessions,
} from "../app/composables/omega.ts";
import type { Session } from "../app/composables/omega.ts";

function sess(overrides: Partial<Session> & { id: string }): Session {
  return {
    label: overrides.id,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const sessions: Session[] = [
  sess({ id: "live" }),
  sess({ id: "live2" }),
  sess({ id: "boxed", archived: true }),
];

describe("filterArchived", () => {
  it("toggle off keeps only the unarchived rows", () => {
    const ids = filterArchived(sessions, false).map((s) => s.id);
    assert.deepEqual(ids, ["live", "live2"]);
  });

  it("toggle on keeps only the archived rows", () => {
    const ids = filterArchived(sessions, true).map((s) => s.id);
    assert.deepEqual(ids, ["boxed"]);
  });

  it("missing archived flag reads as live, not archived", () => {
    const ids = filterArchived(sessions, true).map((s) => s.id);
    assert.deepEqual(ids, ["boxed"]);
    // flag-less rows never leak into the archived view
    assert.ok(!ids.includes("live"));
  });

  it("both sides of the toggle never mix", () => {
    const off = filterArchived(sessions, false);
    const on = filterArchived(sessions, true);
    for (const s of off) assert.ok(!s.archived);
    for (const s of on) assert.ok(s.archived);
  });

  it("an empty list filters to an empty list on both sides", () => {
    assert.deepEqual(filterArchived([], false), []);
    assert.deepEqual(filterArchived([], true), []);
  });

  it("unarchiving drops the row from the archived view instantly", () => {
    // the optimistic patch flips archived on the row; the archived view
    // (toggle on) no longer holds it without waiting for the refresh
    const unarchived = sessions.map((s) =>
      s.id === "boxed" ? { ...s, archived: false } : s,
    );
    assert.deepEqual(filterArchived(unarchived, true), []);
    assert.equal(filterArchived(unarchived, false).length, 3);
  });

  it("bucketing still places archived rows in their day buckets", () => {
    const NOW = new Date("2026-10-17T14:00:00");
    const b = bucketSessions([
      sess({ id: "arch", archived: true, updated_at: "2026-10-17T12:00:00" }),
    ], NOW);
    assert.deepEqual(b.today.map((s) => s.id), ["arch"]);
  });
});