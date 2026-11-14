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
