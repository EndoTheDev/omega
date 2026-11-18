import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
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
import { isLive, isStreaming, isYielded } from "../app/composables/toolStatus.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// walk: every source file under app/.
function walk(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(p));
    else if (/\.(ts|vue)$/.test(entry.name)) out.push(p);
  }
  return out;
}

// renderTranscriptItems: the pending derivation ---------------------

test("a call without its result row renders as the pending chip", () => {
  const items: TranscriptWireItem[] = [
    { kind: "user", content: "run it" },
    { kind: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: { command: "sleep 3" } } },
  ];
  const t = renderTranscriptItems(items).find((i) => i.kind === "tool") as { status: string };
  assert.ok(t, "the tool item renders");
  assert.equal(t.status, "pending", "no result row -> pending, never a done orphan");
});

test("a settled call renders done - its result row lands in the same transcript", () => {
  const items: TranscriptWireItem[] = [
    { kind: "user", content: "run it" },
    { kind: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } },
    { kind: "tool_result", tool_call_id: "c1", content: "ok" },
  ];
  const t = renderTranscriptItems(items).find((i) => i.kind === "tool") as { status: string };
  assert.equal(t.status, "done");
});

test("still-pending siblings render pending beside a settled one", () => {
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

// transcriptHasPending: the watcher's arming truth -------------------

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

// the canvas transcript: pending chip + identity-preserving fill ---

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

// the pending chip's clock ------------------ A yielded call emits NO delta.

test("a pending chip meters from started_at, not from the payload's build instant", () => {
  const started = new Date(Date.now() - 12_000).toISOString();
  const ct = createCanvasTranscript();
  const items = ct.apply({
    session: "s1",
    running: false,
    rows: [
      wireRow("2", "assistant", {
        type: "tool_call",
        tool_call: { id: "c1", name: "shell.run", arguments: { command: "sleep 30" } },
        // the build-time duration is 2s stale by law (the payload is
        // the yield flush) - the stamp must outrank it
        elapsed_ms: 500,
        started_at: started,
      }),
    ],
  });
  const tool = items.find((i) => i.kind === "tool") as { status: string; elapsedMs?: number };
  assert.equal(tool.status, "pending");
  assert.ok(
    tool.elapsedMs !== undefined && tool.elapsedMs >= 11_500 && tool.elapsedMs < 12_500,
    `the meter anchors on the call's true elapsed (got ${tool.elapsedMs})`,
  );
});

test("a dock-tab rebuild of the SAME cached payload keeps the meter monotonic", () => {
  const started = new Date(Date.now() - 12_000).toISOString();
  const payload = {
    session: "s1",
    running: false,
    rows: [
      wireRow("2", "assistant", {
        type: "tool_call",
        tool_call: { id: "c1", name: "shell.run", arguments: {} },
        started_at: started,
      }),
    ],
  };
  const first = createCanvasTranscript()
    .apply(payload)
    .find((i) => i.kind === "tool") as { elapsedMs?: number };
  const firstAnchor = first.elapsedMs ?? -1;
  assert.ok(firstAnchor >= 11_500, `anchored on the call's start (got ${firstAnchor})`);
  const realNow = Date.now;
  try {
    Date.now = () => realNow() + 7_000;
    const second = createCanvasTranscript()
      .apply(payload)
      .find((i) => i.kind === "tool") as { elapsedMs?: number };
    assert.ok(
      second.elapsedMs !== undefined && second.elapsedMs >= firstAnchor + 6_500,
      `the rebuilt meter counts on (${firstAnchor} -> ${second.elapsedMs}), never restarts`,
    );
  } finally {
    Date.now = realNow;
  }
});

test("a stamp-less pending row keeps the old shape (no meter offset)", () => {
  const ct = createCanvasTranscript();
  const items = ct.apply({
    session: "s1",
    running: false,
    rows: [
      wireRow("2", "assistant", { type: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } }),
    ],
  });
  const tool = items.find((i) => i.kind === "tool") as { status: string; elapsedMs?: number };
  assert.equal(tool.status, "pending");
  assert.equal(tool.elapsedMs, undefined, "nothing to anchor on - documented degradation");
});

// handlePayload: the agent_end finish_reason reaches the transport --

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

// the open-state laws keep a chip visible ---------------------------

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

// one live-set predicate: the consumers cannot drift apart -------------- (audit. These
// tests are the AGREEMENT between consumers, never a second definition of the set.

// The status vocabulary the two mappers can mint (omega.ts renderTranscriptItems. The
// predicate must answer NARROWLY for an unknown value instead of inheriting a stale
// default.
const STATUSES = ["running", "pending", "done", "error", "waiting"];

test("the live set is exactly running | pending, for every status value", () => {
  for (const s of STATUSES) {
    assert.equal(isLive(s), s === "running" || s === "pending", `isLive(${s})`);
  }
  assert.deepEqual(
    STATUSES.filter(isLive),
    ["running", "pending"],
    "a new status is not live until toolStatus.ts says so",
  );
});

test("isStreaming is the named running-only half, and streaming implies live", () => {
  for (const s of STATUSES) {
    assert.equal(isStreaming(s), s === "running", `isStreaming(${s})`);
    if (isStreaming(s)) assert.equal(isLive(s), true, `streaming implies live (${s})`);
  }
});

test("every consumer agrees with the predicate for every status value", () => {
  for (const s of STATUSES) {
    // the row's auto-open law (toolDisplay.ts): a live row opens.
    assert.equal(toolRowWantOpen(s, "auto"), isLive(s), `row auto-open ${s}`);
    assert.equal(toolRowWantOpen(s, "expanded"), true, `expanded ${s}`);
    assert.equal(toolRowWantOpen(s, "collapsed"), false, `collapsed ${s}`);
    assert.equal(toolGroupWantOpen(isLive(s), false, "auto"), isLive(s), `group open ${s}`);
    // the wake watcher arms on the yielded half, and the two named halves
    // partition the live set exactly (no status is both, none is neither)
    assert.equal(isYielded(s), s === "pending", `isYielded(${s})`);
    assert.equal(
      isStreaming(s) !== isYielded(s),
      isLive(s),
      `streaming | yielded === live (${s})`,
    );
  }
});

test("all five consumers import the predicate instead of spelling the set", () => {
  const sites: [string, RegExp][] = [
    ["components/chat/ToolCallLine.vue", /from "\.\.\/\.\.\/composables\/toolStatus"/],
    ["components/chat/ToolGroup.vue", /from "\.\.\/\.\.\/composables\/toolStatus"/],
    ["composables/toolDisplay.ts", /from "\.\/toolStatus\.ts"/],
    ["composables/chat.ts", /from "\.\/toolStatus\.ts"/],
    ["composables/omega.ts", /from "\.\/toolStatus\.ts"/],
  ];
  for (const [file, imp] of sites) {
    assert.match(src(file), imp, `${file} imports toolStatus`);
  }
  // each consumer's CALL SITE names the answer it needs.
  assert.match(
    src("components/chat/ToolCallLine.vue"),
    /useElapsed\(\s*\(\) => props\.finalMs,\s*\(\) => isLive\(props\.status\)/,
    "the row's elapsed meter takes the live set",
  );
  assert.match(
    src("components/chat/ToolGroup.vue"),
    /anyActive = computed\(\(\) => props\.items\.some\(\(it\) => isLive\(it\.status\)\)\)/,
    "the group aggregate takes the live set",
  );
  assert.match(
    src("composables/chat.ts"),
    /if \(isLive\(it\.status\)\) \{\s*const anchor = toolCallAnchorMs/,
    "the paint-time anchor takes the live set",
  );
  assert.match(
    src("composables/chat.ts"),
    /item\?\.kind === "tool" &&\s*isStreaming\(item\.status\)/,
    "the activity phase takes the streaming-only half, by name",
  );
  assert.match(
    src("composables/omega.ts"),
    /it\.kind === "tool" && isStreaming\(it\.status\) && it\.name === "shell\.run"/,
    "the gate binding takes the streaming-only half, by name",
  );
  assert.match(
    src("composables/omega.ts"),
    /if \(it\.kind === "tool"\) return isYielded\(it\.status\)/,
    "the wake watcher takes the yielded half, by name (awaitsWake: the tool chip AND the pending verdict card)",
  );
});

test("no file under app/ spells the live set inline any more", () => {
  // The pair, in ANY order and any container.
  const pair = new RegExp(
    `["'](?:running|pending)["'][\\s\\S]{0,60}["'](?:running|pending)["']`,
    "g",
  );
  // The two MAPPERS mint the word as a ternary yield (`cond ? A producer is not a
  // classifier: a classification never yields the word straight out of a `?`.
  const yieldsWord = /\?\s*$/;
  const offenders: string[] = [];
  for (const f of walk(root)) {
    if (f.endsWith("toolStatus.ts")) continue;
    const text = readFileSync(f, "utf8");
    for (const m of text.matchAll(pair)) {
      const start = m.index ?? 0;
      // the 8 chars before the first literal decide: `?
      const prefix = text.slice(Math.max(0, start - 8), start);
      if (yieldsWord.test(prefix)) continue;
      offenders.push(`${f.slice(root.length + 1)}:${text.slice(0, start).split("\n").length}`);
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `toolStatus.ts is the only spelling of the live set (offenders: ${offenders.join(", ")})`,
  );
});

// structural wiring: the render side carries the shapes -------------

test("ToolCallLine renders the pending word and meters on the live predicate", () => {
  const vue = src("components/chat/ToolCallLine.vue");
  assert.ok(vue.includes(`v-if="status === 'pending'"`), "the pending label renders");
  assert.match(vue, /import \{ isLive \} from "\.\.\/\.\.\/composables\/toolStatus"/);
  assert.ok(vue.includes("() => isLive(props.status)"), "the elapsed meter runs while the chip pends");
});

test("ToolGroup keeps the wrench active for pending children", () => {
  const vue = src("components/chat/ToolGroup.vue");
  assert.match(vue, /import \{ isLive \} from "\.\.\/\.\.\/composables\/toolStatus"/);
  assert.ok(vue.includes("isLive(it.status)"), "the aggregate reads the shared live set");
  assert.ok(!vue.includes("anyPending"), "no per-status aggregate is left to drift");
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
  assert.ok(ts.includes("runActive("), "the run-slot probe rides the arming decision");
});

test("the wake is a runs-topic edge - the 2s slot poll is gone (card t_896cf5a8)", () => {
  const ts = src("composables/chat.ts");
  assert.ok(!ts.includes("PENDING_POLL_MS"), "no poll interval constant remains");
  assert.ok(!ts.includes("sleepMs("), "no sleep-loop remains");
  assert.ok(ts.includes("function wakePending("), "the edge wakes the waiter");
  assert.ok(ts.includes("function ridePending("), "the wake attaches and rides");
  assert.ok(
    /noteRunState\(session: string, active: boolean\)[\s\S]{0,400}if \(active\) wakePending\(session\)/.test(ts),
    "the runs topic's active edge is the wake signal",
  );
  assert.ok(ts.includes("disarmPending(key)"), "a closed state drops its waiter");
  const app = src("app.vue");
  assert.ok(app.includes("host.noteRunState(session, active)"), "the shell feeds the edge into the host");
});
