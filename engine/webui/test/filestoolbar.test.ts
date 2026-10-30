// omega webui: the Files tab toolbar (card t_a4504a25). The node suite
// has no DOM, so the behavior half runs at the composable level - the
// POST wire (postFilesWrite / postFilesMkdir), the create-target picking
// (createTargetDir), the reveal chain (revealAncestors), the bus seam -
// and the toolbar/tree contract is structurally pinned on the source
// (the icon.test.ts recipe).
// Run: node --test test/filestoolbar.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  createTargetDir,
  fileTreeBus,
  postFilesMkdir,
  postFilesWrite,
  revealAncestors,
} from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

let savedFetch: typeof globalThis.fetch;
afterEach(() => {
  globalThis.fetch = savedFetch;
});

interface Call {
  url: string;
  method: string;
  body?: unknown;
  contentType?: string;
}

function recordFetch(
  respond: (call: Call) => Response | Promise<Response>,
): Call[] {
  const calls: Call[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (
    url: RequestInfo | URL,
    init?: RequestInit,
  ) => {
    const call: Call = {
      url: String(url),
      method: init?.method ?? "GET",
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
      contentType: (init?.headers as Record<string, string>)?.[
        "Content-Type"
      ],
    };
    calls.push(call);
    return respond(call);
  }) as typeof globalThis.fetch;
  return calls;
}

describe("postFilesWrite wire", () => {
  it("POSTs /files/write with the {path, content} envelope", async () => {
    const calls = recordFetch(
      () => new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );
    await postFilesWrite("docs/notes.md", "hello\n");
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/files/write");
    assert.equal(calls[0].method, "POST");
    assert.equal(calls[0].contentType, "application/json");
    assert.deepEqual(calls[0].body, { path: "docs/notes.md", content: "hello\n" });
  });

  it("throws the server's error message, not a bare status", async () => {
    recordFetch(
      () =>
        new Response(JSON.stringify({ error: "dot-prefixed path segment" }), {
          status: 400,
        }),
    );
    await assert.rejects(
      postFilesWrite(".x", ""),
      /dot-prefixed path segment/,
    );
  });

  it("non-JSON failure bodies fall back to the status code", async () => {
    recordFetch(() => new Response("boom", { status: 500 }));
    await assert.rejects(postFilesWrite("a.txt", ""), /request: 500/);
  });
});

describe("postFilesMkdir wire", () => {
  it("POSTs /files/mkdir with the {path} envelope", async () => {
    const calls = recordFetch(
      () => new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );
    await postFilesMkdir("a-dir");
    assert.deepEqual(calls[0].body, { path: "a-dir" });
    assert.equal(calls[0].url, "/files/mkdir");
    assert.equal(calls[0].method, "POST");
  });

  it("surfaces the loud refusal (already exists) as the thrown message", async () => {
    recordFetch(
      () => new Response(JSON.stringify({ error: "already exists" }), { status: 400 }),
    );
    await assert.rejects(postFilesMkdir("a-dir"), /already exists/);
  });
});

describe("createTargetDir picks the create location", () => {
  it("no selection = the root", () => {
    assert.equal(createTargetDir(null), "");
    assert.equal(createTargetDir(undefined), "");
  });

  it("a selected folder is the target itself", () => {
    assert.equal(createTargetDir({ path: "docs", isDir: true }), "docs");
    assert.equal(createTargetDir({ path: "a/b", isDir: true }), "a/b");
  });

  it("a selected file creates beside it", () => {
    assert.equal(createTargetDir({ path: "docs/notes.md", isDir: false }), "docs");
    assert.equal(createTargetDir({ path: "top.txt", isDir: false }), "");
  });
});

describe("revealAncestors", () => {
  it("a root-level name needs no expansion", () => {
    assert.deepEqual(revealAncestors("new.txt"), []);
  });

  it("nested paths expand every prefix above the node", () => {
    assert.deepEqual(revealAncestors("a/b/c.txt"), ["a", "a/b"]);
    // a created FOLDER's own row shows in its parent's listing - the
    // folder itself is never an ancestor of itself
    assert.deepEqual(revealAncestors("a/b"), ["a"]);
  });
});

describe("fileTreeBus seam", () => {
  it("is a reactive singleton with the three toolbar fields", () => {
    assert.deepEqual(Object.keys(fileTreeBus).sort(), ["collapse", "refresh", "reveal"]);
    const refresh = fileTreeBus.refresh;
    const collapse = fileTreeBus.collapse;
    fileTreeBus.refresh++;
    fileTreeBus.collapse++;
    assert.equal(fileTreeBus.refresh, refresh + 1);
    assert.equal(fileTreeBus.collapse, collapse + 1);
    assert.equal(typeof fileTreeBus.reveal, "string");
  });
});

describe("the band contract (structural pins, no DOM)", () => {
  // card t_a5f9d185: the toolbar strip became the VS Code-style header
  // row. Card t_308e6d9b: that row is the SHARED TabBand now (the
  // universal band) and it is STRUCTURALLY pinned - it renders BEFORE
  // the scroll body (.files-scroll), so a long tree never scrolls it.
  const tab = read("app/components/files/FilesTab.vue");
  const band = read("app/components/ui/TabBand.vue");
  const tree = read("app/components/files/FileTree.vue");
  const icons = read("app/composables/icons.ts");

  it("the Files tab renders the shared TabBand with the four actions", () => {
    assert.match(tab, /<TabBand/, "the band is the shared component");
    assert.doesNotMatch(tab, /class="files-tool"/, "the private band look is gone (band-tool is the contract)");
    for (const title of ["add file", "add folder", "refresh", "collapse folders"]) {
      assert.match(
        tab,
        new RegExp(`title="${title}"`),
        `band button "${title}" carries its title`,
      );
    }
    for (const label of ["add file", "add folder", "refresh files", "collapse folders"]) {
      assert.match(
        tab,
        new RegExp(`aria-label="${label}"`),
        `band button "${label}" is labeled`,
      );
    }
  });

  it("the band is ONE row: workspace name left, actions right, tree below", () => {
    const bandAt = tab.indexOf("<TabBand");
    const listAt = tab.indexOf('class="files-list"');
    assert.ok(bandAt >= 0 && listAt >= 0 && bandAt < listAt, "band above the tree");
    // no resolvable workspace -> no header at all (the Endo ruling)
    assert.match(tab, /v-if="headerName"/, "the band renders only when rooted");
    const scrollAt = tab.indexOf('class="files-scroll"');
    assert.ok(scrollAt >= 0 && bandAt < scrollAt, "the band is OUTSIDE the scroll body");
  });

  it("the band component encodes the non-scrolling structure + tool look", () => {
    assert.match(band, /class="tab-band"/);
    assert.match(band, /flex: none/, "the band never flexes into the scrollport");
    assert.match(band, /role="toolbar"/);
    assert.match(band, /\.band-tool/, "one action-button look for every tab");
  });

  it("the four glyphs ride the icons.ts vocabulary", () => {
    for (const name of ["file-plus", "folder-plus", "refresh", "unfoldless"]) {
      assert.match(
        icons,
        new RegExp(`${name}"?:`),
        `${name} is vocabulary`,
      );
    }
    assert.match(tab, /name="file-plus"/);
    assert.match(tab, /name="folder-plus"/);
    assert.match(tab, /name="refresh"/);
    assert.match(tab, /name="unfoldless"/);
  });

  it("create buttons disable when the root listing refused to load", () => {
    assert.match(tab, /toolsLocked/);
    assert.match(tab, /:disabled="toolsLocked"/);
    assert.match(tab, /rootError\.value \|\| loadingRoot\.value/);
    // refresh and collapse stay live while the tree is fine
    assert.match(tab, /:disabled="loadingRoot"[\s\S]*?@click="refreshFiles"/);
  });

  it("the create flow is an inline form, never window.prompt", () => {
    assert.match(tab, /class="files-add"/);
    assert.match(tab, /aria-label="new node name"/);
    assert.ok(!tab.includes("window.prompt"), "no window.prompt");
    assert.match(
      tab,
      /postFilesWrite\(path, "", sessionId\.value\)/,
      "the toolbar creates EMPTY files under the resolved root",
    );
    assert.match(tab, /postFilesMkdir\(path, sessionId\.value\)/);
  });

  it("a refusal lands in the inline error, a success refreshes + reveals", () => {
    assert.match(tab, /toolError\.value = e instanceof Error \? e\.message/, "inline error");
    assert.match(tab, /fileTreeBus\.refresh\+\+/);
    assert.match(tab, /fileTreeBus\.reveal = path/);
    assert.match(tab, /openFile\(path\)/, "the new file is selected via its preview");
  });

  it("collapse goes through the ONE seam - FileTree owns expansion state", () => {
    assert.match(tab, /fileTreeBus\.collapse\+\+/);
    assert.match(tree, /watch\(\s*\(\) => fileTreeBus\.collapse/, "collapse watch");
    assert.match(tree, /watch\(\s*\(\) => fileTreeBus\.refresh/, "refresh watch");
    assert.match(tree, /watch\(\s*\(\) => fileTreeBus\.reveal/, "reveal watch");
    assert.match(tree, /expanded\.clear\(\)/, "collapse folds every branch");
    // the refresh branch drops the loaded caches so creates turn up
    assert.match(tree, /delete kids\[p\]/);
    assert.match(tree, /delete errs\[p\]/);
  });

  it("FileTree reports row selection up (folders included)", () => {
    assert.match(tree, /select: \[sel: \{ path: string; isDir: boolean \}\]/);
    assert.match(tree, /emit\("select", \{ path, isDir: entry\.is_dir \}\)/);
    assert.match(tab, /@select="onSelect"/);
    assert.match(tab, /onSelect\(sel: SelectedNode\)/);
  });

  it("the header renders in the empty-root state (before the list)", () => {
    const headerAt = tab.indexOf("<TabBand");
    const listAt = tab.indexOf('class="files-list"');
    assert.ok(headerAt >= 0 && listAt >= 0 && headerAt < listAt);
    // the FileTree stays mounted only with entries; the form does not
    assert.match(tab, /<form\s+v-if="creating"/);
  });
});