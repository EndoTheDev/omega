import { rowStatus, type OmegaContinuousRow } from "./continuousRows.ts";

// the row is SECRET-FREE (agent/configedit.go): an agent names a provider INSTANCE,
// never a type, host or key.
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

/** the model is the row's headline (the knob agents are created for); the default agent carries "(home config)" so an empty model_name never renders blank. */
export function agentModelText(a: OmegaAgent): string {
  return a.model_name || (a.name === "default" ? "(home config)" : "");
}

/** honest memory display: 0 = no memory written yet, never "0b". */
export function agentMemoryText(a: OmegaAgent): string {
  if (!a.memory_size) return "-";
  if (a.memory_size < 1024) return `${a.memory_size}b`;
  return `${Math.round(a.memory_size / 1024)}kb`;
}

export function agentDescriptionText(a: OmegaAgent): string {
  return a.description || "";
}

export function agentSkillsText(a: OmegaAgent): string {
  const names = a.skills ?? [];
  return names.length ? names.join(", ") : "";
}

export function agentsListIsEmpty(profiles: readonly OmegaAgent[]): boolean {
  return profiles.length === 0;
}

/** the same label rule the sessions sidebar uses: label, else a short id. */
export function agentSessionTitle(s: OmegaAgentSession): string {
  return s.label || s.id.slice(0, 12) + "...";
}

export function agentProviderText(a: OmegaAgent): string {
  if (a.provider_instance) return a.provider_instance;
  return a.name === "default" ? "(home config)" : "(main's provider)";
}

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

/** no continuous row = no status; the live dot is off only for "sleeping" (rowStatus owns the word). */
export function agentStatusFor(
  c: OmegaContinuousRow | null,
): { on: boolean; label: string } | undefined {
  if (!c) return undefined;
  const label = rowStatus(c);
  return { on: label !== "sleeping", label };
}

/** the default agent can never be disabled: the fold is filtered of it on display, so a stale persisted entry never renders. */
export const DefaultAgentName = "default";

export function agentIsMain(a: Pick<OmegaAgent, "name">): boolean {
  return a.name === DefaultAgentName;
}

/** zero is honest ("no sessions"), never "0 sessions" dressed up as activity. */
export function agentSessionCountText(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "no sessions";
  return n === 1 ? "1 session" : `${n} sessions`;
}

/** empty for a missing/unparseable stamp; a future stamp (clock skew) reads "just now", never a negative age. */
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

/** whitespace collapsed so the preview can never be two lines; no reply means no preview, never a placeholder. */
export function agentPreview(
  a: OmegaAgent,
  now: Date = new Date(),
): { text: string; at: string } {
  const text = (a.last_report ?? "").replace(/\s+/g, " ").trim();
  if (!text) return { text: "", at: "" };
  return { text, at: agentActivityText(a.last_report_at, now) };
}

/** a null roster row renders the config summary alone - never a phantom "sleeping" for an agent with no wake schedule. */
export function mergedRowDetail(
  a: OmegaAgent,
  c: OmegaContinuousRow | null,
): string[] {
  if (!c) return agentDetailLines(a);
  return [...agentDetailLines(a), `wake: ${rowStatus(c)}`];
}

/** the join key: the roster and the config listing share the agent name (its folder). */
export function continuousIndex(
  roster: readonly OmegaContinuousRow[],
): Record<string, OmegaContinuousRow> {
  const out: Record<string, OmegaContinuousRow> = {};
  for (const r of roster) out[r.name] = r;
  return out;
}

// the create door: ONE "+" opens ONE form, and the KIND decides the wire (pure, so the
// suite pins it without a DOM):
//   scaffold   POST /agents - an empty, always-off home
//   continuous POST /continuous - home + canonical chat + wake schedule
//   clone      POST /agents {inherit_from} - a MATERIALIZED FULL COPY at birth (a path
//              is clone + delete, never a link)

export const AGENT_KINDS = ["scaffold", "continuous", "clone"] as const;
export type AgentKind = (typeof AGENT_KINDS)[number];

/** the routing reads only the fields its kind owns - an unused field is never sent. */
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

export function kindNeedsContinuous(kind: AgentKind): boolean {
  return kind === "continuous";
}

/** a clone with no source, or any kind with no name, is null - the caller refuses instead of inventing an endpoint. */
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

/** every OTHER agent home is a legal clone source, main included ("inherit from agent" must copy the home root). */
export function cloneSources(
  profiles: readonly OmegaAgent[],
  draftName: string,
): OmegaAgent[] {
  const name = draftName.trim();
  return profiles.filter((p) => p.name !== name);
}

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

/** the parenthetical consequence is never lost: the kind's note under the grid speaks it. */
export function agentKindOption(kind: AgentKind): string {
  return agentKindLabel(kind).split(" (")[0];
}

// delete = delete the HOME

/** the count is the ROW's session_count, the same number the delete record answers - the dialog can never promise more. */
export function agentDeleteConfirm(name: string, sessions: number): string {
  if (!Number.isFinite(sessions) || sessions < 0) {
    return `delete ${name}? its home, including every session - the session count is unknown`;
  }
  const voice =
    sessions === 0 ? "no sessions" : sessions === 1 ? "1 session" : `${sessions} sessions`;
  return `delete ${name}? its home and ${voice} go with it - this cannot be undone`;
}

export function agentDeleteLabel(name: string): string {
  return `delete ${name}'s home - sessions included`;
}

// the draft diff. One sharp edge the DOM hides: a `<input type="number">` under v-model
// hands back a NUMBER, not the string the draft declares, so every read goes through
// String - a typed 7 must not throw on .trim at submit time.

export const AGENT_INHERIT_KINDS = ["skills", "soul", "user", "memory"] as const;
export type AgentInheritKind = (typeof AGENT_INHERIT_KINDS)[number];

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

/** null when nothing changed - the panel's "no changes" beat, never an empty write. */
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

// the panel never re-parses yaml: one settings read feeds both surfaces.

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

export function scopeIndex(scope?: SettingsScope | null): Record<string, ScopeKnob> {
  const out: Record<string, ScopeKnob> = {};
  for (const g of scope?.groups ?? []) {
    for (const k of g.knobs ?? []) out[k.key] = k;
  }
  return out;
}

export interface FieldChip {
  /** the dotted layer key - the same spelling the write lane's unset vocabulary takes, so reset and write cannot drift. */
  key: string;
  label: string;
  inherited: boolean;
  value: string;
  chip: string;
  note: string;
}

/** the knobs a secondary agent INHERITS, in render order; toggles and skills are never here (the always-off law). */
export const INHERITABLE_FIELDS: ReadonlyArray<{ key: string; label: string }> = [
  { key: "system_prompt", label: "system prompt" },
  { key: "provider.instance", label: "provider" },
  { key: "provider.model_name", label: "model" },
  { key: "max_turns", label: "max turns" },
];

/** main is the only link an agent has; "inherit from agent" at CREATE is a copy, not a link. */
export const INHERIT_SOURCE = "main";

function knobText(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (Array.isArray(v)) return v.join(", ");
  return String(v);
}

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

export function chipOf(chips: readonly FieldChip[], key: string): FieldChip | undefined {
  return chips.find((c) => c.key === key);
}


/** the toggles list is the ENABLED set: empty means "nothing enabled", never "inherits main's features". */
export function agentTogglesNote(toggles: readonly string[]): string {
  return toggles.length
    ? `${toggles.length} enabled - an agent only runs what it names here`
    : "nothing enabled - an agent never inherits main's feature set";
}

/** empty means NO skills: never a silent inheritance of main's registry. */
export function agentSkillsNote(allow: readonly string[]): string {
  return allow.length
    ? `${allow.length} allowed - the explicit allowlist that runs`
    : "no skills - an empty allowlist means none, never main's registry";
}

/** mirrors agent.AlwaysOnAgentModules: not in the toggleable tier at all, so the panel must not offer it. */
export const LOCKED_AGENT_MODULES: readonly string[] = ["agent-loop", "provider"];

/** the subset law: a module the host has off is not offered, so the operator never meets an avoidable refusal. */
export function toggleOptions(
  modules: ReadonlyArray<{ name: string; state?: string; toggleable?: boolean }>,
): string[] {
  return modules
    .filter((m) => m.toggleable !== false && (m.state ?? "mounted") === "mounted")
    .filter((m) => !LOCKED_AGENT_MODULES.includes(m.name))
    .map((m) => m.name);
}

/** the union matters: a capsule the host switched off after the agent enabled it stays visible (dim), or the next save is a 400. */
export function toggleChoices(offered: readonly string[], own: readonly string[]): string[] {
  return [...new Set([...offered, ...own])].sort();
}

/** the agent's VISIBLE registry - the backend validates the same list, so an offered name can never be refused. */
export function skillOptions(a: Pick<OmegaAgent, "skills">): string[] {
  return [...(a.skills ?? [])];
}