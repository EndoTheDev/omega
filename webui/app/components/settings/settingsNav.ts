
import type { SettingGroup, SettingKnob } from "../../composables/omega";

export const GENERAL_SUBGROUP = "general";
export const COMMON_CATEGORY = "common";
export const MODULES_CATEGORY = "modules";
export const NAV_STORAGE_KEY = "omega.settings-category";

// The pane's sections are classified in DATA. A reclassification edits one
// line here - never a rule inferred from a key's spelling.
export const SURFACE_FRONTEND = "frontend";
export const SURFACE_BACKEND = "backend";
export const SURFACE_CURATED = "curated";
export const SURFACE_MODULES = "modules";

interface SettingSurface {
  id: string;
  label: string;
  header: boolean;
}

/** THE render order. A surface missing here still renders (appended, headed by the
    raw id) - a classification can never make a section vanish. */
export const SURFACE_ORDER: readonly SettingSurface[] = [
  { id: SURFACE_CURATED, label: "", header: false },
  { id: SURFACE_FRONTEND, label: "frontend", header: true },
  { id: SURFACE_BACKEND, label: "backend", header: true },
  { id: SURFACE_MODULES, label: "", header: false },
];

/** section id -> surface class. chat/dock are the CLIENT-STATE frontend (values
    live in this browser, never on the server); an undeclared section is BACKEND,
    so a new client-state group must say so here or it renders under the wrong
    header. */
export const SECTION_SURFACES: Readonly<Record<string, string>> = {
  [COMMON_CATEGORY]: SURFACE_CURATED,
  [MODULES_CATEGORY]: SURFACE_MODULES,
  chat: SURFACE_FRONTEND,
  dock: SURFACE_FRONTEND,
  provider: SURFACE_BACKEND,
  server: SURFACE_BACKEND,
  store: SURFACE_BACKEND,
  updates: SURFACE_BACKEND,
  sessions: SURFACE_BACKEND,
  compaction: SURFACE_BACKEND,
  runtime: SURFACE_BACKEND,
  skills: SURFACE_BACKEND,
  memory: SURFACE_BACKEND,
  logging: SURFACE_BACKEND,
  approvals: SURFACE_BACKEND,
};

export function categorySurface(id: string): string {
  return SECTION_SURFACES[id] ?? SURFACE_BACKEND;
}

export interface SurfaceGroup<T> {
  surface: string;
  label: string;
  header: boolean;
  items: T[];
}

/** buckets items by surface class IN SURFACE_ORDER, keeping each bucket's own order.
    A class SURFACE_ORDER does not name still renders - appended, headed by the raw
    id - because a typo'd classification must be LOUD, never a silent drop. */
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


export const KNOB_SUBGROUPS: Readonly<Record<string, string>> = {
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
  "updates.channel": "channel",
  "updates.auto_check": "automation",
  "updates.auto_update": "automation",
  "updates.managed": "scope",
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

/** the child's display name; an id with no line here renders as the id, never a blank row. */
export const SUBGROUP_LABELS: Readonly<Record<string, string>> = {
  general: "general",
  providers: "providers",
  models: "models",
  network: "network",
  runs: "runs",
  workspace: "workspace",
  database: "database",
  channel: "channel",
  automation: "automation",
  scope: "scope",
  retention: "retention",
  summaries: "summaries",
  policy: "policy",
  budget: "token budget",
  window: "kept window",
  loop: "agent loop",
  library: "library",
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

export function knobSubgroup(key: string): string {
  return KNOB_SUBGROUPS[key] ?? GENERAL_SUBGROUP;
}

export function subgroupLabel(id: string): string {
  return SUBGROUP_LABELS[id] ?? id;
}


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

export function categoryCount(cat: NavCategory): number {
  return cat.count ?? cat.knobs.length;
}

/** the MODULES entry the pane APPENDS to the nav: documentFor walks knobs and never
    emits it, so this entry is what makes the section a jump target. */
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

/** stamps every knob with its declared subgroup, on fresh groups - no caller array
    is mutated. */
export function withSubgroups(groups: SettingGroup[]): SettingGroup[] {
  return groups.map((group) => ({
    ...group,
    knobs: group.knobs.map((knob) => ({
      ...knob,
      subgroup: knob.subgroup ?? knobSubgroup(knob.key),
    })),
  }));
}

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


interface Selection {
  category: string;
  subgroup: string | null;
}

interface ResolvedSelection {
  category: NavCategory;
  subgroup: NavSubgroup | null;
}

export function selectionId(category: string, subgroup: string | null = null): string {
  return subgroup === null || subgroup === "" ? category : `${category}/${subgroup}`;
}

export function parseSelection(id: string): Selection {
  const cut = id.indexOf("/");
  if (cut < 0) return { category: id, subgroup: null };
  return { category: id.slice(0, cut), subgroup: id.slice(cut + 1) || null };
}

/** maps a persisted id onto the LIVE nav: an id no longer there answers null (the
    caller falls back); an unknown child answers its category, never a blank pane. */
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

/** the pane's NORMALISATION question, asked of every id it remembers: a document
    filtered to a category the live nav cannot resolve would render nothing. */
export function selectionLost(
  nav: NavCategory[],
  ids: readonly (string | null | undefined)[],
): boolean {
  return ids.some((id) => !!id && resolveSelection(nav, id) === null);
}

/** resolves the REMEMBERED value into the id the pane opens scrolled; a remembered
    child the nav no longer carries normalises to its category, never a broken restore. */
export function anchorSelection(
  nav: NavCategory[],
  remembered: string | null,
): string | null {
  if (!remembered) return null;
  const resolved = resolveSelection(nav, remembered);
  if (!resolved) return null;
  return selectionId(resolved.category.id, resolved.subgroup?.id ?? null);
}

/** the nav's OPEN branches - DERIVED, never remembered: with no collapse set
    anywhere, "what is open" and "where I am" cannot disagree. */
export function expandedCategoryIds(
  selection: string | null,
  queryOpen: ReadonlySet<string> | null = null,
): Set<string> {
  if (queryOpen !== null) return new Set(queryOpen);
  if (!selection) return new Set<string>();
  return new Set<string>([parseSelection(selection).category]);
}


export interface DocBlock {
  id: string;
  label: string;
  heading: boolean;
  knobs: SettingKnob[];
}

export interface DocSection {
  id: string;
  label: string;
  /** the surface class the renderer groups this section under - carried from the
      nav, so document and nav can never disagree about which group a section is in. */
  surface: string;
  count: number;
  blocks: DocBlock[];
}

interface ContentInput {
  /** knob keys the live query matched - null/undefined means NO query is live, which
      is not an EMPTY set (a query that matched nothing: "everything" vs the empty
      state) */
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

/** the CATEGORY the document is narrowed to; a subsection id resolves to its
    CATEGORY ("provider/models" filters to provider, which renders that block). */
export function sectionFilter(
  selected: string | null,
  searching: boolean,
): string | null {
  if (searching) return null;
  if (!selected) return null;
  return parseSelection(selected).category;
}

/** the pane's whole body. "modified only" narrows the SAME document, whole - never
    one category. */
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

export function spyTargetIds(sections: DocSection[]): string[] {
  return sections.flatMap((section) => [
    section.id,
    ...section.blocks
      .map((block) => block.id)
      .filter((id) => id !== section.id),
  ]);
}

export interface SpyNodeLike {
  readonly dataset: { readonly section?: string };
  closest(selector: string): unknown;
}

/** id -> node map the spy observes; a category wins over a block carrying the same id. */
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

export function targetSection<T extends SpyNodeLike>(node: T): T {
  return (node.closest(".settings-category") as T | null) ?? node;
}

/** the nav's grey-out set - the nav itself never changes shape. */
export function matchingCategoryIds(
  nav: NavCategory[],
  input: ContentInput = {},
): Set<string> {
  const keep = keeper(input);
  const ids = new Set<string>();
  for (const category of nav) if (category.knobs.some(keep)) ids.add(category.id);
  return ids;
}


interface ClampGeometry {
  /** the section's top, in the scrollport's own scroll coordinates */
  top: number;
  height: number;
  viewport: number;
  content: number;
}

/** the `[min, max]` scrollTop window a SELECTED section allows: `min` puts the
    section's top at the scrollport's top edge (its sticky heading is the first child
    and pins there), `max` its bottom at the bottom edge. Never `max < min`. */
export function clampedRange(input: ClampGeometry): [number, number] {
  const viewport = Math.max(0, input.viewport);
  const limit = Math.max(0, input.content - viewport);
  const min = Math.min(Math.max(0, input.top), limit);
  const natural = input.top + input.height - viewport;
  const max = Math.max(min, Math.min(natural, limit));
  return [min, max];
}

/** the START follows its box both ways (max(now, min)); the END follows only when
    the pane was resting exactly on it - a pane parked inside is never moved. */

export interface ClampHold {
  at: number;
  max: number;
}

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

/** The clamp and the scroll-spy are suppressed for exactly ONE reason: a scroll the
    PANE issued itself (a jump, a deselect's return to top, a restore, a cleared query). */

export interface PaneScrollState {
  target: number | null;
  graceUntil: number;
  intent: boolean;
}

export function paneScrollSettled(state: PaneScrollState): boolean {
  return state.target === null;
}

/** the ONE scroll the clamp may not measure: the suppression lets a jump's smooth
    scroll finish. A filtered document puts the pane's old position past the section's
    end, so the first frame is often outside the live window - enforcing there cancels
    the jump. */
export function paneScrollOwns(state: PaneScrollState): boolean {
  return state.target !== null && state.intent;
}

/** this scroll event is the USER's: no pane scroll in flight, and the settle window
    (which swallows an animation's trailing repeats) is past. */
export function paneScrollIsUser(state: PaneScrollState, now: number): boolean {
  return state.target === null && now >= state.graceUntil;
}

export function paneScrollSettle(now: number, graceMs: number): PaneScrollState {
  return { target: null, graceUntil: now + graceMs, intent: false };
}

/** The remembered anchor is the USER's: a geometry-armed delivery resolves whatever id
    the new boxes put at the top edge and must not overwrite the anchor an explicit
    jump wrote, so the write is gated on ORDER, never on a flag, and once per scroll. */

export interface SpyAnchorTrace {
  /** when a scroll event was last judged the USER's (ms); 0 = never */
  userScrollAt: number;
  /** when the spy's boxes last moved with no scroll - a resize, a re-arm (ms); 0 = never */
  geometryAt: number;
  wroteAt: number;
}

export function spyAnchorMayWrite(trace: SpyAnchorTrace): boolean {
  return trace.userScrollAt > trace.geometryAt && trace.userScrollAt > trace.wroteAt;
}

/** The pane keeps ONE thing in the URL: `?settings=<section>` rides whatever path
    is current, so the query axis and the path scheme never fight. */

export const SETTINGS_SECTION_PARAM = "settings";

/** the section id a settings URL carries. Null for no parameter AND for a blank one:
    `?settings=` is a deselect that leaked into a bookmark, never a section named "". */
export function parseSettingsSection(href: string): string | null {
  const url = settingsUrl(href);
  const raw = url.searchParams.get(SETTINGS_SECTION_PARAM);
  if (raw === null) return null;
  const id = raw.trim();
  return id === "" ? null : id;
}

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

function settingsUrl(href: string): URL {
  try {
    return new URL(href);
  } catch {
    return new URL(href, "http://omega.invalid/");
  }
}
