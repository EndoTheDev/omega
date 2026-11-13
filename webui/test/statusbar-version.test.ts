// omega webui: unit tests for the statusbar version shortening (card
// t_f6c1b5fe): the bar shows tag + short hash ("v0.8.1 fe5b010"), the
// FULL string keeps living in /health and in the segment's tooltip.
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

import { shortVersion } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

// --- the formatter: tag + 7-char hash, display only ---

test("tagged pseudo version shortens to tag + short hash", () => {
  assert.equal(
    shortVersion("v0.8.1-0.20261019172201-fe5b0106e0f7"),
    "v0.8.1 fe5b010",
  );
});

test("hash is cut to 7 chars", () => {
  assert.equal(
    shortVersion("v1.2.3-0.1-abcdef0123456789"),
    "v1.2.3 abcdef0",
  );
});

test("plain tagged build keeps just the tag", () => {
  assert.equal(shortVersion("v0.8.1"), "v0.8.1");
});

test("unparsable strings pass through unchanged (never hide a build)", () => {
  assert.equal(shortVersion("dev"), "dev");
  assert.equal(shortVersion("v0.8.1-rc.1"), "v0.8.1-rc.1");
  assert.equal(shortVersion(""), "");
});

// --- the presentation: short in the bar, full in the tooltip ---

const bar = src("app/components/shell/StatusBar.vue");

test("the bar renders the shortened string, never the raw one", () => {
  assert.ok(bar.includes("{{ shortVersion(version) }}"), "bar shows the short form");
  assert.ok(!bar.includes("{{ version }} ·"), "raw version no longer in the bar text");
});

test("the tooltip keeps the FULL version string", () => {
  // card t_be30543a: the tooltip sheds "and uptime" with the segment's
  // uptime half (the popover owns that number now); the raw version stays.
  const m = bar.match(/:title="`server build - \$\{version\}`"/);
  assert.ok(m, "title binding still carries the raw version");
  assert.doesNotMatch(bar, /server build and uptime/, "the tooltip promises only the build");
});

// --- the pane corner seam: strip = the card's top half ---

const css = src("app/assets/css/main.css");

test("the tab strip rounds its top corners and keeps the bottom square", () => {
  assert.match(css, /\.dock-tabbar \{[^}]*border-radius: var\(--r-pane\) var\(--r-pane\) 0 0/, "strip: rounded top, square bottom");
});

test("the tucked card pulls its rounded top corners under the strip", () => {
  assert.match(
    css,
    /\.dock-pane > \.dock-content,\n\.dock-pane > \.dock-empty \{ margin-top: calc\(-1 \* var\(--r-pane\)\)/,
    "pane below tucks under the strip",
  );
});

test("mobile untucks the card (the strip is display:none there)", () => {
  const media = css.slice(css.indexOf("@media (max-width: 768px)"));
  assert.match(media, /\.dock-pane > \.dock-content, \.dock-pane > \.dock-empty \{ margin-top: 0; \}/);
});

// --- the truncate treatment (card t_3599d33b): the build is the bar's
// only unbounded string, so the segment owns its width -------------------

test("a nightly tag passes through whole (why the truncate exists)", () => {
  // shortVersion only rewrites the pseudo-version shape; a nightly tag
  // (v0.17.1-nightly.20261112-d57ffb37, 33 chars) reaches the bar intact -
  // the CSS truncate is what keeps it from pushing the layout.
  assert.equal(
    shortVersion("v0.17.1-nightly.20261112-d57ffb37"),
    "v0.17.1-nightly.20261112-d57ffb37",
  );
});

test("the build segment carries the truncate treatment on its text span", () => {
  const seg = bar.match(/<span\s+v-if="version"\s+class="([^"]*)"[\s\S]*?\{\{ shortVersion\(version\) \}\}/);
  assert.ok(seg, "the build segment is still in the bar");
  // the cap rides the INNER span: a display declaration on the .seg would
  // out-rule .hide-sm and put the build back on the narrow bar.
  assert.match(seg![0], /<span class="build-text">/, "the text is wrapped for truncation");
  assert.match(seg![1], /\bhide-sm\b/, "the seg still collapses on the narrow bar");
});

test("the truncate rule caps, clips and ellipsizes the build text", () => {
  const rule = css.match(/\.statusbar \.build-text \{[^}]*\}/)?.[0] ?? "";
  assert.ok(rule, "the build text owns a rule");
  assert.match(rule, /max-width: \d+ch/, "a ch cap, so the footprint follows the mono glyph width");
  assert.match(rule, /min-width: 0/, "no floor: a short tag keeps its natural width at the bar's edge");
  assert.match(rule, /overflow: hidden/, "text is clipped at the cap");
  assert.match(rule, /text-overflow: ellipsis/, "the clip reads as an ellipsis, not a cut glyph");
  assert.match(rule, /white-space: nowrap/, "one line - a bar segment never wraps");
});

test("the build text is a block, never its own flex container", () => {
  // a flex container does not ellipsize its own text: the text becomes an
  // anonymous flex item and text-overflow cannot clip it.
  const rule = css.match(/\.statusbar \.build-text \{[^}]*\}/)?.[0] ?? "";
  assert.match(rule, /display: block/);
  assert.doesNotMatch(rule, /display: flex/);
});