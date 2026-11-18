// omega webui: the cron tab's designed empty state. The composition-test convention:
// the node suite never compiles the SFC - it pins the pure copy module and the source
// wiring (the structure the SFC must render when the jobs list is empty).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { ICONS } from "../app/composables/icons.ts";
import { CRON_EMPTY_HINT, CRON_EMPTY_TEXT } from "../../harness/cron/ui/emptyState.ts";

// module UI source lives at the repo root (webui/test is three levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(...p), "utf8");

test("the cron empty-state copy is the designed voice: headline + activation hint", () => {
  assert.equal(CRON_EMPTY_TEXT, "no cron jobs");
  assert.ok(CRON_EMPTY_HINT.includes("crons.yaml"), "the hint names the config key");
  assert.ok(CRON_EMPTY_HINT.includes("add one"), "the hint names the editor's add affordance");
  assert.ok(CRON_EMPTY_HINT.includes("schedule"), "the hint says what activating does");
});

test("CronJobs renders the empty-state pattern only when the jobs list is empty", () => {
  const src = read(root, "harness", "cron", "ui", "CronJobs.vue");
  const ok = src.includes('v-else-if="filteredJobs.length"');
  const empty = src.indexOf('class="cron-empty"');
  assert.ok(ok && empty > ok, "the empty state is the terminal v-else after the jobs list");
  // a query that hides all rows is a SEARCH MISS, not the configured-empty state.
  const miss = src.indexOf("no matching jobs");
  assert.ok(miss > ok && miss < empty, "the search miss is NOT the activation empty state");
  assert.ok(src.includes('name="cron"'), "the glyph rides the iconset's cron name");
  assert.ok(src.includes("CRON_EMPTY_TEXT"), "the headline rides the constant");
  assert.ok(src.includes("CRON_EMPTY_HINT"), "the hint rides the constant");
  assert.ok(!src.includes("(crons.yaml not activated)"), "the old bare string is gone");
  const emptyChunk = src.slice(src.indexOf('class="cron-empty"'), src.indexOf("</section>"));
  assert.ok(!/<button/i.test(emptyChunk), "no CTA button in the empty state");
});

test("the empty state is centered in the panel body via the component's own styles", () => {
  const src = read(root, "harness", "cron", "ui", "CronJobs.vue");
  assert.ok(src.includes(".cron-body"), "the tab's scroll body is the centering context");
  assert.ok(src.includes("overflow-y: auto"), "the body is the scrollport (the band never scrolls with it)");
  assert.ok(src.includes("justify-content: center"), "vertical centering is on the empty branch");
  assert.ok(src.includes("align-items: center"), "horizontal centering is on the empty branch");
  assert.ok(src.includes("var(--text-dim"), "the dim colors ride the shell variables (both themes)");
});

test("the cron glyph is the iconset's cron name (the membership law)", () => {
  assert.ok(ICONS["cron"], "cron is a registered iconset name");
});