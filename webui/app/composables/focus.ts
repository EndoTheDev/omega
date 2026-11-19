// composer auto-focus. The rules that keep it annoyance-free live here as pure predicates:
// never steal from a focused text field, never intercept modifier combos or non-printables,
// and only the ACTIVE tab's composer receives.

import { ref, type InjectionKey, type Ref } from "vue";

// the focus signal. seq ticks on every request, so a re-request of an ALREADY-focused tab (a
// second type-to-capture burst after a click blurred the textarea) still delivers - consumers
// watch [focusKey, seq] and check the key.

export interface ComposerFocusSignal {
  focusKey: Ref<string | null>;
  seq: Ref<number>;
  request(key: string): void;
}

export function createComposerFocusSignal(): ComposerFocusSignal {
  const focusKey = ref<string | null>(null);
  const seq = ref(0);
  return {
    focusKey,
    seq,
    request(key: string) {
      focusKey.value = key;
      seq.value++;
    },
  };
}

export const COMPOSER_FOCUS_KEY: InjectionKey<ComposerFocusSignal> =
  Symbol("composer-focus");

// Compare .value, never the ref object: a ref === string comparison is always false and the
// feature ships dead, which no gate catches because vue builds strip types.
export function focusDelivers(
  req: { focusKey: Ref<string | null> },
  stateKey: string,
): boolean {
  return stateKey !== "" && req.focusKey.value === stateKey;
}

// a printable, un-modified key: exactly one character in `key` (space included). The
// composer's own Enter-to-send fires on the focused textarea, which the target check skips.
export function isTypeToCaptureKey(e: {
  key: string;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
}): boolean {
  if (e.ctrlKey || e.altKey || e.metaKey) return false;
  return e.key.length === 1;
}

export function isEditableTarget(
  t: { tagName?: string; isContentEditable?: boolean } | null,
): boolean {
  if (!t) return false;
  const tag = t.tagName ?? "";
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    t.isContentEditable === true
  );
}

// the full capture gate: a modal is up (its keystrokes belong to the dialog), a non-chat view
// is showing, or text is already being typed somewhere -> no capture.
export function shouldCaptureTyping(
  e: KeyboardEvent,
  gate: { mainView: string; modalOpen: boolean },
): boolean {
  if (gate.modalOpen || gate.mainView !== "chat") return false;
  if (isEditableTarget(e.target as HTMLElement | null)) return false;
  return isTypeToCaptureKey(e);
}