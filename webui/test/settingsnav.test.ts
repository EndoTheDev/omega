// omega webui: unit tests for the settings MASTER-DETAIL nav (card
// t_af7e1150, settings tier 4): the per-knob subgroup DATA, the two-level
// tree it derives, the selection algebra, the content view, the nav's
// grey-out set - and the SOURCE pins that prove the accordion is gone.
// The node suite has no DOM: the tree/selection/view are pinned as pure
// functions, the pane as structural assertions on SettingsTab.vue (the
// live shape is the journey's job, recipes 110/113).
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
  SEARCH_TITLE,
  SUBGROUP_LABELS,
  buildNav,
  contentFor,
  effectiveSelection,
  fallbackSelection,
  knobSubgroup,
  matchingCategoryIds,
  parseSelection,
  resolveSelection,
  selectionId,
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

// --- the selection algebra ------------------------------------------------

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
  assert.equal(resolveSelection(nav, "gone")?.category.id, undefined);
  assert.equal(resolveSelection(nav, "gone"), null);
  assert.equal(resolveSelection(nav, "provider/gone")?.subgroup, null);
  assert.equal(resolveSelection(nav, "provider/gone")?.category.id, "provider");
});

test("a fresh pane opens on Commonly Used; an agent nav falls back to its first category", () => {
  const main = buildNav(withSubgroups([commonGroup(), providerGroup()]));
  assert.equal(fallbackSelection(main), COMMON_CATEGORY);
  // the agent tab carries the four overridable groups and no curated one
  const agent = buildNav(withSubgroups([group("provider", "provider", [knob("provider.model_name", "model name")])]));
  assert.equal(fallbackSelection(agent), "provider");
  assert.equal(fallbackSelection([]), "");
});

test("the remembered id survives a scope that cannot show it (ruling 13)", () => {
  const main = buildNav(withSubgroups([commonGroup(), providerGroup()]));
  const agent = buildNav(withSubgroups([group("provider", "provider", [knob("provider.model_name", "model name")])]));
  // remembered on Main: resolvable there and on the agent tab alike
  assert.equal(effectiveSelection(main, "provider/models"), "provider/models");
  assert.equal(effectiveSelection(agent, "provider/models"), "provider/models");
  // a child the agent's four groups do not carry normalises to its parent
  assert.equal(effectiveSelection(agent, "provider/connection"), "provider");
  // and the raw id still resolves when Main returns - the memory is raw
  assert.equal(effectiveSelection(main, "provider/connection"), "provider/connection");
});

// --- the content view -----------------------------------------------------

test("one category renders at a time, children as headed sections", () => {
  const nav = buildNav(withSubgroups([providerGroup(), logGroup()]));
  const view = contentFor(nav, "provider");
  assert.equal(view.title, "provider");
  assert.equal(view.count, 3);
  assert.deepEqual(view.sections.map((s) => s.id), ["provider/connection", "provider/models"]);
  assert.ok(view.sections.every((s) => s.heading), "a multi-child view names its children");
  assert.equal(view.searching, false);
});

test("a child selection narrows to that child alone", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  const view = contentFor(nav, "provider/models");
  assert.equal(view.count, 1);
  assert.equal(view.sections.length, 1);
  assert.equal(view.sections[0].heading, false, "a single list never repeats its title");
  assert.equal(view.sections[0].knobs[0].key, "provider.model_name");
});

test("a one-child domain renders one unheaded list", () => {
  const nav = buildNav(withSubgroups([logGroup()]));
  const view = contentFor(nav, "unknown");
  assert.equal(view.sections.length, 1);
  assert.equal(view.sections[0].heading, false);
  assert.equal(view.count, 2);
});

test("the flat virtual category renders flat", () => {
  const nav = buildNav(withSubgroups([commonGroup()]));
  const view = contentFor(nav, COMMON_CATEGORY);
  assert.equal(view.title, "Commonly Used");
  assert.equal(view.sections.length, 1);
  assert.equal(view.sections[0].heading, false);
});

test("a live query is CROSS-category (ruling 6)", () => {
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup(), logGroup()]));
  const hits = new Set(["provider.model_name", "custom.one"]);
  const view = contentFor(nav, "unknown", { hits });
  assert.equal(view.searching, true);
  assert.equal(view.title, SEARCH_TITLE);
  assert.deepEqual(view.sections.map((s) => s.id), ["provider", "unknown"]);
  assert.equal(view.count, 2, "each hit renders ONCE, under the domain that owns it");
});

test("search mode never duplicates a curated hit (the nav keeps its shape)", () => {
  // commonlyUsed carries provider.model_name; the provider section carries
  // it too - the content pane shows one row, the nav still shows the
  // curated category (greyed or lit, never hidden)
  const nav = buildNav(withSubgroups([commonGroup(), providerGroup()]));
  const hits = new Set(["provider.model_name"]);
  const view = contentFor(nav, COMMON_CATEGORY, { hits });
  assert.deepEqual(view.sections.map((s) => s.id), ["provider"]);
  assert.equal(view.count, 1);
  assert.ok(matchingCategoryIds(nav, { hits }).has(COMMON_CATEGORY),
    "the curated category is lit, not dropped");
});

test("an empty hit set is the empty state, never 'everything'", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  const view = contentFor(nav, "provider", { hits: new Set() });
  assert.equal(view.count, 0);
  assert.deepEqual(view.sections, []);
  // null means NO query is live
  assert.equal(contentFor(nav, "provider", { hits: null }).count, 3);
});

test("modified-only narrows the list and drops emptied sections (ruling 8)", () => {
  const nav = buildNav(withSubgroups([providerGroup()]));
  const isModified = (k: SettingKnob) => k.key === "provider.model_name";
  const view = contentFor(nav, "provider", { modifiedOnly: true, isModified });
  assert.equal(view.count, 1);
  assert.deepEqual(view.sections.map((s) => s.id), ["provider/models"]);
  // the switch is inert without the predicate (never drops silently)
  assert.equal(contentFor(nav, "provider", { modifiedOnly: true }).count, 0);
});

test("modified-only intersects with the query", () => {
  const nav = buildNav(withSubgroups([providerGroup(), logGroup()]));
  const hits = new Set(["provider.model_name", "custom.one"]);
  const isModified = (k: SettingKnob) => k.key === "custom.one";
  const view = contentFor(nav, "provider", { hits, modifiedOnly: true, isModified });
  assert.deepEqual(view.sections.map((s) => s.id), ["unknown"]);
  assert.equal(view.count, 1);
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

// --- the pane's SOURCE pins ----------------------------------------------

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const tab = read("app/components/settings/SettingsTab.vue");
const navSource = read("app/components/settings/settingsNav.ts");
const mainCss = read("app/assets/css/main.css");

test("the pane renders the two regions: a nav of categories and one content pane", () => {
  assert.match(tab, /<nav[\s\S]{0,200}class="settings-nav"/, "the nav region exists");
  assert.match(tab, /class="settings-split"/, "the split wrapper exists");
  assert.match(tab, /class="settings-content"/, "the content region exists");
  // the nav builds from the DATA, never from a hand-authored list
  assert.match(tab, /buildNav\(withSubgroups\(allGroups\.value\)\)/,
    "the tree derives from the per-knob subgroup data");
  assert.match(navSource, /KNOB_SUBGROUPS/, "the declaration table lives in one place");
});

test("card t_5656d52f: a parent carries a disclosure chevron - two hit targets, one row", () => {
  // the chevron is its OWN control: toggling the disclosure and selecting
  // the category are different clicks, so a parent can be picked without
  // being expanded
  assert.match(
    tab,
    /class="settings-nav-chev"[\s\S]{0,220}:aria-expanded="isOpen\(cat\.id\)"[\s\S]{0,160}@click="toggleOpen\(cat\.id\)"/,
    "the chevron owns the toggle",
  );
  assert.match(
    tab,
    /class="settings-nav-cat"[\s\S]{0,600}@click="selectCategory\(cat\.id\)"/,
    "the label still selects the category",
  );
  // children are gated by the open state, which starts open and is
  // overridden by a live query (hits must never hide)
  assert.match(tab, /v-for="sub in openChildren\(cat\)"/, "children render through the open gate");
  assert.match(
    tab,
    /const isOpen = \(catId: string\): boolean =>\s*\n\s*searching\.value \|\| !collapsedIds\.value\.includes\(catId\)/,
    "default expanded, and a live query force-opens every parent",
  );
  assert.match(tab, /const collapsedIds = ref<string\[\]>\(\[\]\)/, "component-local, starts expanded");
  assert.doesNotMatch(
    tab,
    /localStorage[\s\S]{0,120}collapsedIds|collapsedIds[\s\S]{0,120}localStorage/,
    "the collapsed set is never persisted (no store field, no client key)",
  );
  assert.match(
    tab,
    /subgroups\.length > 1 \? cat\.subgroups : \[\]/,
    "a one-child domain still shows no duplicate child row, and no chevron",
  );
});

test("no accordion returns: the only collapse left is the nav's own disclosure", () => {
  for (const dead of [
    "isCollapsed",
    "toggleCollapsed",
    "restoreCollapsed",
    "settings-group-head",
    "settings-group-title",
    "omega.settings-collapsed",
    "visibleGroups",
  ]) {
    assert.ok(!tab.includes(dead), `SettingsTab carries no ${dead}`);
  }
  assert.ok(!mainCss.includes("settings-group-head"), "the accordion's CSS is gone");
  assert.ok(!mainCss.includes(".settings-group-title"), "the head type rule is gone");
  assert.ok(!navSource.includes("Collapsed"), "the nav module never resurrects collapse state");
});

test("the nav is 200px wide - room for the chevron, the label and the count", () => {
  assert.match(
    tab,
    /\.settings-nav \{\s*flex: none;\s*width: 200px;\s*overflow-y: auto;\s*overflow-x: hidden;/,
    "wider nav, horizontal overflow still clipped",
  );
  assert.match(tab, /\.settings-nav-row \.settings-nav-cat \{\s*flex: 1;\s*min-width: 0;/, "the label flexes beside the chevron");
  assert.match(tab, /\.settings-nav-count \{\s*flex: none;/, "the count keeps its right-hand seat");
  assert.match(tab, /\.settings-nav-label \{[\s\S]{0,120}text-overflow: ellipsis;[\s\S]{0,80}white-space: nowrap;/,
    "labels ellipsize on one line, never wrap");
});

test("narrow panes DRILL DOWN - nav screen, then content with a back control (ruling 12)", () => {
  assert.match(tab, /v-if="!narrow \|\| !drilled"/, "the nav is its own narrow screen");
  assert.match(tab, /v-if="!narrow \|\| drilled"/, "the content is the second screen");
  assert.match(tab, /class="settings-back"/, "the back control exists");
  assert.match(tab, /aria-label="back to categories"/, "and it is labelled for a screen reader");
  assert.match(tab, /@click="drilled = false"/, "the back control returns to the nav");
  // no second renderer: no dropdown, no accordion path on any width
  assert.doesNotMatch(tab, /settings-jump|category-select|settings-dropdown/,
    "no category dropdown exists");
});

test("the selection is remembered per client, defaulting to Commonly Used", () => {
  assert.ok(tab.includes("rawSelection = ref<string>(COMMON_CATEGORY)"),
    "a fresh pane opens on Commonly Used");
  assert.ok(tab.includes('localStorage.setItem(NAV_STORAGE_KEY, id)'),
    "a selection is remembered in client storage");
  assert.ok(tab.includes('localStorage.getItem(NAV_STORAGE_KEY)'),
    "and restored on the next open");
  assert.equal(NAV_STORAGE_KEY, "omega.settings-category");
});

test("the content head carries the scope strip, the heading, its count and the switch", () => {
  assert.match(tab, /class="settings-content-head"/, "the strip/heading live in the content head");
  assert.match(tab, /class="settings-scopes"/, "the tier-3 strip survives, in the content pane");
  assert.match(tab, /class="settings-content-title"[\s\S]{0,200}\{\{ view\.title \}\}/,
    "the heading is the category name");
  assert.match(tab, /class="settings-knob-count"/, "the knob count rides the heading");
  assert.match(tab, /class="settings-modified"[\s\S]{0,200}:aria-pressed="modifiedOnly"/,
    "the modified-only switch is a pressed-state control");
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
  assert.match(tab, /hits: searchHits\.value/, "the hits feed the view and the grey-out set");
  assert.match(tab, /query\.value\.trim\(\) === ""\) return null/, "no query is null, not an empty hit set");
});
