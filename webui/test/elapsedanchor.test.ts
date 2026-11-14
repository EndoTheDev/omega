// omega webui: the elapsed meter's remount anchor, pinned end to end
// (card t_95385aba). Endo's report: switching tabs or reloading while
// an answer streamed reset the duration label to zero, while tool rows
// survived the same event. The asymmetry was the defect - the tool row
// handed useElapsed an initialMs (the server's elapsed-so-far) and the
// answer/thinking rows did not.
//
// The pure halves live in transcript.test.ts (the restore mapping) and
// omega.test.ts (the panel mapping + the normalizer). What is left is
// the WIRING, which node --test cannot render: these pins hold the
// three links of the chain structurally (the toolrows.test.ts
// convention) - the item fields reach the components, the components
// declare the prop, and each one hands it to the meter as initialMs.
// Run: node --test webui/test/ (node >= 23 runs TS)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

const chatItem = src("components/ChatItem.vue");
const message = src("components/chat/ChatMessage.vue");
const thinking = src("components/chat/ThinkingBlock.vue");
const toolLine = src("components/chat/ToolCallLine.vue");

// --- link 1: ChatItem hands every metered row its anchor ---

test("ChatItem binds :elapsed-ms on all three metered rows", () => {
  // one binding per child component: answer, thinking, tool call. A
  // row that loses its binding falls back to mounting at zero.
  const bindings = chatItem.match(/:elapsed-ms="item\.elapsedMs"/g) ?? [];
  assert.equal(bindings.length, 3, "answer, thinking and tool rows each carry the anchor");
  for (const child of ["<ChatMessage", "<ThinkingBlock", "<ToolCallLine"]) {
    const at = chatItem.indexOf(child);
    assert.ok(at >= 0, `${child} renders`);
    const blockEnd = chatItem.indexOf("/>", at);
    const block = chatItem.slice(at, blockEnd);
    assert.match(block, /:elapsed-ms="item\.elapsedMs"/, `${child} receives the anchor`);
    assert.match(block, /:final-ms="item\.finalMs"/, `${child} keeps the final span too`);
  }
});

// --- link 2 + 3: the components declare the prop and pass it ---

for (const [name, source] of [
  ["ChatMessage", message],
  ["ThinkingBlock", thinking],
  ["ToolCallLine", toolLine],
] as const) {
  test(`${name} declares elapsedMs and anchors useElapsed with it`, () => {
    assert.match(source, /elapsedMs\?: number;/, `${name} declares the prop`);
    // the third useElapsed argument is the initialMs getter - the
    // whole point of the card is that it is not omitted (comments
    // may sit between the arguments, so read the call's own text)
    const at = source.indexOf("useElapsed(");
    assert.ok(at >= 0, `${name} runs the shared meter`);
    const call = source.slice(at, source.indexOf(");", at));
    assert.match(call, /\(\) => props\.elapsedMs \?\? 0/, `${name} passes the elapsed-so-far as initialMs`);
    assert.equal(call.match(/\(\) =>/g)?.length, 3, `${name} passes all three arguments`);
  });
}

// The meter's own contract: an omitted/undefined offset must never
// produce NaN arithmetic (the `startedAt = now() - undefined` shape).
test("useElapsed documents the initialMs contract", () => {
  const meter = src("composables/useElapsed.ts");
  assert.match(meter, /initialMs: \(\) => number = \(\) => 0/, "a missing getter falls back to zero");
  assert.match(meter, /initialMs is the elapsed already consumed at start/);
});
