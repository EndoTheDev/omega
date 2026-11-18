// omega webui: the shared toggle's ONE-CLICK law.
// Endo's defect report: enable/disable toggles needed a DOUBLE click.
// The probe found no double-fire and no swallowed event - the pill
// emits exactly one click; the HOSTS ate it on purpose: every consumer
// carried the two-step arm/confirm (click one armed the row and showed
// a confirm note, click two committed). That shape is now dead, and
// this file pins the law so it cannot come back:
// one activation = one intent (the pill emits once, hosts write once)
// no armed/confirm prop exists on the pill (structural: a host
//     cannot rebuild the second click through the shared component)
// keyboard parity: the activation IS the native button click, so
//     Enter and Space fire the same single click
// the consequence text (what an exclusion means, a module's
//     dependents) rides the pill's title, never a gate
// node --test has no DOM (no jsdom/happy-dom/test-utils in the tree),
// so this is the composition-test convention: source pins over the
// SFCs, exactly like settingsnav.test.ts / registrySkills.test.ts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

const PILL = ["webui", "app", "components", "ui", "TogglePill.vue"] as const;

// every host that renders the shared pill
const HOSTS: { name: string; path: readonly string[]; apply: string }[] = [
  { name: "RegistryList", path: ["webui", "app", "components", "RegistryList.vue"], apply: 'emit("toggle"' },
  { name: "ModulesSection", path: ["webui", "app", "components", "settings", "ModulesSection.vue"], apply: "commitModuleToggle(" },
  { name: "CronJobs", path: ["harness", "cron", "ui", "CronJobs.vue"], apply: "apply(j" },
  { name: "McpServers", path: ["harness", "mcp", "ui", "McpServers.vue"], apply: "apply(s" },
];

// fnBody slices one `function <name>(.) {.
function fnBody(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `the host carries a ${name} handler`);
  const open = src.indexOf("{", start);
  let depth = 0;
  for (let i = open; i < src.length; i += 1) {
    if (src[i] === "{") depth += 1;
    else if (src[i] === "}") {
      depth -= 1;
      if (depth === 0) return src.slice(open, i + 1);
    }
  }
  throw new Error(`unterminated ${name} body`);
}

const count = (haystack: string, needle: string): number =>
  haystack.split(needle).length - 1;

// the pill: ONE activation, ONE intent -----------------------------

test("the pill is a native button that emits exactly one activation per click", () => {
  const src = read(...PILL);
  assert.match(src, /<button\s[^>]*type="button"/, "the switch is a native button (never a checkbox, never a div)");
  assert.match(src, /@click="emit\('click'\)"/, "the click is the single activation site");
  assert.equal(count(src, "emit('click')"), 1, "exactly one activation emit - never a double-fire");
  assert.ok(!src.includes("@dblclick"), "no dblclick affordance anywhere on the switch");
});

test("the pill carries no armed state and no confirm label (the two-click shape is structurally dead)", () => {
  const src = read(...PILL);
  assert.ok(!src.includes("confirmLabel"), "no confirm-label prop: a host cannot relabel the button into a question");
  assert.ok(!/armed\s*[?:]/.test(src), "no armed prop / armed-aria branch");
  assert.ok(!src.includes("armed:"), "no armed class binding");
  for (const prop of ["state: boolean", "label: string", "hint?: string", "disabled?: boolean"]) {
    assert.ok(src.includes(prop), `the pill declares ${prop}`);
  }
});

test("keyboard parity: activation IS the native click, so Enter and Space fire the same single switch", () => {
  const src = read(...PILL);
  // a native <button> is focusable and fires click on Enter/Space. Any keydown handler
  // could double-fire (key + click), so there must be none.
  assert.ok(!src.includes("@keydown") && !src.includes("@keyup"), "no key handler rides the switch");
  assert.ok(!src.includes('tabindex="-1"'), "the switch stays focusable (no negative tabindex)");
  assert.ok(!/disabled="[^"]*"\s*tabindex/.test(src), "disabled state never removes focusability");
});

test("the consequence text rides the accessible name and the title, it is never a gate", () => {
  const src = read(...PILL);
  // (0.17.1 release gate, finding A2): `:aria-label="label"` alone won the accessible
  // name while the consequence rode `title` only - hover-only, so a screen reader never
  // heard it. One composed name now feeds BOTH attributes (they cannot drift), and it
  // carries the label.
  assert.match(src, /:aria-label="accessibleName"/, "the aria is the composed name, not the bare label");
  assert.match(src, /:title="accessibleName"/, "the tooltip is the same composed name (hover text unchanged)");
  // the BUTTON tag is the wiring under pin.
  const tag = src.slice(src.indexOf("<button"), src.indexOf("</button"));
  assert.ok(!tag.includes(':aria-label="label"'), "the bare-label aria cannot come back");
  assert.match(src, /props\.hint \? `\$\{props\.label\} - \$\{props\.hint\}` : props\.label/, "the name folds the hint in when there is one");
  assert.ok(!src.includes('aria-label="armed'), "no armed aria branch");
  // the hint is still only a hint: it never gates the activation
  assert.equal(count(src, "emit('click')"), 1, "one activation emit, hint or no hint");
});

// the hosts: one activation, one write -----------------------------

for (const host of HOSTS) {
  test(`${host.name}: one activation is one write (no arm branch, no confirm gate)`, () => {
    const src = read(...host.path);
    const body = fnBody(src, "onToggle").replace(/\/\/[^\n]*/g, "");
    assert.ok(!body.includes("armed"), "the handler carries no arm branch");
    // exactly one application site.
    assert.equal(count(body, host.apply), 1, `exactly one write per activation (${host.apply})`);
    const usage = src.slice(src.indexOf("<TogglePill"), src.indexOf("/>", src.indexOf("<TogglePill")));
    assert.ok(!usage.includes(":armed"), "the host passes no armed prop");
    assert.ok(!usage.includes("confirm-label"), "the host passes no confirm label");
    assert.match(usage, /@click="onToggle\(/, "the pill's single click is the handler");
  });
}

test("the armed confirm notes are gone from every host (no row note waits on a second click)", () => {
  const hosts = HOSTS.map((h) => ({ ...h, src: read(...h.path) }));
  for (const host of hosts) {
    assert.ok(!host.src.includes("click again to confirm"), `${host.name}: no "click again" copy`);
    assert.ok(!host.src.includes('"confirm apply to'), `${host.name}: no armed confirm aria`);
    assert.ok(!host.src.includes("registry-confirm"), `${host.name}: no armed confirm note element`);
  }
});

test("the busy lock survives: a stacked click cannot overlap two writes", () => {
  for (const host of HOSTS) {
    const src = read(...host.path);
    const body = fnBody(src, "onToggle");
    assert.match(body, /busy(\.value)?\s*!==\s*null/, `${host.name}: the in-flight gate is still first in the handler`);
  }
});

test("the no-toggle row still refuses: its row emits nothing, one click or two", () => {
  // the default agent's switch is GONE rather than locked.
  const src = read(...["webui", "app", "components", "RegistryList.vue"]);
  const body = fnBody(src, "onToggle");
  assert.match(body, /if \(r\.noToggle\) return;/, "a no-toggle row never writes");
  assert.ok(src.includes('v-if="toggleable && !r.noToggle"'), "and it renders no pill at all");
  assert.ok(!src.includes("toggle-locked"), "the locked pill is dead, not parked");
});

test("the consequence text is still reachable: the two informed hosts pass it as a hint", () => {
  const registry = read(...["webui", "app", "components", "RegistryList.vue"]);
  assert.match(registry, /:hint="toggleHint\(r\)"/, "registry rows hint the exclusion consequence");
  assert.match(registry, /registryNote\(props\.kind/, "the hint is the pinned registryNote text");
  assert.ok(!registry.includes("^disable .*?"), "no call site strips a confirm prefix any more");
  const extensions = read(...["webui", "app", "components", "settings", "ModulesSection.vue"]);
  assert.match(extensions, /:hint="rowHint\(row\)"/, "module rows hint the dependents law");
  assert.match(extensions, /toggleNote\(asModuleRow\(row\)/, "the hint is the pinned toggleNote text");
  assert.ok(!extensions.includes("^disable .*?"), "no call site strips a confirm prefix any more");
});
