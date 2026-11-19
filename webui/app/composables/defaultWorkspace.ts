// the loaded server.default_workspace as ONE shared plain module value (not a ref - the
// getter is consulted ONCE, at ensureState time), so a new session's draft chip carries it.

import { reactive } from "vue";

/** the /settings knob key */
export const DEFAULT_WORKSPACE_KNOB = "server.default_workspace";

export function normalizeDefaultWorkspace(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

const signal = reactive({ value: "" });

export function defaultWorkspaceNow(): string {
  return signal.value;
}

export function setDefaultWorkspace(v: unknown): void {
  signal.value = normalizeDefaultWorkspace(v);
}

// the never-blank law of the composer chip's status segment: an UNBOUND tab shows its
// draft intent (the seed IS the configured default), and a BOUND session with an EMPTY row
// pin is rooted at the server's default_workspace - never a blank hiding the real folder.
export function effectiveWorkdir(
  bound: boolean,
  rowWorkspace: string | null | undefined,
  intent: string | null | undefined,
): string {
  if (!bound) return intent ?? "";
  return rowWorkspace || defaultWorkspaceNow();
}