// omega webui: unit tests for the auto-create contract (Endo
// 2026-10-18: POST /chat without session_id IS a new session, the
// create-then-send dance is dead). Covers the wire half (omega.ts:
// bare body, the leading `event: session` frame names the created id)
// and the host half (chat.ts submit binds the arriving id onto the
// state + the dock tree; the conversation itself paints through the
// canvas chat panel's deltas - no transcript is built here).
// Run: node --test test/
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import {
  streamChat,
  type StreamHandlers,
} from "../app/composables/omega.ts";

import {
  createChatHost,
  type ChatHostDeps,
} from "../app/composables/chat.ts";

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

describe("streamChat: the auto-create wire half", () => {
  it("null session_id POSTs a bare body; the session frame names the created id", async () => {
    const captured: unknown[] = [];
    const seen: string[] = [];
    stubFetch(
      sseResponse([
        'event: session\ndata: {"id":"sess-created"}\n\n',
        'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
      captured,
    );
    const handlers: StreamHandlers = {
      ...silent,
      onSession: (id) => seen.push(`session:${id}`),
    };
    await assert.doesNotReject(() =>
      streamChat([{ role: "user", content: "q" }], null, handlers),
    );
    assert.equal(captured.length, 1);
    assert.equal("session_id" in (captured[0] as object), false, "bare body, no session_id");
    assert.deepEqual(seen, ["session:sess-created"], "the session frame binds, render frames are canvas-only now");
  });

  it("a session-keyed send carries session_id and never sees session frames", async () => {
    const captured: unknown[] = [];
    const seen: string[] = [];
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
      captured,
    );
    const handlers: StreamHandlers = {
      ...silent,
      onSession: (id) => seen.push(`session:${id}`),
    };
    await assert.doesNotReject(() =>
      streamChat([{ role: "user", content: "q" }], "s1", handlers),
    );
    assert.equal((captured[0] as { session_id: string }).session_id, "s1");
    assert.deepEqual(seen, []);
  });

  it("a session frame without an onSession handler is skipped harmlessly", async () => {
    stubFetch(
      sseResponse([
        'event: session\ndata: {"id":"sess-x"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
    );
    await assert.doesNotReject(() => streamChat([{ role: "user", content: "q" }], null, silent));
  });
});

describe("chat host: auto-create bind half", () => {
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

  it("submit on an unsaved tab binds the arriving id (state + dock tree)", async () => {
    const bound: Array<[string, string]> = [];
    // a fake /chat for the auto-create: id arrives BEFORE the run frames
    stubFetch(
      sseResponse([
        'event: session\ndata: {"id":"sess-created"}\n\n',
        'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
    );
    const host = createChatHost(deps({
      setTabSessionRef: (key, sessionRef) => bound.push([key, sessionRef ?? ""]),
    }));
    const ok = await host.submit("c1", "hello", "glm-5.3");
    assert.equal(ok, true);
    const st = host.states.get("c1")!;
    assert.equal(st.sessionId, "sess-created", "the state binds the created id");
    assert.deepEqual(bound, [["c1", "sess-created"]], "the dock tree binds it too");
    // the canvas panel paints the conversation - the host builds none
    assert.equal(st.restored.length, 0);
  });

  it("submit on a SAVED tab still POSTs session_id and binds nothing new", async () => {
    const bound: Array<[string, string]> = [];
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
    );
    const host = createChatHost(deps({
      setTabSessionRef: (key, sessionRef) => bound.push([key, sessionRef ?? ""]),
    }));
    host.ensureState("c1", "s1");
    const ok = await host.submit("c1", "again", "glm-5.3");
    assert.equal(ok, true);
    assert.deepEqual(bound, [], "no rebinding for a session-keyed send");
    assert.equal(host.states.get("c1")!.sessionId, "s1");
  });
});