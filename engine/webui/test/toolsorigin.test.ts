// omega webui: the TOOLS tab's MCP-origin exclusion (card t_c2da3379,
// Endo's report: the tools sidebar mixed sequential-thinking.*
// MCP-origin tools into the native list). Pure-surface test like the
// composition-test convention: the filter reads the ORIGIN FIELD the
// wire stamps (core.Tool.Origin via GET /tools), never the name shape -
// native tools are dotted too (sessions.search/shell.run stay), an
// MCP-origin tool is excluded whichever shape its name has.
import { test } from "node:test";
import assert from "node:assert/strict";

import { nativeTools, toolRows, type ToolWire } from "../../../modules/tools/ui/toolRows.ts";

// --- nativeTools: the exclusion (mandate 1 - MCP shows in the MCP tab alone) ---

test("nativeTools excludes origin=mcp tools, keeps native dotted tools", () => {
  const tools: ToolWire[] = [
    { name: "files", description: "read write edit" },
    { name: "sessions.search", description: "search past sessions" },
    { name: "shell.run", description: "run a shell command" },
    { name: "sequential-thinking.sequentialthinking", description: "think in steps", origin: "mcp" },
    { name: "proof.echo", description: "server-qualified mcp tool", origin: "mcp" },
  ];
  const kept = nativeTools(tools).map((t) => t.name);
  // native dotted tools STAY - dotted shape is not the disqualifier
  assert.deepEqual(kept, ["files", "sessions.search", "shell.run"]);
});

test("nativeTools treats any dotted native tool as native (no name heuristics)", () => {
  // a mcp-server-looking name with NO origin stamp is native data:
  // the filter must keep it
  const tools: ToolWire[] = [
    { name: "server-looking.name", description: "native registry tool, unstamped" },
  ];
  assert.equal(nativeTools(tools).length, 1);
});

test("nativeTools does not mutate its input", () => {
  const tools = [{ name: "shell.run", description: "run" }];
  nativeTools(tools);
  assert.equal(tools.length, 1);
});

// --- the rows pipeline consumes the FILTERED slice ---

test("toolRows built from the filtered slice: no mcp rows or groups leak", () => {
  const tools: ToolWire[] = [
    { name: "delegate.task", description: "run a subagent" },
    { name: "delegate.status", description: "watch the subagents" },
    { name: "shell.run", description: "run a command" },
    { name: "sequential-thinking.sequentialthinking", description: "think in steps", origin: "mcp" },
  ];
  const rows = toolRows(nativeTools(tools), []);
  const names = rows.map((r) => r.name);
  // sequential-thinking.* neither renders as its own row nor forms a
  // lone-member domain group
  assert.equal(names.includes("sequential-thinking.sequentialthinking"), false);
  assert.equal(names.includes("sequential-thinking"), false);
  // the native grouping is untouched (delegate merges under its prefix)
  assert.deepEqual(names, ["delegate", "shell.run"]);
});