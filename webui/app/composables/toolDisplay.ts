// omega webui: tool-result display mode. Persisted in localStorage under
// omega.tool-display (same pattern as omega.appearance); a manual summary toggle pins a
// row's state for the session - the mode recomputes around pinned rows, never over
// them.

import { ref, type Ref } from "vue";

import { isLive } from "./toolStatus.ts";

export type ToolDisplay = "expanded" | "collapsed" | "auto";

export const TOOL_DISPLAY_KEY = "omega.tool-display";
export const DEFAULT_TOOL_DISPLAY: ToolDisplay = "auto";

// Anything but the exact non-default strings.
export function parseToolDisplay(v: unknown): ToolDisplay {
  return v === "expanded" || v === "collapsed" ? v : "auto";
}

// toolRowWantOpen: the pure mode+status -> open mapping for a row the user has NOT.
export function toolRowWantOpen(status: string, mode: ToolDisplay): boolean {
  if (mode === "expanded") return true;
  if (mode === "collapsed") return false;
  return isLive(status);
}

// Module-scoped mode ref: ToolCallLine rows live under ChatItem in every render site.
const display = ref<ToolDisplay>(DEFAULT_TOOL_DISPLAY);
if (typeof window !== "undefined") {
  display.value = parseToolDisplay(
    window.localStorage.getItem(TOOL_DISPLAY_KEY),
  );
}

export function useToolDisplay(): Ref<ToolDisplay> {
  return display;
}