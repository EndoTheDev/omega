// omega webui: the settings pane's CATEGORY NAVIGATION - the per-knob
// nesting DATA plus the pure builders the ONE-DOCUMENT pane renders
// (card t_8b464090, which SUPERSEDES the master-detail model of
// t_af7e1150: the pane is one scrollable document of every category and
// the sidebar is a set of JUMP LINKS into it, not a selector).
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
// tree, the one-document builder, the anchor resolution and the grey-out
// set; the component only paints what these functions return.

import type { SettingGroup, SettingKnob } from "../../composables/omega";

/** the implicit child every undeclared knob lands in (ruling 3) */
export const GENERAL_SUBGROUP = "general";
/** the virtual top category of curated knobs (ruling 4) */
export const COMMON_CATEGORY = "common";
/**
 * card t_81d0901b: the MODULES category - the module lifecycle surface,
 * which is CONFIGURATION (toggles over persisted config), not content, so
 * it lives in this pane instead of the retired secondary-pane tab. Its
 * rows are NOT knobs: the category carries no SettingKnob, its body is the
 * ModulesSection component, and `count` reports the module rows it renders
 * so the nav shows a real number instead of a knob count.
 */
export const MODULES_CATEGORY = "modules";
/** client storage key for the remembered LAST ANCHOR (ruling 13 survives) */
export const NAV_STORAGE_KEY = "omega.settings-category";

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
  /** card t_81d0901b: the count a knob-less category reports (its rows).
      Absent on every knob category - the nav then counts `knobs`. */
  count?: number;
}

/** the number the nav prints for a category: its declared row count, or
    the knob count every other category gets (t_81d0901b) */
export function categoryCount(cat: NavCategory): number {
  return cat.count ?? cat.knobs.length;
}

/**
 * modulesNavCategory is the MODULES entry the settings pane PREPENDS to
 * the nav (card t_81d0901b): flat, knob-less, titled "modules", carrying
 * the live module-row count. Its document section is rendered by the
 * pane's own ModulesSection block, so documentFor (which walks knobs)
 * never emits it - the nav entry is what makes it a jump target.
 */
export function modulesNavCategory(count = 0): NavCategory {
  return {
    id: MODULES_CATEGORY,
    label: "modules",
    flat: true,
    knobs: [],
    subgroups: [],
    count,
  };
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

/** anchorSelection resolves the REMEMBERED value (ruling 13's client
    storage) into the id the pane opens scrolled to. There is no fallback
    selection any more: a pane opens with NOTHING selected (card
    t_8b464090), so this answers null for "no anchor" - an empty stored
    value (a DESELECT persists as nothing at all) and a stale category
    both open at the top instead of resurrecting a selection the user
    cleared. A remembered CHILD the current nav no longer carries
    normalises to its category: a stale id is never a broken restore. */
export function anchorSelection(
  nav: NavCategory[],
  remembered: string | null,
): string | null {
  if (!remembered) return null;
  const resolved = resolveSelection(nav, remembered);
  if (!resolved) return null;
  return selectionId(resolved.category.id, resolved.subgroup?.id ?? null);
}

/** expandedCategoryIds is the nav's OPEN branches - DERIVED, never
    remembered (card t_8b464090, superseding t_5656d52f's per-parent
    toggle): the one branch the selection lives in, or - under a live
    query - every parent carrying a hit (`queryOpen`, so hits stay
    reachable whatever is selected). Nothing selected and no query means
    nothing is open. There is no collapse set anywhere: "what is open" and
    "where I am" cannot disagree. */
export function expandedCategoryIds(
  selection: string | null,
  queryOpen: ReadonlySet<string> | null = null,
): Set<string> {
  if (queryOpen !== null) return new Set(queryOpen);
  if (!selection) return new Set<string>();
  return new Set<string>([parseSelection(selection).category]);
}

// --- the ONE document -----------------------------------------------------

export interface DocBlock {
  /** the anchor id this block answers to: "<cat>" or "<cat>/<sub>" */
  id: string;
  label: string;
  /** render the label as a child heading (a multi-child category) */
  heading: boolean;
  knobs: SettingKnob[];
}

export interface DocSection {
  /** the category id - the section's jump target and its data-category */
  id: string;
  label: string;
  /** knobs this section renders right now (filtered) */
  count: number;
  blocks: DocBlock[];
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

/** documentFor is the pane's whole body: EVERY category, in nav order, as
    a headed section - the selection never filters it (card t_8b464090:
    "the right pane ALWAYS shows every knob and its description"). A
    multi-child domain renders its children as headed blocks; a flat or
    one-child domain renders one unheaded list, because a heading that
    repeats the section title is noise.

    Search filters the document IN PLACE - the old cross-category hits
    view is gone, the filtered document IS the hits view - and a live
    query still skips the curated category: its knobs belong to other
    domains, so a hit renders ONCE, under the domain that owns it, while
    the nav keeps the curated entry and greys or lights it (ruling 6).
    "modified only" narrows the SAME document, whole - never one category
    at a time. */
export function documentFor(
  nav: NavCategory[],
  input: ContentInput = {},
): DocSection[] {
  const keep = keeper(input);
  const searching = input.hits != null;
  const sections: DocSection[] = [];
  for (const category of nav) {
    if (searching && category.id === COMMON_CATEGORY) continue;
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
    sections.push({ id: category.id, label: category.label, count, blocks });
  }
  return sections;
}

/** spyTargetIds is the scroll-spy's document order: each section followed
    by its blocks - a block that answers to the section's OWN id (the
    unheaded list of a flat or one-child domain) is not listed twice. */
export function spyTargetIds(sections: DocSection[]): string[] {
  return sections.flatMap((section) => [
    section.id,
    ...section.blocks
      .map((block) => block.id)
      .filter((id) => id !== section.id),
  ]);
}

/** The node surface the spy/clamp target resolution needs: a node's
    data-section id and its DOM ancestry, as a structural subset of
    Element - so the node suite can pin resolution against a hand-built
    tree (the suite has no DOM). */
export interface SpyNodeLike {
  readonly dataset: { readonly section?: string };
  closest(selector: string): unknown;
}

/** buildSpyTargets is the pane's id -> node map (the spy observes these
    nodes, the jump and the clamp measure them). SECTIONS answer FIRST: a
    real `.settings-category` owns its category id. A `.settings-block`
    joins only under an id NO section claims ("category/subgroup") - a flat
    or one-child category renders one unheaded block whose block id IS the
    section id, so the document carries that id twice, and a map built in
    plain DOM order let the inner block win (card t_c4b42357): the jump then
    put the block's top at y=0, i.e. hidden behind the section's own opaque
    sticky heading, and the clamp measured the block's box instead of the
    section's. */
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

/** targetSection is the box a scroll anchor lands IN: the nearest category
    section. For a SECTION target that is the node itself - so a section
    jump subtracts no heading and the section's own first row stays under
    its pinned heading (card t_c4b42357); only a BLOCK jump subtracts the
    heading this resolves to. */
export function targetSection<T extends SpyNodeLike>(node: T): T {
  return (node.closest(".settings-category") as T | null) ?? node;
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

// --- the selected-section scroll clamp (card t_a6b794b5) ------------------

export interface ClampGeometry {
  /** the section's top, in the scrollport's own scroll coordinates */
  top: number;
  /** the section's rendered height */
  height: number;
  /** the scrollport's client height (the pane's visible box) */
  viewport: number;
  /** the scrollport's scrollHeight - the document it scrolls through */
  content: number;
}

/** clampedRange answers the `[min, max]` scrollTop window a SELECTED section
    allows. Selection never filters the document (the one-document ruling),
    so the pane still scrolls - the clamp only bounds WHERE it may rest:
    `min` puts the section's top at the scrollport's top edge (its sticky
    heading is the section's first child, so the heading pins exactly
    there), `max` puts the section's bottom at the bottom edge.

    A section SHORTER than the viewport has no room to travel - `max` would
    land above `min` - so its range collapses to `[min, min]`: while it is
    selected the pane does not scroll at all. That is the honest answer,
    not a bug (card t_a6b794b5 step 4), and the clamp can never produce
    `max < min`.

    Both ends are capped by the document's OWN scroll limit, so a section
    at the very end (whose natural `max` overshoots the content) still
    answers a REACHABLE window instead of a dead one. */
export function clampedRange(input: ClampGeometry): [number, number] {
  const viewport = Math.max(0, input.viewport);
  // the last legal scroll position of the document itself
  const limit = Math.max(0, input.content - viewport);
  // the section's top, never above the document's start nor below its end
  const min = Math.min(Math.max(0, input.top), limit);
  // its bottom at the pane's bottom edge, inside the same document bounds
  const natural = input.top + input.height - viewport;
  const max = Math.max(min, Math.min(natural, limit));
  return [min, max];
}

// --- the pane's OWN scrolls (card t_e7404b84) -----------------------------
//
// The clamp and the scroll-spy are suppressed for exactly ONE reason: a
// scroll the PANE issued itself (a jump, a deselect's return to top, a
// restore, a cleared query's give-back). Those converge on a KNOWN landing
// position, which is what makes the rule decidable: while the pane is
// still travelling to (or has just landed on) that position the event is
// the pane's, and every OTHER scroll event is the user's hand - whatever
// started it.
//
// This REPLACES the intent-event gate (wheel/touchstart/pointerdown/
// keydown). A scrollbar drag, an assistive-technology scroll and a
// programmatic `scrollTop` dispatch none of those, so gating the release
// on them left every such scroll unclamped AND left the spy frozen: the
// nightly landed the pane on the document's end (5709) where the selected
// section's bottom was 2509.

export interface PaneScrollState {
  /** the scrollTop the pane asked for; null when it is not scrolling */
  target: number | null;
  /** the settle window after that scroll ENDED (absolute ms) */
  graceUntil: number;
}

/** paneScrollSettled: the pane's own scroll is over - its animation landed,
    or something else overrode it. The CLAMP may hold the pane to its
    section again from here. */
export function paneScrollSettled(state: PaneScrollState): boolean {
  return state.target === null;
}

/** paneScrollIsUser: this scroll event is the USER's - no pane scroll in
    flight, and the settle window (which swallows an animation's trailing
    repeats, so a deselect cannot re-select what its return-to-top just
    passed) is over. The clamp runs and the suppression ends. */
export function paneScrollIsUser(state: PaneScrollState, now: number): boolean {
  return state.target === null && now >= state.graceUntil;
}

/** paneScrollSettle: the pane's own scroll ended at `now` - open the settle
    window the next scroll event is judged against. */
export function paneScrollSettle(now: number, graceMs: number): PaneScrollState {
  return { target: null, graceUntil: now + graceMs };
}

// --- the anchor is the USER's (card t_63c699ce) ---------------------------
//
// The remembered anchor means "the section the user actually scrolled to".
// The spy decides from a band of intersections, and an intersection moves
// for reasons that are NOT a scroll: a viewport resize, a reflow, the clamp's
// own re-snap. A delivery like that resolves whatever id the NEW boxes put at
// the top edge, and the nightly caught it overwriting the anchor an explicit
// jump had just written - a drawer jump to "compaction" at 340px, a widen to
// 1280px, and the stored anchor read back as the neighbouring section.
//
// So the write is gated on ORDER, never on a flag someone must remember to
// clear: a delivery may write only when the user's scroll is newer than the
// last geometry change, and only once per scroll. A scroll the pane issued
// itself (a jump, a re-clamp, a restore) never stamps `userScrollAt`, so the
// ONE suppression of card t_e7404b84 keeps covering the anchor too.

export interface SpyAnchorTrace {
  /** when a scroll event was last judged the USER's (ms); 0 = never */
  userScrollAt: number;
  /** when the spy's boxes last moved with no scroll - a resize, a re-arm
      (ms); 0 = never */
  geometryAt: number;
  /** the user scroll already written (ms) - one write per scroll */
  wroteAt: number;
}

/** spyAnchorMayWrite: this spy decision may persist the remembered anchor.
    False for every delivery a geometry change armed (the resize that red the
    nightly), false for a scroll the pane issued itself, and false for a
    second write of a scroll already written. */
export function spyAnchorMayWrite(trace: SpyAnchorTrace): boolean {
  return trace.userScrollAt > trace.geometryAt && trace.userScrollAt > trace.wroteAt;
}

