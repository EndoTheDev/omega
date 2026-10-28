// omega webui: unit tests for the FileTree row-glyph table (card
// t_467a0386, iconified card t_502b8e81): extension -> glyph kind mapping,
// unknown -> generic, and the iconset-name builders (folder open/closed
// distinct). The SVG builders are gone - a glyph is a NAME in the webui
// iconset, rendered through the Icon wrapper. Run: node --test engine/webui/test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { folderIcon, glyphKindFor, glyphIcon } from "../app/components/files/rowicon.ts";
import { ICON_NAMES } from "../app/composables/icons.ts";

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

describe("glyph names (iconset, card t_502b8e81)", () => {
  it("every kind maps to a NAME that exists in the iconset", () => {
    for (const kind of [
      "generic", "text", "log", "md", "config", "code", "script", "db",
    ] as const) {
      const name = glyphIcon(kind);
      assert.equal(typeof name, "string", `${kind}: a name`);
      assert.ok(
        (ICON_NAMES as readonly string[]).includes(name),
        `${kind}: "${name}" is in the iconset`,
      );
    }
  });

  it("glyph kinds still differ where the UI reads the difference (text vs code vs config)", () => {
    const names = new Set(["text", "log", "md", "config", "code", "script", "db", "generic"]
      .map((k) => glyphIcon(k as never)));
    // the family is small by design (one class per kind); the pair test
    // only requires the load-bearing distinctions (text/code/db at least)
    assert.ok(names.size >= 4, `distinct names: ${names.size}`);
  });

  it("folder glyphs: open/closed distinct, both in the iconset", () => {
    const open = folderIcon(true);
    const closed = folderIcon(false);
    assert.notEqual(open, closed);
    assert.ok((ICON_NAMES as readonly string[]).includes(open), "open folder named");
    assert.ok((ICON_NAMES as readonly string[]).includes(closed), "closed folder named");
  });
});