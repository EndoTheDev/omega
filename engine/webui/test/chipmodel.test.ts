// omega webui: unit tests for the composer chip's model seed (t_8eb149fa).
// The regression: b8a9251 (composer pins model + workspace on the session)
// removed `:model="selectedModel"` from the app.vue ChatTab tag, but the
// chip's seed + follow code (ChatTab restorePins / the props.model watch)
// still reads props.model - so an unpinned session rendered an EMPTY chip
// and the app default never reached the composer. Journey 83's first
// assert then compared a quoted ab-eval string against the row and never
// clicked - the red hid this gap. These are structural source-contract
// tests (house pattern: read the .vue, assert the wiring).
// Run: node --test test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = new URL("..", import.meta.url).pathname;

const app = () => readFileSync(join(root, "app", "app.vue"), "utf8");
const chatTab = () =>
  readFileSync(join(root, "app", "components", "ChatTab.vue"), "utf8");

describe("composer chip model seed (t_8eb149fa)", () => {
  it("app.vue passes the app default model down to every ChatTab", () => {
    // the ChatTab tag in the pane-chat template must carry :model -
    // the ONLY channel the app default reaches the chip through
    assert.match(
      app(),
      /<ChatTab\s[\s\S]*?:model="selectedModel"/,
      "app.vue ChatTab tag lost :model=\"selectedModel\" - the unpinned " +
        "chip renders empty again",
    );
  });

  it("ChatTab declares the model prop its seed and watch read", () => {
    const src = chatTab();
    // the props interface declares it (types the reads below)
    assert.match(src, /model: string;/, "ChatTab props must declare model");
    // the unpinned seed rides the app default ...
    assert.match(
      src,
      /props\.session\?\.model \|\| props\.model/,
      "the chip seed must fall through to the app default",
    );
    // ... and the watch follows the default while unpinned
    assert.match(src, /\(\) => props\.model/, "the follow-watch");
  });

  it("the app default is seeded from the models fetch, not only from clicks", () => {
    // fetchModels().then seeds selectedModel from m.current - a fresh
    // boot renders a REAL model name, not an empty chip
    assert.match(
      app(),
      /selectedModel\.value = m\.current/,
      "app.vue must seed selectedModel from the /models fetch",
    );
  });
});