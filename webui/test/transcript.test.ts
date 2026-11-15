// omega webui: unit tests for the transcript RESTORE path - the pure
// mapping from GET /sessions/{id}/transcript wire items into the items
// the chat renderer draws (the same shapes the live SSE stream paints;
// no reload-side reconstruction). Run: node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { renderTranscriptItems } from "../app/composables/omega.ts";

import type { TranscriptWireItem } from "../app/composables/omega.ts";

describe("renderTranscriptItems", () => {
  it("renders a segmented session in wire order, nothing reconstructed", () => {
    const wire: TranscriptWireItem[] = [
      { kind: "user", content: "run tests", time: "t0" },
      { kind: "thinking", content: "let me check the file", thinking_ms: 1500 },
      { kind: "tool_call", tool_call: { id: "tc1", name: "go_test", arguments: { pkg: "./..." } } },
      { kind: "tool_result", tool_call_id: "tc1", content: "PASS", elapsed_ms: 3000, answer_ms: 500 },
      { kind: "message", content: "all green", time: "t3", response_ms: 500 },
    ];
    const items = renderTranscriptItems(wire);
    // shapes: user bubble, thinking block, tool line opened then filled by
    // its result, assistant bubble - exactly the order history produced
    assert.deepEqual(
      items.map((i) => (i.kind === "tool" ? `${i.kind}:${i.name}` : i.kind)),
      ["message", "thinking", "tool:go_test", "message"],
    );
    // user carries its store timestamp for ChatMessage's hover title
    assert.equal(items[0].kind === "message" && items[0].role, "user");
    assert.equal(items[0].kind === "message" && items[0].time, "t0");
    // assistant bubble keeps the server-measured duration
    const answer = items[3];
    assert.equal(answer.kind, "message");
    if (answer.kind !== "message") return;
    assert.equal(answer.role, "assistant");
    assert.equal(answer.finalMs, 500);
    assert.equal(answer.time, "t3");
    // thinking block keeps its server-measured span
    const think = items[1];
    assert.equal(think.kind, "thinking");
    if (think.kind !== "thinking") return;
    assert.equal(think.finalMs, 1500);
  });

  it("a tool row carries BOTH durations: the call and the response return (card t_e27c8347)", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tcA", name: "go_test", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tcA", content: "PASS", elapsed_ms: 2100, answer_ms: 14300 },
      { kind: "message", content: "all green", response_ms: 14300 },
    ]);
    const tool = items[0];
    assert.ok(tool.kind === "tool");
    if (tool.kind !== "tool") return;
    assert.equal(tool.finalMs, 2100, "the call window (dispatch -> result)");
    assert.equal(tool.answerMs, 14300, "the response-return window (result -> answer complete)");
  });

  it("a result with no answer window leaves answerMs absent (the answer never came)", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tcB", name: "shell.run", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tcB", content: "out", elapsed_ms: 25 },
    ]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].finalMs, 25);
    assert.equal(items[0].answerMs, undefined);
  });

  it("tool_result merges onto the call like a live frame", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc9", name: "shell.run", arguments: { cmd: "ls" } } },
      { kind: "tool_call", tool_call: { id: "tc10", name: "files.read", arguments: undefined } },
      { kind: "tool_result", tool_call_id: "tc9", content: "out", elapsed_ms: 25 },
      { kind: "tool_result", tool_call_id: "tc11", content: "orphan" },
      { kind: "tool_result", tool_call_id: "tc12", content: "boom", is_error: true, elapsed_ms: 9 },
    ]);
    // one line per call, in call order; an unmatched result renders nothing
    assert.deepEqual(items.map((i) => i.kind), ["tool", "tool"]);
    const a = items[0];
    assert.ok(a.kind === "tool");
    if (a.kind !== "tool") return;
    assert.equal(a.name, "shell.run");
    assert.equal(a.status, "done");
    assert.equal(a.result, "out");
    assert.equal(a.finalMs, 25);
    if (a.kind === "tool" && a.output !== undefined) {
      assert.equal(JSON.parse(a.output).cmd, "ls");
    }
    const b = items[1];
    if (b.kind !== "tool") return;
    // call without arguments: no output block, result never arrived ->
    // status stays done with an empty result (dead result is the
    // interrupted-marker's business, restore never fabricates it)
    assert.equal(b.output, undefined);
    assert.equal(b.finalMs, undefined);
  });

  it("error result flags the line, not a separate item", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tcX", name: "files.write", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tcX", content: "denied", is_error: true, elapsed_ms: 2 },
    ]);
    assert.equal(items.length, 1);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].status, "error");
    assert.equal(items[0].result, "denied");
  });

  it("thin items render nothing; empty payload yields an empty list", () => {
    assert.deepEqual(renderTranscriptItems([]), []);
    assert.deepEqual(renderTranscriptItems(null), []);
    assert.deepEqual(renderTranscriptItems(undefined), []);
    const items = renderTranscriptItems([
      { kind: "user", content: "" },
      { kind: "message", content: "" },
      { kind: "thinking", content: "" },
      { kind: "message", content: "real" },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["message"]);
    assert.equal(items[0].kind === "message" && items[0].role, "assistant");
  });

  it("call without an id still renders its line (never lost)", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "", name: "mystery" } },
      { kind: "tool_result", tool_call_id: "", content: "no id, never attached" },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["tool"]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].name, "mystery");
  });

  // the live-run merge (card t_692254f7 fix A): the run's in-flight
  // items carry `running` and render as the shapes the live stream
  // paints - one contract, two sources.
  it("a running tool_call renders the live line with its elapsed offset", () => {
    const items = renderTranscriptItems([
      {
        kind: "tool_call",
        tool_call: { id: "tc1", name: "shell.run", arguments: { cmd: "sleep 30" } },
        running: true,
        elapsed_ms: 7000,
      },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["tool"]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].status, "running");
    assert.equal(items[0].elapsedMs, 7000);
    assert.equal(items[0].finalMs, undefined, "no final duration while the call runs");
    // a set result flag flips the line terminal even without the hint
    const settled = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc2", name: "sh" }, running: true },
      { kind: "tool_result", tool_call_id: "tc2", content: "out" },
    ]);
    if (settled[0].kind !== "tool") return;
    assert.equal(settled[0].status, "done");
    assert.equal(settled[0].elapsedMs, undefined);
  });

  it("a pending call meters from its started_at, not from the restore's build instant", () => {
    const started = new Date(Date.now() - 9_000).toISOString();
    const now = Date.now();
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run" }, elapsed_ms: 300, started_at: started },
    ]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].status, "pending");
    assert.ok(
      items[0].elapsedMs !== undefined && items[0].elapsedMs >= 8_500 && items[0].elapsedMs <= Date.now() - now + 9_500,
      `the restored row anchors on the call's start (got ${items[0].elapsedMs})`,
    );
    // an answered call never carries a live offset - its result's final
    // duration is the row's number
    const settled = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run" }, started_at: started },
      { kind: "tool_result", tool_call_id: "tc1", content: "done", elapsed_ms: 9_100 },
    ]);
    if (settled[0].kind !== "tool") return;
    assert.equal(settled[0].status, "done");
    assert.equal(settled[0].elapsedMs, undefined);
    assert.equal(settled[0].finalMs, 9_100);
  });

  it("running thinking and message blocks render streaming (the attach adopts them)", () => {
    const items = renderTranscriptItems([
      { kind: "message", content: "pre", time: "t0", response_ms: 100 },
      { kind: "thinking", content: "still going", running: true },
      { kind: "message", content: "partial answer", running: true },
    ]);
    if (items[0].kind !== "message") return;
    assert.equal(items[0].streaming, undefined, "a persisted segment mounts final");
    const think = items[1];
    if (think.kind !== "thinking") return assert.fail("items[1] not thinking");
    assert.equal(think.streaming, true, "open thinking streams from the restored item");
    const bubble = items[2];
    if (bubble.kind !== "message") return assert.fail("items[2] not message");
    assert.equal(bubble.streaming, true, "open response block streams from the restored item");
  });

  // card t_95385aba: the streaming segment's elapsed-so-far is the
  // meter's anchor - a remount (tab switch / reload) counts on from it
  // instead of restarting the answer's clock at zero. Persisted rows
  // never carry it: they mount final.
  it("a running thinking/answer row carries the elapsed-so-far it counts on from", () => {
    const items = renderTranscriptItems([
      { kind: "thinking", content: "still going", running: true, elapsed_ms: 8400 },
      { kind: "message", content: "partial answer", running: true, elapsed_ms: 2500 },
      { kind: "message", content: "settled answer", response_ms: 4100, elapsed_ms: 9999 },
    ]);
    const think = items[0];
    if (think.kind !== "thinking") return assert.fail("items[0] not thinking");
    assert.equal(think.elapsedMs, 8400, "the thinking meter's anchor");
    assert.equal(think.finalMs, undefined, "a live phase has no final span");
    const live = items[1];
    if (live.kind !== "message") return assert.fail("items[1] not message");
    assert.equal(live.elapsedMs, 2500, "the answer meter's anchor");
    const settled = items[2];
    if (settled.kind !== "message") return assert.fail("items[2] not message");
    assert.equal(settled.elapsedMs, undefined, "a persisted answer ignores the anchor");
    assert.equal(settled.finalMs, 4100, "the server's measured span wins");
  });

  // card t_484cbb27: a segment a tool call ended ships SETTLED on the
  // live tail too - no running bit, its measured span in
  // response_ms/thinking_ms - so a remount mid-call paints the frozen
  // label instead of a meter ticking off a closed window, and the
  // landing turn has nothing to snap. No client change is needed: the
  // item shape rides the same two fields a persisted row already uses.
  it("a tool call that ended the segment settles its text rows on the live tail", () => {
    const items = renderTranscriptItems([
      { kind: "thinking", content: "considered", thinking_ms: 2200 },
      { kind: "message", content: "calling the checker", response_ms: 900 },
      { kind: "tool_call", tool_call: { id: "c1", name: "shell.run" }, running: true, elapsed_ms: 5000 },
    ]);
    const think = items[0];
    if (think.kind !== "thinking") return assert.fail("items[0] not thinking");
    assert.equal(think.streaming, undefined, "a finished thinking row never streams");
    assert.equal(think.elapsedMs, undefined, "and carries no meter anchor");
    assert.equal(think.finalMs, 2200, "it carries its measured span");
    const answer = items[1];
    if (answer.kind !== "message") return assert.fail("items[1] not message");
    assert.equal(answer.streaming, undefined, "a finished answer row never streams");
    assert.equal(answer.elapsedMs, undefined, "and carries no meter anchor");
    assert.equal(answer.finalMs, 900, "it carries its measured span");
    const call = items[2];
    if (call.kind !== "tool") return assert.fail("items[2] not tool");
    assert.equal(call.status, "running", "the call itself keeps streaming");
    assert.equal(call.elapsedMs, 5000);
  });
});
