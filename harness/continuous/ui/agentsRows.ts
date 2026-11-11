// omega webui: the agents surface model (card t_7e2b7002). PURE: no
// fetch, no Vue. The AgentsPanel renders from these rows and the unit
// suite pins the honest-display rules here.
// NOTE (card t_7e2b7002, same-file ruling): this lives beside its panel
// (the capsules' ui/ dir; the shell-ring shrink wave 2 moved the agents
// surface out of the shell's components). omega.ts and dockables.ts
// are owned by card t_14f73742 while it runs. The panel fetches its
// own payloads until the wiring lands; the deferred mount lines are
// recorded in the card comment.

// RECORD: card t_c7222095 - continuousRows.ts types fold in here (the
// merged agent row carries the wake state).
import { rowStatus, type OmegaContinuousRow } from "./continuousRows.ts";

export interface OmegaAgent {
  name: string;
  dir: string;
  description?: string;
  provider_type?: string;
  model_name?: string;
  skill_count: number;
  skills?: readonly string[];
  memory_size: number;
  session_count: number;
  last_activity?: string;
  /** card t_81109e1c: the agent's MOST RECENT session - the id the
      roster row click binds a chat tab to (GET /agents, additive
      field). Absent for an agent with no sessions. */
  last_session_id?: string;
}

export interface OmegaAgentSession {
  id: string;
  label?: string;
  agent?: string;
  created_at?: string;
  updated_at?: string;
  message_count?: number;
}

/** row voice for the list: the model is the headline (the knob agents
    are created for); the default agent carries "(home config)" and an
    empty model_name never renders as a phantom empty row. */
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

/** the panel's empty state: default exists by definition, so a bare
    default-only home still renders one row, never an empty table. */
export function agentsListIsEmpty(profiles: readonly OmegaAgent[]): boolean {
  return profiles.length === 0;
}

/** session label voice for the agent's session list (same rule the
    sessions sidebar uses: label, else a truncated id). */
export function agentSessionTitle(s: OmegaAgentSession): string {
  return s.label || s.id.slice(0, 12) + "...";
}

/** agentDetailLines builds the RegistryList row's expandable detail:
    the config summary, one line per fact, the ordering the old detail
    block used (model, provider, dir, skills, memory, last activity).
    A missing fact renders "-" - honest empty. Card t_2d8deb8c: the
    sessions count line DIED with the session listing (ruling 3) - the
    tab shows the agent, not its sessions. */
export function agentDetailLines(a: OmegaAgent): string[] {
  return [
    `model: ${agentModelText(a) || "-"}`,
    `provider: ${a.provider_type || "-"}`,
    `dir: ${a.dir}`,
    `skills: ${agentSkillsText(a) || "none"}`,
    `memory: ${agentMemoryText(a)}`,
    `last activity: ${a.last_activity || "-"}`,
  ];
}

/** agentStatusFor (card t_2d8deb8c, Endo's status-vocabulary ruling):
    the subtitle line's live dot + short word - green (on) for a
    pulsing agent (an unread report or a scheduled wake), dim
    (asleep) for the quiet sleeper. Raw "sleeping" text alone is
    dead: the word only ever rides WITH the dot. Returns undefined
    for a config-only agent (no roster row, no invented state). */
export function agentStatusFor(
  c: OmegaContinuousRow | null,
): { on: boolean; label: string } | undefined {
  if (!c) return undefined;
  const label = rowStatus(c);
  return { on: label !== "sleeping", label };
}

/** the default-agent law (card t_2d8deb8c): the default agent cannot
    be disabled EVER - the fold is filtered of it on display (a stale
    entry an older build persisted must not render a disabled row)
    and the row carries the locked pill. */
export const DefaultAgentName = "default";

/** agentIsMain (card t_81109e1c, ruling 3): the roster includes the
    main agent like every other home, and the row says so. The main
    agent is the one at the home root - the NAME is its identity
    (`Profile.Continuous` is the wake-home flag, true for agents/<name>
    and false for main). */
export function agentIsMain(a: Pick<OmegaAgent, "name">): boolean {
  return a.name === DefaultAgentName;
}

/** agentSessionCountText (card t_81109e1c): the roster's session
    count column. Zero is honest ("no sessions"), never "0 sessions"
    dressed up as activity. */
export function agentSessionCountText(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "no sessions";
  return n === 1 ? "1 session" : `${n} sessions`;
}

/** agentActivityText (card t_81109e1c): the last-activity column as a
    relative age. Empty for a missing/unparseable timestamp (honest
    absence beats an invented row voice); a future stamp (clock skew)
    reads "just now", never a negative age. */
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

/** agentRosterSubtitle (card t_81109e1c): the roster row's subtitle
    line - the facts the tab is FOR: description, model, session
    count, last activity. Missing facts drop out; the count always
    shows (zero is a fact). */
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

/** mergedRowDetail (card t_c7222095): the merged agent row's detail -
    the config summary as today, plus the wake-state line of the
    continuous roster row when the agent carries one. A null roster row
    (no continuous home) renders the config summary alone - never a
    phantom "sleeping" for an agent with no wake schedule. */
export function mergedRowDetail(
  a: OmegaAgent,
  c: OmegaContinuousRow | null,
): string[] {
  if (!c) return agentDetailLines(a);
  return [...agentDetailLines(a), `wake: ${rowStatus(c)}`];
}

/** the join key: the roster and the config listing share the agent
    name (folder presence is registration on both sides). */
export function continuousIndex(
  roster: readonly OmegaContinuousRow[],
): Record<string, OmegaContinuousRow> {
  const out: Record<string, OmegaContinuousRow> = {};
  for (const r of roster) out[r.name] = r;
  return out;
}