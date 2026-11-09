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

test("the kanban split groups under one kanban row: seven dotted tools, one parent (t_f616ed78)", () => {
  // the real post-split wire: the flat action-protocol tool is gone,
  // the seven dotted siblings are the domain
  const tools = [
    { name: "shell.run", description: "run a command" },
    { name: "kanban.list", description: "list board tasks" },
    { name: "kanban.create", description: "create a board task" },
    { name: "kanban.update", description: "update a task" },
    { name: "kanban.complete", description: "mark a task done" },
    { name: "kanban.comment", description: "add a comment" },
    { name: "kanban.delete", description: "remove a task" },
    { name: "kanban.batch", description: "run several operations atomically" },
  ];
  const rows = toolRows(tools, []);
  const kanbanRows = rows.filter((r) => r.name === "kanban");
  assert.equal(kanbanRows.length, 1, "the seven dotted tools merge into ONE kanban row");
  assert.deepEqual(
    kanbanRows[0].detail,
    [
      "kanban.list - list board tasks",
      "kanban.create - create a board task",
      "kanban.update - update a task",
      "kanban.complete - mark a task done",
      "kanban.comment - add a comment",
      "kanban.delete - remove a task",
      "kanban.batch - run several operations atomically",
    ],
    "each member keeps its full dotted identity as a detail line",
  );
  assert.equal(rows.some((r) => r.name.startsWith("kanban.")), false, "no orphan kanban.* rows");
  // the group parent is not a tool name - the toggle fans out to the
  // exact members (the fold keeps real names)
  assert.deepEqual(toggleTargets("kanban", tools), [
    "kanban.list",
    "kanban.create",
    "kanban.update",
    "kanban.complete",
    "kanban.comment",
    "kanban.delete",
    "kanban.batch",
  ]);
});

test("the phase-2 split renders pure-dotted domains as one group row each (t_d8fbad57)", () => {
  // the real post-split wire: files/memory/skills/verdict carry no
  // dotless domain tool - every member is a dotted sibling, so each
  // domain is a group parent row like kanban's
  const tools = [
    { name: "shell.run", description: "run a command" },
    { name: "files.read", description: "read a file" },
    { name: "files.write", description: "write a file" },
    { name: "files.edit", description: "patch a file" },
    { name: "files.batch", description: "atomic multi-file change" },
    { name: "files.grep", description: "search contents" },
    { name: "files.glob", description: "list matching paths" },
    { name: "memory.add", description: "append an entry" },
    { name: "memory.replace", description: "replace an entry" },
    { name: "memory.remove", description: "remove an entry" },
    { name: "memory.batch", description: "atomic multi-change" },
    { name: "skills.read", description: "load a skill" },
    { name: "skills.write", description: "create a skill" },
    { name: "skills.update", description: "patch a skill" },
    { name: "skills.curate", description: "curator pass" },
    { name: "verdict.request", description: "issue a choice card" },
  ];
  const rows = toolRows(tools, []);
  for (const domain of ["files", "memory", "skills"]) {
    const groupRows = rows.filter((r) => r.name === domain);
    assert.equal(groupRows.length, 1, `the dotted ${domain}.* tools merge into ONE ${domain} row`);
    assert.equal(
      rows.some((r) => r.name.startsWith(`${domain}.`)),
      false,
      `no orphan ${domain}.* rows`,
    );
    assert.deepEqual(
      toggleTargets(domain, tools),
      tools.filter((t) => t.name.startsWith(`${domain}.`)).map((t) => t.name),
      `the ${domain} group toggle fans out to its exact members`,
    );
  }
  // verdict.request is a ONE-member domain: no group parent, its own row
  assert.equal(rows.some((r) => r.name === "verdict"), false, "a one-member domain never groups");
  const verdictRow = rows.find((r) => r.name === "verdict.request");
  if (!verdictRow) {
    throw new Error("verdict.request stays its own top-level row");
  }
  assert.deepEqual(verdictRow.detail, []);
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

// --- t_5c9bb50a: the fold-aware management read (GET /tools?disabled=1)
// - a disabled tool KEEPS its row (dimmed) and its re-enable path.
// The inventory shrinking on a disable is the red shape: the one-way
// door ("i disabled web, and now its so gone, i cant even re-enable
// it") ---

test("the fold-aware inventory keeps disabled web.* in their group row - dimmed, never gone", () => {
  // the wire the fold-aware read serves: the web pair rides the
  // inventory with disabled:true stamps, the fold names both
  const tools = [
    { name: "shell.run", description: "run a command" },
    { name: "web.search", description: "search the web", disabled: true },
    { name: "web.fetch", description: "fetch a page", disabled: true },
  ];
  const rows = toolRows(tools, ["web.search", "web.fetch"]);
  const webRow = rows.find((r) => r.name === "web");
  // the row exists - a vanished row has no toggle to flip back
  assert.ok(webRow, "the disabled web group keeps its row");
  assert.equal(webRow.disabled, true, "both members off = the group row dims");
  assert.deepEqual(webRow.detail, [
    "web.search - search the web",
    "web.fetch - fetch a page",
  ]);
});

test("red shape: disabling a tool can never remove its re-enable path (t_5c9bb50a)", () => {
  const tools = [
    { name: "shell.run", description: "run a command" },
    { name: "web.search", description: "search the web", disabled: true },
    { name: "web.fetch", description: "fetch a page", disabled: true },
  ];
  const fold = ["web.search", "web.fetch"];
  const rows = toolRows(tools, fold);
  // every fold-disabled tool still holds a row seat - its own row or
  // a member line inside a domain/group row
  for (const name of fold) {
    const seated = rows.some(
      (r) => r.name === name || r.detail.some((d) => d.startsWith(`${name} - `)),
    );
    assert.ok(seated, `${name} keeps a row seat after its disable`);
  }
  // and the row's toggle still fans out to the EXACT disabled names -
  // the re-enable PUT has real targets, the door stays two-way
  assert.deepEqual(toggleTargets("web", tools), ["web.search", "web.fetch"]);
});

test("a fold-disabled absorbed sibling stays a member of its domain row, not a standalone row (t_7871a56a law under the fold)", () => {
  const tools = [
    { name: "files", description: "read write edit", actions: [{ name: "read", description: "read a file" }] },
    { name: "files.grep", description: "search file contents", disabled: true },
    { name: "files.glob", description: "list matching paths" },
  ];
  const rows = toolRows(tools, ["files.grep"]);
  assert.equal(rows.filter((r) => r.name === "files").length, 1, "one row per domain holds with a disabled member aboard");
  assert.equal(rows.some((r) => r.name === "files.grep"), false, "a disabled absorbed sibling renders no standalone row");
  assert.equal(rows[0].disabled, false, "one member still on = row on (the group-style law)");
  assert.deepEqual(rows[0].detail, [
    "files.read - read a file",
    "files.grep - search file contents",
    "files.glob - list matching paths",
  ]);
  // the re-enable target survives: the domain row's toggle still
  // reaches the disabled member by its exact fold name
  assert.deepEqual(toggleTargets("files", tools), ["files", "files.grep", "files.glob"]);
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
  // t_5c9bb50a: the tab's source is the FOLD-AWARE read - a disabled
  // tool keeps its row. The bare GET /tools (the executable registry
  // the chat loop consumes) must never be this tab's source: it
  // shrinks on a disable and strands the tool with no re-enable
  // toggle - the one-way door.
  assert.ok(src.includes('fetch("/tools?disabled=1")'), "self-contained fold-aware inventory fetch");
  assert.ok(!src.includes('fetch("/tools")'), "the bare executable-registry fetch is never the tab's source");
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