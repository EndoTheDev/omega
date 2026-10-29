// omega webui: the composer's model + effort chips wrap as ONE unit
// (t_641c2870). The regression: the toolbar is a wrapping flex row and
// used to render WorkspaceSelect, ModelSelect, ThinkingSelect, send as
// flat siblings - on a narrow composer the wrap boundary landed between
// the model chip and the effort chip, stranding `default ▾` alone on
// row two while `glm-5.3 ▾` stayed on row one with empty space beside
// it (Endo, 2026-10-29: "make sure modelname and reasoning effort
// stays grouped together"). The fix pairs the two chips inside one
// .model-group flex item (nowrap, min-width: auto - the group holds at
// its own min-content) - a wrap
// moves the pair together or leaves both in place, it cannot split it.
// These are structural source-contract tests (house pattern: read the
// .vue/.css, assert the wiring).
// Run: node --test test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = new URL("..", import.meta.url).pathname;

const composer = () =>
  readFileSync(
    join(root, "app", "components", "chat", "AppComposer.vue"),
    "utf8",
  );
const css = () =>
  readFileSync(join(root, "app", "assets", "css", "main.css"), "utf8");

describe("composer model+effort wrap group (t_641c2870)", () => {
  it("ModelSelect and ThinkingSelect render inside one .model-group", () => {
    const src = composer();
    // the wrapper exists ...
    assert.match(
      src,
      /<div class="model-group">/,
      "AppComposer must wrap the model + effort chips in one .model-group",
    );
    // ... and carries BOTH chips inside it, in the v0.14.3 row order
    const group = src.match(
      /<div class="model-group">[\s\S]*?<\/div>/,
    );
    assert.ok(group, ".model-group block not found");
    assert.match(
      group[0],
      /<ModelSelect[\s\S]*?\/>/,
      "the model chip must live inside .model-group",
    );
    assert.match(
      group[0],
      /<ThinkingSelect[\s\S]*?\/>/,
      "the effort chip must live inside .model-group",
    );
  });

  it("workspace and send stay OUTSIDE the group (they wrap independently)", () => {
    const src = composer();
    const group = src.match(/<div class="model-group">[\s\S]*?<\/div>/);
    assert.ok(group, ".model-group block not found");
    assert.doesNotMatch(
      group[0],
      /WorkspaceSelect/,
      ".model-group must not swallow the workspace chip - it may sit alone on a row",
    );
    assert.doesNotMatch(
      group[0],
      /class="send"/,
      ".model-group must not swallow the send button - it keeps its own row anchor",
    );
    // Endo's ask is specifically the model+effort pair: workspace FIRST
    // (v0.14.3 ruling), the group after it, the button after that.
    const wsAt = src.indexOf("<WorkspaceSelect");
    const groupAt = src.indexOf('<div class="model-group">');
    const sendAt = src.indexOf('class="send"');
    assert.ok(wsAt !== -1 && wsAt < groupAt && groupAt < sendAt,
      "toolbar order must stay workspace -> .model-group -> send");
  });

  it("main.css makes the group a nowrap flex unit the toolbar cannot split", () => {
    const src = css();
    // the group: one unbreakable flex item
    const groupRule = src.match(/\.model-group\s*\{[^}]*\}/);
    assert.ok(groupRule, ".model-group rule missing from main.css");
    assert.match(groupRule[0], /display:\s*flex/, "the group is a flex row");
    assert.match(groupRule[0], /flex-wrap:\s*nowrap/,
      "the group must not wrap internally - chips stay adjacent");
    assert.doesNotMatch(groupRule[0], /min-width:\s*max-content/,
      "the group must not hard-pin to max-content - chips keep their shrink room");
    // the toolbar keeps its wrap: the GROUP crosses the boundary,
    // the row still wraps
    const toolbarRule = src.match(/\.toolbar\s*\{[^}]*\}/);
    assert.ok(toolbarRule, ".toolbar rule missing from main.css");
    assert.match(toolbarRule[0], /flex-wrap:\s*wrap/,
      ".toolbar must keep flex-wrap: wrap - the group wraps as a unit, the row still wraps");
  });
});