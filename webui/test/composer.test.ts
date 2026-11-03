// omega webui: the composer's Enter law. Enter sends, Shift+Enter is the
// native newline (t_631b5362): the final keydown branch guards the send
// with the shiftKey modifier, so Shift+Enter falls through with default
// NOT prevented and the textarea breaks the line itself. The palette
// block stays untouched - while the palette is open, Enter (shift or not)
// is palette-owned: the highlighted item accepts, no send. Run:
// node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const composer = readFileSync(
  join(here, "../app/components/chat/AppComposer.vue"),
  "utf8",
);

// --- the onKeydown harness --------------------------------------------
// AppComposer is a .vue SFC: node cannot import it. The onKeydown source
// is sliced out verbatim (brace-matched) and evaluated with the exact
// bindings closure uses - a real behavior test of the real function text.

function extractOnKeydown(src: string): string {
  const at = src.indexOf("function onKeydown(e: KeyboardEvent)");
  assert.ok(at > -1, "AppComposer carries function onKeydown");
  let depth = 0;
  for (let i = src.indexOf("{", at); i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) return src.slice(at, i + 1);
    }
  }
  assert.fail("unbalanced braces around onKeydown");
}

const clampIndex = (i: number, d: number, len: number): number => {
  if (len === 0) return 0;
  const next = i + d;
  return next < 0 ? len - 1 : next >= len ? 0 : next;
};

type KE = {
  key: string;
  shiftKey: boolean;
  defaultPrevented: boolean;
  stopPropagationFired: boolean;
  preventDefault(): void;
  stopPropagation(): void;
};

function key(keyName: string, shiftKey = false): KE {
  const e: KE = {
    key: keyName,
    shiftKey,
    defaultPrevented: false,
    stopPropagationFired: false,
    preventDefault() {
      e.defaultPrevented = true;
    },
    stopPropagation() {
      e.stopPropagationFired = true;
    },
  };
  return e;
}

function build(opts: { open?: boolean; items?: string[] } = {}) {
  const sends: number[] = [];
  const accepted: Array<{ item: string; submit: boolean }> = [];
  const closed: number[] = [];
  const paletteOpen = { value: opts.open ?? false };
  const items = opts.items ?? [];
  const paletteItems = { value: items };
  const active = { value: 0 };
  const emit = (name: string, ...args: unknown[]) => {
    if (name === "send") sends.push(sends.length + 1);
  };
  const acceptActive = (submit: boolean) => {
    const item = paletteItems.value[active.value];
    accepted.push({ item, submit });
  };
  const closePalette = () => {
    closed.push(closed.length + 1);
  };
  const fnSrc = extractOnKeydown(composer).replace(
    /function onKeydown\(e: KeyboardEvent\)/,
    "function onKeydown(e)",
  );
  const factory = new Function(
    "deps",
    `const { paletteOpen, paletteItems, active, clampIndex, acceptActive, closePalette, emit } = deps;
     ${fnSrc}
     return onKeydown;`,
  );
  const onKeydown = factory({
    paletteOpen,
    paletteItems,
    active,
    clampIndex,
    acceptActive,
    closePalette,
    emit,
  }) as (e: KE) => void;
  return { onKeydown, sends, accepted, closed, active };
}

// --- the law -----------------------------------------------------------

describe("composer Enter law: Enter sends, Shift+Enter breaks the line", () => {
  it("plain Enter sends and claims the key (preventDefault)", () => {
    const { onKeydown, sends } = build();
    const e = key("Enter");
    onKeydown(e);
    assert.equal(sends.length, 1, "Enter emits send");
    assert.equal(e.defaultPrevented, true, "Enter is claimed");
  });

  it("Shift+Enter never sends - default NOT prevented, the native newline lands", () => {
    const { onKeydown, sends } = build();
    const e = key("Enter", true);
    onKeydown(e);
    assert.equal(sends.length, 0, "no send on Shift+Enter");
    assert.equal(
      e.defaultPrevented,
      false,
      "default not prevented: the textarea inserts the newline itself",
    );
  });

  it("the guard is the shiftKey modifier - any other modifier key still sends", () => {
    // Ctrl/Alt+Enter keep the existing shape: only shift is exempt
    const { onKeydown, sends } = build();
    const e = key("Enter", false);
    (e as unknown as { ctrlKey: boolean }).ctrlKey = true;
    onKeydown(e);
    assert.equal(sends.length, 1);
  });
});

describe("palette ownership is unchanged (t_631b5362: palette block untouched)", () => {
  const items = ["a", "b", "c"];

  it("palette open: plain Enter accepts the highlighted item, no send", () => {
    const { onKeydown, sends, accepted, closed } = build({
      open: true,
      items,
    });
    const e = key("Enter");
    onKeydown(e);
    assert.equal(sends.length, 0);
    assert.deepEqual(accepted, [{ item: "a", submit: true }]);
    assert.equal(closed.length, 0);
    assert.equal(e.defaultPrevented, true);
  });

  it("palette open: Shift+Enter is palette-owned too (no newline, no send)", () => {
    // documented choice (card: implementer picks): the palette block keeps
    // ownership of every Enter while open - Shift+Enter accepts the item
    const { onKeydown, sends, accepted } = build({ open: true, items });
    const e = key("Enter", true);
    onKeydown(e);
    assert.equal(sends.length, 0);
    assert.equal(accepted.length, 1);
    assert.equal(e.defaultPrevented, true);
  });

  it("Esc still closes and claims; arrows still walk the list", () => {
    const { onKeydown, closed, active } = build({ open: true, items });
    const down = key("ArrowDown");
    onKeydown(down);
    assert.equal(active.value, 1);
    const esc = key("Escape");
    onKeydown(esc);
    assert.equal(closed.length, 1);
    assert.equal(esc.defaultPrevented, true);
    assert.equal(esc.stopPropagationFired, true);
  });

  it("the palette block never sees a shiftKey condition - it stays as it was", () => {
    const block = composer.slice(
      composer.indexOf("if (paletteOpen.value)"),
      composer.indexOf('if (e.key === "Enter" && !e.shiftKey)'),
    );
    assert.ok(block.length > 0, "palette block precedes the guarded send");
    assert.doesNotMatch(block, /shiftKey/);
  });
});

describe("source canary: the guard exists, the unconditional branch does not", () => {
  it("the final branch guards with !e.shiftKey", () => {
    assert.match(composer, /if \(e\.key === "Enter" && !e\.shiftKey\) \{/);
  });
  it("no bare Enter branch remains outside the palette block", () => {
    const outside = composer.slice(
      composer.indexOf('if (e.key === "Enter" && !e.shiftKey)'),
    );
    assert.doesNotMatch(outside, /e\.key === "Enter"\)/);
  });
});
// --- the fullscreen column anatomy (card t_08ef793b) -------------------
// Endo's settled design (2026-11-02, "card it"): fullscreen = a floating
// CARD, not a window-sized focus ring - the text region is the tall
// element (a typeable canvas the placeholder + cursor live in), the
// control row (+ joins the chips, send anchors right) pins to the card's
// bottom edge with the compact padding rhythm, and the collapse chevron
// is a real button in its own gutter. Source canaries on AppComposer.vue
// + main.css; the compact skeleton is the regression guard.
const css = readFileSync(
  join(here, "../app/assets/css/main.css"),
  "utf8",
);

// a CSS block by selector: from the selector to its closing brace
// (composer rules are flat - no nesting - so this is exact)
function rule(sel: string): string {
  // anchor at a line start - a suffix selector must not grab an earlier
  // longer rule's tail (".composer.expanded .composer-row" contains
  // ".composer-row {")
  const anchor = css.indexOf("\n" + sel);
  assert.ok(anchor > -1, `main.css carries ${sel} at a rule start`);
  const end = css.indexOf("}", anchor);
  return css.slice(anchor, end + 1);
}

describe("fullscreen composer: column anatomy (card t_08ef793b)", () => {
  it("the expanded row is the tall element: it grows and stretches the textarea", () => {
    const row = rule(".composer.expanded .composer-row {");
    assert.match(row, /flex:\s*1/, "the row eats the card's free height");
    assert.match(
      row,
      /align-items:\s*stretch/,
      "the textarea fills the grown row's height (flex-start would leave dead space)",
    );
    const ta = rule(".composer.expanded textarea {");
    assert.match(ta, /flex:\s*1/);
    assert.match(ta, /height:\s*auto/);
    assert.match(ta, /min-height:\s*0/);
  });

  it("the control row pins to the bottom through the composer-bottom class", () => {
    // the row grows above the toolbar - the toolbar, last flex child of
    // the card column, rests on the card's bottom edge
    assert.match(
      css,
      /\.composer\.expanded \.composer-row \{[^}]*flex:\s*1/,
      "the grown row is a composer child above the toolbar",
    );
    const bottom = rule(".composer-bottom {");
    assert.match(
      bottom,
      /align-items:\s*center/,
      "one shared baseline: chips + send center on the row",
    );
    // the template binds the class to the expanded state only
    assert.match(
      composer,
      /'composer-bottom':\s*expanded/,
      "composer-bottom exists only in fullscreen",
    );
  });

  it("the + rides the bottom row; the placeholder waits in the canvas", () => {
    assert.match(
      composer,
      /v-if="expanded"\s*\n\s*class="plus-btn"/,
      "fullscreen: the + joins the chips' row",
    );
    assert.match(
      composer,
      /placeholder="message omega"/,
      "the placeholder renders in the grown canvas",
    );
  });

  it("a click on empty canvas space lands the caret in the input", () => {
    assert.match(composer, /function onCanvasClick/, "the canvas click law");
    assert.match(
      composer,
      /@click="onCanvasClick"/,
      "wired on the composer root",
    );
    // the guard: control clicks pass, empty-space clicks focus
    const fn = composer.slice(
      composer.indexOf("function onCanvasClick"),
      composer.indexOf("// draft clears after send"),
    );
    assert.match(fn, /expanded\.value/, "compact stays untouched (no hijack)");
    assert.match(fn, /closest\(/, "controls opt out");
    assert.match(fn, /ta\.value\?\.focus\(\)/, "the canvas focuses the input");
  });

  it("entering fullscreen parks the cursor in the canvas", () => {
    const toggle = composer.slice(
      composer.indexOf("function toggleExpand"),
      composer.indexOf("// fullscreen canvas law"),
    );
    assert.match(toggle, /if \(expanded\.value\) ta\.value\?\.focus\(\)/);
    // the height math must read the POST-class layout: a sync resize on
    // collapse measures the still-stretched textarea and pins the compact
    // height to the old 84px cap (live-probed collapse round-trip)
    assert.match(toggle, /nextTick\(\(\) => \{/, "resize runs after the class lands");
  });

  it("the chevron is a real button: >=24px target, inset from the corner", () => {
    const btn = rule(".expand-btn {");
    assert.match(btn, /min-width:\s*24px/);
    assert.match(btn, /min-height:\s*24px/);
    assert.match(btn, /top:\s*6px/);
    assert.match(btn, /right:\s*8px/, "inset, not edge-to-edge");
  });

  it("the textarea's scrollbar clears the chevron's gutter in fullscreen", () => {
    const ta = rule(".composer.expanded textarea {");
    assert.match(ta, /margin-right:\s*40px/, "a dedicated right-edge gutter");
  });

  it("the fullscreen focus ring dies - the card keeps its own stroke", () => {
    const fullscreen = rule(".composer.expanded:focus-within {");
    assert.match(
      fullscreen,
      /border-color:\s*var\(--stroke\)/,
      "no orange on the pane-sized card",
    );
    // the compact ruler is untouched
    const compact = rule(".composer:focus-within {");
    assert.match(compact, /rgba\(232,\s*131,\s*58,\s*0\.55\)/);
  });

  it("card geometry: inset margin on all four sides, radius kept", () => {
    const card = rule(".composer.expanded {");
    assert.match(card, /margin:\s*12px/, "floating card, not a pane overlay");
    assert.match(card, /flex-grow/);
    const base = rule(".composer {");
    assert.match(base, /border-radius:\s*var\(--r-card\)/, "radius unchanged");
    assert.match(base, /border:\s*1px solid var\(--stroke\)/, "stroke unchanged");
  });

  it("the send keeps its accent fill at normal contrast in the bottom row", () => {
    assert.match(
      css,
      /\.composer-bottom \.send:disabled \{ opacity: 1/,
      "ruling 4: no wash in fullscreen (base .send:disabled stays the compact ruler)",
    );
  });

  it("the collapse affordance never disappears in fullscreen", () => {
    assert.match(
      composer,
      /v-if="overflowing \|\| expanded"/,
      "a shrinking draft never strands the user in fullscreen",
    );
  });
});

describe("compact composer regression guard (card t_08ef793b: untouched)", () => {
  it("the row keeps its compact skeleton", () => {
    const row = rule(".composer-row {");
    assert.match(row, /align-items:\s*flex-start/);
    assert.doesNotMatch(row, /flex:\s*1/);
  });
  it("the toolbar keeps its compact padding rhythm", () => {
    const toolbar = rule(".toolbar {");
    assert.match(toolbar, /padding:\s*6px 8px/);
    assert.doesNotMatch(toolbar, /composer-bottom/);
  });
  it("the textarea keeps the deterministic 21px row math", () => {
    const ta = rule(".composer textarea {");
    assert.match(ta, /line-height:\s*21px/);
    assert.doesNotMatch(ta, /margin-right/);
  });
  it("the base textarea block carries no fullscreen-only declarations", () => {
    // the gutter and the stretch live under .composer.expanded only
    assert.doesNotMatch(css, /\.composer textarea \{[^}]*margin-right/);
  });
});
