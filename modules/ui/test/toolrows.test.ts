// omega webui: unit tests for the /copy parity action and the tool-rows
// auto mode (card t_868633b3). The pure logic (mode parsing, the
// auto open/collapse state machine) is tested directly; the component
// wiring (raw-content clipboard payload, the manual-pin override, the
// settings knob) is pinned structurally, the composition-test
// convention. Run: node --test modules/ui/test/ (node >= 23)
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

test("auto keeps an interrupted row collapsed - a dead stream never delivers its result", () => {
  assert.equal(toolRowWantOpen("running", "auto", true), false);
  assert.equal(toolRowWantOpen("running", "auto", false), true);
  // pin-independent: the interrupted flag never overrides a hard mode
  assert.equal(toolRowWantOpen("running", "expanded", true), true);
  assert.equal(toolRowWantOpen("running", "collapsed", true), false);
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
  assert.match(t, /toolRowWantOpen\(props\.status, display\.value, props\.interrupted\)/);
});

test("ChatMessage copies the RAW message content, never rendered HTML", () => {
  const m = src("components/chat/ChatMessage.vue");
  assert.match(m, /navigator\.clipboard\.writeText\(props\.content\)/);
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