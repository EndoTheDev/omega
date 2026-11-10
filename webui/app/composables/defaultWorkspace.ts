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
// there is nothing to repaint when the signal moves. (t_8fd38f88 moved
// the workdir segment into the status bar, which DOES render the signal
// for a chat-less page: the value is reactive now - same API, so a live
// Settings save repaints the bar without a reload.)

import { reactive } from "vue";

/** the /settings knob key (the SERVER section's default_workspace). */
export const DEFAULT_WORKSPACE_KNOB = "server.default_workspace";

export function normalizeDefaultWorkspace(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

const signal = reactive({ value: "" });

/** the live default ("" when none is configured). */
export function defaultWorkspaceNow(): string {
  return signal.value;
}

export function setDefaultWorkspace(v: unknown): void {
  signal.value = normalizeDefaultWorkspace(v);
}

// effectiveWorkdir is the never-blank law (card t_ccf74d88; it moved out
// of the composer chip into the status bar, card t_8fd38f88). The folder
// the tool runs root at is what the workdir segment must show:
//   - an UNBOUND tab shows its draft workspace intent (the seed IS the
//     configured default, so a fresh draft already carries it);
//   - a BOUND session with an EMPTY row pin has its runs rooted at the
//     server's default_workspace, so the segment falls back to it -
//     never a blank that hides the folder the commands run in.
export function effectiveWorkdir(
  bound: boolean,
  rowWorkspace: string | null | undefined,
  intent: string | null | undefined,
): string {
  if (!bound) return intent ?? "";
  return rowWorkspace || defaultWorkspaceNow();
}