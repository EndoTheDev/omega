// omega webui: the THEMED SELECT (card t_acad02ec, audit M1). The native
// <select> popup is OS chrome over the themed panes; every band/drawer/
// row picker rides the shared BandSelect - the ModelSelect dropdown
// grammar (chip button + role="listbox" pop + pointerdown
// outside-close) generalized over short option ladders. The node suite
// has no DOM: this pins the grammar on the component's source and each
// call site's pattern presence - the listbox pattern as BEHAVIOR (open,
// options, pick-emit, close), not a grep for one attribute. The drawer
// surface token (audit M4) and the insights fixes (audit M7) ride here
// too: same card, same files.
// Run: node --test webui/test/bandselect.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const webui = join(here, ".."); // webui/
const repo = join(here, "..", ".."); // the repo root (module UIs)

const read = (...parts: string[]): string =>
  readFileSync(join(...parts), "utf8");

// strip HTML comments: the files narrate the native-select bug in their
// header comments - the pins must see the TEMPLATE, not the narrative
const stripComments = (src: string): string =>
  src.replace(/<!--[\s\S]*?-->/g, "");

const bandSelect = stripComments(read(webui, "app", "components", "ui", "BandSelect.vue"));

test("BandSelect is the listbox grammar, not a native select", () => {
  assert.match(bandSelect, /<button[^>]*class="model-chip"/, "a chip button opens the list");
  assert.match(bandSelect, /role="listbox"/, "the pop is an ARIA listbox");
  assert.match(bandSelect, /role="option"/, "each row is an ARIA option");
  assert.match(bandSelect, /:aria-selected=/, "options state the selection");
  assert.match(bandSelect, /emit\("update:modelValue"/, "a pick emits the model update");
  assert.match(bandSelect, /pointerdown/, "outside-pointerdown closes the pop (the native behavior)");
  assert.match(bandSelect, /generic="V extends string \| number"/, "generic over the value type");
  // the component itself renders no native select - that was the bug
  assert.doesNotMatch(bandSelect, /<select[\s>]/);
});

// one entry per converted site: the file, and what must carry the pattern
const SITES = [
  {
    file: join(webui, "app", "components", "settings", "SettingsTab.vue"),
    label: "settings knobs",
    select: /<BandSelect[\s\S]*?:options="knobOptions\(knob\)"/,
    labelProp: /:label="knob\.label"/,
  },
  {
    file: join(webui, "..", "harness", "store", "ui", "InsightsTab.vue"),
    label: "the insights window",
    select: /<BandSelect[\s\S]*?:options="WINDOW_OPTIONS"/,
    labelProp: /label="window in days, 0 = all time"/,
  },
  {
    file: join(repo, "harness", "kanban", "ui", "Board.vue"),
    label: "the board switcher",
    select: /<BandSelect[\s\S]*?:options="boardOptions"/,
    labelProp: /@update:model-value="switchBoard\(\$event\)"/,
  },
  {
    file: join(repo, "harness", "kanban", "ui", "board", "DetailDrawer.vue"),
    label: "the drawer status",
    select: /<BandSelect[\s\S]*?:options="statusOptions"/,
    labelProp: /@update:model-value="\$emit\('move', \$event\)"/,
  },
];

for (const s of SITES) {
  test(`${s.label} rides the themed listbox (the pattern, not the chrome)`, () => {
    const src = stripComments(read(s.file));
    assert.match(src, s.select);
    assert.match(src, s.labelProp);
    // no native select chrome left at this site
    assert.doesNotMatch(src, /<select[\s>]/);
  });
}

test("the drawer paints a real surface - no phantom --omega-bg token (audit M4)", () => {
  const drawer = SITES[3] ? read(SITES[3].file) : "";
  assert.match(drawer, /background:\s*var\(--z1\)/, "the drawer background is the layer-1 surface token");
  assert.doesNotMatch(drawer, /var\(--omega-bg/, "the phantom token is not referenced (browser-white draw)");
});

test("insights rides the token ladder (audit M7)", () => {
  const insights = read(webui, "..", "harness", "store", "ui", "InsightsTab.vue");
  assert.match(insights, /border-radius:\s*var\(--r-chip\)/, "the bar radius rides the 8/6/4 ladder, not 3px");
  assert.doesNotMatch(insights, /border-radius:\s*3px/);
  assert.doesNotMatch(insights, /border: 1px solid var\(--z1-raised\)/, "--z1-raised is a fill, not a border");
  assert.doesNotMatch(insights, /&#8635;/, "the recompute glyph is the Icon vocabulary, not an HTML entity");
  assert.match(insights, /<Icon[^>]*name="refresh"/);
});

test("board radii ride the ladder (the named band-work gaps)", () => {
  const board = read(repo, "harness", "kanban", "ui", "Board.vue");
  for (const [px, token] of [
    ["8px", "--r-pane"],
    ["6px", "--r-card"],
  ] as const) {
    const bare = board.match(new RegExp(`border-radius:\\s*${px}`, "g")) ?? [];
    assert.equal(bare.length, 0, `no bare ${px} radii left - they ride ${token}`);
  }
});
// --- the KEYBOARD LAW (card t_2daae1aa) ----------------------------------
//
// The rows carried role="option" and NOTHING else: a keyboard user could not
// reach a single option, and the whole popup was pointer-only. They are now a
// real listbox riding the SAME grammar the model picker rides (card
// t_a990f422): roving tabindex, arrows/Home/End on the app's one clampIndex
// wrap, Enter/Space picks, Escape closes and hands focus back to the chip.
// The node suite has no DOM, so the law is pinned on the SOURCE here; the
// behavior (focus really moves, the cursor really follows) is proven live
// over CDP on a lane built from this branch.
const modelSelect = stripComments(
  read(webui, "app", "components", "chat", "ModelSelect.vue"),
);

/** the `case "<key>":` labels a component's switch handles, sorted */
const keyLabels = (src: string): string[] =>
  [...src.matchAll(/case\s+("(?:[^"\\]|\\.)*")\s*:/g)].map((m) => m[1]!).sort();

/** every case body, keyed by its label - lets a pin name key AND action */
const caseBodies = (src: string): Map<string, string> => {
  const out = new Map<string, string>();
  // a case with an EMPTY body falls through to the next one (the Enter/Space
  // pair): it inherits that case's body, so both keys assert the same action
  let carried: string[] = [];
  for (const part of src.split(/\n\s*case /).slice(1)) {
    const m = part.match(/^("(?:[^"\\]|\\.)*")\s*:([\s\S]*)$/);
    if (!m) continue;
    if (m[2]!.trim() === "") {
      carried.push(m[1]!);
      continue;
    }
    for (const key of [...carried, m[1]!]) out.set(key, m[2]!);
    carried = [];
  }
  return out;
};

test("keyboard law: exactly one row is a tab stop, and it is the active one", () => {
  assert.match(
    bandSelect,
    /:tabindex="i === activeSeat \? 0 : -1"/,
    "roving tabindex: the ACTIVE row is the only tab stop",
  );
  assert.doesNotMatch(bandSelect, /tabindex="0"/, "no row is hardcoded tabbable");
  assert.match(
    bandSelect,
    /const activeSeat = computed\(\(\) => \{[\s\S]*?Math\.min\(Math\.max\(active\.value, 0\), len - 1\)/,
    "the seat is clamped - a shrinking ladder never leaves zero tab stops",
  );
  assert.match(bandSelect, /@focus="seatOn\(i\)"/, "landing on a row seats the cursor there");
  assert.match(bandSelect, /active: i === activeSeat/, "the highlighted row IS the tab stop");
  assert.match(
    bandSelect,
    /\.model-list li\.active\s*\{[\s\S]*?background: var\(--z1-raised\)/,
    "the keyboard cursor reads as the hover row",
  );
  assert.match(
    bandSelect,
    /\.model-list li:focus-visible\s*\{[\s\S]*?outline: 1px solid var\(--accent\)/,
    "the focus ring is visible on the token ladder",
  );
});

test("keyboard law: arrows/Home/End walk the rows through clampIndex", () => {
  assert.match(
    bandSelect,
    /import \{ clampIndex \} from "\.\.\/\.\.\/composables\/commandRegistry"/,
    "the app's ONE arithmetic is imported, never re-derived",
  );
  assert.match(
    bandSelect,
    /clampIndex\(activeSeat\.value, delta, props\.options\.length\)/,
    "the walk is clampIndex over the live option count",
  );
  assert.doesNotMatch(
    bandSelect,
    /%\s*(len|props\.options\.length)/,
    "no second modulo in this component",
  );
  const bodies = caseBodies(bandSelect.slice(bandSelect.indexOf("function onRowKey")));
  for (const [key, action] of [
    ["ArrowDown", "moveActive(1)"],
    ["ArrowUp", "moveActive(-1)"],
    ["Home", "focusRow(0)"],
    ["End", "focusRow(props.options.length - 1)"],
    ["Enter", "pick(value)"],
    [" ", "pick(value)"],
    ["Escape", "close()"],
    ["Tab", "close(false)"],
  ] as const) {
    const body = bodies.get(`"${key}"`);
    assert.ok(body, `the row handler binds ${key}`);
    assert.ok(body.includes(action), `${key} runs ${action}`);
  }
});

test("keyboard law: Escape and a pick hand focus back to the chip (one close path)", () => {
  assert.equal(
    (bandSelect.match(/open\.value = false/g) ?? []).length,
    1,
    "exactly one close path - no second place that hides the pop",
  );
  assert.equal(
    (bandSelect.match(/chipEl\.value\?\.focus\(\)/g) ?? []).length,
    1,
    "exactly one focus-return site",
  );
  assert.match(
    bandSelect,
    /const hadFocus = !!root\.value\?\.contains\(document\.activeElement\)/,
    "focus is only reclaimed when the keyboard was actually inside the pop",
  );
  assert.match(
    bandSelect,
    /if \(restoreFocus && hadFocus\) void nextTick\(\(\) => chipEl\.value\?\.focus\(\)\)/,
    "the trigger takes focus back after the pop unmounts",
  );
  const bodies = caseBodies(bandSelect.slice(bandSelect.indexOf("function onRowKey")));
  assert.ok(
    /e\.stopPropagation\(\);[\s\S]*?close\(\)/.test(bodies.get('"Escape"') ?? ""),
    "Escape stops at the pop - a host drawer binds Escape to its own close",
  );
  // Space is the shell's type-to-capture key (app.vue focuses the chat
  // composer on any PRINTABLE keydown): an activation must not escape the
  // pop, or the focus return above is dead on the chat view.
  for (const key of ['"Enter"', '" "']) {
    assert.ok(
      /e\.stopPropagation\(\);[\s\S]*?pick\(value\)/.test(bodies.get(key) ?? ""),
      `${key} stops the act keystroke - type-to-capture must not steal the focus return`,
    );
  }
  // the SAME obligation on the other member of the roving family (ModelSelect
  // is the template this grammar came from): its pick keys ride one line, so
  // pinning BOTH bodies here reds the moment either implementation drops the
  // stop. Without it a Space pick on a model row bubbles to app.vue and the
  // focus lands in .composer-input instead of the chip that opened the pop.
  const modelBodies = caseBodies(
    modelSelect.slice(modelSelect.indexOf("function onRowKey")),
  );
  assert.ok(
    /e\.stopPropagation\(\);[\s\S]*?close\(\)/.test(
      modelBodies.get('"Escape"') ?? "",
    ),
    "ModelSelect Escape stops at the pop too",
  );
  for (const key of ['"Enter"', '" "']) {
    assert.ok(
      /e\.stopPropagation\(\);[\s\S]*?pickRow\(section, model\)/.test(
        modelBodies.get(key) ?? "",
      ),
      `ModelSelect ${key} stops the act keystroke - type-to-capture must not steal the focus return`,
    );
  }
});

test("one law, two implementations: BandSelect and ModelSelect bind the same key set", () => {
  const law = ['" "', '"ArrowDown"', '"ArrowUp"', '"End"', '"Enter"', '"Escape"', '"Home"', '"Tab"'];
  assert.deepEqual(keyLabels(bandSelect), law, "the select's grammar is the named law");
  assert.deepEqual(
    keyLabels(modelSelect),
    law,
    "the model picker rides the same law - a key added to one reds here (extract the pattern when a third roving listbox lands)",
  );
});

test("the chip names its ONE listbox, and opening seats the current value", () => {
  assert.match(
    bandSelect,
    /:aria-controls="open \? listId : undefined"/,
    "the chip announces the pop it controls",
  );
  assert.match(
    bandSelect,
    /<div\s+v-if="open"\s+:id="listId"\s+class="model-pop"\s+role="listbox"/,
    "aria-controls targets the single listbox itself, never a multi-listbox container (the t_a990f422 Nit shape)",
  );
  const opened = bandSelect.slice(bandSelect.indexOf("function openNow"));
  assert.match(
    opened,
    /const seat = props\.options\.findIndex\(\(o\) => o\.value === props\.modelValue\)/,
    "the cursor opens where the knob already points, not on row one",
  );
  assert.match(
    opened,
    /if \(open\.value\) focusRow\(activeSeat\.value\)/,
    "the cursor lands once the rows exist (the pop mounts on the next tick)",
  );
});
