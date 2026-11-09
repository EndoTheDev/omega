// omega webui: the viewer's image branch (card t_08d822bb) - the read's
// mime decides: image/* renders an <img> on GET /files/raw, every other
// binary keeps the honest "binary file - no preview" notice. The suite
// pins the structural wiring (ViewerTab branches, the raw URL law, the
// backend's sniff + jail) and the fetch wire. Run:
// node --test test/viewerimg.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { filesRawUrl, fetchFilesRead } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
// the repo root (webui/webui is two levels deep)
const repo = fileURLToPath(new URL("../../", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const readRepo = (rel: string) => readFileSync(`${repo}/${rel}`, "utf8");

const viewer = read("app/components/ViewerTab.vue");
const omega = read("app/composables/omega.ts");
const serverFiles = readRepo("harness/files/routes.go");
// the /files/raw registration lives in the files capsule (the module
// routes rail, card t_48eeb015)
const filesPlugin = readRepo("harness/files/plugin.go");

// --- the viewer's image branch -------------------------------------------------

describe("ViewerTab's image branch (t_08d822bb)", () => {
  it("binds the <img> to GET /files/raw, gated on the sniffed mime", () => {
    assert.match(viewer, /v-if="view === 'image'"/, "an image mime renders its own branch");
    assert.match(viewer, /class="viewer-img"/, "the <img> element exists");
    assert.match(viewer, /:src="rawSrc"/, "the img rides the computed raw url");
    assert.match(viewer, /filesRawUrl\(/, "the raw url comes from the omega seam");
    assert.match(
      viewer,
      /fileViewKind\(props\.viewer\.name, mime\.value, binary\.value\)/,
      "mime, not extension, decides the branch",
    );
  });

  it("the honest binary notice stays for every NON-image binary", () => {
    assert.match(viewer, /binary file - no preview/, "the notice survives");
    assert.match(viewer, /view\.value !== "code"/, "binary files never line-split");
  });

  it("the image fits the pane, scrollable when larger", () => {
    assert.match(viewer, /viewer-imgwrap[^]*overflow: auto/, "the wrap scrolls");
    assert.match(viewer, /object-fit: contain/, "the img contains");
  });

  it("the mime rides the read payload; absent is the old path", () => {
    assert.match(omega, /mime\?: string/, "FilesReadPayload carries the mime");
    assert.match(viewer, /data\.mime \?\? ""/, "a pre-law server's absent mime degrades");
  });
});

// --- the raw route, backend shape ------------------------------------------------

describe("GET /files/raw (t_08d822bb)", () => {
  it("is registered; images only; the sniff is stdlib", () => {
    assert.match(
      filesPlugin,
      /RegisterRoute\("\/files\/raw"/,
      "the route is registered (the files capsule's route rail)",
    );
    assert.match(serverFiles, /http\.DetectContentType/, "the sniff is stdlib, no new dependency");
    assert.match(
      serverFiles,
      /strings\.HasPrefix\(mime, "image\/"\)/,
      "the raw route serves image/* (and pdf, t_42c8aced)",
    );
    assert.match(serverFiles, /not renderable/, "non-renderable mimes refuse loudly");
  });

  it("rides the EXACT read surface's root jail, never a copy", () => {
    const handler = serverFiles.slice(
      serverFiles.indexOf("func (r *Routes) handleFilesRaw"),
      serverFiles.indexOf("// filesPostCap"),
    );
    for (const step of [
      "validateRelPath(",
      "resolveFilesRoot(",
      "resolveUnderRoot(",
      "ensureUnderRoot(",
      "os.IsNotExist(err) {",
      "fileError(w, http.StatusNotFound",
    ]) {
      assert.ok(handler.includes(step), `the raw handler runs ${step}`);
    }
  });
});

// --- the raw-url wire -------------------------------------------------------------

describe("filesRawUrl wire", () => {
  it("builds /files/raw with the encoded path + session", () => {
    assert.equal(
      filesRawUrl(".agent-browser/tmp/screenshots/screenshot x.png", "sess-1"),
      `/files/raw?path=${encodeURIComponent(".agent-browser/tmp/screenshots/screenshot x.png").replace("%20", "+")}&session=sess-1`,
    );
    assert.equal(filesRawUrl("a.svg"), "/files/raw?path=a.svg", "no session, no param");
  });

  it("fetchFilesRead passes the mime through untouched", async () => {
    const savedFetch = globalThis.fetch;
    afterEach(() => {
      globalThis.fetch = savedFetch as typeof globalThis.fetch;
    });
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({ content: "", truncated: false, binary: true, mime: "image/png" }),
        { status: 200 },
      )) as typeof globalThis.fetch;
    const data = await fetchFilesRead("shot.png", "s1");
    assert.equal(data.mime, "image/png");
    // a pre-law server's absent mime degrades to undefined, never a lie
    globalThis.fetch = (async () =>
      new Response(JSON.stringify({ content: "x", truncated: false, binary: false }), {
        status: 200,
      })) as typeof globalThis.fetch;
    assert.equal((await fetchFilesRead("a.txt")).mime, undefined);
  });
});