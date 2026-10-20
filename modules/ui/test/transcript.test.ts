// omega webui: unit tests for the transcript RESTORE path - the pure
// mapping from GET /sessions/{id}/transcript wire items into the items
// the chat renderer draws (the same shapes the live SSE stream paints;
// no reload-side reconstruction). Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
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
      { kind: "tool_result", tool_call_id: "tc1", content: "PASS", elapsed_ms: 3000 },
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
});