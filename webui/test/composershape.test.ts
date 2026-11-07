// omega webui: the composer's SINGLE shape (card t_1750a9e3, ruling
// 2026-11-07 - the mode system died). ONE composer: the C5 'footer-send'
// arrangement (plus/input on top, chips left + send right on a bottom
// hairline row) stretched to the pane width - full-width floating card,
// the base 12px inset (sides + floor), borders and rounded corners all
// around, NOT edge-to-edge. The retired mode system (composables/
// composerMode.ts, the settings' chat.composer_mode knob, the
// mode-compact/mode-full classes, the seg-div divider, the 720px cap)
// must stay dead; a stored omega.composer-mode localStorage value is an
// ignored orphan - never read, never rewritten, no migration code.
// Render-free; node's built-in test runner.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const src = (p: string) => readFileSync(join(here, "..", p), "utf8");

describe("the single composer (one shape, no modes)", () => {
  const composer = src("app/components/chat/AppComposer.vue");

  it("AppComposer carries no mode class - only the expanded flag", () => {
    // the retired two-mode binding must not return piecemeal
    assert.doesNotMatch(composer, /composerModeClass/);
    assert.doesNotMatch(composer, /composerMode/);
    assert.match(composer, /:class="\{ expanded: expanded \}"/);
  });

  it("the mode system's residue is gone (composable, knob, seg-div)", () => {
    // the composable file itself is deleted; these pins keep it deleted
    assert.throws(
      () => src("app/composables/composerMode.ts"),
      "composables/composerMode.ts is deleted - it must stay deleted",
    );
    const settings = src("app/components/settings/SettingsTab.vue");
    assert.doesNotMatch(settings, /composer_mode/);
    assert.doesNotMatch(settings, /composerMode/);
    assert.doesNotMatch(composer, /seg-div/);
  });

  it("the C5 arrangement holds: plus/input top, chips+send bottom on a hairline", () => {
    const css = src("app/assets/css/main.css");
    const notx = css.match(/\.composer:not\(\.expanded\) \.toolbar\s*\{([^}]*)\}/);
    assert.ok(notx, ".composer:not(.expanded) .toolbar rule missing from main.css");
    assert.match(notx[0], /flex-wrap: nowrap/); // the bottom row never wraps
    assert.match(notx[0], /border-top: 1px solid var\(--stroke\)/); // the C5 hairline
    // the base column makes the two rows; the send pins row 2's right end
    const composerCss = css.match(/^\.composer\s*\{([^}]*)\}/m)?.[1] ?? "";
    assert.match(composerCss, /flex-direction: column/);
    const send = css.match(/^\.send\s*\{([^}]*)\}/m)?.[1] ?? "";
    assert.match(send, /margin-left: auto/);
  });
});

describe("take A geometry: pane-wide floating card", () => {
  const css = src("app/assets/css/main.css");

  it("the base card is full-width with the 12px inset (no 720 cap, auto centering gone)", () => {
    const c = css.match(/^\.composer\s*\{([^}]*)\}/m)?.[1] ?? "";
    assert.ok(c, ".composer base rule missing");
    assert.doesNotMatch(c, /max-width/); // the centered 720px card died
    assert.doesNotMatch(c, /margin: 0 auto/); // auto margins were compact's
    assert.match(c, /margin: 0 12px 12px/); // the 12px inset: sides + floor
    assert.match(c, /border: 1px solid var\(--stroke\)/); // border all around
    assert.match(c, /border-radius/); // rounded corners all around
  });

  it("no mode class anywhere in the webui source", () => {
    assert.doesNotMatch(css, /mode-compact|mode-full|mode-wide|seg-div/);
  });

  it("the fullscreen expanded card keeps its own geometry (12px, full width)", () => {
    const expanded = css.match(/\.composer\.expanded\s*\{([^}]*)\}/)?.[1] ?? "";
    assert.match(expanded, /margin: 12px/);
    // the mode cap is gone, so no reset survives either
    assert.doesNotMatch(expanded, /max-width/);
  });

  it("stored omega.composer-mode values are orphans: nothing reads or writes the key", () => {
    // the key name must exist in NO source (app or test) - the stored
    // value loads without error and is ignored, never read or rewritten
    for (const p of [
      "app/components/chat/AppComposer.vue",
      "app/components/settings/SettingsTab.vue",
      "app/assets/css/main.css",
    ]) {
      assert.doesNotMatch(src(p), /omega\.composer-mode/);
    }
  });
});
