// omega webui: mobile drawer tab derivation. REPRO 1's root cause was the drawer riding
// inside the dock pane's slot: a desktop-closed sidebar prunes the edge pane, so the
// slot never rendered and the drawer opened blank. This module is the pure derivation
// so the unit suite can pin the decoupling contract (the drawer never indexes the dock
// tree's presence - only its data, when available). The title resolver is injected by
// the caller (app.vue passes the same voice the dock tabbar uses - a module
// dockable.json title wins, the registry title is the fallback).

import type { DockPane } from "./dock";
import { dockableIcon, type DockComponentName } from "./dockables.ts";

export interface DrawerTab {
  id: DockComponentName;
  title: string;
  icon: string | null;
}

/**
 * the drawer's tab set for one edge.
 * The drawer's own active state indexes this list - it never touches the dock tree.
 */
export function drawerTabsForEdge(
  pane: DockPane | null,
  fallback: readonly DockComponentName[],
  resolveTitle: (id: DockComponentName) => string,
): DrawerTab[] {
  const ids: DockComponentName[] = pane
    ? (pane.tabs.map((t) => t.component) as DockComponentName[])
    : (fallback as readonly DockComponentName[]).slice();
  return ids.map((id) => ({ id, title: resolveTitle(id), icon: dockableIcon(id) }));
}

/**
 * clamp an active index against a tab set so a DnD that shrank the edge pane's tabs
 * while the drawer was closed never indexes past the end
 */
export function clampDrawerTab(
  tabs: readonly DrawerTab[],
  index: number,
): DrawerTab | undefined {
  if (tabs.length === 0) return undefined;
  return tabs[Math.min(Math.max(0, index), tabs.length - 1)];
}

// -- the menubar toggle route ----------------------------- The menubar's three edge.
// Below it the tree is chrome the zero-collapse CSS overrides on sight - the toggles
// must drive the OVERLAY DRAWER FLAGS instead, or the user clicks a toggle that changes
// state nothing renders (report 1). The routing decision lives here so the node suite
// can pin the contract behaviorally: on mobile the dock api is NEVER called, and the
// drawer flags flip independent of it.

export type MobileShell = "sidebar" | "inspector" | "bottom";

/** the mobile flip - one drawer at a time (the established overlay rule) */
export function shellToggleMobile(
  shell: MobileShell,
  open: Record<MobileShell, boolean>,
): Record<MobileShell, boolean> {
  const next: Record<MobileShell, boolean> = {
    sidebar: false,
    inspector: false,
    bottom: false,
  };
  next[shell] = !open[shell];
  return next;
}

/** the dock surface the desktop route needs (the DOCK_API_KEY subset) */
export interface ShellDockApi {
  toggleEdge: (component: "sidebar" | "inspector", side: "before" | "after") => void;
  toggleBottom: () => void;
}

/**
 * route one menubar toggle.
 * Mobile: flips the overlay flags (one drawer at a time), NEVER calls the dock - the
 * tree keeps its desktop shape for when the viewport returns above the breakpoint.
 */
export function routeShellToggle(
  shell: MobileShell,
  isMobile: boolean,
  dock: ShellDockApi | undefined,
  open: Record<MobileShell, boolean>,
): Record<MobileShell, boolean> {
  if (!isMobile) {
    if (dock) {
      if (shell === "sidebar") dock.toggleEdge("sidebar", "before");
      else if (shell === "inspector") dock.toggleEdge("inspector", "after");
      else dock.toggleBottom();
    }
    return open;
  }
  return shellToggleMobile(shell, open);
}

// -- the drawer's renderable clamp ------------------------ A tab is FREE CONTENT in
// the dock.
export const DRAWER_RENDERABLE: Record<MobileShell, readonly DockComponentName[]> = {
  sidebar: ["sidebar", "agents", "files", "settings"],
  inspector: ["files", "settings", "insights", "notifications", "agents"],
  bottom: ["files", "settings", "observability", "terminal"],
};

/** the ids NO drawer block branches. */
export const DRAWER_UNBRANCHED: readonly DockComponentName[] = [
  "chat",
  "viewer",
  "browser",
];

/**
 * a drawer's tab set.
 * `canRender` is the caller's clamp: the branch chain's id set plus the module panels
 * the catch-all resolves.
 * An id it refuses never reaches the drawer's active-id chain (no lying chip, no
 * "module not loaded").
 */
export function drawerTabs(
  pane: DockPane | null,
  floor: readonly DockComponentName[],
  resolveTitle: (id: DockComponentName) => string,
  canRender: (id: DockComponentName) => boolean,
): DrawerTab[] {
  const keep = (tabs: readonly DrawerTab[]) => tabs.filter((t) => canRender(t.id));
  const live = pane ? keep(drawerTabsForEdge(pane, floor, resolveTitle)) : [];
  const seen = new Set(live.map((t) => t.id));
  const rest = keep(drawerTabsForEdge(null, floor, resolveTitle)).filter((t) => !seen.has(t.id));
  return live.concat(rest);
}