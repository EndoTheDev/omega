// omega webui: unit tests for the FileTree row-glyph table (card
// t_467a0386): extension -> glyph kind mapping, unknown -> generic,
// and the static inline-SVG builders (folder open/closed distinct
// without new colors). Run: node --test engine/webui/test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { folderSvg, glyphKindFor, glyphSvg } from "../app/components/files/rowicon.ts";

describe("glyphKindFor", () => {
  it("maps the common set omega's home shows", () => {
    assert.equal(glyphKindFor("main.go"), "code");
    assert.equal(glyphKindFor("omega.db"), "db");
    assert.equal(glyphKindFor("omega.log"), "log");
    assert.equal(glyphKindFor("config.yaml"), "config");
    assert.equal(glyphKindFor("package.json"), "config");
    assert.equal(glyphKindFor("AGENTS.md"), "md");
    assert.equal(glyphKindFor("notes.txt"), "text");
    assert.equal(glyphKindFor("run.sh"), "script");
    assert.equal(glyphKindFor("tool.py"), "code");
    assert.equal(glyphKindFor("App.vue"), "code");
  });

  it("unknown extensions fall to the generic glyph", () => {
    assert.equal(glyphKindFor("Makefile"), "generic");
    assert.equal(glyphKindFor("UNKNOWN.bin"), "generic");
    assert.equal(glyphKindFor("archive.tar.gz"), "generic");
  });

  it("dot only names an extension when it IS one (dotfiles, no-ext)", () => {
    assert.equal(glyphKindFor(".gitignore"), "generic");
    assert.equal(glyphKindFor("README"), "generic");
    assert.equal(glyphKindFor(""), "generic");
  });

  it("extension case never changes the mapping", () => {
    assert.equal(glyphKindFor("FILE.TXT"), "text");
    assert.equal(glyphKindFor("Data.YAML"), "config");
  });
});

describe("glyph builders", () => {
  it("every class builds an inline SVG using only currentColor", () => {
    for (const kind of [
      "generic", "text", "log", "md", "config", "code", "script", "db",
    ] as const) {
      const s = glyphSvg(kind);
      assert.ok(s.startsWith("<svg"), `${kind}: svg opens`);
      assert.ok(s.includes("currentColor"), `${kind}: currentColor`);
      assert.ok(!s.includes("http"), `${kind}: no external refs`);
    }
  });

  it("folder glyphs: open fills (filled vs outline), both currentColor", () => {
    const open = folderSvg(true);
    const closed = folderSvg(false);
    assert.ok(open.includes('fill="currentColor"'));
    assert.ok(closed.includes('fill="none"'));
    assert.ok(open.includes("currentColor") && closed.includes("currentColor"));
    assert.notEqual(open, closed);
  });

  it("glyph kinds all differ pairwise (each class renders its own paths)", () => {
    const svgs = [
      "generic", "text", "log", "md", "config", "code", "script", "db",
    ].map((k) => glyphSvg(k as never));
    assert.equal(new Set(svgs).size, svgs.length);
  });
});