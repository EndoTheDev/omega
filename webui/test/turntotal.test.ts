// omega webui: the ANSWER row's elapsed badge is the TURN TOTAL, not the last segment.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  applyTurnTotals,
  normalizeChatPayload,
  renderTranscriptItems,
} from "../app/composables/omega.ts";
import { createCanvasTranscript } from "../app/composables/chat.ts";

import type {
  ChatPayloadWire,
  TranscriptItem,
  TranscriptWireItem,
} from "../app/composables/omega.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// the store's created_at shape (core.NowISO: RFC 3339, UTC, nanoseconds)
const T0 = "2026-11-15T09:00:00.000000000Z";
const at = (ms: number) => new Date(Date.parse(T0) + ms).toISOString();

function gateTurn(answerMs = 841): TranscriptWireItem[] {
  return [
    { kind: "user", content: "sleep 30 then tell me", time: T0 },
    { kind: "thinking", content: "he wants a pause", thinking_ms: 1000 },
    {
      kind: "tool_call",
      tool_call: { id: "tc1", name: "shell.run", arguments: { cmd: "sleep 30" } },
    },
    {
      kind: "tool_result",
      tool_call_id: "tc1",
      content: "done",
      elapsed_ms: 49_800,
      answer_ms: answerMs,
    },
    { kind: "message", content: "slept 30.", time: at(51_000), response_ms: answerMs },
  ];
}

// a phase row (thinking, tool) is not a MessageItem.
const phaseTotal = (item: TranscriptItem): number | undefined =>
  (item as { turnMs?: number }).turnMs;

function answerItem(items: TranscriptItem[]) {
  const last = items[items.length - 1];
  assert.equal(last?.kind, "message");
  if (!last || last.kind !== "message") throw new Error("no trailing answer row");
  return last;
}

describe("the turn total on the restored transcript", () => {
  it("the answer row carries the whole turn, its own span stays intact", () => {
    const items = renderTranscriptItems(gateTurn());
    const answer = answerItem(items);
    assert.equal(answer.role, "assistant");
    assert.equal(answer.turnMs, 51_000, "prompt arrival -> answer landing");
    // the segment's own measurement is not destroyed.
    assert.equal(answer.finalMs, 841);
    const think = items[1];
    assert.ok(think.kind === "thinking");
    if (think.kind !== "thinking") return;
    assert.equal(think.finalMs, 1000);
    assert.equal(phaseTotal(think), undefined, "a phase row never wears a turn total");
    const tool = items[2];
    assert.ok(tool.kind === "tool");
    if (tool.kind !== "tool") return;
    assert.equal(tool.finalMs, 49_800);
    assert.equal(phaseTotal(tool), undefined);
  });

  it("only the turn's LAST prose segment is the answer; a mid-turn phase is not", () => {
    const items = renderTranscriptItems([
      { kind: "user", content: "check the build", time: T0 },
      { kind: "message", content: "let me look.", time: at(1200), response_ms: 1200 },
      { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tc1", content: "ok", elapsed_ms: 30_000 },
      { kind: "message", content: "it builds.", time: at(40_000), response_ms: 900 },
    ]);
    const phase = items[1];
    assert.ok(phase.kind === "message");
    if (phase.kind !== "message") return;
    assert.equal(phase.turnMs, undefined, "prose before a call is a phase, not the answer");
    assert.equal(phase.finalMs, 1200);
    assert.equal(answerItem(items).turnMs, 40_000);
  });

  it("each turn totals itself - two turns never share a number", () => {
    const items = renderTranscriptItems([
      { kind: "user", content: "one", time: at(0) },
      { kind: "message", content: "a", time: at(5_000), response_ms: 5_000 },
      { kind: "user", content: "two", time: at(20_000) },
      { kind: "message", content: "b", time: at(32_000), response_ms: 2_000 },
    ]);
    const first = items[1];
    assert.ok(first.kind === "message");
    if (first.kind !== "message") return;
    assert.equal(first.turnMs, 5_000);
    assert.equal(answerItem(items).turnMs, 12_000, "the second turn measures from ITS prompt");
  });

  it("a turn whose run is still going (no answer yet) stamps nothing", () => {
    const items = renderTranscriptItems([
      { kind: "user", content: "go", time: T0 },
      { kind: "thinking", content: "hmm", running: true, elapsed_ms: 4000 },
    ]);
    assert.ok(items.every((it) => it.kind !== "message" || it.turnMs === undefined));
  });

  it("a turn that ends in a tool call has no answer row to stamp", () => {
    const items = renderTranscriptItems([
      { kind: "user", content: "go", time: T0 },
      { kind: "message", content: "running it.", time: at(1000), response_ms: 1000 },
      { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tc1", content: "out", elapsed_ms: 500 },
    ]);
    const prose = items[1];
    assert.ok(prose.kind === "message");
    if (prose.kind !== "message") return;
    assert.equal(prose.turnMs, undefined, "a call below the prose ends the phase check");
  });
});

describe("where the stamps are missing the badge degrades to the segment", () => {
  it("no user head to measure from (a truncated canvas window)", () => {
    const items = renderTranscriptItems([
      { kind: "message", content: "an old answer", time: at(1000), response_ms: 841 },
    ]);
    assert.equal(answerItem(items).turnMs, undefined);
    assert.equal(answerItem(items).finalMs, 841, "the span it always showed survives");
  });

  it("an answer the store has not stamped yet", () => {
    const items = renderTranscriptItems([
      { kind: "user", content: "go", time: T0 },
      { kind: "message", content: "answer", response_ms: 841 },
    ]);
    assert.equal(answerItem(items).turnMs, undefined);
  });

  it("a malformed or backwards stamp never fabricates a number", () => {
    const unparseable = renderTranscriptItems([
      { kind: "user", content: "go", time: "t0" },
      { kind: "message", content: "answer", time: "t3", response_ms: 500 },
    ]);
    assert.equal(answerItem(unparseable).turnMs, undefined);
    const backwards = renderTranscriptItems([
      { kind: "user", content: "go", time: at(9_000) },
      { kind: "message", content: "answer", time: at(1_000), response_ms: 500 },
    ]);
    assert.equal(answerItem(backwards).turnMs, undefined);
  });
});

describe("the pass is a paint, not an event (idempotent + self-clearing)", () => {
  it("re-running never stacks or drifts", () => {
    const items = renderTranscriptItems(gateTurn());
    assert.equal(answerItem(items).turnMs, 51_000);
    applyTurnTotals(items);
    applyTurnTotals(items);
    assert.equal(answerItem(items).turnMs, 51_000);
  });

  it("a row that stops being the answer drops its total (the live delta)", () => {
    const items: TranscriptItem[] = [
      { kind: "message", role: "user", content: "check", time: T0 },
      { kind: "message", role: "assistant", content: "on it.", time: at(2000), finalMs: 2000 },
    ];
    applyTurnTotals(items);
    const prose = items[1];
    assert.ok(prose.kind === "message");
    if (prose.kind !== "message") return;
    assert.equal(prose.turnMs, 2000);
    items.push({ kind: "tool", name: "shell.run", status: "running" });
    applyTurnTotals(items);
    assert.equal(prose.turnMs, undefined, "the stale total is cleared, not left standing");
  });
});

describe("the LIVE canvas serves the same two stamps", () => {
  const payload = (withTime: boolean): ChatPayloadWire => ({
    session: "s1",
    running: false,
    rows: [
      {
        id: "u1",
        role: "user",
        status: "complete",
        time: withTime ? T0 : undefined,
        blocks: [{ type: "text", content: "sleep 30" }],
      },
      {
        id: "a1",
        role: "assistant",
        status: "complete",
        time: withTime ? at(51_000) : undefined,
        blocks: [{ type: "text", content: "slept 30.", response_ms: 841 }],
      },
    ],
  });

  it("a persisted live turn computes the same total the restore does", () => {
    const live = createCanvasTranscript().apply(payload(true));
    assert.equal(answerItem(live).turnMs, 51_000);
    assert.equal(answerItem(live).finalMs, 841);
    assert.equal(answerItem(renderTranscriptItems(gateTurn())).turnMs, 51_000);
  });

  it("an in-flight row (no stamp yet) keeps its segment meter", () => {
    const live = createCanvasTranscript().apply(payload(false));
    assert.equal(answerItem(live).turnMs, undefined);
    assert.equal(answerItem(live).finalMs, 841);
  });

  it("the normalizer keeps a stamp and drops a malformed one", () => {
    const kept = normalizeChatPayload({
      rows: [{ id: "u1", role: "user", status: "complete", time: T0, blocks: [] }],
    });
    assert.equal(kept?.rows?.[0]?.time, T0);
    const dropped = normalizeChatPayload({
      rows: [{ id: "u1", role: "user", status: "complete", time: 42, blocks: [] }],
    });
    assert.equal(dropped?.rows?.[0]?.time, undefined);
  });
});

describe("the wiring: item field -> component prop -> meter", () => {
  it("ChatItem hands the total to the answer row", () => {
    assert.match(src("components/ChatItem.vue"), /:turn-ms="item\.turnMs"/);
  });

  it("ChatMessage declares the prop and prefers it over the segment span", () => {
    const message = src("components/chat/ChatMessage.vue");
    assert.match(message, /turnMs\?: number;/, "the prop is declared");
    assert.match(
      message,
      /\(\) => props\.turnMs \?\? props\.finalMs/,
      "the frozen label prefers the turn total",
    );
    assert.match(message, /\(\) => props\.streaming \?\? false/);
    assert.match(message, /\(\) => props\.elapsedMs \?\? 0/);
  });

  it("both render sources run the one pass", () => {
    assert.match(
      src("composables/omega.ts"),
      /applyTurnTotals\(out\);/,
      "the restore builder stamps",
    );
    assert.match(src("composables/chat.ts"), /applyTurnTotals\(out\);/, "the canvas paints");
  });
});
