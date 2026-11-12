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
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

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

  it("the scrollport's shrink chain runs wrap -> .messages (the live body is gone)", () => {
    // card t_5bb319f6 retired the run-only body; the chain that must hold
    // is the one every stacked run uses: the wrap shrinks below content,
    // the column owns the overflow
    const wrap = rule(css(), ".messages-wrap {");
    assert.ok(wrap, ".messages-wrap rule not found");
    assert.match(wrap, /min-height:\s*0/, "the wrap must shrink below content");
    const messages = rule(css(), ".messages {");
    assert.ok(messages, ".messages rule not found");
    assert.match(messages, /flex:\s*1/);
    assert.match(messages, /overflow-y:\s*auto/);
    assert.doesNotMatch(
      cssNoComments(),
      /\.messages\.live-body/,
      "no run-only scroll body: the pin's card (t_5bb319f6) renders one column",
    );
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
      "the scroll chain must be class-based, never a :has() override",
    );
  });

  it("the live pin rides the column's own scroll listener", () => {
    const src = chatTab();
    const pin = src.indexOf('class="live-pin"');
    assert.ok(pin !== -1, "the pinned question row is missing");
    // ONE scrollport: the history and the streaming turn share it, so a
    // mid-stream scroll-up reaches the older messages (t_5bb319f6)
    const listeners = src.match(/@scroll="onScroll"/g) ?? [];
    assert.equal(listeners.length, 1, "exactly one scroll container is wired");
    assert.ok(!/liveBodyEl/.test(src), "the run-only body is retired");
  });

  it("the tail-follow gate yields to manual scrolls", () => {
    const src = chatTab();
    // no append may stomp on a manual scroll: follow only when pinned
    assert.match(
      src,
      /if\s*\(!force\s*&&\s*!pinned\.value\)\s*return/,
      "the follow gate must return when the user scrolled away from the tail",
    );
  });
});

describe("content-change follow (t_bef3f45e)", () => {
  it("the scroll decision routes through the shrink-clamp guard", () => {
    const src = chatTab();
    assert.match(
      src,
      /pinned\.value = resumesFollow\(/,
      "onScroll must derive the follow state from resumesFollow",
    );
    // the guard fuel: the scroller height at the last scroll event -
    // declared once, written in the scroll handler and in the follow
    // write's own-event branch (which resyncs it without deciding)
    const writers = src.match(/lastScrollHeight\s*=/g) ?? [];
    assert.equal(
      writers.length,
      3,
      "lastScrollHeight: the declaration plus two scroll-event-path writes",
    );
  });

  it("a ResizeObserver follows content growth between transcript changes", () => {
    const src = chatTab();
    assert.match(
      src,
      /new ResizeObserver\(onContentResize\)/,
      "the content observer must be the follow's second driver",
    );
    assert.match(src, /function resizeContentRO\(/, "the target sync exists");
    // the callback follows under the pinned gate and keeps the pill fresh
    const cb = src.indexOf("function onContentResize");
    assert.ok(cb !== -1, "onContentResize missing");
    const body = src.slice(cb, src.indexOf("}", src.indexOf("jumpPill.value", cb)));
    assert.match(body, /scrollDown\(\)/, "the RO callback follows (scrollDown gates on pinned)");
    assert.match(
      body,
      /jumpPill\.value = pillVisible\(/,
      "the RO callback keeps the pill fresh between scroll events",
    );
  });

  it("the RO targets re-sync on every growth surface", () => {
    const src = chatTab();
    // transcript deltas (items mount into the one column)
    const tw = src.indexOf("watch(transcript, () => {");
    assert.ok(tw !== -1, "the transcript watcher exists");
    assert.match(
      src.slice(tw, tw + 300),
      /resizeContentRO/,
      "the transcript watcher must re-sync the RO targets",
    );
    // a run starting re-syncs too (the pin slot mounts inside the column)
    const lw = src.indexOf("watch(liveLayout,");
    assert.ok(lw !== -1, "the liveLayout watch exists");
    assert.match(
      src.slice(lw, lw + 300),
      /resizeContentRO/,
      "the liveLayout watch must re-sync the RO targets",
    );
    // mount-time priming
    const mounted = src.indexOf("onMounted(async () => {");
    assert.ok(mounted !== -1);
    assert.match(
      src.slice(mounted, mounted + 900),
      /resizeContentRO/,
      "the mount primes the RO targets",
    );
    // teardown
    assert.match(src, /contentRO\?\.disconnect\(\)/, "the observer disconnects on unmount");
  });
});