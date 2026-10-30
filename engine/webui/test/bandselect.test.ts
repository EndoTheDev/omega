// omega webui: the THEMED SELECT (card t_acad02ec, audit M1). The native
// <select> popup is OS chrome over the themed panes; every band/drawer/
// row picker rides the shared BandSelect - the ModelSelect/ThinkingSelect
// dropdown grammar (chip button + role="listbox" pop + pointerdown
// outside-close) generalized over short option ladders. The node suite
// has no DOM: this pins the grammar on the component's source and each
// call site's pattern presence - the listbox pattern as BEHAVIOR (open,
// options, pick-emit, close), not a grep for one attribute. The drawer
// surface token (audit M4) and the insights fixes (audit M7) ride here
// too: same card, same files.
// Run: node --test engine/webui/test/bandselect.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const engine = join(here, "..", ".."); // engine/
const repo = join(engine, ".."); // the repo root (module UIs)

const read = (...parts: string[]): string =>
  readFileSync(join(...parts), "utf8");

// strip HTML comments: the files narrate the native-select bug in their
// header comments - the pins must see the TEMPLATE, not the narrative
const stripComments = (src: string): string =>
  src.replace(/<!--[\s\S]*?-->/g, "");

const bandSelect = stripComments(read(engine, "webui", "app", "components", "ui", "BandSelect.vue"));

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
    file: join(engine, "webui", "app", "components", "settings", "SettingsTab.vue"),
    label: "settings knobs",
    select: /<BandSelect[\s\S]*?:options="knobOptions\(knob\)"/,
    labelProp: /:label="knob\.label"/,
  },
  {
    file: join(engine, "webui", "app", "components", "insights", "InsightsTab.vue"),
    label: "the insights window",
    select: /<BandSelect[\s\S]*?:options="WINDOW_OPTIONS"/,
    labelProp: /label="window in days, 0 = all time"/,
  },
  {
    file: join(repo, "modules", "kanban", "ui", "Board.vue"),
    label: "the board switcher",
    select: /<BandSelect[\s\S]*?:options="boardOptions"/,
    labelProp: /@update:model-value="switchBoard\(\$event\)"/,
  },
  {
    file: join(repo, "modules", "kanban", "ui", "board", "DetailDrawer.vue"),
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
  const insights = read(engine, "webui", "app", "components", "insights", "InsightsTab.vue");
  assert.match(insights, /border-radius:\s*var\(--r-chip\)/, "the bar radius rides the 8/6/4 ladder, not 3px");
  assert.doesNotMatch(insights, /border-radius:\s*3px/);
  assert.doesNotMatch(insights, /border: 1px solid var\(--z1-raised\)/, "--z1-raised is a fill, not a border");
  assert.doesNotMatch(insights, /&#8635;/, "the recompute glyph is the Icon vocabulary, not an HTML entity");
  assert.match(insights, /<Icon[^>]*name="refresh"/);
});

test("board radii ride the ladder (the named band-work gaps)", () => {
  const board = read(repo, "modules", "kanban", "ui", "Board.vue");
  for (const [px, token] of [
    ["8px", "--r-pane"],
    ["6px", "--r-card"],
  ] as const) {
    const bare = board.match(new RegExp(`border-radius:\\s*${px}`, "g")) ?? [];
    assert.equal(bare.length, 0, `no bare ${px} radii left - they ride ${token}`);
  }
});