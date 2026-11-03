// omega webui: the CRON tab's in-band search model (card t_529628d5,
// Endo's band-search ruling). The band seat filters the ALREADY-LOADED
// job list client-side - no new backend route; GET /cron/jobs keeps its
// fetch semantics. A job survives when the query is a case-insensitive
// substring of the job's NAME or its SCHEDULE expression. Filtering
// only HIDES rows (the row anatomy law, card t_561dc4e0): the survivors
// keep their exact row shape. Pure - no Vue, no fetch - the node suite
// pins it without compiling the SFC (the toggle.ts recipe).

import type { CronJobWire } from "./toggle";

export function filterCronJobs(
  jobs: readonly CronJobWire[],
  query: string,
): CronJobWire[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...jobs];
  return jobs.filter(
    (j) =>
      j.name.toLowerCase().includes(q) ||
      j.schedule.toLowerCase().includes(q),
  );
}