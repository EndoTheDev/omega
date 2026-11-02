// omega webui: the Cron tab's JOB EDITOR chain (card t_ecec864e, the
// settings-exposure ruling: the tab shows and edits ALL job fields).
// The editor's behavior lives in this PURE module (the toggle.ts /
// search.ts convention): the node suite probes the wire with a fetch
// log and pins the validation laws without compiling CronJobs.vue - the
// SFC only wires the form and the buttons.
//
// Scope law: MAIN-home jobs only. A row with an `agent` (wake, owned by
// the agent home's own config/crons.yaml) is read-only here - editing
// it would write across a home boundary (the LoadAllConfigs law). The
// toggle keeps working on those rows.

import type { CronJobWire } from "./toggle";

export interface CronJobWireFull extends CronJobWire {
  kind?: string; // cron | interval (cron when absent)
  minutes?: number; // interval cadence
  deliver?: string; // resolved target: new-session | continuous-agent | append-to-file
  model?: string; // empty = the session provider model
  file?: string; // the append-to-file target, home-relative
}

// JobDraft is the form shape: every field a string so inputs bind
// directly (minutes and enabled turn typed at save time).
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

// editableOf: agent-home (wake) rows are the agent's own file - the
// editor refuses them by hiding the affordance.
export function editableOf(j: CronJobWireFull): boolean {
  return !j.agent;
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
    prompt: "",
    deliver: j.deliver ?? "new-session",
    file: j.file ?? "",
    agent: j.agent ?? "",
    model: j.model ?? "",
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
  return "";
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
  return parts.length ? ` - ${parts.join(", ")}` : "";
}