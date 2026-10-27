// omega webui: unit tests for the ephemeral chat contract (card
// t_d96a5cd5). The wire half: streamChat carries the ephemeral flag.
// The host half: the no-persist state binds the server-minted `eph-`
// id, never marks read (no row to read), never retries (nothing
// persisted to replay) and DELETEs the server record when the last
// bound state closes (the contract: close = gone).
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

// a recording fetch: every call lands in the log as [method, url, body?].
// respond is called per request (fresh response each time - a Response
// body can be consumed once).
type Call = [string, string, unknown?];
function recordFetch(respond: () => Response) {
  const calls: Call[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    calls.push([
      init?.method ?? "GET",
      String(url),
      init?.body ? JSON.parse(String(init.body)) : undefined,
    ]);
    return respond();
  }) as typeof globalThis.fetch;
  return calls;
}

// a 404 JSON response, for closes and misses
const notFound = (): Response =>
  ({ ok: false, status: 404, json: async () => ({}) }) as unknown as Response;

describe("streamChat: the ephemeral wire half", () => {
  it("the ephemeral flag rides the body, the session frame binds the eph id", async () => {
    const calls = recordFetch(() =>
      sseResponse([
        'event: session\ndata: {"id":"eph-abc"}\n\n',
        'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
    );
    await streamChat([{ role: "user", content: "q" }], null, silent, undefined, undefined, undefined, true);
    assert.equal(calls.length, 1);
    const body = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(body.ephemeral, true, "the flag rides the wire");
    await streamChat([{ role: "user", content: "q" }], null, silent);
    const body2 = calls[1]?.[2] as Record<string, unknown>;
    assert.equal("ephemeral" in body2, false, "a plain send never carries the flag");
  });
});

describe("chat host: the ephemeral contract", () => {
  function deps(over: Partial<ChatHostDeps> = {}): ChatHostDeps {
    return {
      setTabSessionRef: () => {},
      refreshSessions: () => {},
      sessionRow: () => undefined,
      markRead: () => {},
      isFocused: () => true,
      ...over,
    };
  }

  function runFrames(): Response {
    return sseResponse([
      'event: session\ndata: {"id":"eph-xyz"}\n\n',
      'event: response_chunk\ndata: {"type":"response_chunk","content":"hi"}\n\n',
      'event: agent_end\ndata: {"type":"agent_end"}\n\n',
    ]);
  }

  it("an ephemeral submit binds the eph id and never marks read (no row)", async () => {
    const calls = recordFetch(runFrames);
    const reads: string[] = [];
    const host = createChatHost(deps({
      markRead: (id) => reads.push(id),
    }));
    const st = host.ensureState("c1", null, true);
    assert.equal(st.ephemeral, true);
    const ok = await host.submit("c1", "throwaway");
    assert.equal(ok, true);
    assert.equal(st.sessionId, "eph-xyz", "the server-minted eph id binds");
    const sendBody = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(sendBody.ephemeral, true, "the run rides the ephemeral flag");
    // the focused ephemeral tab STILL never sends the read-stamp: the
    // PATCH would 204 into a store write that does not exist
    assert.deepEqual(reads, [], "no read stamp for a session with no row");
  });

  it("retry never fires on an ephemeral state (nothing persisted to replay)", async () => {
    recordFetch(runFrames);
    const host = createChatHost(deps());
    host.ensureState("c1", null, true);
    await host.submit("c1", "hello");
    const sent = recordFetch(notFound);
    const ok = await host.retry("c1");
    assert.equal(ok, false, "the retry is refused client-side");
    assert.deepEqual(sent, [], "no fetch at all - not even a doomed one");
  });

  it("closing the last state bound to an eph id DELETEs the server record", async () => {
    recordFetch(runFrames);
    const host = createChatHost(deps());
    host.ensureState("c1", null, true);
    await host.submit("c1", "hello");
    assert.equal(host.states.get("c1")!.sessionId, "eph-xyz");
    const calls = recordFetch(notFound);
    host.closeState("c1");
    assert.deepEqual(
      calls.map((c) => [c[0], c[1]]),
      [["DELETE", "/sessions/eph-xyz"]],
      "close = gone: the registry record drops",
    );
  });

  it("a close of a NON-ephemeral state fires no DELETE", async () => {
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    const calls = recordFetch(notFound);
    host.closeState("c1");
    assert.deepEqual(calls, []);
  });

  it("load() reattaches an eph id from the registry transcript (reload)", async () => {
    // restore = the transcript endpoint, served from server memory:
    // the user turn + the settled answer, running:false (the run ended)
    recordFetch(() =>
      ({
        ok: true,
        json: async () => ({
          session: { id: "eph-xyz", created_at: "2026-10-22T00:00:00Z", updated_at: "2026-10-22T00:00:10Z" },
          items: [
            { kind: "user", content: "throwaway" },
            { kind: "message", content: "hi" },
          ],
          running: false,
        }),
      }) as unknown as Response,
    );
    const host = createChatHost(deps());
    host.ensureState("r1", "eph-xyz", true);
    await host.load("r1", "eph-xyz");
    const st = host.states.get("r1")!;
    assert.equal(st.loaded, true, "the in-memory transcript restored");
    assert.equal(st.restored.length, 2);
    assert.equal(st.restored[0]?.kind, "message");
    assert.equal(st.restored[1]?.kind, "message");
  });
});