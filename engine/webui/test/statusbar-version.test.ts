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
  const m = bar.match(/:title="`server build and uptime - \$\{version\}`"/);
  assert.ok(m, "title binding still carries the raw version");
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