// omega webui: the Cron tab's JOB EDITOR chain (card t_ecec864e, the
// settings-exposure ruling: the tab shows and edits ALL job fields).
// The editor's behavior lives in this PURE module (the toggle.ts /
// search.ts convention): the node suite probes the wire with a fetch
// log and pins the validation laws without compiling CronJobs.vue - the
// SFC only wires the form and the buttons.
//
// Scope law: MAIN-home jobs only. A row the agent home OWNS (its yaml
// lives in that home's own config/crons.yaml) is read-only here -
// editing it would write across a home boundary (the LoadAllConfigs
// law). The toggle keeps working on those rows. Ownership rides the
// wire as `agent_owned`, never inferred from `agent` (card
// t_21824d98: a main-home job's AUTHORED `agent:` is main's file and
// stays editable).

import type { CronJobWire } from "./toggle";

export interface CronJobWireFull extends CronJobWire {
  kind?: string; // cron | interval (cron when absent)
  minutes?: number; // interval cadence
  deliver?: string; // resolved target: new-session | continuous-agent | append-to-file
  model?: string; // empty = the session provider model
  file?: string; // the append-to-file target, home-relative
  prompt?: string; // the authored user message (the editor's whole-job upsert needs it)
  skills?: string[]; // the fire-pinned skill names
  // agent_owned marks the job's FILE ownership (the loader's
  // AgentOwned): true when the yaml lives in an agent home's own
  // config/crons.yaml. Agent alone cannot carry this - a main-home
  // job's authored `agent:` and a wake's owning home both set it.
  agent_owned?: boolean;
  // the last fire's outcome: status "" (never fired) | ok | error, the
  // completion wall clock in RFC3339, and the capped error message.
  status?: string;
  last?: string;
  status_note?: string;
}

// JobDraft is the form shape: every field a string so inputs bind
// directly (minutes and enabled turn typed at save time). skills is
// the comma-separated editor voice - the list shape lives only on the
// wire (parseSkills turns it).
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

// the deliver vocabulary - the ONE enum the cron Job schema carries
// (harness/cron/cron.go). Legacy aliases resolve server-side; the
// editor speaks the canonical values only.
export const DELIVER_TARGETS = [
  "new-session",
  "continuous-agent",
  "append-to-file",
] as const;

// editableOf: a row the agent home OWNS is the agent's own file - the
// editor refuses it by hiding the affordance. The refusal reads the
// ownership mark, never `agent`: a main-home job's AUTHORED `agent:`
// lives in main's crons.yaml and keeps its pencil (card t_21824d98).
export function editableOf(j: CronJobWireFull): boolean {
  return !j.agent_owned;
}

// emptyDraft: a new job lands cron-kind, new-session, enabled - the
// schema's own defaults.
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

// draftOf prefills the form from a wire row (kind defaulting to cron -
// the schema's default, absent on the wire for legacy jobs).
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

// validateDraft returns "" when the draft obeys the schema laws the Go
// loader enforces (the editor NEVER ships a job the loader would
// reject - one validation law, mirrored), else the human error line.
export function validateDraft(d: JobDraft): string {
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
  return "";
}

// parseSkills turns the comma-separated editor voice into the wire's
// list: split on the comma, trim, drop empties, keep order (the Go
// loader's own normalizeSkills law, mirrored).
export function parseSkills(s: string): string[] {
  const out: string[] = [];
  for (const part of s.split(",")) {
    const name = part.trim();
    if (name !== "") out.push(name);
  }
  return out;
}

// --- the field option sources (card t_60e0651e) ----------------------
//
// Three GETs, all pre-existing, no new registry:
//
//   GET /agents   the roster inventory, harness/continuous/agentsrest.go
//                 (LoadProfiles: the main profile first, then every
//                 agents/<name> home, named `default` for the home root)
//   GET /models   the provider catalog the settings model knobs read,
//                 agent/api/server.go handleModels
//   GET /skills   the loaded skill inventory, api/server_inspector.go
//
// Every control degrades to free text (or a named empty list) when its
// GET fails - an option source that is down must never strand an edit.

// FieldOption is one dropdown row in the editor's option sources.
// `hint` is the secondary line (a description, "the main agent");
// `group` labels the source family so a test can tell them apart.
export interface FieldOption {
  value: string;
  label: string;
  hint: string;
  group: string;
}

// agentRowWire is the subset of GET /agents this editor reads (the
// roster row: agent.Profile + last_session_id).
interface AgentRowWire {
  name?: string;
  description?: string;
  continuous?: boolean;
}

// fetchAgentOptions reads the agents registry. Typing a name that is
// NOT on this list stays legal: a job may reference an agent home that
// no longer exists, and the editor must never strand that job's edit.
//
// The wire's `continuous` flag marks a SECONDARY agent home (the one
// that wakes as a subprocess, agent/profiles.go loadProfile); the home
// root - the main agent, named `default` - answers false and never
// wakes as a subprocess (harness/continuous/wake.go refuses it), so its
// row says so where the operator picks it. Verified live against
// GET /agents, not read off the stale Profile comment.
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

// fetchModelOptions reads the model catalog the settings knobs use.
//
// The offered VALUE is a BARE model name, and that is the load-bearing
// choice: a cron job's `model:` is consumed as a bare model on the
// provider - harness/cron/runner.go hands it straight to
// p.WithModel(model) for a session fire, and harness/continuous/wake.go
// rides it as OMEGA_MODEL for a wake fire - while the `instance:model`
// slot grammar is resolved only in the chat/slot lanes
// (agent/api/server.go through agent.ParseModelSlot). Offering an
// instance-qualified value here would write a string every fire fails
// on, so the list is the base provider's own catalog (the flat
// `models` array the settings pickers fall back to); an operator who
// needs another instance's model types it, exactly as before. The
// grouped `providers` sections stay unread for the same reason.
export async function fetchModelOptions(): Promise<FieldOption[]> {
  const res = await fetch("/models");
  if (!res.ok) throw new Error(`GET /models: ${res.status}`);
  const wire = (await res.json()) as ModelListWire;
  const seen = new Set<string>();
  const out: FieldOption[] = [];
  for (const raw of wire.models ?? []) {
    const name = (raw ?? "").trim();
    if (name === "" || seen.has(name)) continue;
    seen.add(name);
    out.push({ value: name, label: name, hint: "", group: "models" });
  }
  return out;
}

// skillRowWire is one GET /skills row (name + description only).
interface SkillRowWire {
  name?: string;
  description?: string;
}

// fetchSkillOptions reads the skills registry. The skills control is
// STRICT: only names that answer here are offered, and only those
// survive a job's load (strictSkills below).
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

// filterFieldOptions narrows a dropdown by the typed query, matching
// the value or the hint (case-insensitive). An empty query is the full
// list - opening a control always shows the whole catalog.
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
// -1 = nothing active (the typed text stands); the first Down lands on
// row 0 and the first Up on the last row.
export function moveComboIndex(
  index: number,
  delta: number,
  length: number,
): number {
  if (length <= 0) return -1;
  if (index < 0) return delta > 0 ? 0 : length - 1;
  return (((index + delta) % length) + length) % length;
}

// --- the strict skills multi-select (ruling 3) -----------------------

// strictSkills splits a job's pinned names into what the registry can
// serve and what it cannot. STRICT means the control loads only known
// names: an unknown one cannot be represented as a chip, so it is
// dropped - and returned, so the editor can SAY it dropped something
// instead of losing it silently.
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

// skillsVoice renders a chip list back into the draft's comma voice -
// the ONE string saveCronJob parses (the upsert path is untouched).
export function skillsVoice(skills: string[]): string {
  return skills.join(", ");
}

// saveCronJob POSTs the full job definition (the /cron/save route: the
// server validates through the loader's own ParseConfig, upserts at
// the yaml.Node level, hot-reloads) and answers the fresh listing in
// the toggle's wire contract - one response carries the state it
// landed, then the listing re-fetches. Failure throws before any
// re-fetch so the prior rows stand.
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

// cadenceOf renders the row's firing voice, kind-aware: the 5-field
// expression for cron, the minute interval for interval (pure - the
// node suite pins it).
export function cadenceOf(j: CronJobWireFull): string {
  if ((j.kind ?? "cron") === "interval") return `${j.minutes ?? 0}m interval`;
  return j.schedule ?? "";
}

// jobMeta renders the row's dim meta fragments - what the defaulting
// law resolves and where the fire lands. Empty-resolved fields stay
// quiet (they ARE the defaults; noise would bury the authored ones).
export function jobMeta(j: CronJobWireFull): string {
  const parts: string[] = [];
  if (j.deliver && j.deliver !== "new-session") parts.push(j.deliver);
  if (j.agent) parts.push(`agent ${j.agent}`);
  if (j.model) parts.push(`model ${j.model}`);
  if (j.file) parts.push(`file ${j.file}`);
  if (j.skills?.length) parts.push(`skills ${j.skills.join(", ")}`);
  return parts.length ? ` - ${parts.join(", ")}` : "";
}

// lastMeta renders the row's last-fire voice (t_3885c975): " - last ok
// 11/07 02:14", with the capped error note riding an error. The
// never-fired row stays quiet - "-" means no fire, silence here is the
// same truth. The note display caps under 120 chars: the row stays a
// row (the full message is in /cron's log and the fire session).
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

// statusStamp renders an RFC3339 timestamp as a local, compact
// date-time ("11/07 02:14") - the same shape the row renders `next`
// with. An unparseable string renders verbatim (the wire's truth).
export function statusStamp(rfc3339: string): string {
  const t = new Date(rfc3339);
  if (Number.isNaN(t.getTime())) return rfc3339;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(t.getMonth() + 1)}/${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}`;
}