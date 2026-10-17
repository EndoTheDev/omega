// omega webui: unit tests for the SSE parser + payload dispatch (the
// pure half of the streamChat composable). Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { handlePayload, parseFrame } from "../app/composables/omega.ts";

import type { StreamHandlers } from "../app/composables/omega.ts";

// silent handlers that record every call
function recording() {
  const calls: string[] = [];
  const handlers: StreamHandlers = {
    onThinking: (c) => calls.push(`think:${c}`),
    onResponse: (c) => calls.push(`resp:${c}`),
    onToolCall: (t) => calls.push(`tool:${t.name}:${t.status}:${t.output}`),
    onToolResult: (r) =>
      calls.push(`result:${r.isError ? "err" : "ok"}:${r.content}`),
    onStatus: (s) => calls.push(`status:${s}`),
    onUsage: (u) =>
      calls.push(
        `usage:${u.promptTokens}/${u.completionTokens}/${u.cachedTokens}`,
      ),
  };
  return { handlers, calls };
}

describe("parseFrame", () => {
  it("extracts the data payload", () => {
    const p = parseFrame(
      'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}',
    );
    assert.deepEqual(p, { type: "response_chunk", content: "hi" });
  });

  it("null when no data line", () => {
    assert.equal(parseFrame("event: turn_start"), null);
  });

  it("null when data is not JSON", () => {
    assert.equal(parseFrame("data: {broken"), null);
  });
});

describe("handlePayload", () => {
  it("response chunks fire onStatus once then onResponse", () => {
    const { handlers, calls } = recording();
    const state = { seenResponse: false };
    handlePayload({ type: "response_chunk", content: "a" }, handlers, state);
    handlePayload({ type: "response_chunk", content: "b" }, handlers, state);
    assert.deepEqual(calls, ["status:responding", "resp:a", "resp:b"]);
  });

  it("thinking chunks dispatch without touching response state", () => {
    const { handlers, calls } = recording();
    const state = { seenResponse: false };
    handlePayload({ type: "thinking_chunk", content: "hm" }, handlers, state);
    assert.deepEqual(calls, ["think:hm"]);
    assert.equal(state.seenResponse, false);
  });

  it("tool_call parses the wire shape", () => {
    const { handlers, calls } = recording();
    handlePayload(
      {
        type: "tool_call",
        tool_call: {
          id: "c1",
          name: "shell.run",
          arguments: { command: "date" },
        },
      },
      handlers,
      { seenResponse: true },
    );
    assert.deepEqual(calls, [
      "status:tools",
      'tool:shell.run:running:{\n  "command": "date"\n}',
    ]);
  });

  it("tool_result carries is_error", () => {
    const { handlers, calls } = recording();
    handlePayload(
      { type: "tool_result", message: { content: "boom", is_error: true } },
      handlers,
      { seenResponse: true },
    );
    assert.deepEqual(calls, ["result:err:boom"]);
  });

  it("stream_end with usage fires onUsage", () => {
    const { handlers, calls } = recording();
    handlePayload(
      {
        type: "stream_end",
        prompt_eval_count: 10,
        eval_count: 20,
        cached_count: 4,
      },
      handlers,
      { seenResponse: true },
    );
    assert.deepEqual(calls, ["usage:10/20/4"]);
  });

  it("unknown event types are ignored", () => {
    const { handlers, calls } = recording();
    handlePayload({ type: "agent_start" }, handlers, { seenResponse: false });
    assert.deepEqual(calls, []);
  });

  it("ERROR PAYLOADS THROW - the regression test: a swallowed error is an invisible dead stream", () => {
    const { handlers } = recording();
    assert.throws(
      () =>
        handlePayload({ type: "error", error: "provider exploded" }, handlers, {
          seenResponse: false,
        }),
      /provider exploded/,
    );
  });
});
