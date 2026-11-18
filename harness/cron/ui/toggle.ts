// omega webui: the Cron tab's per-job toggle chain.

// CronJobWire is one GET /cron/jobs row (JobInfo's JSON shape).
export interface CronJobWire {
  name: string;
  schedule: string;
  enabled: boolean;
  next: string;
  agent?: string;
  agent_owned?: boolean;
}

// setCronJobEnabled persists one job's enabled flag the ruled way.
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
  // the toggle response IS the fresh job set (the wire contract.
  await res.json();
  const list = await fetch("/cron/jobs");
  if (!list.ok) throw new Error(`GET /cron/jobs: ${list.status}`);
  return (await list.json()) as CronJobWire[];
}

// cronKeyOf derives a job's StateKey from its wire row.
export function cronKeyOf(j: CronJobWire): string {
  return j.agent_owned && j.agent ? `${j.agent}/${j.name}` : j.name;
}