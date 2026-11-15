// omega webui: card t_63582d1f - the file tree goes CHEVRONLESS. Endo's
// three rulings, pinned structurally in the sources (the node suite has
// no DOM: what is pinned is the wiring):
//   1. chevrons die      - no expand/collapse triangle, no .chev left in
//                          the tree's sources, the dead main.css rule gone
//   2. the icon carries  - the row glyph maps through rowicon.ts
//                          folderIcon(isOpen(...)), the row exposes
//                          aria-expanded + tabindex, Enter/Space activate
//   3. hover indent guides - a ::before hairline overlay per nesting
//                          level, opacity-revealed by :hover only (no
//                          mousemove anywhere), no layout shift
// Run: node --test webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

const tree = read("../harness/files/ui/FileTree.vue");
const css = read("app/assets/css/main.css");

// --- ruling 1:chevrons die ---

test("the file tree carries no chevron vocabulary anywhere", () => {
  assert.doesNotMatch(
    tree,
    /class="[^"]*chev/,
    "FileTree.vue renders no .chev span",
  );
  assert.doesNotMatch(
    tree,
    /chevron-(down|up|left|right)/,
    "no chevron iconset glyph rides the tree",
  );
  assert.doesNotMatch(
    css,
    /\.files-tree \.chev/,
    "main.css's tree expander rule is gone (the glyph died, its CSS died)",
  );
});

// --- ruling 2: the folder icon carries the state ---

test("the row glyph binds the open-state folder mapping, not a fixed icon", () => {
  assert.match(
    tree,
    /:name="rowGlyphIcon\(entry\)"/,
    "the row glyph rides the state-capable name function",
  );
  assert.match(
    tree,
    /if \(entry\.is_dir\) return folderIcon\(isOpen\(entryPath\(entry\)\)\)/,
    "folders map through folderIcon(isOpen(...)) - the ICON is the state",
  );
  // and the mapping itself stays pinned (open/closed distinct):
  // rowicon.test.ts owns the table; this side pins the wiring only.
});

test("the row carries the a11y affordance the chevron replaced", () => {
  assert.match(
    tree,
    /role="treeitem"/,
    "rows expose the treeitem role the triangle used to imply",
  );
  assert.match(
    tree,
    /:aria-expanded="entry\.is_dir \? isOpen\(entryPath\(entry\)\) : undefined"/,
    "aria-expanded lives ON the row, folder rows only",
  );
  assert.match(tree, /:tabindex="0"/, "rows are focusable (keyboard reachable)");
  assert.match(
    tree,
    /@keydown\.enter\.prevent="activate\(entry\)"/,
    "Enter activates a focused row",
  );
  assert.match(
    tree,
    /@keydown\.space\.prevent="activate\(entry\)"/,
    "Space activates a focused row",
  );
  // one activation path: clicks and keys flow through the same function
  assert.match(
    tree,
    /@click="activate\(entry, \$event\)"/,
    "clicks hit the same activation used by the keyboard",
  );
  // the ARIA tree/group ladder: the root is the tree, branches are groups
  assert.match(
    tree,
    /:role="depth > 0 \? 'group' : 'tree'"/,
    "the root ul is role=tree, nested uls are role=group",
  );
});

// --- ruling 3: hover indent guides ---

test("indent guides are overlay hairlines revealed by :hover only", () => {
  assert.match(
    tree,
    /\.files-tree\.guided::before/,
    "the guide is a ::before overlay, never flow content",
  );
  assert.match(
    tree,
    /opacity: 0;[^}]*transition: opacity/,
    "resting guides are invisible and the reveal is an opacity fade",
  );
  assert.match(tree, /transition: opacity 0\.15s ease/, "fast subtle fade");
  assert.match(
    tree,
    /\.files-tree:hover \.files-tree::before/,
    ":hover on the tree container reveals every descendant level's line",
  );
  assert.ok(
    !tree.includes("mousemove") && !tree.includes("mouseleave"),
    "CSS :hover drives the reveal - zero listeners per row",
  );
  assert.match(
    tree,
    /"\-\-guide-x": `\$\{props\.depth \* 4 \+ 6\}px`/,
    "each level ul positions its own line from the depth step",
  );
  // layout-shift guard: the overlay never participates in flow
  assert.match(
    tree,
    /position: absolute;[\s\S]{0,200}?pointer-events: none;/,
    "absolute + pointer-events-none: the line paints, it never pushes",
  );
  // narrow mode keeps the law (no narrow exemption carved into the CSS)
  assert.ok(
    !tree.includes(".narrow::before") && !tree.includes(".narrow.guided"),
    "the narrow adaptation gets no guide exemption",
  );
});

// --- the indent law as of card t_856a1faa (2026-11-15, Endo's tuned
// numbers from the files tuner): row height 26 -> 24, base 10 -> 8,
// step 10 -> 4, level pad 8 -> 4, guide offset 12 -> 6, icon gap 7 -> 6,
// list gap 8 -> 6. The reading size stays 12px. His numbers outrank the
// old pins - this test carries the NEW contract, at full strength. ---

test("the density/indent law pins the tuned step, base and rhythm", () => {
  // the 4px step and the +8 base remain the indent math on BOTH sides
  assert.match(
    tree,
    /props\.depth \* 4 \+ 8}px`/,
    "row indent math is 8px base + a 4px step",
  );
  assert.match(
    tree,
    /props\.depth \* 4 \+ 6}px`/,
    "guide-x rides the same 4px step, 6px before the glyph column",
  );
  // the loading row sits one glyph column past its level's row indent
  assert.match(
    tree,
    /`\$\{depth \* 4 \+ 22\}px`/,
    "the loading row tracks the row indent (8 + 4d) plus 14px",
  );
  // whole-rule pins: a value drifting back toward the old geometry is a red
  // the ROOT ul keeps its untouched inline padding (8px both sides) - the
  // contract names the per-level left pad only, never the container rhythm.
  assert.match(
    css,
    /\.files-tree \{\n  list-style: none;\n  margin: 0;\n  padding: 0 8px;\n\}/,
    "the root tree ul keeps its 8px inline padding",
  );
  assert.match(
    css,
    /\.files-tree \.files-tree \{\n  padding-left: 4px;\n\}/,
    "only a nested level ul pads 4px left - the per-level indent",
  );
  // the root rule must not be the nesting rule (no shorthand smuggling the
  // 4px onto the root's left or onto any level's right edge)
  assert.doesNotMatch(
    css,
    /\.files-tree \{\n  list-style: none;\n  margin: 0;\n  padding: 0 4px;\n\}/,
    "the per-level pad never rides the root container shorthand",
  );
  assert.match(
    css,
    /\.tree-row \{\n  display: flex;\n  align-items: baseline;\n  gap: 6px;\n {2}height: 24px;\n {2}padding-block: 4px;\n {2}padding-inline-end: 10px;\n {2}border-radius: var\(--r-chip\);\n {2}font-size: 12px;\n {2}cursor: pointer;\n\}/,
    "the row is 24px tall with a 6px icon gap and a 12px reading size",
  );
  assert.match(
    css,
    /\.files-list \{\n  list-style: none;\n  margin: 0 0 6px;\n  padding: 0 8px;\n\}/,
    "the status list keeps its 6px gap above the tree",
  );
});