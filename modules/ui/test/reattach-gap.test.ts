// webui unit tests for the reattach completeness gaps (card t_692254f7):
// the transcript's live-run merge (fix A - the restored tail renders the
// run's in-flight items) and the orphaned tool_result materialize
// (fix B - a result with no running tool line lands as a row, never a
// silent drop). Host-level: createChatHost + stubbed fetch.
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import { createChatHost, type ChatHostDeps } from "../app/composables/chat.ts";

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

// an events stream that stays open until the test releases it - the
// window between the restore render and the stream's frames
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

// stub fetch with a URL router: transcript -> detail, everything else
// (the /events attach) -> the given Response
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

describe("reattach completeness: the transcript merges the live run (fix A)", () => {
  it("restore renders persisted history + the live tool row + the open thinking block", async () => {
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
    const loading = host.load("c1", "s1");
    // let the restore land; the attach stream is held open
    await flush();
    await flush();
    const st = host.states.get("c1")!;
    assert.deepEqual(
      st.transcript.map((i) => i.kind),
      ["message", "tool", "thinking"],
      "user bubble, the LIVE tool row, the open thinking block - the run is visible",
    );
    const tool = st.transcript[1];
    if (tool.kind !== "tool") return assert.fail("not a tool line");
    assert.equal(tool.name, "shell.run");
    assert.equal(tool.status, "running", "the in-flight call is running, not done");
    assert.equal(tool.elapsedMs, 7000, "the meter counts on from the server's elapsed");
    // release the held attach: a result frame arrives late - it must
    // land ON the restored row (it is still the most recent running
    // line), not be dropped
    held.release([
      'event: tool_result\ndata: {"type":"tool_result","message":{"content":"1..30","tool_call_id":"tc1"}}\n\n',
      'event: agent_end\ndata: {"type":"agent_end"}\n\n',
    ]);
    await loading;
    await flush();
    assert.equal(tool.status, "done", "the attached result attached to the restored row");
    assert.equal(tool.result, "1..30");
  });
});

describe("reattach completeness: no silent drops (fix B)", () => {
  it("an orphaned tool_result MATERIALIZES its row", async () => {
    // the call frame was broadcast between the transcript snapshot and
    // the attach subscribe: the wire delivers the result alone
    const held = stubFetch(
      {
        session: { id: "s1", updated_at: "t" },
        running: true,
        items: [{ kind: "user", content: "run it", time: "t0" }],
      },
      (h) => h.resp,
    );
    const host = createChatHost(deps());
    const loading = host.load("c1", "s1");
    await flush();
    await flush();
    held.release([
      'event: tool_result\ndata: {"type":"tool_result","message":{"content":"the work","tool_call_id":"tcX"}}\n\n',
      'event: agent_end\ndata: {"type":"agent_end"}\n\n',
    ]);
    await loading;
    await flush();
    const st = host.states.get("c1")!;
    assert.deepEqual(
      st.transcript.map((i) => i.kind),
      ["message", "tool"],
      "the result is never dropped - its row materializes retroactively",
    );
    const tool = st.transcript[1];
    if (tool.kind !== "tool") return assert.fail("not a tool line");
    assert.equal(tool.name, "tool", "the name falls back when nothing resolved the id");
    assert.equal(tool.status, "done");
    assert.equal(tool.result, "the work");
  });

  it("a live tool_call frame keeps its id, its result still attaches normally", async () => {
    const held = stubFetch(
      {
        session: { id: "s1", updated_at: "t" },
        running: true,
        items: [{ kind: "user", content: "run it", time: "t0" }],
      },
      (h) => h.resp,
    );
    const host = createChatHost(deps());
    const loading = host.load("c1", "s1");
    await flush();
    await flush();
    held.release([
      'event: tool_call\ndata: {"type":"tool_call","tool_call":{"id":"tcA","name":"files.read","arguments":{"path":"x"}}}\n\n',
      'event: tool_result\ndata: {"type":"tool_result","message":{"content":"contents","tool_call_id":"tcA"}}\n\n',
      'event: agent_end\ndata: {"type":"agent_end"}\n\n',
    ]);
    await loading;
    await flush();
    const st = host.states.get("c1")!;
    assert.deepEqual(st.transcript.map((i) => i.kind), ["message", "tool"]);
    const tool = st.transcript[1];
    if (tool.kind !== "tool") return assert.fail("not a tool line");
    assert.equal(tool.name, "files.read");
    assert.equal(tool.result, "contents");
  });
});

describe("reattach completeness: restore + attach read as one run", () => {
  it("the open thinking block is adopted, not duplicated", async () => {
    const held = stubFetch(
      {
        session: { id: "s1", updated_at: "t" },
        running: true,
        items: [
          { kind: "user", content: "q", time: "t0" },
          { kind: "thinking", content: "pre-refresh reasoning", running: true },
        ],
      },
      (h) => h.resp,
    );
    const host = createChatHost(deps());
    const loading = host.load("c1", "s1");
    await flush();
    await flush();
    held.release([
      'event: thinking_chunk\ndata: {"type":"thinking_chunk","content":" that continues"}\n\n',
      'event: agent_end\ndata: {"type":"agent_end"}\n\n',
    ]);
    await loading;
    await flush();
    const st = host.states.get("c1")!;
    const thinks = st.transcript.filter((i) => i.kind === "thinking");
    assert.equal(thinks.length, 1, "one continuous block, not a duplicate");
    const think = thinks[0];
    assert.ok(think.kind === "thinking");
    assert.equal(think.content, "pre-refresh reasoning that continues");
    assert.equal(think.streaming, false, "finalize closes the adopted block");
  });

  it("a per-tab interrupted marker retires when the session is running", async () => {
    const host = createChatHost(deps());
    const st = host.ensureState("c1", "s1");
    // the tab holds a stale marker from a previous (dead) stream
    st.transcript.push({ kind: "interrupted" });
    const held = stubFetch(
      {
        session: { id: "s1", updated_at: "t" },
        running: true,
        items: [{ kind: "user", content: "q", time: "t0" }],
      },
      (h) => h.resp,
    );
    const loading = host.load("c1", "s1");
    await flush();
    await flush();
    held.release(['event: agent_end\ndata: {"type":"agent_end"}\n\n']);
    await loading;
    await flush();
    assert.equal(
      st.transcript.some((i) => i.kind === "interrupted"),
      false,
      "a live run proves the stream did not end - the marker is stale",
    );
  });
});