// omega webui: the cron console row's status dot - one word per last fire.
//
// The dot answers "how did the last fire end": green after a clean fire, red after a
// failed one, amber when the job never fired at all (an unknown outcome is not a failure,
// but it is not a success either). The live-fire mark is a separate signal (the row's
// menu offers Stop run while it is set), never folded into the outcome color.

export type CronDotState = "ok" | "error" | "never";

export function cronDotOf(job: { status?: string }): CronDotState {
  if (job.status === "ok") return "ok";
  if (job.status === "error") return "error";
  return "never";
}

/** the dot's hover voice: what the color claims, in words */
export function cronDotLabel(job: {
  status?: string;
  last?: string;
  status_note?: string;
}): string {
  switch (cronDotOf(job)) {
    case "ok":
      return `last fire ok${job.last ? " - " + job.last : ""}`;
    case "error":
      return `last fire failed${job.status_note ? ": " + job.status_note : ""}`;
    default:
      return "never fired";
  }
}
