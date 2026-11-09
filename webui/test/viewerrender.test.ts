// omega webui: the viewer's content-class branches (card t_42c8aced) -
// markdown renders through the CHAT grammar + the codeblock decoration,
// pdf rides the browser's native viewer inside an <iframe> (the raw
// route's widened mime gate, no renderer dependency), a non-previewable
// binary keeps the honest no-preview state (name + size + download
// lane), code/text keeps the line-numbered monospace body. The suite
// pins the pure branch logic, the wire (download lane, size passthrough)
// and the structural wiring. Run: node --test test/viewerrender.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  bytesLabel,
  fileViewKind,
  filesRawUrl,
  fetchFilesRead,
} from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
// the repo root (webui/webui is two levels deep - harness/ lives beside webui/)
const repo = fileURLToPath(new URL("../../", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const readRepo = (rel: string) => readFileSync(`${repo}/${rel}`, "utf8");

const viewer = read("app/components/ViewerTab.vue");
const omega = read("app/composables/omega.ts");
const serverFiles = readRepo("harness/files/routes.go");

// --- the pure branch logic -------------------------------------------------------

describe("fileViewKind (t_42c8aced)", () => {
  it("mime decides pdf/image - sniffed bytes, never the extension", () => {
    assert.equal(fileViewKind("doc.pdf", "application/pdf", true), "pdf");
    assert.equal(fileViewKind("shot.png", "image/png", true), "image");
    assert.equal(fileViewKind("icon.svg", "image/svg+xml", false), "image");
    // extension tricks do not unlock a render lane: a text-named .pdf
    // sniffs text/plain and stays text
    assert.equal(fileViewKind("fake.pdf", "text/plain; charset=utf-8", false), "code");
    assert.equal(fileViewKind("a.md", "application/octet-stream", true), "binary");
  });

  it("binary keeps the no-preview state before any extension check", () => {
    assert.equal(fileViewKind("omega", "application/octet-stream", true), "binary");
    assert.equal(fileViewKind("blob", "", true), "binary");
  });

  it(".md/.markdown text renders markdown; everything else is the text body", () => {
    assert.equal(fileViewKind("SKILL.md", "text/plain; charset=utf-8", false), "markdown");
    assert.equal(fileViewKind("notes.markdown", "text/plain; charset=utf-8", false), "markdown");
    assert.equal(fileViewKind("README.MD", "text/plain; charset=utf-8", false), "markdown");
    assert.equal(fileViewKind("main.py", "text/x-python", false), "code");
    assert.equal(fileViewKind("schema", "", false), "code");
    assert.equal(fileViewKind("no-ext", "text/plain; charset=utf-8", false), "code");
  });
});

describe("bytesLabel", () => {
  it("renders the size the binary state announces; absent is empty", () => {
    assert.equal(bytesLabel(512), "512 B");
    assert.equal(bytesLabel(2048), "2 KiB");
    assert.equal(bytesLabel(1024 * 1024 * 1.5), "1.5 MiB");
    assert.equal(bytesLabel(undefined), "");
  });
});

// --- the viewer's five bodies ----------------------------------------------------

describe("ViewerTab's content-class branches (t_42c8aced)", () => {
  it("the skill body IS markdown - the chat grammar renders it", () => {
    assert.match(viewer, /kind === "skill"\) return "markdown"/, "skills are markdown");
    assert.match(viewer, /import {[\S\s]*renderMarkdown[\S\s]*} from "\.\.\/composables\/omega"/, "the chat renderer");
    assert.match(
      viewer,
      /renderedMd\.value = view\.value === "markdown" \? renderMarkdown\(content\.value\) : ""/,
      "one render path, gated on the branch",
    );
    assert.match(viewer, /v-html="renderedMd"/, "the markdown body");
    assert.match(viewer, /class="viewer-md md"/, "the .md grammar context");
  });

  it("fenced blocks get the shared codebar decoration after the render", () => {
    assert.match(viewer, /decorateCodeblocks\(mdEl\.value, collapsed\)/, "the chat's decoration pass");
    assert.match(viewer, /nextTick\(\(\) => decorateCodeblocks/, "applied after the v-html paints");
  });

  it("pdf rides the browser's native viewer inside an iframe on the raw route", () => {
    assert.match(viewer, /v-else-if="view === 'pdf'"/, "the pdf branch");
    assert.match(viewer, /class="viewer-pdf"/, "the iframe element");
    assert.match(viewer, /:src="rawSrc"/, "same-origin pdf bytes");
    assert.ok(!viewer.includes("pdfjs"), "no pdf renderer dependency - the browser owns pdf");
  });

  it("binary keeps the honest no-preview state: name + size + download", () => {
    assert.match(viewer, /binary file - no preview/, "the notice");
    assert.match(viewer, /:href="downloadUrl"/, "the download lane link");
    assert.match(viewer, /:download="leafName"/, "the browser prefills the save name");
    assert.match(viewer, /filesRawUrl\(\s*props\.viewer\.name, focusedSession\?\.value \?\? undefined, \{ download: true \}\)/, "the download query rides the raw url");
    assert.match(viewer, /bytesLabel\(size\.value\)/, "the size label");
  });

  it("code/text keeps the line-numbered monospace body", () => {
    assert.match(viewer, /class="viewer-body"/, "the text body");
    assert.match(viewer, /class="viewer-no"/, "the line-number gutter");
    assert.match(viewer, /font-family: var\(--mono\)/, "the mono tokens");
  });
});

// --- the wire --------------------------------------------------------------------

describe("the t_42c8aced wire", () => {
  it("filesRawUrl carries the download lane; the default omits it", () => {
    assert.equal(filesRawUrl("blob.bin", undefined, { download: true }), "/files/raw?path=blob.bin&download=1");
    assert.equal(
      filesRawUrl("blob.bin", "s1", { download: true }),
      "/files/raw?path=blob.bin&session=s1&download=1",
    );
    assert.equal(filesRawUrl("a.pdf"), "/files/raw?path=a.pdf", "render bytes need no download param");
  });

  it("FilesReadPayload carries the size; fetchFilesRead passes it through", async () => {
    assert.match(omega, /size\?: number/, "the payload carries the size");
    const savedFetch = globalThis.fetch;
    afterEach(() => {
      globalThis.fetch = savedFetch as typeof globalThis.fetch;
    });
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({ content: "", truncated: false, binary: true, mime: "application/octet-stream", size: 72 }),
        { status: 200 },
      )) as typeof globalThis.fetch;
    const data = await fetchFilesRead("omega.bin", "s1");
    assert.equal(data.size, 72);
    // a pre-law server's absent size degrades to undefined, never a lie
    globalThis.fetch = (async () =>
      new Response(JSON.stringify({ content: "x", truncated: false, binary: false }), {
        status: 200,
      })) as typeof globalThis.fetch;
    assert.equal((await fetchFilesRead("a.txt")).size, undefined);
  });

  it("the backend rides pdf through the same gate; nosniff guards both lanes", () => {
    assert.match(
      serverFiles,
      /strings\.HasPrefix\(mime, "image\/"\) \|\| mime == "application\/pdf"/,
      "the renderable pair",
    );
    assert.match(serverFiles, /Content-Disposition/, "the attachment lane");
    assert.match(serverFiles, /"download"/, "the download query decides the lane");
    // one guard set covers every lane that passes the gate; the refusals
    // write no headers at all
    const count = (serverFiles.match(/X-Content-Type-Options/g) ?? []).length;
    assert.ok(count === 1, `nosniff is set once, covering both lanes (${count})`);
  });
});
