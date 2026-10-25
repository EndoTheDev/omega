// webui unit tests for transport-death detection in streamChat, the
// server-side cancel path (agent_end "cancelled" reads as interrupted),
// and the stop affordance through the chat panel's action pipe
// (chat-last 2a: no transcript markers, no REST stop client side).
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import {
  handlePayload,
  streamChat,
  lastCompleteUserIndex,
  lastUserText,
  createItemKeySource,
  StreamInterruptedError,
} from "../app/composables/omega.ts";
import { createChatHost, type ChatHostDeps } from "../app/composables/chat.ts";
import type { StreamHandlers, TranscriptItemLike } from "../app/composables/omega.ts";

const silent: StreamHandlers = {
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
  it("stream ending with agent_end resolves - clean end", async () => {
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
    const state = { sawEnd: false };
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
    const state = { sawEnd: false };
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

describe("host.cancel: the stop face rides the action pipe", () => {
  function deps(over: Partial<ChatHostDeps> = {}): ChatHostDeps {
    return {
      setTabSessionRef: () => {},
      refreshSessions: () => {},
      sessionRow: () => undefined,
      markRead: () => {},
      isFocused: () => false,
      ...over,
    };
  }

  it("POSTs /ui/actions with the chat panel's stop action + the session arg", async () => {
    const calls: { url: RequestInfo | URL; body: Record<string, unknown> }[] = [];
    savedFetch = globalThis.fetch;
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url, body: JSON.parse(String(init?.body)) as Record<string, unknown> });
      return { ok: true, json: async () => ({}) } as unknown as Response;
    }) as typeof globalThis.fetch;
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.cancel("c1");
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(calls.length, 1);
    assert.equal(String(calls[0]!.url), "/ui/actions");
    assert.equal(calls[0]!.body.module, "http_channel");
    assert.equal(calls[0]!.body.action, "stop");
    assert.deepEqual(calls[0]!.body.args, { session: "s1" });
  });

  it("an unsaved tab (no session binding yet) fires nothing", async () => {
    const calls: unknown[] = [];
    savedFetch = globalThis.fetch;
    globalThis.fetch = (async (..._args: unknown[]) => {
      calls.push(_args);
      return { ok: true } as unknown as Response;
    }) as typeof globalThis.fetch;
    const host = createChatHost(deps());
    host.ensureState("c1");
    host.cancel("c1");
    await new Promise((r) => setTimeout(r, 0));
    assert.deepEqual(calls, []);
  });

  it("a failed pipe stop surfaces in the status line, never a throw", async () => {
    savedFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
      ({ ok: false, status: 500, text: async () => "session not active" }) as unknown as Response) as typeof globalThis.fetch;
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.cancel("c1");
    await new Promise((r) => setTimeout(r, 0));
    assert.match(host.states.get("c1")!.statusLine, /stop failed/);
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
  ];

  it("finds the LAST user message, skipping assistant items and blocks", () => {
    assert.equal(lastCompleteUserIndex(transcript), 4);
    assert.equal(lastUserText(transcript), "retry me");
  });

  it("null when the transcript has no user message", () => {
    assert.equal(lastUserText([{ kind: "message", role: "assistant", content: "x" }]), null);
    assert.equal(lastCompleteUserIndex([]), -1);
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
});