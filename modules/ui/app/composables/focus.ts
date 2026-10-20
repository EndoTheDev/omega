// omega webui: composer auto-focus (card t_b40131ac). Two UX moves:
// (1) selecting a chat - a sidebar row, a dock tab click, a new chat -
// focuses the ACTIVE chat tab's composer; (2) window-level
// type-to-capture: with the omega window focused, a printable key with
// no modifiers jumps into the focused chat's composer, Slack-style.
// The rules that keep it annoying-free live here as pure predicates so
// the suite covers them: never steal from a focused text field, never
// intercept modifier combos or non-printables, only the ACTIVE tab's
// composer receives (a hidden tab's instance never matches its state
// key while hidden, so a request aimed there is simply not delivered).

import { ref, type InjectionKey, type Ref } from "vue";

// --- the focus signal -------------------------------------------------------
// app.vue watches its focused chat-tab key and calls request() on every
// chat selection and every type-to-capture keystroke; each mounted
// ChatTab holds this signal and delivers focus when the request names
// ITS state key (each pane renders only its ACTIVE tab, so a matching
// mounted instance is by construction the visible one). seq ticks on
// every request - a re-request of an ALREADY-focused tab (a second
// type-to-capture burst after a click blurred the textarea) must still
// deliver, so the consumers watch [focusKey, seq] and check the key.

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

// does a request deliver to the chat tab bound to stateKey? Takes the
// SIGNAL itself (focusKey is a Ref - compare .value, never the ref
// object: a ref === string comparison is always false and the feature
// ships dead, which no gate catches because vue builds strip types).
export function focusDelivers(
  req: { focusKey: Ref<string | null> },
  stateKey: string,
): boolean {
  return stateKey !== "" && req.focusKey.value === stateKey;
}

// --- the selectors ----------------------------------------------------------

// a printable, un-modified key: exactly one character in `key` (space
// included, shift+letter uppercase included) with no ctrl/alt/meta.
// Modifier combos stay browser/menu shortcuts; non-printables (Tab,
// Enter, arrows, F-keys, Escape, IME Process, Dead) go where they
// belong. The composer's own Enter-to-send keydown is untouched - it
// fires on the focused textarea, which the target check below never
// intercepts anyway.
export function isTypeToCaptureKey(e: {
  key: string;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
}): boolean {
  if (e.ctrlKey || e.altKey || e.metaKey) return false;
  return e.key.length === 1;
}

// nothing gets stolen while the user is typing somewhere visible: any
// text-entry element keeps the keystroke - settings search, session
// search, a native file dialog's hidden input, and the composer's own
// textarea (whose input event flows naturally).
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

// the full capture gate for one keydown: a modal is up (the delete
// confirm - its keystrokes belong to the dialog), a non-chat view is
// showing (board/settings never host the composer), or text is already
// being typed somewhere -> no capture.
export function shouldCaptureTyping(
  e: KeyboardEvent,
  gate: { mainView: string; modalOpen: boolean },
): boolean {
  if (gate.modalOpen || gate.mainView !== "chat") return false;
  if (isEditableTarget(e.target as HTMLElement | null)) return false;
  return isTypeToCaptureKey(e);
}