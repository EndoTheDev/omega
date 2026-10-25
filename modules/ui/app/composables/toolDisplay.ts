// omega webui: tool-result display mode (/tools auto parity, card
// t_868633b3). Three modes: expanded (every row open), collapsed (every
// row shut), auto - the TUI default where a running call expands and a
// settled one collapses. Persisted in localStorage under
// omega.tool-display (same pattern as omega.appearance); a manual
// summary toggle pins a row's state for the session - the mode
// recomputes around pinned rows, never over them.

import { ref, type Ref } from "vue";

export type ToolDisplay = "expanded" | "collapsed" | "auto";

export const TOOL_DISPLAY_KEY = "omega.tool-display";
export const DEFAULT_TOOL_DISPLAY: ToolDisplay = "auto";

// Anything but the exact non-default strings (missing key, older build,
// tampered value) falls back to the default mode, like parseAppearance.
export function parseToolDisplay(v: unknown): ToolDisplay {
  return v === "expanded" || v === "collapsed" ? v : "auto";
}

// toolRowWantOpen: the pure mode+status -> open mapping for a row the
// user has NOT pinned. Auto = the TUI behavior: a running call is open
// (you watch it work), a settled one is closed (the result collapses).
// Pure.
export function toolRowWantOpen(status: string, mode: ToolDisplay): boolean {
  if (mode === "expanded") return true;
  if (mode === "collapsed") return false;
  return status === "running";
}

// Module-scoped mode ref: ToolCallLine rows live under ChatItem in every
// render site (chat chronolog, stacked blocks, the pinned live header) -
// a shared ref reaches them all without prop-drilling the knob through
// four component layers. Restored at SPA module load (browser); node
// tests keep the default.
const display = ref<ToolDisplay>(DEFAULT_TOOL_DISPLAY);
if (typeof window !== "undefined") {
  display.value = parseToolDisplay(
    window.localStorage.getItem(TOOL_DISPLAY_KEY),
  );
}

export function useToolDisplay(): Ref<ToolDisplay> {
  return display;
}