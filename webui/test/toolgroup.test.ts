// omega webui: unit tests for the tool-call grouping (card t_e4280de7).
// The pure logic (run folding, variant exclusion, the group open state
// machine) is tested directly; the component wiring (three render
// sites, the shared display mode, the pin click, the CSS width laws)
// is pinned structurally, the composition-test convention. Run:
// node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { groupToolRuns, isToolRun, toolGroupWantOpen } from "../app/composables/toolGroup.ts";
import type { TranscriptItem } from "../app/composables/omega.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// item builders keep the tests shape-honest: the same items the live
// stream and the restore path produce
const user = (content: string): TranscriptItem => ({ kind: "message", role: "user", content });
const assistant = (content: string): TranscriptItem => ({
  kind: "message",
  role: "assistant",
  content,
});
const thinking = (content: string): TranscriptItem => ({ kind: "thinking", content });
const tool = (name: string, id?: string): TranscriptItem => ({
  kind: "tool",
  name,
  status: "done",
  id,
});

// --- groupToolRuns: the run folding ---

test("two or more consecutive tool rows fold into ONE group entry", () => {
  const a = tool("shell.run", "t1");
  const b = tool("shell.run", "t2");
  const c = tool("files.read", "t3");
  const out = groupToolRuns([user("go"), a, b, c, assistant("done")]);
  assert.deepEqual(
    out.map((e) => (isToolRun(e) ? "group" : e.kind)),
    ["message", "group", "message"],
  );
  const g = out[1];
  assert.ok(isToolRun(g));
  assert.equal(g.items.length, 3);
  // identity preserved: the group carries the SAME objects
  assert.equal(g.items[0], a);
  assert.equal(g.items[2], c);
});

test("a single lone tool row stays in flow - nothing to collapse", () => {
  const out = groupToolRuns([user("go"), tool("shell.run"), assistant("hi")]);
  assert.deepEqual(
    out.map((e) => (isToolRun(e) ? "group" : e.kind)),
    ["message", "tool", "message"],
  );
});

test("any other item kind breaks the run: only consecutive tools group", () => {
  const out = groupToolRuns([tool("a"), thinking("hm"), tool("b"), tool("c")]);
  assert.deepEqual(
    out.map((e) => (isToolRun(e) ? "group" : e.kind)),
    ["tool", "thinking", "group"],
  );
});

test("a variant-anchored tool row is NEVER swallowed - it splits the run", () => {
  const a = tool("shell.run", "t1");
  const anchor = tool("shell.run", "head");
  const b = tool("files.read", "t3");
  const out = groupToolRuns([a, anchor, b], (it) => it.id === "head");
  assert.deepEqual(
    out.map((e) => (isToolRun(e) ? "group" : e.kind)),
    ["tool", "tool", "tool"],
  );
  assert.ok(!isToolRun(out[1]));
  // the anchor row itself is in flow, same object
  assert.equal(out[1], anchor);
});

test("recomputing with a variant map that LOADED late splits a formed group", () => {
  const a = tool("shell.run", "t1");
  const b = tool("files.read", "head");
  const out = groupToolRuns([a, b], () => false); // no variants yet: grouped
  assert.equal(isToolRun(out[0]), true);
  const out2 = groupToolRuns([a, b], (it) => it.id === "head"); // variants loaded
  assert.deepEqual(
    out2.map((e) => (isToolRun(e) ? "group" : e.kind)),
    ["tool", "tool"],
  );
});

test("empty transcript, empty result", () => {
  assert.deepEqual(groupToolRuns([]), []);
});

// --- toolGroupWantOpen: the group's auto state machine ---

test("expanded/collapsed force one state regardless of run state", () => {
  for (const live of [true, false]) {
    assert.equal(toolGroupWantOpen(false, live, "expanded"), true);
    assert.equal(toolGroupWantOpen(true, live, "expanded"), true);
    assert.equal(toolGroupWantOpen(false, live, "collapsed"), false);
    assert.equal(toolGroupWantOpen(true, live, "collapsed"), false);
  }
});

test("auto keeps the group open while the run is live or any row runs", () => {
  assert.equal(toolGroupWantOpen(true, false, "auto"), true);
  assert.equal(toolGroupWantOpen(false, true, "auto"), true, "live across the whole run");
  assert.equal(toolGroupWantOpen(false, false, "auto"), false, "settled collapses");
});

// --- structural wiring: the card's must-haves ---

test("ChatTab renders the group at all THREE sites and keys on item identity", () => {
  const t = src("components/ChatTab.vue");
  // the grouping rides the transcript level (render-site view), never
  // a transcript mutation
  assert.match(t, /import \{ groupToolRuns, isToolRun, type ChatEntry \}/);
  assert.match(t, /groupToolRuns\(transcript\.value, variantOfItem\)/);
  assert.match(t, /groupToolRuns\(block, variantOfItem\)/);
  assert.match(t, /groupToolRuns\(liveRun\.value\.body, variantOfItem\)/);
  // the group's key is its HEAD ROW's key - a growing run keeps its
  // details element (pins survive the growth)
  assert.match(t, /return isToolRun\(entry\) \? itemKey\(entry\.items\[0\]!\) : itemKey\(entry\)/);
  // the live gate rides the busy slot so auto stays open across the
  // whole run (no per-call yoyo)
  assert.match(t, /:live="busy"/);
  assert.equal(t.match(/<ToolGroup(?![\w-])/g)?.length, 3, "one render per site");
});

test("ToolGroup mirrors the row/block fold: shared mode, pin click, child passthrough", () => {
  const g = src("components/chat/ToolGroup.vue");
  assert.match(g, /<details\s+ref="el"/);
  assert.match(g, /@click="onSummaryClick"/);
  assert.match(g, /pinned\.value = !el\.value\.open/);
  assert.match(g, /applyAuto\(\) \{\s*\n\s*if \(pinned\.value !== null \|\| !el\.value\) return/);
  // the state machine rides the SHARED composable, not a private copy;
  // the combined active flag (running OR pending - async agent loop
  // phase 3) keeps a chip-holding group open
  assert.match(g, /useToolDisplay\(\)/);
  assert.match(g, /toolGroupWantOpen\(anyActive\.value, props\.live \?\? false, display\.value\)/);
  assert.match(g, /anyPending/);
  // children render EXACTLY the ChatItem tool-branch markup: rows keep
  // their own fold + gate card, a group is a container not a re-render
  assert.match(g, /<ToolCallLine/);
  assert.match(g, /<GateCard/);
  // aggregate status rides the existing wrench glyph (one name one meaning)
  assert.match(g, /name="tools"/);
});

test("the width laws ride the row's: base cap, stacked full, mobile 88%", () => {
  const css = src("assets/css/main.css");
  // the summary face mirrors .thinking's block summary (one fold grammar)
  assert.match(css, /\.toolgroup summary \{/);
  assert.match(css, /\.toolgroup summary::-webkit-details-marker \{ display: none; \}/);
  assert.match(css, /\.toolgroup \{[^}]*max-width: 70%/);
  assert.match(css, /\.messages\.stacked \.toolgroup \{ max-width: 100%; \}/);
  assert.match(css, /\.bubble, \.thinking, \.toolcall, \.toolgroup \{ max-width: 88%; \}/);
  // rows render at FULL width inside the group - the cap belongs to
  // the group, not to the rows in its body
  assert.match(css, /\.toolgroup \.toolcall \{ max-width: 100%; \}/);
});
