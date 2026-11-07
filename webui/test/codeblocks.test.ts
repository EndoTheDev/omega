// omega webui: unit tests for the codeblock post-render enhancement
// (composables/codeblocks.ts). Run: node --test webui/test/
// (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { codeLang, codeLines } from "../app/composables/codeblocks.ts";
import { renderMarkdown } from "../app/composables/omega.ts";
import { readFileSync } from "node:fs";

describe("codeLang", () => {
  it("extracts the language from markdown-it / hljs classes", () => {
    assert.equal(codeLang("language-go"), "go");
    assert.equal(codeLang("hljs language-typescript"), "typescript");
    assert.equal(codeLang("language-python extra"), "python");
  });

  it("falls back to 'code' without a language class", () => {
    assert.equal(codeLang("hljs"), "code");
    assert.equal(codeLang(""), "code");
  });
});

describe("codeLines", () => {
  it("counts lines, ignoring the trailing newline", () => {
    assert.equal(codeLines("a\nb\n"), 2);
    assert.equal(codeLines("a\nb\n\n\n"), 2);
  });

  it("empty code is zero lines", () => {
    assert.equal(codeLines(""), 0);
    assert.equal(codeLines("\n"), 0);
  });
});

describe("the codeblock copy button (card t_a92450ea)", () => {
  const src = readFileSync(new URL("../app/composables/codeblocks.ts", import.meta.url), "utf8");

  it("rides the ONE copy path and pins the copied affordance to its verdict", () => {
    // the call and the import must be the same name - the pin came from a
    // live catch on t_a92450ea: a renamed call + a fixed import builds
    // clean (typeCheck:false) then reds in every browser with a bare
    // ReferenceError inside the click handler
    assert.match(src, /import \{ copyText \} from "\.\/clipboard\.ts"/);
    assert.match(src, /copyText\(code\?\.textContent \?\? ""\)/);
    assert.doesNotMatch(src, /copyTextToClipboard/);
    assert.doesNotMatch(src, /navigator\.clipboard/);
    // the affordance reflects the verdict, never a voided rejection
    assert.match(src, /ok \? "copied" : "copy failed"/);
  });
});

describe("rendered fence feeding decorateCodeblocks", () => {
  it("highlighted fence carries the language class on the code element", () => {
    const html = renderMarkdown("```go\nfmt.Println(1)\n```");
    assert.match(html, /<code class="hljs language-go">/);
  });

  it("unstyled fence (unknown lang) still carries language-<lang>", () => {
    const html = renderMarkdown("```brainfuck\nx\n```");
    assert.match(html, /<code class="language-brainfuck">/);
  });

  it("open streaming fence keeps its language class too", () => {
    const html = renderMarkdown("```go\nfunc main() {\n");
    assert.match(html, /<code class="language-go">/);
  });
});