// omega webui: the transcript column tracks the composer (card t_5063675a,
// Endo's in-app comment on chat a330a233: "adjust the width of the user
// message bubble to follow the width of the chat composer - do the same
// with all the other blocks ... also adjust the height of those blocks so
// that the gaps between them are smaller").
//
// ONE column edge: the transcript's side inset IS the composer's side
// margin, at both breakpoints. Every block (user bubble, thinking, tool
// rows) is 100% of that column already, so the pair of numbers is the whole
// geometry - drifting one re-opens the 8px mismatch this card closed.
//
// Inside a turn the meta strips (thinking / tool runs) sit TIGHT, and the
// t_84e4653c grouping law still holds: the between-turn gap stays the
// dominant one. Flat 'chat' (the chronological appearance) keeps its
// uniform 12px rhythm - this law is the STACKED column's.
//
// Render-free CSS pin: reads main.css and asserts the numbers. Run:
// node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, "..", "app/assets/css/main.css"), "utf8");

/** the body of the first BASE rule whose selector is exactly `selector` */
const rule = (selector: string): string => {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = css.match(new RegExp(`^${escaped}\\s*\\{([^}]*)\\}`, "m"));
  assert.ok(m, `${selector} rule is missing from main.css`);
  return m![1];
};

// the mobile block is the LAST @media (max-width: 768px) of the sheet - the
// responsive overrides live there, the earlier one is the bar/drawer set
const mobileBlock = (): string => {
  const at = css.lastIndexOf("@media (max-width: 768px)");
  assert.ok(at > 0, "the mobile media block is missing from main.css");
  return css.slice(at);
};

/** `16px 12px` -> { top, right, bottom, left } */
const sides = (value: string) => {
  const parts = value.trim().split(/\s+/);
  const [top, right = top, bottom = top, left = right] = parts;
  return { top, right, bottom, left };
};

describe("the transcript column tracks the composer (t_5063675a)", () => {
  it("the desktop column's side inset IS the composer's side margin", () => {
    const msgs = sides(
      rule(".messages").match(/padding:\s*([^;]+);/)![1],
    );
    const composer = sides(
      rule(".composer").match(/margin:\s*([^;]+);/)![1],
    );
    assert.equal(msgs.left, composer.left, "transcript left edge sits on the composer's");
    assert.equal(msgs.right, composer.right, "transcript right edge sits on the composer's");
  });

  it("the mobile column tracks the mobile composer too", () => {
    const mobile = mobileBlock();
    const msgs = mobile.match(/\.messages\s*\{\s*padding:\s*([^;}]+)[;}]/)?.[1];
    const composer = mobile.match(/\.composer\s*\{\s*margin:\s*([^;}]+)[;}]/)?.[1];
    assert.ok(msgs, "the mobile .messages padding is missing");
    assert.ok(composer, "the mobile .composer margin is missing");
    const m = sides(msgs!);
    const c = sides(composer!);
    assert.equal(m.left, c.left);
    assert.equal(m.right, c.right);
  });

  it("the meta strips sit tight inside a turn", () => {
    // the thinking / tool strips are the rows whose gaps Endo asked to shrink
    // - the number is taste, the ceiling is not: an airy inside-turn gap is
    // the bug this card fixes
    const gap = rule(".msg-block").match(/gap:\s*(\d+)px/)![1];
    assert.ok(Number(gap) <= 8, `inside-turn gap is ${gap}px - the meta strips are not tight`);
  });

  it("the turn boundary stays the dominant gap (t_84e4653c grouping law)", () => {
    const within = Number(rule(".msg-block").match(/gap:\s*(\d+)px/)![1]);
    const between = Number(rule(".messages.stacked").match(/gap:\s*(\d+)px/)![1]);
    assert.ok(
      within < between,
      `inside-turn ${within}px must stay below the ${between}px turn boundary or the blocks stop grouping`,
    );
    // and the contrast has to be visible, not a hairline apart
    assert.ok(between >= within * 3, "the turn boundary must read as a boundary, not a nudge");
  });

  it("flat 'chat' keeps its uniform chronology (this law is the stacked column's)", () => {
    assert.match(
      rule(".messages"),
      /gap:\s*12px/,
      "the flat appearance's between-item gap is untouched by the stacked geometry",
    );
  });
});

// The 2026-11-15 compactness sweep (Endo, in-app on chat efd2b0f1, 946px
// lane: "the assistance response is a little too wide ... just a little.
// make it less wide ... and the gaps between thinking and tool calls
// blocks could also be smaller/compacter"). Round 1 shipped the 92% inset;
// round 2 (t_f146ad97) is HIS OWN tuned numbers out of the chat-spacing
// tuner, pasted as a spec - the numbers are law, the tests follow them.
//
// Two things carry the sweep: the strip rows tighten inside the turn block
// (while the panels that reuse those rows keep their click floor) and the
// between-turn gap widens so each dense turn reads as one block.
describe("the tuned chat spacing keeps the transcript a document (t_b25df7b0 + t_f146ad97)", () => {
  const pct = (body: string, prop: string) =>
    Number(body.match(new RegExp(`${prop}:\\s*(\\d+)%`))![1]);
  /** `0 0 0 18px` -> { top, right, bottom, left } */
  const pad = (body: string) => sides(body.match(/padding:\s*([^;]+);/)![1]);

  it("the stacked assistant answer runs the full width with an 18px left inset", () => {
    const bubble = rule(".messages.stacked .msg.assistant .bubble");
    assert.equal(
      pct(bubble, "max-width"),
      100,
      "the answer keeps the full column width - the tuned inset moved to the left edge",
    );
    const p = pad(bubble);
    assert.equal(parseFloat(p.left), 18, "the answer text starts 18px inside the turn's left edge");
    assert.equal(parseFloat(p.right), 0, "the right edge stays on the column - the answer is not re-capped");
  });

  it("every other stacked row stays full width (the 2026-10-20 law still stands)", () => {
    const user = rule(".messages.stacked .msg.user .bubble");
    assert.match(user, /width:\s*100%;/, "user rows are documents, not pills");
    assert.equal(pct(user, "max-width"), 100);
    // these are single-line rules; the answer's inset is the only exception
    for (const line of [
      ".messages.stacked .toolcall { max-width: 100%; }",
      ".messages.stacked .thinking { max-width: 100%; }",
      ".messages.stacked .toolgroup { max-width: 100%; }",
    ]) {
      assert.ok(css.includes(line), `${line} is missing - every non-answer stacked row stays full width`);
    }
  });

  it("the transcript strip rows tighten, the reused rows keep their click floor", () => {
    const base = Number(rule(".thinking summary").match(/min-height:\s*(\d+)px/)![1]);
    const scoped = css.match(
      /\.msg-block \.thinking summary,\s*\n\.msg-block \.toolcall summary,\s*\n\.msg-block \.toolgroup summary \{\s*min-height:\s*(\d+)px/,
    );
    assert.ok(scoped, "the turn block's strip-row min-height rule is missing from main.css");
    const tight = Number(scoped![1]);
    assert.ok(tight < base, `the strip rows did not tighten (${tight}px vs the ${base}px base)`);
    assert.ok(tight >= 32, `the strip rows collapsed below a comfortable click row (${tight}px)`);
  });
});

// HIS tuned numbers (t_f146ad97): the six declarations Endo dialled in the
// chat-spacing tuner, pinned exactly. A taste number that only ever drifts
// silently is the bug this suite exists to catch - changing one of these is
// a new spec from him, so the change has to touch this test on purpose.
describe("Endo's tuned chat spacing - the six declarations (t_f146ad97)", () => {
  it("the within-turn rhythm, strip height and chip insets are exactly as dialled", () => {
    assert.match(rule(".msg-block"), /gap:\s*4px/, "inside-turn gap is 4px");
    assert.match(
      css,
      /\.msg-block \.thinking summary,\s*\n\.msg-block \.toolcall summary,\s*\n\.msg-block \.toolgroup summary \{\s*min-height:\s*32px/,
      "every turn strip is a 32px row",
    );
    assert.match(rule(".thinking .thinking-variant"), /margin:\s*4px 6px 8px 4px/, "thinking chip: left 4 / right 6 / top 4 / bottom 8");
    assert.match(rule(".toolcall .tool-variant"), /margin:\s*4px 6px 4px 4px/, "tool chip: left 4 / right 6 / top 4 / bottom 4");
  });

  it("the answer width and the turn boundary are exactly as dialled", () => {
    const bubble = rule(".messages.stacked .msg.assistant .bubble");
    assert.match(bubble, /max-width:\s*100%/, "the answer is full width");
    assert.match(bubble, /padding:\s*0 0 0 18px/, "and its text insets 18px from the left");
    assert.equal(
      Number(rule(".messages.stacked").match(/gap:\s*(\d+)px/)![1]),
      48,
      "the stacked turn boundary is 48px",
    );
  });

  it("the narrow breakpoint carries the same tuned answer", () => {
    assert.match(
      mobileBlock(),
      /\.messages\.stacked \.msg\.assistant \.bubble \{ max-width: 100%; padding-left: 18px; \}/,
      "the mobile 88% family cap must not re-cap the stacked answer",
    );
  });
});
