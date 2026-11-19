// omega webui: the loaded server.default_workspace as ONE shared value. The chat host
// reads it when a new session's draft state is born, so the composer's workspace chip
// carries the default exactly as if the user had picked it.

// A plain module value, not a ref: the getter is consulted ONCE, at ensureState time.

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

// effectiveWorkdir is the never-blank law
// of the composer chip into the status bar, ). The folder
// the tool runs root at is what the workdir segment must show:
// an UNBOUND tab shows its draft workspace intent (the seed IS the
//     configured default, so a fresh draft already carries it);
// a BOUND session with an EMPTY row pin has its runs rooted at the
// server's default_workspace, so the segment falls back to it
//     never a blank that hides the folder the commands run.
export function effectiveWorkdir(
  bound: boolean,
  rowWorkspace: string | null | undefined,
  intent: string | null | undefined,
): string {
  if (!bound) return intent ?? "";
  return rowWorkspace || defaultWorkspaceNow();
}