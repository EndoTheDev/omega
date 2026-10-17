import { test } from "node:test";
import assert from "node:assert/strict";

import {
  sessionRowStatus,
  type Session,
} from "../app/composables/omega.ts";

// helper builds a session row with only the fields the selector reads
function row(
  overrides: Partial<Session> = {},
): Session {
  return {
    id: "s1",
    created_at: "2026-10-17T10:00:00Z",
    updated_at: "2026-10-17T10:00:00Z",
    ...overrides,
  };
}

const NO_STREAM = new Set<string>();
const STREAM_S1 = new Set(["s1"]);

// --- shape channel: pinned owns the shape, hover is the affordance ---

test("unpinned idle row shows a dot", () => {
  const st = sessionRowStatus(row(), NO_STREAM, false, null);
  assert.equal(st.shape, "dot");
  assert.equal(st.color, "idle");
});

test("pinned row always shows a star, even unhovered", () => {
  const st = sessionRowStatus(row({ pinned: true }), NO_STREAM, false, null);
  assert.equal(st.shape, "star");
});

test("hovering an unpinned row morphs the dot into a star", () => {
  const st = sessionRowStatus(row(), NO_STREAM, true, null);
  assert.equal(st.shape, "star");
});

test("hovering a pinned row keeps the star (no downgrade)", () => {
  const st = sessionRowStatus(row({ pinned: true }), NO_STREAM, true, null);
  assert.equal(st.shape, "star");
});

// --- color channel: streaming > unread > idle ---

test("unread row turns blue while idle", () => {
  const st = sessionRowStatus(row({ unread: true }), NO_STREAM, false, null);
  assert.equal(st.color, "unread");
  assert.equal(st.shape, "dot");
});

test("pinned + unread is a blue star", () => {
  const st = sessionRowStatus(
    row({ pinned: true, unread: true }),
    NO_STREAM,
    false,
    null,
  );
  assert.equal(st.shape, "star");
  assert.equal(st.color, "unread");
});

test("streaming wins over unread", () => {
  const st = sessionRowStatus(
    row({ unread: true }),
    STREAM_S1,
    false,
    null,
  );
  assert.equal(st.color, "streaming");
});

test("streaming wins over unread regardless of shape", () => {
  const st = sessionRowStatus(
    row({ pinned: true, unread: true }),
    STREAM_S1,
    false,
    null,
  );
  assert.equal(st.shape, "star");
  assert.equal(st.color, "streaming");
});

test("hover-star on a streaming row stays streaming orange", () => {
  const st = sessionRowStatus(row(), STREAM_S1, true, null);
  assert.equal(st.shape, "star");
  assert.equal(st.color, "streaming");
});

// --- the viewed session never reads unread ---

test("the active session suppresses unread (the open is the read)", () => {
  const st = sessionRowStatus(
    row({ unread: true }),
    NO_STREAM,
    false,
    "s1",
  );
  assert.equal(st.color, "idle");
});

test("another session's id does not suppress unread", () => {
  const st = sessionRowStatus(
    row({ unread: true }),
    NO_STREAM,
    false,
    "other",
  );
  assert.equal(st.color, "unread");
});

test("the active session still pulses while streaming", () => {
  const st = sessionRowStatus(
    row({ unread: true }),
    STREAM_S1,
    false,
    "s1",
  );
  assert.equal(st.color, "streaming");
});

// --- missing fields behave as neutral defaults ---

test("a session without unread or pinned defaults to grey dot", () => {
  const st = sessionRowStatus(
    { id: "s1" } as Session,
    NO_STREAM,
    false,
    null,
  );
  assert.equal(st.shape, "dot");
  assert.equal(st.color, "idle");
});