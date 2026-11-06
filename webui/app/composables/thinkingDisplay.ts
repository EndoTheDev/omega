// omega webui: thinking display mode (card t_428ed4fb). The thinking
// block gets the tool rows' display knob (card t_868633b3): expanded
// (always open), collapsed (always shut - the one-line summary stays
// visible), auto - collapsed by DEFAULT, even while streaming; a
// manual summary click pins the block open for the session. Persisted
// in localStorage under omega.thinking-display (same pattern as
// omega.tool-display).

import { ref, type Ref } from "vue";

export type ThinkingDisplay = "expanded" | "collapsed" | "auto";

export const THINKING_DISPLAY_KEY = "omega.thinking-display";
export const DEFAULT_THINKING_DISPLAY: ThinkingDisplay = "auto";

// Anything but the exact non-default strings (missing key, older build,
// tampered value) falls back to the default mode, like parseToolDisplay.
export function parseThinkingDisplay(v: unknown): ThinkingDisplay {
  return v === "expanded" || v === "collapsed" ? v : "auto";
}

// thinkingWantOpen: the pure mode -> open mapping for a block the user
// has NOT pinned. Auto never opens on its own - collapsed while
// streaming and after; a summary click is the only path open. Pure.
export function thinkingWantOpen(mode: ThinkingDisplay): boolean {
  return mode === "expanded";
}

// Module-scoped mode ref: ThinkingBlock lives under ChatItem in every
// render site (chat chronolog, stacked blocks) - a shared ref reaches
// them all without prop-drilling the knob. Restored at SPA module load
// (browser); node tests keep the default.
const display = ref<ThinkingDisplay>(DEFAULT_THINKING_DISPLAY);
if (typeof window !== "undefined") {
  display.value = parseThinkingDisplay(
    window.localStorage.getItem(THINKING_DISPLAY_KEY),
  );
}

export function useThinkingDisplay(): Ref<ThinkingDisplay> {
  return display;
}
