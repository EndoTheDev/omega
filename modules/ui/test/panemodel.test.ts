// omega webui: unit tests for the pane-model reset (card t_4ae27e0d) - the
// four gates from the card: the never-empty invariant, focus-existing
// session routing, sidebar tabs unclosable, hover-X on main panes only.
// The engine gates live in dock.test.ts; this file pins the SHELL WIRING
// (app.vue / DockPane.vue / MenuBar.vue sources) on top of them.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import {
  ensurePinnedEdges,
  firstMainPaneId,
  makePane,
  makeSplit,
  paneCount,
  removePane,
  firstChatTab,
  findChatTab,
  openTab,
  type DockNode,
  type DockPane,
} from "../app/composables/dock.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

function fixedTree(): DockNode {
  return makeSplit("row", [
    makePane("sidebar", 240, "left"),
    makePane("chat", null),
    makePane("files", 300, "right"),
  ]);
}

// --- gate: the never-empty invariant (last-pane close refused) --------------

describe("gate: the never-empty invariant", () => {
  it("the last pane refuses removal", () => {
    const single: DockNode = makePane("chat");
    assert.equal(removePane(single, single.id), single);
  });
  it("pinned panes refuse removal (sidebars never go)", () => {
    const t = fixedTree();
    for (const paneName of ["sidebar", "files"] as const) {
      const p = panesOf(t).find((x) => x.tabs[0]?.component === paneName)!;
      assert.equal(removePane(t, p.id), t, `${paneName} pane never goes`);
    }
    assert.equal(paneCount(t), 3);
  });
  it("a loaded tree always has BOTH pinned edges after repair", () => {
    const out = ensurePinnedEdges(makePane("chat"));
    assert.equal(paneCount(out), 3);
  });
});

// --- gate: focus-existing-session routing ------------------------------------

describe("gate: session routing (open/focus, never a duplicate)", () => {
  it("a tab already bound to the session exists ANYWHERE (tree walk)", () => {
    const t: DockNode = makeSplit("row", [makePane("sidebar", 240), makePane("chat", null)]);
    const pane = panesOf(t).find((p) => p.tabs[0]?.component === "chat")!;
    const withTab = openTab(t, pane.id, {
      component: "chat",
      sessionRef: "s1",
      key: "c1",
    });
    // the opener's guard: firstChatTab(sessionId) finds it in its pane...
    const found = firstChatTab(withTab, "s1");
    assert.ok(found);
    // ...and findChatTab resolves the pane's binding for the focusing call
    assert.equal(findChatTab(withTab, "c1")!.paneId, pane.id);
  });
  it("the routing target is the FIRST MAIN pane (sidebars never host chat)", () => {
    const t = fixedTree();
    assert.equal(firstMainPaneId(t), paneByComp(t, "chat").id);
    // with the main pane gone, ensurePinnedEdges keeps the shape; the
    // store's ensureMainPane re-inserts (covered in dockables.test.ts)
  });
  it("app.vue routes opens through focus-first + ensureMainPane", () => {
    const appVue = src("app/app.vue");
    // the sidebar click and the '+' flows share THE opener
    assert.match(appVue, /openChatTab\(sessionId, paneId\??\)/);
    // the opener FOCUSES the existing binding before minting anything
    assert.match(appVue, /dock\.firstChatTab\(sessionId\)/);
    assert.match(appVue, /dock\.ensureMainPane\(\)/);
    // the settled rule's counterpart: focus moves, the URL follows
    assert.match(appVue, /history\.replaceState/);
  });
});

// --- gate: sidebar tabs unclosable -------------------------------------------

describe("gate: sidebar tabs unclosable", () => {
  it("tab chrome follows the TAB, not the pane (t_d6b657a7)", () => {
    const pane = src("app/components/DockPane.vue");
    // the close X renders behind a closable-component check only...
    assert.match(pane, /v-if="tabClosable\(t\)"/);
    // ...and the shell marker no longer hides tab chrome: a chat tab in a
    // sidebar keeps its X
    assert.doesNotMatch(src("app/assets/css/main.css"), /\.dock-pane\.pinned \.dock-tab-close/);
    // every tab chip drags - the lock lives in the hit-test, not the chip
    assert.match(pane, /draggable="true"/);
  });
  it("edge shells: no actions menu, no remove X, but the '+' refills", () => {
    const pane = src("app/components/DockPane.vue");
    // the actions ('...') button stays shell-guarded
    assert.match(pane, /v-if="!pinned"/);
    assert.match(pane, /v-if="api\.canRemovePane\(node\.id\)"/);
    // the menus ride the MENU inventory now - the refill source for any
    // pane (sessions excluded: locked; settings: no entry point)
    assert.match(pane, /MENU_DOCKABLES\.map\(dockableOf\)/);
    assert.doesNotMatch(pane, /v-for="d in DOCKABLES"/);
    assert.doesNotMatch(pane, /MAIN_DOCKABLES/);
  });
});

// --- gate: hover-X on main panes only -----------------------------------------

describe("gate: hover-X on main panes only", () => {
  it("the resting tab hides its X; hover (or keyboard focus) reveals it", () => {
    const css = src("app/assets/css/main.css");
    // the resting close button is transparent...
    assert.match(css, /\.dock-tab-close\s*\{[\s\S]*?opacity: 0/);
    // ...hover reveals it; the active state does NOT (hover only)
    assert.match(css, /\.dock-tab:hover \.dock-tab-close/);
    assert.doesNotMatch(css, /\.dock-tab\.active \.dock-tab-close/);
    // keyboard parity survives the hover-only rule
    assert.match(css, /\.dock-tab-close:focus-visible \{ opacity: 1/);
  });
});

// --- gate: the fixed-shape chrome ---------------------------------------------

describe("gate: fixed shape chrome", () => {
  it("MenuBar: the desktop toggles are BACK, the gear is GONE (t_47b776ad)", () => {
    const bar = src("app/components/shell/MenuBar.vue");
    // the toggle pair re-asserts the toggleEdge contract: state-aware
    // titles, hidden desktop-side only via the edge-toggle class
    assert.match(bar, /aria-label="Toggle left sidebar"/);
    assert.match(bar, /aria-label="Toggle right sidebar"/);
    assert.match(bar, /class="icon-btn edge-toggle"/);
    // the mobile drawer handles survive unchanged
    assert.match(bar, /drawer-btn/);
    assert.match(bar, /aria-label="Open the sessions drawer"/);
    assert.match(bar, /aria-label="Open the inspector drawer"/);
    // the settings gear died: settings opens from the pane '+' menu
    assert.doesNotMatch(bar, /aria-label="Settings"/);
    assert.doesNotMatch(bar, /\$emit\("settings"\)/);
  });
  it("app.vue wires the reset: desktop toggles drive toggleEdge, the gear's opener is gone", () => {
    const appVue = src("app/app.vue");
    // the toggles route through the dock's toggleEdge
    assert.match(appVue, /toggleEdge/);
    // ensureMainPane still guards the main-pane routing
    assert.match(appVue, /ensureMainPane/);
    // openSettings died with the gear; the '+'-menu path is untouched
    assert.doesNotMatch(appVue, /openSettings/);
    assert.doesNotMatch(appVue, /@settings/);
    // the inspector chip strip dissolved into real dock tabs
    assert.match(appVue, /#pane-files/);
    assert.match(appVue, /#pane-http_channel/);
    assert.doesNotMatch(appVue, /#pane-inspector/);
    assert.match(appVue, /InspectorPanel/);
    assert.match(appVue, /FilesTab/);
  });
  it("the old chip strip is gone from the stylesheet", () => {
    const css = src("app/assets/css/main.css");
    assert.doesNotMatch(css, /\.inspector-tab\b/);
    assert.match(css, /module-panels/);
  });
});

// helpers ------------------------------------------------------------------

function panesOf(t: DockNode): DockPane[] {
  const out: DockPane[] = [];
  const walk = (n: DockNode): void => {
    if (n.kind === "pane") out.push(n);
    else n.children.forEach(walk);
  };
  walk(t);
  return out;
}
function paneByComp(t: DockNode, c: string): DockPane {
  const p = panesOf(t).find((x) => x.tabs[0]?.component === c);
  assert.ok(p, `expected a ${c} pane`);
  return p!;
}