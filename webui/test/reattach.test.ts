// webui unit tests for the stream-reattach surface (card t_2f9b6110):
// attachSession subscribes to the DETACHED server-side run's live events
// (GET /sessions/<id>/events) and shares consumeSSE's frame grammar and
// clean-end rule with streamChat. Run: node --test test/
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import {
  attachSession,
  StreamInterruptedError,
} from "../app/composables/omega.ts";
import type { StreamHandlers } from "../app/composables/omega.ts";

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

function stubFetch(resp: Response, captured?: string[]) {
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL) => {
    if (captured) captured.push(String(url));
    return resp;
  }) as typeof globalThis.fetch;
}

describe("attachSession: reattach to the detached run", () => {
  it("GETs /sessions/<id>/events with the encoded session id", async () => {
    const captured: string[] = [];
    stubFetch(
      sseResponse(['event: agent_end\ndata: {"type":"agent_end"}\n\n']),
      captured,
    );
    const ok = await attachSession("sess 1", silent);
    assert.equal(ok, true);
    assert.deepEqual(captured, ["/sessions/sess%201/events"]);
  });

  it("stream ending WITH agent_end resolves - clean end, NO marker", async () => {
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"live tail"}\n\n',
        'event: agent_end\ndata: {"type":"agent_end"}\n\n',
      ]),
    );
    await assert.doesNotReject(() => attachSession("s1", silent));
  });

  it("stream ending WITHOUT agent_end throws StreamInterruptedError", async () => {
    stubFetch(
      sseResponse([
        'event: response_chunk\ndata: {"type":"response_chunk","content":"par"}\n\n',
        // connection drops after partial output: no agent_end ever arrives
      ]),
    );
    await assert.rejects(
      () => attachSession("s1", silent),
      StreamInterruptedError,
    );
  });

  it("reader error mid-stream throws StreamInterruptedError", async () => {
    stubFetch(dyingResponse());
    await assert.rejects(
      () => attachSession("s1", silent),
      StreamInterruptedError,
    );
  });

  it("404 (session not active) resolves FALSE - the caller must treat it "
    + "as 'no live run, render settled history', never as an error", async () => {
    stubFetch({ ok: false, status: 404 } as unknown as Response);
    const ok = await attachSession("s1", silent);
    assert.equal(ok, false);
  });
});