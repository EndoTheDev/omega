// omega logs tab: unit tests for the pushed tail's pane-side shim (card
// t_0e7bc0bb). The shell owns the socket; this module owns the two
// window-event directions and the merge that folds a delta onto the
// snapshot. The merge is where a duplicate or a lost line would show up,
// so it is pinned here rather than hoped at in a browser.
// Run: node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  LOG_STREAM_EVENT,
  LOG_SUBSCRIBE_EVENT,
  MAX_LINES,
  SNAPSHOT_OVERLAP,
  mergeTail,
  type LogStreamDetail,
} from "../../api/http_channel/ui/logstream.ts";

// the pane owns the call sites: the merge's positional bound is only as
// good as the argument the pane hands it, so the wiring is pinned too
const paneSource = () =>
  readFileSync(
    resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "api", "http_channel", "ui", "LogsList.vue"),
    "utf8",
  );

test("the pane's event names are the documented wire strings", () => {
  assert.equal(LOG_SUBSCRIBE_EVENT, "omega-logs-subscribe");
  assert.equal(LOG_STREAM_EVENT, "omega-log-stream");
  assert.ok(MAX_LINES >= 200, "the buffer holds at least one REST snapshot");
  assert.ok(SNAPSHOT_OVERLAP > 0, "the snapshot merge's overlap window is open");
});

test("an ordinary delta appends - nothing is dropped", () => {
  const base = ["a", "b"];
  assert.deepEqual(mergeTail(base, ["c", "d"]), ["a", "b", "c", "d"]);
  // the base is not mutated (the caller assigns the result)
  assert.deepEqual(base, ["a", "b"]);
});

test("the snapshot/live overlap is folded once, not rendered twice", () => {
  // subscribe-first mount: a line written during the REST read is in
  // both the snapshot and the buffered push (the window is open here and
  // only here - see the positional bound on mergeTail)
  assert.deepEqual(mergeTail(["x", "a", "b"], ["b", "c"], MAX_LINES, SNAPSHOT_OVERLAP), ["x", "a", "b", "c"]);
  assert.deepEqual(mergeTail(["x", "a"], ["x", "a", "b"], MAX_LINES, SNAPSHOT_OVERLAP), ["x", "a", "b"]);
  // no shared run: a plain append (the same line twice in the log is a
  // real repeat, not an overlap to swallow)
  assert.deepEqual(mergeTail(["x", "a"], ["x"], MAX_LINES, SNAPSHOT_OVERLAP), ["x", "a", "x"]);
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

test("a delta frame never overlaps the tail - a repeated line survives", () => {
  // The tailer publishes byte-offset deltas (api/server_inspector.go
  // pushLogDelta): frame N and frame N+1 are disjoint, so a line the log
  // printed twice in a row is TWO lines, not an overlap to swallow.
  const tail = mergeTail(mergeTail(["boot"], ["tick"]), ["tick"]);
  assert.deepEqual(tail, ["boot", "tick", "tick"]);
});

test("a repeated run spanning the frame join stays whole", () => {
  // a loop printing the same warning: the join must not collapse it
  const tail = mergeTail(mergeTail(["boot"], ["E", "E"]), ["E", "E", "done"]);
  assert.deepEqual(tail, ["boot", "E", "E", "E", "E", "done"]);

  // a block the log legitimately printed twice across the join
  const block = mergeTail(["boot", "t0", "t1"], ["t0", "t1", "t2"]);
  assert.deepEqual(block, ["boot", "t0", "t1", "t0", "t1", "t2"]);
});

test("the same lines are an overlap at the snapshot join and an append on a delta", () => {
  // the whole point of a POSITIONAL bound: content cannot tell a duplicate
  // from a repeat, the merge's place in the stream can
  assert.deepEqual(mergeTail(["x", "a", "b"], ["b", "c"], MAX_LINES, SNAPSHOT_OVERLAP), ["x", "a", "b", "c"]);
  assert.deepEqual(mergeTail(["x", "a", "b"], ["b", "c"]), ["x", "a", "b", "b", "c"]);
});

test("the pane opens the overlap window only on the post-read fold", () => {
  const source = paneSource();
  // both load() folds are snapshot joins and pass the open window
  assert.equal(
    source.match(/mergeTail\([^;]*SNAPSHOT_OVERLAP\)/g)?.length,
    2,
    "both post-read folds pass the snapshot window",
  );
  // the live frame path appends at the default bound of zero - a repeated
  // line there is real and must never be dropped
  assert.match(source, /lines\.value = mergeTail\(lines\.value, incoming\);/);
});

test("the shell's stream detail shape carries either lines or a reset", () => {
  const append: LogStreamDetail = { lines: ["one"] };
  const reset: LogStreamDetail = { reset: true };
  assert.deepEqual(Object.keys(append), ["lines"]);
  assert.deepEqual(Object.keys(reset), ["reset"]);
});
