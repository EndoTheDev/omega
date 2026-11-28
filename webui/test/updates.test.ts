// omega webui: the update channel's pane logic and its wiring.
//
// The pane never downloads or installs by itself - it renders the server's status and
// posts actions. Everything here is pure, plus two source assertions for the mounts the
// node suite cannot render.
// Run: node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  updateCanFinalize,
  updateHeadline,
  updateInFlight,
  updateReceiptLine,
  updateRestartPrompt,
  type UpdateStatus,
} from "../app/composables/updates.ts";
import {
  SURFACE_BACKEND,
  categorySurface,
  knobSubgroup,
} from "../app/components/settings/settingsNav.ts";

const status = (patch: Partial<UpdateStatus>): UpdateStatus => ({
  state: "idle",
  current: "v0.19.0",
  channel: "stable",
  managed: true,
  auto_check: true,
  auto_update: false,
  active_runs: 0,
  ...patch,
});

test("the headline carries the state's facts, never a bare state word", () => {
  assert.match(updateHeadline(status({ state: "idle" })), /no check yet/);
  assert.match(updateHeadline(status({ state: "up-to-date" })), /up to date on the stable channel/);
  assert.match(
    updateHeadline(status({ state: "available", available: "v0.20.0", behind: 3 })),
    /v0\.20\.0 is available on the stable channel - 3 releases behind/,
  );
  assert.match(updateHeadline(status({ state: "staging", progress: 40 })), /40%/);
  assert.match(updateHeadline(status({ state: "ready", staged_version: "v0.20.0" })), /pre-flight green/);
  assert.match(updateHeadline(status({ state: "failed", error: "checksum mismatch" })), /checksum mismatch/);
  assert.match(updateHeadline(status({ state: "idle", managed: false })), /never self-updates/);
});

test("only a staged, pre-flight-green release is finalize-legal", () => {
  assert.equal(updateCanFinalize(status({ state: "available", available: "v0.20.0" })), false);
  assert.equal(updateCanFinalize(status({ state: "ready" })), false, "ready without a staged version is not legal");
  assert.equal(updateCanFinalize(status({ state: "ready", staged_version: "v0.20.0" })), true);
  assert.equal(updateCanFinalize(null), false);
});

test("in-flight states are the ones the pane polls on", () => {
  for (const state of ["checking", "staging", "preflight", "finalizing"]) {
    assert.equal(updateInFlight(status({ state })), true, `${state} is in flight`);
  }
  for (const state of ["idle", "up-to-date", "available", "ready", "failed"]) {
    assert.equal(updateInFlight(status({ state })), false, `${state} is settled`);
  }
});

test("a rollback receipt names version, why and what was restored", () => {
  const line = updateReceiptLine({
    version: "v0.20.0",
    outcome: "rollback",
    why: "health window failed after 30s",
    restored: ["omega.db", "config.yaml"],
    at: 1,
  });
  assert.match(line, /v0\.20\.0 rolled back/);
  assert.match(line, /health window failed after 30s/);
  assert.match(line, /Restored: omega\.db, config\.yaml/);
  assert.match(
    updateReceiptLine({ version: "v0.20.0", outcome: "pass", at: 1 }),
    /installed and healthy/,
  );
});

test("a failed finalize renders its why once - the receipt line is the carrier", () => {
  // The pane has three carriers: the headline, the error paragraph (the status error it
  // copies) and the receipt line. A failed finalize answers with the receipt only, so the
  // why must appear exactly once across all three.
  const st = status({
    state: "failed",
    receipt: {
      version: "v0.20.0",
      outcome: "rollback",
      why: "health window failed after 30s",
      restored: ["omega.db"],
      at: 1,
    },
  });
  const why = "health window failed after 30s";
  const carriers = (s: UpdateStatus) =>
    [updateHeadline(s), s.error ?? "", updateReceiptLine(s.receipt)].join("\n");
  assert.equal(carriers(st).split(why).length - 1, 1, `rendered:\n${carriers(st)}`);

  // The same status carrying the why in its error too is what rendered it three times:
  // the server's duplicate field, not the pane, was the defect.
  const doubled = status({ ...st, error: why });
  assert.equal(carriers(doubled).split(why).length - 1, 3);
});

test("the restart prompt counts the live runs (R7: never a silent restart)", () => {
  assert.match(updateRestartPrompt(status({ active_runs: 0 })), /Restart omega now\?/);
  assert.match(updateRestartPrompt(status({ active_runs: 1 })), /1 run active/);
  assert.match(updateRestartPrompt(status({ active_runs: 3 })), /restarting ends them/);
});

test("the updates section is backend config with its own subgroups", () => {
  assert.equal(categorySurface("updates"), SURFACE_BACKEND);
  assert.equal(knobSubgroup("updates.channel"), "channel");
  assert.equal(knobSubgroup("updates.auto_check"), "automation");
  assert.equal(knobSubgroup("updates.auto_update"), "automation");
  assert.equal(knobSubgroup("updates.managed"), "scope");
});

test("the pane and the chip are mounted where the card promises", () => {
  const here = fileURLToPath(new URL("../app/components/", import.meta.url));
  const settings = readFileSync(here + "settings/SettingsTab.vue", "utf8");
  assert.match(settings, /<UpdatesSection v-if="section\.id === 'updates'" \/>/);
  const statusBar = readFileSync(here + "shell/StatusBar.vue", "utf8");
  assert.match(statusBar, /<UpdateChip \/>/);
});
