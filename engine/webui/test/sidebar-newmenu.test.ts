// omega webui: source-check for the sidebar's new-session control (card
// t_55a571ff): the split button reads "+ New Session" and the type
// picker is DATA - three entries (New Session, Ephemeral, Continuous),
// the Continuous placeholder rendering disabled with the design-pending
// note (Endo ruling: ship the label now, wire behavior in the
// continuous-agents arc). The template renders the list, so the
// contract is pinned against AppSidebar.vue's source.
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

const sidebar = src("app/components/sessions/AppSidebar.vue");
// the picker's data lives in one array - match only inside it, never the
// whole component (other sites carry label-like strings)
const picker =
  sidebar.match(/const newMenuEntries: NewMenuEntry\[\] = \[([\s\S]*?)\n\s*\];/)?.[1] ??
  "";

describe("AppSidebar: the new-session control (card t_55a571ff)", () => {
  it('the main chip reads "+ New Session"', () => {
    assert.match(sidebar, /\+\s*New Session/, "the split button's body label");
  });

  it("the picker holds the three entries, in order, as data", () => {
    const labels = [...picker.matchAll(/label:\s*"([^"]+)",/g)].map((m) => m[1]);
    assert.deepEqual(labels, ["New Session", "Ephemeral", "Continuous"]);
    assert.equal(labels.length, 3, "exactly three entries ship now");
  });

  it("the Continuous entry is wired - it opens the continuous tab (card t_72ecca67)", () => {
    const entry = picker.match(/\{[\s\S]{0,300}?label:\s*"Continuous",[\s\S]*?\},/);
    assert.ok(entry, "the Continuous entry exists as its own object");
    assert.doesNotMatch(entry[0], /disabled:\s*true/, "the placeholder is no longer disabled");
    assert.match(entry[0], /run:\s*\(\)\s*=>\s*emit\("new-continuous"\)/, "the entry emits new-continuous");
    // card t_55a571ff's disabled-dim rule still holds for any future
    // disabled entry - the styling stays even now nothing uses it.
    assert.match(sidebar, /\.new-menu-item:disabled\b/, "the disabled styling survives");
  });

  it("the picker renders from the data list - a disabled entry renders inert", () => {
    assert.match(sidebar, /v-for="entry in newMenuEntries"/, "the menu is the data list");
    assert.match(sidebar, /:disabled="entry\.disabled"/, "the disabled flag renders");
    assert.match(
      sidebar,
      /\.new-menu-item:disabled\b/,
      "the disabled entry gets the dim, inert styling",
    );
  });

  it("the ephemeral entry still routes through the existing opener", () => {
    assert.match(sidebar, /run:\s*\(\)\s*=>\s*pickNew\(true\)/, "Ephemeral mints the draft");
  });
});

describe("AppSidebar: the caret affordance (card t_a0a466c6)", () => {
  it("the caret zone is its own visual zone with a divider", () => {
    const caret = sidebar.match(/\.new-caret\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(caret, /border-left:\s*1px solid var\(--stroke\)/, "the hairline divider uses the chip border color");
  });

  it("hovering the caret lights the caret zone with the z2 token", () => {
    const hover = sidebar.match(/\.new-caret:hover\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(hover, /background:\s*var\(--z2\)/, "the caret zone's hover background");
  });

  it("the chevron renders at 12x12 through the iconset wrapper (card t_502b8e81)", () => {
    // the hand-rolled svg is gone - the caret is the wrapper's
    // chevron-down at 12, the 8px inline chevron with it
    const at = sidebar.indexOf('class="new-caret-chevron"');
    assert.notEqual(at, -1, "the caret chip is the wrapper's chevron-down");
    const start = sidebar.lastIndexOf("<Icon", at);
    const end = sidebar.indexOf("/>", at);
    const chunk = sidebar.slice(start, end);
    assert.match(chunk, /name="chevron-down"/, "the vocabulary name");
    assert.match(chunk, /:size="12"/, "12px, up from the invisible 8x8");
    assert.ok(
      !sidebar.includes("<svg"),
      "no hand-rolled svg in the sidebar any more",
    );
  });
});

describe("AppSidebar: the caret opens on a real click (card t_902f7ba5)", () => {
  // the suite has no DOM, so the real-pointer repro lives outside it
  // (live driver proof, 10 consecutive real clicks on the caret). What
  // the suite CAN pin is the mechanism that made real clicks die: the
  // clickaway listener catching its own opening event. The guard is
  // the contract - a pointer inside .new-btn is the toggle, never
  // clickaway, and the guard must return BEFORE the close.
  it("the clickaway listener guards against its own opening click", () => {
    const body =
      sidebar.match(/function onDocClick\([^)]*\)\s*\{([\s\S]*?)\n  \}/)?.[1] ?? "";
    assert.ok(body.trim().length > 0, "onDocClick exists");
    assert.match(
      body,
      /closest\?\.\(\s*["'`]\.new-btn["'`]\s*\)/,
      "the guard: a click inside the split button never counts as clickaway",
    );
    assert.ok(
      body.indexOf("closest") >= 0 &&
        body.indexOf("closest") < body.indexOf("newMenuOpen.value = false"),
      "the guard returns before the close runs",
    );
    assert.match(
      body,
      /newMenuOpen\.value = false/,
      "clickaway still closes the menu on real outside clicks",
    );
  });
});