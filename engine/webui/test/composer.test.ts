// omega webui: the composer's Enter law. Enter sends, Shift+Enter is the
// native newline (t_631b5362): the final keydown branch guards the send
// with the shiftKey modifier, so Shift+Enter falls through with default
// NOT prevented and the textarea breaks the line itself. The palette
// block stays untouched - while the palette is open, Enter (shift or not)
// is palette-owned: the highlighted item accepts, no send. Run:
// node --test engine/webui/test/ (node >= 23 runs TS natively)
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