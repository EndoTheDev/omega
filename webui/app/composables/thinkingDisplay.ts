// omega webui: thinking display mode.

import { ref, type Ref } from "vue";

export type ThinkingDisplay = "expanded" | "collapsed" | "auto";

export const THINKING_DISPLAY_KEY = "omega.thinking-display";
export const DEFAULT_THINKING_DISPLAY: ThinkingDisplay = "auto";

// Anything but the exact non-default strings.
export function parseThinkingDisplay(v: unknown): ThinkingDisplay {
  return v === "expanded" || v === "collapsed" ? v : "auto";
}

// thinkingWantOpen: the pure mode -> open mapping for a block the user has NOT pinned.
// Auto never opens on its own - collapsed while streaming and after; a summary click is
// the only path open.
export function thinkingWantOpen(mode: ThinkingDisplay): boolean {
  return mode === "expanded";
}

// Module-scoped mode ref: ThinkingBlock lives under ChatItem in every render site.
const display = ref<ThinkingDisplay>(DEFAULT_THINKING_DISPLAY);
if (typeof window !== "undefined") {
  display.value = parseThinkingDisplay(
    window.localStorage.getItem(THINKING_DISPLAY_KEY),
  );
}

export function useThinkingDisplay(): Ref<ThinkingDisplay> {
  return display;
}
