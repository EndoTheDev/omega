// omega webui: unit tests for the inspector file-tree helpers in
// composables/omega.ts (rel-path join/clean/validate, expanded-set
// persistence, entry sort). Run: node --test "test/*.test.ts" (node >= 23
// runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  cleanRelPath,
  joinRelPath,
  parseExpandedPaths,
  serializeExpandedPaths,
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

describe("parseExpandedPaths", () => {
  it("restores valid paths in order", () => {
    assert.deepEqual(parseExpandedPaths(JSON.stringify(["cmd", "cmd/agent"])), [
      "cmd",
      "cmd/agent",
    ]);
  });

  it("parses empty for malformed JSON and non-array payloads", () => {
    assert.deepEqual(parseExpandedPaths("not json"), []);
    assert.deepEqual(parseExpandedPaths("42"), []);
    assert.deepEqual(parseExpandedPaths('{"cmd":1}'), []);
    assert.deepEqual(parseExpandedPaths(""), []);
  });

  it("drops entries the backend would reject", () => {
    assert.deepEqual(
      parseExpandedPaths(JSON.stringify(["/abs", "..", "a", "", ".x", 7, null])),
      ["a"],
    );
  });

  it("collapses duplicates, preserving first occurrence order", () => {
    assert.deepEqual(
      parseExpandedPaths(JSON.stringify(["docs", "a/b", "docs", "a"])),
      ["docs", "a/b", "a"],
    );
  });
});

describe("serializeExpandedPaths", () => {
  it("writes a sorted JSON array regardless of set order", () => {
    assert.equal(
      serializeExpandedPaths(new Set(["z", "a/b", "a"])),
      '["a","a/b","z"]',
    );
  });

  it("drops invalid paths so a poisoned set never persists", () => {
    assert.equal(
      serializeExpandedPaths(["ok/sub", "", ".hid", "/abs"]),
      '["ok/sub"]',
    );
  });

  it("round-trips through parseExpandedPaths", () => {
    const set = new Set(["docs", "docs/guides", "cmd"]);
    assert.deepEqual(
      parseExpandedPaths(serializeExpandedPaths(set)),
      ["cmd", "docs", "docs/guides"],
    );
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