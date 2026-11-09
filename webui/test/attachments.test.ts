// omega webui: non-image attachment helpers (card t_8a569163) - the
// composer strip's and the sent bubble's pure side. Pins:
//   1. the kind -> glyph table (the iconset law's vocabulary, one shape
//      per class) and its agreement with the file tree's own table;
//   2. every glyph name is a real iconset entry (an unknown name THROWS
//      at render - a typo must fail here first);
//   3. the pick router (image lane vs upload lane);
//   4. the /attach-file wire: multipart, session_id part BEFORE the file
//      part (the parent card's named nit - reversed, the upload stages
//      under _pending and only /chat's relocation rescues it), the cap
//      refused before any byte leaves the browser;
//   5. the component wiring: the strip renders file chips, the picker
//      takes any file, the send path carries the reference array.
// Run: node --test webui/test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  attachmentIcon,
  attachmentSizeLabel,
  isImageFile,
} from "../app/composables/attachments.ts";
import {
  MAX_CHAT_FILE_BYTES,
  uploadAttachment,
} from "../app/composables/omega.ts";
import { ICON_NAMES } from "../app/composables/icons.ts";
import { glyphIcon, glyphKindFor } from "../../harness/files/ui/rowicon.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

describe("attachmentIcon: one class, one glyph", () => {
  it("maps the types the card names", () => {
    assert.equal(attachmentIcon("pdf"), "file-pdf");
    assert.equal(attachmentIcon("md"), "file-text");
    assert.equal(attachmentIcon("txt"), "file-text");
    assert.equal(attachmentIcon("csv"), "file-sheet");
    assert.equal(attachmentIcon("xlsx"), "file-sheet");
    assert.equal(attachmentIcon("json"), "file-config");
    assert.equal(attachmentIcon("yaml"), "file-config");
    assert.equal(attachmentIcon("go"), "file-code");
    assert.equal(attachmentIcon("py"), "file-code");
    assert.equal(attachmentIcon("sh"), "file-script");
    assert.equal(attachmentIcon("sqlite"), "file-db");
    assert.equal(attachmentIcon("zip"), "file-archive");
  });

  it("is case-insensitive and falls back to the generic file glyph", () => {
    assert.equal(attachmentIcon("PDF"), "file-pdf");
    assert.equal(attachmentIcon("Markdown"), "file-text");
    assert.equal(attachmentIcon(""), "file-generic");
    assert.equal(attachmentIcon("file"), "file-generic"); // the server's no-extension label
    assert.equal(attachmentIcon("wat"), "file-generic");
  });

  it("every glyph name is an iconset vocabulary entry", () => {
    const set = new Set(ICON_NAMES as readonly string[]);
    const kinds = [
      "pdf", "md", "txt", "csv", "xlsx", "json", "yaml", "go", "sh",
      "sqlite", "zip", "file", "unknown",
    ];
    for (const kind of kinds) {
      const name = attachmentIcon(kind);
      assert.ok(set.has(name), `${kind}: "${name}" is not in the iconset`);
    }
  });

  it("agrees with the file tree's table wherever both know the type", () => {
    // the SAME .md must carry the SAME glyph in a tree row and an
    // attachment chip - two tables, one vocabulary (drift fails here)
    for (const ext of [
      "md", "markdown", "txt", "log", "json", "yaml", "yml", "toml",
      "go", "ts", "tsx", "js", "mjs", "cjs", "vue", "py", "rs", "sh",
      "bash", "db", "sqlite",
    ]) {
      assert.equal(
        attachmentIcon(ext),
        glyphIcon(glyphKindFor(`file.${ext}`)),
        `${ext}: the strip and the tree disagree on the glyph`,
      );
    }
  });
});

describe("attachmentSizeLabel: the ONE byte formatter", () => {
  it("renders B / KiB / MiB", () => {
    assert.equal(attachmentSizeLabel(512), "512 B");
    assert.equal(attachmentSizeLabel(2048), "2 KiB");
    assert.equal(attachmentSizeLabel(Math.round(1.5 * 1024 * 1024)), "1.5 MiB");
  });
  it("an absent size is an empty label, never 'undefined'", () => {
    assert.equal(attachmentSizeLabel(undefined), "");
    assert.equal(attachmentSizeLabel(0), "0 B");
  });
});

describe("isImageFile: the pick router", () => {
  it("image mimes and image names ride the image lane", () => {
    assert.equal(isImageFile({ name: "clip.png", type: "image/png" }), true);
    assert.equal(isImageFile({ name: "shot.PNG", type: "" }), true);
    assert.equal(isImageFile({ name: "pic.jpeg", type: "" }), true);
    assert.equal(isImageFile({ name: "anim.webp", type: "application/octet-stream" }), true);
  });
  it("everything else rides the upload-reference lane", () => {
    assert.equal(isImageFile({ name: "notes.md", type: "text/markdown" }), false);
    assert.equal(isImageFile({ name: "paper.pdf", type: "application/pdf" }), false);
    assert.equal(isImageFile({ name: "rows.csv", type: "text/csv" }), false);
    assert.equal(isImageFile({ name: "Makefile", type: "" }), false);
  });
});

describe("uploadAttachment: the /attach-file wire", () => {
  type Call = { url: string; init: RequestInit };

  function stubFetch(answer: () => Response): Call[] {
    const calls: Call[] = [];
    (globalThis as { fetch: unknown }).fetch = (async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      return answer();
    }) as typeof fetch;
    return calls;
  }

  const ref = { kind: "md", name: "notes.md", size: 42, path: "/h/uploads/s1/notes.md" };

  it("posts multipart with session_id BEFORE the file part", async () => {
    const calls = stubFetch(() =>
      new Response(JSON.stringify(ref), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const out = await uploadAttachment(new File(["hello"], "notes.md"), "s1");
    assert.deepEqual(out, ref);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.url, "/attach-file");
    assert.equal(calls[0]!.init.method, "POST");
    const form = calls[0]!.init.body as FormData;
    assert.ok(form instanceof FormData, "the body is multipart/form-data");
    const names = [...form.keys()];
    // the part ORDER is load-bearing (t_1ffd07de's nit): the handler
    // reads the session id before the bytes, or the upload stages
    assert.deepEqual(names, ["session_id", "file"]);
    assert.equal(form.get("session_id"), "s1");
  });

  it("a draft upload carries no session part (the server stages it)", async () => {
    const calls = stubFetch(() => new Response(JSON.stringify(ref), { status: 200 }));
    await uploadAttachment(new File(["x"], "notes.md"), null);
    const form = calls[0]!.init.body as FormData;
    assert.deepEqual([...form.keys()], ["file"]);
  });

  it("a 413 answers the server's own words", async () => {
    stubFetch(() => new Response("file too large (10485761 bytes, max 10485760)", { status: 413 }));
    await assert.rejects(
      () => uploadAttachment(new File(["x"], "big.bin")),
      /attach-file: 413: file too large/,
    );
  });

  it("the cap is refused BEFORE any request leaves the browser", async () => {
    let called = 0;
    (globalThis as { fetch: unknown }).fetch = (async () => {
      called++;
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    const oversize = new File([new Uint8Array(MAX_CHAT_FILE_BYTES + 1)], "big.bin");
    await assert.rejects(() => uploadAttachment(oversize), /too large: .*max 10 MB/);
    assert.equal(called, 0, "no request rides an oversize pick");
  });
});

describe("the composer + transcript wiring", () => {
  it("the picker takes ANY file - the image-only accept attribute is dead", () => {
    const composer = read("app/components/chat/AppComposer.vue");
    assert.ok(!composer.includes('accept="image/*"'), "no image-only accept");
    assert.match(composer, /ref="fileInput"[\s\S]*?type="file"[\s\S]*?multiple/, "the picker stays multiple");
  });

  it("the strip renders a file chip: type glyph + name + size, no <img>", () => {
    const composer = read("app/components/chat/AppComposer.vue");
    const start = composer.indexOf('v-for="(f, i) in files"');
    const end = composer.indexOf("attach-file-remove");
    assert.ok(start > 0 && end > start, "the file-chip block exists");
    const chip = composer.slice(start, end);
    assert.match(chip, /:name="attachmentIcon\(f\.kind\)"/, "the chip carries the type glyph");
    assert.match(chip, /attachmentSizeLabel\(f\.size\)/, "the chip carries the size");
    assert.ok(!chip.includes("<img"), "a file chip is never thumbnailed");
  });

  it("the composer emits the picked files and the strip's removal", () => {
    const composer = read("app/components/chat/AppComposer.vue");
    assert.match(composer, /"attach-file-refs", files: File\[\]/, "the pick hands the host raw files");
    assert.match(composer, /"remove-file-attachment", index: number/, "the chip's x is a removal");
    assert.match(composer, /emit\("attach-file-refs", refs\)/, "the non-image branch emits");
    assert.match(composer, /isImageFile\(file\)/, "the pick routes through the shared router");
  });

  it("ChatTab wires the composer to the host and passes the pending refs", () => {
    const tab = read("app/components/ChatTab.vue");
    assert.match(tab, /:files="state\.files"/);
    assert.match(tab, /host\.attachFileRefs\(stateKey, files\)/);
    assert.match(tab, /host\.removeFileAttachment\(stateKey, i\)/);
  });

  it("the sent bubble renders the reference chips (ChatMessage <- ChatItem)", () => {
    assert.match(read("app/components/ChatItem.vue"), /:attachments="item\.attachments"/);
    const msg = read("app/components/chat/ChatMessage.vue");
    assert.match(msg, /v-if="attachments\?\.length"/);
    assert.match(msg, /:name="attachmentIcon\(f\.kind\)"/);
    assert.match(msg, /attachmentSizeLabel\(f\.size\)/);
    assert.match(msg, /attachments\?: OmegaAttachment\[\]/, "the prop is typed against the wire record");
  });

  it("the send path carries the references on the user message", () => {
    const chat = read("app/composables/chat.ts");
    assert.match(chat, /attachments: files\.length \? files : undefined/, "the send echoes the refs");
    assert.match(chat, /!st\.files\.length/, "a file-only turn is sendable");
    assert.match(chat, /uploadAttachment\(file, st\.sessionId\)/, "the host uploads with the tab's session");
  });
});
