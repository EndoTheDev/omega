// omega webui: the bottom panel's WIDTH MODE (card t_d7fd3ad3 item 6).
// Two modes: "aligned" - the bottom panel's width tracks the main panes'
// bounding box (the VS Code panel read: the sidebars stand clear of the
// panel); "full" - the panel spans the whole dock area (the pre-card
// behavior). "aligned" is the default. Persisted in localStorage under
// omega.dock-bottom-width-mode (last choice wins; garbage restores the
// default - the parseAppearance/toolDisplay tolerance).

import { ref, type Ref } from "vue";

export type BottomWidthMode = "aligned" | "full";

export const BOTTOM_WIDTH_KEY = "omega.dock-bottom-width-mode";
export const DEFAULT_BOTTOM_WIDTH_MODE: BottomWidthMode = "aligned";

export function parseBottomWidthMode(v: unknown): BottomWidthMode {
  return v === "full" ? "full" : "aligned";
}

// Module-scoped ref: the mode is read by the applier (app.vue's geometry
// pass) and written by the settings knob - one shared signal, no
// prop-drilling. Restored at SPA module load (browser); node tests keep
// the default.
const mode = ref<BottomWidthMode>(DEFAULT_BOTTOM_WIDTH_MODE);
if (typeof window !== "undefined") {
  mode.value = parseBottomWidthMode(
    window.localStorage.getItem(BOTTOM_WIDTH_KEY),
  );
}

export function useBottomWidthMode(): Ref<BottomWidthMode> {
  return mode;
}

export function setBottomWidthMode(v: BottomWidthMode): void {
  mode.value = v;
  if (typeof window !== "undefined")
    window.localStorage.setItem(BOTTOM_WIDTH_KEY, v);
}