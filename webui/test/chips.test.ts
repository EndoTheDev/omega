// omega webui: unit tests for the live chip store (card t_896cf5a8,
// step 5). The pending chip's streaming half: chunks append as they
// land, the producer's truncation marker rides the STREAMING path (not
// only the fill), and a fill settles the tail. The render wiring is
// pinned structurally - the component strings are the honest witness
// that the store reaches the row.
// Run: node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  MAX_LIVE_CHIPS,
  TRUNCATION_MARK,
  chipDisplay,
  chipOf,
  ingestChipFrame,
  liveChips,
  resetLiveChips,
} from "../app/composables/chips.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

function reset() {
  resetLiveChips();
}

test("live chunks append in order - the pending chip grows as the call runs", () => {
  reset();
  ingestChipFrame({ call_id: "c1", tool: "shell.run", text: "line1\n" });
  ingestChipFrame({ call_id: "c1", tool: "shell.run", text: "line2\n" });
  const chip = chipOf("c1");
  assert.ok(chip);
  assert.equal(chip.text, "line1\nline2\n");
  assert.equal(chip.settled, false);
});

test("the truncation marker rides the streaming path, before any fill", () => {
  reset();
  ingestChipFrame({ call_id: "c1", text: "head bytes", truncated: true });
  const chip = chipOf("c1");
  assert.ok(chip);
  assert.equal(chip.settled, false, "the marker fires while the call still runs");
  const display = chipDisplay(chip);
  assert.ok(display.startsWith("head bytes"), "the kept bytes stay");
  assert.ok(display.includes(TRUNCATION_MARK), "the marker is appended on the live tail");
  assert.ok(TRUNCATION_MARK.includes("truncated"), "the marker says what happened");
});

test("a clean tail renders without a marker", () => {
  reset();
  ingestChipFrame({ call_id: "c1", text: "ok" });
  assert.equal(chipDisplay(chipOf("c1")), "ok");
});

test("the fill settles the chip and takes its content when nothing streamed", () => {
  reset();
  ingestChipFrame({ call_id: "c1", fill: true, content: "final output", elapsed_ms: 900 });
  const chip = chipOf("c1");
  assert.ok(chip);
  assert.equal(chip.settled, true);
  assert.equal(chip.text, "final output", "a silent call still shows its result through the fill");
  assert.equal(chipDisplay(chip), "final output");
});

test("a fill after a streamed tail keeps the streamed text (no duplicate result)", () => {
  reset();
  ingestChipFrame({ call_id: "c1", text: "streamed" });
  ingestChipFrame({ call_id: "c1", fill: true, content: "streamed" });
  const chip = chipOf("c1");
  assert.ok(chip);
  assert.equal(chip.text, "streamed", "the accumulated tail is not overwritten");
  assert.equal(chip.settled, true);
});

test("an error fill marks the chip, a timeout rides through", () => {
  reset();
  ingestChipFrame({ call_id: "c1", fill: true, content: "boom", is_error: true, timeout: true });
  const chip = chipOf("c1");
  assert.ok(chip);
  assert.equal(chip.isError, true);
});

test("a frame without a call id is ignored - nothing to attach to", () => {
  reset();
  ingestChipFrame({ call_id: "", text: "x" });
  ingestChipFrame(null as never);
  assert.equal(liveChips.size, 0);
});

test("the store caps at MAX_LIVE_CHIPS, evicting the oldest tail", () => {
  reset();
  for (let i = 0; i < MAX_LIVE_CHIPS + 5; i++) {
    ingestChipFrame({ call_id: `c${i}`, text: `chunk${i}` });
  }
  assert.equal(liveChips.size, MAX_LIVE_CHIPS);
  assert.equal(chipOf("c0"), undefined, "the oldest tail is gone");
});

test("an unknown call id reads nothing (a row with no live tail)", () => {
  reset();
  assert.equal(chipOf("nope"), undefined);
  assert.equal(chipOf(undefined), undefined);
  assert.equal(chipDisplay(undefined), "", "no tail renders no section");
});

// --- the render wiring (a dead store passes every pure test) --------------

// derivationSource is the transcript derivation's own source: from the
// computed's declaration to the next computed. The purity pin reads it.
function derivationSource(source: string): string {
  const start = source.indexOf("const transcript = computed(");
  assert.ok(start >= 0, "the transcript derivation exists");
  const end = source.indexOf("const statusLine", start);
  assert.ok(end > start, "the derivation sits ahead of the status line");
  return source.slice(start, end);
}

test("the row reads its own live tail from the store, keyed by the call id", () => {
  const chatItem = src("components/ChatItem.vue");
  assert.ok(chatItem.includes(':call-id="item.callId"'), "ChatItem forwards the call id");
  const line = src("components/chat/ToolCallLine.vue");
  assert.ok(line.includes("chipOf(props.callId)"), "the row reads its own chip by call id");
  assert.ok(line.includes("chipDisplay("), "the truncation marker rides the row's read");
  assert.ok(line.includes('v-else-if="liveText"'), "the row renders the tail while it streams");
  assert.ok(line.includes('liveSettled ? "output" : "live output"'), "the label names the live half");
  assert.ok(
    line.includes('v-if="result"') && line.indexOf('v-if="result"') < line.indexOf('v-else-if="liveText"'),
    "the settled result wins over the tail",
  );
});

// card t_64d14d2b: the derivation is PURE. It used to write
// it.liveText/it.liveSettled while reading the chip store, so every landed
// chunk re-derived the transcript, the blocks and the activity line - and a
// write inside a getter can re-arm the effects that read it (t_a356fcc7's
// spin class). The tail is read at the row instead.
test("the transcript derivation is a pure projection - no writes, no chip read", () => {
  const chatTab = src("components/ChatTab.vue");
  const body = derivationSource(chatTab);
  assert.equal(body.includes("chipOf"), false, "the derivation does not read the chip store");
  assert.equal(body.includes("chipDisplay"), false, "the derivation does not merge a tail");
  const writes = body
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith("//"))
    .filter((l) => !/^(const|let|var)\b/.test(l))
    .filter((l) => /(^|[^=!<>+\-*/%&|^])=(?![=>])/.test(l) || /\+\+|--/.test(l));
  assert.deepEqual(writes, [], "an assignment inside a computed re-arms its own readers");
  assert.equal(chatTab.includes("it.liveText"), false, "the retired merge target is gone");
  assert.equal(
    src("composables/omega.ts").includes("liveText"),
    false,
    "the item carries no live-tail field",
  );
});

test("the tool call id reaches the row from both render paths", () => {
  const omega = src("composables/omega.ts");
  const chat = src("composables/chat.ts");
  assert.ok(omega.includes("callId: cid || undefined"), "the restore path stamps the call id");
  assert.ok(chat.includes("it.callId = callId"), "the canvas panel path stamps the call id");
});
