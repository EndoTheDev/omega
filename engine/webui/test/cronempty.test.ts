// omega webui: the cron tab's designed empty state (card t_6072e718).
// The composition-test convention: the node suite never compiles the
// SFC - it pins the pure copy module and the source wiring (the
// structure the SFC must render when the jobs list is empty).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { ICONS } from "../app/composables/icons.ts";
import { CRON_EMPTY_HINT, CRON_EMPTY_TEXT } from "../../../modules/cron/ui/emptyState.ts";

// module UI source lives at the repo root (engine/webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
const read = (...p: string[]) => readFileSync(join(...p), "utf8");

test("the cron empty-state copy is the designed voice: headline + activation hint", () => {
  assert.equal(CRON_EMPTY_TEXT, "no cron jobs");
  // the hint keeps the literal config key - the user must know WHERE
  // to act (activation is a file action, so there is no CTA button)
  assert.ok(CRON_EMPTY_HINT.includes("crons.yaml"), "the hint names the config key");
  assert.ok(CRON_EMPTY_HINT.includes("schedule"), "the hint says what activating does");
  // and it is a hint line, not the old bare parenthetical string
  assert.ok(!CRON_EMPTY_HINT.includes("("), "no inline parenthetical");
});

test("CronJobs renders the empty-state pattern only when the jobs list is empty", () => {
  const src = read(root, "modules", "cron", "ui", "CronJobs.vue");
  // the branch order: error -> loading -> jobs -> empty
  const ok = src.includes('v-else-if="jobs.length"');
  const empty = src.indexOf('class="cron-empty"');
  assert.ok(ok && empty > ok, "the empty state is the terminal v-else after the jobs list");
  // the pattern structure: icon glyph + headline + hint, on the constants
  assert.ok(src.includes('name="cron"'), "the glyph rides the iconset's cron name");
  assert.ok(src.includes("CRON_EMPTY_TEXT"), "the headline rides the constant");
  assert.ok(src.includes("CRON_EMPTY_HINT"), "the hint rides the constant");
  assert.ok(!src.includes("(crons.yaml not activated)"), "the old bare string is gone");
  // no invented call-to-action (card t_6072e718: activation is a file action)
  const template = src.split("<script")[0];
  assert.ok(!/<button/i.test(template), "no CTA button in the empty state");
});

test("the empty state is centered in the panel body via the component's own styles", () => {
  const src = read(root, "modules", "cron", "ui", "CronJobs.vue");
  assert.ok(src.includes("min-height: 100%"), "the root owns the body height so centering has room");
  assert.ok(src.includes("justify-content: center"), "vertical centering is on the empty branch");
  assert.ok(src.includes("align-items: center"), "horizontal centering is on the empty branch");
  assert.ok(src.includes("var(--text-dim"), "the dim colors ride the shell variables (both themes)");
});

test("the cron glyph is the iconset's cron name (the membership law)", () => {
  assert.ok(ICONS["cron"], "cron is a registered iconset name");
});