// omega webui: unit tests for the async pending chips (agent loop
// phase 3, rulings 6/7/8 - result-at-once v1). The pure halves are
// tested directly: the transcript restore's pending derivation, the
// canvas transcript's pending chip + identity-preserving fill, the
// agent_end finish_reason reaching the transport (the watcher's
// arming signal), and the open-state laws that keep a chip visible.
// The watcher's live poll-attach loop and the browser-level wake
// witness are pinned by recipe 104 (scripts/journeys) - the browser
// is the honest witness there, the node suite pins the shapes here.
// Run: node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  renderTranscriptItems,
  transcriptHasPending,
  handlePayload,
  StreamInterruptedError,
  type TranscriptWireItem,
  type StreamHandlers,
  type ChatRowWire,
  type ChatBlockWire,
} from "../app/composables/omega.ts";
import { createCanvasTranscript } from "../app/composables/chat.ts";
import {
  toolRowWantOpen,
  type ToolDisplay,
} from "../app/composables/toolDisplay.ts";
import { toolGroupWantOpen } from "../app/composables/toolGroup.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// --- renderTranscriptItems: the pending derivation ------------------------

test("a call without its result row renders as the pending chip", () => {
  const items: TranscriptWireItem[] = [
    { kind: "user", content: "run it" },
    { kind: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: { command: "sleep 3" } } },
  ];
  const [t] = renderTranscriptItems(items) as { kind: string; status: string }[];
  assert.ok(t, "the tool item renders");
  assert.equal(t.status, "pending", "no result row -> pending, never a done orphan");
});

test("a settled call renders done - its result row lands in the same transcript", () => {
  const items: TranscriptWireItem[] = [
    { kind: "user", content: "run it" },
    { kind: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } },
    { kind: "tool_result", tool_call_id: "c1", content: "ok" },
  ];
  const [t] = renderTranscriptItems(items) as { status: string }[];
  assert.equal(t.status, "done");
});

test("still-pending siblings render pending beside a settled one", () => {
  // two yield-class calls in one turn: the first result re-entered,
  // the second still awaits its wake - history shows both truthfully
  const items: TranscriptWireItem[] = [
    { kind: "user", content: "run both" },
    { kind: "tool_call", tool_call: { id: "c1", name: "delegate.task", arguments: {} } },
    { kind: "tool_call", tool_call: { id: "c2", name: "delegate.task", arguments: {} } },
    { kind: "tool_result", tool_call_id: "c1", content: "first answer" },
  ];
  const tools = renderTranscriptItems(items).filter((i) => i.kind === "tool");
  assert.equal(tools.length, 2);
  assert.equal((tools[0] as { status: string }).status, "done");
  assert.equal((tools[1] as { status: string }).status, "pending", "the unresolved sibling chips");
});

test("the live tail's running bit outranks the pending derivation", () => {
  const items: TranscriptWireItem[] = [
    { kind: "tool_call", running: true, elapsed_ms: 4, tool_call: { id: "c1", name: "shell.run", arguments: {} } },
  ];
  const [t] = renderTranscriptItems(items) as { status: string; elapsedMs?: number }[];
  assert.equal(t.status, "running");
  assert.equal(t.elapsedMs, 4, "the restored still-running meter offset rides");
});

test("an id-less call keeps the done shape - nothing to match a result against", () => {
  const items: TranscriptWireItem[] = [
    { kind: "tool_call", tool_call: { name: "shell.run", arguments: {} } },
  ];
  const [t] = renderTranscriptItems(items) as { status: string }[];
  assert.equal(t.status, "done");
});

// --- transcriptHasPending: the watcher's arming truth ----------------------

test("transcriptHasPending reads the rendered items", () => {
  const pending = renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } },
  ]);
  const settled = renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } },
    { kind: "tool_result", tool_call_id: "c1", content: "ok" },
  ]);
  assert.equal(transcriptHasPending(pending), true);
  assert.equal(transcriptHasPending(settled), false);
  assert.equal(transcriptHasPending([]), false);
});

// --- the canvas transcript: pending chip + identity-preserving fill ------

const wireRow = (
  id: string,
  role: string,
  block: ChatBlockWire,
  streaming = false,
): ChatRowWire => ({
  id,
  role,
  status: streaming ? "streaming" : "complete",
  blocks: [block],
});

test("the exit flush of a yielded turn paints the pending chip", () => {
  const ct = createCanvasTranscript();
  // the run ended (running=false): the assistant row carries the
  // dispatched call, no result row follows - the panel's exit-flush
  // shape of a yielded turn
  const items = ct.apply({
    session: "s1",
    running: false,
    rows: [
      wireRow("1", "user", { type: "text", content: "run it" }),
      wireRow("2", "assistant", { type: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: { command: "sleep 3" } } }),
    ],
  });
  const tool = items.find((i) => i.kind === "tool") as { status: string };
  assert.ok(tool, "the call row renders");
  assert.equal(tool.status, "pending", "ruling 6: the chip, not an orphaned done line");
});

test("the wake turn fills the SAME chip in place", () => {
  const ct = createCanvasTranscript();
  const before = ct.apply({
    session: "s1",
    running: false,
    rows: [
      wireRow("2", "assistant", { type: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } }),
    ],
  });
  const chip = before.find((i) => i.kind === "tool");
  assert.ok(chip);
  // the wake run's first delta: the yield_result row (render role
  // tool) lands right after the call, the panel runs again
  const after = ct.apply({
    session: "s1",
    running: true,
    rows: [
      wireRow("2", "assistant", { type: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } }),
      wireRow("3", "tool", { type: "tool_result", tool_call_id: "c1", content: "ASYNC-DONE" }),
    ],
  });
  const filled = after.find((i) => i.kind === "tool");
  assert.strictEqual(filled, chip, "identity preserved - the chip fills, never remounts");
  const t = filled as { status: string; result?: string };
  assert.equal(t.status, "done");
  assert.equal(t.result, "ASYNC-DONE");
});

test("the live tail's streaming block stays running", () => {
  const ct = createCanvasTranscript();
  const items = ct.apply({
    session: "s1",
    running: true,
    rows: [
      wireRow("2", "assistant", { type: "tool_call", running: true, tool_call: { id: "c1", name: "shell.run", arguments: {} } }, true),
    ],
  });
  const tool = items.find((i) => i.kind === "tool") as { status: string };
  assert.equal(tool.status, "running", "an in-flight block never chips as pending");
});

test("canvas siblings: settled beside pending in one payload", () => {
  const ct = createCanvasTranscript();
  const items = ct.apply({
    session: "s1",
    running: false,
    rows: [
      wireRow("2", "assistant", { type: "tool_call", tool_call: { id: "c1", name: "delegate.task", arguments: {} } }),
      wireRow("3", "assistant", { type: "tool_call", tool_call: { id: "c2", name: "delegate.task", arguments: {} } }),
      wireRow("4", "tool", { type: "tool_result", tool_call_id: "c1", content: "first answer" }),
    ],
  });
  const tools = items.filter((i) => i.kind === "tool") as { status: string }[];
  assert.equal(tools.length, 2);
  assert.equal(tools[0]!.status, "done");
  assert.equal(tools[1]!.status, "pending", "the still-pending sibling chips in history");
});

// --- handlePayload: the agent_end finish_reason reaches the transport -----

test("agent_end yields its finish_reason to onRunEnd - the watcher's arming signal", () => {
  const seen: string[] = [];
  const handlers: StreamHandlers = { onRunEnd: (fr) => seen.push(fr) };
  const state: { sawEnd?: boolean } = {};
  handlePayload({ type: "agent_end", finish_reason: "yielded" }, handlers, state);
  assert.deepEqual(seen, ["yielded"]);
  assert.equal(state.sawEnd, true);
  handlePayload({ type: "agent_end", finish_reason: "stop" }, handlers, state);
  assert.deepEqual(seen, ["yielded", "stop"]);
});

test("a cancelled agent_end still reports its finish before the interrupt throw", () => {
  const seen: string[] = [];
  const handlers: StreamHandlers = { onRunEnd: (fr) => seen.push(fr) };
  assert.throws(
    () => handlePayload({ type: "agent_end", finish_reason: "cancelled", error: "context canceled" }, handlers, {}),
    StreamInterruptedError,
    "the cancel path keeps its interrupt semantics",
  );
  assert.deepEqual(seen, ["cancelled"]);
});

test("stream_end usage keeps its onUsage consumer (no transport regression)", () => {
  let usage: { promptTokens: number; completionTokens: number } | null = null;
  const handlers: StreamHandlers = {
    onUsage: (u) => { usage = u; },
  };
  handlePayload({ type: "stream_end", prompt_eval_count: 10, eval_count: 5 }, handlers, {});
  assert.deepEqual(usage, { promptTokens: 10, completionTokens: 5, cachedTokens: 0 });
});

// --- the open-state laws keep a chip visible ------------------------------

test("auto opens a pending row - the chip is the visible half of the contract", () => {
  assert.equal(toolRowWantOpen("pending", "auto" as ToolDisplay), true);
  assert.equal(toolRowWantOpen("running", "auto" as ToolDisplay), true);
  assert.equal(toolRowWantOpen("done", "auto" as ToolDisplay), false);
  assert.equal(toolRowWantOpen("pending", "collapsed" as ToolDisplay), false);
  assert.equal(toolRowWantOpen("pending", "expanded" as ToolDisplay), true);
});

test("a group with a pending child stays open in auto", () => {
  // the caller passes the combined active flag (running or pending)
  assert.equal(toolGroupWantOpen(true, false, "auto"), true, "pending child, idle panel");
  assert.equal(toolGroupWantOpen(false, false, "auto"), false, "all settled");
  assert.equal(toolGroupWantOpen(true, true, "auto"), true);
  assert.equal(toolGroupWantOpen(true, false, "collapsed"), false);
});

// --- structural wiring: the render side carries the shapes ----------------

test("ToolCallLine renders the pending word and ticks the meter for pending", () => {
  const vue = src("components/chat/ToolCallLine.vue");
  assert.ok(vue.includes(`v-if="status === 'pending'"`), "the pending label renders");
  assert.ok(
    vue.includes(`props.status === "running" || props.status === "pending"`),
    "the elapsed meter runs while the chip pends",
  );
});

test("ToolGroup keeps the wrench active for pending children", () => {
  const vue = src("components/chat/ToolGroup.vue");
  assert.ok(vue.includes("anyPending"), "the aggregate state reads pending children");
  assert.ok(vue.includes("anyActive"), "the open law reads the combined active flag");
});

test("the chat host arms its pending watcher off yielded runs and restored pending chips", () => {
  const ts = src("composables/chat.ts");
  assert.ok(ts.includes("watchPending"), "the watcher exists");
  assert.ok(
    ts.includes(`st.lastFinishReason === "yielded"`),
    "a yielded agent_end arms the watch",
  );
  assert.ok(
    ts.includes("transcriptHasPending(st.restored)"),
    "a restored pending chip arms the watch on load",
  );
  assert.ok(ts.includes("runActive("), "the run-slot probe rides the attach decision");
});
