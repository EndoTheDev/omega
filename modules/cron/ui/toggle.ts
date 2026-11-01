// omega webui: the Cron tab's per-job toggle chain (card t_561dc4e0).
// The toggle button's behavior lives here so the node suite probes it
// with a fetch log - the SFC (CronJobs.vue) only wires the arming and
// the click, module-UI style. The empty state stays pure in
// emptyState.ts: no fetch.

// CronJobWire is one GET /cron/jobs row (core.CronJobInfo's JSON
// shape): name, 5-field schedule, enablement, the next fire in
// RFC3339 when it fires, and the agent home owning the job (empty for
// main's own jobs). agent is omitempty on the Go wire, so it reads as
// undefined here.
export interface CronJobWire {
  name: string;
  schedule: string;
  enabled: boolean;
  next: string;
  agent?: string;
}

// setCronJobEnabled persists one job's enabled flag the ruled way:
// POST /cron/toggle (the server writes the `enabled:` flag into the
// crons.yaml that owns the job - the yaml.Node fold-write, mcp.yaml's
// own toggle law in modules/mcp/status.go - and hot-reloads the
// runner, no restart), then re-fetches the listing. Returns the fresh
// listing; failure throws so the panel's error line shows it and the
// prior rows stand (a refused write never fakes a swap - the
// reload.ts ruling). The key is the job's StateKey: main-home jobs key
// on their name, agent wake jobs on <agent>/<name> - two agents can
// both carry a job literally named wake.
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
  // the toggle response IS the fresh job set (the wire contract: one
  // response carries the state it landed) - the listing is the
  // re-fetch, the ruled two-step
  await res.json();
  const list = await fetch("/cron/jobs");
  if (!list.ok) throw new Error(`GET /cron/jobs: ${list.status}`);
  return (await list.json()) as CronJobWire[];
}

// cronKeyOf derives a job's StateKey from its wire row - the same
// <agent>/<name> shape the Go state keys on (modules/cron/cron.go
// StateKey).
export function cronKeyOf(j: CronJobWire): string {
  return j.agent ? `${j.agent}/${j.name}` : j.name;
}