// omega webui: unit tests for the settings ONE-DOCUMENT pane (card
// t_8b464090, which SUPERSEDES the master-detail nav of t_af7e1150): the
// per-knob subgroup DATA, the two-level tree it derives, the document the
// pane paints (EVERY category, in nav order, filtered in place), the nav's
// grey-out set, the last-anchor resolution - and the SOURCE pins that prove
// the selector/two-screen model is gone.
//
// card t_cd9952a3 extends that law without reversing it: a nav SELECTION now
// FILTERS the rendered document to that section and its subsections, a live
// search OVERRIDES the filter, and the selected section rides the in-app URL
// (`?settings=<id>`) so a deep link lands pre-filtered. Those rules and the
// URL are pure and pinned here; the click's live behaviour is journey 113.
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
  SETTINGS_SECTION_PARAM,
  SUBGROUP_LABELS,
  anchorSelection,
  buildNav,
  clampedRange,
  documentFor,
  expandedCategoryIds,
  knobSubgroup,
  matchingCategoryIds,
  paneScrollIsUser,
  paneScrollSettle,
  paneScrollSettled,
  parseSelection,
  parseSettingsSection,
  resolveSelection,
  sectionFilter,
  selectionId,
  spyAnchorMayWrite,
  spyTargetIds,
  subgroupLabel,
  withSettingsSection,
  withSubgroups,
  buildSpyTargets,
  targetSection,
  type PaneScrollState,
  type SpyAnchorTrace,
  type SpyNodeLike,
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

// --- the SECTION FILTER (card t_cd9952a3) ---------------------------------
//
// Part 1: a nav selection FILTERS the right pane to that section plus its
// subsections; a deselect - or Commonly Used, the curated all-view -
// restores the whole document. Part 2: a search query OVERRIDES the
// selection (one global filter across every section) and clearing it gives
// the filter back. Part 3: the in-app URL carries the selected section, so
// a deep link lands pre-filtered. The click's live behaviour is journey
// 113's job; the rules and the URL are pure and pinned here.

test("the filter is the selected CATEGORY - and Commonly Used is the all-view", () => {
  assert.equal(sectionFilter(null, false), null, "nothing selected = the all-view");
  assert.equal(sectionFilter("provider", false), "provider");
  assert.equal(
    sectionFilter("provider/models", false),
    "provider",
    "a subsection narrows to its CATEGORY, which renders that block",
  );
  assert.equal(
    sectionFilter(COMMON_CATEGORY, false),
    null,
    "Commonly Used is the curated ALL-VIEW entry, never a filter",
  );
  assert.equal(
    sectionFilter("provider/models", true),
    null,
    "a live query stands the selection down - search is global",
  );
});

test("a selected section shows its rows and hides every other section", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup(), logGroup()]));
  const all = documentFor(nav);
  assert.equal(all.length, 3, "the all-view really is every category");
  const doc = documentFor(nav, { only: sectionFilter("provider", false) });
  assert.deepEqual(
    doc.map((section) => section.id),
    ["provider"],
    "only the selected category survives",
  );
  assert.deepEqual(
    doc[0].blocks.map((block) => block.id),
    ["provider/connection", "provider/models"],
    "its subsections render with it",
  );
  assert.equal(
    doc[0].count,
    3,
    "every row of the section is shown - the filter hides SECTIONS, never rows",
  );
  // the surviving ids are the same anchors the nav and the spy use
  assert.deepEqual(spyTargetIds(doc), [
    "provider",
    "provider/connection",
    "provider/models",
  ]);
});

test("a subsection selection filters to its CATEGORY (a block is not a section)", () => {
  const nav = buildNav(withSubgroups([providerGroup(), logGroup()]));
  const doc = documentFor(nav, { only: sectionFilter("provider/models", false) });
  assert.deepEqual(doc.map((section) => section.id), ["provider"]);
  assert.equal(doc[0].count, 3, "the whole category, not just the clicked child");
});

test("a search query OVERRIDES the section filter, and clearing it gives the filter back", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup(), logGroup()]));
  const hits = new Set(["provider.model_name", "custom.one"]);
  // the pane's own composition: sectionFilter returns null while a query is up
  const searched = documentFor(nav, {
    hits,
    only: sectionFilter("provider", true),
  });
  assert.deepEqual(
    searched.map((section) => section.id),
    ["provider", "unknown"],
    "a live query matches EVERY section, not the selected one",
  );
  // a caller that forgets cannot filter a live query either: the builder
  // stands the filter down itself (one truth, two doors)
  assert.deepEqual(
    documentFor(nav, { hits, only: "provider" }).map((section) => section.id),
    ["provider", "unknown"],
    "the builder ignores the filter under a live query",
  );
  // the query clears: the filter is still active and comes back
  const cleared = documentFor(nav, {
    hits: null,
    only: sectionFilter("provider", false),
  });
  assert.deepEqual(cleared.map((section) => section.id), ["provider"]);
});

test("modified-only still intersects the filtered section", () => {
  const nav = buildNav(withSubgroups([providerGroup(), logGroup()]));
  const isModified = (k: SettingKnob) =>
    k.key === "provider.model_name" || k.key === "custom.two";
  const doc = documentFor(nav, {
    modifiedOnly: true,
    isModified,
    only: "provider",
  });
  assert.deepEqual(doc.map((section) => section.id), ["provider"]);
  assert.equal(doc[0].count, 1, "the section shrinks to its modified rows");
});

test("the in-app URL carries the selected section on the query axis", () => {
  assert.equal(SETTINGS_SECTION_PARAM, "settings");
  // the PATH keeps owning the focused chat session (card t_06c01beb), so a
  // section rides the query and the shipped scheme is untouched
  assert.equal(withSettingsSection("/", "provider"), "/?settings=provider");
  assert.equal(
    withSettingsSection("/chat/abc123", "provider/models"),
    "/chat/abc123?settings=provider/models",
    "the chat path survives, and a two-level id stays readable",
  );
  assert.equal(
    withSettingsSection("/chat/abc123?settings=provider/models", null),
    "/chat/abc123",
    "a deselect leaves NO stale section in the URL",
  );
  assert.equal(
    withSettingsSection("/?a=1&settings=dock&b=2", "store"),
    "/?a=1&b=2&settings=store",
    "other parameters survive the rewrite",
  );
  // reading it back is exact, whatever encoded the slash
  assert.equal(parseSettingsSection("/?settings=provider%2Fmodels"), "provider/models");
  assert.equal(parseSettingsSection("/chat/abc?settings=dock"), "dock");
  assert.equal(parseSettingsSection("/chat/abc"), null, "no parameter is no section");
  assert.equal(
    parseSettingsSection("/?settings="),
    null,
    "a blank value is a deselect that leaked into a bookmark, never a section named ''",
  );
  assert.equal(parseSettingsSection("/?other=1"), null);
});

test("a deep link lands pre-filtered: URL -> selection -> filter -> document", () => {
  const nav = buildNav(
    withSubgroups([
      commonGroup(),
      providerGroup(),
      logGroup(),
      group("dock", "dock", [knob("dock.bottom_width", "bottom panel width", "aligned")]),
    ]),
  );
  const href = "/chat/abc123?settings=provider/models";
  const id = parseSettingsSection(href);
  assert.equal(id, "provider/models", "the URL names the section");
  const resolved = resolveSelection(nav, id as string);
  assert.ok(resolved, "the id resolves against the LIVE nav");
  // the pane's opening selection is the nav's own id (a stale child
  // normalises to its category), clamped to the category and FILTERED
  const opening = selectionId(resolved!.category.id, resolved!.subgroup?.id ?? null);
  assert.equal(opening, "provider/models");
  const doc = documentFor(nav, { only: sectionFilter(opening, false) });
  assert.deepEqual(
    doc.map((section) => section.id),
    ["provider"],
    "the deep link opens the pane pre-filtered, not merely scrolled",
  );
  assert.equal(doc.length, 1, "the other categories are NOT rendered");
  // a stale deep link is not an error: it opens the all-view
  assert.equal(resolveSelection(nav, "retired"), null);
  assert.equal(documentFor(nav, { only: sectionFilter(null, false) }).length, nav.length);
});

test("the pane filters on its REAL selection, reads and writes the URL", () => {
  assert.match(navSource, /export function sectionFilter\(/, "the rule lives in the pure module");
  assert.match(
    tab,
    /sectionFilter\(clampId\.value, searching\.value\)/,
    "the pane asks the pure rule with its REAL selection",
  );
  assert.ok(
    !/sectionFilter\(selection\.value/.test(tab),
    "the spy's highlight never filters - a scroll must not collapse the document",
  );
  assert.match(tab, /only: filterSection\.value,/, "the document is built with the filter");
  assert.match(
    tab,
    /filterSection\.value === null \|\| filterSection\.value === MODULES_CATEGORY/,
    "the MODULES section follows the filter like any other category",
  );
  // the write: replaceState only, on the section a REAL selection set
  assert.match(
    tab,
    /watch\(clampId, \(section\) => writeUrlSection\(section\)\)/,
    "a real selection writes the URL",
  );
  assert.match(
    tab,
    /history\.replaceState\([\s\S]{0,80}withSettingsSection\(window\.location\.href, section\)/,
    "replaceState only - the app never grows the history stack",
  );
  // the read: an explicit door that outranks the remembered anchor
  assert.match(
    tab,
    /const fromUrl = urlSectionSelection\(\);[\s\S]{0,400}clampId\.value = parseSelection\(fromUrl\)\.category;/,
    "a URL section opens FILTERED and clamped - the user named it",
  );
  assert.match(
    tab,
    /const id = parseSettingsSection\(window\.location\.href\);/,
    "the live URL is the source of the deep link",
  );
});

test("a palette jump releases a blocking filter instead of dying on it", () => {
  assert.match(
    tab,
    /const blocking = filterSection\.value;[\s\S]{0,240}blocking !== null && blocking !== id[\s\S]{0,320}applyJump\(\)/,
    "the command's request wins: the filter is released and the jump re-lands",
  );
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

// --- spy target RESOLUTION in the document (card t_c4b42357) -------------
//
// A flat (or one-child) category renders ONE unheaded block, so its block id
// IS the section id: the document carries `data-section="common"` twice. The
// resolution must hand that id to the SECTION - the box that owns the sticky
// heading - or a jump lands the block at y=0, hidden behind that heading, and
// the clamp measures the block instead of the section. The tree here is hand
// built (the suite has no DOM); `.settings-category` ancestry is what the
// real closest() answers.

type FakeNode = SpyNodeLike & {
  readonly classes: readonly string[];
  readonly dataset: { readonly section?: string };
  readonly parent: FakeNode | null;
};

function fakeNode(
  classes: string[],
  section?: string,
  parent: FakeNode | null = null,
): FakeNode {
  const node: FakeNode = {
    classes,
    parent, // closest() walks this chain, exactly as a real element's ancestry
    dataset: section === undefined ? {} : { section },
    closest(selector: string) {
      if (selector !== ".settings-category")
        throw new Error(`unexpected selector ${selector}`);
      let cur: FakeNode | null = node;
      while (cur && !cur.classes.includes("settings-category")) cur = cur.parent;
      return cur;
    },
  };
  return node;
}

function fakeRoot(nodes: FakeNode[]) {
  return {
    querySelectorAll(selector: string): FakeNode[] {
      const match = /^\.([\w-]+)\[data-section\]$/.exec(selector);
      if (!match) throw new Error(`unexpected selector ${selector}`);
      const cls = match[1];
      return nodes.filter(
        (node) => node.classes.includes(cls) && node.dataset.section !== undefined,
      );
    },
  };
}

test("a flat category's repeated data-section id resolves to the SECTION, never its block", () => {
  // DOM order exactly as the pane paints it: the section, then its one block
  const common = fakeNode(["settings-category"], "common");
  const commonBlock = fakeNode(["settings-block"], "common", common);
  const provider = fakeNode(["settings-category"], "provider");
  const models = fakeNode(["settings-block"], "provider/models", provider);
  const store = fakeNode(["settings-category"], "store");
  const storeBlock = fakeNode(["settings-block"], "store", store);
  const pane = fakeRoot([common, commonBlock, provider, models, store, storeBlock]);

  const targets = buildSpyTargets<FakeNode>(pane);
  assert.equal(targets.get("common"), common, "the flat category's id belongs to its SECTION");
  assert.equal(targets.get("store"), store, "and so does a one-child category's id");
  assert.equal(targets.get("provider"), provider, "a multi-child category answers for itself");
  assert.equal(
    targets.get("provider/models"),
    models,
    "a genuinely distinct block still answers for itself",
  );
  assert.equal(targets.size, 4, "the duplicate id never adds a second entry");

  // the jump's clear: a section target IS its own section, so it subtracts no
  // heading (isBlock false) - the pane scrolls the section's top to 0 and its
  // first row flows BELOW the pinned heading instead of under it
  const sectionTarget = targets.get("common")!;
  assert.equal(targetSection(sectionTarget), sectionTarget, "a section target resolves to itself");
  assert.equal(
    sectionTarget !== targetSection(sectionTarget),
    false,
    "isBlock is false for a section target",
  );

  // a block target still clears its section's sticky heading
  const blockTarget = targets.get("provider/models")!;
  assert.equal(targetSection(blockTarget), provider, "a block target resolves to its section");
  assert.equal(
    blockTarget !== targetSection(blockTarget),
    true,
    "isBlock is true for a block target",
  );
});

// --- the pane's SOURCE pins ----------------------------------------------

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const tab = read("app/components/settings/SettingsTab.vue");
const navSource = read("app/components/settings/settingsNav.ts");

test("the jump/clamp targets are SECTIONS first (card t_c4b42357)", () => {
  assert.match(
    tab,
    /buildSpyTargets<HTMLElement>\(/,
    "one shared, section-first target map builder",
  );
  assert.ok(
    !tab.includes('querySelectorAll<HTMLElement>("[data-section]")'),
    "no plain DOM-order scan - the inner block shadowed its own section",
  );
  assert.match(
    tab,
    /\.settings-category\[data-section="\$\{category\}"\]/,
    "the clamp's fallback resolves a section, never a block",
  );
  assert.match(
    tab,
    /const section = targetSection\(node\);\s*const isBlock = node !== section;/,
    "the clear is measured from the SAME box the jump scrolls",
  );
});

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
    /function selectCategory\(id: string\) \{[\s\S]{0,260}selection\.value = id;[\s\S]{0,200}clampId\.value = picked\.category;/,
    "the click selects and clamps to its category",
  );
  // card t_cd9952a3: the click also FILTERS the document to that category,
  // so the jump is measured after that render - a pre-filter measurement
  // would target an offset the filter deletes.
  assert.match(
    tab,
    /void nextTick\(\(\) => scrollToAnchor\(id\)\)/,
    "the jump is measured once the section filter has rendered",
  );
  // card t_a8063683: the jump is a scrollTop computation, not a bare
  // scrollIntoView - the section heading is STICKY, so a block landing at
  // exactly 0 would paint behind the pinned heading (it subtracts the
  // heading's measured height for a block, nothing for a section).
  assert.match(
    tab,
    /const clear = isBlock \? pinnedHeadingHeight\(node\) : 0;[\s\S]{0,300}root\.scrollTo\(\{ top: root\.scrollTop \+ delta - clear, behavior \}\)/,
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
    /if \(\s*selection\.value === id \|\|\s*\(picked\.subgroup === null && clampId\.value === picked\.category\)\s*\) \{\s*deselect\(\);\s*return;/,
    "the same entry toggles the selection off - and so does the category holding the scroll",
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
    /if \(narrow\.value\) closeDrawer\(\);/,
    "a jump closes the drawer - through the one close path that returns focus (card t_a990f422)",
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
  // clearing the query gives the document back where it was - and card
  // t_cd9952a3: when a SECTION FILTER is waiting behind the query, the
  // document that returns is that one section, so the give-back is its top
  assert.match(
    tab,
    /const top = filterSection\.value === null \? lastDocScrollTop : 0;/,
    "the give-back keeps the filter's section top when one is active",
  );
  assert.match(tab, /node\.scrollTop = top/, "the interrupted scroll position returns");
});

// --- the selected-section scroll clamp (card t_a6b794b5) ------------------
//
// Endo, 2026-11-11: selecting a category must bound the pane's scroll to
// that category; a deselect restores free scroll through the whole
// document. The maths is here because it is the part that is easy to get
// wrong: a range that inverts, or one that answers a position the document
// cannot reach, is a pane that fights the hand or locks the user out.

test("a full-height section pins the pane to its own top", () => {
  // exactly as tall as the pane: there is no travel at all
  assert.deepEqual(
    clampedRange({ top: 300, height: 800, viewport: 800, content: 5000 }),
    [300, 300],
    "one legal position - the section's top at the pane's top edge",
  );
});

test("a taller section lets the pane travel its own height, no further", () => {
  const [min, max] = clampedRange({ top: 300, height: 2000, viewport: 800, content: 5000 });
  assert.equal(min, 300, "the section's top at the pane's top edge");
  assert.equal(max, 1500, "the section's bottom at the pane's bottom edge");
  assert.equal(300 + 2000 - 800, max, "max is exactly the bottom-aligned position");
  // and the document continues below the section: the clamp, not the
  // document's own end, is what stops the pane here
  assert.ok(max < 5000 - 800, "a mid-document section stops short of the document's end");
});

test("a section shorter than the viewport does not scroll at all", () => {
  // step 4: an empty range collapses to the section's top - an inverted
  // range [top, top - 600] would either invert or scroll into nowhere
  for (const height of [0, 40, 120, 799]) {
    const [min, max] = clampedRange({ top: 500, height, viewport: 800, content: 5000 });
    assert.equal(min, 500, `height ${height}: the range starts at the section's top`);
    assert.equal(max, 500, `height ${height}: and ends there - the pane rests`);
  }
});

test("the range can never invert, whatever the geometry", () => {
  for (let top = 0; top <= 2000; top += 137)
    for (const height of [0, 1, 300, 800, 1600])
      for (const viewport of [1, 400, 800, 1200]) {
        const [min, max] = clampedRange({ top, height, viewport, content: 5000 });
        assert.ok(
          min <= max,
          `top ${top} height ${height} viewport ${viewport} answered [${min}, ${max}]`,
        );
      }
});

test("a section at the very end answers a REACHABLE window", () => {
  // its natural max (top + height - viewport) overshoots the document's
  // last legal scroll position; answering that would leave the clamp
  // asking for a scrollTop the browser cannot hold
  assert.deepEqual(
    clampedRange({ top: 900, height: 100, viewport: 800, content: 1000 }),
    [200, 200],
    "the document's own end becomes the range",
  );
  assert.deepEqual(
    clampedRange({ top: 300, height: 700, viewport: 800, content: 1000 }),
    [200, 200],
    "a tall last section pins to the bottom of the document it ends",
  );
  // a document shorter than the pane has exactly one legal position
  assert.deepEqual(
    clampedRange({ top: 40, height: 100, viewport: 800, content: 500 }),
    [0, 0],
    "nothing can scroll, so the range is the document's top",
  );
});

test("only a REAL selection clamps, measured fresh, on the scroll event", () => {
  // the arithmetic lives in settingsNav (asserted above); a second copy in
  // the component is the drift this pins out
  assert.match(navSource, /export function clampedRange\(/);
  assert.match(tab, /const \[min, max\] = clampedRange\(\{/, "the pane asks the pure function");
  // a nav click clamps to the section it selected - and the spy, which
  // moves `selection` while the user scrolls, never clamps (the card's trap:
  // otherwise free scroll seizes up the moment the user scrolls)
  assert.match(
    tab,
    /clampId\.value = picked\.category;/,
    "a nav click clamps to its category section",
  );
  assert.equal(
    (tab.match(/clampId\.value = (?!==)/g) ?? []).length,
    5,
    "exactly five writers: the click, the deselect, the scope that lost the section, the deep link's restore (t_cd9952a3), and the palette jump's filter release (t_cd9952a3)",
  );
  assert.ok(
    !/selection\.value = best;[\s\S]{0,240}clampId/.test(tab),
    "the spy moves the highlight and never the clamp",
  );
  // step 2: compare before assigning (a write fires another scroll event)
  assert.match(
    tab,
    /if \(Math\.abs\(next - now\) < 1\) return;/,
    "the clamp writes only when it actually moves the pane",
  );
  // the trap: the clamp lands on the scroll EVENT (a frame callback alone
  // is not enough - a hidden/occluded tab throttles it to nothing, card
  // t_e7404b84) and again on the frame after, for wheel momentum
  assert.match(tab, /if \(clampId\.value\) scheduleClamp\(\);/, "the scroll handler schedules");
  assert.match(
    tab,
    /function scheduleClamp\(\) \{\s*applyClamp\(\);/,
    "the clamp lands on the event, not only on the next frame",
  );
  assert.match(tab, /requestAnimationFrame\(\(\) => \{/, "and again on the next frame");
  // step 5: ONE suppression - a scroll the PANE issued itself, never the
  // spy's intent-event lock: a scrollbar drag, an AT scroll and a
  // programmatic scrollTop dispatch no intent event at all, and gating on
  // them left those scrolls unclamped (card t_e7404b84)
  assert.match(
    tab,
    /if \(!paneScrollSettled\(paneScroll\)\) return;/,
    "the pane's own scroll is the clamp's only suppression",
  );
  assert.ok(
    !/if \(spyLocked\) return;[\s\S]{0,400}function scheduleClamp/.test(tab),
    "the clamp no longer rides the spy's intent-event lock",
  );
  // step 3: a section's box changes without any scroll - re-measure then
  assert.match(tab, /new ResizeObserver\(\(\) => applyClamp\(\)\)/, "the boxes are observed");
  assert.match(tab, /function observeClamp\(\)/);
  assert.match(
    tab,
    /syncSpy\(\);\s*observeClamp\(\);\s*applyClamp\(\);/,
    "a rebuilt document re-arms and re-measures the clamp",
  );
});


// --- card t_e7404b84: a scroll with NO intent event is still the USER's ---
//
// The probe that opened this card (private lane 18201, 13 sections, pane
// clientHeight 423, document end 6247, provider range [1813, 2722]):
//
//   nav-click provider                                  -> scrollTop 1813
//   `.settings-body`.scrollTop = 1e6, NO intent event,
//     dispatched 80ms after that click                  -> 6247 = the
//       document's end, UNCLAMPED (the section's bottom is 2722), and it
//       STAYED there
//   the same assignment 1.2s after the click            -> 2722, clamped
//   a real wheel (the CDP input pipeline)               -> 2722, clamped
//   a real scrollbar drag                               -> pointerdown does
//       fire on the pane here, so the intent path released it
//   requestAnimationFrame blacked out + the recipe's own
//     force_scroll (synthetic wheel + scrollTop)         -> 6247 again
//
// So two things were true, and both are fixed: (1) the clamp only ever ran
// inside a requestAnimationFrame callback, which a hidden or occluded tab
// throttles to nothing, and (2) the suppression that gated it could only be
// released by wheel/touchstart/pointerdown/keydown - none of which a
// programmatic scrollTop, a scrollbar drag in other engines or an
// assistive-technology scroll dispatches. The suppression is now the pane's
// OWN scroll, tracked to its settle, and nothing else.
test("a scroll the pane did not issue is clamped and ends the suppression (t_e7404b84)", () => {
  const GRACE = 200;
  // the pane issued a jump: until its events stop arriving, they are its own
  const travelling: PaneScrollState = { target: 1813, graceUntil: 0 };
  assert.equal(paneScrollSettled(travelling), false, "an animation in flight is not settled");
  assert.equal(paneScrollIsUser(travelling, 1000), false, "nor is it the user's scroll");
  // the events stop -> settle -> its last repeats stay the pane's (a
  // deselect must not re-select the section its return-to-top just passed)
  const settled = paneScrollSettle(1000, GRACE);
  assert.deepEqual(settled, { target: null, graceUntil: 1200 }, "the pane's own scroll ended");
  assert.equal(paneScrollSettled(settled), true, "the clamp may run again");
  assert.equal(paneScrollIsUser(settled, 1100), false, "a trailing repeat is still the pane's");
  // and past the settle window EVERY scroll is the user's: the probe's
  // programmatic 1e6, a scrollbar drag, an AT scroll - all clamped, and all
  // of them release the spy
  assert.equal(
    paneScrollIsUser(settled, 1200),
    true,
    "a scroll with no pane animation behind it is the user's - clamped, spy released",
  );
  assert.equal(paneScrollIsUser({ target: null, graceUntil: 0 }, 0), true, "a fresh pane has no suppression at all");
});

test("the clamp rides the scroll event and never the intent-event lock", () => {
  // a frame callback alone is not enough: a hidden/occluded tab throttles
  // requestAnimationFrame to nothing and the pane kept every scroll
  assert.match(
    tab,
    /function scheduleClamp\(\) \{\s*applyClamp\(\);/,
    "the clamp lands on the event, not only on the next frame",
  );
  assert.match(tab, /requestAnimationFrame\(\(\) => \{/, "and again on the next frame, for momentum");
  assert.match(
    tab,
    /if \(!paneScrollSettled\(paneScroll\)\) return;/,
    "the clamp's ONE suppression is the pane's own scroll",
  );
  assert.match(
    tab,
    /if \(bodyEl\.value && !paneOwnScroll\(\)\) \{\s*unlockSpy\(\);[\s\S]{0,220}anchorTrace\.userScrollAt = nowMs\(\);/,
    "any other scroll event releases the suppression - and it is the only one that stamps the user's scroll",
  );
  assert.ok(
    !/if \(spyLocked\) return;[\s\S]{0,400}function scheduleClamp/.test(tab),
    "the clamp no longer rides the spy's intent-event lock",
  );
  // the pane tracks the target of every scroll it issues itself
  assert.match(tab, /function notePaneScroll\(root: HTMLElement, top: number\)/, "the track exists");
  assert.match(tab, /notePaneScroll\(root, root\.scrollTop \+ delta - clear\)/, "a jump tracks its landing");
  assert.match(tab, /notePaneScroll\(root, 0\);/, "a deselect's return to top tracks its landing");
  assert.match(
    tab,
    /notePaneScroll\(node, top\);/,
    "a cleared query's give-back tracks its landing",
  );
  // and an interrupted animation cannot strand the pane outside its section
  assert.match(
    tab,
    /paneScroll = paneScrollSettle\(nowMs\(\), PANE_SETTLE_GRACE_MS\);[\s\S]{0,220}if \(clampId\.value\) scheduleClamp\(\);/,
    "when the pane's own scroll settles, the clamp re-measures",
  );
});

// --- card t_63c699ce: a resize is not a scroll ---------------------------
//
// The probe that opened this card (private lane 18201, settings pane, 13
// categories; narrow 340x780 -> widen 1280x800) sampled the stored anchor
// every 100ms across the widen, with the pane's own smooth jump still in
// flight and the spy's 650ms post-click hold already expired:
//
//   108ms  anchor compaction          scrollTop 2807  (the jump's animation)
//   404ms  anchor compaction          scrollTop 4039  <-- widen lands here
//   503ms  anchor server/workspace    scrollTop 3340  <-- the flip
//   609ms  anchor compaction          scrollTop 3904  (the clamp's re-snap)
//   704ms  anchor compaction/budget   scrollTop 4217
//   801ms  anchor compaction/window   scrollTop 4335
//   1001ms anchor compaction/window   scrollTop 4399  (settled)
//
// Every write after 404ms was a delivery the RESIZE armed: the observer
// re-resolved the band at the new boxes and persisted whatever id it found,
// then the clamp's own snap re-armed it again. Nothing scrolled. The rule the
// pane now follows is ORDER - the anchor is written only by a scroll the
// pane did not issue itself, once per scroll - and it is pure, so it is
// pinned here rather than in a browser.
test("a resize re-spies the highlight but never rewrites the anchor (t_63c699ce)", () => {
  const fresh: SpyAnchorTrace = { userScrollAt: 0, geometryAt: 0, wroteAt: 0 };
  assert.equal(spyAnchorMayWrite(fresh), false, "a pane nobody scrolled writes nothing");
  // the drawer jump is the PANE's own scroll (notePaneScroll), so it stamps
  // no user scroll - and the widen after it is geometry
  const jumpedThenWidened: SpyAnchorTrace = { userScrollAt: 0, geometryAt: 1000, wroteAt: 0 };
  assert.equal(
    spyAnchorMayWrite(jumpedThenWidened),
    false,
    "the delivery a widen arms leaves the explicit jump's anchor alone",
  );
  // the user's own hand, after that geometry change, is the one that writes
  const scrolled: SpyAnchorTrace = { ...jumpedThenWidened, userScrollAt: 1200 };
  assert.equal(spyAnchorMayWrite(scrolled), true, "a user scroll writes the anchor");
  assert.equal(
    spyAnchorMayWrite({ ...scrolled, wroteAt: 1200 }),
    false,
    "one write per scroll - the clamp's trailing delivery writes nothing",
  );
  // a scroll OLDER than the geometry change is not evidence: the resize
  // resolved new boxes, whatever the user did before it
  assert.equal(
    spyAnchorMayWrite({ userScrollAt: 900, geometryAt: 1000, wroteAt: 0 }),
    false,
    "a resize closes the write the user's previous scroll opened",
  );
});

test("the spy's anchor write is gated on the user's scroll, never on geometry", () => {
  assert.match(
    navSource,
    /export function spyAnchorMayWrite\(trace: SpyAnchorTrace\): boolean \{\s*return trace\.userScrollAt > trace\.geometryAt && trace\.userScrollAt > trace\.wroteAt;/,
    "the rule is ORDER: user scroll newer than the last geometry change, once per scroll",
  );
  // the observer decides the highlight as before, but the WRITE asks the rule
  assert.match(
    tab,
    /function applySpySelection\(best: string \| null\) \{[\s\S]{0,400}selection\.value = best;[\s\S]{0,300}if \(!spyAnchorMayWrite\(anchorTrace\)\) return;\s*anchorTrace\.wroteAt = anchorTrace\.userScrollAt;\s*persistAnchor\(best\);/,
    "the spy persists only what the user's scroll earned",
  );
  // geometry is stamped by the two things that move the boxes: a re-arm...
  assert.match(
    tab,
    /\/\/ a re-arm is a GEOMETRY change[^\n]*\n\s*anchorTrace\.geometryAt = nowMs\(\);/,
    "re-arming the spy is a geometry change",
  );
  // ...and the scrollport's own box observer
  assert.match(
    tab,
    /new ResizeObserver\(\(\) => \{\s*anchorTrace\.geometryAt = nowMs\(\);\s*\}\)/,
    "the scrollport's box is observed as geometry",
  );
  assert.match(tab, /paneBoxObserver\?\.disconnect\(\);/, "the box observer dies with the pane");
  // a re-clamp is the PANE's own scroll, so its event cannot arm a write
  assert.match(
    tab,
    /notePaneScroll\(root, next\);\s*root\.scrollTop = next;/,
    "the clamp's snap is noted as the pane's own scroll",
  );
  assert.ok(
    !/localStorage\.setItem\(NAV_STORAGE_KEY, best\)/.test(tab),
    "no second write path around the gate",
  );
});

// --- card t_720c9cff: a scroll inside a lock is clamped when the lock ends --
//
// The nightly reded on the SAME clamp assertion the merge before had fixed
// (113-settings-nav: scrollTop 5709, want 2509, document end 5709 - byte for
// byte the pre-fix numbers). The card guessed the 650ms spy lock refused the
// clamp; the probe on the dev lane says otherwise, and it reproduces:
//
//   nav-click provider, then wheel + `scrollTop = 1e6` in the NEXT task
//     (inside the same frame - a fast host's jsq round trip)
//                                                      -> 6247, STAYED there
//   the same wheel + assignment 2s after the click      -> 2722 (clamped)
//   the same, 3s after (no lock, nothing suppressed)    -> 2722 (clamped)
//   the same frame, WITHOUT the wheel                   -> 2722 (clamped)
//
// The un-wheeled row is the card's own hypothesis refuted: it sits inside the
// very same 650ms lock and still clamps, because the clamp never read that
// lock (t_e7404b84 removed it). The swallow is an EVENT-LESS scroll: the pane
// was already resting at the document's end (a previous free scroll left it
// there), so `scrollTop = 1e6` is a same-value instant request and fires no
// scroll event at all; the wheel that preceded it ended the pane's own scroll
// tracking AND cleared the 400ms settle timer that would have re-measured.
// Nothing is left pending, so the pane sits outside its section for good.
test("a scroll inside the lock window is clamped when the lock ends (t_720c9cff)", () => {
  // the swallowed state is DECIDABLE: nothing is in flight, the pane is just
  // never re-measured - which is why the clamp is allowed to run the moment a
  // lock lifts (the t_e7404b84 gate answers "yes, settled")
  const swallowed: PaneScrollState = { target: null, graceUntil: 0 };
  assert.equal(
    paneScrollSettled(swallowed),
    true,
    "the pane's own scroll is over - the clamp would bite, it is only never asked",
  );
  // and the range it was kept outside is the same maths the nightly's numbers
  // fall out of: the CI red names only the document end (5709) and the wanted
  // end (2509), so the probe document's shape is what is pinned here
  const lane = { top: 1813, height: 1332, viewport: 423, content: 6670 };
  assert.deepEqual(
    clampedRange(lane),
    [1813, 2722],
    "the selected section's window, measured on the probe's document",
  );
  const nightly = { top: 1589, height: 1343, viewport: 423, content: 6132 };
  assert.equal(nightly.content - nightly.viewport, 5709, "the nightly's document end");
  const [, hi] = clampedRange(nightly);
  assert.equal(hi, 2509, "and its 'want 2509' is this same clampedRange, no special case");

  // ONE pass, on the locks that already exist: the timed lock's expiry...
  // card t_81d0901b: the expiry ALSO re-resolves the spy highlight from the
  // resting boxes (a long jump's last intersection batch can land inside the
  // lock window) - the clamp re-measure stays the first call.
  assert.match(
    tab,
    /spyTimer = setTimeout\(\(\) => \{\s*spyLocked = false;\s*spyTimer = undefined;\s*clampOnLockEnd\(\);[\s\S]{0,400}?settleSpyIfIdle\(\);\s*\}, ms\);/,
    "the timed lock re-measures when it expires, not never",
  );
  // ...and a deselect's open-ended suppression, which lifts on real input
  assert.match(
    tab,
    /function unlockSpy\(\) \{[\s\S]{0,220}spyLocked = false;\s*clampOnLockEnd\(\);/,
    "the suppression's release re-measures too - same shape, same pass",
  );
  // the pass reuses the clamp machinery instead of inventing a second one
  assert.match(
    tab,
    /function clampOnLockEnd\(\) \{\s*if \(clampId\.value\) scheduleClamp\(\);\s*\}/,
    "one pass through the existing scheduleClamp - no new primitive",
  );
  assert.ok(
    !/function clampOnLockEnd\(\)[\s\S]{0,200}root\.scrollTop =/.test(tab),
    "and it never writes scrollTop itself: applyClamp stays the only writer",
  );
  // the timed lock's LENGTH is untouched: it exists so the observer does not
  // fight the smooth scroll it started
  assert.match(tab, /lockSpy\(650\);/, "the 650ms jump window is unchanged");
  // and the pane-scroll suppression's own gate is untouched (PANE_SCROLL_GAP_MS)
  assert.match(tab, /const PANE_SCROLL_GAP_MS = 400;/, "the stream-suppression window is unchanged");
});

// --- the A11Y keyboard law (card t_a990f422) -----------------------------
//
// vera's trio, scoped to settings:
//   1. the model rows carried role="option" and nothing else - unreachable
//      by keyboard, un-pickable by Enter;
//   2. the narrow nav drawer had no Escape, no trap, no focus return, and a
//      scrim that was not decoration;
//   3. the pane's ONE heading STICKS, so any scroll omega did not issue
//      landed its target behind it (WCAG 2.2 SC 2.4.11).
// The node suite has no DOM: the handlers are pinned at the source, and the
// live keyboard round trip is the journey's job (recipe 113) plus the lane
// probe recorded on the card.

const picker = read("app/components/chat/ModelSelect.vue");
const fieldSource = read("app/components/settings/ModelField.vue");

test("settings model rows are a keyboard listbox: roving tabindex, arrows, Enter, Escape", () => {
  // the settings picker IS this component (card t_dac84a33 shares it with
  // the composer) - the keyboard law therefore reaches the settings rows
  assert.match(
    fieldSource,
    /import ModelSelect from "\.\.\/chat\/ModelSelect\.vue";/,
    "the settings model field renders the shared picker, not a second one",
  );

  // one row in the tab order at a time: Tab enters the list ON the cursor
  // instead of walking a 200-model catalog
  assert.match(
    picker,
    /:tabindex="isActiveRow\(g\.name, m\) \? 0 : -1"/,
    "a roving tabindex - exactly one row is tabbable",
  );
  // the rows are a listbox's options, and the popup they sit in is the
  // combobox the search input drives
  assert.match(picker, /role="listbox"/, "the section list is a real listbox");
  assert.match(picker, /@keydown="onRowKey\(\$event, g\.name, m\)"/, "the rows own their keydown");
  assert.match(
    picker,
    /function onRowKey\(e: KeyboardEvent, section: string, model: string\)/,
    "one handler for the whole grammar",
  );
  for (const key of ['"ArrowDown"', '"ArrowUp"', '"Enter"', '"Escape"']) {
    assert.ok(
      picker.includes(`case ${key}:`) || picker.includes(`e.key === ${key}`),
      `the rows handle ${key}`,
    );
  }
  // the wrap is the app's ONE keyboard arithmetic, not a second one
  assert.match(
    picker,
    /import \{ clampIndex \} from "\.\.\/\.\.\/composables\/commandRegistry";/,
    "the rows ride the command palette's clampIndex",
  );
  assert.match(
    picker,
    /const next = clampIndex\(activeIndex\.value, delta, flatRows\.value\.length\);/,
    "arrows wrap",
  );

  // focus follows the cursor AND the cursor follows focus: the row that
  // takes focus is the row the highlight paints
  assert.match(picker, /@focus="seatOn\(g\.name, m\)"/, "focusing a row seats the cursor on it");
  assert.match(picker, /active: isActiveRow\(g\.name, m\)/, "the active row is highlighted");
  assert.match(picker, /\.model-list li\.active \{/, "and the highlight has a rule");

  // a keyboard pick rides the SAME guard as a click: a disabled row never
  // emits, its reason is information
  assert.match(
    picker,
    /function pickRow\(section: string, model: string\) \{\s*if \(reasonFor\(section, model\) !== null\) return;/,
    "Enter on a disabled row is a no-op, not an override",
  );

  // the popup opens on the value's own row, and typing "filter models"
  // still filters: the seat falls back to row one when the value is gone
  assert.match(
    picker,
    /function seatOnCurrent\(\)[\s\S]{0,260}activeKey\.value = seat \? rowKey\(seat\.section, seat\.model\) : null;/,
    "the popup seats on the current value",
  );
  assert.match(
    picker,
    /const seat = rows\.findIndex\(\s*\(r\) => rowKey\(r\.section, r\.model\) === activeKey\.value,\s*\);\s*return seat >= 0 \? seat : 0;/,
    "a filtered-away seat falls back to the first row",
  );

  // Escape closes and hands focus BACK to the chip: a keyboard user is
  // never dropped on <body>
  assert.match(picker, /const chipEl = ref<HTMLButtonElement \| null>\(null\);/, "the chip is the focus return target");
  assert.match(picker, /ref="chipEl"/, "and the template binds it");
  assert.match(
    picker,
    /const hadFocus = !!root\.value\?\.contains\(document\.activeElement\);[\s\S]{0,300}if \(restoreFocus && hadFocus\) void nextTick\(\(\) => chipEl\.value\?\.focus\(\)\);/,
    "closing from inside the popup returns focus to the chip",
  );
  // leaving by Tab must not be stolen back
  assert.match(picker, /case "Tab":[\s\S]{0,140}close\(false\);/, "a Tab close never steals the Tab");
});

test("the settings drawer honours Escape, holds focus, and returns it to the toggle", () => {
  // the toggle moves focus into the drawer and is the return target
  assert.match(tab, /ref="jumpEl"/, "the toggle is addressable");
  assert.match(
    tab,
    /@click="drawerOpen \? closeDrawer\(\) : openDrawer\(\)"/,
    "one open path and one close path, never a bare boolean flip",
  );
  assert.match(
    tab,
    /function openDrawer\(\) \{\s*drawerOpen\.value = true;[\s\S]{0,240}nextTick\(\(\) => drawerFocusables\(\)\[0\]\?\.focus\(\)\);/,
    "opening lands focus INSIDE the drawer - a trap needs something to hold",
  );
  assert.match(
    tab,
    /function closeDrawer\(\) \{\s*if \(!drawerOpen\.value\) return;[\s\S]{0,320}nextTick\(\(\) => jumpEl\.value\?\.focus\(\)\);/,
    "closing hands focus back to the toggle that opened it",
  );
  // EVERY close lands in closeDrawer: a jump link, a deselect, the scrim.
  // Exactly one assignment means no path can bypass the focus return.
  const clears = tab.match(/drawerOpen\.value = false;/g) ?? [];
  assert.equal(clears.length, 1, "closeDrawer is the ONLY place the drawer closes");
  assert.equal(
    (tab.match(/if \(narrow\.value\) closeDrawer\(\);/g) ?? []).length,
    2,
    "a jump and a deselect both close through the one path",
  );

  // Escape closes; Tab and Shift+Tab wrap at the drawer's edges
  assert.match(tab, /@keydown="onDrawerKey"/, "the pane root owns the drawer's keys");
  assert.match(
    tab,
    /if \(!narrow\.value \|\| !drawerOpen\.value\) return;\s*if \(e\.key === "Escape"\) \{\s*e\.preventDefault\(\);\s*e\.stopPropagation\(\);\s*closeDrawer\(\);/,
    "Escape closes the drawer",
  );
  assert.match(
    tab,
    /if \(e\.shiftKey && active === first\) \{\s*e\.preventDefault\(\);\s*last\.focus\(\);\s*\} else if \(!e\.shiftKey && active === last\) \{\s*e\.preventDefault\(\);\s*first\.focus\(\);/,
    "Tab wraps forward, Shift+Tab wraps back",
  );
  assert.match(tab, /const FOCUSABLE =\s*'button:not\(\[disabled\]\), \[href\]/, "the trap walks the drawer's own focusables");

  // the scrim is decoration: click-to-close stays, semantics leave
  assert.match(
    tab,
    /class="settings-scrim"\s*role="presentation"\s*@click="closeDrawer\(\)"/,
    "the scrim is presentational and still click-to-close",
  );
});

test("the settings body reserves the pinned heading for native scrolls (SC 2.4.11)", () => {
  assert.match(
    tab,
    /\.settings-body \{\s*scroll-padding-top: 44px;\s*\}/,
    "the scrollport pads its own top edge",
  );
  // the number is not a mood: the pinned heading measures 42px on the lane
  // (20px padding-top + 6px padding-bottom + an 11px heading's line box +
  // the 1px rule), and the pad must leave a landed target CLEAR of it, not
  // flush under it
  const heading = tab.match(
    /\.settings-section-title \{\s*position: sticky;[\s\S]{0,400}?padding: (\d+)px 0 (\d+)px;/,
  );
  assert.ok(heading, "the sticky heading's own padding is readable");
  const declared = Number(heading![1]) + Number(heading![2]) + 1; // + the top rule
  const pad = tab.match(/scroll-padding-top: (\d+)px;/);
  assert.ok(pad, "a pixel value, not a keyword");
  const value = Number(pad![1]);
  assert.ok(
    value > declared,
    `scroll-padding-top ${value}px must exceed the heading's ${declared}px of padding + rule`,
  );
  // the heading still pins - the pad is an addition to that law, not a
  // replacement for it
  assert.match(tab, /\.settings-section-title \{\s*position: sticky;/, "the heading still sticks");
  // and the pane's OWN jump keeps measuring the live heading: the constant
  // is for the scrolls omega does not issue
  assert.match(tab, /const clear = isBlock \? pinnedHeadingHeight\(node\) : 0;/, "the measured jump is untouched");
});

