// omega webui: the chat composer's DISPLAY MODE (card t_5752861e,
// reshape t_f9fd3820, v4 t_d14c2d14 - the three-mode set is retired).
// Exactly TWO modes: "compact" (sketch C5 'footer-send') - the centered
// narrow card in TWO rows: plus-btn + input on top, chips left +
// send right below, a hairline between the rows; "full" (sketch W6
// 'segmented', the old 'wide' renamed) - ONE row spanning the pane
// width with hairline segments: input | chips | send. The ladder-era
// modes are dead ('wide' and the edge-to-edge 'full'): a stored 'wide'
// migrates to 'full' (the rewrite also lands in localStorage so the
// stale value never returns), garbage restores the default 'compact'.
// "compact" is the default. Persisted in localStorage under
// omega.composer-mode (last choice wins). Global for v1 - the card
// scopes per-workspace as fine-to-have later; the composer's shape is
// a browser preference, not session state.

import { ref, type Ref } from "vue";

export type ComposerMode = "compact" | "full";

export const COMPOSER_MODE_KEY = "omega.composer-mode";
export const DEFAULT_COMPOSER_MODE: ComposerMode = "compact";

export function parseComposerMode(v: unknown): ComposerMode {
  if (v === "compact") return "compact";
  // 'wide' is the retired value's rename - it loads as 'full' (the old
  // edge-to-edge geometry is gone; the single-row bar owns the name)
  if (v === "wide" || v === "full") return "full";
  return "compact";
}

// Module-scoped ref: the mode is read by the composer's class binding
// (AppComposer) and written by the settings knob (SettingsTab) - one
// shared signal, no prop-drilling (the bottomWidth.ts shape). Restored
// at SPA module load (browser); node tests keep the default.
const mode = ref<ComposerMode>(DEFAULT_COMPOSER_MODE);
if (typeof window !== "undefined") {
  const stored = window.localStorage.getItem(COMPOSER_MODE_KEY);
  mode.value = parseComposerMode(stored);
  // the stored-value migration (t_d14c2d14): a legacy 'wide' pick
  // lands as 'full' AND the stored key is rewritten, so the stale
  // 'wide' string never resurfaces in future reads
  if (stored === "wide")
    window.localStorage.setItem(COMPOSER_MODE_KEY, "full");
}

export function useComposerMode(): Ref<ComposerMode> {
  return mode;
}

export function setComposerMode(v: ComposerMode): void {
  mode.value = v;
  if (typeof window !== "undefined")
    window.localStorage.setItem(COMPOSER_MODE_KEY, v);
}
