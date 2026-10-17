// omega webui: unit tests for the session list helpers (bucketing,
// relative age, search filter, token formatting, row meta) in
// composables/omega.ts. Run: node --test "test/*.test.ts" (node >= 23
// runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  bucketSessions,
  filterSessions,
  formatTokens,
  relativeAge,
  rowMeta,
} from "../app/composables/omega.ts";
import type { Session } from "../app/composables/omega.ts";

function sess(overrides: Partial<Session> & { id: string }): Session {
  return {
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const NOW = new Date("2026-10-17T14:00:00");

// 2026-10-17 is a Saturday; NOW is 14:00 local, midnight local is the
// today/yesterday/earlier boundary
const TODAY = "2026-10-17";
const YESTERDAY = "2026-10-16";

describe("relativeAge", () => {
  it("renders minutes, hours, days compactly", () => {
    assert.equal(relativeAge(new Date(NOW.getTime() - 35 * 60000).toISOString(), NOW), "35m");
    assert.equal(relativeAge(new Date(NOW.getTime() - 2 * 3600000).toISOString(), NOW), "2h");
    assert.equal(
      relativeAge(new Date(NOW.getTime() - 3 * 86400000).toISOString(), NOW),
      "3d",
    );
  });

  it("renders sub-minute as now, hides invalid timestamps", () => {
    assert.equal(relativeAge(new Date(NOW.getTime() - 20000).toISOString(), NOW), "now");
    assert.equal(relativeAge("not-a-date", NOW), "");
  });
});

describe("formatTokens", () => {
  it("passes small counts through, k-abbreviates above 1000", () => {
    assert.equal(formatTokens(0), "0");
    assert.equal(formatTokens(999), "999");
    assert.equal(formatTokens(1000), "1k");
    assert.equal(formatTokens(2135), "2.1k");
    assert.equal(formatTokens(12030), "12k");
  });
});

describe("filterSessions", () => {
  const sessions = [
    sess({ id: "a", label: "Database migration" }),
    sess({ id: "b", label: "Webui search box" }),
    sess({ id: "c" }),
  ];

  it("empty query matches everything", () => {
    assert.equal(filterSessions(sessions, "").length, 3);
    assert.equal(filterSessions(sessions, "   ").length, 3);
  });

  it("matches the label as a case-insensitive substring", () => {
    const hits = filterSessions(sessions, "SEARCH");
    assert.deepEqual(hits.map((s) => s.id), ["b"]);
    assert.equal(filterSessions(sessions, "data").length, 1);
    assert.equal(filterSessions(sessions, "zzz").length, 0);
  });
});

describe("bucketSessions", () => {
  it("groups by local calendar day and keeps pinned out of the buckets", () => {
    const sessions = [
      sess({ id: "old", updated_at: "2026-10-01T10:00:00" }),
      sess({ id: "yest", updated_at: `${YESTERDAY}T23:50:00` }),
      sess({ id: "yest2", updated_at: `${YESTERDAY}T08:00:00` }),
      sess({ id: "today", updated_at: `${TODAY}T09:00:00` }),
      sess({ id: "pin", updated_at: "2026-10-10T12:00:00", pinned: true }),
    ];
    const b = bucketSessions(sessions, NOW);
    // pinned session keeps its own group even though its day is "earlier"
    assert.deepEqual(b.pinned.map((s) => s.id), ["pin"]);
    assert.deepEqual(
      b.today.map((s) => s.id),
      ["today"],
    );
    // within a bucket: newest first (23:50 before 08:00)
    assert.deepEqual(
      b.yesterday.map((s) => s.id),
      ["yest", "yest2"],
    );
    assert.deepEqual(
      b.earlier.map((s) => s.id),
      ["old"],
    );
  });

  it("yesterday stays yesterday across the midnight edge of the NOW reference", () => {
    // a 23:50 session is Yesterday for BOTH 00:10 today and 13:00 today
    const edge = new Date(`${TODAY}T00:10:00`);
    const sessions = [sess({ id: "y", updated_at: `${YESTERDAY}T23:50:00` })];
    assert.deepEqual(bucketSessions(sessions, edge).yesterday.map((s) => s.id), ["y"]);
    assert.deepEqual(bucketSessions(sessions, NOW).yesterday.map((s) => s.id), ["y"]);
  });

  it("unpinning moves a session from pinned back into its day bucket", () => {
    const pinnedToday = [sess({ id: "a", updated_at: `${TODAY}T12:00:00`, pinned: true })];
    const b = bucketSessions(pinnedToday, NOW);
    assert.deepEqual(b.pinned.map((s) => s.id), ["a"]);
    assert.deepEqual(b.today.map((s) => s.id), []);
    const unpinned = pinnedToday.map((s) => ({ ...s, pinned: false }));
    const b2 = bucketSessions(unpinned, NOW);
    assert.deepEqual(b2.pinned.map((s) => s.id), []);
    assert.deepEqual(b2.today.map((s) => s.id), ["a"]);
  });

  it("malformed updated_at lands in earlier instead of crashing", () => {
    const b = bucketSessions([sess({ id: "bad", updated_at: "nope" })], NOW);
    assert.deepEqual(b.earlier.map((s) => s.id), ["bad"]);
  });
});

describe("rowMeta", () => {
  it("joins the known fields with separators, k-abbreviated tokens", () => {
    assert.equal(
      rowMeta({
        ...sess({ id: "a" }),
        model: "glm-5.3-flash",
        message_count: 12,
        tool_count: 3,
        prompt_tokens: 1500,
        completion_tokens: 635,
      }),
      "glm-5.3-flash - 12 msgs - 3 tools - 2.1k tok",
    );
  });

  it("omits unknown pieces - zero counts and no model", () => {
    assert.equal(
      rowMeta({ ...sess({ id: "a" }), message_count: 4 }),
      "4 msgs",
    );
    assert.equal(
      rowMeta({
        ...sess({ id: "a" }),
        model: "m1",
        message_count: 2,
        tool_count: 0,
        prompt_tokens: 0,
        completion_tokens: 0,
      }),
      "m1 - 2 msgs",
    );
  });

  it("renders nothing when nothing is known", () => {
    assert.equal(rowMeta(sess({ id: "a" })), "");
  });
});