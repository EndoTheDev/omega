// omega webui: the settings pane's CATEGORY NAVIGATION. A category with no knobs never
// enters the nav.

import type { SettingGroup, SettingKnob } from "../../composables/omega";

/** the implicit child every undeclared knob lands in */
export const GENERAL_SUBGROUP = "general";
/** the virtual top category of curated knobs */
export const COMMON_CATEGORY = "common";
/** the MODULES category - the module lifecycle surface, which is CONFIGURATION. */
export const MODULES_CATEGORY = "modules";
/** client storage key for the remembered LAST ANCHOR */
export const NAV_STORAGE_KEY = "omega.settings-category";

// the surface classification ------------------------- The pane's sections are. The
// classification is DATA - a table, never a derivation from a key's spelling - exactly
// like the nesting table below: Endo vetoes the mapping at review, and a veto edits ONE
// line here.
export const SURFACE_FRONTEND = "frontend";
export const SURFACE_BACKEND = "backend";
export const SURFACE_CURATED = "curated";
export const SURFACE_MODULES = "modules";

export interface SettingSurface {
  id: string;
  label: string;
  header: boolean;
}

/**
 * THE render order.
 * A surface missing from here still renders (its group is appended, headed by the raw
 * id) - a classification can never make a section vanish.
 */
export const SURFACE_ORDER: readonly SettingSurface[] = [
  { id: SURFACE_CURATED, label: "", header: false },
  { id: SURFACE_FRONTEND, label: "frontend", header: true },
  { id: SURFACE_BACKEND, label: "backend", header: true },
  { id: SURFACE_MODULES, label: "", header: false },
];

/**
 * SECTION_SURFACES: section id -> surface class.
 * The two CLIENT-STATE domains are the frontend: their values live in this browser
 * (theme, dock width, turn display) and never reach the server.
 * An undeclared section is BACKEND - a new schema group describes server config until a
 * line here says otherwise (and a new CLIENT-STATE group must say so explicitly, or it
 * renders under the wrong header).
 */
export const SECTION_SURFACES: Readonly<Record<string, string>> = {
  [COMMON_CATEGORY]: SURFACE_CURATED,
  [MODULES_CATEGORY]: SURFACE_MODULES,
  chat: SURFACE_FRONTEND,
  dock: SURFACE_FRONTEND,
  provider: SURFACE_BACKEND,
  server: SURFACE_BACKEND,
  store: SURFACE_BACKEND,
  sessions: SURFACE_BACKEND,
  compaction: SURFACE_BACKEND,
  runtime: SURFACE_BACKEND,
  skills: SURFACE_BACKEND,
  memory: SURFACE_BACKEND,
  logging: SURFACE_BACKEND,
  approvals: SURFACE_BACKEND,
};

/** the surface a section renders under - the declared one, else backend */
export function categorySurface(id: string): string {
  return SECTION_SURFACES[id] ?? SURFACE_BACKEND;
}

export interface SurfaceGroup<T> {
  surface: string;
  label: string;
  header: boolean;
  items: T[];
}

/**
 * groupBySurface buckets items by their surface class IN SURFACE_ORDER, keeping each
 * bucket's own order (callers pass nav/document order, so a stable bucket is a stable
 * group).
 * A class SURFACE_ORDER does not name still renders - appended, headed by the raw id -
 * because a typo'd classification must be LOUD, never a silently dropped section.
 */
export function groupBySurface<T>(
  items: readonly T[],
  surfaceOf: (item: T) => string,
): SurfaceGroup<T>[] {
  const buckets = new Map<string, T[]>();
  for (const item of items) {
    const id = surfaceOf(item);
    const bucket = buckets.get(id);
    if (bucket) bucket.push(item);
    else buckets.set(id, [item]);
  }
  const groups: SurfaceGroup<T>[] = [];
  for (const surface of SURFACE_ORDER) {
    const bucket = buckets.get(surface.id);
    if (bucket === undefined || bucket.length === 0) continue;
    groups.push({
      surface: surface.id,
      label: surface.label,
      header: surface.header,
      items: bucket,
    });
    buckets.delete(surface.id);
  }
  for (const [surface, bucket] of buckets) {
    groups.push({ surface, label: surface, header: true, items: bucket });
  }
  return groups;
}

// the nesting data --------------------------------------

/** KNOB_SUBGROUPS: knob key -> subgroup id. */
export const KNOB_SUBGROUPS: Readonly<Record<string, string>> = {
  // provider -: the connection subgroup and the instances subgroup MERGED into one.
  "provider.instances": "providers",
  "provider.model_name": "models",
  "provider.vision_model": "models",
  "provider.title_model": "models",
  "provider.approvals_model": "models",
  "provider.compaction_model": "models",
  "provider.summarizer_model": "models",
  "provider.skills_model": "models",
  "server.port": "network",
  "server.bind": "network",
  "server.run_timeout": "runs",
  "server.default_workspace": "workspace",
  "store.db_path": "database",
  "sessions.retention_days": "retention",
  "sessions.summarize_after_hours": "summaries",
  "compaction.enabled": "policy",
  "compaction.threshold": "policy",
  "compaction.context_window": "budget",
  "compaction.reserve_tokens": "budget",
  "compaction.max_tool_output": "budget",
  "compaction.keep_first": "window",
  "compaction.keep_last": "window",
  "system_prompt": "loop",
  "max_turns": "loop",
  "http_timeout": "network",
  "skills.dir": "library",
  "skills.nudge_interval": "nudges",
  "memory.enabled": "state",
  "memory.char_limit": "limits",
  "memory.user_char_limit": "limits",
  "memory.file": "files",
  "memory.user_file": "files",
  "memory.user_profile_enabled": "profile",
  "logging.enabled": "state",
  "logging.file": "files",
  "approvals.mode": "policy",
  "approvals.smart_policy": "smart-policy",
  "chat.color_mode": "appearance",
  "chat.theme": "appearance",
  "chat.show_thinking": "reasoning",
  "chat.appearance": "turn-display",
  "chat.tool_display": "turn-display",
  "chat.thinking_display": "turn-display",
  "dock.bottom_width": "panel",
};

/**
 * SUBGROUP_LABELS: the child's display name.
 * An id with no line here renders as the id itself - a missing label is never a blank
 * nav row.
 */
export const SUBGROUP_LABELS: Readonly<Record<string, string>> = {
  general: "general",
  providers: "providers",
  models: "models",
  network: "network",
  runs: "runs",
  workspace: "workspace",
  database: "database",
  retention: "retention",
  summaries: "summaries",
  policy: "policy",
  budget: "token budget",
  window: "kept window",
  loop: "agent loop",
  library: "library",
  nudges: "nudges",
  state: "state",
  limits: "limits",
  files: "files",
  profile: "user profile",
  "smart-policy": "smart policy",
  appearance: "appearance",
  reasoning: "reasoning",
  "turn-display": "turn display",
  panel: "panel",
};

/** the subgroup a knob belongs to - the declared one, else "general" */
export function knobSubgroup(key: string): string {
  return KNOB_SUBGROUPS[key] ?? GENERAL_SUBGROUP;
}

/** the display name of a subgroup id (tolerant: the id is the fallback) */
export function subgroupLabel(id: string): string {
  return SUBGROUP_LABELS[id] ?? id;
}

// the tree ----------------------------------------------------------

export interface NavSubgroup {
  id: string;
  label: string;
  knobs: SettingKnob[];
}

export interface NavCategory {
  id: string;
  label: string;
  flat: boolean;
  surface: string;
  knobs: SettingKnob[];
  subgroups: NavSubgroup[];
  count?: number;
}

/** the number the nav prints for a category. */
export function categoryCount(cat: NavCategory): number {
  return cat.count ?? cat.knobs.length;
}

/**
 * modulesNavCategory is the MODULES entry the settings pane APPENDS to the nav.
 * Its document section is rendered by the pane's own ModulesSection block, so
 * documentFor (which walks knobs) never emits it - the nav entry is what makes it a
 * jump target.
 */
export function modulesNavCategory(count = 0): NavCategory {
  return {
    id: MODULES_CATEGORY,
    label: "modules",
    flat: true,
    surface: SURFACE_MODULES,
    knobs: [],
    subgroups: [],
    count,
  };
}

/**
 * withSubgroups stamps every knob with its declared subgroup, so the nav's `subgroup`.
 * Returns fresh groups - the caller's arrays are never mutated.
 */
export function withSubgroups(groups: SettingGroup[]): SettingGroup[] {
  return groups.map((group) => ({
    ...group,
    knobs: group.knobs.map((knob) => ({
      ...knob,
      subgroup: knob.subgroup ?? knobSubgroup(knob.key),
    })),
  }));
}

/** buildNav derives the two-level tree from the (already annotated) groups. */
export function buildNav(groups: SettingGroup[]): NavCategory[] {
  const nav: NavCategory[] = [];
  for (const group of groups) {
    if (group.knobs.length === 0) continue;
    const flat = group.key === COMMON_CATEGORY;
    const surface = categorySurface(group.key);
    if (flat) {
      nav.push({
        id: group.key,
        label: group.label,
        flat: true,
        surface,
        knobs: group.knobs,
        subgroups: [],
      });
      continue;
    }
    const order: string[] = [];
    const byId = new Map<string, SettingKnob[]>();
    for (const knob of group.knobs) {
      const id = knob.subgroup ?? knobSubgroup(knob.key);
      const bucket = byId.get(id);
      if (bucket) bucket.push(knob);
      else {
        byId.set(id, [knob]);
        order.push(id);
      }
    }
    if (order.length > 1 && order.includes(GENERAL_SUBGROUP)) {
      order.splice(order.indexOf(GENERAL_SUBGROUP), 1);
      order.unshift(GENERAL_SUBGROUP);
    }
    nav.push({
      id: group.key,
      label: group.label,
      flat: false,
      surface,
      knobs: group.knobs,
      subgroups: order.map((id) => ({
        id,
        label: subgroupLabel(id),
        knobs: byId.get(id) ?? [],
      })),
    });
  }
  return nav;
}

// selection algebra -------------------------------------------------

export interface Selection {
  category: string;
  subgroup: string | null;
}

export interface ResolvedSelection {
  category: NavCategory;
  subgroup: NavSubgroup | null;
}

/** the selection id the nav buttons carry and the pane remembers. */
export function selectionId(category: string, subgroup: string | null = null): string {
  return subgroup === null || subgroup === "" ? category : `${category}/${subgroup}`;
}

export function parseSelection(id: string): Selection {
  const cut = id.indexOf("/");
  if (cut < 0) return { category: id, subgroup: null };
  return { category: id.slice(0, cut), subgroup: id.slice(cut + 1) || null };
}

/**
 * resolveSelection maps a persisted id onto the LIVE nav: a category or child that no
 * longer exists (module unmounted, knob set changed) is not an error - it answers null
 * and the caller falls back.
 * A child id under a known category but an unknown child resolves to the category
 * itself (never a blank pane).
 */
export function resolveSelection(
  nav: NavCategory[],
  id: string,
): ResolvedSelection | null {
  const selection = parseSelection(id);
  const category = nav.find((c) => c.id === selection.category);
  if (!category) return null;
  if (selection.subgroup === null) return { category, subgroup: null };
  const subgroup = category.subgroups.find((s) => s.id === selection.subgroup);
  return { category, subgroup: subgroup ?? null };
}

/**
 * selectionLost is the pane's NORMALISATION question, asked of every id it remembers.
 * Normalising the highlight ONLY left that filter standing, and a document filtered to
 * a category the live nav cannot resolve renders NOTHING (the pane answers "no settings
 * here", and the URL keeps a section the pane is not showing).
 */
export function selectionLost(
  nav: NavCategory[],
  ids: readonly (string | null | undefined)[],
): boolean {
  return ids.some((id) => !!id && resolveSelection(nav, id) === null);
}

/**
 * anchorSelection resolves the REMEMBERED value into the id the pane opens scrolled.
 * A remembered CHILD the current nav no longer carries normalises to its category: a
 * stale id is never a broken restore.
 */
export function anchorSelection(
  nav: NavCategory[],
  remembered: string | null,
): string | null {
  if (!remembered) return null;
  const resolved = resolveSelection(nav, remembered);
  if (!resolved) return null;
  return selectionId(resolved.category.id, resolved.subgroup?.id ?? null);
}

/**
 * expandedCategoryIds is the nav's OPEN branches - DERIVED, never remembered.
 * There is no collapse set anywhere: "what is open" and "where I am" cannot disagree.
 */
export function expandedCategoryIds(
  selection: string | null,
  queryOpen: ReadonlySet<string> | null = null,
): Set<string> {
  if (queryOpen !== null) return new Set(queryOpen);
  if (!selection) return new Set<string>();
  return new Set<string>([parseSelection(selection).category]);
}

// the ONE document --------------------------------------------------

export interface DocBlock {
  id: string;
  label: string;
  heading: boolean;
  knobs: SettingKnob[];
}

export interface DocSection {
  id: string;
  label: string;
  /**
   * the surface class the renderer groups this section under (carried from the nav so
   * the document and the nav can never disagree about which group a section belongs to)
   * / /
   */
  surface: string;
  count: number;
  blocks: DocBlock[];
}

export interface ContentInput {
  hits?: ReadonlySet<string> | null;
  modifiedOnly?: boolean;
  isModified?: (knob: SettingKnob) => boolean;
  only?: string | null;
}

function keeper(input: ContentInput): (knob: SettingKnob) => boolean {
  const hits = input.hits ?? null;
  const modifiedOnly = input.modifiedOnly === true;
  const isModified = input.isModified;
  return (knob) => {
    if (hits !== null && !hits.has(knob.key)) return false;
    if (modifiedOnly && !(isModified ? isModified(knob) : false)) return false;
    return true;
  };
}

/**
 * sectionFilter is the CATEGORY the pane's document is narrowed to,.
 * It used to be exempted as a "curated all-view" (its knobs are borrowed from every
 * domain, so the narrowing looked like a duplicate list); the exemption read as a
 * broken filter, because every category narrowed except this one, which left the whole
 * document on screen.
 * A subsection id resolves to its CATEGORY: "provider/models" filters to provider
 * (which renders that block), never to a section that does not exist.
 */
export function sectionFilter(
  selected: string | null,
  searching: boolean,
): string | null {
  if (searching) return null;
  if (!selected) return null;
  return parseSelection(selected).category;
}

/**
 * documentFor is the pane's whole body.
 * "modified only" narrows the SAME document, whole - never one category at a time.
 */
export function documentFor(
  nav: NavCategory[],
  input: ContentInput = {},
): DocSection[] {
  const keep = keeper(input);
  const searching = input.hits != null;
  const only = searching ? null : input.only ?? null;
  const sections: DocSection[] = [];
  for (const category of nav) {
    if (searching && category.id === COMMON_CATEGORY) continue;
    if (only !== null && category.id !== only) continue;
    const blocks: DocBlock[] = [];
    if (category.flat || category.subgroups.length <= 1) {
      const knobs = category.knobs.filter(keep);
      if (knobs.length > 0)
        blocks.push({
          id: category.id,
          label: category.label,
          heading: false,
          knobs,
        });
    } else {
      for (const child of category.subgroups) {
        const knobs = child.knobs.filter(keep);
        if (knobs.length === 0) continue;
        blocks.push({
          id: selectionId(category.id, child.id),
          label: child.label,
          heading: true,
          knobs,
        });
      }
    }
    const count = blocks.reduce((n, block) => n + block.knobs.length, 0);
    if (count === 0) continue;
    sections.push({
      id: category.id,
      label: category.label,
      surface: category.surface,
      count,
      blocks,
    });
  }
  return sections;
}

/** spyTargetIds is the scroll-spy's document order. */
export function spyTargetIds(sections: DocSection[]): string[] {
  return sections.flatMap((section) => [
    section.id,
    ...section.blocks
      .map((block) => block.id)
      .filter((id) => id !== section.id),
  ]);
}

/** The node surface the spy/clamp target resolution needs. */
export interface SpyNodeLike {
  readonly dataset: { readonly section?: string };
  closest(selector: string): unknown;
}

/** buildSpyTargets is the pane's id -> node map (the spy observes these nodes, the. */
export function buildSpyTargets<T extends SpyNodeLike>(root: {
  querySelectorAll(selector: string): Iterable<T>;
}): Map<string, T> {
  const targets = new Map<string, T>();
  for (const node of root.querySelectorAll(".settings-category[data-section]")) {
    const id = node.dataset.section;
    if (id) targets.set(id, node);
  }
  for (const node of root.querySelectorAll(".settings-block[data-section]")) {
    const id = node.dataset.section;
    if (id && !targets.has(id)) targets.set(id, node);
  }
  return targets;
}

/** targetSection is the box a scroll anchor lands IN: the nearest category section. */
export function targetSection<T extends SpyNodeLike>(node: T): T {
  return (node.closest(".settings-category") as T | null) ?? node;
}

/**
 * matchingCategoryIds is the nav's grey-out set.
 * The nav itself never changes shape.
 */
export function matchingCategoryIds(
  nav: NavCategory[],
  input: ContentInput = {},
): Set<string> {
  const keep = keeper(input);
  const ids = new Set<string>();
  for (const category of nav) if (category.knobs.some(keep)) ids.add(category.id);
  return ids;
}

// the selected-section scroll clamp ---------------

export interface ClampGeometry {
  top: number;
  height: number;
  viewport: number;
  content: number;
}

/**
 * clampedRange answers the `[min, max]` scrollTop window a SELECTED section allows.
 * The clamped-range maths is settled law and keeps it verbatim: the pane still scrolls,
 * and the clamp only bounds WHERE it may rest - `min` puts the section's top at the
 * scrollport's top edge (its sticky heading is the section's first child, so the
 * heading pins exactly there), `max` puts the section's bottom at the bottom edge.
 * That is the honest answer, not a bug, and the clamp can never produce `max < min`.
 */
export function clampedRange(input: ClampGeometry): [number, number] {
  const viewport = Math.max(0, input.viewport);
  const limit = Math.max(0, input.content - viewport);
  // the section's top, never above the document's start nor below its end
  const min = Math.min(Math.max(0, input.top), limit);
  const natural = input.top + input.height - viewport;
  const max = Math.max(min, Math.min(natural, limit));
  return [min, max];
}

// the end follows the box ---------------------------- clampedRange bounds a. That
// makes the START follow its box in both directions (it is enforced with max(now, min),
// so a section whose top moves DOWN drags the pane with it) and the END follow it in
// only ONE (min(now, max) pulls a pane UP when the end shrinks, but a section that
// GROWS under a pane resting on its bottom leaves that pane short of it for as long as
// nothing scrolls again - the nightly's clamp witness rested at 810 while its own end
// measured 814, and every later pass found 810 a legal position, so the 4px never
// healed). A later pass that measures a LONGER end while the pane still rests exactly
// there answers the new end; every other pass answers null and the pane keeps its
// place. Only a pane ON the end follows - one parked anywhere inside the section is
// never moved.

export interface ClampHold {
  at: number;
  max: number;
}

/** clampEndFollow answers the scrollTop a pane must take when the window's END grew. */
export function clampEndFollow(
  now: number,
  hold: ClampHold | null,
  max: number,
): number | null {
  if (hold === null) return null;
  // the record only means anything when the pane WAS on that end
  if (Math.abs(hold.at - hold.max) > 1) return null;
  // the pane is no longer where the clamp left it - the user's own scroll
  if (Math.abs(now - hold.at) > 1) return null;
  // the end did not grow: nothing to follow (a SHRUNK end is the bound's)
  if (max <= hold.max + 0.5) return null;
  if (Math.abs(max - now) < 1) return null;
  return max;
}

// the pane's OWN scrolls ----------------------------- The clamp and the scroll-spy are
// suppressed for exactly ONE reason: a scroll the PANE issued itself (a jump, a
// deselect's return to top, a restore, a cleared query's give-back).

export interface PaneScrollState {
  target: number | null;
  graceUntil: number;
  intent: boolean;
}

/** paneScrollSettled: the pane's own scroll is over. */
export function paneScrollSettled(state: PaneScrollState): boolean {
  return state.target === null;
}

/**
 * paneScrollOwns: the ONE scroll the clamp may not measure.
 * The suppression exists so a jump's smooth scroll is never cut short: the pane is
 * travelling to a position it chose, and the window it passes through on the way is not
 * the clamp's business (a filtered document puts the pane's old position just past the
 * section's end, so the FIRST frame of a jump is often outside the live window -
 * enforcing there would cancel the animation a few px in and the pane would never reach
 * its section).
 */
export function paneScrollOwns(state: PaneScrollState): boolean {
  return state.target !== null && state.intent;
}

/**
 * paneScrollIsUser: this scroll event is the USER's - no pane scroll in flight, and the
 * settle window (which swallows an animation's trailing repeats, so a deselect cannot
 * re-select what its return-to-top just passed) is.
 */
export function paneScrollIsUser(state: PaneScrollState, now: number): boolean {
  return state.target === null && now >= state.graceUntil;
}

/** paneScrollSettle: the pane's own scroll ended at `now`. */
export function paneScrollSettle(now: number, graceMs: number): PaneScrollState {
  return { target: null, graceUntil: now + graceMs, intent: false };
}

// the anchor is the USER's --------------------------- The remembered anchor means. A
// delivery like that resolves whatever id the NEW boxes put at the top edge, and the
// nightly caught it overwriting the anchor an explicit jump had just written - a drawer
// jump to "compaction" at 340px, a widen to 1280px, and the stored anchor read back as
// the neighbouring section. So the write is gated on ORDER, never on a flag someone
// must remember to clear: a delivery may write only when the user's scroll is newer
// than the last geometry change, and only once per scroll. A scroll the pane issued
// itself (a jump, a re-clamp, a restore) never stamps `userScrollAt`, so the ONE
// suppression of keeps covering the anchor too.

export interface SpyAnchorTrace {
  /** when a scroll event was last judged the USER's (ms); 0 = never */
  userScrollAt: number;
  /**
   * when the spy's boxes last moved with no scroll - a resize, a re-arm (ms); 0 = never
   * / /
   */
  geometryAt: number;
  wroteAt: number;
}

/** spyAnchorMayWrite: this spy decision may persist the remembered anchor. */
export function spyAnchorMayWrite(trace: SpyAnchorTrace): boolean {
  return trace.userScrollAt > trace.geometryAt && trace.userScrollAt > trace.wroteAt;
}

// the URL carries the section ----------------- The in-app URL owns the focused. This
// card adds the settings pane's section on the QUERY axis instead of inventing a second
// path scheme: `?settings=<section>` rides whatever path is current, so the two never
// fight, and the settings-path card can add `/settings` later without touching this
// parameter.

/** the query parameter the settings section rides */
export const SETTINGS_SECTION_PARAM = "settings";

/**
 * parseSettingsSection answers the section id a settings URL carries.
 * Null for "no parameter" and for a BLANK one: `?settings=` is a deselect that leaked
 * into a bookmark, never a section named "".
 */
export function parseSettingsSection(href: string): string | null {
  const url = settingsUrl(href);
  const raw = url.searchParams.get(SETTINGS_SECTION_PARAM);
  if (raw === null) return null;
  const id = raw.trim();
  return id === "" ? null : id;
}

/** withSettingsSection answers the path a replaceState should commit. */
export function withSettingsSection(
  href: string,
  section: string | null,
): string {
  const url = settingsUrl(href);
  const pairs: string[] = [];
  url.searchParams.forEach((value, key) => {
    if (key === SETTINGS_SECTION_PARAM) return;
    pairs.push(`${encodeURIComponent(key)}=${encodeURIComponent(value)}`);
  });
  const id = (section ?? "").trim();
  if (id !== "") {
    pairs.push(
      `${SETTINGS_SECTION_PARAM}=${encodeURIComponent(id).replace(/%2F/g, "/")}`,
    );
  }
  const search = pairs.length > 0 ? `?${pairs.join("&")}` : "";
  return `${url.pathname}${search}${url.hash}`;
}

/** settingsUrl parses an absolute OR relative href (the node suite pins it with. */
function settingsUrl(href: string): URL {
  try {
    return new URL(href);
  } catch {
    return new URL(href, "http://omega.invalid/");
  }
}
