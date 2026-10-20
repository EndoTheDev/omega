// webui unit tests for the interrupted-stream marker + retry affordance:
// transport-death detection in streamChat, the server-side cancel path
// (agent_end "cancelled" reads as interrupted), the stopSession POST,
// and the pure transcript transitions the marker/retry are built from.
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import {
  handlePayload,
  streamChat,
  stopSession,
  lastCompleteUserIndex,
  lastUserText,
  hasInterruptedMarker,
  withoutMarkers,
  createItemKeySource,
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

describe("server-side cancel reads as interrupted", () => {
  it("agent_end finish_reason cancelled + 'context canceled' throws StreamInterruptedError", async () => {
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"par"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end","turns":1,"finish_reason":"cancelled","error":"context canceled"}\n\n',
      ]),
    );
    await assert.rejects(
      () => streamChat([{ role: "user", content: "q" }], "s1", silent),
      StreamInterruptedError,
    );
  });

  it("agent_end cancelled with a DEADLINE error does NOT throw (timeout keeps its error frame)", async () => {
    stubFetch(
      sseResponse([
        'event: agent_end\ndata: {"type":"agent_end","turns":1,"finish_reason":"cancelled","error":"context deadline exceeded"}\n\n',
      ]),
    );
    await assert.doesNotReject(() =>
      streamChat([{ role: "user", content: "q" }], "s1", silent),
    );
  });

  it("cancelled agent_end still marks the transport state sawEnd", () => {
    const state = { seenResponse: false, sawEnd: false };
    assert.throws(() =>
      handlePayload(
        { type: "agent_end", turns: 1, finish_reason: "cancelled", error: "context canceled" },
        silent,
        state,
      ),
    );
    assert.equal(state.sawEnd, true);
  });
});

describe("stopSession", () => {
  it("POSTs /sessions/<id>/stop", async () => {
    const calls: { url: RequestInfo | URL; init?: RequestInit }[] = [];
    savedFetch = globalThis.fetch;
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url, init });
      return { ok: true } as unknown as Response;
    }) as typeof globalThis.fetch;
    await stopSession("s1");
    assert.equal(calls.length, 1);
    assert.equal(String(calls[0].url), "/sessions/s1/stop");
    assert.equal(calls[0].init?.method, "POST");
  });

  it("404 (run already ended) and 409 (not started yet) stay silent", async () => {
    savedFetch = globalThis.fetch;
    for (const status of [404, 409]) {
      globalThis.fetch = (async () =>
        ({ ok: false, status }) as unknown as Response) as typeof globalThis.fetch;
      await assert.doesNotReject(() => stopSession("s1"));
    }
  });

  it("an unexpected failure throws", async () => {
    savedFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
      ({ ok: false, status: 500 }) as unknown as Response) as typeof globalThis.fetch;
    await assert.rejects(() => stopSession("s1"), /stop: 500/);
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

describe("retry transcript transitions (server replay semantics, t_357cbe9c)", () => {
  // the retry is SERVER-side now: POST /chat {"retry": true} re-runs
  // the PERSISTED last user turn - the client pushes NO user bubble,
  // performs no transcript surgery, and only retires its stale marker
  // (the fresh response appends on its own as the stream runs)
  it("retry keeps one user bubble - the client only strips the marker", () => {
    const st: TranscriptItemLike[] = [
      { kind: "message", role: "user", content: "old q" },
      { kind: "message", role: "assistant", content: "old a" },
      { kind: "message", role: "user", content: "kick off" },
      { kind: "thinking", content: "hm" },
      { kind: "tool" },
      { kind: "message", role: "assistant", content: "partial" },
      { kind: "interrupted" },
    ];
    // lastUserText is the presence check behind the affordance: text
    // exists to replay, the text itself never re-rides the wire
    assert.equal(lastUserText(st), "kick off");
    const after = withoutMarkers(st);
    // every pre-retry item survives untouched, in order, NOTHING pushed
    assert.deepEqual(
      after.map((i) => [i.kind, i.role, i.content]),
      [
        ["message", "user", "old q"],
        ["message", "assistant", "old a"],
        ["message", "user", "kick off"],
        ["thinking", undefined, "hm"],
        ["tool", undefined, undefined],
        ["message", "assistant", "partial"],
      ],
    );
    assert.equal(hasInterruptedMarker(after), false);
  });

  it("retry of a clean exchange touches nothing (no marker to retire)", () => {
    const st: TranscriptItemLike[] = [
      { kind: "message", role: "user", content: "q" },
      { kind: "message", role: "assistant", content: "a" },
    ];
    assert.equal(lastUserText(st), "q");
    assert.equal(withoutMarkers(st).length, 2);
    assert.equal(hasInterruptedMarker(st), false);
  });

  it("retry without a user message does nothing (composer hides the affordance)", () => {
    const st: TranscriptItemLike[] = [
      { kind: "message", role: "assistant", content: "x" },
      { kind: "interrupted" },
    ];
    assert.equal(lastUserText(st), null);
    assert.equal(hasInterruptedMarker(st), true);
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

// keyed v-for identity (card t_bc62c9ba round 1): index keys reused bubble
// COMPONENTS across the transcript's two identity-breaking swaps, leaking
// elapsed labels between sessions and resuming a dead attempt's timer on
// a retried tool call. Identity keys fix both - tested from the key
// source's side, which is the whole of the contract.
describe("createItemKeySource", () => {
  it("the same object always gets the same key (kept items keep their bubble)", () => {
    const keyOf = createItemKeySource();
    const a = { kind: "message" };
    const b = { kind: "tool" };
    assert.equal(keyOf(a), keyOf(a));
    assert.equal(keyOf(b), keyOf(b));
    assert.notEqual(keyOf(a), keyOf(b));
  });

  it("a rebuilt/similar object gets a FRESH key (no phantom reuse)", () => {
    const keyOf = createItemKeySource();
    const first = { kind: "message", content: "hello" };
    const second = { kind: "message", content: "hello" };
    assert.notEqual(keyOf(first), keyOf(second));
  });

  it("retryStream: kept head items keep keys, the retried exchange mounts fresh", () => {
    const keyOf = createItemKeySource();
    const head: TranscriptItemLike[] = [
      { kind: "message", role: "user", content: "old q" },
      { kind: "tool", name: "sh", status: "done" },
    ];
    const headKeys = head.map(keyOf);
    // a retry's APPENDED stream (and stripped-stale-marker rebuilds)
    // push NEW objects - each must mount as a new bubble, not reuse
    // head keys
    const retried: TranscriptItemLike = {
      kind: "tool",
      name: "sh",
      status: "running",
    };
    assert.notEqual(keyOf(retried), headKeys[0]);
    assert.notEqual(keyOf(retried), headKeys[1]);
    // and the dropped tail's key never collides with the next attempt's
    // (fresh-object keys keep increasing per source)
    assert.ok(keyOf(retried) > headKeys[1]);
  });
});