// omega webui: the chat composer's DISPLAY MODE (card t_5752861e).
// Three modes: "compact" - the current single-line grow shape (the card's
// 12px side inset); "wide" - wider composer bounds (the side inset
// tightens to 4px); "full" - full-width, edge to edge with no inset.
// "compact" is the default. Persisted in localStorage under
// omega.composer-mode (last choice wins; garbage restores the default -
// the parseAppearance/bottomWidth tolerance). Global for v1 - the card
// scopes per-workspace as fine-to-have later; the composer's shape is
// a browser preference, not session state.

import { ref, type Ref } from "vue";

export type ComposerMode = "compact" | "wide" | "full";

export const COMPOSER_MODE_KEY = "omega.composer-mode";
export const DEFAULT_COMPOSER_MODE: ComposerMode = "compact";

export function parseComposerMode(v: unknown): ComposerMode {
  return v === "wide" || v === "full" ? v : "compact";
}

// Module-scoped ref: the mode is read by the composer's class binding
// (AppComposer) and written by the settings knob (SettingsTab) - one
// shared signal, no prop-drilling (the bottomWidth.ts shape). Restored
// at SPA module load (browser); node tests keep the default.
const mode = ref<ComposerMode>(DEFAULT_COMPOSER_MODE);
if (typeof window !== "undefined") {
  mode.value = parseComposerMode(
    window.localStorage.getItem(COMPOSER_MODE_KEY),
  );
}

export function useComposerMode(): Ref<ComposerMode> {
  return mode;
}

export function setComposerMode(v: ComposerMode): void {
  mode.value = v;
  if (typeof window !== "undefined")
    window.localStorage.setItem(COMPOSER_MODE_KEY, v);
}
