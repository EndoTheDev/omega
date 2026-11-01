// omega webui: the Terminal tab (card t_55a3fc4b). Registry membership
// + the source-wiring greps (the composition-test convention): the tab
// is the terminal module's face - claim/stream/input/resize/kill ride
// the module's own routes, the pane never gates (the owner's ruling:
// no restrictions), and the outlet unmount drops the SSE, never the
// shell.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  DOCK_COMPONENTS,
  DOCKABLES,
  MENU_DOCKABLES,
  defaultHostTabs,
  dockableOf,
  isClosableTab,
  isLockedTab,
} from "../app/composables/dockables.ts";
import { ICONS } from "../app/composables/icons.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
const read = (...p: string[]) => readFileSync(join(...p), "utf8");

// --- the registry entry (the dock contract) -----------------------------

test("terminal is a registered component: singleton, bottom home, unclosable", () => {
  assert.ok(
    (DOCK_COMPONENTS as readonly string[]).includes("terminal"),
    "the union carries it",
  );
  const d = dockableOf("terminal");
  assert.equal(d.title, "Terminal");
  assert.equal(d.icon, "terminal");
  assert.equal(d.policy, "singleton", "one terminal per pane - the pane owns its shell");
  assert.equal(d.area, "bottom", "the bottom panel is the home the registry reserved");
  assert.equal(d.defaultHome ?? d.area, "bottom", "a fresh bottom host seats it");
  assert.equal(d.closable, false, "unclosable like every pinned view");
  assert.ok(ICONS[d.icon], "the icon is an iconset name (the membership law)");
});

test("terminal moves freely: it is offered in the '+' menus and not locked", () => {
  assert.ok(MENU_DOCKABLES.includes("terminal"), "the '+' menu offers the tab");
  assert.equal(isLockedTab("terminal"), false, "pinned LOCATIONS, free CONTENT");
  assert.equal(isClosableTab("terminal"), false, "no hover-X on a pinned tab");
});

test("a fresh bottom host seats terminal (the t_67fc1905 reservation)", () => {
  const tabs = defaultHostTabs("bottom");
  assert.ok(
    tabs.includes("terminal"),
    `the bottom host's fresh strip should carry terminal, got ${tabs.join(", ")}`,
  );
});

test("the registry's every entry keeps its floor law (terminal = tool family)", () => {
  const d = DOCKABLES.find((x) => x.id === "terminal");
  assert.ok(d, "registry entry present");
  assert.equal(d.minPx, 128, "the tool family floor");
});

// --- the source wiring (the composition-test convention) ----------------

const appVue = read(root, "engine/webui/app/app.vue");
const tabVue = read(
  root,
  "engine/webui/app/components/terminal/TerminalTab.vue",
);

test("app.vue hosts the terminal outlet - any pane slot by union membership", () => {
  assert.match(appVue, /#pane-terminal/, "the dock slot template exists");
  assert.match(appVue, /<[Tt]erminalTab/, "the outlet renders the tab");
});

test("the pane claims (reattach-else-spawn) and never gates", () => {
  assert.match(
    tabVue,
    /POST["'` ]+["'`]?\(?\/terminal\/sessions/,
    "the claim rides POST /terminal/sessions",
  );
  assert.doesNotMatch(
    tabVue,
    /confirm|disabled[^-]|opt-in/i,
    "NO confirm-gate, NO opt-in - the owner's ruling",
  );
});

test("the SSE pipe is the module's stream route, replay-first", () => {
  assert.match(tabVue, /\/terminal\/\$\{[^}]+\}\/stream/, "the stream route");
  assert.match(tabVue, /EventSource/, "SSE consumption");
  assert.match(tabVue, /"o"|"m"|"x"/, "the three frame kinds are handled");
});

test("keystrokes POST input, size POSTs resize, no raw bytes on the wire", () => {
  assert.match(tabVue, /\/terminal\/\$\{[^}]+\}\/input/, "the input route");
  assert.match(tabVue, /application\/octet-stream/, "raw octet frames, not JSON-quoted");
  assert.match(tabVue, /\/terminal\/\$\{[^}]+\}\/resize/, "the resize route");
  assert.match(tabVue, /t\.onData/, "xterm keystrokes feed the input");
});

test("the kill affordance is the module's DELETE route", () => {
  assert.match(tabVue, /method: "DELETE"/, "kill = DELETE /terminal/{id}");
  assert.match(tabVue, /kill terminal/, "the button exists by aria-label");
});

test("the outlet unmount drops the SSE, never the shell", () => {
  const unmount = tabVue.slice(tabVue.indexOf("onBeforeUnmount"));
  assert.match(unmount, /closeStream\(\)/, "the SSE closes on unmount");
  assert.doesNotMatch(
    unmount.slice(0, 200),
    /\/terminal\/[^"]*",\s*\{\s*method: "DELETE"/,
    "no DELETE rides the unmount - the pty idles for the reattach",
  );
  assert.match(
    tabVue,
    /onBeforeUnmount[\s\S]*closeStream[\s\S]*\}/,
    "stream teardown happens in the unmount hook",
  );
});