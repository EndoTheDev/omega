// omega webui: the canvas transcript's IDENTITY LAW. A prose row's cache address is its
// STORE ROW ID, never its position. This file is the permanent pin for the audit probe:
// 41+ rows, the head leaves, one append - every survivor keeps its object, the new tail
// row never picks up an old.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { effect, nextTick, stop } from "vue";

import { createCanvasTranscript } from "../app/composables/chat.ts";
import type { ChatPayloadWire, ChatRowWire } from "../app/composables/omega.ts";

const here = dirname(fileURLToPath(import.meta.url));
const chatSource = readFileSync(join(here, "..", "app", "composables", "chat.ts"), "utf8");
const omegaSource = readFileSync(join(here, "..", "app", "composables", "omega.ts"), "utf8");

// row builders: the panel's own v1 vocabulary (ONE block per row)

function userRow(id: string | undefined, content: string): ChatRowWire {
  return {
    ...(id ? { id } : {}),
    role: "user",
    status: "complete",
    blocks: [{ type: "text", content }],
  };
}

function textRow(id: string | undefined, content: string, streaming = false): ChatRowWire {
  return {
    ...(id ? { id } : {}),
    role: "assistant",
    status: streaming ? "streaming" : "complete",
    blocks: [{ type: "text", content, running: streaming || undefined }],
  };
}

function thinkingRow(id: string | undefined, content: string, streaming = false): ChatRowWire {
  return {
    ...(id ? { id } : {}),
    role: "assistant",
    status: streaming ? "streaming" : "complete",
    blocks: [{ type: "thinking", content, running: streaming || undefined }],
  };
}

function toolCallRow(id: string | undefined, callId: string): ChatRowWire {
  return {
    ...(id ? { id } : {}),
    role: "assistant",
    status: "complete",
    blocks: [{ type: "tool_call", tool_call: { id: callId, name: "shell.run", arguments: {} } }],
  };
}

function toolResultRow(id: string | undefined, callId: string, content: string): ChatRowWire {
  return {
    ...(id ? { id } : {}),
    role: "tool",
    status: "complete",
    blocks: [{ type: "tool_result", tool_call_id: callId, content }],
  };
}

// a 41-row persisted payload that exercises every prose branch: user
// bubbles, assistant segments and thinking rows, in wire order. The
// SHAPE follows the store id (not the window offset): on the wire an id
// names ONE row, so a window shifted by one keeps every surviving id
// pointing at the same kind of row - the address carries the shape too
// (omega.ts transcriptAddress), and a fixture that flips a row's shape
// under one id would be testing an id the store cannot serve.
function proseWindow(prefix: string, first: number, count: number): ChatRowWire[] {
  const rows: ChatRowWire[] = [];
  for (let n = 0; n < count; n++) {
    const id = `${prefix}${first + n}`;
    // the shape follows the ROW ID, not the slot in the window: a sliding
    // window re-lists the same rows, and a store row never changes its
    // role or block type (the address is (id, role: block type), cards
    // t_a356fcc7 + t_a20fbad7)
    const cycle = (first + n) % 3;
    if (cycle === 0) rows.push(userRow(id, `q${id}`));
    else if (cycle === 1) rows.push(thinkingRow(id, `think ${id}`));
    else rows.push(textRow(id, `answer ${id}`));
  }
  return rows;
}

const payload = (rows: ChatRowWire[], running = false): ChatPayloadWire => ({
  session: "s1",
  running,
  rows,
});

// the acceptance probe: a 41+ row window where the head leaves

test("41 rows, one append past the window: every surviving row keeps its object", () => {
  const t = createCanvasTranscript();
  const first = t.apply(payload(proseWindow("r", 0, 41)));
  assert.equal(first.length, 41);

  const second = t.apply(payload([...proseWindow("r", 1, 40), userRow("r41", "q")]));
  assert.equal(second.length, 41);
  for (let i = 0; i < 40; i++) {
    assert.equal(second[i], first[i + 1], `row r${i + 1} kept its object across the slide`);
  }
  assert.ok(!second.includes(first[0]!), "the departed head's object is not re-used");
});

test("the probe: a new tail row never inherits a surviving row's object", () => {
  const t = createCanvasTranscript();
  const first = t.apply(payload(proseWindow("r", 0, 40)));
  const known = new Set<unknown>(first);

  const second = t.apply(payload([...proseWindow("r", 1, 39), userRow("r40", "q"), textRow("r41", "answer")]));
  assert.equal(second.length, 41);
  // every survivor is still the SAME object (identity survived 39/40)
  for (let i = 0; i < 39; i++) {
    assert.equal(second[i], first[i + 1], `r${i + 1} survived the slide`);
  }
  // the two rows that never existed on the canvas are fresh objects
  assert.ok(!known.has(second[39]), "the persisted tail row (r40) is its own object");
  assert.ok(!known.has(second[40]), "the appended row (r41) is its own object");
  assert.notEqual(second[40], second[39], "the tail row never wears its neighbour's object");
});

test("41 rows of a mixed transcript: thinking and tool rows survive the slide too", () => {
  const t = createCanvasTranscript();
  const head = [
    userRow("m1", "run it"),
    thinkingRow("m2", "hmm"),
    toolCallRow("m3", "c1"),
    // the result row folds onto the call's item, so it renders no row of its own.
    toolResultRow("m4", "c1", "ok"),
  ];
  const filler = proseWindow("f", 0, 37);
  const first = t.apply(payload([...head, ...filler]));
  assert.equal(first.length, 40);

  const second = t.apply(payload([...head.slice(1), ...filler, textRow("m5", "done")]));
  assert.equal(second.length, 40);
  const toolBefore = first.find((i) => i.kind === "tool");
  const toolAfter = second.find((i) => i.kind === "tool");
  assert.ok(toolBefore && toolAfter);
  assert.equal(toolAfter, toolBefore, "the tool row keeps its call-id object");
  for (let i = 0; i < 39; i++) {
    assert.equal(second[i], first[i + 1], `item ${i} survived the slide`);
  }
  assert.ok(!first.includes(second[39]!), "the appended tail row is new");
});

// the id-less case: a live row before the store names it

test("an id-less live tail keeps its slot across a head slide", () => {
  const t = createCanvasTranscript();
  const live = textRow(undefined, "streaming…", true);
  const first = t.apply(payload([...proseWindow("r", 0, 39), live], true));
  assert.equal(first.length, 40);
  const liveBefore = first[39]!;

  const second = t.apply(payload([...proseWindow("r", 1, 38), live], true));
  assert.equal(second.length, 39);
  for (let i = 0; i < 38; i++) {
    assert.equal(second[i], first[i + 1], `persisted row r${i + 1} kept its object`);
  }
  assert.equal(second[38], liveBefore, "the id-less tail kept its slot object");
  assert.equal((second[38] as { content: string }).content, "streaming…", "and carries its live content");
});

test("the live -> persisted transition adopts the slot object (no swap on an id)", async () => {
  const t = createCanvasTranscript();
  const before = t.apply(
    payload([userRow("1", "q"), thinkingRow(undefined, "hmm", true), textRow(undefined, "par", true)], true),
  );

  // REACTIVE-PROXY LAW, re-bound here under the row-id address by. A render effect that
  // read those fields must be invalidated by those writes (vue's default scheduler: the
  // re-run lands on the microtask queue, hence the nextTick below).
  const seen: Array<readonly [string | undefined, string]> = [];
  const reader = effect(() => {
    const item = before[2] as { id?: string; content: string };
    seen.push([item.id, item.content] as const);
  });

  const after = t.apply(
    payload([userRow("1", "q"), thinkingRow("9", "hmm"), textRow("10", "partial")]),
  );
  await nextTick();
  stop(reader);

  assert.equal(after[1], before[1], "the thinking row keeps its object when the store names it");
  assert.equal(after[2], before[2], "the answer row keeps its object when the store names it");
  assert.equal((after[1] as { id?: string }).id, "9");
  assert.equal((after[2] as { id?: string }).id, "10");

  // the in-place writes invalidated the reader, and its LAST reading is the adopted.
  assert.deepEqual(seen[0], [undefined, "par"], "the reader first sees the live row");
  assert.ok(seen.length > 1, `the in-place write invalidated the reader (ran ${seen.length}x)`);
  assert.deepEqual(seen[seen.length - 1], ["10", "partial"], "and it saw the adopted id and the delta");
});

test("a fresh tail row never adopts a live row's object", () => {
  const t = createCanvasTranscript();
  const live = textRow(undefined, "streaming", true);
  const first = t.apply(payload([...proseWindow("r", 0, 39), live], true));
  const liveItem = first[39]!;

  // the live row persists (id r40) and a NEW live row starts after it
  const second = t.apply(
    payload([...proseWindow("r", 1, 39), textRow("r40", "streaming…"), textRow(undefined, "next", true)], true),
  );
  assert.equal(second.length, 41);
  assert.equal(second[39], liveItem, "the persisted row owns the object it streamed in");
  assert.equal((second[39] as { id?: string }).id, "r40");
  assert.notEqual(second[40], liveItem, "the new live row is a fresh object");
  assert.ok(!first.includes(second[40]!), "the new live row re-uses nothing already on the canvas");
});

test("a persisting row behind a live row mints its own object - no collapse", () => {
  const t = createCanvasTranscript();
  const first = t.apply(payload([userRow("1", "q"), textRow(undefined, "live", true)], true));
  const liveItem = first[1]!;
  // the live row is still streaming and a text row persists in the SAME payload. The
  // live row already consumed the shape's ordinal, so the persisting row peeks at the
  // NEXT slot - never at the object the live row is painting into right now. Two rows,
  // two objects: no merge, no overwritten live content.
  const second = t.apply(payload([userRow("1", "q"), textRow(undefined, "live", true), textRow("5", "persisted")]));
  assert.equal(second.length, 3, "both rows paint - no collapse onto one object");
  assert.equal(second[1], liveItem, "the live row keeps the object it streamed in");
  assert.notEqual(second[2], liveItem, "the persisting row mints its own");
  assert.equal((second[1] as { content: string }).content, "live");
  assert.equal((second[2] as { content: string }).content, "persisted");
});

test("a vanished live row leaves no object for a later row of that shape", () => {
  const t = createCanvasTranscript();
  const withLive = t.apply(payload([userRow("1", "q"), thinkingRow(undefined, "hmm", true)], true));
  const liveThink = withLive[1]!;
  t.apply(payload([userRow("3", "q")]));
  // a DIFFERENT, already-persisted thinking row arrives afterwards
  const later = t.apply(payload([userRow("3", "q"), thinkingRow("77", "other")]));
  assert.notEqual(later[1], liveThink, "the vanished row's object is not inherited by an unrelated row");
  assert.equal((later[1] as { content: string }).content, "other");
  assert.equal((later[1] as { id?: string }).id, "77");
});

// the tool rows ride the same address law

// a call row with neither a store id nor a call id.
function anonToolCallRow(): ChatRowWire {
  return {
    role: "assistant",
    status: "streaming",
    blocks: [{ type: "tool_call", tool_call: { name: "shell.run", arguments: {} }, running: true }],
  };
}

test("an id-less tool call is addressed by its slot, never by its index", () => {
  const t = createCanvasTranscript();
  const first = t.apply(payload([...proseWindow("r", 0, 39), anonToolCallRow()], true));
  assert.equal(first.length, 40);
  const callBefore = first[39]!;

  const second = t.apply(payload([...proseWindow("r", 1, 38), anonToolCallRow()], true));
  assert.equal(second.length, 39);
  assert.equal(second[38], callBefore, "the id-less call kept its object across the slide");
});

test("a vanished id-less tool call leaves no object for a later call", () => {
  const t = createCanvasTranscript();
  const first = t.apply(payload([userRow("1", "q"), anonToolCallRow()], true));
  const before = first[1]!;
  t.apply(payload([userRow("1", "q")]));
  const later = t.apply(payload([userRow("1", "q"), anonToolCallRow()]));
  assert.notEqual(later[1], before, "the gone call's object is not inherited");
});

// source pins: the law is in the code, and the old key is gone

test("chat.ts: prose rows are addressed by store id; the positional sig is gone", () => {
  assert.doesNotMatch(
    chatSource,
    /`\$\{row\.role\}:\$\{index\}:\$\{row\.blocks\?\.length \?\? 0\}`/,
    "the positional prose sig must not come back",
  );
  assert.doesNotMatch(chatSource, /\bconst sig = /, "no positional sig variable");
  // the address rule lives in ONE place: omega.ts transcriptAddress,
  // which the REST restore path resolves too (card t_a20fbad7). chat.ts
  // DELEGATES - a local spelling here would let the two paths drift. The
  // shape carries the BLOCK TYPE beside the role: one store row lands as
  // several wire rows (a reasoning block and the answer block share the
  // row id), and an id-only address aliased them into one object whose
  // `kind` flipped on every paint - the reactive write loop of card
  // t_a356fcc7.
  assert.match(chatSource, /transcriptAddress\(row\.id, `\$\{row\.role\}:\$\{block\.type\}`/, "the canvas delegates the store-id address");
  assert.match(chatSource, /transcriptAddress\(undefined, `\$\{row\.role\}:\$\{block\.type\}`, ctx\.ordinals, consume\)/, "and the id-less slot address");
  assert.doesNotMatch(chatSource, /`row:\$\{row\.id\}`/, "no second spelling of the id address");
  assert.doesNotMatch(chatSource, /`live:\$\{shape\}:\$\{ordinal\}`/, "no second spelling of the slot address");
  assert.match(omegaSource, /if \(id\) return `row:\$\{id\}:\$\{shape\}`/, "omega.ts owns the id address");
  assert.match(omegaSource, /`live:\$\{shape\}:\$\{ordinal\}`/, "and the id-less slot address");
  assert.match(omegaSource, /The ordinal counts ID-LESS rows/, "the ordinal's law is stated where it is obeyed");
  // the id-less case is STATED where a reader meets it, not implied
  assert.match(chatSource, /THE ID-LESS CASE, stated/, "the id-less fallback is documented in the contract");
  assert.match(chatSource, /in-flight rows carry none/, "and names the wire's own law");
  // no cache key carries a payload POSITION any more - the tool, verdict
  // and result rows ride the same address law (a call id or the row's own
  // address), so a slide can never hand one row's object to another
  assert.doesNotMatch(chatSource, /`tc:\$\{index\}`/, "no positional call key");
  assert.doesNotMatch(chatSource, /`result:\$\{index\}/, "no positional result key");
});
