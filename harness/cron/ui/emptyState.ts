// omega webui: the cron tab's empty-state copy.

/** the muted headline shown when GET /cron/jobs returns zero jobs */
export const CRON_EMPTY_TEXT = "no cron jobs";

/** the activation hint - keeps the config key the user must create */
export const CRON_EMPTY_HINT =
  "no jobs configured - add one with the + tool (writes crons.yaml) to schedule work, or copy the file into omega's home dir";