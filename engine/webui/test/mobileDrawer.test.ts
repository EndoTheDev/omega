// omega webui: unit tests for the mobile drawer tab derivation (card
// t_e76ce7b6). The drawer is decoupled from the dock tree: it reads the
// live edge pane's tabs when the pane exists, and falls back to the
// registry default when the pane was pruned by a desktop close (REPRO 1's
// root cause - the drawer must open with content regardless of the
// desktop-closed state). Run: node --test "test/*.test.ts"
import { test } from "node:test";
import assert from "node:assert/strict";

import { drawerTabsForEdge, clampDrawerTab } from "../app/composables/mobileDrawer.ts";
import {
  INSPECTOR_TAB_COMPONENTS,
  defaultHostTabs,
  COMPONENT_TITLES,
  dockableIcon,
  type DockComponentName,
} from "../app/composables/dockables.ts";
import type { DockPane } from "../app/composables/dock.ts";

const title = (id: DockComponentName) => COMPONENT_TITLES[id];

function pane(tabs: DockComponentName[], edge: "left" | "right"): DockPane {
  return {
    kind: "pane",
    id: "n1",
    tabs: tabs.map((component) => ({ component })),
    active: 0,
    fixed: 240,
    edge,
  };
}

test("drawerTabsForEdge: a pruned edge pane (null) falls back to the registry default - REPRO 1", () => {
  // the desktop toggle prunes the inspector pane from the tree; the
  // drawer must still derive the full inspector tab strip from the
  // registry default, not from the (absent) pane
  const tabs = drawerTabsForEdge(null, INSPECTOR_TAB_COMPONENTS, title);
  assert.equal(tabs.length, INSPECTOR_TAB_COMPONENTS.length);
  assert.deepEqual(
    tabs.map((t) => t.id),
    [...INSPECTOR_TAB_COMPONENTS],
  );
  assert.deepEqual(
    tabs.map((t) => t.title),
    [...INSPECTOR_TAB_COMPONENTS].map((id) => COMPONENT_TITLES[id]),
  );
});

test("drawerTabsForEdge: the sidebar default is sessions alone", () => {
  const tabs = drawerTabsForEdge(null, ["sidebar"], title);
  assert.deepEqual(tabs, [
    { id: "sidebar", title: COMPONENT_TITLES.sidebar, icon: dockableIcon("sidebar") },
  ]);
});

test("drawerTabsForEdge: a live edge pane drives the tab set (its actual tabs, in order)", () => {
  const live = pane(["files", "cron", "skills"], "right");
  const tabs = drawerTabsForEdge(live, INSPECTOR_TAB_COMPONENTS, title);
  assert.deepEqual(
    tabs.map((t) => t.id),
    ["files", "cron", "skills"],
  );
});

test("drawerTabsForEdge: a free-content left pane hosting dragged-in inspector tabs surfaces them", () => {
  // the left shell is sessions home but accepts free tabs; the drawer
  // reflects whatever the pane actually holds
  const live = pane(["sidebar", "files"], "left");
  const tabs = drawerTabsForEdge(live, ["sidebar"], title);
  assert.deepEqual(
    tabs.map((t) => t.id),
    ["sidebar", "files"],
  );
});

test("drawerTabsForEdge: the injected title resolver wins over the registry fallback", () => {
  // logs left the inspector strip (bottom-only home, Endo's comment
  // 2026-10-26) - the resolver still wins for any seated id; assert
  // over the bottom host's default strip, where logs still lives
  const resolve = (id: DockComponentName) =>
    id === "http_channel" ? "Logs" : COMPONENT_TITLES[id];
  const tabs = drawerTabsForEdge(null, defaultHostTabs("bottom"), resolve);
  const logs = tabs.find((t) => t.id === "http_channel");
  assert.equal(logs?.title, "Logs");
});

test("clampDrawerTab: an out-of-range active index clamps to the last tab", () => {
  const tabs = drawerTabsForEdge(null, INSPECTOR_TAB_COMPONENTS, title);
  // a DnD shrank the pane's tabs while the drawer held index 5; the
  // drawer never indexes past the end
  assert.equal(clampDrawerTab(tabs, 99)?.id, tabs[tabs.length - 1]!.id);
  assert.equal(clampDrawerTab(tabs, -3)?.id, tabs[0]!.id);
  assert.equal(clampDrawerTab(tabs, 2)?.id, tabs[2]!.id);
});

test("clampDrawerTab: an empty tab set yields undefined (no pane, no fallback)", () => {
  assert.equal(clampDrawerTab([], 0), undefined);
});