// omega webui: the PAINT-WRITE LAW (card t_a356fcc7).
//
// The chat pane paints from ONE cached item per transcript row: the canvas
// paint (createCanvasTranscript.apply) does NOT build fresh objects, it
// rewrites its cached, REACTIVE rows in place, because identity is what
// keeps the meters, the folds and the keyed v-for steady across a stream
// (t_494639c8 / t_078a2c35). That makes every assignment inside a paint a
// reactive write on the objects the paint just handed out.
//
// The law this file pins: A PAINT OF AN UNCHANGED PAYLOAD WRITES NOTHING
// THAT CHANGES A VALUE. A changed write invalidates every computed and
// effect that read the field - including the computeds that called the
// paint - so a value that differs between two paints of the same payload
// re-arms the paint it just came from. Vue's production build has no
// recursive-update guard: the flush loop never drains, the main thread is
// pinned, clicks do nothing, and a reload re-enters the same spin (his
// 2026-11-18 report: the approval gate never painted, only a fresh tab
// recovered).
//
// Two writes were that shape and both are pinned here:
//   1. ADDRESS ALIASING - one store row lands as several wire rows (a
//      reasoning block and the answer block share the row id), and the
//      cache addressed both by `row:<id>`. One object then carried two
//      kinds, so every paint wrote `kind: thinking -> message` over
//      itself. Also visibly wrong: the live pane dropped the reasoning
//      row entirely (one object cannot be both).
//   2. THE LOCAL CLOCK - a running call's elapsed offset was derived as
//      `Date.now() - started_at`, which differs on every paint. The
//      payload already carries the server's freshened elapsed
//      (chat_panel.go freshenRunningCalls), so the paint has no reason to
//      consult the wall clock at all.
//
// Run: node --test test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";

import { watch } from "vue";

import { createCanvasTranscript } from "../app/composables/chat.ts";
import { applyTurnTotals, type ChatPayloadWire, type ChatRowWire } from "../app/composables/omega.ts";

const payload = (rows: ChatRowWire[], running = false): ChatPayloadWire => ({
  session: "s1",
  running,
  rows,
});

// the incident's turn shape: ONE store row that reasoned and then answered
// (agent/api expands a stored row's blocks into one wire row per block, all
// sharing the store row id), plus an open tool call - the gate window.
function multiBlockTurn(id: string): ChatRowWire[] {
  return [
    { id: "1", role: "user", status: "complete", time: "2026-11-18T07:51:11Z", blocks: [{ type: "text", content: "count the go files" }] },
    { id, role: "assistant", status: "complete", time: "2026-11-18T07:51:12Z", blocks: [{ type: "thinking", content: "let me look", thinking_ms: 412 }] },
    { id, role: "assistant", status: "complete", time: "2026-11-18T07:51:13Z", blocks: [{ type: "text", content: "Counting files now.", response_ms: 900 }] },
  ];
}

function openCallRow(): ChatRowWire {
  return {
    role: "assistant",
    status: "streaming",
    blocks: [
      {
        type: "tool_call",
        running: true,
        // the server's own open stamp; NO elapsed_ms on this paint
        started_at: "2026-11-18T07:51:14Z",
        tool_call: { id: "call_gate", name: "shell.run", arguments: { command: "dir /s /b *.go" } },
      },
    ],
  };
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

// snapshot is what an effect actually depends on: the fields a paint writes.
// A watcher over it fires exactly when a paint CHANGED a rendered value.
function snapshot(items: readonly unknown[]): string {
  return JSON.stringify(
    items.map((raw) => {
      const it = raw as Record<string, unknown>;
      return [it.kind, it.role, it.content, it.status, it.elapsedMs, it.turnMs, it.streaming];
    }),
  );
}

test("one store row with two blocks: reasoning and answer are DIFFERENT items", () => {
  const t = createCanvasTranscript();
  const items = t.apply(payload(multiBlockTurn("7")));
  assert.equal(items.length, 3);
  const [user, thinking, answer] = items;
  assert.equal(user?.kind, "message");
  assert.equal(thinking?.kind, "thinking");
  assert.equal(answer?.kind, "message");
  // the aliasing defect: both blocks were ONE object, so the pane could
  // never show the reasoning row and the answer row at the same time
  assert.notEqual(thinking, answer, "the thinking row and the answer row are one store row, two items");
  assert.equal((thinking as { content?: string }).content, "let me look");
  assert.equal((answer as { content?: string }).content, "Counting files now.");

  // and the identity survives the next paint (the id law, per block)
  const again = t.apply(payload(multiBlockTurn("7")));
  assert.equal(again[1], thinking);
  assert.equal(again[2], answer);
});

test("a paint of an unchanged payload changes NOTHING (no write re-arms the paint)", async () => {
  const t = createCanvasTranscript();
  const rows = [...multiBlockTurn("7"), openCallRow()];
  const items = t.apply(payload(rows, true));

  let paints = 0;
  const stop = watch(
    () => snapshot(items),
    () => {
      paints++;
    },
    { flush: "sync" },
  );
  // two more paints of the SAME payload. The gap is what makes the local
  // clock visible: a Date.now()-derived elapsed offset differs across it.
  await sleep(25);
  t.apply(payload(rows, true));
  await sleep(25);
  t.apply(payload(rows, true));
  stop();
  assert.equal(paints, 0, "an unchanged payload repainted a rendered value");
});

test("the meter anchor is minted at first sight and never re-derived per paint", () => {
  const t = createCanvasTranscript();
  const rows = [...multiBlockTurn("7"), openCallRow()];
  const items = t.apply(payload(rows, true));
  const tool = items.find((it) => it.kind === "tool") as { elapsedMs?: number } | undefined;
  const firstAnchor = tool?.elapsedMs;
  assert.equal(typeof firstAnchor, "number", "a running call with a start stamp meters from it");

  let paints = 0;
  const stop = watch(
    () => snapshot(items),
    () => {
      paints++;
    },
    { flush: "sync" },
  );
  // a clock that moves between two paints of the SAME payload. The old
  // paint re-derived `now - started_at` here, so every paint wrote a new
  // number; the row's own meter is what counts on, not the paint.
  const realNow = Date.now;
  try {
    Date.now = () => realNow() + 7_000;
    t.apply(payload(rows, true));
  } finally {
    Date.now = realNow;
  }
  stop();
  assert.equal(paints, 0, "the repaint wrote a re-derived meter value");
  assert.equal(tool?.elapsedMs, firstAnchor, "the anchor is the one minted at first sight");
});

test("applyTurnTotals writes only on change (no clear-then-stamp churn)", () => {
  const t = createCanvasTranscript();
  const items = t.apply(payload(multiBlockTurn("7")));
  // the first paint stamped the answer's turn total from the two row stamps
  const answer = items[2] as { turnMs?: number };
  assert.equal(answer.turnMs, 2000);

  let writes = 0;
  const stop = watch(
    () => snapshot(items),
    () => {
      writes++;
    },
    { flush: "sync" },
  );
  applyTurnTotals(items);
  applyTurnTotals(items);
  stop();
  assert.equal(writes, 0, "re-stamping the same turn wrote a rendered value again");
});

test("a phase below the answer takes the total away once, not on every paint", () => {
  const t = createCanvasTranscript();
  const rows = multiBlockTurn("7");
  const items = t.apply(payload(rows));
  const answer = items[2] as { turnMs?: number };
  assert.equal(answer.turnMs, 2000);

  let writes = 0;
  const stop = watch(
    () => snapshot(items),
    () => {
      writes++;
    },
    { flush: "sync" },
  );
  // the turn continues past the answer: the badge moves to the new answer
  const withPhase = [...rows, openCallRow()];
  t.apply(payload(withPhase, true));
  const afterFirst = writes;
  t.apply(payload(withPhase, true));
  stop();
  assert.equal(answer.turnMs, undefined, "the segment stopped being the answer");
  assert.ok(afterFirst > 0, "the phase landing was a real change");
  assert.equal(writes, afterFirst, "and the SECOND paint of it changed nothing");
});
