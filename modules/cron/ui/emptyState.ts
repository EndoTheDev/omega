// omega webui: the cron tab's empty-state copy (card t_6072e718). PURE
// on purpose (the serverRows.ts / narrow.ts convention): the node suite
// pins these strings without compiling the SFC they render in.
//
// The empty state is the designed pattern: the cron clock glyph, the
// muted "no cron jobs" headline, and the activation hint below it. The
// hint points at the + editor tool (card t_ecec864e: the tab edits jobs
// on the main home) and keeps the literal config key - both doors stay
// named.

/** the muted headline shown when GET /cron/jobs returns zero jobs */
export const CRON_EMPTY_TEXT = "no cron jobs";

/** the activation hint - keeps the config key the user must create */
export const CRON_EMPTY_HINT =
  "no jobs configured - add one with the + tool (writes crons.yaml) to schedule work, or copy the file into omega's home dir";