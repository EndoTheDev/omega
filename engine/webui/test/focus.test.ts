// omega webui: unit tests for card t_b40131ac (composer auto-focus):
// focus-on-select + window type-to-capture. The gating rules are pure
// predicates in composables/focus.ts - no steal from a focused text
// field, no modifier / non-printable interception, signal delivery to
// the matching active-tab key only.
// Run: node --test test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { ref } from "vue";

import {
  COMPOSER_FOCUS_KEY,
  createComposerFocusSignal,
  focusDelivers,
  isEditableTarget,
  isTypeToCaptureKey,
  shouldCaptureTyping,
} from "../app/composables/focus.ts";

// --- type-to-capture: the key filter ----------------------------------------

describe("isTypeToCaptureKey", () => {
  const plain = (key: string) => ({ key, ctrlKey: false, altKey: false, metaKey: false });

  it("accepts printable single-character keys", () => {
    assert.equal(isTypeToCaptureKey(plain("a")), true);
    assert.equal(isTypeToCaptureKey(plain("A")), true, "shift+typed letters are printable");
    assert.equal(isTypeToCaptureKey(plain(" ")), true, "space types too");
    assert.equal(isTypeToCaptureKey(plain("1")), true);
    assert.equal(isTypeToCaptureKey(plain("ä")), true, "non-ascii printables are printable");
    assert.equal(isTypeToCaptureKey(plain("?")), true);
  });

  it("rejects modifier combos - they stay browser/menu shortcuts", () => {
    const ctrl = { ...plain("c"), ctrlKey: true };
    const meta = { ...plain("r"), metaKey: true };
    const alt = { ...plain("ArrowLeft"), altKey: true };
    assert.equal(isTypeToCaptureKey(ctrl), false, "ctrl+c never captures");
    assert.equal(isTypeToCaptureKey(meta), false);
    assert.equal(isTypeToCaptureKey(alt), false);
  });

  it("rejects non-printable keys", () => {
    for (const key of ["Tab", "Enter", "Backspace", "Delete", "Escape",
      "ArrowUp", "ArrowDown", "F5", "Shift", "Control", "Dead", "Process"]) {
      assert.equal(isTypeToCaptureKey(plain(key)), false, key);
    }
  });
});

// --- type-to-capture: the target filter -------------------------------------

describe("isEditableTarget", () => {
  const tag = (name: string, editable = false) =>
    ({ tagName: name, isContentEditable: editable });

  it("keeps the keystroke in any text-entry element", () => {
    assert.equal(isEditableTarget(tag("INPUT")), true);
    assert.equal(isEditableTarget(tag("TEXTAREA")), true);
    assert.equal(isEditableTarget(tag("SELECT")), true);
    assert.equal(isEditableTarget(tag("DIV", true)), true, "contenteditable steals nothing");
    assert.equal(isEditableTarget(tag("input")), false, "case-sensitive: real DOM tags are upper");
  });

  it("does not treat the rest of the page as editable", () => {
    assert.equal(isEditableTarget(tag("BODY")), false);
    assert.equal(isEditableTarget(tag("BUTTON")), false);
    assert.equal(isEditableTarget(tag("P")), false);
    assert.equal(isEditableTarget(null), false, "no target: nothing owns focus");
  });
});

// --- the full capture gate ---------------------------------------------------

describe("shouldCaptureTyping", () => {
  const evt = (key: string, target: { tagName?: string; isContentEditable?: boolean } | null) =>
    ({ key, ctrlKey: false, altKey: false, metaKey: true, target }) as unknown as KeyboardEvent;

  it("captures a printable key into a non-editable target on chat", () => {
    assert.equal(
      shouldCaptureTyping({ key: "h", ctrlKey: false, altKey: false, metaKey: false,
        target: { tagName: "BODY" } } as KeyboardEvent, { mainView: "chat", modalOpen: false }),
      true,
    );
  });

  it("never steals while another input is focused", () => {
    for (const target of [{ tagName: "INPUT" }, { tagName: "TEXTAREA" },
      { tagName: "SELECT" }, { tagName: "DIV", isContentEditable: true }]) {
      assert.equal(
        shouldCaptureTyping({ key: "h", ctrlKey: false, altKey: false, metaKey: false,
          target } as KeyboardEvent, { mainView: "chat", modalOpen: false }),
        false,
      );
    }
  });

  it("does not intercept under a modal or off the chat view", () => {
    const ev = { key: "h", ctrlKey: false, altKey: false, metaKey: false,
      target: { tagName: "BODY" } } as KeyboardEvent;
    assert.equal(shouldCaptureTyping(ev, { mainView: "chat", modalOpen: true }), false,
      "the delete confirm owns its keystrokes");
    assert.equal(shouldCaptureTyping(ev, { mainView: "board", modalOpen: false }), false);
  });

  it("modifier combos never capture no matter where focus sits", () => {
    assert.equal(
      shouldCaptureTyping(evt("c", { tagName: "BODY" }), { mainView: "chat", modalOpen: false }),
      false,
    );
  });
});

// --- the focus signal ---------------------------------------------------------

describe("composer focus signal", () => {
  it("request names the key and ticks the sequence every time", () => {
    const s = createComposerFocusSignal();
    assert.equal(s.focusKey.value, null);
    s.request("c1");
    assert.equal(s.focusKey.value, "c1");
    assert.equal(s.seq.value, 1);
    // a re-request of the SAME key must still deliver: a second
    // type-to-capture burst after a click blurred the textarea
    s.request("c1");
    assert.equal(s.seq.value, 2);
    s.request("c2");
    assert.equal(s.focusKey.value, "c2");
    assert.equal(s.seq.value, 3);
  });

  it("focusDelivers only to the matching, keyed tab", () => {
    const s = createComposerFocusSignal();
    s.request("c1");
    // production passes the SIGNAL: focusKey is a Ref. Tests that
    // unwrap .value here validate a shape the app never sends.
    assert.equal(focusDelivers(s, "c1"), true);
    assert.equal(focusDelivers(s, "c2"), false, "hidden/other tabs never receive");
    assert.equal(focusDelivers({ focusKey: ref("c9") }, ""), false, "a keyless tab receives nothing");
    assert.equal(focusDelivers({ focusKey: ref(null) }, "c1"), false);
  });

  it("the injection key exists (app provides, ChatTab injects)", () => {
    assert.equal(typeof COMPOSER_FOCUS_KEY, "symbol");
  });
});