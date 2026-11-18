// omega webui: unit tests for the transcript RESTORE path.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createElapsedMeter, renderTranscriptItems } from "../app/composables/omega.ts";
import { createCanvasTranscript } from "../app/composables/chat.ts";
import { syncConsumedSpan } from "../app/composables/useElapsed.ts";

import type { RenderTimers, TranscriptWireItem } from "../app/composables/omega.ts";

describe("renderTranscriptItems", () => {
  it("renders a segmented session in wire order, nothing reconstructed", () => {
    const wire: TranscriptWireItem[] = [
      { kind: "user", content: "run tests", time: "t0" },
      { kind: "thinking", content: "let me check the file", thinking_ms: 1500 },
      { kind: "tool_call", tool_call: { id: "tc1", name: "go_test", arguments: { pkg: "./..." } } },
      { kind: "tool_result", tool_call_id: "tc1", content: "PASS", elapsed_ms: 3000, answer_ms: 500 },
      { kind: "message", content: "all green", time: "t3", response_ms: 500 },
    ];
    const items = renderTranscriptItems(wire);
    // shapes: user bubble, thinking block, tool line opened then filled by
    // its result, assistant bubble - exactly the order history produced
    assert.deepEqual(
      items.map((i) => (i.kind === "tool" ? `${i.kind}:${i.name}` : i.kind)),
      ["message", "thinking", "tool:go_test", "message"],
    );
    // user carries its store timestamp for ChatMessage's hover title
    assert.equal(items[0].kind === "message" && items[0].role, "user");
    assert.equal(items[0].kind === "message" && items[0].time, "t0");
    const answer = items[3];
    assert.equal(answer.kind, "message");
    if (answer.kind !== "message") return;
    assert.equal(answer.role, "assistant");
    assert.equal(answer.finalMs, 500);
    assert.equal(answer.time, "t3");
    const think = items[1];
    assert.equal(think.kind, "thinking");
    if (think.kind !== "thinking") return;
    assert.equal(think.finalMs, 1500);
  });

  it("a tool row carries BOTH durations: the call and the response return (card t_e27c8347)", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tcA", name: "go_test", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tcA", content: "PASS", elapsed_ms: 2100, answer_ms: 14300 },
      { kind: "message", content: "all green", response_ms: 14300 },
    ]);
    const tool = items[0];
    assert.ok(tool.kind === "tool");
    if (tool.kind !== "tool") return;
    assert.equal(tool.finalMs, 2100, "the call window (dispatch -> result)");
    assert.equal(tool.answerMs, 14300, "the response-return window (result -> answer complete)");
  });

  it("a result with no answer window leaves answerMs absent (the answer never came)", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tcB", name: "shell.run", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tcB", content: "out", elapsed_ms: 25 },
    ]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].finalMs, 25);
    assert.equal(items[0].answerMs, undefined);
  });

  it("tool_result merges onto the call like a live frame", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc9", name: "shell.run", arguments: { cmd: "ls" } } },
      { kind: "tool_call", tool_call: { id: "tc10", name: "files.read", arguments: undefined } },
      { kind: "tool_result", tool_call_id: "tc9", content: "out", elapsed_ms: 25 },
      { kind: "tool_result", tool_call_id: "tc11", content: "orphan" },
      { kind: "tool_result", tool_call_id: "tc12", content: "boom", is_error: true, elapsed_ms: 9 },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["tool", "tool"]);
    const a = items[0];
    assert.ok(a.kind === "tool");
    if (a.kind !== "tool") return;
    assert.equal(a.name, "shell.run");
    assert.equal(a.status, "done");
    assert.equal(a.result, "out");
    assert.equal(a.finalMs, 25);
    if (a.kind === "tool" && a.output !== undefined) {
      assert.equal(JSON.parse(a.output).cmd, "ls");
    }
    const b = items[1];
    if (b.kind !== "tool") return;
    // call without arguments: no output block, result never arrived ->
    // status stays done with an empty result (dead result is the
    // interrupted-marker's business, restore never fabricates it)
    assert.equal(b.output, undefined);
    assert.equal(b.finalMs, undefined);
  });

  it("error result flags the line, not a separate item", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tcX", name: "files.write", arguments: {} } },
      { kind: "tool_result", tool_call_id: "tcX", content: "denied", is_error: true, elapsed_ms: 2 },
    ]);
    assert.equal(items.length, 1);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].status, "error");
    assert.equal(items[0].result, "denied");
  });

  it("thin items render nothing; empty payload yields an empty list", () => {
    assert.deepEqual(renderTranscriptItems([]), []);
    assert.deepEqual(renderTranscriptItems(null), []);
    assert.deepEqual(renderTranscriptItems(undefined), []);
    const items = renderTranscriptItems([
      { kind: "user", content: "" },
      { kind: "message", content: "" },
      { kind: "thinking", content: "" },
      { kind: "message", content: "real" },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["message"]);
    assert.equal(items[0].kind === "message" && items[0].role, "assistant");
  });

  it("call without an id still renders its line (never lost)", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "", name: "mystery" } },
      { kind: "tool_result", tool_call_id: "", content: "no id, never attached" },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["tool"]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].name, "mystery");
  });

  // the live-run merge: the run's in-flight items carry `running` and render as the
  // shapes the live stream paints - one contract, two sources.
  it("a running tool_call renders the live line with its elapsed offset", () => {
    const items = renderTranscriptItems([
      {
        kind: "tool_call",
        tool_call: { id: "tc1", name: "shell.run", arguments: { cmd: "sleep 30" } },
        running: true,
        elapsed_ms: 7000,
      },
    ]);
    assert.deepEqual(items.map((i) => i.kind), ["tool"]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].status, "running");
    assert.equal(items[0].elapsedMs, 7000);
    assert.equal(items[0].finalMs, undefined, "no final duration while the call runs");
    const settled = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc2", name: "sh" }, running: true },
      { kind: "tool_result", tool_call_id: "tc2", content: "out" },
    ]);
    if (settled[0].kind !== "tool") return;
    assert.equal(settled[0].status, "done");
    assert.equal(settled[0].elapsedMs, undefined);
  });

  it("a pending call meters from its started_at, not from the restore's build instant", () => {
    const started = new Date(Date.now() - 9_000).toISOString();
    const now = Date.now();
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run" }, elapsed_ms: 300, started_at: started },
    ]);
    if (items[0].kind !== "tool") return;
    assert.equal(items[0].status, "pending");
    assert.ok(
      items[0].elapsedMs !== undefined && items[0].elapsedMs >= 8_500 && items[0].elapsedMs <= Date.now() - now + 9_500,
      `the restored row anchors on the call's start (got ${items[0].elapsedMs})`,
    );
    // an answered call never carries a live offset.
    const settled = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run" }, started_at: started },
      { kind: "tool_result", tool_call_id: "tc1", content: "done", elapsed_ms: 9_100 },
    ]);
    if (settled[0].kind !== "tool") return;
    assert.equal(settled[0].status, "done");
    assert.equal(settled[0].elapsedMs, undefined);
    assert.equal(settled[0].finalMs, 9_100);
  });

  it("running thinking and message blocks render streaming (the attach adopts them)", () => {
    const items = renderTranscriptItems([
      { kind: "message", content: "pre", time: "t0", response_ms: 100 },
      { kind: "thinking", content: "still going", running: true },
      { kind: "message", content: "partial answer", running: true },
    ]);
    if (items[0].kind !== "message") return;
    assert.equal(items[0].streaming, undefined, "a persisted segment mounts final");
    const think = items[1];
    if (think.kind !== "thinking") return assert.fail("items[1] not thinking");
    assert.equal(think.streaming, true, "open thinking streams from the restored item");
    const bubble = items[2];
    if (bubble.kind !== "message") return assert.fail("items[2] not message");
    assert.equal(bubble.streaming, true, "open response block streams from the restored item");
  });

  // the streaming segment's elapsed-so-far is the meter's anchor. Persisted rows never
  // carry it: they mount final.
  it("a running thinking/answer row carries the elapsed-so-far it counts on from", () => {
    const items = renderTranscriptItems([
      { kind: "thinking", content: "still going", running: true, elapsed_ms: 8400 },
      { kind: "message", content: "partial answer", running: true, elapsed_ms: 2500 },
      { kind: "message", content: "settled answer", response_ms: 4100, elapsed_ms: 9999 },
    ]);
    const think = items[0];
    if (think.kind !== "thinking") return assert.fail("items[0] not thinking");
    assert.equal(think.elapsedMs, 8400, "the thinking meter's anchor");
    assert.equal(think.finalMs, undefined, "a live phase has no final span");
    const live = items[1];
    if (live.kind !== "message") return assert.fail("items[1] not message");
    assert.equal(live.elapsedMs, 2500, "the answer meter's anchor");
    const settled = items[2];
    if (settled.kind !== "message") return assert.fail("items[2] not message");
    assert.equal(settled.elapsedMs, undefined, "a persisted answer ignores the anchor");
    assert.equal(settled.finalMs, 4100, "the server's measured span wins");
  });

  // a segment a tool call ended ships SETTLED on the live tail too.
  it("a tool call that ended the segment settles its text rows on the live tail", () => {
    const items = renderTranscriptItems([
      { kind: "thinking", content: "considered", thinking_ms: 2200 },
      { kind: "message", content: "calling the checker", response_ms: 900 },
      { kind: "tool_call", tool_call: { id: "c1", name: "shell.run" }, running: true, elapsed_ms: 5000 },
    ]);
    const think = items[0];
    if (think.kind !== "thinking") return assert.fail("items[0] not thinking");
    assert.equal(think.streaming, undefined, "a finished thinking row never streams");
    assert.equal(think.elapsedMs, undefined, "and carries no meter anchor");
    assert.equal(think.finalMs, 2200, "it carries its measured span");
    const answer = items[1];
    if (answer.kind !== "message") return assert.fail("items[1] not message");
    assert.equal(answer.streaming, undefined, "a finished answer row never streams");
    assert.equal(answer.elapsedMs, undefined, "and carries no meter anchor");
    assert.equal(answer.finalMs, 900, "it carries its measured span");
    const call = items[2];
    if (call.kind !== "tool") return assert.fail("items[2] not tool");
    assert.equal(call.status, "running", "the call itself keeps streaming");
    assert.equal(call.elapsedMs, 5000);
  });
});

// -- the meter's LATE anchor: the consumed span arrives after mount ---. The
// acceptance: a fake clock, initialMs 0, 30s pushed through the getter -> the label
// jumps once, and a repeated or stale arrival never double-counts.
describe("useElapsed late anchor (the consumed span arrives after mount)", () => {
  // manual fake clock, the omega.test.ts shape: one stored tick callback
  function fakeMeter() {
    const labels: string[] = [];
    let now = 0;
    let fire: (() => void) | null = null;
    const timers: RenderTimers = {
      set: (fn) => {
        fire = fn;
        return 0;
      },
      clear: () => {
        fire = null;
      },
    };
    const meter = createElapsedMeter((l) => labels.push(l), () => now, timers, 0);
    return {
      meter,
      labels,
      last: () => labels[labels.length - 1],
      ticksPending: () => fire !== null,
      advance(ms: number) {
        now += ms;
        const f = fire;
        fire = null;
        f?.();
      },
    };
  }

  it("jumps once from 0 to the arrived span, then counts on from it", () => {
    const t = fakeMeter();
    let consumed = 0;
    const anchor = () => consumed;
    t.meter.start();
    assert.deepEqual(t.labels, ["0ms"], "a row that mounted before its span starts at zero");
    t.advance(1000);
    assert.equal(t.last(), "1s");
    // the span arrives: the call had already burned 30s before this row knew anything.
    consumed = 30_000;
    syncConsumedSpan(t.meter, anchor);
    assert.equal(t.last(), "30s", "the label jumps to the real elapsed");
    assert.equal(t.labels.length, 3, "and jumps exactly once");
    t.advance(1000);
    assert.equal(t.last(), "31s", "the meter counts on from the new anchor");
    // a repeat of the same paint and a stale payload landing later are both no-ops.
    syncConsumedSpan(t.meter, anchor);
    assert.equal(t.labels.length, 4, "a repeated arrival emits nothing");
    consumed = 12_000;
    syncConsumedSpan(t.meter, anchor);
    assert.equal(t.last(), "31s", "a stale (smaller) span never walks the label back");
  });

  it("replaces the base instead of adding to it (no double count)", () => {
    const t = fakeMeter();
    let consumed = 0;
    const anchor = () => consumed;
    t.meter.start();
    t.advance(10_000);
    assert.equal(t.last(), "10s", "the meter burned 10s on its own");
    consumed = 30_000;
    syncConsumedSpan(t.meter, anchor);
    assert.equal(t.last(), "30s", "30s consumed is 30s, never 10s + 30s");
    // a later paint of the same live call hands a bigger elapsed-so-far.
    consumed = 31_000;
    syncConsumedSpan(t.meter, anchor);
    assert.equal(t.last(), "31s", "a growing paint re-anchors without stacking");
    assert.ok(t.ticksPending(), "and the ticker is still armed");
  });

  it("holds an arrival that lands while the row is still settled for the next start", () => {
    const t = fakeMeter();
    let consumed = 0;
    const anchor = () => consumed;
    consumed = 30_000;
    syncConsumedSpan(t.meter, anchor);
    assert.deepEqual(t.labels, [], "a settled meter emits nothing");
    t.meter.start();
    assert.equal(t.last(), "30s", "the first start counts on from the arrived span");
    // a row that already froze keeps its frozen label.
    t.meter.stop();
    const frozen = t.last();
    consumed = 45_000;
    syncConsumedSpan(t.meter, anchor);
    assert.equal(t.last(), frozen, "a stopped meter never re-emits a late span");
  });

  // The cases above drive the reconciler directly.
  it("useElapsed watches the anchor getter and hands it to the meter", () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const source = readFileSync(join(here, "..", "app", "composables", "useElapsed.ts"), "utf8");
    assert.match(source, /watch\(initialMs, \(\) => syncConsumedSpan\(meter, initialMs\)\)/, "the setup capture is not the last word - the getter is watched");
    const helper = source.slice(source.indexOf("export function syncConsumedSpan"));
    assert.match(helper, /meter\.reanchor\(initialMs\(\)\)/, "the watched value reaches the meter's reanchor");
  });
});

// -- the verdict countdown anchor ------------------- The card's acceptance is a live.
describe("verdict countdown anchor", () => {
  const VERDICT_TIMEOUT_S = 300;

  function verdictCallRow(callId: string, startedAt?: string): unknown {
    return {
      role: "assistant",
      status: "streaming",
      blocks: [
        {
          type: "tool_call",
          running: true,
          ...(startedAt ? { started_at: startedAt } : {}),
          tool_call: {
            id: callId,
            name: "verdict.request",
            arguments: {
              title: "ship it?",
              options: [{ label: "yes" }, { label: "no" }],
              timeout_s: VERDICT_TIMEOUT_S,
            },
          },
        },
      ],
    };
  }

  interface AnchorCard {
    kind: string;
    status: string;
    issuedAt?: number;
  }

  function paneAnchor(
    pane: ReturnType<typeof createCanvasTranscript>,
    row: unknown,
    session = "s1",
  ): AnchorCard {
    const items = pane.apply({ session, running: true, rows: [row] } as never);
    return items[0] as unknown as AnchorCard;
  }

  it("a remount mid-deadline continues: the server's start stamp is the anchor", () => {
    const startedAt = new Date(Date.now() - 4 * 60_000).toISOString();
    const row = verdictCallRow("v-remount-stamp", startedAt);
    const first = paneAnchor(createCanvasTranscript(), row);
    assert.equal(first.kind, "verdict");
    assert.equal(
      first.issuedAt,
      Date.parse(startedAt),
      "the anchor is the call's server clock, not the paint's arrival",
    );
    const remounted = paneAnchor(createCanvasTranscript(), row);
    assert.equal(remounted.issuedAt, first.issuedAt, "the label continues on the call, never on the mount");
    const left = (first.issuedAt ?? 0) + VERDICT_TIMEOUT_S * 1000 - Date.now();
    assert.ok(left > 30_000 && left <= 60_000, `about a minute of the five left, got ${left}ms`);
  });

  it("no server stamp: the call id's first sight holds across panes", () => {
    const row = verdictCallRow("v-remount-sight");
    // The two panes are four minutes apart.
    const realNow = Date.now;
    const first = (() => {
      Date.now = () => realNow() - 4 * 60_000;
      try {
        return paneAnchor(createCanvasTranscript(), row);
      } finally {
        Date.now = realNow;
      }
    })();
    assert.ok(first.issuedAt, "a card the wire serves without a clock still anchors");
    const second = paneAnchor(createCanvasTranscript(), row);
    assert.equal(second.issuedAt, first.issuedAt, "the anchor is the call id's fact, not the pane's");
    assert.ok(
      realNow() - (second.issuedAt ?? 0) > 3 * 60_000,
      "the second pane reads the FIRST pane's instant, four minutes back",
    );
  });

  it("a start stamp arriving on a later delta moves the anchor once, onto the truth", () => {
    const pane = createCanvasTranscript();
    const bare = paneAnchor(pane, verdictCallRow("v-late-stamp"));
    const bareAt = bare.issuedAt ?? Date.now();
    const startedAt = new Date(bareAt - 4 * 60_000).toISOString();
    const stamped = paneAnchor(pane, verdictCallRow("v-late-stamp", startedAt));
    assert.equal(stamped, bare, "same card object: the paint moves the field, never the identity");
    assert.equal(stamped.issuedAt, Date.parse(startedAt), "the server's clock outranks first sight");
    assert.ok(stamped.issuedAt! < bareAt, "and it moves the deadline EARLIER, not later");
    const again = paneAnchor(pane, verdictCallRow("v-late-stamp", startedAt));
    assert.equal(again.issuedAt, Date.parse(startedAt), "a fixed instant never drifts on a repaint");
  });

  it("a session switch never resets a card's anchor", () => {
    const pane = createCanvasTranscript();
    const card = paneAnchor(pane, verdictCallRow("v-cross-session"), "s1");
    pane.apply({
      session: "s2",
      rows: [{ role: "user", status: "complete", blocks: [{ type: "text", content: "other" }] }],
    } as never);
    const back = paneAnchor(pane, verdictCallRow("v-cross-session"), "s1");
    assert.equal(back.issuedAt, card.issuedAt, "the call's instant survives a panel retarget");
  });

  it("a settled card keeps no anchor, and a later paint never re-mints one", () => {
    const pane = createCanvasTranscript();
    paneAnchor(pane, verdictCallRow("v-settles"));
    const settled = pane.apply({
      session: "s1",
      rows: [
        verdictCallRow("v-settles"),
        {
          role: "tool",
          status: "complete",
          blocks: [{ type: "tool_result", tool_call_id: "v-settles", content: '{"verdict":"skip"}' }],
        },
      ],
    } as never)[0] as unknown as AnchorCard;
    assert.equal(settled.status, "settled");
    assert.equal(settled.issuedAt, undefined, "a settled card has no countdown to anchor");
    // the server drops started_at once the result lands: the bare call row must not.
    const repaint = paneAnchor(pane, verdictCallRow("v-settles"));
    assert.equal(repaint.status, "settled");
    assert.equal(repaint.issuedAt, undefined, "the spent anchor stays spent");
  });

  it("the card's label subtracts a live clock from that fixed anchor", () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const source = readFileSync(join(here, "..", "app", "components", "chat", "VerdictCard.vue"), "utf8");
    assert.match(
      source,
      /props\.item\.issuedAt\s*\+\s*timeoutS\s*\*\s*1000\s*-\s*now\.value/,
      "the label is anchor + timeout - now: only `now` restarts when the card remounts",
    );
    assert.match(
      source,
      /if \(!props\.item\.issuedAt\) return ""/,
      "no anchor renders no label - never a fabricated full countdown",
    );
  });
});
