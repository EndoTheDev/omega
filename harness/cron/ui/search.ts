// omega webui: the CRON tab's in-band search model.

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