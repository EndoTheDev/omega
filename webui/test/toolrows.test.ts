// omega webui: unit tests for the /copy parity action and the tool-rows
// auto mode (card t_868633b3). The pure logic (mode parsing, the
// auto open/collapse state machine) is tested directly; the component
// wiring (raw-content clipboard payload, the manual-pin override, the
// settings knob) is pinned structurally, the composition-test
// convention. Run: node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  parseToolDisplay,
  toolRowWantOpen,
  DEFAULT_TOOL_DISPLAY,
  TOOL_DISPLAY_KEY,
} from "../app/composables/toolDisplay.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// --- parseToolDisplay: the localStorage round-trip ---

test("parseToolDisplay accepts only the two non-default modes", () => {
  assert.equal(parseToolDisplay("expanded"), "expanded");
  assert.equal(parseToolDisplay("collapsed"), "collapsed");
  assert.equal(parseToolDisplay("auto"), "auto");
});

test("parseToolDisplay falls back to auto on missing/tampered values", () => {
  assert.equal(DEFAULT_TOOL_DISPLAY, "auto");
  assert.equal(parseToolDisplay(null), "auto");
  assert.equal(parseToolDisplay(undefined), "auto");
  assert.equal(parseToolDisplay(""), "auto");
  assert.equal(parseToolDisplay("Auto"), "auto", "case-sensitive like parseAppearance");
  assert.equal(parseToolDisplay({ auto: true }), "auto");
});

// --- toolRowWantOpen: the auto state machine ---

test("expanded/collapsed force one state regardless of status", () => {
  for (const status of ["running", "done", "error", "unknown-status"]) {
    assert.equal(toolRowWantOpen(status, "expanded"), true, `expanded ${status}`);
    assert.equal(toolRowWantOpen(status, "collapsed"), false, `collapsed ${status}`);
  }
});

test("auto expands a running row, collapses a settled one", () => {
  assert.equal(toolRowWantOpen("running", "auto"), true);
  assert.equal(toolRowWantOpen("done", "auto"), false);
  assert.equal(toolRowWantOpen("error", "auto"), false);
  // anything not literally running is settled - a live merge should
  // keep statuses in the known set, and a stray value errs closed
  assert.equal(toolRowWantOpen("", "auto"), false);
});

// --- structural wiring: the card's must-haves in the components ---

test("the shared mode key persists under omega.tool-display", () => {
  assert.equal(TOOL_DISPLAY_KEY, "omega.tool-display");
});

test("ToolCallLine pins a manual toggle and recomputes around it", () => {
  const t = src("components/chat/ToolCallLine.vue");
  // the element must be reachable: the details ref is the anchor every
  // apply path reads - without it applyAuto no-ops forever
  assert.match(t, /<details\s+ref="el"/);
  // the pin: a summary click records the post-toggle state and auto
  // recompute (mount + status/mode watcher) skips pinned rows
  assert.match(t, /@click="onSummaryClick"/);
  assert.match(t, /pinned\.value = !el\.value\.open/);
  assert.match(t, /applyAuto\(\) \{\s*\n\s*if \(pinned\.value !== null \|\| !el\.value\) return/);
  // the state machine rides the shared composable, not a private copy
  assert.match(t, /useToolDisplay\(\)/);
  assert.match(t, /toolRowWantOpen\(props\.status, display\.value\)/);
});

test("ChatMessage copies the RAW message content, never rendered HTML", () => {
  const m = src("components/chat/ChatMessage.vue");
  // the ONE copy path (card t_a92450ea): the helper, awaited, verdict-
  // branched - never a direct navigator.clipboard call that voids its
  // rejection in insecure contexts
  assert.match(m, /const raw = await copyText\(props\.content\)/);
  assert.match(m, /if \(!raw\) \{/);
  assert.doesNotMatch(m, /navigator\.clipboard/);
  // the copy glyph must not bubble into bubble-level handlers
  assert.match(m, /@click\.stop="copyMessage"/);
  // the payload is the source prop - if someone swaps in mdEl innerText,
  // rendered HTML leaks into the clipboard
  assert.doesNotMatch(m, /innerText|innerHTML/);
});

test("the settings knob is a client chat select with the three modes", () => {
  const s = src("components/settings/SettingsTab.vue");
  assert.match(s, /key: "chat\.tool_display"/);
  assert.match(s, /options: \["auto", "expanded", "collapsed"\]/);
  assert.match(s, /emit\("update:toolDisplay", value as ToolDisplay\)/);
  const a = src("app.vue");
  // v-model binds the SHARED ref so the knob writes the signal rows read
  assert.match(a, /v-model:tool-display="toolDisplay"/);
  assert.match(a, /const toolDisplay = useToolDisplay\(\)/);
  assert.match(a, /localStorage\.setItem\(TOOL_DISPLAY_KEY, mode\)/);
});
// --- the smart gate's decision record (card t_1b3e2a8b) ---
// The gate's verdict stamp rides the tool_result; ToolCallLine renders
// it on the call's own row, verbatim. Layers:
// - pure: both wire sources (transcript restore + canvas panel rows)
//   carry the stamp onto the tool item, absent stays absent;
// - source pins: ToolCallLine renders the line inside the call's entry
//   (a record, no buttons), ChatItem passes it through.

const GATE_LINES = [
  "smart gate: auto-approved",
  "smart gate: auto-denied",
  "smart gate: escalated -> approved",
  "smart gate: escalated -> denied - command skipped",
  "smart gate: escalated -> expired",
  "smart gate: escalated -> cancelled",
];

test("renderTranscriptItems rides the gate stamp onto the call row, verbatim", async () => {
  const omega = await import("../app/composables/omega.ts");
  for (const line of GATE_LINES) {
    const items = omega.renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "g1", name: "shell.run", arguments: { command: "x" } } },
      { kind: "tool_result", tool_call_id: "g1", content: "out", is_error: false, gate: line },
    ]);
    const t = items[0];
    assert.ok(t.kind === "tool");
    assert.equal(t.gate, line);
  }
  // no stamp: the field stays absent - no line, nothing rendered
  const bare = omega.renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "g2", name: "shell.run", arguments: { command: "x" } } },
    { kind: "tool_result", tool_call_id: "g2", content: "out", is_error: false },
  ]);
  assert.ok(bare[0].kind === "tool");
  assert.equal(bare[0].gate, undefined);
});

test("the canvas panel path rides the gate stamp onto the call row", async () => {
  const chat = await import("../app/composables/chat.ts");
  const omega = await import("../app/composables/omega.ts");
  const canvas = chat.createCanvasTranscript();
  for (const line of GATE_LINES) {
    canvas.reset && canvas.reset();
    const payload = omega.normalizeChatPayload({
      rows: [
        { role: "assistant", status: "complete", blocks: [{ type: "tool_call", tool_call: { id: "g1", name: "shell.run", arguments: { command: "x" } } }] },
        { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "g1", content: "out", gate: line }] },
      ],
    });
    assert.ok(payload);
    const items = canvas.apply(payload);
    // one tool row (the result merges onto the call item); the stamp
    // rides the merge
    assert.deepEqual(items.map((i) => i.kind), ["tool"]);
    const t = items[0];
    assert.ok(t.kind === "tool");
    assert.equal(t.gate, line);
  }
  // without a stamp: nothing
  const canvas2 = chat.createCanvasTranscript();
  const bare = omega.normalizeChatPayload({
    rows: [
      { role: "assistant", status: "complete", blocks: [{ type: "tool_call", tool_call: { id: "g2", name: "shell.run", arguments: { command: "x" } } }] },
      { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "g2", content: "out" }] },
    ],
  });
  const items = canvas2.apply(bare);
  const t = items[0];
  assert.ok(t.kind === "tool");
  assert.equal(t.gate, undefined);
});

test("ToolCallLine renders the gate record inside the call's entry; ChatItem passes it", () => {
  const line = src("components/chat/ToolCallLine.vue");
  // the record renders from the stamp prop, verbatim - a dim one-line
  // record inside the guarded call's entry, no buttons after the fact
  assert.match(line, /gate\?: string/);
  assert.match(line, /v-if="gate"\s+class="tool-gate">\{\{ gate \}\}/);
  // no interactive affordance on the record line
  assert.doesNotMatch(line, /gate.*@click|button[^\n]*gate/i);
  const item = src("components/ChatItem.vue");
  assert.match(item, /:gate="item\.gate"/);
});
