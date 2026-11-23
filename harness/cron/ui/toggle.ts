// omega webui: the Cron tab's per-job toggle chain.

// CronJobWire is one GET /cron/jobs row (JobInfo's JSON shape).
export interface CronJobWire {
  name: string;
  schedule: string;
  enabled: boolean;
  next: string;
  agent?: string;
  agent_owned?: boolean;
  // the last fire's outcome: "" (never) | "ok" | "error", its completion stamp and the
  // capped error note. The console's status dot reads status only.
  status?: string;
  last?: string;
  status_note?: string;
  // the live-fire mark the run routes answer: true while this job's fire is in flight.
  running?: boolean;
}

async function fetchCronJobs(): Promise<CronJobWire[]> {
  const list = await fetch("/cron/jobs");
  if (!list.ok) throw new Error(`GET /cron/jobs: ${list.status}`);
  return (await list.json()) as CronJobWire[];
}

// runCronJobNow is POST /cron/run {key}: one fire the schedule did not order. The answer
// is the fresh job set, carrying the live mark the menu reads.
export async function runCronJobNow(key: string): Promise<CronJobWire[]> {
  const res = await fetch("/cron/run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ key }),
  });
  if (!res.ok) throw new Error(`POST /cron/run: ${res.status}`);
  return (await res.json()) as CronJobWire[];
}

// stopCronRun is POST /cron/stop {key}: cancel the fire in flight.
export async function stopCronRun(key: string): Promise<CronJobWire[]> {
  const res = await fetch("/cron/stop", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ key }),
  });
  if (!res.ok) throw new Error(`POST /cron/stop: ${res.status}`);
  return (await res.json()) as CronJobWire[];
}

export async function setCronJobEnabled(
  key: string,
  enabled: boolean,
): Promise<CronJobWire[]> {
  const res = await fetch("/cron/toggle", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ key, enabled }),
  });
  if (!res.ok) throw new Error(`POST /cron/toggle: ${res.status}`);
  await res.json();
  return fetchCronJobs();
}

// cronKeyOf derives a job's StateKey from its wire row.
export function cronKeyOf(j: CronJobWire): string {
  return j.agent_owned && j.agent ? `${j.agent}/${j.name}` : j.name;
}