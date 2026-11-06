// omega webui: the composer's display modes (card t_5752861e, the width
// ladder t_4a6431f8). Three modes - compact (the base card shape, the
// class binding's null: 12px inset, 720px cap - the narrow reading
// card), wide (mode-wide: the cap lifts to 1100px, inset 4px), full
// (mode-full: no cap, edge to edge, no inset, no radius). Inset-only
// deltas read as the same shape - each mode owns an upper WIDTH bound.
// The knob rides the shared-signal shape
// (bottomWidth.ts): SettingsTab writes, AppComposer reads, localStorage
// persists under omega.composer-mode. The fullscreen (.expanded) card
// geometry stays pinned by its own ruling - the modes hold only when
// NOT expanded. Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const src = (p: string) => readFileSync(join(here, "..", p), "utf8");

// --- the shared signal (real module: pure TS, node-safe) -------------
import {
  parseComposerMode,
  setComposerMode,
  useComposerMode,
  DEFAULT_COMPOSER_MODE,
  COMPOSER_MODE_KEY,
  type ComposerMode,
} from "../app/composables/composerMode.ts";

describe("composerMode composable", () => {
  it("defaults to compact", () => {
    assert.equal(DEFAULT_COMPOSER_MODE, "compact");
    assert.equal(COMPOSER_MODE_KEY, "omega.composer-mode");
  });

  it("parse tolerates garbage - anything but wide/full is compact", () => {
    assert.equal(parseComposerMode("compact"), "compact");
    assert.equal(parseComposerMode("wide"), "wide");
    assert.equal(parseComposerMode("full"), "full");
    assert.equal(parseComposerMode(null), "compact");
    assert.equal(parseComposerMode(undefined), "compact");
    assert.equal(parseComposerMode(42), "compact");
    assert.equal(parseComposerMode("FULL"), "compact");
    assert.equal(parseComposerMode("aligned"), "compact");
  });

  it("the module ref writes and reads through the setter (no window in node: no storage crash)", () => {
    const mode = useComposerMode();
    assert.equal(parseComposerMode(mode.value), "compact");
    setComposerMode("wide");
    assert.equal(mode.value, "wide");
    setComposerMode("full");
    assert.equal(mode.value, "full");
    setComposerMode("compact");
    assert.equal(mode.value, "compact");
  });
});

// --- source contracts -------------------------------------------------

describe("composerMode wiring", () => {
  const composer = src("app/components/chat/AppComposer.vue");
  const settings = src("app/components/settings/SettingsTab.vue");
  const css = src("app/assets/css/main.css");

  it("AppComposer binds the mode class beside the expanded flag", () => {
    assert.match(
      composer,
      /:class="\[composerModeClass, \{ expanded: expanded \}\]"/,
    );
    assert.match(composer, /import .*composables\/composerMode/);
    assert.match(composer, /return m === "compact" \? null : `mode-\$\{m\}`/);
  });

  it("SettingsTab carries the chat.composer_mode client knob and writes the signal", () => {
    assert.match(settings, /key: "chat\.composer_mode"/);
    assert.match(settings, /options: \["compact", "wide", "full"\]/);
    assert.match(settings, /setComposerMode\(value as ComposerMode\)/);
    assert.match(settings, /if \(knob\.key === "chat\.composer_mode"\) return composerMode\.value/);
  });

  it("the CSS carries both non-compact shapes, gated to NOT expanded", () => {
    assert.match(css, /\.composer\.mode-wide:not\(\.expanded\)/);
    assert.match(css, /\.composer\.mode-full:not\(\.expanded\)/);
  });

  it("the mode ladder owns a WIDTH bound per mode - inset-only deltas are dead (t_4a6431f8)", () => {
    const base = css.match(/\.composer \{([^}]*)\}/)?.[1] ?? "";
    assert.match(base, /max-width: 720px/); // compact: the narrow reading card
    const wide = css.match(/\.composer\.mode-wide:not\(\.expanded\) \{([^}]*)\}/)?.[1] ?? "";
    assert.match(wide, /max-width: 1100px/);
    const full = css.match(/\.composer\.mode-full:not\(\.expanded\) \{([^}]*)\}/)?.[1] ?? "";
    assert.match(full, /max-width: none/);
    const expanded = css.match(/\.composer\.expanded \{([^}]*)\}/)?.[1] ?? "";
    assert.match(expanded, /max-width: none/); // fullscreen ignores the cap
  });
});
