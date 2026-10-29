// omega webui: the Files tab root resolution law (card t_a5f9d185). The
// client half: every /files request carries the focused session's ID
// (never a root - the trust boundary resolves server-side), the tab
// re-roots on session focus change, and the header shows the basename
// of what the server resolved, hidden entirely on the cwd fallback.
// Run: node --test test/filesroot.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  fetchFilesBrowse,
  fetchFilesRead,
  filesHeaderVisible,
  filesRootName,
  postFilesMkdir,
  postFilesWrite,
} from "../app/composables/omega.ts";
import { createChatHost, type ChatHostDeps } from "../app/composables/chat.ts";

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
    };
    calls.push(call);
    return respond(call);
  }) as typeof globalThis.fetch;
  return calls;
}

describe("files wire carries the focused session (never a root)", () => {
  it("fetchFilesBrowse appends ?session=<id>", async () => {
    const calls = recordFetch(
      () =>
        new Response(
          JSON.stringify({ entries: [], root: "/home/proj", source: "session" }),
          { status: 200 },
        ),
    );
    const data = await fetchFilesBrowse("", "sess-1");
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.url, "/files/browse?session=sess-1");
    assert.equal(data.root, "/home/proj");
    assert.equal(data.source, "session");
  });

  it("fetchFilesBrowse without a session keeps the bare URL (cwd law)", async () => {
    const calls = recordFetch(
      () => new Response(JSON.stringify({ entries: [] }), { status: 200 }),
    );
    await fetchFilesBrowse("");
    assert.equal(calls[0]!.url, "/files/browse");
  });

  it("path and session ride one query string", async () => {
    const calls = recordFetch(
      () => new Response(JSON.stringify({ entries: [] }), { status: 200 }),
    );
    await fetchFilesBrowse("docs", "sess-2");
    assert.equal(calls[0]!.url, "/files/browse?path=docs&session=sess-2");
  });

  it("fetchFilesRead roots the preview on the focused session", async () => {
    const calls = recordFetch(
      () =>
        new Response(
          JSON.stringify({ content: "x", truncated: false, binary: false }),
          { status: 200 },
        ),
    );
    await fetchFilesRead("notes.md", "sess-3");
    assert.equal(calls[0]!.url, "/files/read?path=notes.md&session=sess-3");
  });

  it("postFilesWrite / postFilesMkdir include the session in the envelope", async () => {
    const calls = recordFetch(
      () => new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );
    await postFilesWrite("a.md", "body", "sess-4");
    await postFilesMkdir("dir", "sess-4");
    assert.equal(calls[0]!.url, "/files/write");
    assert.deepEqual(calls[0]!.body, { path: "a.md", content: "body", session: "sess-4" });
    assert.equal(calls[1]!.url, "/files/mkdir");
    assert.deepEqual(calls[1]!.body, { path: "dir", session: "sess-4" });
  });

  it("a sessionless create keeps the pre-law envelope (additive, not breaking)", async () => {
    const calls = recordFetch(
      () => new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );
    await postFilesWrite("a.md", "", undefined);
    assert.deepEqual(calls[0]!.body, { path: "a.md", content: "" });
  });
});

describe("header name + visibility (pure predicates)", () => {
  it("filesRootName picks the basename of the resolved root", () => {
    assert.equal(filesRootName("/home/endo/projects"), "projects");
    assert.equal(filesRootName("/"), "/");
    assert.equal(filesRootName(""), "");
    assert.equal(filesRootName("C:\\Users\\endo\\proj"), "proj");
  });

  it("filesHeaderVisible: session/default show, cwd hides (no dead header)", () => {
    assert.equal(filesHeaderVisible("session"), true);
    assert.equal(filesHeaderVisible("default"), true);
    assert.equal(filesHeaderVisible("cwd"), false);
    // a pre-law server sends no source at all - the row stays hidden
    assert.equal(filesHeaderVisible(undefined), false);
  });
});

describe("the re-root contract (structural pins)", () => {
  const tab = read("app/components/files/FilesTab.vue");
  const viewer = read("app/components/ViewerTab.vue");
  const app = read("app/app.vue");
  const viewerSeam = read("app/composables/viewer.ts");

  it("the tab reads the focused-session seam and re-roots on change", () => {
    assert.match(tab, /inject\(FOCUSED_SESSION_KEY/);
    assert.match(tab, /watch\(\[sessionId, rowWorkspace\]/);
    assert.match(tab, /fetchFilesBrowse\(\s*"",\s*sessionId\.value/, "every browse carries the id");
    // the row workspace is a second trigger: a re-pin re-roots live
    assert.match(tab, /sessionRow\(id\)\?\.workspace/);
  });

  it("the viewer reads under the SAME resolution the tab browsed under", () => {
    assert.match(viewer, /FOCUSED_SESSION_KEY/);
    assert.match(viewer, /fetchFilesRead\(props\.viewer\.name, focusedSession\?\.value/);
  });

  it("app.vue provides the focused session (the seam has one source)", () => {
    assert.match(app, /provide\(FOCUSED_SESSION_KEY, focusedSessionId\)/);
    assert.match(viewerSeam, /FOCUSED_SESSION_KEY/);
  });

  it("the header trusts the server's root/source verdict only", () => {
    assert.match(tab, /headerSource\.value = data\.source/);
    assert.match(tab, /filesHeaderVisible\(headerSource\.value\)/);
    assert.match(tab, /filesRootName\(headerRoot\.value\)/);
  });
});

describe("the ChatHost carries sessionRow (behavior pin, card t_cd2f25b0)", () => {
  // The defect class the source-grep pins cannot catch: FilesTab.vue's
  // row-workspace computed calls chatHost?.sessionRow(id) on the HOST
  // the app injects - but sessionRow lived only on the createChatHost
  // DEPS. A missing host method does not throw "inject null" and does
  // not stay quiet: the computed throws TypeError mid-render and the
  // whole Files tab render tree dies (the blank pane, the missing
  // re-root). This pins the host surface itself, the way the component
  // consumes it.
  const ROW = {
    id: "sess-1",
    created_at: "2026-10-29T00:00:00Z",
    updated_at: "2026-10-29T00:00:00Z",
    workspace: "/home/proj",
  };
  const deps: ChatHostDeps = {
    setTabSessionRef: () => {},
    refreshSessions: () => {},
    sessionRow: (id) => (id === "sess-1" ? ROW : undefined),
    markRead: () => {},
    isFocused: () => false,
  };
  const host = createChatHost(deps);

  it("host.sessionRow is a function (dead seam = TypeError mid-render)", () => {
    assert.equal(typeof host.sessionRow, "function");
  });

  it("host.sessionRow delegates to the deps' resolver (unknown -> undefined)", () => {
    assert.deepEqual(host.sessionRow("sess-1"), ROW);
    assert.equal(host.sessionRow("missing"), undefined);
  });

  it("the Files tab's row-workspace trigger reads through the host", () => {
    // the exact consume shape FilesTab.vue's rowWorkspace computed uses
    assert.equal(host.sessionRow("sess-1")?.workspace ?? null, "/home/proj");
  });
});

describe("settings: default workspace knob (structural pins)", () => {
  const tab = read("app/components/settings/SettingsTab.vue");
  const settingsServer = readFileSync(
    `${root}/../../cmd/omega/settings.go`,
    "utf8",
  );
  const wsSelect = read("app/components/chat/WorkspaceSelect.vue");

  it("the settings pane renders a path + browse row for the knob", () => {
    assert.match(tab, /server\.default_workspace/);
    assert.match(tab, /class="setting-path"/);
    assert.match(tab, /WorkspaceSelect/, "browse reuses the workspace picker");
    assert.match(tab, /browse-label="browse"/);
  });

  it("the picker's browse mode shows a fixed chip, not the value chip", () => {
    assert.match(wsSelect, /browseLabel/);
  });

  it("the Go schema carries the knob in the server group", () => {
    assert.match(settingsServer, /"server\.default_workspace"/);
    assert.match(settingsServer, /DefaultWorkspace/);
  });
});