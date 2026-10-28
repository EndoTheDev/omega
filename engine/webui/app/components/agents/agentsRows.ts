// omega webui: the agents surface model (card t_7e2b7002). PURE: no
// fetch, no Vue. The AgentsPanel renders from these rows and the unit
// suite pins the honest-display rules here.
// NOTE (card t_7e2b7002, same-file ruling): this lives in
// app/components/agents/, NOT composables/ - omega.ts and dockables.ts
// are owned by card t_14f73742 while it runs. The panel fetches its
// own payloads until the wiring lands; the deferred mount lines are
// recorded in the card comment.

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
    block used (model, provider, dir, skills, memory, sessions, last
    activity). A missing fact renders "-" - honest empty. */
export function agentDetailLines(a: OmegaAgent): string[] {
  return [
    `model: ${agentModelText(a) || "-"}`,
    `provider: ${a.provider_type || "-"}`,
    `dir: ${a.dir}`,
    `skills: ${agentSkillsText(a) || "none"}`,
    `memory: ${agentMemoryText(a)}`,
    `sessions: ${a.session_count}`,
    `last activity: ${a.last_activity || "-"}`,
  ];
}