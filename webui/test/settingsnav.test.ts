// omega webui: unit tests for the settings ONE-DOCUMENT pane (card
// t_8b464090, which SUPERSEDES the master-detail nav of t_af7e1150): the
// per-knob subgroup DATA, the two-level tree it derives, the document the
// pane paints (EVERY category, in nav order, filtered in place), the nav's
// grey-out set, the last-anchor resolution - and the SOURCE pins that prove
// the selector/two-screen model is gone.
//
// The node suite has no DOM: the document, the anchor and the grey set are
// pinned as pure functions, the pane as structural assertions on
// SettingsTab.vue (the live scroll behaviour is the journey's job, recipes
// 110/113).
// Run: node --test webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import type { SettingGroup, SettingKnob } from "../app/composables/omega.ts";
import {
  COMMON_CATEGORY,
  GENERAL_SUBGROUP,
  KNOB_SUBGROUPS,
  NAV_STORAGE_KEY,
  SUBGROUP_LABELS,
  anchorSelection,
  buildNav,
  documentFor,
  expandedCategoryIds,
  knobSubgroup,
  matchingCategoryIds,
  parseSelection,
  resolveSelection,
  selectionId,
  spyTargetIds,
  subgroupLabel,
  withSubgroups,
} from "../app/components/settings/settingsNav.ts";

// --- fixtures -------------------------------------------------------------

const knob = (
  key: string,
  label: string,
  value: unknown = "",
  extra: Partial<SettingKnob> = {},
): SettingKnob => ({ key, label, type: "text", value, ...extra });

const group = (key: string, label: string, knobs: SettingKnob[]): SettingGroup => ({
  key,
  label,
  knobs,
});

// a provider domain with two declared children (connection, models)
const providerGroup = () =>
  group("provider", "provider", [
    knob("provider.type", "type", "ollama"),
    knob("provider.host", "base url", ""),
    knob("provider.model_name", "model name", "glm-5.3"),
  ]);

// every knob of this domain is undeclared -> one implicit "general" child
const logGroup = () =>
  group("unknown", "unknown", [
    knob("custom.one", "one knob", "x"),
    knob("custom.two", "another knob", "y"),
  ]);

const commonGroup = () =>
  group(COMMON_CATEGORY, "Commonly Used", [knob("provider.model_name", "model name", "glm-5.3")]);

// --- the nesting DATA -----------------------------------------------------

test("the table is data per knob, never a derivation from the key", () => {
  // the card's own example: theme -> appearance
  assert.equal(knobSubgroup("chat.theme"), "appearance");
  assert.equal(knobSubgroup("chat.color_mode"), "appearance");
  assert.equal(knobSubgroup("provider.model_name"), "models");
  assert.equal(knobSubgroup("provider.host"), "connection");
  // a knob with no line lands in the implicit general child
  assert.equal(knobSubgroup("totally.undeclared"), GENERAL_SUBGROUP);
  // and the lookup is NOT "first path segment" or any other key spelling
  assert.notEqual(knobSubgroup("provider.model_name"), "provider");
});

test("every declared child has a display label (no blank nav row)", () => {
  const declared = new Set(Object.values(KNOB_SUBGROUPS));
  for (const id of declared) {
    const label = subgroupLabel(id);
    assert.equal(typeof label, "string");
    assert.ok(label.length > 0, `child ${id} renders a non-empty label`);
    assert.equal(SUBGROUP_LABELS[id], label, `${id} is labelled by the table, not by the id`);
  }
});

test("withSubgroups stamps every knob and never mutates the caller", () => {
  const groups = [providerGroup(), logGroup()];
  const stamped = withSubgroups(groups);
  assert.equal(stamped[0].knobs[0].subgroup, "connection");
  assert.equal(stamped[0].knobs[2].subgroup, "models");
  assert.equal(stamped[1].knobs[1].subgroup, GENERAL_SUBGROUP);
  assert.equal(groups[0].knobs[0].subgroup, undefined, "the input stays untouched");
});

// --- the two-level tree ---------------------------------------------------

test("buildNav derives parents with their children underneath", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  assert.equal(nav.length, 1);
  assert.equal(nav[0].id, "provider");
  assert.deepEqual(nav[0].subgroups.map((s) => s.id), ["connection", "models"]);
  assert.deepEqual(nav[0].subgroups[0].knobs.map((k) => k.key), [
    "provider.type",
    "provider.host",
  ]);
  assert.equal(nav[0].knobs.length, 3, "the parent still owns every knob");
});

test("an undeclared domain collapses to ONE implicit general child", () => {
  const nav = buildNav(withSubgroups([logGroup()]));
  assert.equal(nav[0].subgroups.length, 1);
  assert.equal(nav[0].subgroups[0].id, GENERAL_SUBGROUP);
  assert.equal(nav[0].subgroups[0].knobs.length, 2);
});

test("general is pinned FIRST among a domain's children (ruling 3)", () => {
  const mixed = group("provider", "provider", [
    knob("provider.model_name", "model name"), // models
    knob("totally.undeclared", "unclassified"), // general
    knob("provider.host", "base url"), // connection
  ]);
  const nav = buildNav(withSubgroups([mixed]));
  assert.deepEqual(nav[0].subgroups.map((s) => s.id), [
    GENERAL_SUBGROUP,
    "models",
    "connection",
  ]);
});

test("empty categories never enter the nav (ruling 14)", () => {
  const nav = buildNav(withSubgroups([group("store", "store", [])]));
  assert.deepEqual(nav, []);
});

test("Commonly Used is a virtual FLAT top category", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup()]));
  assert.equal(nav[0].id, COMMON_CATEGORY);
  assert.equal(nav[0].flat, true);
  assert.deepEqual(nav[0].subgroups, [], "curated knobs never nest across domains");
  assert.equal(nav[1].id, "provider");
});

// --- the ONE document -----------------------------------------------------

test("the document carries EVERY category, in nav order, with no selection", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup(), logGroup()]));
  const doc = documentFor(nav);
  assert.deepEqual(
    doc.map((section) => section.id),
    nav.map((category) => category.id),
    "one section per category, same order as the nav",
  );
  assert.equal(doc[0].id, COMMON_CATEGORY, "the curated category leads");
  assert.equal(doc[1].id, "provider");
  assert.equal(doc[2].id, "unknown");
  // every knob is present, and the counts add up to the nav's own totals
  const rendered = doc.reduce((n, section) => n + section.count, 0);
  const total = nav.reduce((n, category) => n + category.knobs.length, 0);
  assert.equal(rendered, total, "no category is dropped from the one page");
});

test("a multi-child domain renders its children as headed blocks", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  const section = documentFor(nav)[0];
  assert.equal(section.id, "provider");
  assert.equal(section.count, 3);
  assert.deepEqual(section.blocks.map((block) => block.id), [
    "provider/connection",
    "provider/models",
  ]);
  assert.ok(section.blocks.every((block) => block.heading), "each block names itself");
  // the block ids are the jump targets the nav's child rows carry
  assert.equal(selectionId("provider", "models"), "provider/models");
});

test("a flat or one-child domain renders ONE unheaded block", () => {
  const nav = buildNav(withSubgroups([logGroup(), commonGroup()]));
  const doc = documentFor(nav);
  assert.equal(doc[0].blocks.length, 1, "one implicit child: no duplicate heading");
  assert.equal(doc[0].blocks[0].heading, false);
  assert.equal(doc[0].blocks[0].id, "unknown", "the block answers to the category id");
  const common = doc.find((section) => section.id === COMMON_CATEGORY);
  assert.ok(common !== undefined);
  assert.equal(common.blocks[0].heading, false, "the curated list stays flat");
});

test("a live query filters the document IN PLACE and drops emptied sections", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup(), logGroup()]));
  const hits = new Set(["provider.model_name", "custom.one"]);
  const doc = documentFor(nav, { hits });
  assert.deepEqual(doc.map((section) => section.id), ["provider", "unknown"]);
  assert.equal(
    doc.reduce((n, section) => n + section.count, 0),
    2,
    "each hit renders ONCE, under the domain that owns it",
  );
  // the curated category is a VIEW of other domains' knobs: a live query
  // skips its section so nothing is listed twice (the nav keeps the entry)
  assert.ok(!doc.some((section) => section.id === COMMON_CATEGORY));
});

test("a hit inside a category keeps only that category's surviving block", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  const doc = documentFor(nav, { hits: new Set(["provider.model_name"]) });
  assert.equal(doc.length, 1);
  assert.deepEqual(doc[0].blocks.map((block) => block.id), ["provider/models"]);
  assert.equal(doc[0].count, 1);
});

test("an empty hit set is the empty state, never 'everything'", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  assert.deepEqual(documentFor(nav, { hits: new Set() }), []);
  // null means NO query is live
  assert.equal(documentFor(nav, { hits: null }).length, 1);
  assert.equal(documentFor(nav, { hits: null })[0].count, 3);
});

test("modified-only filters the WHOLE document, not one category", () => {
  const nav = buildNav(withSubgroups([providerGroup(), logGroup()]));
  const isModified = (k: SettingKnob) =>
    k.key === "provider.model_name" || k.key === "custom.two";
  const doc = documentFor(nav, { modifiedOnly: true, isModified });
  assert.deepEqual(
    doc.map((section) => section.id),
    ["provider", "unknown"],
    "both categories survive - the switch used to narrow one category at a time",
  );
  assert.equal(doc.reduce((n, section) => n + section.count, 0), 2);
  // the switch is inert without the predicate (never drops silently)
  assert.deepEqual(documentFor(nav, { modifiedOnly: true }), []);
});

test("modified-only intersects with the query", () => {
  const nav = buildNav(withSubgroups([providerGroup(), logGroup()]));
  const hits = new Set(["provider.model_name", "custom.one"]);
  const isModified = (k: SettingKnob) => k.key === "custom.one";
  const doc = documentFor(nav, { hits, modifiedOnly: true, isModified });
  assert.deepEqual(doc.map((section) => section.id), ["unknown"]);
  assert.equal(doc[0].count, 1);
});

test("spyTargetIds lists each section then its blocks, in document order", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup()]));
  const doc = documentFor(nav);
  assert.deepEqual(spyTargetIds(doc), [
    COMMON_CATEGORY,
    "provider",
    "provider/connection",
    "provider/models",
  ]);
});

// --- the nav's grey-out set ----------------------------------------------

test("matchingCategoryIds answers exactly the categories with a survivor", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup(), logGroup()]));
  assert.deepEqual(
    [...matchingCategoryIds(nav, { hits: new Set(["custom.one"]) })],
    ["unknown"],
  );
  // no query, no modified-only: nothing is greyed
  assert.deepEqual(
    [...matchingCategoryIds(nav, {})].sort(),
    [COMMON_CATEGORY, "unknown", "provider"].sort(),
  );
  // the modified-only switch greys the untouched categories too
  assert.deepEqual(
    [...matchingCategoryIds(nav, { modifiedOnly: true, isModified: (k) => k.key === "provider.host" })],
    ["provider"],
  );
});

// --- the anchor (ruling 13 survives as a scroll anchor) -------------------

test("selection ids round-trip, one level or two", () => {
  assert.equal(selectionId("provider"), "provider");
  assert.equal(selectionId("provider", "models"), "provider/models");
  assert.deepEqual(parseSelection("provider"), { category: "provider", subgroup: null });
  assert.deepEqual(parseSelection("provider/models"), {
    category: "provider",
    subgroup: "models",
  });
});

test("resolveSelection tolerates a vanished category and a vanished child", () => {
  const nav = buildNav(withSubgroups([providerGroup(), commonGroup()]));
  assert.equal(resolveSelection(nav, "gone"), null);
  assert.equal(resolveSelection(nav, "provider/gone")?.subgroup, null);
  assert.equal(resolveSelection(nav, "provider/gone")?.category.id, "provider");
});

test("anchorSelection restores only what the live nav still carries", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup()]));
  // a remembered category and a remembered child both restore verbatim
  assert.equal(anchorSelection(nav, "provider"), "provider");
  assert.equal(anchorSelection(nav, "provider/models"), "provider/models");
  // NO anchor - the opening state, and what a DESELECT persists - never
  // invents a selection (there is no fallback category any more)
  assert.equal(anchorSelection(nav, null), null);
  assert.equal(anchorSelection(nav, ""), null);
  // a category that no longer exists opens at the top instead of breaking
  assert.equal(anchorSelection(nav, "unmounted-domain"), null);
  // a child the nav dropped normalises to its category - a stale id is
  // never a dangling anchor
  assert.equal(anchorSelection(nav, "provider/removed-child"), "provider");
  assert.equal(NAV_STORAGE_KEY, "omega.settings-category", "the key survives the model change");
});

// --- the pane's SOURCE pins ----------------------------------------------

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const tab = read("app/components/settings/SettingsTab.vue");
const navSource = read("app/components/settings/settingsNav.ts");

test("the pane renders ONE document of every category", () => {
  assert.match(tab, /v-for="section in docSections"/, "the document is the content pane");
  assert.match(
    tab,
    /documentFor\(nav\.value, \{[\s\S]{0,220}hits: searchHits\.value/,
    "the document derives from the pure builder",
  );
  assert.match(tab, /:data-category="section\.id"/, "journey recipes key on the section id");
  assert.match(tab, /:data-section="section\.id"/, "and the spy keys on the same attribute");
  assert.match(tab, /v-for="block in section\.blocks"/, "children render as blocks inside the section");
  assert.match(navSource, /export function documentFor\(/, "the builder lives in one place");
  // there is no per-selection view left to swap in
  assert.ok(!tab.includes("view.sections"), "no single-category view remains");
  assert.ok(!tab.includes("contentFor("), "the master-detail content view is gone");
});

test("a nav click SCROLLS the pane (smooth, reduced-motion aware) and marks it", () => {
  assert.match(
    tab,
    /function selectCategory\(id: string\) \{[\s\S]{0,260}selection\.value = id;[\s\S]{0,320}scrollToAnchor\(id\)/,
    "the click selects and jumps",
  );
  // card t_a8063683: the jump is a scrollTop computation, not a bare
  // scrollIntoView - the section heading is STICKY, so a block landing at
  // exactly 0 would paint behind the pinned heading (it subtracts the
  // heading's measured height for a block, nothing for a section).
  assert.match(
    tab,
    /const clear = isBlock \? pinnedHeadingHeight\(node\) : 0;[\s\S]{0,140}root\.scrollTo\(\{ top: root\.scrollTop \+ delta - clear, behavior \}\)/,
    "the jump scrolls the pane and clears the pinned heading for a block",
  );
  assert.match(
    tab,
    /prefers-reduced-motion: reduce/,
    "the smooth scroll honours prefers-reduced-motion",
  );
  assert.match(
    tab,
    /@click="selectCategory\(cat\.id\)"/,
    "every nav row is a jump link",
  );
});

test("a second click on the selected entry DESELECTS: clear, collapse, top", () => {
  assert.match(
    tab,
    /if \(selection\.value === id\) \{\s*deselect\(\);\s*return;/,
    "the same entry toggles the selection off",
  );
  assert.match(
    tab,
    /function deselect\(\) \{[\s\S]{0,200}selection\.value = null;[\s\S]{0,200}persistAnchor\(null\);[\s\S]{0,200}scrollToAnchor\(null\)/,
    "deselect clears the highlight, drops the anchor and returns to the top",
  );
  assert.match(
    tab,
    /aria-current="selection === cat\.id \? 'true' : undefined"/,
    "the highlight is the selection, never a filter",
  );
});

test("the scroll-spy watches the sections and never fights a click", () => {
  assert.match(tab, /new IntersectionObserver\(/, "an observer drives the highlight");
  assert.match(
    tab,
    /rootMargin: "0px 0px -80% 0px"/,
    "a thin top band, so neighbours cannot flicker mid-scroll",
  );
  // the observer is suppressed while a jump animates, and a deliberate
  // deselect stays suppressed until the NEXT real scroll
  assert.match(tab, /lockSpy\(650\);/, "a nav click holds the observer through its smooth scroll");
  assert.match(tab, /lockSpy\(\); \/\/ stays shut until the NEXT real scroll/, "a deselect holds it until input");
  assert.match(
    tab,
    /const INTENT_EVENTS = \["wheel", "touchstart", "pointerdown", "keydown"\]/,
    "real input is what releases the suppression",
  );
  assert.match(tab, /persistAnchor\(best\)/, "the spy's landing place becomes the next anchor");
});

test("expansion is DERIVED from the selection - no remembered toggle set", () => {
  // nothing selected -> nothing open: the opening state
  assert.deepEqual([...expandedCategoryIds(null)], []);
  // the selected branch is the ONLY one open - a category or a subsection
  assert.deepEqual([...expandedCategoryIds("provider")], ["provider"]);
  assert.deepEqual(
    [...expandedCategoryIds("provider/models")],
    ["provider"],
    "selecting a subsection opens its parent",
  );
  // a live query force-opens every parent carrying a hit, whatever is
  // selected - and only those
  assert.deepEqual(
    [...expandedCategoryIds(null, new Set(["provider", "memory"]))].sort(),
    ["memory", "provider"],
  );
  assert.deepEqual(
    [...expandedCategoryIds("provider", new Set(["memory"]))],
    ["memory"],
    "the query wins while it is live",
  );
  // the component asks the pure rule, never a set of its own
  assert.match(
    tab,
    /const expandedIds = computed<ReadonlySet<string>>\(\(\) =>\s*expandedCategoryIds\(/,
    "expansion is the derived function",
  );
  for (const dead of ["collapsedIds", "toggleOpen", "isOpen(", "omega.settings-collapsed"]) {
    assert.ok(!tab.includes(dead), `SettingsTab carries no ${dead} - expansion is not a preference`);
  }
  assert.ok(!navSource.includes("Collapsed"), "the nav module never resurrects collapse state");
});

test("the two-screen drill-down is GONE: narrow is a drawer over the same document", () => {
  for (const dead of ["drilled", "settings-back", 'v-if="!narrow || drilled"']) {
    assert.ok(!tab.includes(dead), `the narrow two-screen state is gone (${dead})`);
  }
  assert.match(tab, /<button[\s\S]{0,220}class="settings-jump"/, "narrow carries the drawer toggle");
  assert.match(tab, /:class="\{ drawer: narrow \}"/, "the nav becomes the drawer");
  assert.match(tab, /v-if="!narrow \|\| drawerOpen"/, "wide keeps the nav always on screen");
  assert.match(
    tab,
    /if \(narrow\.value\) drawerOpen\.value = false;/,
    "a jump closes the drawer",
  );
  // the document itself is never gated by the width - same renderer
  assert.match(tab, /<section class="settings-content">/, "the content has no narrow gate");
});

test("the last anchor is remembered per client and restored on open", () => {
  assert.match(tab, /localStorage\.setItem\(NAV_STORAGE_KEY, id\)/, "a selection is remembered");
  assert.match(
    tab,
    /else localStorage\.removeItem\(NAV_STORAGE_KEY\)/,
    "a DESELECT persists as no anchor",
  );
  assert.match(tab, /const id = anchorSelection\(nav\.value, stored\)/, "the open resolves the anchor");
  assert.match(
    tab,
    /if \(!id\) return;[\s\S]{0,160}selection\.value = id;/,
    "a pane with no anchor opens with nothing selected",
  );
});

test("the content head carries the scope strip and NOTHING else", () => {
  // card t_a8063683 (Endo, 2026-11-11): the heading mirror stripe is gone.
  // It repeated the heading the pane thought it was looking at and lagged
  // behind the scroll (and said "Commonly Used" twice). The document's own
  // section heading IS the heading, and it sticks instead.
  assert.match(
    tab,
    /<div v-if="scopeTabs\.length > 1" class="settings-content-head">/,
    "the head wrapper itself is gated - an empty flex child would still hold a seat",
  );
  assert.match(tab, /class="settings-scopes"/, "the tier-3 strip survives, in the content pane");
  for (const dead of [
    "settings-content-bar",
    "settings-content-title",
    "barTitle",
    "barCount",
  ]) {
    assert.ok(!tab.includes(dead), `the mirror stripe and its vocabulary are gone (${dead})`);
  }
});

test("the modified-only switch is a pane-wide control beside the search", () => {
  // card t_a8063683: it narrows the WHOLE document, so it belongs with the
  // other pane-wide control, not with a heading it would outlive.
  const bar = tab.match(/<div class="settings-filterbar">[\s\S]*?<\/div>/)?.[0] ?? "";
  assert.ok(bar.includes("<SearchInput"), "the filter bar carries the search");
  assert.match(
    bar,
    /class="settings-modified"[\s\S]{0,200}:aria-pressed="modifiedOnly"/,
    "the modified-only switch lives in the filter bar, still a pressed-state control",
  );
  assert.ok(
    bar.indexOf("<SearchInput") < bar.indexOf("settings-modified"),
    "the toggle sits beside the search, not in front of it",
  );
});

test("the section heading is the pane's STICKY header", () => {
  const rule = tab.match(/\.settings-section-title\s*\{[^}]*\}/)?.[0] ?? "";
  assert.match(rule, /position:\s*sticky/, "the only heading sticks");
  assert.match(rule, /top:\s*0/, "it pins at the scrollport's top edge");
  // a transparent header would let rows scroll visibly underneath it
  assert.match(rule, /background:\s*var\(--z1\)/, "it is OPAQUE, on the pane's own token");
  // a sticky box pins its BORDER box at top: 0 - the old 14px top margin
  // would leave a sliver above it through which rows show
  assert.match(rule, /margin:\s*0 0 2px/, "no top margin on the sticky box");
  assert.match(rule, /padding:\s*20px 0 6px/, "the old 14px lead rides padding-top");
  const first =
    tab.match(/\.settings-category:first-of-type \.\s*settings-section-title\s*\{[^}]*\}/)?.[0] ??
    "";
  assert.match(first, /border-top:\s*0/, "the first heading keeps its borderless top");
  assert.match(first, /padding-top:\s*4px/, "and its 4px lead, with no top margin");
});

test("the rows are EXACTLY tier 2b's (ruling 10: no restyle)", () => {
  for (const row of [
    "class=\"setting-dot\"",
    "class=\"setting-reset\"",
    "class=\"setting-dirty\"",
    "class=\"setting-row setting-toggle\"",
    "class=\"setting-item\"",
  ]) {
    assert.ok(tab.includes(row), `the row keeps ${row}`);
  }
});

test("the search stays ONE filter, wired to filterSettingGroups", () => {
  assert.match(tab, /filterSettingGroups\(allGroups\.value, query\.value\)/);
  assert.equal((tab.match(/<SearchInput/g) ?? []).length, 1, "one input, not one per screen");
  assert.match(tab, /hits: searchHits\.value/, "the hits feed the document and the grey-out set");
  assert.match(tab, /query\.value\.trim\(\) === ""\) return null/, "no query is null, not an empty hit set");
  // clearing the query gives the document back where it was
  assert.match(tab, /node\.scrollTop = lastDocScrollTop/, "the interrupted scroll position returns");
});
