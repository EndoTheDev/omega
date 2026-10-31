// omega webui: unit tests for the width-driven narrow adaptation
// (card t_49d0b308). The measured class flip (ResizeObserver) is
// browser code; what the unit suite pins is:
//   - the pure threshold comparison (both copies agree on one value),
//   - the CLASS PRESENCE wiring in the two adapted panels
//     (source-level, the composition-test convention).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

import { isNarrowWidth, NARROW_MAX_PX } from "../app/composables/narrow.ts";
import {
  isNarrowWidth as boardIsNarrowWidth,
  NARROW_MAX_PX as BOARD_NARROW_MAX_PX,
} from "../../../modules/kanban/ui/board/boardLogic.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

test("isNarrowWidth: the ~300px column is narrow, a main pane is wide", () => {
  assert.equal(isNarrowWidth(0), false, "unmounted measure noise is never narrow");
  assert.equal(isNarrowWidth(128), true, "the drag floor");
  assert.equal(isNarrowWidth(240), true, "the sidebar default");
  assert.equal(isNarrowWidth(300), true, "the inspector default");
  assert.equal(isNarrowWidth(344), true);
  assert.equal(isNarrowWidth(NARROW_MAX_PX), false, "the threshold itself is wide");
  assert.equal(isNarrowWidth(640), false, "the snap point");
  assert.equal(isNarrowWidth(-5), false, "garbage never narrows");
});

test("the two narrow thresholds agree (shell + the board's module-owned copy)", () => {
  // one value: the settings panel and the board flip on the same
  // rendered width, or the sidebar would show both layouts at once
  assert.equal(BOARD_NARROW_MAX_PX, NARROW_MAX_PX);
  for (const px of [0, 100, 300, 360, 400, 640]) {
    assert.equal(boardIsNarrowWidth(px), isNarrowWidth(px), `at ${px}px`);
  }
});

test("SettingsTab binds the width-driven narrow class", () => {
  const src = readFileSync(
    join(root, "app", "components", "settings", "SettingsTab.vue"),
    "utf8",
  );
  assert.match(src, /narrow: narrow/, "the root class binding");
  assert.match(src, /useNarrow\(rootEl\)/, "the measurement composable");
  assert.match(src, /ref="rootEl"/, "the root carries the observe target");
  // the narrow rules exist in the stylesheet (card t_ffab9823: NO block
  // rule on .settings.narrow itself - the root keeps no padding/gap in
  // narrow either; the rules are per-knob only)
  const css = readFileSync(join(root, "app", "assets", "css", "main.css"), "utf8");
  assert.ok(/\.settings\.narrow \.setting-item/.test(css), "the narrow rules are in");
});

test("the shell narrow composable exists and is mounted-safe", () => {
  const src = readFileSync(join(root, "app", "composables", "narrow.ts"), "utf8");
  assert.match(src, /export const NARROW_MAX_PX = 360/);
  assert.match(src, /ResizeObserver/);
  assert.match(src, /typeof ResizeObserver === "undefined"/, "guards ancient hosts");
});

test("Board.vue mounts its narrow observer on its root", () => {
  const board = join(root, "..", "..", "modules", "kanban", "ui", "Board.vue");
  const boardEl = join(root, "..", "..", "modules", "kanban", "ui", "board", "boardLogic.ts");
  assert.ok(existsSync(board));
  assert.ok(existsSync(boardEl));
  const src = readFileSync(board, "utf8");
  assert.match(src, /narrow: narrow/, "the root class binding");
  assert.match(src, /isNarrowWidth/);
  assert.match(src, /ResizeObserver/);
  // the compact view's rules are in the module's own styles
  assert.match(src, /\.board\.narrow \.board-scroll/, "stacked columns rule");
});