// omega webui: the Cron tab's JOB EDITOR chain. Ownership rides the wire as
// `agent_owned`, never inferred from `agent`.

import type { CronJobWire } from "./toggle";

export interface CronJobWireFull extends CronJobWire {
  kind?: string; // cron | interval (cron when absent)
  minutes?: number; // interval cadence
  deliver?: string; // resolved target: new-session | continuous-agent | append-to-file
  model?: string; // empty = the session provider model
  file?: string; // the append-to-file target, home-relative
  prompt?: string; // the authored user message (the editor's whole-job upsert needs it)
  skills?: string[]; // the fire-pinned skill names
  // agent_owned marks the job's FILE ownership (the loader's AgentOwned). Agent alone
  // cannot carry this - a main-home job's authored `agent:` and a wake's owning home
  // both set it.
  agent_owned?: boolean;
  // the last fire's outcome: status "" (never fired) | ok | error, the completion.
  status?: string;
  last?: string;
  status_note?: string;
}

// JobDraft is the form shape.
export interface JobDraft {
  name: string;
  kind: string;
  schedule: string;
  minutes: string;
  prompt: string;
  deliver: string;
  file: string;
  agent: string;
  model: string;
  skills: string;
  enabled: boolean;
}

// the deliver vocabulary - the ONE enum the cron Job schema carries.
export const DELIVER_TARGETS = [
  "new-session",
  "continuous-agent",
  "append-to-file",
] as const;

// editableOf: a row the agent home OWNS is the agent's own file. The refusal reads the
// ownership mark, never `agent`: a main-home job's AUTHORED `agent:` lives in main's
// crons.yaml and keeps its pencil.
export function editableOf(j: CronJobWireFull): boolean {
  return !j.agent_owned;
}

// emptyDraft: a new job lands cron-kind, new-session, enabled.
export function emptyDraft(): JobDraft {
  return {
    name: "",
    kind: "cron",
    schedule: "",
    minutes: "",
    prompt: "",
    deliver: "new-session",
    file: "",
    agent: "",
    model: "",
    skills: "",
    enabled: true,
  };
}

// draftOf prefills the form from a wire row (kind defaulting to cron.
export function draftOf(j: CronJobWireFull): JobDraft {
  return {
    name: j.name,
    kind: j.kind ?? "cron",
    schedule: j.schedule ?? "",
    minutes: j.minutes != null ? String(j.minutes) : "",
    prompt: j.prompt ?? "",
    deliver: j.deliver ?? "new-session",
    file: j.file ?? "",
    agent: j.agent ?? "",
    model: j.model ?? "",
    skills: (j.skills ?? []).join(", "),
    enabled: j.enabled,
  };
}

// validateDraft returns "" when the draft obeys the schema laws the Go loader enforces
// (the editor NEVER ships a job the loader would reject - one validation law,
// mirrored), else the human error line. The inheritance chain's law rides here too
// (chainViolation): a wake that leans on the main agent would persist and never fire,
// so it never leaves the editor either.
export function validateDraft(
  d: JobDraft,
  mainAgent: string = MAIN_AGENT_FALLBACK,
): string {
  const name = d.name.trim();
  if (name === "") return "name is required";
  if (!/^[a-z0-9-]+$/.test(name)) return "name must match [a-z0-9-]";
  if (d.kind === "interval") {
    if (d.schedule.trim() !== "") return "interval jobs take minutes, not schedule";
    if (!/^\d+$/.test(d.minutes.trim()) || Number(d.minutes.trim()) < 1)
      return "interval jobs need minutes >= 1";
  } else if (d.kind === "cron") {
    if (d.minutes.trim() !== "") return "cron jobs take schedule, not minutes";
    if (d.schedule.trim() === "") return "schedule is required";
  } else {
    return "kind must be cron or interval";
  }
  if (d.prompt.trim() === "") return "prompt is required";
  const deliver = d.deliver.trim();
  if (deliver !== "" && !(DELIVER_TARGETS as readonly string[]).includes(deliver))
    return "deliver must be new-session, continuous-agent, or append-to-file";
  if (d.file.trim() !== "" && deliver !== "append-to-file")
    return "file only applies to deliver append-to-file";
  const seenSkills = new Set<string>();
  for (const s of parseSkills(d.skills)) {
    if (!/^[a-z0-9-]+$/.test(s)) return "skill names must match [a-z0-9-]";
    if (seenSkills.has(s)) return `duplicate skill ${s}`;
    seenSkills.add(s);
  }
  // the chain's law last: a schema-broken draft names its schema problem first, never.
  return chainViolation(d, mainAgent);
}

// parseSkills turns the comma-separated editor voice into the wire's list.
export function parseSkills(s: string): string[] {
  const out: string[] = [];
  for (const part of s.split(",")) {
    const name = part.trim();
    if (name !== "") out.push(name);
  }
  return out;
}

// -- the field option sources ---------------------
//
// Three GETs, all pre-existing, no new registry:
//
// GET /agents the roster inventory, harness/continuous/agentsrest.go
//                 (LoadProfiles: the main profile first, then every
//                 agents/<name> home, named `default` for the home root)
// GET /models the provider catalog the settings model knobs read,
//                 agent/api/server.go handleModels
// GET /skills the loaded skill inventory, api/server_inspector.go
//
// Every control degrades to free text (or a named empty list) when its
// GET fails - an option source that is down must never strand an edit.

// FieldOption is one dropdown row in the editor's option sources.
export interface FieldOption {
  value: string;
  label: string;
  hint: string;
  group: string;
  model?: string;
  continuous?: boolean;
}

// agentRowWire is the subset of GET /agents this editor reads (the
// roster row: agent.Profile facts; the canonical chat id rides the row
// but the cron editor never opens chats).
interface AgentRowWire {
  name?: string;
  description?: string;
  continuous?: boolean;
  model_name?: string;
}

// fetchAgentOptions reads the agents registry. Typing a name that is NOT on this list
// stays legal: a job may reference an agent home that no longer exists, and the editor
// must never strand that job's edit. The wire's `continuous` flag marks a SECONDARY
// agent home (the one that wakes as a subprocess, agent/profiles.go loadProfile); the
// home root - the main agent, named `default` - answers false and never wakes as a
// subprocess (harness/continuous/wake.go refuses it), so its row says so where the
// operator picks it.
export async function fetchAgentOptions(): Promise<FieldOption[]> {
  const res = await fetch("/agents");
  if (!res.ok) throw new Error(`GET /agents: ${res.status}`);
  const rows = (await res.json()) as AgentRowWire[];
  const out: FieldOption[] = [];
  for (const r of rows) {
    const name = (r.name ?? "").trim();
    if (name === "") continue;
    out.push({
      value: name,
      label: name,
      hint:
        r.description ||
        (r.continuous
          ? "agent home"
          : "the main agent - in-process, never wakes"),
      group: "agents",
      model: (r.model_name ?? "").trim(),
      continuous: r.continuous === true,
    });
  }
  return out;
}

// modelListWire is the subset of GET /models this editor reads.
interface ModelListWire {
  current?: string;
  models?: string[];
  providers?: { name: string; models?: string[]; error?: string }[];
}

// ModelCatalog is fetchModelOptions' answer.
export interface ModelCatalog {
  options: FieldOption[];
  current: string;
}

// fetchModelOptions reads the model catalog the settings knobs use. The offered VALUE
// is a BARE model name, and that is the load-bearing choice: a cron job's `model:` is
// consumed as a bare model on the provider - harness/cron/runner.go hands it straight
// to p.WithModel(model) for a session fire, and harness/continuous/wake.go rides it as
// OMEGA_MODEL for a wake fire - while the `instance:model` slot grammar is resolved
// only in the chat/slot lanes (agent/api/server.go through agent.ParseModelSlot).
// Offering an instance-qualified value here would write a string every fire fails on,
// so the list is the base provider's own catalog (the flat `models` array the settings
// pickers fall back to); an operator who needs another instance's model types it,
// exactly as before.
export async function fetchModelOptions(): Promise<ModelCatalog> {
  const res = await fetch("/models");
  if (!res.ok) throw new Error(`GET /models: ${res.status}`);
  const wire = (await res.json()) as ModelListWire;
  const seen = new Set<string>();
  const options: FieldOption[] = [];
  for (const raw of wire.models ?? []) {
    const name = (raw ?? "").trim();
    if (name === "" || seen.has(name)) continue;
    seen.add(name);
    options.push({ value: name, label: name, hint: "", group: "models" });
  }
  return { options, current: (wire.current ?? "").trim() };
}

// skillRowWire is one GET /skills row (name + description only).
interface SkillRowWire {
  name?: string;
  description?: string;
}

// fetchSkillOptions reads the skills registry.
export async function fetchSkillOptions(): Promise<FieldOption[]> {
  const res = await fetch("/skills");
  if (!res.ok) throw new Error(`GET /skills: ${res.status}`);
  const rows = (await res.json()) as SkillRowWire[];
  const out: FieldOption[] = [];
  for (const r of rows) {
    const name = (r.name ?? "").trim();
    if (name === "") continue;
    out.push({
      value: name,
      label: name,
      hint: r.description ?? "",
      group: "skills",
    });
  }
  return out;
}

// filterFieldOptions narrows a dropdown by the typed query, matching the value or the.
export function filterFieldOptions(
  options: FieldOption[],
  query: string,
): FieldOption[] {
  const q = query.trim().toLowerCase();
  if (q === "") return options;
  return options.filter(
    (o) =>
      o.value.toLowerCase().includes(q) || o.hint.toLowerCase().includes(q),
  );
}

// optionKnown reports whether a typed value names a listed option.
export function optionKnown(options: FieldOption[], value: string): boolean {
  const v = value.trim();
  return options.some((o) => o.value === v);
}

// moveComboIndex steps a combobox's active row, wrapping at both ends.
export function moveComboIndex(
  index: number,
  delta: number,
  length: number,
): number {
  if (length <= 0) return -1;
  if (index < 0) return delta > 0 ? 0 : length - 1;
  return (((index + delta) % length) + length) % length;
}

// -- the inheritance chain ------------------------
//
// ONE state machine, two links:
//
// agent: "" -> INHERITED from the main agent
// agent: "scout" -> AUTHORED
// model: "" -> INHERITED from the agent the fire resolves to
// model: "glm" -> AUTHORED
//
// The STATE is presence - an absent field inherits, a present one is
// authored - and the chain never writes a resolved value back into the
// field (the pencil fix's law, one field read two ways is
// a bug waiting for its second reader). What the chain adds is the
// DISPLAY: a chip that says "inherited" plus the link that supplies
// the value, so an empty box stops being a mystery.
//
// The model link follows the agent the FIRE runs under, not the name
// in the box: an in-process fire runs as the main agent
// (runner.go's wrong-target law), so its model is the main agent's;
// a wake runs inside the agent's own home, where the agent's config
// layer answers and a layer that authors nothing inherits the main
// again (agent/configedit.go's LayerValue presence law: "an
// explicitly empty value is a real override" - for the chain, an
// absent model_name is the inherit).
//
// The same state machine rules the agents panel (its config layer
// carries the identical authored-vs-inherited question); each module
// keeps its own copy, the narrow.ts precedent - module UI imports no
// other module's code.

// ChainValue is one field's chain state.
export interface ChainValue {
  inherited: boolean;
  // value is what the fire will use. "" when the chain cannot resolve it (an agent the
  // registry does not list, or a model catalog that never answered).
  value: string;
  source: string;
  note: string;
  // blocked marks a chain the fire can NEVER satisfy - a wake leaning on the main
  // agent, which never wakes as a subprocess.
  blocked: boolean;
}

// ChainDraft is the slice of the form the chain reads.
export interface ChainDraft {
  agent: string;
  model: string;
  deliver: string;
}

// MAIN_AGENT_FALLBACK mirrors the Go constant agent.DefaultAgentName. The roster names
// it, but GET /agents that never answered must still render the chain honestly.
export const MAIN_AGENT_FALLBACK = "default";

// mainAgentOf reads the main agent's name off the roster.
export function mainAgentOf(roster: FieldOption[]): string {
  const main = roster.find((o) => o.continuous === false);
  return main?.value ?? MAIN_AGENT_FALLBACK;
}

const CHAIN_AUTHORED: ChainValue = {
  inherited: false,
  value: "",
  source: "",
  note: "",
  blocked: false,
};

// isWake reports whether the draft's deliver target fires into an agent's own.
function isWake(d: ChainDraft): boolean {
  return d.deliver.trim() === "continuous-agent";
}

// agentChain resolves the agent link: absent = the main agent.
export function agentChain(d: ChainDraft, roster: FieldOption[]): ChainValue {
  const authored = d.agent.trim();
  if (authored !== "") return { ...CHAIN_AUTHORED, value: authored };
  const main = mainAgentOf(roster);
  if (isWake(d)) {
    // the chain resolves, but the fire cannot: the main agent is the
    // daemon itself and never wakes as a subprocess (continuous/wake.go
    // refuses it, runner.go logs "main never wakes")
    return {
      inherited: true,
      value: main,
      source: "the main agent",
      note: `inherits the main agent (${main}), which never wakes`,
      blocked: true,
    };
  }
  return {
    inherited: true,
    value: main,
    source: "the main agent",
    note: `inherits the main agent (${main})`,
    blocked: false,
  };
}

// modelChain resolves the model link.
export function modelChain(
  d: ChainDraft,
  roster: FieldOption[],
  mainModel: string,
): ChainValue {
  const authored = d.model.trim();
  if (authored !== "") return { ...CHAIN_AUTHORED, value: authored };
  const agent = agentChain(d, roster);
  const main = mainAgentOf(roster);
  if (isWake(d) && agent.value !== main) {
    const row = roster.find((o) => o.value === agent.value);
    if (!row) {
      return {
        inherited: true,
        value: "",
        source: `agent ${agent.value}`,
        note: `inherits agent ${agent.value} - not in the registry, so its model is unknown`,
        blocked: false,
      };
    }
    if (row.model) {
      return {
        inherited: true,
        value: row.model,
        source: `agent ${agent.value}`,
        note: `inherits agent ${agent.value} (${row.model})`,
        blocked: false,
      };
    }
    return {
      inherited: true,
      value: mainModel,
      source: `agent ${agent.value}, which inherits the main agent`,
      note: mainModel
        ? `inherits agent ${agent.value}, which inherits the main agent (${mainModel})`
        : `inherits agent ${agent.value}, which inherits the main agent`,
      blocked: false,
    };
  }
  return {
    inherited: true,
    value: mainModel,
    source: "the main agent",
    note: mainModel
      ? `inherits the main agent (${mainModel})`
      : "inherits the main agent",
    // a wake inheriting the main agent cannot fire at all.
    blocked: agent.blocked,
  };
}

// chainViolation is the chain's ONE law as the operator reads it: a wake needs an agent
// of its own, because the chain's fallback (the main agent) never wakes.
export function chainViolation(d: ChainDraft, mainAgent: string): string {
  if (!isWake(d)) return "";
  const agent = d.agent.trim();
  if (agent !== "" && agent !== mainAgent) return "";
  return `continuous-agent needs its own agent - an empty agent inherits the main agent (${mainAgent}), which never wakes`;
}

// -- the strict skills multi-select ----------------------

// strictSkills splits a job's pinned names into what the registry can serve and what it
// cannot. STRICT means the control loads only known names: an unknown one cannot be
// represented as a chip, so it is dropped - and returned, so the editor can SAY it
// dropped something instead of losing it silently.
export function strictSkills(
  skills: string[],
  options: FieldOption[],
): { kept: string[]; dropped: string[] } {
  const known = new Set(options.map((o) => o.value));
  const kept: string[] = [];
  const dropped: string[] = [];
  for (const s of skills) {
    if (known.has(s)) {
      if (!kept.includes(s)) kept.push(s);
    } else {
      dropped.push(s);
    }
  }
  return { kept, dropped };
}

// withSkill / withoutSkill are the checkbox toggles over a chip list.
export function withSkill(skills: string[], name: string): string[] {
  return skills.includes(name) ? skills : [...skills, name];
}

export function withoutSkill(skills: string[], name: string): string[] {
  return skills.filter((s) => s !== name);
}

// skillsVoice renders a chip list back into the draft's comma voice.
export function skillsVoice(skills: string[]): string {
  return skills.join(", ");
}

// saveCronJob POSTs the full job definition (the /cron/save route: the server validates
// through the loader's own ParseConfig, upserts at the yaml.Node level, hot-reloads)
// and answers the fresh listing in the toggle's wire contract - one response carries
// the state it landed, then the listing re-fetches.
export async function saveCronJob(d: JobDraft): Promise<CronJobWireFull[]> {
  const body = {
    name: d.name.trim(),
    kind: d.kind,
    schedule: d.kind === "cron" ? d.schedule.trim() : "",
    minutes: d.kind === "interval" ? Number(d.minutes.trim()) : 0,
    prompt: d.prompt,
    skills: parseSkills(d.skills),
    deliver: d.deliver,
    file: d.file.trim(),
    agent: d.agent,
    model: d.model.trim(),
    enabled: d.enabled,
  };
  const res = await fetch("/cron/save", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const detail = await res.text().catch(() => "");
    throw new Error(`POST /cron/save: ${res.status}${detail ? ` - ${detail.trim()}` : ""}`);
  }
  await res.json();
  const list = await fetch("/cron/jobs");
  if (!list.ok) throw new Error(`GET /cron/jobs: ${list.status}`);
  return (await list.json()) as CronJobWireFull[];
}

// deleteCronJob removes one MAIN-home job (POST /cron/delete); an
// unknown name throws with the 404 - never a silent no-op.
export async function deleteCronJob(name: string): Promise<CronJobWireFull[]> {
  const res = await fetch("/cron/delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(`POST /cron/delete: ${res.status}`);
  await res.json();
  const list = await fetch("/cron/jobs");
  if (!list.ok) throw new Error(`GET /cron/jobs: ${list.status}`);
  return (await list.json()) as CronJobWireFull[];
}

// cadenceOf renders the row's firing voice, kind-aware.
export function cadenceOf(j: CronJobWireFull): string {
  if ((j.kind ?? "cron") === "interval") return `${j.minutes ?? 0}m interval`;
  return j.schedule ?? "";
}

// jobMeta renders the row's dim meta fragments.
export function jobMeta(j: CronJobWireFull): string {
  const parts: string[] = [];
  if (j.deliver && j.deliver !== "new-session") parts.push(j.deliver);
  if (j.agent) parts.push(`agent ${j.agent}`);
  if (j.model) parts.push(`model ${j.model}`);
  if (j.file) parts.push(`file ${j.file}`);
  if (j.skills?.length) parts.push(`skills ${j.skills.join(", ")}`);
  return parts.length ? ` - ${parts.join(", ")}` : "";
}

// lastMeta renders the row's last-fire voice. The never-fired row stays quiet - "-"
// means no fire, silence here is the same truth.
const NOTE_DISPLAY_CAP = 120;

export function lastMeta(j: CronJobWireFull): string {
  if (!j.status || !j.last) return "";
  const stamp = statusStamp(j.last);
  if (j.status === "error" && j.status_note) {
    const note =
      j.status_note.length > NOTE_DISPLAY_CAP
        ? `${j.status_note.slice(0, NOTE_DISPLAY_CAP)}...`
        : j.status_note;
    return ` - last error ${stamp}: ${note}`;
  }
  return ` - last ${j.status} ${stamp}`;
}

// statusStamp renders an RFC3339 timestamp as a local, compact date-time ("11/07
// 02:14").
export function statusStamp(rfc3339: string): string {
  const t = new Date(rfc3339);
  if (Number.isNaN(t.getTime())) return rfc3339;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(t.getMonth() + 1)}/${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}`;
}