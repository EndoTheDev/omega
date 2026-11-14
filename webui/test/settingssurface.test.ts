// omega webui: card t_f05b1f28 - the settings pane's SURFACE CLASSES.
//
// Two rulings ride this file:
//
//   1. the MODULES section is the pane's TAIL, not its head (the module
//      list is the last thing you read);
//   2. the sections are GROUPED frontend / backend under their own headers
//      - NOT a flat reorder - so every schema section carries a surface
//      class and the renderer groups by it.
//
// The classification is a TABLE (SECTION_SURFACES) and the render order is
// a TABLE (SURFACE_ORDER): Endo vetoes the mapping at review, and a veto
// edits one line. Everything here is pure - the component only paints what
// these functions return - and the panel wiring (nav template, document
// template, spy order) is pinned as SOURCE assertions, because the node
// suite has no DOM; the live behaviour is journey 113's job.
// Run: node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import type { SettingGroup, SettingKnob } from "../app/composables/omega.ts";
import {
  COMMON_CATEGORY,
  MODULES_CATEGORY,
  SECTION_SURFACES,
  SURFACE_BACKEND,
  SURFACE_CURATED,
  SURFACE_FRONTEND,
  SURFACE_MODULES,
  SURFACE_ORDER,
  buildNav,
  categorySurface,
  documentFor,
  groupBySurface,
  modulesNavCategory,
  withSubgroups,
} from "../app/components/settings/settingsNav.ts";

// --- fixtures -------------------------------------------------------------

const knob = (key: string, label: string): SettingKnob => ({
  key,
  label,
  type: "text",
  value: "",
});

const group = (key: string, label: string, knobs: SettingKnob[]): SettingGroup => ({
  key,
  label,
  knobs,
});

/** the pane's real payload shape: the curated view, the two client-state
    domains, the ten server domains - in WIRE order, exactly as the pane
    hands it to buildNav */
const payloadGroups = (): SettingGroup[] => [
  group(COMMON_CATEGORY, "Commonly Used", [knob("provider.model_name", "model name")]),
  group("chat", "chat", [knob("chat.theme", "theme")]),
  group("dock", "dock", [knob("dock.bottom_width", "bottom width")]),
  group("provider", "provider", [knob("provider.model_name", "model name")]),
  group("server", "server", [knob("server.port", "port")]),
  group("store", "store", [knob("store.db_path", "database path")]),
  group("sessions", "sessions", [knob("sessions.retention_days", "retention")]),
  group("compaction", "compaction", [knob("compaction.threshold", "threshold")]),
  group("runtime", "runtime", [knob("max_turns", "max turns")]),
  group("skills", "skills", [knob("skills.dir", "skills directory")]),
  group("memory", "memory", [knob("memory.enabled", "enabled")]),
  group("logging", "logging", [knob("logging.enabled", "enabled")]),
  group("approvals", "approvals", [knob("approvals.mode", "mode")]),
];

/** the pane's nav array: the knob nav in wire order, MODULES appended */
const paneNav = () => [...buildNav(withSubgroups(payloadGroups())), modulesNavCategory(4)];

const flatten = <T>(groups: { items: T[] }[]): T[] => groups.flatMap((g) => g.items);

// --- 1. the classification table -----------------------------------------

test("SURFACE_ORDER is curated, frontend, backend, modules - and only the two schema bands carry a header", () => {
  assert.deepEqual(
    SURFACE_ORDER.map((s) => s.id),
    [SURFACE_CURATED, SURFACE_FRONTEND, SURFACE_BACKEND, SURFACE_MODULES],
    "the render order: the curated all-view heads the pane, MODULES closes it",
  );
  const headed = SURFACE_ORDER.filter((s) => s.header).map((s) => s.id);
  assert.deepEqual(headed, [SURFACE_FRONTEND, SURFACE_BACKEND], "exactly two group headers");
  for (const surface of SURFACE_ORDER) {
    if (surface.header) assert.ok(surface.label.length > 0, `${surface.id} names its header`);
    else assert.equal(surface.label, "", `${surface.id} is headerless - no empty band`);
  }
});

test("the mapping: client state is frontend, server config is backend", () => {
  // the two CLIENT-STATE domains - their values never reach the server
  assert.equal(categorySurface("chat"), SURFACE_FRONTEND);
  assert.equal(categorySurface("dock"), SURFACE_FRONTEND);
  // every server config domain
  for (const id of [
    "provider",
    "server",
    "store",
    "sessions",
    "compaction",
    "runtime",
    "skills",
    "memory",
    "logging",
    "approvals",
  ]) {
    assert.equal(categorySurface(id), SURFACE_BACKEND, `${id} is server config`);
  }
  // the two classes that are NOT schema sections
  assert.equal(categorySurface(COMMON_CATEGORY), SURFACE_CURATED);
  assert.equal(categorySurface(MODULES_CATEGORY), SURFACE_MODULES);
  // a new group with no line describes server config until a line says else
  assert.equal(categorySurface("a.brand.new.domain"), SURFACE_BACKEND);
  // and the table is EXPLICIT for every domain the server ships - a group
  // moving between surfaces is a reviewable diff, never a silent default
  for (const id of ["chat", "dock", "provider", "server", "store", "sessions", "compaction"]) {
    assert.equal(typeof SECTION_SURFACES[id], "string", `${id} has its own line`);
  }
});

test("buildNav and modulesNavCategory stamp the surface class", () => {
  const nav = buildNav(withSubgroups(payloadGroups()));
  const byId = new Map(nav.map((cat) => [cat.id, cat.surface]));
  assert.equal(byId.get("chat"), SURFACE_FRONTEND);
  assert.equal(byId.get("dock"), SURFACE_FRONTEND);
  assert.equal(byId.get("provider"), SURFACE_BACKEND);
  assert.equal(byId.get(COMMON_CATEGORY), SURFACE_CURATED);
  assert.equal(modulesNavCategory(2).surface, SURFACE_MODULES);
});

// --- 2. the grouping -----------------------------------------------------

test("groupBySurface buckets in SURFACE_ORDER and keeps each bucket's own order", () => {
  const groups = groupBySurface(paneNav(), (cat) => cat.surface);
  assert.deepEqual(
    groups.map((g) => g.surface),
    [SURFACE_CURATED, SURFACE_FRONTEND, SURFACE_BACKEND, SURFACE_MODULES],
  );
  assert.deepEqual(groups.map((g) => g.header), [false, true, true, false], "the bands, in order");
  assert.deepEqual(
    groups.map((g) => g.label),
    ["", "frontend", "backend", ""],
    "the headers read frontend / backend - nothing else",
  );
  assert.deepEqual(
    flatten(groups).map((cat) => cat.id),
    [
      COMMON_CATEGORY,
      "chat",
      "dock",
      "provider",
      "server",
      "store",
      "sessions",
      "compaction",
      "runtime",
      "skills",
      "memory",
      "logging",
      "approvals",
      MODULES_CATEGORY,
    ],
    "the flattened groups ARE the pane order: curated, frontend, backend, modules",
  );
  // nothing is lost and nothing is duplicated by the bucketing
  assert.equal(flatten(groups).length, paneNav().length);
});

test("MODULES is the LAST section and Commonly Used the FIRST (card item 1)", () => {
  const ordered = flatten(groupBySurface(paneNav(), (cat) => cat.surface));
  assert.equal(ordered[0]!.id, COMMON_CATEGORY, "the curated all-view opens the pane");
  assert.equal(ordered[ordered.length - 1]!.id, MODULES_CATEGORY, "the module list closes it");
  // and the document the pane paints agrees: the module list is the tail
  const sections = documentFor(ordered);
  assert.notEqual(sections[sections.length - 1]!.id, MODULES_CATEGORY, "documentFor walks knobs");
  assert.equal(sections[0]!.id, COMMON_CATEGORY, "the curated section heads the document");
});

test("a surface SURFACE_ORDER does not name still renders - loud, never dropped", () => {
  const nav = [
    { id: "a", label: "a", flat: true, surface: "backend", knobs: [knob("a.k", "a")], subgroups: [] },
    { id: "b", label: "b", flat: true, surface: "sideways", knobs: [knob("b.k", "b")], subgroups: [] },
  ];
  const groups = groupBySurface(nav, (cat) => cat.surface);
  assert.deepEqual(
    groups.map((g) => g.surface),
    ["backend", "sideways"],
    "an unknown class is appended after the declared ones",
  );
  const stray = groups[groups.length - 1]!;
  assert.equal(stray.header, true, "and it prints - a typo is visible in the pane");
  assert.equal(stray.label, "sideways", "headed by the raw id, so the typo names itself");
  assert.equal(stray.items.length, 1, "its section is NOT swallowed");
});

test("an empty class prints nothing (no header over an empty band)", () => {
  const nav = [
    { id: "provider", label: "provider", flat: true, surface: SURFACE_BACKEND, knobs: [knob("provider.host", "host")], subgroups: [] },
  ];
  const groups = groupBySurface(nav, (cat) => cat.surface);
  assert.deepEqual(
    groups.map((g) => g.surface),
    [SURFACE_BACKEND],
    "the frontend band does not render at all when it carries no section",
  );
});

// --- 3. the document carries the class ------------------------------------

test("documentFor stamps each section with its surface class", () => {
  const sections = documentFor(paneNav());
  const byId = new Map(sections.map((s) => [s.id, s.surface]));
  assert.equal(byId.get("chat"), SURFACE_FRONTEND);
  assert.equal(byId.get("provider"), SURFACE_BACKEND);
  assert.equal(byId.get(COMMON_CATEGORY), SURFACE_CURATED);
  // the document groups into exactly the two bands the nav prints
  const groups = groupBySurface(sections, (s) => s.surface);
  assert.deepEqual(
    groups.map((g) => g.label),
    ["", "frontend", "backend"],
    "the module band is absent - its body is not a knob section",
  );
});

test("the section FILTER keeps the class but prints no band (the section heads the pane)", () => {
  const sections = documentFor(paneNav(), { only: "chat" });
  assert.deepEqual(sections.map((s) => s.id), ["chat"]);
  assert.equal(sections[0]!.surface, SURFACE_FRONTEND, "the class survives the filter");
  // ... and the pane's own gate drops the band while a section is filtered:
  // card t_cd9952a3's law is that the selected section HEADS the document,
  // so nothing may sit above it (the nav, still banded, names the group)
  assert.match(
    tab,
    /v-if="group\.header && filterSection === null"/,
    "the document band is wayfinding for the long list, not a header over a single section",
  );
});

// --- 4. the pane's source wiring -----------------------------------------

const root = fileURLToPath(new URL("..", import.meta.url));
const tab = readFileSync(`${root}/app/components/settings/SettingsTab.vue`, "utf8");

test("the nav and the document both render the groups, in one order", () => {
  assert.match(
    tab,
    /const navGroups = computed<SurfaceGroup<NavCategory>\[\]>\(\(\) =>\s*groupBySurface\(/,
    "the nav's groups are the pure bucketing",
  );
  assert.match(
    tab,
    /const nav = computed<NavCategory\[\]>\(\(\) =>\s*navGroups\.value\.flatMap\(\(group\) => group\.items\)/,
    "and the nav array IS the flattened groups - one order for jump links, document and spy",
  );
  assert.match(tab, /v-for="group in navGroups"/, "the jump links print the group bands");
  assert.match(tab, /v-for="group in docGroups"/, "the document prints them too");
  assert.match(tab, /class="settings-nav-surface"/, "the nav band has its own class");
  assert.match(
    tab,
    /v-if="group\.header && filterSection === null"\s+class="settings-surface-title">\{\{ group\.label \}\}/,
    "the document band prints the group's own label, and only when the class carries one",
  );
  assert.match(
    tab,
    /const docGroups = computed<SurfaceGroup<DocSection>\[\]>\(\(\) =>\s*groupBySurface\(docSections\.value/,
    "the document's groups come from the same function",
  );
});

test("MODULES is appended last - the nav entry AND the document block", () => {
  assert.match(
    tab,
    /\[\.\.\.buildNav\(withSubgroups\(allGroups\.value\)\), modulesNavCategory\(moduleCount\.value\)\]/,
    "the module entry is the LAST element of the nav input",
  );
  // the modules section body is rendered by its own block (documentFor walks
  // knobs only), and it must sit AFTER the surface groups
  const docLoop = tab.indexOf('v-for="group in docGroups"');
  const modulesBlock = tab.indexOf('class="settings-category settings-modules-category"');
  assert.ok(docLoop > 0 && modulesBlock > 0, "both blocks exist");
  assert.ok(
    modulesBlock > docLoop,
    "the MODULES section renders after the grouped document - it is the pane's tail",
  );
});

test("the scroll-spy resolves MODULES last, where the section now sits", () => {
  assert.match(
    tab,
    /const spyOrder = computed<string\[\]>\(\(\) => \[\s*\.\.\.spyTargetIds\(docSections\.value\),\s*\.\.\.\(modulesShown\.value \? \[MODULES_CATEGORY\] : \[\]\),/,
    "the module id rides last, matching document order",
  );
});

test("the group bands are styled as bands, not as a second pinned heading", () => {
  const navRule = tab.match(/\.settings-nav-surface\s*\{[^}]*\}/)?.[0] ?? "";
  assert.ok(navRule.length > 0, "the nav band has a rule");
  assert.match(navRule, /text-transform:\s*uppercase/, "the band reads as a label");
  assert.match(navRule, /border-top:\s*1px solid var\(--stroke\)/, "and marks the group boundary");
  const docRule = tab.match(/\.settings-surface-title\s*\{[^}]*\}/)?.[0] ?? "";
  assert.ok(docRule.length > 0, "the document band has a rule");
  assert.doesNotMatch(
    docRule,
    /position:\s*sticky/,
    "the section heading keeps the ONE pinned level (t_a8063683) - the band scrolls away",
  );
});
