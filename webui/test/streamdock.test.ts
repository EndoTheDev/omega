// omega webui: mid-stream live status docks at the stream tail, the
// scrollbar hugs the pane edge (card t_c252b9c6). Endo's report
// (2026-10-29, classic appearance): while a run streamed, the activity
// line ("thinking · 1s") rendered in a narrow strip at the TOP-RIGHT of
// the pane - clipped by the pane edge - and the scrollbar floated
// inset from the pane's right edge with whitespace to its spare.
// Root cause: .messages-wrap was a ROW flex container, so the
// activity/status line sat BESIDE the .messages scrollport as a
// phantom right strip instead of BELOW it, and the strip ate the
// scrollbar's right margin. The fix makes the wrap vertical: the
// scrollport spans the full pane body (border-to-border scrollbar,
// the bubble max-widths still cap the readable column) and the
// activity/status lines park under the scroller - at the stream tail,
// just above the composer, inside the flow, never clipped. The jump
// pill keeps its absolute placement on the wrap: an absolutely
// positioned child of the scroll container itself scrolls away with
// the content.
// These are structural source-contract tests (house pattern: read the
// .vue/.css, assert the wiring).
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
const chatMessage = () =>
  readFileSync(
    join(root, "app", "components", "chat", "ChatMessage.vue"),
    "utf8",
  );

// pull a top-level CSS rule body: selector ... { ... } (flat match - the
// rules these tests read carry no nested blocks)
const rule = (src: string, selector: string): string | null => {
  const idx = src.indexOf(selector);
  if (idx === -1) return null;
  const open = src.indexOf("{", idx);
  const close = src.indexOf("}", open);
  if (open === -1 || close === -1) return null;
  return src.slice(open + 1, close);
};

describe("messages-wrap: the scrollport spans the pane body (t_c252b9c6)", () => {
  it(".messages-wrap stacks VERTICALLY - no side-by-side right strip", () => {
    const body = rule(css(), ".messages-wrap {");
    assert.ok(body, ".messages-wrap rule not found");
    assert.match(body, /flex-direction:\s*column/, "the wrap must be a column flex container: the scrollport on top, the activity/status line under it");
    assert.match(body, /position:\s*relative/, "the wrap keeps owning the pill's absolute placement");
  });

  it(".messages (the scrollport) is uncapped - the scrollbar hugs the pane edge", () => {
    const body = rule(css(), ".messages {");
    assert.ok(body, ".messages rule not found");
    assert.match(body, /overflow-y:\s*auto/, "the scroller must own the overflow");
    assert.doesNotMatch(
      body,
      /max-width/,
      "the scrollport must not be width-capped: the readability caps live on the bubbles (.bubble/.toolcall), not on the scroll container",
    );
  });

  it("the old row-direction override for the live branch is gone", () => {
    const src = css();
    assert.doesNotMatch(
      src,
      /\.messages-wrap:has\(\.live-body\)/,
      "the :has(.live-body) column override is moot once the wrap stacks vertically always",
    );
  });
});

describe("the streaming status line rides the stream tail (t_c252b9c6)", () => {
  // the wrap's children, in template order: the scroller, then the
  // activity/status lines, then the pill
  const wrapBlock = (): string => {
    const src = chatTab();
    const start = src.indexOf('class="messages-wrap"');
    assert.ok(start !== -1, "the .messages-wrap element must exist");
    return src.slice(start);
  };

  it("the activity/status lines are SIBLINGS that follow the scroller, not its children", () => {
    const src = chatTab();
    const scrollerIdx = src.indexOf('class="messages"');
    const activityIdx = src.indexOf('class="activity-line"');
    const statusIdx = src.indexOf('class="status-line"');
    const scrollerClose = src.indexOf("</div>", scrollerIdx);
    assert.ok(scrollerIdx !== -1 && activityIdx !== -1 && statusIdx !== -1);
    // both lines sit AFTER the scroller's element and OUTSIDE it
    assert.ok(
      activityIdx > scrollerClose,
      "the activity line must render below the scrollport (stream tail, above the composer), never inside the scroller",
    );
    assert.ok(
      statusIdx > scrollerClose,
      "the status line must render below the scrollport (stream tail, above the composer), never inside the scroller",
    );
    // and inside the wrap, not past it (composer comes later)
    const composerIdx = src.indexOf("<AppComposer");
    assert.ok(activityIdx < composerIdx, "the activity line belongs to the wrap, above the composer");
    assert.ok(statusIdx < composerIdx, "the status line belongs to the wrap, above the composer");
  });

  it("both lines carry the placement class contract (above-composer flow, no absolute)", () => {
    const activity = rule(css(), ".activity-line {");
    assert.ok(activity, ".activity-line rule not found");
    assert.doesNotMatch(activity, /position:\s*absolute/, "the activity line is a flow child of the wrap, not an absolute dock");
    const status = rule(css(), ".status-line {");
    assert.ok(status, ".status-line rule not found");
    assert.doesNotMatch(status, /position:\s*absolute/, "the status line is a flow child of the wrap, not an absolute dock");
  });

  it("the jump pill keeps its absolute wrap placement (it must not scroll away)", () => {
    const pill = rule(css(), ".jump-live {");
    assert.ok(pill, ".jump-live rule not found");
    assert.match(pill, /position:\s*absolute/, "the pill stays viewport-pinned on the wrap");
    // and the template renders it inside the wrap, not the scroller
    const src = chatTab();
    const pillIdx = src.indexOf('class="jump-live"');
    const scrollerClose = src.indexOf("</div>", src.indexOf('class="messages"'));
    assert.ok(pillIdx > scrollerClose, "the pill is a child of the wrap, never of the scrolling .messages");
  });
});

describe("the streaming bubble's elapsed meter rides the content tail (t_c252b9c6)", () => {
  it("the live .msg-elapsed renders while STREAMING, inside the bubble", () => {
    const src = chatMessage();
    assert.match(src, /streaming && elapsedLabel[\s\S]*?class="msg-elapsed"/, "the live elapsed meter must show mid-stream at the bubble's tail");
  });

  it("the action row (fork/copy/final duration) stays hidden while streaming", () => {
    const src = chatMessage();
    assert.match(src, /v-if="!streaming && role === 'assistant'"/, "the settled action row only mounts after the run ends");
  });
});