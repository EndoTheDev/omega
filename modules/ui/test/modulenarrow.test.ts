// omega webui: unit tests for the module-UI responsive contract
// (card t_a2fdc630). The recipe = the kanban Board approach: each
// component measures its OWN rendered container and flips a `narrow`
// class below the threshold. The measured flip itself is browser code;
// what the node suite pins is the source wiring (the composition-test
// convention) plus the PURE threshold agreement across every copy.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

import { isNarrowWidth, NARROW_MAX_PX } from "../app/composables/narrow.ts";
import { isNarrowWidth as cronIsNarrowWidth } from "../../cron/ui/narrow.ts";
import { isNarrowWidth as logsIsNarrowWidth } from "../../http_channel/ui/narrow.ts";
import { isNarrowWidth as skillsIsNarrowWidth } from "../../skills/ui/narrow.ts";
import { isNarrowWidth as toolsIsNarrowWidth } from "../../tools/ui/narrow.ts";
// the recipe's original: the Board's module-owned copy must stay in the
// fold too (a drag between hosts must show one adaptation, not two)
import {
  isNarrowWidth as boardIsNarrowWidth,
} from "../../kanban/ui/board/boardLogic.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const modules = resolve(root, "..", ".."); // the worktree root
const read = (...p: string[]) => readFileSync(join(...p), "utf8");

test("every narrow copy agrees with the shell threshold (shell + all modules)", () => {
  // one flip point: a component dragged between hosts (inspector
  // column -> main pane, drawer -> dock, bottom panel later) must show
  // ONE adaptation, not both layouts at once. The copy per module is
  // deliberate (module UI never imports shell code); the VALUE is
  // shared and pinned here.
  for (const [name, fn] of [
    ["cron", cronIsNarrowWidth],
    ["http_channel", logsIsNarrowWidth],
    ["skills", skillsIsNarrowWidth],
    ["tools", toolsIsNarrowWidth],
    ["kanban", boardIsNarrowWidth],
  ] as const) {
    for (const px of [0, 100, 240, 300, 360, 400, 640]) {
      assert.equal(fn(px), isNarrowWidth(px), `${name} at ${px}px`);
    }
  }
  assert.equal(NARROW_MAX_PX, 360);
});

// --- component wiring: the contract's shape, per component ------------

// [component path, module ("null" = shell-owned), scoped narrow rules
// that must ship in that SFC's own stylesheet]
const RETROFIT: Array<{
  rel: string;
  label: string;
  module: "cron" | "http_channel" | "skills" | "tools" | null;
  rules: RegExp[];
}> = [
  {
    rel: "modules/cron/ui/CronJobs.vue",
    label: "cron",
    module: "cron",
    rules: [/\.cron-list\.narrow \{/, /\.cron-list\.narrow \.cron-head \{/],
  },
  {
    rel: "modules/http_channel/ui/LogsList.vue",
    label: "logs",
    module: "http_channel",
    rules: [/\.logs-list\.narrow \./],
  },
  {
    rel: "modules/skills/ui/SkillsList.vue",
    label: "skills",
    module: "skills",
    rules: [/\.skills-list\.narrow \{/],
  },
  {
    rel: "modules/tools/ui/ToolsList.vue",
    label: "tools",
    module: "tools",
    rules: [/\.tools-list\.narrow \{/],
  },
  {
    // the shell's own inspector Files tab rides the same recipe
    rel: "modules/ui/app/components/files/FilesTab.vue",
    label: "files",
    module: null,
    rules: [/\.inspector-panel\.narrow \.file-preview \{/],
  },
];

for (const entry of RETROFIT) {
  test(`narrow wiring (contract shape): ${entry.label}`, () => {
    const src = read(modules, ...entry.rel.split("/"));
    assert.match(src, /narrow: narrow/, "the root class binding");
    assert.match(src, /ref="rootEl"/, "the root carries the observe target");
    // the observer (module-owned copy of the Board's inline block, or
    // the shell composable for shell components) actually measures
    if (entry.module === null) {
      assert.match(src, /from "\~\/composables\/narrow"/, "shell-owned: the shell composable");
      assert.match(src, /useNarrow\(rootEl\)/, "the shell composable drives the flip");
    } else {
      assert.match(src, /\.observe\(rootEl\.value\)/, "the root is observed");
      assert.match(src, /typeof ResizeObserver === "undefined"/, "guards ancient hosts");
      assert.match(src, /requestAnimationFrame/, "never delivers inside the observer (loop lesson)");
      assert.match(src, /disconnect\(\)/, "clean teardown on unmount");
      assert.match(src, /from "\.\/narrow"/, "module-owned copy - module UI never imports shell code");
      assert.ok(!src.includes("composables/omega"), "no shell client dependency");
      assert.ok(!src.includes("composables/narrow"), "no shell composable import");
    }
    // both paths guard the ancient-host + zero-width + rAF lessons (the
    // shell composable carries them in narrow.ts; module copies inline)
    if (entry.module !== null) {
      assert.match(src, /w > 0/, "zero-width measure noise is never narrow");
      // mount measures NOW: a remount-in-place tab gets no further
      // resize entry, the class cannot wait for the next width change
      assert.match(
        src,
        /narrow\.value = isNarrowWidth\(Math\.round\(rootEl\.value\.getBoundingClientRect\(\)\.width\)\)/,
        "the mount-measure line",
      );
    }
    for (const rule of entry.rules) {
      assert.match(src, rule, "its narrow rules ship in its own stylesheet");
    }
  });
}

test("FilesTab passes the narrow flag down to the recursive tree", () => {
  const tab = read(modules, "modules", "ui", "app", "components", "files", "FilesTab.vue");
  assert.match(tab, /:narrow="narrow"/, "the host passes its measurement down");
  const tree = read(modules, "modules", "ui", "app", "components", "files", "FileTree.vue");
  assert.match(tree, /narrow\?: boolean/, "the child accepts the inherited flag");
  assert.match(tree, /:narrow="narrow"/, "the recursion forwards it");
  assert.match(tree, /\.files-tree\.narrow \.size \{/, "the condensed-columns rule");
  // and the tree never grows its own observer
  assert.ok(!tree.includes("ResizeObserver"), "one measurable root: the tree inherits");
});

test("the sweep's inventory is explicit - settings and the Board keep their adaptations", () => {
  // every inspector surface named by the card; insights/extensions
  // views are not shipped yet (checked at round start, 2026-10-21).
  const settings = read(modules, "modules", "ui", "app", "components", "settings", "SettingsTab.vue");
  assert.match(settings, /narrow: narrow/, "settings keeps its narrow adaptation (card t_49d0b308)");
  const board = read(modules, "modules", "kanban", "ui", "Board.vue");
  assert.match(board, /narrow: narrow/, "the Board IS the recipe");
});