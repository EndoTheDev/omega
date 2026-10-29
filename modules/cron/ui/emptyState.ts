// omega webui: the cron tab's empty-state copy (card t_6072e718). PURE
// on purpose (the serverRows.ts / narrow.ts convention): the node suite
// pins these strings without compiling the SFC they render in.
//
// The empty state is the designed pattern: the cron clock glyph, the
// muted "no cron jobs" headline, and the activation hint below it. The
// hint keeps the literal config key (crons.yaml) - the user has to know
// WHERE to act, since activation is a file action, not an in-pane
// button (READ-ONLY v1, card t_db430fea).

/** the muted headline shown when GET /cron/jobs returns zero jobs */
export const CRON_EMPTY_TEXT = "no cron jobs";

/** the activation hint - keeps the config key the user must create */
export const CRON_EMPTY_HINT =
  "crons.yaml is not activated - create it in omega's home dir to schedule jobs";