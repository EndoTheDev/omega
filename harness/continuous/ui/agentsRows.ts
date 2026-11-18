// omega webui: the agents surface model.

// RECORD: - continuousRows.ts types fold in here (the merged agent row carries the.
import { rowStatus, type OmegaContinuousRow } from "./continuousRows.ts";

// The 0.17.3 make-over made the row SECRET-FREE (agent/configedit.go law 2): an agent
// names a provider INSTANCE, never a type/host/key.
export interface OmegaAgent {
  name: string;
  dir: string;
  description?: string;
  provider_instance?: string;
  model_name?: string;
  toggles?: readonly string[];
  skills_allow?: readonly string[];
  inherits?: readonly string[];
  skill_count: number;
  skills?: readonly string[];
  memory_size: number;
  soul_size?: number;
  session_count: number;
  last_activity?: string;
  canonical_session_id?: string;
  last_report?: string;
  last_report_at?: string;
  continuous?: boolean;
}

export interface OmegaAgentSession {
  id: string;
  label?: string;
  agent?: string;
  created_at?: string;
  updated_at?: string;
  message_count?: number;
}

/**
 * row voice for the list: the model is the headline (the knob agents are created for);
 * the default agent carries "(home config)" and an empty model_name never renders as a
 * phantom empty row.
 */
export function agentModelText(a: OmegaAgent): string {
  return a.model_name || (a.name === "default" ? "(home config)" : "");
}

/** honest memory display: 0 = no memory written yet, never "0b". */
export function agentMemoryText(a: OmegaAgent): string {
  if (!a.memory_size) return "-";
  if (a.memory_size < 1024) return `${a.memory_size}b`;
  return `${Math.round(a.memory_size / 1024)}kb`;
}

/** description placeholder: an empty description is honest empty. */
export function agentDescriptionText(a: OmegaAgent): string {
  return a.description || "";
}

/** skills summary: the count is the primary signal, names when few. */
export function agentSkillsText(a: OmegaAgent): string {
  const names = a.skills ?? [];
  return names.length ? names.join(", ") : "";
}

/**
 * the panel's empty state: default exists by definition, so a bare default-only home
 * still renders one row, never an empty table.
 */
export function agentsListIsEmpty(profiles: readonly OmegaAgent[]): boolean {
  return profiles.length === 0;
}

/**
 * session label voice for the agent's session list (same rule the sessions sidebar
 * uses.
 */
export function agentSessionTitle(s: OmegaAgentSession): string {
  return s.label || s.id.slice(0, 12) + "...";
}

/** the provider voice (0.17.3 item 4). */
export function agentProviderText(a: OmegaAgent): string {
  if (a.provider_instance) return a.provider_instance;
  return a.name === "default" ? "(home config)" : "(main's provider)";
}

/**
 * agentDetailLines builds the RegistryList row's expandable detail: the config summary,
 * one line per fact, the ordering the old detail block used (model, provider, dir,
 * skills, memory, last activity).
 */
export function agentDetailLines(a: OmegaAgent): string[] {
  return [
    `model: ${agentModelText(a) || "-"}`,
    `provider: ${agentProviderText(a)}`,
    `dir: ${a.dir}`,
    `skills: ${agentSkillsText(a) || "none"}`,
    `memory: ${agentMemoryText(a)}`,
    `last activity: ${a.last_activity || "-"}`,
  ];
}

/** agentStatusFor: the subtitle line's live dot + short word. */
export function agentStatusFor(
  c: OmegaContinuousRow | null,
): { on: boolean; label: string } | undefined {
  if (!c) return undefined;
  const label = rowStatus(c);
  return { on: label !== "sleeping", label };
}

/**
 * the default-agent law: the default agent cannot be disabled EVER - the fold is
 * filtered of it on display (a stale entry an older build persisted must not render a
 * disabled row) and the row carries the locked pill.
 */
export const DefaultAgentName = "default";

/**
 * agentIsMain: the roster includes the main agent like every other home, and the row.
 */
export function agentIsMain(a: Pick<OmegaAgent, "name">): boolean {
  return a.name === DefaultAgentName;
}

/**
 * agentSessionCountText: the roster's session count column.
 * Zero is honest ("no sessions"), never "0 sessions" dressed up as activity.
 */
export function agentSessionCountText(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "no sessions";
  return n === 1 ? "1 session" : `${n} sessions`;
}

/**
 * agentActivityText: the last-activity column as a relative age.
 * Empty for a missing/unparseable timestamp (honest absence beats an invented row
 * voice); a future stamp (clock skew) reads "just now", never a negative age.
 */
export function agentActivityText(iso?: string, now: Date = new Date()): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const secs = Math.floor((now.getTime() - t) / 1000);
  if (secs < 60) return "just now";
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return iso.slice(0, 10);
}

/** agentRosterSubtitle: the roster row's subtitle line - the facts the tab is FOR. */
export function agentRosterSubtitle(a: OmegaAgent, now: Date = new Date()): string {
  const parts: string[] = [];
  if (a.description) parts.push(a.description);
  const model = agentModelText(a);
  if (model) parts.push(model);
  parts.push(agentSessionCountText(a.session_count));
  const activity = agentActivityText(a.last_activity, now);
  if (activity) parts.push(`last ${activity}`);
  return parts.join(" · ");
}

/**
 * agentPreview: the roster row's one-line preview - the newest thing the agent said in
 * its canonical chat, whitespace collapsed so it can never be two lines, plus the age
 * of that reply.
 * No reply yet = no preview line (never a placeholder).
 */
export function agentPreview(
  a: OmegaAgent,
  now: Date = new Date(),
): { text: string; at: string } {
  const text = (a.last_report ?? "").replace(/\s+/g, " ").trim();
  if (!text) return { text: "", at: "" };
  return { text, at: agentActivityText(a.last_report_at, now) };
}

/**
 * mergedRowDetail: the merged agent row's detail.
 * A null roster row (no continuous home) renders the config summary alone - never a
 * phantom "sleeping" for an agent with no wake schedule.
 */
export function mergedRowDetail(
  a: OmegaAgent,
  c: OmegaContinuousRow | null,
): string[] {
  if (!c) return agentDetailLines(a);
  return [...agentDetailLines(a), `wake: ${rowStatus(c)}`];
}

/** the join key: the roster and the config listing share the agent name (folder. */
export function continuousIndex(
  roster: readonly OmegaContinuousRow[],
): Record<string, OmegaContinuousRow> {
  const out: Record<string, OmegaContinuousRow> = {};
  for (const r of roster) out[r.name] = r;
  return out;
}

// -- the create door (0.17.3 rulings 1 + 6) -------------------------
//
// ONE "+" opens ONE form. The KIND is a choice inside that form, and the
// kind decides the wire - that routing is pure, so the suite pins it
// without a DOM:
//
// scaffold POST /agents an empty, always-off home
// continuous POST /continuous home + canonical chat + wake schedule
// clone POST /agents {inherit_from}: a MATERIALIZED FULL
// COPY at birth
//                                   path is clone + delete, never a link)
//
// The "@" duplicate door and its near-identical second form died here.

export const AGENT_KINDS = ["scaffold", "continuous", "clone"] as const;
export type AgentKind = (typeof AGENT_KINDS)[number];

/**
 * AgentCreateDraft is the ONE form's shape.
 * The routing below reads only the fields its kind owns - an unused field is never
 * sent.
 */
export interface AgentCreateDraft {
  name: string;
  kind: AgentKind;
  description: string;
  directive: string;
  schedule: string;
  inherit_from: string;
}

export function emptyCreateDraft(): AgentCreateDraft {
  return {
    name: "",
    kind: "scaffold",
    description: "",
    directive: "",
    schedule: "",
    inherit_from: "",
  };
}

/**
 * kindNeedsContinuous: only the continuous kind needs the continuous capsule mounted.
 */
export function kindNeedsContinuous(kind: AgentKind): boolean {
  return kind === "continuous";
}

/**
 * createRequest is the ONE door's routing.
 * A clone with no source (or any kind with no name) is the form's own incomplete state,
 * reported as null so the caller refuses instead of inventing an endpoint.
 */
export function createRequest(
  d: AgentCreateDraft,
): { path: string; body: Record<string, string> } | null {
  const name = d.name.trim();
  if (!name) return null;
  if (d.kind === "continuous") {
    return {
      path: "/continuous",
      body: { name, directive: d.directive, schedule: d.schedule.trim() },
    };
  }
  if (d.kind === "clone") {
    const from = d.inherit_from.trim();
    if (!from) return null;
    return {
      path: "/agents",
      body: { name, description: d.description, inherit_from: from },
    };
  }
  return { path: "/agents", body: { name, description: d.description } };
}

/**
 * cloneSources: every OTHER agent home is a legal clone source (the main agent included
 * - "inherit from agent" must be able to copy the home root).
 */
export function cloneSources(
  profiles: readonly OmegaAgent[],
  draftName: string,
): OmegaAgent[] {
  const name = draftName.trim();
  return profiles.filter((p) => p.name !== name);
}

/** agentKindLabel: the kind's voice in the picker. */
export function agentKindLabel(kind: AgentKind): string {
  switch (kind) {
    case "continuous":
      return "continuous agent (directive + wake schedule)";
    case "clone":
      return "inherit from an agent (full copy at birth)";
    default:
      return "plain scaffold (empty, always off)";
  }
}

/**
 * agentKindOption: the CLOSED option's line in the create form's kind select.
 * The parenthetical consequence is never lost: the kind's note under the grid speaks it
 * (the scaffold's empty home, the clone's materialized copy) and the continuous kind's
 * own directive/schedule fields appear with it.
 */
export function agentKindOption(kind: AgentKind): string {
  return agentKindLabel(kind).split(" (")[0];
}

// -- delete = delete the HOME --------------------

/**
 * agentDeleteConfirm names what the trigger will take.
 * The count is the ROW's session_count - the same number the delete receipt answers -
 * so the dialog can never promise a different number than the delete delivers.
 */
export function agentDeleteConfirm(name: string, sessions: number): string {
  if (!Number.isFinite(sessions) || sessions < 0) {
    return `delete ${name}? its home, including every session - the session count is unknown`;
  }
  const voice =
    sessions === 0 ? "no sessions" : sessions === 1 ? "1 session" : `${sessions} sessions`;
  return `delete ${name}? its home and ${voice} go with it - this cannot be undone`;
}

/**
 * agentDeleteLabel: the trigger's own word, in the destructive voice - never a bare.
 */
export function agentDeleteLabel(name: string): string {
  return `delete ${name}'s home - sessions included`;
}

// -- the edit write (the draft diff) -------------------------------- The panel's. The
// diff is pure and lives here because it has one sharp edge the DOM hides: a `<input
// type="number">` under v-model hands back a NUMBER, not the string the draft declares
// - every read goes through String so a typed 7 cannot throw on.trim at submit time.

export const AGENT_INHERIT_KINDS = ["skills", "soul", "user", "memory"] as const;
export type AgentInheritKind = (typeof AGENT_INHERIT_KINDS)[number];

/** AgentEditDraft is the panel's edit form shape. */
export interface AgentEditDraft {
  description: string;
  system_prompt: string;
  provider_instance: string;
  model_name: string;
  max_turns: string | number;
  toggles: readonly string[];
  skills_allow: readonly string[];
  inherit: Record<AgentInheritKind, boolean>;
}

/** AgentEditTarget is the saved truth the draft diffs against. */
export interface AgentEditTarget {
  description?: string;
  toggles?: readonly string[];
  skills_allow?: readonly string[];
  inherits?: readonly string[];
}

function sameSet(a: readonly string[], b: readonly string[]): boolean {
  const left = [...a].sort();
  const right = [...b].sort();
  return left.length === right.length && left.every((v, i) => v === right[i]);
}

/**
 * editRequest builds the PUT body for one draft - nil when nothing changed (the panel's
 * "no changes" beat, never an empty write).
 */
export function editRequest(
  d: AgentEditDraft,
  saved: AgentEditTarget,
  chips: readonly FieldChip[],
): Record<string, unknown> | null {
  const seed = (key: string): string => chipOf(chips, key)?.value ?? "";
  const body: Record<string, unknown> = {};
  const unset: string[] = [];
  if (d.description !== (saved.description ?? "")) body.description = d.description.trim();
  if (d.system_prompt !== seed("system_prompt")) body.system_prompt = d.system_prompt.trim();
  if (d.provider_instance !== seed("provider.instance")) {
    body.provider_instance = d.provider_instance.trim();
  }
  if (d.model_name !== seed("provider.model_name")) body.model_name = d.model_name.trim();
  const turns = String(d.max_turns ?? "").trim();
  if (turns !== seed("max_turns")) {
    if (turns === "") unset.push("max_turns");
    else body.max_turns = Number(turns);
  }
  if (!sameSet(d.toggles, saved.toggles ?? [])) body.toggles = [...d.toggles].sort();
  if (!sameSet(d.skills_allow, saved.skills_allow ?? [])) {
    body.skills_allow = [...d.skills_allow].sort();
  }
  const inheritChanged = AGENT_INHERIT_KINDS.some(
    (kind) => d.inherit[kind] !== (saved.inherits ?? []).includes(kind),
  );
  if (inheritChanged) body.inherit = { ...d.inherit };
  if (unset.length) body.unset = unset;
  return Object.keys(body).length ? body : null;
}

// -- the inheritance chips ------------------------ ONE state machine, both surfaces.
// The panel never re-parses yaml: one read, two surfaces.

/** ScopeKnob is one knob inside an agent scope (core.SettingsScopeKnob). */
export interface ScopeKnob {
  key: string;
  value: unknown;
  overridden: boolean;
}

export interface ScopeGroup {
  key: string;
  overridden: boolean;
  knobs: ScopeKnob[];
}

export interface SettingsScope {
  name: string;
  groups: ScopeGroup[];
  error?: string;
}

/** scopeIndex flattens one agent's scope into knob-by-key. */
export function scopeIndex(scope?: SettingsScope | null): Record<string, ScopeKnob> {
  const out: Record<string, ScopeKnob> = {};
  for (const g of scope?.groups ?? []) {
    for (const k of g.knobs ?? []) out[k.key] = k;
  }
  return out;
}

/** FieldChip is one inheritable knob's state on the panel. */
export interface FieldChip {
  /**
   * the dotted layer key - the SAME spelling the write lane's `unset` vocabulary takes,
   * so the chip's reset button and the write path cannot drift
   */
  key: string;
  label: string;
  inherited: boolean;
  value: string;
  chip: string;
  note: string;
}

/**
 * the knobs a secondary agent INHERITS, in the panel's render order.
 * Toggles and skills are not here either: the always-off law makes them the agent's own
 * set, never main's (rulings 4 and 5).
 */
export const INHERITABLE_FIELDS: ReadonlyArray<{ key: string; label: string }> = [
  { key: "system_prompt", label: "system prompt" },
  { key: "provider.instance", label: "provider" },
  { key: "provider.model_name", label: "model" },
  { key: "max_turns", label: "max turns" },
];

/**
 * INHERIT_SOURCE is the chip's source word.
 * Main is the only link an agent has: an agent layers over the home config, never over
 * another agent ("inherit from agent" at CREATE is a copy, not a link).
 */
export const INHERIT_SOURCE = "main";

function knobText(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (Array.isArray(v)) return v.join(", ");
  return String(v);
}

/** fieldChips resolves the inheritable knobs for one row. */
export function fieldChips(scope?: SettingsScope | null): FieldChip[] {
  const idx = scopeIndex(scope);
  return INHERITABLE_FIELDS.map(({ key, label }) => {
    const knob = idx[key];
    const inherited = !knob?.overridden;
    const value = knobText(knob?.value);
    return {
      key,
      label,
      inherited,
      value,
      chip: inherited ? `inherited from ${INHERIT_SOURCE}` : "",
      note: inherited
        ? value
          ? `${label} inherited from ${INHERIT_SOURCE} (${value})`
          : `${label} inherited from ${INHERIT_SOURCE}`
        : "",
    };
  });
}

/**
 * chipOf looks one knob up in a resolved list (the panel keys its template by dotted.
 */
export function chipOf(chips: readonly FieldChip[], key: string): FieldChip | undefined {
  return chips.find((c) => c.key === key);
}


/**
 * agentTogglesNote: the toggles list is the ENABLED set, so an empty list is a real
 * answer - "nothing enabled", never "inherits main's features" (the ALWAYS OFF law).
 */
export function agentTogglesNote(toggles: readonly string[]): string {
  return toggles.length
    ? `${toggles.length} enabled - an agent only runs what it names here`
    : "nothing enabled - an agent never inherits main's feature set";
}

/**
 * agentSkillsNote: the allowlist's own voice.
 * Empty means NO skills; it is never a silent inheritance of main's registry.
 */
export function agentSkillsNote(allow: readonly string[]): string {
  return allow.length
    ? `${allow.length} allowed - the explicit allowlist that runs`
    : "no skills - an empty allowlist means none, never main's registry";
}

/**
 * lockedAgentModules mirrors agent.AlwaysOnAgentModules: the pair is not in the
 * toggleable tier at all, so the panel must not offer it.
 */
export const LOCKED_AGENT_MODULES: readonly string[] = ["agent-loop", "provider"];

/**
 * toggleOptions: the capsule names an agent may switch on.
 * The subset law starts here: a module the host has off is not offered, so the operator
 * never meets a refusal they could have avoided.
 */
export function toggleOptions(
  modules: ReadonlyArray<{ name: string; state?: string; toggleable?: boolean }>,
): string[] {
  return modules
    .filter((m) => m.toggleable !== false && (m.state ?? "mounted") === "mounted")
    .filter((m) => !LOCKED_AGENT_MODULES.includes(m.name))
    .map((m) => m.name);
}

/**
 * toggleChoices: the checkbox set for ONE agent.
 * The union matters: a capsule the host switched off AFTER the agent enabled it is not
 * offered, and hiding it would leave an invisible enabled entry that makes the next
 * toggle save a 400 (the subset law refuses the name the operator cannot see).
 * Shown dim with a reason instead, so unchecking it is a deliberate act, never a silent
 * drop.
 */
export function toggleChoices(offered: readonly string[], own: readonly string[]): string[] {
  return [...new Set([...offered, ...own])].sort();
}

/**
 * skillOptions: the allowlist's checkbox set is the agent's VISIBLE registry
 * (row.skills) - the same list the backend validates against, so a name offered here
 * can never be refused as unknown.
 */
export function skillOptions(a: Pick<OmegaAgent, "skills">): string[] {
  return [...(a.skills ?? [])];
}