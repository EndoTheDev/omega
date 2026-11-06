// omega webui: the composer's display modes (card t_5752861e, v4
// t_d14c2d14 - the three-mode set is retired). Exactly TWO modes -
// compact (mode-compact: the C5 'footer-send' card - plus/input on top,
// chips left + send right on a bottom row, a hairline between the rows,
// centered via auto margins with the 720px cap) and full (mode-full:
// the W6 'segmented' one-row bar - the old 'wide' renamed, input |
// chips | send hairline segments, the pane width with the card's 12px
// inset). The knob rides the shared-signal shape (bottomWidth.ts):
// SettingsTab writes, AppComposer reads, localStorage persists under
// omega.composer-mode, a stored 'wide' migrates to 'full'. The
// fullscreen (.expanded) card geometry stays pinned by its own ruling -
// the modes hold only when NOT expanded. Render-free; node's built-in
// test runner.
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

  it("parse is the migration: 'wide' loads as 'full', garbage is compact", () => {
    assert.equal(parseComposerMode("compact"), "compact");
    assert.equal(parseComposerMode("wide"), "full"); // the retired rename
    assert.equal(parseComposerMode("full"), "full");
    assert.equal(parseComposerMode(null), "compact");
    assert.equal(parseComposerMode(undefined), "compact");
    assert.equal(parseComposerMode(42), "compact");
    assert.equal(parseComposerMode("FULL"), "compact");
    assert.equal(parseComposerMode("aligned"), "compact");
    // the mode set is exactly these two strings anywhere in the composable
    const moduleSrc = src("app/composables/composerMode.ts");
    assert.doesNotMatch(moduleSrc, /"compact" \| "wide" \| "full"/);
  });

  it("the module ref writes and reads through the setter (no window in node: no storage crash)", () => {
    const mode = useComposerMode();
    assert.equal(parseComposerMode(mode.value), "compact");
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
    assert.match(composer, /return `mode-\$\{parseComposerMode\(composerMode\.value\)\}`/);
    // the retired three-mode branch is gone
    assert.doesNotMatch(composer, /m === "wide"/);
  });

  it("the W6 chips|send segment rides a real seg-div element (full, not expanded)", () => {
    assert.match(
      composer,
      /v-if="!expanded && isFull"\s+class="seg-div"/,
    );
    assert.match(css, /\.composer \.seg-div \{[^}]*width: 1px;/);
    assert.match(css, /\.composer \.seg-div \{[^}]*height: 26px;/);
  });

  it("SettingsTab carries the chat.composer_mode client knob with exactly the two values", () => {
    assert.match(settings, /key: "chat\.composer_mode"/);
    assert.match(settings, /options: \["compact", "full"\]/);
    assert.doesNotMatch(settings, /"compact", "wide"/);
    assert.match(settings, /setComposerMode\(value as ComposerMode\)/);
    assert.match(settings, /if \(knob\.key === "chat\.composer_mode"\) return composerMode\.value/);
  });

  it("the CSS carries both modifier shapes, gated to NOT expanded", () => {
    assert.match(css, /\.composer\.mode-compact:not\(\.expanded\)/);
    assert.match(css, /\.composer\.mode-full:not\(\.expanded\)/);
    assert.doesNotMatch(css, /mode-wide/); // the ladder is retired
  });

  it("compact is centered AND two rows: input top, chips+send bottom on a hairline", () => {
    const compact = css.match(/\.composer\.mode-compact:not\(\.expanded\) \{([^}]*)\}/)?.[1] ?? "";
    assert.match(compact, /max-width: 720px/); // the current card width treatment
    assert.match(compact, /margin: 0 auto 12px/); // centered in the pane
    assert.doesNotMatch(compact, /flex-direction/); // the base column makes the two rows
    const toolbar = css.match(
      /\.composer\.mode-compact:not\(\.expanded\) \.toolbar \{([^}]*)\}/,
    )?.[1] ?? "";
    assert.match(toolbar, /flex-wrap: nowrap/); // the bottom row never wraps
    assert.match(toolbar, /border-top: 1px solid var\(--stroke\)/); // the C5 hairline
    // send right + chips left on row 2 come from the base .send margin
    const send = css.match(/\.send \{([^}]*)\}/)?.[1] ?? "";
    assert.match(send, /margin-left: auto/);
  });

  it("full is ONE row: the base card's inset, hairline segments (input | chips | send)", () => {
    const full = css.match(/\.composer\.mode-full:not\(\.expanded\) \{([^}]*)\}/)?.[1] ?? "";
    assert.match(full, /flex-direction: row/); // ONE segmented row
    assert.match(full, /align-items: center/);
    assert.doesNotMatch(full, /border-radius: 0/); // the padded card keeps its radius
    const row = css.match(
      /\.composer\.mode-full:not\(\.expanded\) \.composer-row \{([^}]*)\}/,
    )?.[1] ?? "";
    assert.match(row, /flex: 1/); // the input's segment grows
    const toolbar = css.match(
      /\.composer\.mode-full:not\(\.expanded\) \.toolbar \{([^}]*)\}/,
    )?.[1] ?? "";
    assert.match(toolbar, /flex-wrap: nowrap/);
    assert.match(toolbar, /border-left: 1px solid var\(--stroke\)/); // input|chips hairline
  });

  it("the fullscreen card ignores the caps (expanded pinned by its own ruling)", () => {
    const expanded = css.match(/\.composer\.expanded \{([^}]*)\}/)?.[1] ?? "";
    assert.match(expanded, /max-width: none/);
  });
});
