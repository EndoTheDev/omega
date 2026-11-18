// omega webui: the composer's squeeze ladder. The law now: a label fits WHOLE or is
// deliberately ABSENT (never an ellipsis), and the row gives way in ONE measured order
// - full label -> chevron only -> stacked rows. These tests are the budgets, the ORDER
// and the source contract.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  composerStageBands,
  composerStageFor,
  LADDER_SLACK_PX,
  MODEL_CHIP_FULL_CHROME_PX,
  MODEL_CHIP_ICON_PX,
  MODEL_GROUP_GAP_PX,
  TOOLBAR_GAP_PX,
  TOOLBAR_PAD_PX,
  type ComposerRowCosts,
  type ComposerStage,
} from "../app/composables/composerladder.ts";

const here = dirname(fileURLToPath(import.meta.url));
const src = (p: string) => readFileSync(join(here, "..", p), "utf8");

// the measured row: the label's own 136.8px, the effort chip's 92.4px, the send's.
const MEASURED: ComposerRowCosts = {
  label: 136.8,
  effort: 92.4,
  rest: 62.3,
  gaps: 1,
};

const round1 = (n: number) => Math.round(n * 10) / 10;

describe("the ladder's measured chrome (the numbers, not a guess)", () => {
  it("the chip chrome recomputes from the CSS box it stands for", () => {
    assert.equal(MODEL_CHIP_FULL_CHROME_PX, 14 + 14 + 6 + 6 + 2);
    assert.equal(MODEL_CHIP_ICON_PX, 14 + 14 + 6 + 2);
    assert.equal(MODEL_GROUP_GAP_PX, 8);
    assert.equal(TOOLBAR_GAP_PX, 8);
    assert.equal(TOOLBAR_PAD_PX, 8);
    assert.equal(LADDER_SLACK_PX, 8);
  });

  it("the three band edges match the documented arithmetic", () => {
    const bands = composerStageBands(MEASURED);
    // full: 136.8 + 42 + 8 + 92.4 + 62.3 + (8 gap + 16 pad + 8 slack) = 373.5
    assert.equal(round1(bands.full), 373.5);
    // icon: 36.0 + 8 + 92.4 + 62.3 + 32 = 230.7
    assert.equal(round1(bands.icon), 230.7);
    // stack: max(36 + 8 + 92.4, 62.3) + 16 + 8 = 160.4
    assert.equal(round1(bands.stacked), 160.4);
  });

  it("the ladder ENDS at the stacked rung - omega needs no fourth", () => {
    const bands = composerStageBands(MEASURED);
    // the narrowest real composer.
    const narrowestPane = 200 - 24 - 2;
    assert.equal(narrowestPane, 174);
    assert.ok(
      narrowestPane >= bands.stacked,
      `the stacked row must fit a 200px pane (${narrowestPane} >= ${bands.stacked})`,
    );
    assert.equal(composerStageFor(narrowestPane, MEASURED), "stacked");
  });
});

describe("the ladder's order: full -> icon -> stacked, one rung per band", () => {
  const bands = composerStageBands(MEASURED);
  const rung = { full: 0, icon: 1, stacked: 2 } as const;
  const sweep = Array.from({ length: 1001 }, (_, i) => 140 + i);

  it("a wider composer never runs a narrower rung", () => {
    let prev = rung[composerStageFor(sweep[0]!, MEASURED)];
    for (const w of sweep) {
      const now = rung[composerStageFor(w, MEASURED)];
      assert.ok(
        now <= prev,
        `width ${w} climbed back to a wider rung (${prev} -> ${now})`,
      );
      prev = now;
    }
  });

  it("each rung owns exactly ONE contiguous band - never two firing at once", () => {
    const seen: ComposerStage[][] = [];
    let run: ComposerStage[] = [];
    let last: ComposerStage | null = null;
    for (const w of sweep) {
      const s = composerStageFor(w, MEASURED);
      if (s !== last) {
        if (run.length) seen.push(run);
        run = [s];
        last = s;
      } else {
        run.push(s);
      }
    }
    if (run.length) seen.push(run);
    assert.equal(
      seen.length,
      3,
      `expected three bands (stacked, icon, full), got ${seen.length}`,
    );
    assert.deepEqual(
      seen.map((b) => b[0]),
      ["stacked", "icon", "full"],
      "the bands run narrow -> wide in the ladder's own order",
    );
  });

  it("the edges are exactly where the arithmetic says", () => {
    assert.equal(composerStageFor(bands.full, MEASURED), "full");
    assert.equal(composerStageFor(bands.full - 1, MEASURED), "icon");
    assert.equal(composerStageFor(bands.icon, MEASURED), "icon");
    assert.equal(composerStageFor(bands.icon - 1, MEASURED), "stacked");
    // the maintainer's 294px pane: the composer is 294 - 24 - 2 = 268 wide there - the
    // shed rung, which is exactly the "deepse.." fix
    assert.equal(composerStageFor(268, MEASURED), "icon");
  });

  it("send is never stranded alone while the row still fits", () => {
    for (const w of sweep) {
      if (w < bands.icon) continue;
      assert.notEqual(
        composerStageFor(w, MEASURED),
        "stacked",
        `width ${w}: the one-row toolbar still fits, so the send must stay on it`,
      );
    }
  });
});

describe("the ladder follows the LABEL, not one baked-in model name", () => {
  const bands = composerStageBands(MEASURED);

  it("a longer label moves the full edge by exactly its own growth", () => {
    // "keypool:deepseek-v4.1-flash" = 60.2px more mono text (26 chars vs 18)
    const longer: ComposerRowCosts = {
      ...MEASURED,
      label: 26 * (136.8 / 18),
    };
    const grown = composerStageBands(longer);
    assert.equal(
      round1(grown.full - bands.full),
      round1(26 * (136.8 / 18) - 136.8),
    );
  });

  it("the shed and stacked bands do not move at all with the label", () => {
    const long = composerStageBands({ ...MEASURED, label: 420 });
    assert.equal(round1(long.icon), round1(bands.icon));
    assert.equal(round1(long.stacked), round1(bands.stacked));
  });

  it("a 400px composer sheds a long label instead of clipping it", () => {
    // full no longer fits at 400 once the label is 260px, so rung two runs
    assert.equal(composerStageFor(400, { ...MEASURED, label: 260 }), "icon");
  });
});

describe("the ladder's source contract", () => {
  const css = src("app/assets/css/main.css");
  const composer = src("app/components/chat/AppComposer.vue");
  const modelSelect = src("app/components/chat/ModelSelect.vue");

  it("the shed rung takes the label out of the ROW, never truncates it", () => {
    // rungs are CUMULATIVE: the stacked rung keeps the shed, or the label comes back.
    for (const rung of ["stage-icon", "stage-stacked"]) {
      const shed = css.match(
        new RegExp(`\\.composer\\.${rung}[^{]*\\.chip-name\\s*\\{([^}]*)\\}`),
      );
      assert.ok(shed, `the .composer.${rung} .chip-name rule is missing`);
      assert.match(shed[1]!, /visibility:\s*hidden/);
      assert.match(shed[1]!, /position:\s*absolute/);
      assert.doesNotMatch(
        shed[1]!,
        /text-overflow|overflow:\s*hidden/,
        "the shed rung must not truncate - the label is absent, whole",
      );
    }
  });

  it("no rule anywhere lets a composer label die mid-word", () => {
    // the composer's chips: never ellipsized, never capped into a clip
    const chipRules = [
      ...css.matchAll(/\.composer[^{]*\.chip-name\s*\{([^}]*)\}/g),
    ];
    assert.ok(chipRules.length > 0, "the composer's chip-name rules vanished");
    for (const rule of chipRules) {
      assert.doesNotMatch(rule[1]!, /text-overflow:\s*ellipsis/);
    }
    assert.match(
      css,
      /\.composer \.model-chip \.chip-name\s*\{[^}]*white-space:\s*nowrap/,
      "the composer's chip label is one line, whole",
    );
    const capped = css.match(
      /\.composer \.model-group \.model-select:not\(\.band-select\) \.model-chip\s*\{([^}]*)\}/,
    );
    assert.ok(capped, "the composer's model chip needs its own max-width rule");
    assert.match(capped[1]!, /max-width:\s*none/);
    const narrow = css.slice(css.lastIndexOf("@media (max-width: 768px)"));
    assert.doesNotMatch(narrow, /model-chip\s*\{[^}]*max-width:\s*96px/);
    assert.doesNotMatch(narrow, /model-chip[^}]*ellipsis/);
  });

  it("the stacked rung is a column: chips line one, send line two", () => {
    const stacked = css.match(/\.composer\.stage-stacked \.toolbar\s*\{([^}]*)\}/);
    assert.ok(stacked, "the .composer.stage-stacked .toolbar rule is missing");
    // a COLUMN, never a wrap: a wrapped row still fits its slack at the rung's own.
    assert.match(stacked[1]!, /flex-direction:\s*column/);
    assert.doesNotMatch(stacked[1]!, /flex-wrap:\s*wrap/);
    const send = css.match(/\.composer\.stage-stacked \.send\s*\{([^}]*)\}/);
    assert.ok(send, "the send keeps its right anchor in the stacked rung");
    assert.match(send[1]!, /align-self:\s*flex-end/);
  });

  it("AppComposer binds the rung off the measured stage", () => {
    assert.match(
      composer,
      /'stage-icon': stage === 'icon'/,
      "the icon rung is bound from the ladder's stage",
    );
    assert.match(composer, /'stage-stacked': stage === 'stacked'/);
    assert.match(composer, /ref="root"/, "the ladder measures the composer root");
    assert.match(
      composer,
      /useComposerLadder\(root, ladderTouch\)/,
      "the composer root + the cost touch drive the ladder",
    );
    // the touch must bump on every face that changes the row's cost without resizing.
    const touch = composer.match(/const ladderTouch = computed\(([\s\S]*?)\);/);
    assert.ok(touch, "ladderTouch is missing");
    for (const p of ["props.model", "props.thinking", "props.busy", "expanded.value"]) {
      assert.ok(
        touch[1]!.includes(p),
        `ladderTouch must re-measure on ${p} (that face changes width)`,
      );
    }
  });

  it("the shed label keeps its accessible name", () => {
    assert.match(modelSelect, /class="chip-name"/);
    assert.match(
      modelSelect,
      /:aria-label="chipText"/,
      "with the label out of the flow, the chip's name is the model",
    );
  });
});
