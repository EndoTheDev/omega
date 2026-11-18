// omega webui: the transcript column tracks the composer.
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

// the mobile block is the LAST @media.
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
    assert.ok(between >= within * 2, "the turn boundary must read as a boundary, not a nudge");
    assert.ok(between - within >= 4, "the turn boundary needs at least a 4px step over the within-turn gap");
  });

  it("flat 'chat' keeps its uniform chronology (this law is the stacked column's)", () => {
    assert.match(
      rule(".messages"),
      /gap:\s*12px/,
      "the flat appearance's between-item gap is untouched by the stacked geometry",
    );
  });
});

describe("the tuned chat spacing keeps the transcript a document (t_b25df7b0 + t_f146ad97)", () => {
  const pct = (body: string, prop: string) =>
    Number(body.match(new RegExp(`${prop}:\\s*(\\d+)%`))![1]);
  /** `0 0 0 4px` -> { top, right, bottom, left } */
  const pad = (body: string) => sides(body.match(/padding:\s*([^;]+);/)![1]);

  it("the stacked assistant answer runs the full width with a 4px left inset", () => {
    const bubble = rule(".messages.stacked .msg.assistant .bubble");
    assert.equal(
      pct(bubble, "max-width"),
      100,
      "the answer keeps the full column width - the tuned inset moved to the left edge",
    );
    const p = pad(bubble);
    assert.equal(parseFloat(p.left), 4, "the answer text starts 4px inside the turn's left edge");
    assert.equal(parseFloat(p.right), 0, "the right edge stays on the column - the answer is not re-capped");
  });

  it("every other stacked row stays full width (the 2026-10-20 law still stands)", () => {
    const user = rule(".messages.stacked .msg.user .bubble");
    assert.match(user, /width:\s*100%;/, "user rows are documents, not pills");
    assert.equal(pct(user, "max-width"), 100);
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
    assert.ok(tight >= 32, `the strip rows collapsed below HIS tuned strip height (${tight}px)`);
  });
});

// HIS tuned numbers: the six declarations Endo dialled in the chat-spacing tuner,
// pinned exactly. A taste number that only ever drifts silently is the bug this suite
// exists to catch - changing one of these is a new spec from him, so the change has to
// touch this test on purpose.
describe("Endo's tuned chat spacing - the six declarations (t_dd0bd19e round 3)", () => {
  it("the within-turn rhythm, strip height and chip insets are exactly as dialled", () => {
    assert.match(rule(".msg-block"), /gap:\s*4px/, "inside-turn gap is 4px - the strips keep one log with a breath");
    assert.match(
      css,
      /\.msg-block \.thinking summary,\s*\n\.msg-block \.toolcall summary,\s*\n\.msg-block \.toolgroup summary \{\s*min-height:\s*32px/,
      "every turn strip is a 32px row",
    );
    assert.match(rule(".thinking .thinking-variant"), /margin-inline:\s*4px 8px/, "thinking chip sides: left 4 / right 8");
    assert.match(rule(".thinking .thinking-variant"), /margin-block:\s*4px 8px/, "thinking chip keeps its block margins (top 4 / bottom 8)");
    assert.match(rule(".toolcall .tool-variant"), /margin-inline:\s*4px 8px/, "tool chip sides: left 4 / right 8");
    assert.match(rule(".toolcall .tool-variant"), /margin-block:\s*4px 4px/, "tool chip keeps its block margins (top/bottom 4)");
  });

  it("the answer width and the turn boundary are exactly as dialled", () => {
    const bubble = rule(".messages.stacked .msg.assistant .bubble");
    assert.match(bubble, /max-width:\s*100%/, "the answer is full width");
    assert.match(bubble, /padding:\s*0 0 0 4px/, "and its text insets 4px from the left");
    assert.equal(
      Number(rule(".messages.stacked").match(/gap:\s*(\d+)px/)![1]),
      8,
      "the stacked turn boundary is 8px",
    );
  });

  it("the narrow breakpoint carries the same tuned answer", () => {
    assert.match(
      mobileBlock(),
      /\.messages\.stacked \.msg\.assistant \.bubble \{ max-width: 100%; padding-left: 4px; \}/,
      "the mobile 88% family cap must not re-cap the stacked answer",
    );
  });
});
