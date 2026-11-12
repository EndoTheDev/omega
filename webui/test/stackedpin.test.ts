// omega webui: the stacked live-run pin must not lock the scroll
// (card t_5bb319f6). Endo's report (2026-11-12, stacked appearance):
// mid-stream the last user message pinned at the top and the pane would
// not scroll past it - prior messages were unreadable for the whole run.
//
// Cause: the two-region split of t_c40164cf rendered the pinned header
// PLUS a run-only body, and that body was the ONLY scroll region - older
// history was not in the DOM at all, so scrollTop 0 sat directly under
// the pin until agent_end. The pin now rides a sticky slot inside the one
// scrollport: history above it stays scrollable, the append follow stays
// gated on the pinned flag.
//
// Part 1 replays a fake stream through the same follow decisions the
// component makes (resumesFollow / pillVisible / the pinned gate in
// scrollDown). Part 2 pins the wiring in the component and the CSS so
// the model cannot drift away from the shipped path silently.
// Run: node --test test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";
import { pillVisible, resumesFollow } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const chatTab = () =>
  readFileSync(join(root, "app", "components", "ChatTab.vue"), "utf8");
const css = () =>
  readFileSync(join(root, "app", "assets", "css", "main.css"), "utf8");

// FakePane is the scrollport as ChatTab wires it: `append()` mirrors
// scrollDown under its `if (!force && !pinned.value) return` gate (and
// records the scrollTop its write landed at), `deliver()` mirrors the
// browser handing back the scroll event that write raised, and
// `scroll()` mirrors a reader gesture through onScroll. Heights are
// arbitrary; the decisions are what matter.
class FakePane {
  scrollTop = 0;
  clientHeight = 400;
  scrollHeight = 4000;
  pinned = true;
  lastScrollHeight = 4000;
  pill = false;
  followWrite = -1;
  private pending = 0;

  get bottom(): number {
    return this.scrollHeight - this.clientHeight;
  }

  /** a stream delta lands: content grows, the tail follows only if pinned */
  append(growth: number): void {
    this.scrollHeight += growth;
    if (!this.pinned) return;
    this.scrollTop = this.bottom;
    this.followWrite = this.scrollTop;
    this.pending += 1;
  }

  /** content grows with no follow write flushed yet (the paint beat) */
  growOnly(growth: number): void {
    this.scrollHeight += growth;
  }

  /** the browser delivers the scroll events our writes raised */
  deliver(): void {
    for (let i = 0; i < this.pending; i++) {
      if (this.followWrite >= 0 && this.scrollTop === this.followWrite) {
        // our own write coming back - never a reader decision
        this.followWrite = -1;
        this.lastScrollHeight = this.scrollHeight;
        continue;
      }
      this.followWrite = -1;
      this.decide();
    }
    this.pending = 0;
  }

  private decide(): void {
    this.pinned = resumesFollow(
      this.pinned,
      this.lastScrollHeight,
      this.scrollTop,
      this.clientHeight,
      this.scrollHeight,
    );
    this.lastScrollHeight = this.scrollHeight;
    this.pill = pillVisible(this.scrollTop, this.clientHeight, this.scrollHeight);
  }

  /** a real reader scroll (the wheel), clamped like the browser */
  scroll(to: number): void {
    this.scrollTop = Math.max(0, Math.min(to, this.bottom));
    this.followWrite = -1;
    this.decide();
  }

  scrollUpBy(px: number): void {
    this.scroll(this.scrollTop - px);
  }
}

describe("stacked live pin: a fake stream cannot trap the scroll (t_5bb319f6)", () => {
  it("holds the tail while the reader stays at the bottom", () => {
    const pane = new FakePane();
    pane.append(300);
    pane.append(300);
    pane.append(300);
    assert.equal(pane.scrollTop, pane.bottom, "a pinned pane follows every delta");
    assert.equal(pane.pinned, true);
    assert.equal(pane.pill, false, "following the tail hides the pill");
  });

  it("scrolling up mid-stream moves the pane AND holds it there", () => {
    const pane = new FakePane();
    pane.append(600); // a long stream has painted above the fold
    const atTail = pane.scrollTop;
    pane.scrollUpBy(1200);
    // (a) the scroll moved - the history above the pin is reachable
    assert.equal(pane.scrollTop, atTail - 1200);
    assert.equal(pane.pinned, false, "scrolling up detaches the follow");
    assert.equal(pane.pill, true, "the way back to the tail is offered");
    const parked = pane.scrollTop;
    // (b) the next stream events must NOT snap the pane back
    for (let i = 0; i < 5; i++) pane.append(250);
    assert.equal(pane.scrollTop, parked, "a delta must not yank an unpinned pane");
    assert.equal(pane.pinned, false);
    assert.equal(pane.pill, true);
  });

  it("the top of the history is a position, not a clamp under the pin", () => {
    // the reported repro: scroll to scrollTop 0 and the pane is stuck
    // there until agent_end because nothing else exists above the pin
    const pane = new FakePane();
    pane.append(800);
    pane.scroll(0);
    assert.equal(pane.scrollTop, 0);
    for (let i = 0; i < 5; i++) pane.append(200);
    assert.equal(pane.scrollTop, 0, "the oldest row stays readable while the run streams");
    assert.equal(pane.pinned, false);
  });

  it("scrolling back to the tail re-arms the follow", () => {
    const pane = new FakePane();
    pane.append(600);
    pane.scrollUpBy(1000);
    assert.equal(pane.pinned, false);
    pane.scroll(pane.bottom); // the reader comes back on their own
    assert.equal(pane.pinned, true);
    assert.equal(pane.pill, false);
    pane.append(400);
    assert.equal(pane.scrollTop, pane.bottom, "the follow resumes from a real scroll");
  });

  it("a follow write's own scroll event cannot un-latch the tail", () => {
    // the live defect (t_5bb319f6): the follow writes scrollTop = bottom,
    // a delta grows the scroller before that write's scroll event lands,
    // and the event then measures the pane "above the tail" - the run
    // streams away from a reader who never touched the wheel
    const pane = new FakePane();
    pane.append(600); // a follow write is queued
    pane.growOnly(500); // the next delta beat, no write flushed yet
    const naive = resumesFollow(true, pane.lastScrollHeight, pane.scrollTop, pane.clientHeight, pane.scrollHeight);
    assert.equal(naive, false, "without the guard the plain check un-latches");
    pane.deliver();
    assert.equal(pane.pinned, true, "our own write coming back is not a scroll decision");
    pane.append(400);
    assert.equal(pane.scrollTop, pane.bottom, "the follow still owns the tail");
  });

  it("a fold that clamps an unpinned pane to the tail does not resume the follow", () => {
    // agent_end folds the run's thinking strip: the scroller shrinks and
    // the browser drags scrollTop to the new bottom - a layout event, so
    // the reader parked in the history must not be reprieved to live output
    const pane = new FakePane();
    pane.append(3600); // scrollHeight 7600, bottom 7200
    pane.scrollUpBy(2000); // parked at 5200
    const parked = pane.scrollTop;
    pane.scrollHeight = 5600; // the fold
    pane.scroll(parked + 1000); // the clamp delivers the reader to 5200 -> 5200 is bottom-... 
    assert.equal(pane.scrollTop, Math.min(parked + 1000, pane.bottom));
    assert.equal(pane.pinned, false, "a shrink arrival at the tail is not a scroll decision");
  });
});

describe("one scrollport, sticky pin: the shipped wiring", () => {
  const src = () => chatTab();

  it("the stacked column is the ONLY scroll container during a run", () => {
    const s = src();
    const listeners = s.match(/@scroll="onScroll"/g) ?? [];
    assert.equal(listeners.length, 1, "one scrollport: no per-branch scroll container");
    assert.ok(
      !/liveBodyEl/.test(s),
      "the run-only body (liveBodyEl) is retired - it held no history",
    );
    assert.ok(
      !/live-body/.test(s),
      "no live-body container: the run paints in the same column as the history",
    );
    assert.ok(
      !/groupedBody/.test(s),
      "the render list is the whole transcript, never a run-only slice",
    );
  });

  it("the pin is a sticky slot on the streaming turn's user row", () => {
    const s = src();
    assert.match(s, /v-else-if="pinnedSlot\(block, index\)"/, "the pin slot is declared per row");
    assert.match(
      s,
      /class="live-pin"/,
      "the pin keeps its own row class",
    );
    const fn = s.slice(s.indexOf("function pinnedSlot"), s.indexOf("}", s.indexOf("return block ===")) + 1);
    assert.match(fn, /index !== 0/, "only the block's HEAD row can pin (the user message)");
    assert.match(fn, /liveLayout\.value/, "the pin exists only while the turn streams");
    assert.match(
      fn,
      /groupedBlocks\.value\[groupedBlocks\.value\.length - 1\]/,
      "the pin is the LAST block - the live turn",
    );
  });

  it("the append follow is gated on the pinned flag and nothing else scrolls the pane", () => {
    const s = src();
    assert.match(
      s,
      /if\s*\(!force\s*&&\s*!pinned\.value\)\s*return/,
      "a delta must return early once the reader scrolled away",
    );
    const writes = s.match(/scrollTop\s*=\s*el\.scrollHeight/g) ?? [];
    assert.equal(writes.length, 1, "exactly one tail-follow write in the component");
    // the follow's own write must not be read back as a reader scroll
    assert.match(
      s,
      /followWrite = el\.scrollTop/,
      "scrollDown records where its write landed",
    );
    assert.match(
      s,
      /if \(followWrite >= 0 && el\.scrollTop === followWrite\)/,
      "onScroll recognises its own write before deciding the follow state",
    );
    assert.ok(
      !/!\s*live\s*&&\s*was/.test(s),
      "the run's end must not swap the column (a swap would discard the reader's place)",
    );
  });

  it("the pin's CSS sticks inside the scrollport, not to a fixed header band", () => {
    const c = css();
    const start = c.indexOf(".live-pin {");
    assert.ok(start !== -1, ".live-pin rule not found");
    const rule = c.slice(start, c.indexOf("}", start));
    assert.match(rule, /position:\s*sticky/, "sticky, so the pin releases at its flow position");
    assert.match(rule, /top:\s*0/, "stuck flush to the scrollport top while the run paints under it");
    assert.match(rule, /z-index:\s*\d+/, "the pin paints above the rows sliding under it");
    assert.match(rule, /background:/, "the pin must cover what scrolls behind it");
    assert.doesNotMatch(rule, /flex:\s*none/, "the pin is not a fixed-region band any more");
    assert.ok(
      !/\.messages\.live-body/.test(c),
      "the retired run-only body leaves no scroll geometry behind",
    );
    assert.match(
      c.slice(c.indexOf(".messages {"), c.indexOf("}", c.indexOf(".messages {"))),
      /overflow-y:\s*auto/,
      "the single column stays the scrollport",
    );
  });
});
