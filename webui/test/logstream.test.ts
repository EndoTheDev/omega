// omega logs tab: unit tests for the pushed tail's pane-side shim (card
// t_0e7bc0bb). The shell owns the socket; this module owns the two
// window-event directions and the merge that folds a delta onto the
// snapshot. The merge is where a duplicate or a lost line would show up,
// so it is pinned here rather than hoped at in a browser.
// Run: node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  LOG_STREAM_EVENT,
  LOG_SUBSCRIBE_EVENT,
  MAX_LINES,
  mergeTail,
  type LogStreamDetail,
} from "../../api/http_channel/ui/logstream.ts";

test("the pane's event names are the documented wire strings", () => {
  assert.equal(LOG_SUBSCRIBE_EVENT, "omega-logs-subscribe");
  assert.equal(LOG_STREAM_EVENT, "omega-log-stream");
  assert.ok(MAX_LINES >= 200, "the buffer holds at least one REST snapshot");
});

test("an ordinary delta appends - nothing is dropped", () => {
  const base = ["a", "b"];
  assert.deepEqual(mergeTail(base, ["c", "d"]), ["a", "b", "c", "d"]);
  // the base is not mutated (the caller assigns the result)
  assert.deepEqual(base, ["a", "b"]);
});

test("the snapshot/live overlap is folded once, not rendered twice", () => {
  // subscribe-first mount: a line written during the REST read is in
  // both the snapshot and the buffered push
  assert.deepEqual(mergeTail(["x", "a", "b"], ["b", "c"]), ["x", "a", "b", "c"]);
  assert.deepEqual(mergeTail(["x", "a"], ["x", "a", "b"]), ["x", "a", "b"]);
  // no shared run: a plain append (the same line twice in the log is a
  // real repeat, not an overlap to swallow)
  assert.deepEqual(mergeTail(["x", "a"], ["x"]), ["x", "a", "x"]);
});

test("an empty push leaves the tail alone", () => {
  assert.deepEqual(mergeTail(["a"], []), ["a"]);
  assert.deepEqual(mergeTail([], []), []);
});

test("the buffer is trimmed from the head at MAX_LINES", () => {
  const big = Array.from({ length: MAX_LINES }, (_, i) => `line ${i}`);
  const out = mergeTail(big, ["newest"]);
  assert.equal(out.length, MAX_LINES);
  assert.equal(out[out.length - 1], "newest");
  assert.equal(out[0], "line 1", "the oldest line was trimmed");
});

test("the shell's stream detail shape carries either lines or a reset", () => {
  const append: LogStreamDetail = { lines: ["one"] };
  const reset: LogStreamDetail = { reset: true };
  assert.deepEqual(Object.keys(append), ["lines"]);
  assert.deepEqual(Object.keys(reset), ["reset"]);
});
