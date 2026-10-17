// omega webui: unit tests for the inspector file-manager helpers in
// composables/omega.ts (rel-path join/clean/validate, breadcrumb
// derivation, entry sort). Run: node --test "test/*.test.ts" (node >= 23
// runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  cleanRelPath,
  fileCrumbs,
  joinRelPath,
  sortFileEntries,
} from "../app/composables/omega.ts";
import type { FileEntry } from "../app/composables/omega.ts";

function entry(name: string, isDir = false): FileEntry {
  return { name, size_bytes: isDir ? 0 : 12, is_dir: isDir, mod_time: "" };
}

describe("cleanRelPath", () => {
  it("passes valid paths through unchanged", () => {
    assert.equal(cleanRelPath(""), "");
    assert.equal(cleanRelPath("go.mod"), "go.mod");
    assert.equal(cleanRelPath("cmd/omega"), "cmd/omega");
    assert.equal(cleanRelPath("docs/a b.txt"), "docs/a b.txt");
  });

  it("rejects absolute paths, empty and dot-prefixed segments", () => {
    assert.equal(cleanRelPath("/etc/passwd"), null);
    assert.equal(cleanRelPath(".."), null);
    assert.equal(cleanRelPath("a/../b"), null);
    assert.equal(cleanRelPath("a//b"), null);
    assert.equal(cleanRelPath("a/"), null);
    assert.equal(cleanRelPath(".git"), null);
    assert.equal(cleanRelPath("docs/.hidden"), null);
  });
});

describe("joinRelPath", () => {
  it("descends from root and from a subdirectory", () => {
    assert.equal(joinRelPath("", "go.mod"), "go.mod");
    assert.equal(joinRelPath("cmd", "omega"), "cmd/omega");
  });
});

describe("fileCrumbs", () => {
  it("derives the root crumb plus one crumb per segment", () => {
    assert.deepEqual(fileCrumbs(""), [{ name: "/", path: "" }]);
    assert.deepEqual(fileCrumbs("docs"), [
      { name: "/", path: "" },
      { name: "docs", path: "docs" },
    ]);
    assert.deepEqual(fileCrumbs("docs/guides"), [
      { name: "/", path: "" },
      { name: "docs", path: "docs" },
      { name: "guides", path: "docs/guides" },
    ]);
  });

  it("falls back to the root trail for invalid paths", () => {
    assert.deepEqual(fileCrumbs("../evil"), [{ name: "/", path: "" }]);
  });
});

describe("sortFileEntries", () => {
  it("puts directories first, then names case-insensitively", () => {
    const got = sortFileEntries([
      entry("zeta.txt"),
      entry("beta", true),
      entry("Alpha.txt"),
      entry("alpha.txt"),
      entry("apple", true),
    ]);
    assert.deepEqual(
      got.map((e) => e.name),
      ["apple", "beta", "Alpha.txt", "alpha.txt", "zeta.txt"],
    );
  });

  it("does not mutate the caller's array", () => {
    const original = [entry("b"), entry("a")];
    sortFileEntries(original);
    assert.deepEqual(
      original.map((e) => e.name),
      ["b", "a"],
    );
  });
});