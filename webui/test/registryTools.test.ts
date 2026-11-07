// omega webui: the registry-browser TOOLS tab toggle behavior (v0.14.1,
// card t_372b9e9a). Pure-surface test like moduletoggle.test.ts + the
// source greps (the composition-test convention).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  registryNote,
} from "../app/composables/omega.ts";
import { toolRows, toggleTargets } from "../../harness/tools/ui/toolRows.ts";

// module UI source lives at the repo root (webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

// --- tools rows: the one-domain grouping (t_e06b38bc) ---

test("tool rows group by domain: actions as dotted detail, siblings merged, lone dotted rows stay", () => {
  const tools = [
    { name: "files", description: "read write edit", actions: [{ name: "write", description: "write a file" }] },
    { name: "kanban", description: "the board", actions: [{ name: "list", description: "list tasks" }] },
    { name: "delegate.task", description: "run a subagent" },
    { name: "delegate.status", description: "watch the subagents" },
    { name: "web.search", description: "search the web" },
    { name: "web.fetch", description: "fetch a page" },
    { name: "shell.run", description: "run a command" },
  ];
  const rows = toolRows(tools, ["shell.run"]);
  // dotless domain tools are parent rows with dotted action children
  assert.deepEqual(rows[0].detail, ["files.write - write a file"]);
  assert.equal(rows[0].name, "files");
  // sibling-verb tools merge under their prefix: one parent, both
  // full identities nested
  const delegateRow = rows.find((r) => r.name === "delegate");
  assert.ok(delegateRow, "delegate renders as one parent row");
  assert.deepEqual(delegateRow.detail, [
    "delegate.task - run a subagent",
    "delegate.status - watch the subagents",
  ]);
  // a lone dotted tool is NOT grouped into a one-member parent
  const shellRow = rows.find((r) => r.name === "shell.run");
  assert.ok(shellRow, "shell.run stays its own row");
  assert.deepEqual(shellRow.detail, []);
  assert.equal(shellRow.disabled, true, "the fold flags by exact tool name");
});

test("toggleTargets: an exact tool toggles itself, a group parent fans out to its members", () => {
  const tools = [
    { name: "kanban", description: "the board" },
    { name: "delegate.task", description: "t" },
    { name: "delegate.status", description: "s" },
    { name: "shell.run", description: "r" },
  ];
  assert.deepEqual(toggleTargets("kanban", tools), ["kanban"], "domain tools are real names");
  assert.deepEqual(toggleTargets("delegate", tools), ["delegate.task", "delegate.status"], "group parents fan out");
  assert.deepEqual(toggleTargets("shell.run", tools), ["shell.run"]);
});

// --- t_7871a56a: the dotless/dotted collision (files + files.grep +
// files.glob) - the domain row ABSORBS the dotted siblings; two rows
// on one domain is a red shape that must stay impossible ---

test("a dotless domain row absorbs its dotted siblings: ONE files row, actions + grep + glob nested (t_7871a56a)", () => {
  const tools = [
    { name: "files", description: "read write edit", actions: [
      { name: "read", description: "read a file" },
      { name: "write", description: "write a file" },
    ] },
    { name: "files.grep", description: "search file contents" },
    { name: "files.glob", description: "list matching paths" },
  ];
  const rows = toolRows(tools, []);
  // the red shape: exactly one row per domain, never two "files" rows
  const filesRows = rows.filter((r) => r.name === "files");
  assert.equal(filesRows.length, 1, "one row per domain - the duplicate-row collision stays dead");
  assert.deepEqual(rows.filter((r) => r.name.startsWith("files.")).length, 0, "no orphan dotted rows under an absorbed prefix");
  // actions first, then the absorbed siblings by inventory order, all
  // under the same detail-line contract
  assert.deepEqual(filesRows[0].detail, [
    "files.read - read a file",
    "files.write - write a file",
    "files.grep - search file contents",
    "files.glob - list matching paths",
  ]);
});

test("absorption holds wherever the domain tool sits in the inventory (order law)", () => {
  // dotted members BEFORE the dotless tool: the skip law must still
  // deliver one merged row, seated at the domain tool's position
  const tools = [
    { name: "files.grep", description: "search file contents" },
    { name: "files.glob", description: "list matching paths" },
    { name: "files", description: "read write edit", actions: [
      { name: "write", description: "write a file" },
    ] },
  ];
  const rows = toolRows(tools, []);
  assert.equal(rows.length, 1, "the absorbed members render no rows of their own");
  assert.equal(rows[0].name, "files");
  assert.deepEqual(rows[0].detail, [
    "files.write - write a file",
    "files.grep - search file contents",
    "files.glob - list matching paths",
  ]);
});

test("a merged domain row's disabled is group-style: off only when every member is off", () => {
  const tools = [
    { name: "files", description: "read write edit", actions: [{ name: "read", description: "read a file" }] },
    { name: "files.grep", description: "search file contents" },
    { name: "files.glob", description: "list matching paths" },
  ];
  const allOff = toolRows(tools, ["files", "files.grep", "files.glob"]);
  assert.equal(allOff[0].disabled, true, "every member off = row off");
  const partial = toolRows(tools, ["files.grep"]);
  assert.equal(partial[0].disabled, false, "one member still on = row on");
  const domainOnly = toolRows(tools, ["files"]);
  assert.equal(domainOnly[0].disabled, false, "the domain tool off but siblings on = row on (the toggle fans to all)");
});

test("toggleTargets: a merged domain row fans out to the domain tool AND its absorbed siblings (t_7871a56a)", () => {
  const tools = [
    { name: "files", description: "read write edit", actions: [] },
    { name: "files.grep", description: "search file contents" },
    { name: "files.glob", description: "list matching paths" },
    { name: "kanban", description: "the board" },
  ];
  assert.deepEqual(
    toggleTargets("files", tools),
    ["files", "files.grep", "files.glob"],
    "the exact-name shortcut must not strand the absorbed members",
  );
  // a dotless tool with NO dotted siblings still toggles itself alone
  assert.deepEqual(toggleTargets("kanban", tools), ["kanban"]);
});

test("arming a tool disable names the tool-list consequence; enable is silent", () => {
  assert.equal(registryNote("tools", "shell", false), "disable shell? absent from the agent tool list");
  assert.equal(registryNote("tools", "shell", true), "");
});

test("ToolsList consumes the shared component and the fold wire", () => {
  const src = readFileSync(join(root, "harness", "tools", "ui", "ToolsList.vue"), "utf8");
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes('kind="tools"'), "the tools namespace rides to the note law");
  assert.ok(src.includes('fetch("/tools")'), "self-contained inventory fetch");
  assert.ok(src.includes("/disabled/tools"), "the toggle PUTs the tools namespace");
  assert.ok(src.includes('from "./toolRows"'), "the row model lives in the pure helper (t_e06b38bc grouping)");
  assert.ok(src.includes("toggleTargets"), "the group-parent toggle fans out to exact tool names");
  assert.ok(!src.includes("composables/omega"), "no shell client dependency - module UI carries its own wiring");
  assert.ok(src.includes("status === 501") && src.includes("toggleable.value = false"), "a missing seam renders read-only");
  // the grouping contract lives in toolRows.ts (node-tested above); the
  // component only wires the computed rows + the fan-out toggle
});

// t_504a78ca regression: RegistryList gates with `v-if="rows.length"`.
// rows as a plain function makes .length the ARITY (0) - data loads,
// the tab renders empty. The rows binding must hand RegistryList an
// array (computed, the AgentsPanel shape), not a function.
test("ToolsList builds rows as a computed (arity trap: a plain function rows() renders empty)", () => {
  const src = readFileSync(join(root, "harness", "tools", "ui", "ToolsList.vue"), "utf8");
  assert.ok(src.includes("const rows = computed("), "rows computed, so :rows unwraps to the array");
  assert.ok(!src.includes("function rows()"), "no plain-function row builder under :rows");
  assert.ok(src.includes(':rows="visibleRows"'), "the shared component gets the rows binding (the band filter's survivors - card t_529628d5)");
});