// omega webui: card t_4bf1d5b7 - the mobile BOTTOM drawer + icon-only
// drawer tab chips. Two Endo reports, one layer:
//   report 1: the menubar's bottom toggle flipped the DESKTOP tree below
//     the breakpoint, where the zero-collapse CSS takes the .edge-bottom
//     cell out of flow regardless - the click changed state nothing
//     rendered. The fix routes the toggle: desktop keeps driving the
//     tree, mobile flips the BOTTOM DRAWER flag and never touches the
//     dock api (routeShellToggle - pinned here behaviorally).
//   report 2: the mobile drawer strips rendered `{{ t.title }}` text;
//     the ruling: icons. DrawerTab now carries the registry glyph
//     (dockableIcon - the same vocabulary the desktop chips use).
// Convention: the node suite never compiles the SFC (composition-test
// law); the derivation + route are behavior, the SFC wiring is a
// structural pin.
// Run: node --test "test/*.test.ts"
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  drawerTabsForEdge,
  shellToggleMobile,
  routeShellToggle,
  type MobileShell,
  type ShellDockApi,
} from "../app/composables/mobileDrawer.ts";
import {
  COMPONENT_TITLES,
  defaultHostTabs,
  dockableIcon,
  type DockComponentName,
} from "../app/composables/dockables.ts";
import type { DockPane } from "../app/composables/dock.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

function pane(tabs: DockComponentName[], edge: "left" | "right" | "bottom"): DockPane {
  return {
    kind: "pane",
    id: "n1",
    tabs: tabs.map((component) => ({ component })),
    active: 0,
    fixed: 240,
    edge,
  };
}

// --- report 1: the bottom drawer opens on the toggle, tree untouched ------

function stubDock(): ShellDockApi & { calls: string[] } {
  const calls: string[] = [];
  return {
    calls,
    toggleEdge: (c, side) => calls.push(`toggleEdge:${c}:${side}`),
    toggleBottom: () => calls.push("toggleBottom"),
  };
}

const closed: Record<MobileShell, boolean> = {
  sidebar: false,
  inspector: false,
  bottom: false,
};

test("routeShellToggle: BELOW the breakpoint the dock api is never called - the tree keeps its shape", () => {
  const dock = stubDock();
  const next = routeShellToggle("bottom", true, dock, closed);
  assert.deepEqual(dock.calls, [], "no tree op ran under the breakpoint");
  assert.deepEqual(next, { sidebar: false, inspector: false, bottom: true });
  // and back: the same toggle closes the drawer - no tree op either way
  const shut = routeShellToggle("bottom", true, dock, next);
  assert.deepEqual(dock.calls, []);
  assert.deepEqual(shut, closed);
});

test("routeShellToggle: the bottom drawer flips independent of the dock flags (view state, not layout)", () => {
  const dock = stubDock();
  // a DESKTOP-closed bottom pane (t_aa9dbada closed-flag semantics) must
  // not gate the mobile drawer - the route reads only the drawer flags
  const next = routeShellToggle("bottom", true, dock, {
    sidebar: false,
    inspector: false,
    bottom: false,
  });
  assert.equal(next.bottom, true);
  assert.deepEqual(dock.calls, []);
});

test("routeShellToggle: one drawer at a time on mobile", () => {
  const dock = stubDock();
  const open = { sidebar: true, inspector: false, bottom: false };
  const next = routeShellToggle("bottom", true, dock, open);
  assert.deepEqual(next, { sidebar: false, inspector: false, bottom: true });
});

test("routeShellToggle: ABOVE the breakpoint the toggle drives the dock tree, flags untouched", () => {
  const dock = stubDock();
  const open = { ...closed, bottom: true };
  // desktop sidebar/inspector edges
  routeShellToggle("sidebar", false, dock, open);
  routeShellToggle("inspector", false, dock, open);
  // the bottom toggle is dock.toggleBottom again (the hide/reopen path)
  const next = routeShellToggle("bottom", false, dock, open);
  assert.deepEqual(dock.calls, [
    "toggleEdge:sidebar:before",
    "toggleEdge:inspector:after",
    "toggleBottom",
  ]);
  assert.deepEqual(next, open, "desktop leaves the drawer flags alone");
});

test("routeShellToggle: a missing dock api degrades to a no-op, never a throw", () => {
  assert.deepEqual(routeShellToggle("bottom", false, undefined, closed), closed);
});

test("shellToggleMobile: flips only the requested shell, closes the others", () => {
  assert.deepEqual(
    shellToggleMobile("inspector", { sidebar: false, inspector: false, bottom: true }),
    { sidebar: false, inspector: true, bottom: false },
  );
  assert.deepEqual(
    shellToggleMobile("sidebar", closed),
    { sidebar: true, inspector: false, bottom: false },
  );
});

// --- report 2: the drawer tab chips carry the registry glyph -------------

test("drawerTabsForEdge: every tab carries the dockable-registry iconset glyph", () => {
  const tabs = drawerTabsForEdge(
    pane(["files", "cron", "skills"], "right"),
    [],
    (id) => COMPONENT_TITLES[id],
  );
  for (const t of tabs) {
    assert.equal(t.icon, dockableIcon(t.id), `${t.id}: chip glyph matches the registry`);
    assert.ok(t.icon, `${t.id}: a registry tab never renders a bare chip`);
    assert.ok(t.title.length > 0, `${t.id}: the title survives (aria/tooltip voice)`);
  }
});

test("drawerTabsForEdge: an id outside the openable registry renders a bare chip, not a throw", () => {
  // lenient like dockableIcon: pre-modernizer trees can carry such ids
  // (the dockables doc names "inspector" as the canonical stray)
  const stray = "inspector" as DockComponentName;
  const tabs = drawerTabsForEdge(
    pane([stray], "right"),
    [],
    (id) => COMPONENT_TITLES[id] ?? id,
  );
  assert.equal(tabs[0]!.icon, null);
});

test("the bottom drawer's default tab set derives from the registry bottom-home (logs), with its glyph", () => {
  const tabs = drawerTabsForEdge(
    null,
    defaultHostTabs("bottom"),
    (id) => COMPONENT_TITLES[id],
  );
  assert.ok(tabs.length > 0, "the registry seats at least one bottom-home tab");
  for (const t of tabs) {
    assert.equal(t.id, "http_channel");
    assert.equal(t.icon, dockableIcon("http_channel"));
  }
});

test("the bottom drawer reads the LIVE bottom pane's tabs when the tree holds one", () => {
  const tabs = drawerTabsForEdge(
    pane(["http_channel", "files"], "bottom"),
    defaultHostTabs("bottom"),
    (id) => COMPONENT_TITLES[id],
  );
  assert.deepEqual(tabs.map((t) => t.id), ["http_channel", "files"]);
});

// --- structural pins (no-SFC-compile convention) -------------------------

test("MobileDrawer renders the icon chip, not the text title", () => {
  const sfc = read("app/components/shell/MobileDrawer.vue");
  // the chip renders the Icon wrapper from the tab's registry glyph...
  assert.ok(sfc.includes("<Icon"), "the chip renders through the iconset wrapper");
  assert.ok(sfc.includes(":name=\"t.icon\""), "the glyph comes from the tab's registry icon");
  // ...and the title is the accessible name + tooltip, never body text
  assert.ok(sfc.includes(':aria-label="t.title"'), "the title rides the accessible name");
  assert.ok(!/\{\{\s*t\.title\s*\}\}/.test(sfc), "no text title renders inside the chip");
  // the bottom edge is a supported side
  assert.ok(sfc.includes('"left" | "right" | "bottom"'), "side covers bottom");
});

test("app.vue: the bottom drawer wiring exists and the toggle routes (not inlines) the decision", () => {
  const app = read("app/app.vue");
  // a third drawer, side="bottom", driven by its own open flag
  assert.ok(app.includes('side="bottom"'), "the bottom drawer is mounted");
  assert.ok(app.includes(":open=\"bottomDrawerOpen\""), "it rides the bottomDrawerOpen flag");
  // the scrim covers all three drawers
  assert.ok(
    app.includes("isMobile && (drawerOpen || inspectorOpen || bottomDrawerOpen)"),
    "the scrim closes the bottom drawer too",
  );
  // the menubar emits route through app.vue (MenuBar no longer injects
  // the dock api itself - one routing decision, one place)
  assert.ok(app.includes("@toggle-bottom=\"toggleBottomPanel\""), "app.vue owns the bottom route");
  assert.ok(app.includes("routeShellToggle"), "the route is the pinned pure helper");
  const menubar = read("app/components/shell/MenuBar.vue");
  assert.ok(menubar.includes("$emit('toggle-bottom')"), "the button emits; it does not drive the dock");
  assert.ok(!menubar.includes("DOCK_API_KEY"), "MenuBar no longer injects the dock api");
  // the zero-collapse CSS still owns the cell; the drawer owns the content
  const bottompanel = read("app/assets/css/bottompanel.css");
  assert.ok(bottompanel.includes(".dock-pane.edge-bottom"), "the zero-collapse cell rule stands");
  const main = read("app/assets/css/main.css");
  assert.ok(main.includes(".mobile-drawer.bottom"), "the bottom slide geometry exists");
  assert.ok(main.includes("translateY(100%)"), "the bottom drawer slides up");
});