// omega webui: unit tests for the SSE parser + payload dispatch (the
// pure half of the streamChat composable). chat-last 2a: the render
// frames (thinking/response/tool) carry NO callbacks anymore - the chat
// pane paints from the canvas panel's deltas; what the payload dispatch
// still routes is usage, terminal state, errors and view_model_delta.
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  handlePayload,
  parseFrame,
  normalizeCanvasPanel,
  normalizeChatPayload,
  renderChatPanel,
  chatPanelContent,
  chatPanelMessageCount,
  chatPanelOf,
} from "../app/composables/omega.ts";
import { createCanvasTranscript } from "../app/composables/chat.ts";

import type { StreamHandlers } from "../app/composables/omega.ts";

// silent handlers that record every call
function recording() {
  const calls: string[] = [];
  const handlers: StreamHandlers = {
    onUsage: (u) =>
      calls.push(
        `usage:${u.promptTokens}/${u.completionTokens}/${u.cachedTokens}`,
      ),
    onViewModelDelta: (p) => calls.push(`delta:${p.id}:${p.kind}`),
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
  it("render frames carry no callbacks - the canvas deltas paint the pane", () => {
    const { handlers, calls } = recording();
    const state = { seenResponse: false };
    handlePayload({ type: "thinking_chunk", content: "hm" }, handlers, state);
    handlePayload({ type: "response_chunk", content: "a" }, handlers, state);
    handlePayload({ type: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } }, handlers, state);
    handlePayload({ type: "tool_result", message: { content: "boom", is_error: true } }, handlers, state);
    assert.deepEqual(calls, [], "no transport consumer for render frames");
    assert.equal(state.seenResponse, true, "the stream state still tracks the response half");
  });

  it("a view_model_delta frame reaches the handler as a normalized panel", () => {
    const { handlers, calls } = recording();
    handlePayload(
      { type: "view_model_delta", panel: { id: "chat", title: "Chat", kind: "chat", payload: { session: "s1", rows: [] } } },
      handlers,
      { seenResponse: false },
    );
    assert.deepEqual(calls, ["delta:chat:chat"]);
  });

  it("a malformed delta panel is skipped (the snapshot is the catch-up)", () => {
    const { handlers, calls } = recording();
    handlePayload({ type: "view_model_delta", panel: 7 }, handlers, { seenResponse: false });
    assert.deepEqual(calls, []);
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

describe("normalizeChatPayload: the chat panel payload's wire guard", () => {
  it("guards role/status rows, drops malformed entries, keeps the rest", () => {
    const payload = normalizeChatPayload({
      session: "s1",
      running: true,
      rows: [
        { id: "3", role: "user", status: "complete", blocks: [{ type: "text", content: "q" }] },
        { role: "junk", status: "x" },                    // role junk: dropped
        "not-a-row",                                       // junk: dropped
        {
          id: "4",
          role: "assistant",
          status: "streaming",
          blocks: [
            { type: "text", content: "partial", running: true },
            { junk: true },                                // bad block: dropped
          ],
        },
        { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "c9", content: "out", is_error: true, elapsed_ms: 800 }] },
      ],
    })!;
    assert.ok(payload);
    assert.equal(payload.session, "s1");
    assert.equal(payload.running, true);
    assert.equal(payload.rows?.length, 3);
    assert.deepEqual(payload.rows?.[1]?.blocks?.map((b) => b.type), ["text"]);
    assert.equal(payload.rows?.[1]?.blocks?.[0]?.running, true);
    assert.equal(payload.rows?.[2]?.blocks?.[0]?.tool_call_id, "c9");
  });

  it("junk payloads degrade to null - the pane renders, never crashes", () => {
    assert.equal(normalizeChatPayload(null), null);
    assert.equal(normalizeChatPayload([1, 2]), null);
    assert.equal(normalizeChatPayload("chat"), null);
  });

  it("renderChatPanel maps the rows onto the component contract", () => {
    const items = renderChatPanel(
      normalizeChatPayload({
        session: "s1",
        rows: [
          { id: "3", role: "user", status: "complete", blocks: [{ type: "text", content: "q" }] },
          { role: "assistant", status: "streaming", blocks: [{ type: "thinking", content: "hm", running: true }] },
          {
            role: "assistant",
            status: "complete",
            blocks: [{ type: "tool_call", tool_call: { id: "c9", name: "files.read", arguments: { path: "x" } } }],
          },
          { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "c9", content: "contents" }] },
          { id: "5", role: "assistant", status: "complete", blocks: [{ type: "text", content: "## done" }] },
        ],
      })!,
    );
    assert.deepEqual(
      items.map((i) => i.kind),
      ["message", "thinking", "tool", "message"],
      "the call row + result row merge exactly like the restore merge; the prose row is its own bubble",
    );
    const user = items[0] as { role: string; id?: string; content: string };
    assert.equal(user.role, "user");
    assert.equal(user.id, "3");
    const think = items[1] as { streaming?: boolean };
    assert.equal(think.streaming, true, "the open block streams");
    const tool = items[2] as { name: string; result?: string; status: string };
    assert.equal(tool.name, "files.read");
    assert.equal(tool.result, "contents");
    assert.equal(tool.status, "done");
  });

  it("chatPanelContent + chatPanelMessageCount read the rows", () => {
    const payload = normalizeChatPayload({
      rows: [
        { role: "user", status: "complete", blocks: [{ type: "text", content: "1234" }] },
        { role: "assistant", status: "streaming", blocks: [{ type: "text", content: "12345678" }] },
        { role: "assistant", status: "complete", blocks: [{ type: "thinking", content: "12" }] },
        { role: "tool", status: "complete", blocks: [{ type: "tool_result", content: "ignore" }] },
      ],
    })!;
    // the estimate counts assistant prose + thinking only (chars/4)
    assert.equal(chatPanelContent(payload), 10);
    assert.equal(chatPanelMessageCount(payload), 2);
  });

  it("chatPanelOf picks the chat-kind panel, null elsewhere", () => {
    assert.equal(chatPanelOf([]), null);
    const p = { id: "x", title: "x", kind: "chat", payload: null, actions: [] };
    assert.equal(chatPanelOf([p]), p);
  });
});

describe("createCanvasTranscript: identity survives the deltas", () => {
  it("a repaint of the same rows keeps the item objects (meters keep ticking)", () => {
    const t = createCanvasTranscript();
    const payload = (content: string, running: boolean) =>
      ({
        session: "s1",
        running,
        rows: [
          {
            role: "assistant",
            status: "streaming",
            blocks: [{ type: "tool_call", running, tool_call: { id: "tc1", name: "shell.run" }, elapsed_ms: running ? 4000 : undefined }],
          },
          {
            role: "assistant",
            status: running ? "streaming" : "complete",
            blocks: [{ type: "text", content, running }],
          },
        ],
      }) as never;
    const first = t.apply(payload("", true));
    const second = t.apply(payload("now with words", true));
    assert.equal(first[0], second[0], "the tool row object is reused across deltas");
    assert.equal(first[1], second[1], "the text row object is reused across deltas");
    assert.equal((second[1] as { content: string }).content, "now with words");
    const tool = second[0] as { status: string; elapsedMs?: number };
    assert.equal(tool.status, "running");
    assert.equal(tool.elapsedMs, 4000, "the running meter counts on from the elapsed");
    // the result row lands: it settles the SAME tool item, meter spent
    const done = t.apply({
      session: "s1",
      rows: [
        { role: "assistant", status: "complete", blocks: [{ type: "tool_call", tool_call: { id: "tc1", name: "shell.run" } }] },
        { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "tc1", content: "1..30" }] },
        { role: "assistant", status: "complete", blocks: [{ type: "text", content: "done text", running: false }] },
      ],
    } as never);
    assert.equal(done[0], second[0], "the result row merges onto the kept call item");
    const settled = done[0] as { status: string; result?: string; elapsedMs?: number };
    assert.equal(settled.status, "done");
    assert.equal(settled.result, "1..30");
    assert.equal(settled.elapsedMs, undefined, "the meter spends when the call closes");
  });

  it("a user turn rides the panel and keeps identity once persisted forms it", () => {
    const t = createCanvasTranscript();
    const rows = (id?: string) =>
      [
        { id, role: "user", status: "complete", blocks: [{ type: "text", content: "hello" }] },
      ] as never;
    const live = t.apply({ session: "s2", rows: rows() } as never);
    const stored = t.apply({ session: "s2", rows: rows("9") } as never);
    assert.equal(live[0], stored[0], "id-less -> ided keeps the bubble (keyed v-for stays stable)");
    assert.equal((stored[0] as { id?: string }).id, "9");
  });

  it("a session switch resets the identity cache", () => {
    const t = createCanvasTranscript();
    const a = t.apply({ session: "s1", rows: [{ role: "user", status: "complete", blocks: [{ type: "text", content: "a" }] }] } as never);
    const b = t.apply({ session: "s2", rows: [{ role: "user", status: "complete", blocks: [{ type: "text", content: "b" }] }] } as never);
    assert.notEqual(a[0], b[0], "another conversation repaints fresh items");
    assert.equal((b[0] as { content: string }).content, "b");
  });

  it("the verdict card maps once and settles; live cards anchor a countdown", () => {
    const t = createCanvasTranscript();
    const args = { title: "ship?", options: [{ label: "yes" }, { label: "no" }] };
    const rows = (...rs: unknown[]) => ({ session: "s1", rows: rs }) as never;
    const open = t.apply(rows({
      role: "assistant",
      status: "streaming",
      blocks: [{ type: "tool_call", running: true, tool_call: { id: "v1", name: "verdict", arguments: args } }],
    }));
    const card = open[0] as { kind: string; status: string; issuedAt?: number; title: string };
    assert.equal(card.kind, "verdict");
    assert.equal(card.status, "pending");
    assert.ok(card.issuedAt, "a live card anchors its countdown");
    // repaint: the SAME card object, same countdown anchor
    const again = t.apply(rows({
      role: "assistant",
      status: "streaming",
      blocks: [{ type: "tool_call", running: true, tool_call: { id: "v1", name: "verdict", arguments: args } }],
    }));
    assert.equal(again[0], open[0]);
    assert.equal((again[0] as { issuedAt?: number }).issuedAt, card.issuedAt);
    // the result row settles the card
    const settledRows = t.apply(rows(
      {
        role: "assistant",
        status: "complete",
        blocks: [{ type: "tool_call", tool_call: { id: "v1", name: "verdict", arguments: args } }],
      },
      { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "v1", content: '{"verdict":"answer","option":"yes"}' }] },
    ));
    const settled = settledRows[0] as { kind: string; status: string; verdictKind?: string; picked?: string };
    assert.equal(settled.status, "settled", "the result row settles the card");
    assert.equal(settled.verdictKind, "answer");
    assert.equal(settled.picked, "yes");
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
