// omega webui: the stacked live-run body stays a real scrollport
// (card t_64878551). Endo's report (2026-10-29, stacked appearance):
// during a stacked run the live body was fully unscrollable - the wheel
// did nothing for the whole run - read as the wrap-to-live-body flex
// shrink chain being lost in the t_c252b9c6 wrap restructure. The
// wrap-to-live-body shrink law: under the ALWAYS-vertical wrap every
// flex item on the wrap-to-live-body path must shrink below its content
// (min-height: 0) or the body grows past the pane and overflow-y:auto
// has nothing to scroll (content is clipped, never contained). The
// live-body rule is class-based on the BRANCH class - the appearance
// chip must not own scroll geometry, and :has() stays retired
// (9208ac8). The JS side is contract too: the live body owns the
// scroll listeners while it is mounted, and the tail-follow gate keeps
// manual scrolls (it resumes only at the bottom).
// Structural source-contract tests (house pattern: read the .vue/.css,
// assert the wiring).
// Run: node --test test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = new URL("..", import.meta.url).pathname;

const css = () =>
  readFileSync(join(root, "app", "assets", "css", "main.css"), "utf8");
const chatTab = () =>
  readFileSync(join(root, "app", "components", "ChatTab.vue"), "utf8");

// pull a top-level CSS rule body: selector ... { ... } (flat match)
const rule = (src: string, selector: string): string | null => {
  const i = src.indexOf(selector);
  if (i === -1) return null;
  const open = src.indexOf("{", i);
  const close = src.indexOf("}", open);
  if (open === -1 || close === -1) return null;
  return src.slice(open + 1, close);
};

const cssNoComments = () => css().replace(/\/\*[\s\S]*?\*\//g, " ");

const hasProp = (body: string | null, prop: string): boolean =>
  body !== null && new RegExp(`(?:^|[;\\\\s])${prop}\\s*:`).test(body);

describe("live-run scroll chain: the body is contained, not clipped (t_64878551)", () => {
  it(".messages-wrap carries the upstream shrink floor (min-height: 0)", () => {
    const body = rule(css(), ".messages-wrap {");
    assert.ok(body, ".messages-wrap rule not found");
    assert.match(
      body,
      /min-height:\s*0/,
      "the wrap must shrink below content or the live body grows past the pane",
    );
    assert.match(
      body,
      /flex-direction:\s*column/,
      "the wrap is ALWAYS vertical (t_c252b9c6): pin above, body below",
    );
  });

  it(".messages.live-body carries min-height: 0 on the BRANCH class", () => {
    const body = rule(css(), ".messages.live-body {");
    assert.ok(
      body,
      ".messages.live-body rule not found: the live body's shrink chain must live on a class-based rule",
    );
    assert.match(body, /min-height:\s*0/);
  });

  it("the scroll geometry does not hang on the stacked appearance chip", () => {
    const stacked = rule(css(), ".messages.stacked.live-body {");
    assert.ok(stacked, ".messages.stacked.live-body rule not found");
    assert.doesNotMatch(
      stacked,
      /min-height/,
      "min-height must not live on .stacked: the appearance chip is not a scroll owner",
    );
  });

  it(".messages stays the scrollport (flex: 1 + overflow-y: auto)", () => {
    const body = rule(css(), ".messages {");
    assert.ok(body, ".messages rule not found");
    assert.match(body, /flex:\s*1/);
    assert.match(body, /overflow-y:\s*auto/);
  });

  it("the :has(.live-body) override stays retired", () => {
    assert.doesNotMatch(
      cssNoComments(),
      /:has\(\.live-body\)/,
      "the wrap is vertical always - no direction-dependent :has() rule",
    );
  });

  it("the shrink chain is not :has()-dependent", () => {
    assert.doesNotMatch(
      cssNoComments(),
      /:has\([^)]*live-body/,
      "the live body's shrink chain must be class-based, never a :has() override",
    );
  });

  it("the live branch wires the pin, the body and the scroll listener", () => {
    const src = chatTab();
    const pin = src.indexOf('class="live-pin"');
    const body = src.indexOf('"messages live-body stacked"');
    assert.ok(pin !== -1, "the pinned question header is missing");
    assert.ok(
      body !== -1,
      'the live body must render class "messages live-body stacked"',
    );
    assert.ok(
      pin < body,
      "the pinned question renders ABOVE the agent body",
    );
    // the body element carries the scroll wiring (tail-follow + pill)
    const afterBody = src.slice(body, body + 300);
    assert.match(
      afterBody,
      /@scroll="onScroll"/,
      "the live body owns the scroll listener while it is mounted",
    );
  });

  it("the tail-follow gate yields to manual scrolls", () => {
    const src = chatTab();
    // the live body is the ACTIVE scroll container while mounted
    assert.match(
      src,
      /liveBodyEl\.value\s*\?\?\s*messagesEl\.value/,
      "scrollEl() must prefer the live body while the live layout is mounted",
    );
    // no append may stomp on a manual scroll: follow only when pinned
    assert.match(
      src,
      /if\s*\(!force\s*&&\s*!pinned\.value\)\s*return/,
      "the follow gate must return when the user scrolled away from the tail",
    );
  });
});