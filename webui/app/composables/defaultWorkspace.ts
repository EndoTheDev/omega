// omega webui: the loaded server.default_workspace as ONE shared value
// (card t_30137f67). The chat host's deps.defaultWorkspace getter reads
// it when a new session's draft state is born - the composer's
// workspace chip then carries the default exactly as if the user had
// picked it (plain sticky; the send consumes it, an explicit chip clear
// wins over it).
//
// Two writers, both riding the SAME /settings read the Settings tab
// renders (no second config path):
//   - app.vue's boot read (backfills the chat tab states born before
//     any settings payload exists -> host.seedDefaultWorkspace)
//   - SettingsTab's applyPayload (every reload + save round-trip)
//     - a live Settings edit applies to the next new session without
//       a reload. "" = the default was cleared: new chips start blank.
//
// A plain module value, not a ref: the getter is consulted ONCE, at
// ensureState time - the chip renders the state's workspaceIntent, so
// there is nothing to repaint when the signal moves.

/** the /settings knob key (the SERVER section's default_workspace). */
export const DEFAULT_WORKSPACE_KNOB = "server.default_workspace";

export function normalizeDefaultWorkspace(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

let signal = "";

/** the chip seed (the host's live getter): "" when none is configured. */
export function defaultWorkspaceNow(): string {
  return signal;
}

export function setDefaultWorkspace(v: unknown): void {
  signal = normalizeDefaultWorkspace(v);
}