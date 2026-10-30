// omega webui: the sessions panel's ruled top-to-bottom order (card
// t_311fc40b, Endo's ruling 2026-10-30): tab band, then the new-session
// button on its own row, then search, then the session rows. The node
// suite has no DOM, so the ORDER is pinned structurally on the template
// source - the same contract style tabband.test.ts uses - plus the CSS
// facts that seat each row (band title alone; the button row's shared
// 12px side edges with the search; the search still first ABOVE the
// .session-list scrollport). The mobile drawer renders the SAME
// component (app.vue mounts AppSidebar twice), so one source order
// covers both surfaces.
// Run: node --test test/sidebar-order.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

const sidebar = src("app/components/sessions/AppSidebar.vue");
const mainCss = src("app/assets/css/main.css");

// template landmarks, in SOURCE order (the file renders top-to-bottom)
const indexOf = (re: RegExp): number => sidebar.search(re);

describe("AppSidebar: the ruled panel order (card t_311fc40b)", () => {
  it("renders band -> new-session button -> search -> rows, top to bottom", () => {
    const band = indexOf(/<TabBand title="Sessions"/);
    const buttonRow = indexOf(/class="new-btn"/);
    const search = indexOf(/<SearchInput/);
    const list = indexOf(/<ul class="session-list">/);
    assert.ok(band >= 0, "the band renders");
    assert.ok(buttonRow >= 0, "the new-session button renders");
    assert.ok(search >= 0, "the search renders");
    assert.ok(list >= 0, "the session rows render");
    assert.ok(band < buttonRow, "the band comes BEFORE the new-session button");
    assert.ok(buttonRow < search, "the new-session button comes BEFORE the search");
    assert.ok(search < list, "the search comes BEFORE the session rows");
  });

  it("the band carries the title ALONE - the new-session button is OUT of it", () => {
    const bandStart = sidebar.indexOf('<TabBand title="Sessions"');
    const bandEnd = sidebar.indexOf("/>", bandStart);
    const bandChunk = sidebar.slice(bandStart, bandEnd);
    // self-closing band: no slot, no actions inside
    assert.doesNotMatch(
      sidebar.slice(bandStart, bandEnd + 40),
      /\+ New Session/,
      "the split button does not live in the band",
    );
    assert.doesNotMatch(bandChunk, /class="head-actions"/, "the old band actions wrapper is gone");
  });

  it("the old band-actions plumbing is dead - no orphan wrappers or selectors", () => {
    assert.doesNotMatch(sidebar, /head-actions/, "no .head-actions element or selector survives");
    assert.doesNotMatch(
      sidebar,
      /:deep\(\.tab-band-actions\)/,
      "the band-actions flex helper died with the empty slot",
    );
  });

  it("the button row seats on the SAME 12px side edges as the search row", () => {
    const row = sidebar.match(/\.new-row\s*\{[^}]*\}/)?.[0] ?? "";
    const search = mainCss.match(/\.session-search\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(row, /padding:\s*8px\s+12px/, "the button row: band rhythm top, 12px sides");
    assert.match(search, /padding:\s*0\s+12px\s+8px/, "the search row: same 12px sides");
    assert.match(row, /position:\s*relative/, "the row anchors the type picker dropdown");
  });

  it("the caret type-picker travels WITH the button row, not the band", () => {
    const rowStart = sidebar.indexOf('class="new-btn"');
    const searchAt = sidebar.indexOf('class="session-search"');
    assert.ok(searchAt > rowStart, "the button row precedes the search row");
    const rowChunk = sidebar.slice(rowStart, searchAt);
    assert.match(rowChunk, /class="new-caret"/, "the caret button rides the row");
    assert.match(rowChunk, /v-for="entry in newMenuEntries"/, "the picker dropdown renders from the row");
    assert.doesNotMatch(rowChunk, /<SearchInput/, "the search does NOT share the button row");
  });

  it("the scrollport contract holds - band and rows stay OUT of .session-list", () => {
    const bandAt = sidebar.indexOf("<TabBand");
    const listAt = sidebar.indexOf('<ul class="session-list">');
    assert.ok(bandAt < listAt, "the band renders BEFORE the scrollport (tabband.test.ts law)");
    assert.doesNotMatch(sidebar, /class="sessions-head"/, "no old floating head row returns");
  });
});