// omega webui: the document root never scrolls; inner surfaces own their scroll.
//
// The SPA is window chrome, not a document. With the root unpinned, anything a
// frame exceeds becomes a document scroll - and in a host that draws CLASSIC
// scrollbars the horizontal one eats width, the vertical one eats height, while
// .shell's 100dvh still resolves to the full layout viewport: one overflow
// manufactures the second axis and the whole SPA slides inside its own window.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, "..", "app/assets/css/main.css"), "utf8");

/** the body of the first BASE rule whose selector is exactly `selector` */
const rule = (selector: string): string => {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = css.match(new RegExp(`^${escaped}\\s*\\{([^}]*)\\}`, "m"));
  assert.ok(m, `${selector} rule is missing from main.css`);
  return m![1];
};

describe("the document root is pinned", () => {
  it("html, body, #__nuxt carry overflow: hidden", () => {
    const body = rule("html, body, #__nuxt");
    assert.match(body, /overflow:\s*hidden/, "the root must never scroll");
  });

  it(".shell neither overflows its own box nor lets a child widen the column", () => {
    const body = rule(".shell");
    assert.match(body, /overflow:\s*hidden/, ".shell is the frame's clip boundary");
    // a plain 1fr column takes the widest item's min-content as its floor, which
    // is exactly how the menubar band pushed the frame past the viewport.
    assert.match(body, /grid-template-columns:\s*minmax\(0,\s*1fr\)/,
      "the shell column must not be sized by a child's min-content");
  });

  it("the caption cells compress instead of overflowing the band", () => {
    assert.match(rule(".window-controls"), /min-width:\s*0/);
    assert.match(rule(".window-controls"), /width:\s*138px/);
    const cell = rule(".win-cell");
    assert.match(cell, /width:\s*46px/);
    assert.match(cell, /min-width:\s*0/);
  });
});

describe("inner surfaces still scroll", () => {
  const scrollers: [string, string][] = [
    [".messages", "the transcript"],
    [".settings-body", "the settings pane"],
    [".session-list", "the session list"],
    [".files-scroll", "the file tree"],
  ];

  for (const [selector, what] of scrollers) {
    it(`${selector} keeps its own overflow (${what})`, () => {
      assert.match(rule(selector), /overflow(-y)?:\s*(auto|scroll)/,
        `${what} must scroll inside the pinned root`);
    });
  }

  it("a fenced code block scrolls its own overflow", () => {
    assert.match(css, /\.codeblock pre\s*\{[^}]*overflow:\s*auto/,
      "the transcript's code blocks must keep their own scroll box");
  });
});
