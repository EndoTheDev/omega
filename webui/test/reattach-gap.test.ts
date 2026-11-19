// webui unit tests for the reattach surface after the canvas migration.
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import { createChatHost, type ChatHostDeps } from "../app/composables/chat.ts";
import type { CanvasPanel } from "../app/composables/omega.ts";

function sse(frames: string[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) controller.enqueue(enc.encode(f));
      controller.close();
    },
  });
  return { ok: true, body } as unknown as Response;
}

// an events stream that stays open until the test releases it.
function heldStream(): { resp: Response; release: (frames: string[]) => void } {
  const enc = new TextEncoder();
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  return {
    resp: { ok: true, body } as unknown as Response,
    release(frames) {
      for (const f of frames) controller.enqueue(enc.encode(f));
      controller.close();
    },
  };
}

function deps(): ChatHostDeps {
  return {
    setTabSessionRef: () => {},
    refreshSessions: () => {},
    sessionRow: () => undefined,
    markRead: () => {},
    isFocused: () => false,
  };
}

// stub fetch with a URL router.
function stubFetch(
  detail: Record<string, unknown>,
  events: (held: { resp: Response; release: (fs: string[]) => void }) => Response,
) {
  const held = heldStream();
  const detailResp = { ok: true, json: async () => detail } as unknown as Response;
  globalThis.fetch = (async (url: RequestInfo | URL) => {
    const path = String(url);
    if (path.includes("/transcript")) return detailResp;
    return events(held) as Response;
  }) as typeof globalThis.fetch;
  return held;
}

const flush = () => new Promise((r) => setTimeout(r, 0));

let savedFetch: typeof globalThis.fetch;
afterEach(() => {
  globalThis.fetch = savedFetch;
});
savedFetch = globalThis.fetch;

describe("reattach: the serve-side merge renders on restore", () => {
  it("restore renders persisted history + the live tool row + the open thinking block", async () => {
    // the transcript endpoint composes the live run's open tail BEHIND the persisted.
    const held = stubFetch(
      {
        session: { id: "s1", updated_at: "t" },
        running: true,
        items: [
          { kind: "user", content: "count to 30", time: "t0" },
          {
            kind: "tool_call",
            tool_call: { id: "tc1", name: "shell.run", arguments: { cmd: "sleep 30" } },
            running: true,
            elapsed_ms: 7000,
          },
          { kind: "thinking", content: "reading the numbers", running: true },
        ],
      },
      (h) => h.resp,
    );
    const host = createChatHost(deps());
    const st = host.ensureState("c1", "s1");
    const loading = host.load("c1", "s1");
    await flush();
    await flush();
    assert.deepEqual(
      st.restored.map((i) => i.kind),
      ["message", "tool", "thinking"],
      "user bubble, the LIVE tool row, the open thinking block - the run is visible",
    );
    const tool = st.restored[1];
    if (tool?.kind !== "tool") return assert.fail("not a tool line");
    assert.equal(tool.name, "shell.run");
    assert.equal(tool.status, "running", "the in-flight call is running, not done");
    assert.equal(tool.elapsedMs, 7000, "the meter counts on from the server's elapsed");
    held.release(['event: agent_end\ndata: {"type":"agent_end"}\n\n']);
    await loading;
  });
});

describe("reattach: the attach forwards the panel's deltas", () => {
  it("a view_model_delta frame on the held stream reaches the canvas store", async () => {
    const panel: CanvasPanel = {
      id: "chat",
      title: "Chat",
      kind: "chat",
      payload: { session: "s1", running: true, rows: [{ id: "r1", role: "user", status: "complete", blocks: [{ type: "text", content: "count to 30" }] }] },
      actions: [],
    };
    const delta = JSON.stringify({ type: "view_model_delta", panel });
    const deltas: CanvasPanel[] = [];
    const held = stubFetch(
      {
        session: { id: "s1", updated_at: "t" },
        running: true,
        items: [{ kind: "user", content: "count to 30", time: "t0" }],
      },
      (h) => h.resp,
    );
    const host = createChatHost({ ...deps(), canvasDelta: (p) => deltas.push(p) });
    const st = host.ensureState("c1", "s1");
    const loading = host.load("c1", "s1");
    await flush();
    await flush();
    assert.deepEqual(deltas, [], "nothing before the stream delivers");
    held.release([
      `event: view_model_delta\ndata: ${delta}\n\n`,
      'event: agent_end\ndata: {"type":"agent_end"}\n\n',
    ]);
    await loading;
    await flush();
    assert.equal(st.attached, false, "the attach ended cleanly");
    assert.equal(deltas.length, 1, "the panel delta forwarded verbatim");
    assert.equal(deltas[0]?.kind, "chat");
    assert.equal(st.lastDeltasPerSecond >= 1, true, "the delta meter saw the repaint");
  });

  it("404 (session not active) resolves FALSE - render settled history, never error", async () => {
    globalThis.fetch = (async () =>
      ({ ok: false, status: 404 } as unknown as Response)) as typeof globalThis.fetch;
    savedFetch = globalThis.fetch;
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    await host.attach("c1");
    assert.equal(host.states.get("c1")!.attached, false);
  });
});