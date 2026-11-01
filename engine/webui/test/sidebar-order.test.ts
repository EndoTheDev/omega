// omega webui: the sessions panel's ruled top-to-bottom order. Card
// t_311fc40b (Endo's ruling 2026-10-30) ruled "band, button, search";
// card t_7ee0a673 (Endo's comment 3, 2026-11-01) SUPERSEDES it: the
// search seats IN the band's top right (the band-tool slot), the old
// search row below the band dies, and the ruled order is
//   band [title + search] -> new-session button -> session rows
// Card t_7ee0a673's second mandate: ONE ALIGNED WIDTH - the band's own
// 8px inset is the single inset for the whole sidebar stack (title,
// button row, section headers, rows), no stair-step. The node suite has
// no DOM, so the order AND the geometry are pinned structurally on the
// source - the same contract style tabband.test.ts uses. The mobile
// drawer renders the SAME component (app.vue mounts AppSidebar twice),
// so one source order covers both surfaces.
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

describe("AppSidebar: the ruled panel order (card t_7ee0a673)", () => {
  it("renders band [title+search] -> new-session button -> rows, top to bottom", () => {
    const band = indexOf(/<TabBand title="Sessions">/);
    const search = indexOf(/<SearchInput/);
    const buttonRow = indexOf(/class="new-btn"/);
    const list = indexOf(/<ul class="session-list">/);
    assert.ok(band >= 0, "the band renders");
    assert.ok(search >= 0, "the search renders");
    assert.ok(buttonRow >= 0, "the new-session button renders");
    assert.ok(list >= 0, "the session rows render");
    assert.ok(band < search, "the band comes FIRST");
    assert.ok(search < buttonRow, "the search seats INSIDE the band, before the button row");
    assert.ok(buttonRow < list, "the button row comes BEFORE the session rows");
  });

  it("the search seats IN the band's top right (the band-tool slot, card t_7ee0a673)", () => {
    const bandStart = sidebar.indexOf('<TabBand title="Sessions">');
    const bandEnd = sidebar.indexOf("</TabBand>", bandStart);
    assert.ok(bandEnd > bandStart, "the band is no longer self-closing - it owns a slot");
    const bandChunk = sidebar.slice(bandStart, bandEnd);
    assert.match(bandChunk, /<SearchInput/, "the search input lives in the band");
    assert.match(bandChunk, /v-model="query"/, "the same query ref binds in-band (behavior intact)");
    // the NEW-session control stays OUT of the band (card t_311fc40b's
    // ruling survives the seat change: only the search moved in)
    assert.doesNotMatch(
      sidebar.slice(bandStart, bandEnd + 40),
      /\+ New Session/,
      "the split button does not live in the band",
    );
  });

  it("the search's old row below the band is DEAD (t_311fc40b's three-row law is gone)", () => {
    assert.doesNotMatch(sidebar, /class="session-search"/, "no .session-search wrapper in the template");
    assert.doesNotMatch(mainCss, /\.session-search/, "no .session-search selector in the shared sheet");
  });

  it("the new-session control keeps its OWN full-width row below the band", () => {
    const bandEnd = sidebar.indexOf("</TabBand>");
    const row = sidebar.match(/\.new-row\s*\{[^}]*\}/)?.[0] ?? "";
    assert.ok(row.length > 0, "the button row rule exists");
    assert.ok(sidebar.indexOf('class="new-btn"') > bandEnd, "the row renders after the band");
    assert.match(row, /padding:\s*8px;/, "the row rides the band's rhythm (top) and insets");
    assert.match(row, /position:\s*relative/, "the row anchors the type picker dropdown");
  });

  it("the caret type-picker travels WITH the button row, not the band", () => {
    const rowStart = sidebar.indexOf('class="new-btn"');
    const listAt = sidebar.indexOf('class="session-list"');
    const rowChunk = sidebar.slice(rowStart, listAt);
    assert.match(rowChunk, /class="new-caret"/, "the caret button rides the row");
    assert.match(rowChunk, /v-for="entry in newMenuEntries"/, "the picker dropdown renders from the row");
    assert.doesNotMatch(rowChunk, /<SearchInput/, "the search does NOT share the button row");
  });

  it("ONE ALIGNED WIDTH - the band's 8px inset is the whole stack's inset (card t_7ee0a673)", () => {
    // the geometry reference: the band component's own sides
    const tabband = src("app/components/ui/TabBand.vue");
    const bandRule = tabband.match(/\.tab-band\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(bandRule, /padding:\s*0\s+8px/, "the band title insets 8px - the reference edge");
    // the button row: same 8px sides (pinned above via .new-row)
    const newRow = sidebar.match(/\.new-row\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(newRow, /padding:\s*8px;/, "the button row seats on the band's 8px edges, not 12px");
    // the section headers: their OWN side padding is zero - the list's
    // 8px sides do the inset, so header text starts at the title's edge
    const label = mainCss.match(/button\.bucket-label\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(label, /padding:\s*10px\s+0\s+4px/, "no 10px header indent - no stair-step");
    // the rows: the list box carries the 8px sides
    const list = mainCss.match(/\.session-list\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(list, /padding:\s*0\s+8px/, "the row boxes share the same 8px edges");
  });

  it("the in-band input keeps the band's 22px row - compact padding, no height growth", () => {
    const seat = sidebar.match(/\.band-search :deep\(\.search-input input\)\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(seat, /padding:\s*2px/, "the band seat's input is compact (the shared 5px grows the band)");
  });

  it("the narrow adaptation stacks the band by MEASURE, not a breakpoint guess", () => {
    assert.match(sidebar, /BAND_STACK_PX\s*=\s*\d+/, "the stack threshold is a named constant");
    assert.match(sidebar, /new ResizeObserver/, "the width is measured on the aside");
    assert.match(sidebar, /stacked\.value = w < BAND_STACK_PX/, "the class flips on the measured width");
    const wrap = sidebar.match(/\.sessions\.stacked :deep\(\.tab-band\)\s*\{[^}]*\}/)?.[0] ?? "";
    assert.match(wrap, /flex-wrap:\s*wrap/, "the stacked band wraps: title row, then input row");
  });

  it("the scrollport contract holds - band and rows stay OUT of .session-list", () => {
    const bandAt = sidebar.indexOf("<TabBand");
    const listAt = sidebar.indexOf('<ul class="session-list">');
    assert.ok(bandAt < listAt, "the band renders BEFORE the scrollport (tabband.test.ts law)");
    assert.doesNotMatch(sidebar, /class="sessions-head"/, "no old floating head row returns");
  });
});