// omega webui: mobile drawer tab derivation (card t_e76ce7b6).
//
// The mobile drawer renders the pinned-edge content (sessions on the
// left, the inspector tabs on the right) as its own overlay, independent
// of the dock tree. REPRO 1's root cause was the drawer riding inside
// the dock pane's slot: a desktop-closed sidebar prunes the edge pane,
// so the slot never rendered and the drawer opened blank. The fix
// separates the states - the drawer reads the live edge pane's tabs when
// it exists, and falls back to the registry default when the pane was
// pruned. This module is the pure derivation so the unit suite can pin
// the decoupling contract (the drawer never indexes the dock tree's
// presence - only its data, when available).
//
// PURE on purpose: no Vue, no module-panels import. The title resolver
// is injected by the caller (app.vue passes the same voice the dock
// tabbar uses - a module dockable.json title wins, the registry title is
// the fallback).

import type { DockPane } from "./dock";
import type { DockComponentName } from "./dockables";

export interface DrawerTab {
  id: DockComponentName;
  title: string;
}

/** the drawer's tab set for one edge: the live edge pane's tabs when it
    exists, else the registry default. The drawer's own active state
    indexes this list - it never touches the dock tree. */
export function drawerTabsForEdge(
  pane: DockPane | null,
  fallback: readonly DockComponentName[],
  resolveTitle: (id: DockComponentName) => string,
): DrawerTab[] {
  const ids: DockComponentName[] = pane
    ? (pane.tabs.map((t) => t.component) as DockComponentName[])
    : (fallback as readonly DockComponentName[]).slice();
  return ids.map((id) => ({ id, title: resolveTitle(id) }));
}

/** clamp an active index against a tab set so a DnD that shrank the edge
    pane's tabs while the drawer was closed never indexes past the end */
export function clampDrawerTab(
  tabs: readonly DrawerTab[],
  index: number,
): DrawerTab | undefined {
  if (tabs.length === 0) return undefined;
  return tabs[Math.min(Math.max(0, index), tabs.length - 1)];
}