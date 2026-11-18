// omega webui: THE NEW-CONVERSATION DIM. While the focused tab IS the draft
// conversation, every "new conversation" door would only focus the tab already on
// screen: the sidebar's '+' (title "New conversation"), its menu's "New Session" row,
// the palette's "New chat" command and a main pane's '+' each dim and disable -
// aria-disabled + tabindex -1, dimmed but NEVER removed, the same law the last-tab
// close X obeys.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  DOCK_DRAFT_FOCUS_KEY,
  isDraftChatKey,
  makeSplit,
  type DockNode,
  type DockPane,
} from "../app/composables/dock.ts";
import { buildPaletteCommands } from "../app/composables/commandRegistry.ts";

const HERE = dirname(fileURLToPath(import.meta.url));
const APP = resolve(HERE, "..", "app");
const REPO = resolve(HERE, "..", "..");
const read = (rel: string) => readFileSync(join(APP, rel), "utf8");

// -- the pure predicate ------------------------------------------------

/** a tree with two chat tabs in one pane: index 0 the draft, 1 bound */
function tree(): DockNode {
  const pane: DockPane = {
    kind: "pane",
    id: "p1",
    tabs: [
      { component: "chat", key: "k-draft" },
      { component: "chat", key: "k-bound", sessionRef: "s-1" },
    ],
    active: 0,
    fixed: null,
  };
  return makeSplit("row", [pane]);
}

test("the focused DRAFT tab is the dim condition", () => {
  assert.equal(isDraftChatKey(tree(), "k-draft"), true);
});

test("a bound chat tab never dims a new-conversation door", () => {
  assert.equal(isDraftChatKey(tree(), "k-bound"), false);
});

test("no focus (null) and an unknown key never dim", () => {
  assert.equal(isDraftChatKey(tree(), null), false);
  assert.equal(isDraftChatKey(tree(), "k-gone"), false);
});

test("a non-chat tab under the focus never dims", () => {
  const pane: DockPane = {
    kind: "pane",
    id: "p1",
    tabs: [{ component: "settings", key: "k-set" }],
    active: 0,
    fixed: null,
  };
  assert.equal(isDraftChatKey(makeSplit("row", [pane]), "k-set"), false);
});

// -- the palette's command shape --------------------------------------

function commands(draftFocused?: boolean) {
  const seen: string[] = [];
  const cmds = buildPaletteCommands({
    openDockable: (id) => seen.push("open:" + id),
    openChat: () => seen.push("chat"),
    closeCurrentTab: () => seen.push("close"),
    toggleBottomPanel: () => seen.push("bottom"),
    toggleSidebar: (side) => seen.push("sidebar:" + side),
    openSessionsSidebar: () => seen.push("sessionsbar"),
    openRecentSession: () => seen.push("recent"),
    reloadLayout: () => seen.push("reload"),
    clearSavedLayout: () => seen.push("clear"),
    draftFocused,
  });
  return { cmds, seen };
}

test("New chat dims under the draft focus and stays in the list", () => {
  const dim = commands(true).cmds.find((c) => c.id === "open:chat");
  assert.ok(dim, "the command is never removed by the dim");
  assert.equal(dim.title, "New chat");
  assert.equal(dim.dimmed, true);

  const live = commands(false).cmds.find((c) => c.id === "open:chat");
  assert.ok(live);
  assert.equal(live.dimmed, false);
});

test("only the new-chat command carries the dim", () => {
  const { cmds } = commands(true);
  const dimmed = cmds.filter((c) => c.dimmed);
  assert.deepEqual(dimmed.map((c) => c.id), ["open:chat"]);
});

test("running the dimmed command still focuses the draft (honest no-op)", async () => {
  const { cmds, seen } = commands(true);
  const cmd = cmds.find((c) => c.id === "open:chat")!;
  await cmd.run();
  assert.deepEqual(seen, ["chat"], "the action is untouched - the row only says what it will do");
});

// -- the doors' source pins -------------------------------------------

test("app.vue owns the fact once and provides it", () => {
  const app = read("app.vue");
  assert.match(app, /const draftConversationFocused = computed\(\(\) =>\s*isDraftChatKey\(dock\.tree\.value, focusedKey\.value\)/, "the predicted condition is the pure predicate");
  assert.match(app, /provide\(DOCK_DRAFT_FOCUS_KEY, draftConversationFocused\)/, "ONE provider");
  assert.equal(
    (app.match(/provide\(DOCK_DRAFT_FOCUS_KEY/g) ?? []).length,
    1,
    "never a second provider - two would let doors disagree",
  );
  // the module-UI door takes a prop (module UI imports no shell code)
  assert.equal(
    (app.match(/:draft-focused="draftConversationFocused"/g) ?? []).length,
    2,
    "both AppSidebar mounts (desktop + mobile drawer) carry the fact",
  );
  assert.match(app, /draftFocused: draftConversationFocused\.value/, "the palette build gets the same fact");
});

test("the sessions sidebar's '+' and its New Session row both dim", () => {
  const bar = readFileSync(join(REPO, "harness/store/ui/AppSidebar.vue"), "utf8");
  assert.match(bar, /draftFocused\?: boolean/, "the prop the shell passes");
  // the '+': dimmed + disabled + out of tab order, never removed
  assert.match(bar, /class="new-main"[\s\S]{0,220}'door-dimmed': draftFocused/, "the '+ New Session' door dims");
  assert.match(bar, /:aria-disabled="draftFocused \|\| undefined"/);
  assert.match(bar, /:tabindex="draftFocused \? -1 : undefined"/);
  assert.ok(bar.includes("+ New Session"), "the door is still there - dim is never removal");
  assert.match(bar, /dimmed: props\.draftFocused/, "the menu entry follows the same live fact");
  assert.match(bar, /'door-dimmed': entry\.dimmed/);
  // the dim is NOT the native disabled attribute (that kills the title)
  assert.doesNotMatch(
    bar,
    /class="new-main"[\s\S]{0,200}?disabled="draftFocused"/,
    "aria-disabled, not the native attribute - the title must stay readable",
  );
});

test("a main pane's '+' dims on the injected fact", () => {
  const pane = read("components/DockPane.vue");
  assert.match(pane, /inject\(DOCK_DRAFT_FOCUS_KEY, null\)/, "the shell's one provider");
  assert.match(pane, /const draftFocused = computed\(\(\) => draftFocus\?\.value === true\)/);
  assert.match(pane, /:aria-disabled="draftFocused \|\| undefined"/);
  assert.match(pane, /:tabindex="draftFocused \? -1 : undefined"/);
  assert.match(pane, /'door-dimmed': draftFocused/);
  assert.match(pane, /class="dock-tab-new"/, "the '+' keeps rendering");
});

test("the palette row carries the dim as an accessible state", () => {
  const pal = read("components/nav/CommandPalette.vue");
  assert.match(pal, /dimmed: c\.dimmed/);
  assert.match(pal, /:aria-disabled="c\.dimmed \|\| undefined"/);
});

test("the dim value is ONE global class (card law: no second copy)", () => {
  const css = read("assets/css/main.css");
  assert.match(css, /\.door-dimmed \{\n  opacity: 0\.4;\n  cursor: default;\n\}/, "the one dim value");
  // the last-tab close X keeps its own (hover-strip) treatment.
  assert.match(css, /\.dock-tab-close\.locked,[\s\S]{0,120}opacity: 0\.3;/);
});

test("the inject key is a shell seam, exported from dock.ts", () => {
  assert.equal(typeof DOCK_DRAFT_FOCUS_KEY, "symbol");
  assert.equal(String(DOCK_DRAFT_FOCUS_KEY), "Symbol(omega-dock-draft-focus)");
});
