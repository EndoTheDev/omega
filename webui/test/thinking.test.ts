// omega webui: unit tests for the thinking display mode (card
// t_428ed4fb). The pure logic (mode parsing, the open mapping) is
// tested directly; the component wiring (the manual-pin override, the
// settings knob) is pinned structurally, the composition-test
// convention (same shape as test/toolrows.test.ts). Run:
// node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  parseThinkingDisplay,
  thinkingWantOpen,
  DEFAULT_THINKING_DISPLAY,
  THINKING_DISPLAY_KEY,
} from "../app/composables/thinkingDisplay.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// --- parseThinkingDisplay: the localStorage round-trip ---

test("parseThinkingDisplay accepts the two non-default modes", () => {
  assert.equal(parseThinkingDisplay("expanded"), "expanded");
  assert.equal(parseThinkingDisplay("collapsed"), "collapsed");
  assert.equal(parseThinkingDisplay("auto"), "auto");
});

test("parseThinkingDisplay falls back to auto on missing/tampered values", () => {
  assert.equal(DEFAULT_THINKING_DISPLAY, "auto");
  assert.equal(parseThinkingDisplay(null), "auto");
  assert.equal(parseThinkingDisplay(undefined), "auto");
  assert.equal(parseThinkingDisplay(""), "auto");
  assert.equal(parseThinkingDisplay("Auto"), "auto", "case-sensitive like parseToolDisplay");
  assert.equal(parseThinkingDisplay({ auto: true }), "auto");
});

// --- thinkingWantOpen: the pure open mapping ---

test("only expanded forces the block open; auto/collapsed stay shut", () => {
  assert.equal(thinkingWantOpen("expanded"), true);
  assert.equal(thinkingWantOpen("collapsed"), false);
  assert.equal(thinkingWantOpen("auto"), false);
});

test("the shared mode key persists under omega.thinking-display", () => {
  assert.equal(THINKING_DISPLAY_KEY, "omega.thinking-display");
});

// --- structural wiring: the card's must-haves in the components ---

test("ThinkingBlock pins a manual toggle and skips pinned recompute", () => {
  const t = src("components/chat/ThinkingBlock.vue");
  // the element must be reachable: the details ref is the anchor every
  // apply path reads - without it applyAuto no-ops forever
  assert.match(t, /<details\s+ref="el"/);
  // the element stays UNCONTROLLED: the streaming-driven :open binding
  // is gone (card t_428ed4fb superseded auto-expand-while-streaming)
  assert.doesNotMatch(t, /:open="streaming"/);
  // the pin: a summary click records the post-toggle state and auto
  // recompute (mount + mode watcher) skips pinned blocks
  assert.match(t, /@click="onSummaryClick"/);
  assert.match(t, /pinned\.value = !el\.value\.open/);
  assert.match(t, /applyAuto\(\) \{\s*\n\s*if \(pinned\.value !== null \|\| !el\.value\) return/);
  // the mapping rides the shared composable, not a private copy
  assert.match(t, /useThinkingDisplay\(\)/);
  assert.match(t, /thinkingWantOpen\(display\.value\)/);
});

test("the settings knob is a client chat select beside the tool display one", () => {
  const s = src("components/settings/SettingsTab.vue");
  assert.match(s, /key: "chat\.thinking_display"/);
  assert.match(s, /options: \["auto", "expanded", "collapsed"\]/);
  assert.match(s, /emit\("update:thinkingDisplay", value as ThinkingDisplay\)/);
  const a = src("app.vue");
  // v-model binds the SHARED ref so the knob writes the signal blocks read
  assert.match(a, /v-model:thinking-display="thinkingDisplay"/);
  assert.match(a, /const thinkingDisplay = useThinkingDisplay\(\)/);
  assert.match(a, /localStorage\.setItem\(THINKING_DISPLAY_KEY, mode\)/);
});
