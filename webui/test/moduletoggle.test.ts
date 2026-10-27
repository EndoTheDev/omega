// omega webui: unit tests for the module toggle surface (v0.12 E2).
// The pure helpers bind at the wire shape /modules serves: the
// normalizer tolerates junk, the toggle note carries the Q3 dependents
// and the tier law. Run: node --test webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  normalizeModulesPayload,
  toggleNote,
  type ModuleRow,
} from "../app/composables/omega.ts";

const row = (over: Partial<ModuleRow>): ModuleRow => ({
  name: "kanban",
  state: "mounted",
  toggleable: true,
  ...over,
});

// --- normalizeModulesPayload: a malformed response never kills the panel ---

test("normalize keeps well-formed modules and refusals", () => {
  const out = normalizeModulesPayload({
    modules: [
      { name: "kanban", state: "mounted", toggleable: true, dependents: ["cron"] },
      { name: "store", state: "mounted", toggleable: false },
    ],
    refusals: [{ module: "kanban", predicate: "toggle.enable", err: "panicked: boom", at: "2026-10-27T00:00:00Z" }],
  });
  assert.equal(out.modules.length, 2);
  assert.equal(out.modules[0].dependents?.[0], "cron");
  assert.equal(out.refusals[0].err, "panicked: boom");
});

test("normalize drops nameless and junk entries", () => {
  const out = normalizeModulesPayload({
    modules: [null, "nope", { state: "mounted", toggleable: true }, { name: "", state: "mounted" }, { name: "cron", state: "weird" }],
    refusals: [null, { module: "kanban" }, { err: "no module" }],
  });
  assert.equal(out.modules.length, 1);
  // unknown state folds to the safe mounted default
  assert.equal(out.modules[0].name, "cron");
  assert.equal(out.modules[0].state, "mounted");
  assert.equal(out.refusals.length, 0);
});

test("normalize tolerates junk root payloads", () => {
  for (const junk of [null, 42, "x", {}, { modules: "nope" }, { modules: [5] }]) {
    assert.deepEqual(normalizeModulesPayload(junk), { modules: [], refusals: [] });
  }
});

// --- toggleNote: the confirm text carries Q3 and the tier law ---

test("disable with dependents names what goes down", () => {
  const m = row({ dependents: ["cron", "delegate"] });
  assert.equal(toggleNote(m, false), "disable kanban? this also disables: cron, delegate");
});

test("disable without dependents just asks", () => {
  assert.equal(toggleNote(row(), false), "disable kanban?");
});

test("enable needs no confirm", () => {
  assert.equal(toggleNote(row(), true), "");
});

test("not-toggleable states the always-on law instead", () => {
  assert.equal(toggleNote(row({ toggleable: false }), false), "kanban is machine-critical or engine ring - always on");
  assert.equal(toggleNote(row({ toggleable: false }), true), "kanban is machine-critical or engine ring - always on");
});