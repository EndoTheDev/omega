// omega webui: the file tree store (card t_ccd4a000) - expansion,
// failures, per-folder error keying, refresh/clear, persistence and the
// fixed-phrase failure hint, driven WITHOUT a DOM through the
// composable extracted from FileTree.vue.
//
// The mandated scenarios:
//   - a failed children request paints NO child row (store level: the
//     branch collapses and only the store's internal mark exists) +
//     structural pins in filestoolbar.test.ts
//   - the error keys by the folder path that ISSUED the request
//   - a later successful children response clears the mark
//   - two folders expand in one restore, one fails, one succeeds ->
//     only the failed one is marked
//
// Run: node --test test/filetree.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import { createFileTreeStore } from "../app/composables/filetree.ts";
import { emitLocalToast, onToastPanel } from "../app/composables/canvas.ts";
import { fileTreeBus } from "../app/composables/omega.ts";
import type { FileEntry, FilesBrowsePayload } from "../app/composables/omega.ts";

const tick = () => new Promise((r) => setTimeout(r, 0));

function listing(names: [string, boolean][]): FilesBrowsePayload {
  const entries: FileEntry[] = names.map(([name, isDir]) => ({
    name,
    size_bytes: isDir ? 0 : 12,
    is_dir: isDir,
    mod_time: "",
  }));
  return { entries, root: "/work", source: "session" };
}

interface FakeBrowse {
  calls: string[];
  browse: typeof import("../app/composables/omega.ts").fetchFilesBrowse;
}

// fakeBrowse: every path resolves, rejects, or returns entries on demand
function fakeBrowse(
  rule: (path: string) => FilesBrowsePayload | string,
): FakeBrowse {
  const calls: string[] = [];
  const browse = (async (path: string) => {
    calls.push(path);
    const out = rule(path);
    if (typeof out === "string") {
      throw new Error(out);
    }
    return out;
  }) as typeof import("../app/composables/omega.ts").fetchFilesBrowse;
  return { calls, browse };
}

const errors: string[] = [];

afterEach(() => {
  errors.length = 0;
  // reset the shared bus counters this file pokes
  fileTreeBus.refresh++;
});

describe("filetree store - failure keying (t_ccd4a000)", () => {
  it("two folders expand in one restore, one fails, one succeeds -> only the failed one is marked", async () => {
    const { browse } = fakeBrowse((p) => {
      if (p === ".git") return "traversal path segment";
      if (p === ".github") return "resolve path: boom";
      if (p === "agents") return listing([["lead.toml", false]]);
      return listing([]);
    });
    const store = createFileTreeStore({
      browse,
      seed: [".git", ".github", "agents"],
    });
    await tick();
    assert.ok(store.isFailed(".git"), "the failed folder is marked");
    assert.ok(store.isFailed(".github"), "its own failed sibling is marked too");
    assert.ok(!store.isFailed("agents"), "the successful folder is NOT marked");
    assert.ok(store.isOpen(".git") === false, "a failed expansion leaves the folder CLOSED");
    assert.ok(store.isOpen("agents") === true, "a successful restore stays open");
    assert.deepEqual(
      store.childrenOf(".git"),
      [],
      "a failed folder paints no child rows",
    );
    assert.deepEqual(store.childrenOf("agents").map((e) => e.name), ["lead.toml"]);
  });

  it("the mark keys by the folder that ISSUED the request, never a sibling", async () => {
    // two folders fail with DIFFERENT server messages: each key holds
    // its own message and nothing else is marked after restore
    const { browse } = fakeBrowse((p) => {
      if (p === ".git") return "resolve path: boom";
      if (p === ".worktrees") return "read directory: boom";
      return listing([]);
    });
    const seen: Record<string, string> = {};
    const store = createFileTreeStore({
      browse,
      seed: [".git", ".worktrees", "docs"],
      onError: (path, message) => {
        seen[path] = message;
      },
    });
    await tick();
    assert.ok(store.isFailed(".git"));
    assert.ok(store.isFailed(".worktrees"));
    assert.ok(!store.isFailed("docs"));
    // and each callback fired once, for its own path
    assert.deepEqual(Object.keys(seen).sort(), [".git", ".worktrees"]);
    assert.match(seen[".git"], /resolve path/);
    assert.match(seen[".worktrees"], /read directory/);
  });

  it("a later successful children response clears the failed mark", async () => {
    let refuse = true;
    const { browse, calls } = fakeBrowse((p) => {
      if (p === ".git" && refuse) return "traversal path segment";
      return listing([["HEAD", false]]);
    });
    const store = createFileTreeStore({ browse, seed: [".git"] });
    await tick();
    assert.ok(store.isFailed(".git"));
    assert.ok(!store.isOpen(".git"));

    // the user retries by clicking the chevron: the failed cache was
    // dropped, so a real refetch fires; this time it succeeds
    refuse = false;
    const callsBefore = calls.length;
    store.toggle(".git");
    await tick();
    assert.equal(calls.length, callsBefore + 1, "the retry refetches");
    assert.ok(!store.isFailed(".git"), "success clears the error state");
    assert.ok(store.isOpen(".git"));
    assert.deepEqual(store.childrenOf(".git").map((e) => e.name), ["HEAD"]);
  });

  it("a refresh clears every failed mark; a retry re-arms the branch", async () => {
    let failGit = true;
    const { browse, calls } = fakeBrowse((p) => {
      if (p === ".git" && failGit) return "traversal path segment";
      return listing([]);
    });
    const store = createFileTreeStore({ browse, seed: [".git", "agents"] });
    await tick();
    assert.ok(store.isFailed(".git"));
    // the failure collapsed the branch, so the refresh's reload covers
    // the OPEN set only - the mark clears either way
    failGit = false;
    fileTreeBus.refresh++;
    await tick();
    assert.ok(!store.isFailed(".git"), "the refresh gives failures a fresh chance");
    assert.ok(store.isOpen(".git") === false, "the collapsed branch stays closed");
    // the user's retry (chevron click) refetches fresh and opens
    const before = calls.length;
    store.toggle(".git");
    await tick();
    assert.equal(calls.length, before + 1, "the retry refetches");
    assert.ok(store.isOpen(".git"), "the retry re-arms the branch");
  });

  it("the default failure hint is ONE fixed-phrase toast - no backend vocabulary", async () => {
    // shift the funnel's module-level dedupe fingerprint first - the
    // other stores in this file already emitted the identical payload
    emitLocalToast({ sentinel: `filetree-test-${Date.now()}` });
    const panels: unknown[] = [];
    const release = onToastPanel((p) => panels.push(p));
    try {
      const { browse } = fakeBrowse(() => "dot-prefixed path segment");
      const store = createFileTreeStore({ browse, seed: [".git", ".github", ".worktrees"] });
      await tick();
      assert.equal(
        panels.length,
        1,
        "a batched restore's identical failures collapse into ONE toast",
      );
      const payload = panels[0] as { title?: string; source_id?: string };
      assert.equal(payload.title, "folder failed to load");
      assert.equal(payload.source_id, "files-tree");
      assert.ok(
        !JSON.stringify(payload).includes("dot-prefixed"),
        "the backend error string never rides the hint",
      );
      // and the tree keeps none of it: the store's mark is internal
      assert.ok(store.isFailed(".github"));
    } finally {
      release();
    }
  });

  it("a successful load replaces the children and re-sorts them", async () => {
    const { browse } = fakeBrowse(() => listing([["zeta.txt", false], ["beta", true]]));
    const store = createFileTreeStore({ browse });
    store.toggle("docs");
    await tick();
    assert.ok(store.isOpen("docs"));
    assert.deepEqual(
      store.childrenOf("docs").map((e) => e.name),
      ["beta", "zeta.txt"],
      "directories first, then names",
    );
  });
});

describe("filetree store - persistence", () => {
  it("keeps the expansion in sync and a NEW store restores from it", async () => {
    let saved = "";
    (globalThis as { localStorage?: unknown }).localStorage = {
      getItem: (_k: string) => saved,
      setItem: (_k: string, v: string) => {
        saved = v;
      },
    };
    try {
      // the restore-time seeds predate the persistence watch (the
      // component's order: restore, then watch) - persistence records
      // MUTATIONS only, exactly as it did inside FileTree.vue
      const store = createFileTreeStore({
        browse: fakeBrowse(() => listing([])).browse,
        seed: ["agents"],
      });
      await tick();
      store.toggle("docs");
      await tick();
      assert.ok(saved.includes('"docs"'), "the toggled expansion persists");
      assert.ok(store.isOpen("agents"), "the seed stays open");

      // a NEW store (a reloaded tab) restores from the same key
      const second = createFileTreeStore({
        browse: fakeBrowse(() => listing([])).browse,
      });
      await tick();
      assert.ok(second.isOpen("docs"), "the persisted branch restores");
    } finally {
      (globalThis as { localStorage?: unknown }).localStorage = undefined;
    }
  });

  it("hidden branches persist and restore - ordinary paths since the guard narrowed", async () => {
    let saved = JSON.stringify([".git", ".github"]);
    (globalThis as { localStorage?: unknown }).localStorage = {
      getItem: (_k: string) => saved,
      setItem: (_k: string, v: string) => {
        saved = v;
      },
    };
    try {
      const store = createFileTreeStore({
        browse: fakeBrowse(() => listing([])).browse,
      });
      await tick();
      assert.ok(store.isOpen(".git"), "the hidden branch restores");
      assert.ok(store.isOpen(".github"));
    } finally {
      (globalThis as { localStorage?: unknown }).localStorage = undefined;
    }
  });
});