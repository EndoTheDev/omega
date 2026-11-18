// omega webui: the composer's model + effort chips wrap as ONE unit. The fix pairs the
// two chips inside one.model-group flex item (nowrap, min-width: auto - the group holds
// at its own min-content) - a wrap moves the pair together or leaves both in place, it
// cannot split it. These are structural source-contract tests (house pattern: read
// the.vue/.css, assert the wiring).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

const composer = () =>
  readFileSync(
    join(root, "app", "components", "chat", "AppComposer.vue"),
    "utf8",
  );
const css = () =>
  readFileSync(join(root, "app", "assets", "css", "main.css"), "utf8");

describe("composer model+effort wrap group (t_641c2870)", () => {
  it("ModelSelect and the effort BandSelect render inside one .model-group", () => {
    const src = composer();
    assert.match(
      src,
      /<div class="model-group">/,
      "AppComposer must wrap the model + effort chips in one .model-group",
    );
    // ..
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
      /<BandSelect[\s\S]*?\/>/,
      "the effort chip must live inside .model-group (the shared BandSelect since the v0.14.1 review-lens migration)",
    );
  });

  it("workspace and send stay OUTSIDE the group (they wrap independently)", () => {
    const src = composer();
    const group = src.match(/<div class="model-group">[\s\S]*?<\/div>/);
    assert.ok(group, ".model-group block not found");
    assert.doesNotMatch(
      group[0],
      /WorkspaceSelect/,
      ".model-group must not swallow the workspace picker - it is not a composer chip at all any more (card t_8fd38f88 moved it to the status bar)",
    );
    assert.doesNotMatch(
      group[0],
      /class="send"/,
      ".model-group must not swallow the send button - it keeps its own row anchor",
    );
    // the workdir selector LEFT the composer entirely.
    assert.doesNotMatch(
      src,
      /<WorkspaceSelect/,
      "the composer must not render the workdir picker (the status bar owns it)",
    );
    const groupAt = src.indexOf('<div class="model-group">');
    const sendAt = src.indexOf('class="send"');
    assert.ok(groupAt !== -1 && groupAt < sendAt,
      "toolbar order stays .model-group -> send");
  });

  it("main.css makes the group a nowrap flex unit the toolbar cannot split", () => {
    const src = css();
    const groupRule = src.match(/\.model-group\s*\{[^}]*\}/);
    assert.ok(groupRule, ".model-group rule missing from main.css");
    assert.match(groupRule[0], /display:\s*flex/, "the group is a flex row");
    assert.match(groupRule[0], /flex-wrap:\s*nowrap/,
      "the group must not wrap internally - chips stay adjacent");
    assert.doesNotMatch(groupRule[0], /min-width:\s*max-content/,
      "the group must not hard-pin to max-content - chips keep their shrink room");
    // the toolbar keeps its wrap: the GROUP crosses the boundary,
    // the row still wraps. (Line-anchored: the base `.toolbar` rule -
    // the composer's override below it must not shadow the match.)
    const toolbarRule = src.match(/^\.toolbar\s*\{[^}]*\}/m);
    assert.ok(toolbarRule, ".toolbar rule missing from main.css");
    assert.match(toolbarRule[0], /flex-wrap: wrap/,
      ".toolbar must keep flex-wrap: wrap - the group wraps as a unit, the row still wraps");
    // the composer's override: the BOTTOM row's chips never wrap while the composer.
    const composerRule = src.match(/\.composer:not\(\.expanded\) \.toolbar\s*\{[^}]*\}/);
    assert.ok(composerRule, "the composer toolbar rule missing from main.css");
    assert.match(composerRule[0], /flex-wrap: nowrap/,
      "the composer pins its bottom row to one line");
  });
});