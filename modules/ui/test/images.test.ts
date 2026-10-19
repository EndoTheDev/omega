// omega webui: image input support - the pure helpers (magic sniff,
// data URLs, retry image carry). Run: node --test modules/ui/test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  dataUrl,
  lastUserImages,
  lastUserText,
  mediaTypeFromBase64,
  type OmegaImage,
  type TranscriptItemLike,
} from "../app/composables/omega.ts";

function b64(...bytes: number[]): string {
  return Buffer.from(new Uint8Array(bytes)).toString("base64");
}

describe("mediaTypeFromBase64", () => {
  it("detects png, jpeg, gif, bmp from magic bytes", () => {
    const png = b64(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a);
    assert.equal(mediaTypeFromBase64(png), "image/png");
    assert.equal(mediaTypeFromBase64(b64(0xff, 0xd8, 0xff, 0xe0)), "image/jpeg");
    assert.equal(mediaTypeFromBase64(b64(0x47, 0x49, 0x46, 0x38, 0x39, 0x61)), "image/gif");
    assert.equal(mediaTypeFromBase64(b64(0x42, 0x4d, 0x00)), "image/bmp");
  });
  it("detects webp only with the WEBP marker at offset 8", () => {
    const webp = b64(0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0, 0x57, 0x45, 0x42, 0x50);
    const riffOnly = b64(0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0);
    const riffWrong = b64(0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0, 0x41, 0x56, 0x49, 0x20);
    assert.equal(mediaTypeFromBase64(webp), "image/webp");
    assert.equal(mediaTypeFromBase64(riffOnly), null);
    assert.equal(mediaTypeFromBase64(riffWrong), null);
  });
  it("rejects non-image data and malformed base64", () => {
    assert.equal(mediaTypeFromBase64(Buffer.from("<html>hello</html>").toString("base64")), null);
    assert.equal(mediaTypeFromBase64("!!!!not base64!!!"), null);
  });
  it("partial signatures do not match (full byte compare)", () => {
    assert.equal(mediaTypeFromBase64(b64(0x89, 0x50, 0x4e)), null);
    assert.equal(mediaTypeFromBase64(b64(0x42)), null);
  });
});

describe("dataUrl", () => {
  it("builds a data: URL from the image shape", () => {
    const img: OmegaImage = { media_type: "image/png", base64: "iVBORw0KGgo=" };
    assert.equal(dataUrl(img), "data:image/png;base64,iVBORw0KGgo=");
  });
});

describe("lastUserImages", () => {
  const png: OmegaImage = { media_type: "image/png", base64: "iVBORw0KGgo=" };
  it("returns the last user message's images", () => {
    const items: TranscriptItemLike[] = [
      { kind: "message", role: "user", content: "with", images: [png] },
      { kind: "message", role: "assistant", content: "here" },
      { kind: "message", role: "user", content: "again", images: [png] },
    ];
    assert.deepEqual(lastUserImages(items), [png]);
  });
  it("returns [] when no user message or no images", () => {
    assert.deepEqual(lastUserImages([]), []);
    assert.deepEqual(lastUserImages([{ kind: "message", role: "user", content: "plain" }]), []);
    assert.deepEqual(lastUserImages([{ kind: "thinking", content: "..." }]), []);
  });
  it("lastUserText still works alongside", () => {
    const items: TranscriptItemLike[] = [{ kind: "message", role: "user", content: "hi", images: [] }];
    assert.equal(lastUserText(items), "hi");
    assert.deepEqual(lastUserImages(items), []);
  });
});