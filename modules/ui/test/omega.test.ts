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

  // card t_692254f7 fix B: the materialize path needs the call ids -
  // tool_call frames carry their id, tool_result frames their
  // tool_call_id, both ride the same normalized callbacks
  it("tool_call and tool_result ids reach the handlers", () => {
    const got: { callId?: string; resultId?: string } = {};
    handlePayload(
      { type: "tool_call", tool_call: { id: "tc9", name: "shell.run", arguments: {} } },
      {
        onThinking: () => {},
        onResponse: () => {},
        onToolCall: (t) => {
          got.callId = t.id;
        },
        onToolResult: (r) => {
          got.resultId = r.toolCallId;
        },
        onStatus: () => {},
        onUsage: () => {},
      },
      { seenResponse: true },
    );
    assert.equal(got.callId, "tc9");
    handlePayload(
      { type: "tool_result", message: { content: "out", tool_call_id: "tc9" } },
      {
        onThinking: () => {},
        onResponse: () => {},
        onToolCall: () => {},
        onToolResult: (r) => {
          got.resultId = r.toolCallId;
        },
        onStatus: () => {},
        onUsage: () => {},
      },
      { seenResponse: true },
    );
    assert.equal(got.resultId, "tc9");
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

import {
  THINKING_LEVELS,
  THINKING_UI_LEVELS,
  thinkingLevelWire,
  createElapsedMeter,
} from "../app/composables/omega.ts";

describe("thinkingLevelWire", () => {
  it("UI levels mirror the go ladder with 'default' replacing 'none'", () => {
    assert.deepEqual(
      [...THINKING_UI_LEVELS],
      ["default", ...THINKING_LEVELS.slice(1)],
    );
    assert.equal(THINKING_LEVELS.length, 10);
  });

  it("maps the selection to the wire index", () => {
    assert.equal(thinkingLevelWire("default"), undefined);
    assert.equal(thinkingLevelWire("none"), 0); // backend skips 0: no param
    assert.equal(thinkingLevelWire("off"), 1);
    assert.equal(thinkingLevelWire("medium"), 5);
    assert.equal(thinkingLevelWire("ultra"), 9);
  });

  it("unknown selections fall back to unset (provider default)", () => {
    assert.equal(thinkingLevelWire(""), undefined);
    assert.equal(thinkingLevelWire("bogus"), undefined);
  });
});

// the reattach merge's restored running tool counts on from the
// server's elapsed-so-far instead of restarting the call's clock
// (card t_692254f7). Manual fake clock: one stored tick callback.
describe("createElapsedMeter initialMs (restored running call offset)", () => {
  function fakeMeter(offset: number) {
    const labels: string[] = [];
    let now = 0;
    let fire: (() => void) | null = null;
    const meter = createElapsedMeter(
      (l) => labels.push(l),
      () => now,
      { set: (fn) => { fire = fn; return 0; }, clear: () => { fire = null; } },
      offset,
    );
    return {
      meter,
      labels,
      tick(ms: number) {
        now += ms;
        const f = fire;
        fire = null;
        f?.();
      },
    };
  }

  it("starts the label at the consumed span and ticks on from it", () => {
    // wall clock at 0, call started 7s ago: the restored tool has
    // already burned 7s before this meter ever mounted
    const t = fakeMeter(7000);
    t.meter.start();
    assert.deepEqual(t.labels, ["7s"], "first emit is the consumed span");
    t.tick(1000);
    t.tick(1000);
    assert.deepEqual(t.labels, ["7s", "8s", "9s"]);
  });

  it("zero offset keeps the plain meter contract", () => {
    const t = fakeMeter(0);
    t.meter.start();
    assert.deepEqual(t.labels, ["0ms"]);
    t.tick(1000);
    assert.deepEqual(t.labels, ["0ms", "1s"]);
  });
});
