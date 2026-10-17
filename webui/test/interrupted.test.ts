// webui unit tests for the interrupted-stream marker + retry affordance:
// transport-death detection in streamChat (clean end vs abnormal end) and
// the pure transcript transitions the marker/retry are built from.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import {
  handlePayload,
  streamChat,
  lastCompleteUserIndex,
  lastUserText,
  dropLastExchange,
  hasInterruptedMarker,
  withoutMarkers,
  StreamInterruptedError,
} from "../app/composables/omega.ts";

import type { StreamHandlers, TranscriptItemLike } from "../app/composables/omega.ts";

const silent: StreamHandlers = {
  onThinking: () => {},
  onResponse: () => {},
  onToolCall: () => {},
  onToolResult: () => {},
  onStatus: () => {},
  onUsage: () => {},
};

// a response whose body streams the given SSE frames then ends cleanly
function sseResponse(frames: string[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) controller.enqueue(enc.encode(f));
      controller.close();
    },
  });
  return { ok: true, body } as unknown as Response;
}

// a response that delivers a frame, then the connection DIES mid-stream
function dyingResponse(): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(
        enc.encode('event: response_chunk\ndata: {"type":"response_chunk","content":"par"}\n\n'),
      );
      controller.error(new Error("network cut"));
    },
  });
  return { ok: true, body } as unknown as Response;
}

let savedFetch: typeof globalThis.fetch;
afterEach(() => {
  globalThis.fetch = savedFetch;
});

function stubFetch(resp: Response, captured?: unknown[]) {
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (_url: RequestInfo | URL, init?: RequestInit) => {
    if (captured) captured.push(init?.body ? JSON.parse(String(init.body)) : null);
    return resp;
  }) as typeof globalThis.fetch;
}

describe("streamChat: clean end vs transport death", () => {
  it("stream ending with agent_end resolves - clean end, NO marker", async () => {
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
        'event: stream_end\ndata: {"type":"stream_end","eval_count":3}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
    );
    await assert.doesNotReject(() =>
      streamChat([{ role: "user", content: "q" }], "s1", silent),
    );
  });

  it("stream ending WITHOUT agent_end throws StreamInterruptedError", async () => {
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"par"}\n\n',
        // connection drops after partial output: no agent_end ever arrives
      ]),
    );
    await assert.rejects(
      () => streamChat([{ role: "user", content: "q" }], "s1", silent),
      StreamInterruptedError,
    );
  });

  it("reader error mid-stream throws StreamInterruptedError", async () => {
    stubFetch(dyingResponse());
    await assert.rejects(
      () => streamChat([{ role: "user", content: "q" }], "s1", silent),
      StreamInterruptedError,
    );
  });

  it("StreamInterruptedError is a real Error", () => {
    const e = new StreamInterruptedError();
    assert.ok(e instanceof Error);
    assert.equal(e.name, "StreamInterruptedError");
  });

  it("agent payloads still throw a PLAIN error (agent error, not transport)", async () => {
    stubFetch(
      sseResponse(['event: error\ndata: {"type":"error","error":"boom"}\n\n']),
    );
    await assert.rejects(
      () => streamChat([{ role: "user", content: "q" }], "s1", silent),
      (e: unknown) => !(e instanceof StreamInterruptedError) && e instanceof Error,
    );
  });

  it("handlePayload records agent_end as the terminal event", () => {
    const state = { seenResponse: false, sawEnd: false };
    handlePayload({ type: "stream_end" }, silent, state);
    assert.equal(state.sawEnd, false);
    handlePayload({ type: "agent_end" }, silent, state);
    assert.equal(state.sawEnd, true);
  });
});

describe("last complete user message", () => {
  const transcript: TranscriptItemLike[] = [
    { kind: "message", role: "user", content: "first" },
    { kind: "thinking", content: "..." },
    { kind: "tool", name: "sh", status: "done" },
    { kind: "message", role: "assistant", content: "pre" },
    { kind: "message", role: "user", content: "retry me" },
    { kind: "thinking", content: "hm" },
    { kind: "message", role: "assistant", content: "partial out" },
    { kind: "interrupted" },
  ];

  it("finds the LAST user message, skipping markers and assistant items", () => {
    assert.equal(lastCompleteUserIndex(transcript), 4);
    assert.equal(lastUserText(transcript), "retry me");
  });

  it("null when the transcript has no user message", () => {
    assert.equal(lastUserText([{ kind: "message", role: "assistant", content: "x" }]), null);
    assert.equal(lastCompleteUserIndex([]), -1);
  });
});

describe("retry transcript transitions", () => {
  const transcript: TranscriptItemLike[] = [
    { kind: "message", role: "user", content: "old q" },
    { kind: "message", role: "assistant", content: "old a" },
    { kind: "message", role: "user", content: "kick off" },
    { kind: "thinking", content: "hm" },
    { kind: "tool", name: "sh", status: "done" },
    { kind: "message", role: "assistant", content: "partial" },
    { kind: "interrupted" },
  ];

  it("abnormal end: marker pushed, transcript otherwise intact", () => {
    const st = [
      { kind: "message", role: "user", content: "kick off" },
      { kind: "message", role: "assistant", content: "partial" },
    ];
    st.push({ kind: "interrupted" });
    assert.equal(hasInterruptedMarker(st), true);
    assert.equal(st.length, 3);
    assert.deepEqual(st[1], { kind: "message", role: "assistant", content: "partial" });
  });

  it("clean end: no marker in the transcript", () => {
    const st = [
      { kind: "message", role: "user", content: "q" },
      { kind: "message", role: "assistant", content: "full a" },
    ];
    assert.equal(hasInterruptedMarker(st), false);
  });

  it("retry drops the exchange (user msg + tail) and re-sends its text", () => {
    const text = lastUserText(transcript);
    assert.equal(text, "kick off");
    const after = dropLastExchange(transcript);
    // everything the failed run produced is gone, including its opening
    // user message (the send path pushes it back - no duplicate bubble)
    assert.equal(hasInterruptedMarker(after), false);
    assert.deepEqual(
      after.map((i) => [i.kind, i.role, i.content]),
      [
        ["message", "user", "old q"],
        ["message", "assistant", "old a"],
      ],
    );
  });

  it("retry without a user message leaves the transcript unchanged", () => {
    const st: TranscriptItemLike[] = [
      { kind: "message", role: "assistant", content: "x" },
      { kind: "interrupted" },
    ];
    const after = dropLastExchange(st);
    assert.equal(after, st);
  });

  it("markers only: withoutMarkers strips them, keeping order", () => {
    const st: TranscriptItemLike[] = [
      { kind: "message", role: "user", content: "q" },
      { kind: "interrupted" },
      { kind: "message", role: "assistant", content: "a" },
      { kind: "interrupted" },
    ];
    const after = withoutMarkers(st);
    assert.deepEqual(
      after,
      [
        { kind: "message", role: "user", content: "q" },
        { kind: "message", role: "assistant", content: "a" },
      ].map((i) => i as TranscriptItemLike),
    );
  });
});