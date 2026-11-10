// omega webui: the settings pane's CATEGORY NAVIGATION - the per-knob
// nesting DATA plus the pure builders the master-detail pane renders
// (settings tier 4, card t_af7e1150).
//
// Ruling 2: nesting is declared AS DATA per knob - a `subgroup` field on
// each knob - and the nav builds itself from that data. KNOB_SUBGROUPS is
// that declaration table: nothing here derives a subgroup from a config
// key's spelling, and no hand-authored tree exists. Adding a knob to a
// child means adding ONE line here; a knob with no line lands in the
// implicit "general" child (ruling 3).
//
// Why the table is client-side: the chat.* and dock.* knobs exist only in
// the pane's own definitions - the server has no schema row for them - so
// a wire field could not carry the card's own example (theme ->
// "appearance"). One table serves both origins because a knob key is
// unique across them.
//
// Two levels, one shape: a CATEGORY is a domain (provider, chat, ...), a
// SUBGROUP is the indented child under it. A knob with no declared
// subgroup lands in the implicit "general" child pinned to the TOP of its
// domain - nothing floats loose at domain level.
//
// Commonly Used is a VIRTUAL top category (ruling 4): it gathers curated
// knobs from every domain, renders FLAT (nesting across domains would
// mean nothing), and comes first because the pane's group list puts it
// first. A category with no knobs never enters the nav (ruling 14 - an
// unmounted module's domain is absent, not empty).
//
// Pure on purpose: no Vue, no fetch, no DOM. The unit suite pins the
// tree, the selection algebra and the content view; the component only
// paints what these functions return.

import type { SettingGroup, SettingKnob } from "../../composables/omega";

/** the implicit child every undeclared knob lands in (ruling 3) */
export const GENERAL_SUBGROUP = "general";
/** the virtual top category of curated knobs (ruling 4) */
export const COMMON_CATEGORY = "common";
/** client storage key for the remembered selection (ruling 13) */
export const NAV_STORAGE_KEY = "omega.settings-category";
/** what the content pane says while a live query finds nothing */
export const SEARCH_TITLE = "search results";

// --- the nesting data (ruling 2) -----------------------------------------

/** KNOB_SUBGROUPS: knob key -> subgroup id. THE declaration. */
export const KNOB_SUBGROUPS: Readonly<Record<string, string>> = {
  // provider - the endpoint, the models it serves, the named instances
  "provider.type": "connection",
  "provider.host": "connection",
  "provider.api_key": "connection",
  "provider.model_name": "models",
  "provider.vision_model": "models",
  "provider.title_model": "models",
  "provider.approvals_model": "models",
  "provider.compaction_model": "models",
  "provider.summarizer_model": "models",
  "provider.skills_model": "models",
  "provider.instances": "instances",
  // server
  "server.port": "network",
  "server.bind": "network",
  "server.run_timeout": "runs",
  "server.default_workspace": "workspace",
  // store
  "store.db_path": "database",
  // sessions
  "sessions.retention_days": "retention",
  "sessions.summarize_after_hours": "summaries",
  // compaction
  "compaction.enabled": "policy",
  "compaction.threshold": "policy",
  "compaction.context_window": "budget",
  "compaction.reserve_tokens": "budget",
  "compaction.max_tool_output": "budget",
  "compaction.keep_first": "window",
  "compaction.keep_last": "window",
  // runtime
  "system_prompt": "loop",
  "max_turns": "loop",
  "http_timeout": "network",
  // skills
  "skills.dir": "library",
  "skills.nudge_interval": "nudges",
  // memory
  "memory.enabled": "state",
  "memory.char_limit": "limits",
  "memory.user_char_limit": "limits",
  "memory.file": "files",
  "memory.user_file": "files",
  "memory.user_profile_enabled": "profile",
  // logging
  "logging.enabled": "state",
  "logging.file": "files",
  // approvals
  "approvals.mode": "policy",
  "approvals.smart_policy": "smart-policy",
  // chat (client state)
  "chat.color_mode": "appearance",
  "chat.theme": "appearance",
  "chat.show_thinking": "reasoning",
  "chat.appearance": "turn-display",
  "chat.tool_display": "turn-display",
  "chat.thinking_display": "turn-display",
  // dock (client state)
  "dock.bottom_width": "panel",
};

/** SUBGROUP_LABELS: the child's display name. An id with no line here
    renders as the id itself - a missing label is never a blank nav row. */
export const SUBGROUP_LABELS: Readonly<Record<string, string>> = {
  general: "general",
  connection: "connection",
  models: "models",
  instances: "instances",
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

// --- the tree -------------------------------------------------------------

export interface NavSubgroup {
  id: string;
  label: string;
  knobs: SettingKnob[];
}

export interface NavCategory {
  id: string;
  label: string;
  /** flat = no children (the virtual Commonly Used category) */
  flat: boolean;
  knobs: SettingKnob[];
  subgroups: NavSubgroup[];
}

/** withSubgroups stamps every knob with its declared subgroup, so the
    nav's `subgroup` field is plain data on the knob itself (ruling 2).
    Returns fresh groups - the caller's arrays are never mutated. */
export function withSubgroups(groups: SettingGroup[]): SettingGroup[] {
  return groups.map((group) => ({
    ...group,
    knobs: group.knobs.map((knob) => ({
      ...knob,
      subgroup: knob.subgroup ?? knobSubgroup(knob.key),
    })),
  }));
}

/** buildNav derives the two-level tree from the (already annotated)
    groups. Knob order inside a child is the wire order; child order is
    first appearance with "general" pinned first (ruling 3). Empty
    categories stay out (ruling 14). */
export function buildNav(groups: SettingGroup[]): NavCategory[] {
  const nav: NavCategory[] = [];
  for (const group of groups) {
    if (group.knobs.length === 0) continue;
    const flat = group.key === COMMON_CATEGORY;
    if (flat) {
      nav.push({
        id: group.key,
        label: group.label,
        flat: true,
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

// --- selection algebra ----------------------------------------------------

export interface Selection {
  category: string;
  subgroup: string | null;
}

export interface ResolvedSelection {
  category: NavCategory;
  subgroup: NavSubgroup | null;
}

/** the selection id the nav buttons carry and the pane remembers:
    "<category>" or "<category>/<subgroup>" */
export function selectionId(category: string, subgroup: string | null = null): string {
  return subgroup === null || subgroup === "" ? category : `${category}/${subgroup}`;
}

export function parseSelection(id: string): Selection {
  const cut = id.indexOf("/");
  if (cut < 0) return { category: id, subgroup: null };
  return { category: id.slice(0, cut), subgroup: id.slice(cut + 1) || null };
}

/** resolveSelection maps a persisted id onto the LIVE nav: a category or
    child that no longer exists (module unmounted, knob set changed) is
    not an error - it answers null and the caller falls back. A child id
    under a known category but an unknown child resolves to the category
    itself (never a blank pane). */
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

/** the category a fresh pane opens on: Commonly Used when the nav has it
    (the Main scope), else the first category (an agent scope, which
    carries the four overridable groups and nothing else) */
export function fallbackSelection(nav: NavCategory[]): string {
  const common = nav.find((c) => c.id === COMMON_CATEGORY);
  if (common) return common.id;
  return nav[0]?.id ?? "";
}

/** the remembered id when it still resolves, else the fallback. The
    answer is NORMALISED against the live nav: a remembered CHILD that
    this scope no longer carries resolves to its category, so the nav
    lights the parent instead of nothing at all (a stale id is never a
    blank pane, and the raw memory stays untouched in storage). */
export function effectiveSelection(nav: NavCategory[], remembered: string): string {
  const resolved = resolveSelection(nav, remembered);
  if (!resolved) return fallbackSelection(nav);
  return selectionId(resolved.category.id, resolved.subgroup?.id ?? null);
}

// --- the content pane -----------------------------------------------------

export interface ContentSection {
  id: string;
  label: string;
  /** render the label as a child heading (a multi-child category view) */
  heading: boolean;
  knobs: SettingKnob[];
}

export interface ContentView {
  title: string;
  sections: ContentSection[];
  count: number;
  searching: boolean;
}

export interface ContentInput {
  /** knob keys the live query matched - null/undefined means no query is
      live (an EMPTY set is a query that matched nothing: the difference
      decides between "everything" and "the empty state") */
  hits?: ReadonlySet<string> | null;
  /** ruling 8: keep only the knobs that differ from their base */
  modifiedOnly?: boolean;
  isModified?: (knob: SettingKnob) => boolean;
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

/** contentFor answers what the content pane renders for a selection.
    A live query is CROSS-CATEGORY (ruling 6): every category with a hit
    contributes a section, whatever the selection is. Without a query the
    selection decides - the whole category (its children as headed
    sections) or one child. */
export function contentFor(
  nav: NavCategory[],
  id: string,
  input: ContentInput = {},
): ContentView {
  const keep = keeper(input);
  if (input.hits != null) {
    const sections: ContentSection[] = [];
    let count = 0;
    for (const category of nav) {
      // the curated category is a VIEW of knobs other domains own: in
      // search mode it is skipped, so a hit renders once - under its
      // domain - while the nav keeps its shape and greys instead (ruling
      // 6: never hide, never list one knob twice)
      if (category.id === COMMON_CATEGORY) continue;
      const knobs = category.knobs.filter(keep);
      if (knobs.length === 0) continue;
      sections.push({ id: category.id, label: category.label, heading: true, knobs });
      count += knobs.length;
    }
    return { title: SEARCH_TITLE, sections, count, searching: true };
  }
  const resolved =
    resolveSelection(nav, id) ?? (nav[0] ? { category: nav[0], subgroup: null } : null);
  if (!resolved) return { title: "", sections: [], count: 0, searching: false };
  const { category, subgroup } = resolved;
  const sections: ContentSection[] = [];
  if (subgroup) {
    const knobs = subgroup.knobs.filter(keep);
    if (knobs.length > 0)
      sections.push({
        id: selectionId(category.id, subgroup.id),
        label: subgroup.label,
        heading: false,
        knobs,
      });
  } else if (category.flat || category.subgroups.length <= 1) {
    // a flat category (Commonly Used) or a domain whose every knob lands
    // in one child: one unheaded list - a heading that repeats the title
    // is noise
    const knobs = category.knobs.filter(keep);
    if (knobs.length > 0)
      sections.push({
        id: category.id,
        label: category.label,
        heading: false,
        knobs,
      });
  } else {
    for (const child of category.subgroups) {
      const knobs = child.knobs.filter(keep);
      if (knobs.length === 0) continue;
      sections.push({
        id: selectionId(category.id, child.id),
        label: child.label,
        heading: true,
        knobs,
      });
    }
  }
  const count = sections.reduce((n, section) => n + section.knobs.length, 0);
  return { title: category.label, sections, count, searching: false };
}

/** matchingCategoryIds is the nav's grey-out set (ruling 6): the
    categories that still carry a knob under the live query and the
    modified-only switch. The nav itself never changes shape. */
export function matchingCategoryIds(
  nav: NavCategory[],
  input: ContentInput = {},
): Set<string> {
  const keep = keeper(input);
  const ids = new Set<string>();
  for (const category of nav) if (category.knobs.some(keep)) ids.add(category.id);
  return ids;
}
