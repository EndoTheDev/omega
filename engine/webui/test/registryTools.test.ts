// omega webui: the registry-browser TOOLS tab toggle behavior (v0.14.1,
// card t_372b9e9a). Pure-surface test like moduletoggle.test.ts + the
// source greps (the composition-test convention).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  normalizeDisabledPayload,
  withDisabledFlags,
  registryNote,
} from "../app/composables/omega.ts";

// module UI source lives at the repo root (engine/webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

// --- tools rows: actions[] collapse into the expandable detail ---

test("tool rows carry their actions as dotted detail lines; the fold flags by name", () => {
  // the row model is built inline in the tab; the contract it renders
  // is RegistryRow + withDisabledFlags - pin that here at the wire shape
  const tools = [
    { name: "memory", description: "memory tools", actions: [{ name: "add", description: "write a fact" }] },
    { name: "shell", description: "sh", actions: undefined },
  ];
  const fold = normalizeDisabledPayload({ tools: ["shell"] }).tools;
  const rows = withDisabledFlags(
    tools.map((t) => ({
      name: t.name,
      description: t.description,
      detail: (t.actions ?? []).map((a) => `${t.name}.${a.name} - ${a.description}`),
    })),
    fold,
  );
  assert.equal(rows[0].disabled, false);
  assert.deepEqual(rows[0].detail, ["memory.add - write a fact"], "domain actions are the detail");
  assert.equal(rows[1].disabled, true);
});

test("arming a tool disable names the tool-list consequence; enable is silent", () => {
  assert.equal(registryNote("tools", "shell", false), "disable shell? absent from the agent tool list");
  assert.equal(registryNote("tools", "shell", true), "");
});

test("ToolsList consumes the shared component and the fold wire", () => {
  const src = readFileSync(join(root, "modules", "tools", "ui", "ToolsList.vue"), "utf8");
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes('kind="tools"'), "the tools namespace rides to the note law");
  assert.ok(src.includes('fetch("/tools")'), "self-contained inventory fetch");
  assert.ok(src.includes("/disabled/tools"), "the toggle PUTs the tools namespace");
  assert.ok(src.includes("new Set(fold.value)"), "flags merge by name");
  assert.ok(!src.includes("composables/omega"), "no shell client dependency - module UI carries its own wiring");
  assert.ok(src.includes("status === 501") && src.includes("toggleable.value = false"), "a missing seam renders read-only");
  // the RegistryList contract: a tool's actions render as detail lines
  // (expandable), not as always-visible sub-rows
  assert.ok(src.includes("t.name}.${a.name}"), "dotted action detail naming");
});