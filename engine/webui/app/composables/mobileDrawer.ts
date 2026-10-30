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
import { dockableIcon, type DockComponentName } from "./dockables.ts";

export interface DrawerTab {
  id: DockComponentName;
  title: string;
  /** the tab chip's iconset glyph (composables/icons.ts via dockableIcon);
      null when the id sits outside the openable registry - the chip then
      renders bare and only the aria label names it */
  icon: string | null;
}

/** the drawer's tab set for one edge: the live edge pane's tabs when it
    exists, else the registry default. The drawer's own active state
    indexes this list - it never touches the dock tree. Tabs carry the
    SAME iconset vocabulary the desktop edge chips use (card t_4bf1d5b7:
    the drawer layer owns mobile tab rendering - icons once, no text
    label; the title is the tooltip + aria name). */
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

/** clamp an active index against a tab set so a DnD that shrank the edge
    pane's tabs while the drawer was closed never indexes past the end */
export function clampDrawerTab(
  tabs: readonly DrawerTab[],
  index: number,
): DrawerTab | undefined {
  if (tabs.length === 0) return undefined;
  return tabs[Math.min(Math.max(0, index), tabs.length - 1)];
}

// --- the menubar toggle route (card t_4bf1d5b7) -----------------------------
// The menubar's three edge toggles surface at EVERY breakpoint. Above 768px
// they flip the DOCK TREE (toggleEdge / toggleBottom: hide/reopen the pinned
// pane). Below it the tree is chrome the zero-collapse CSS overrides on
// sight - the toggles must drive the OVERLAY DRAWER FLAGS instead, or the
// user clicks a toggle that changes state nothing renders (report 1). The
// routing decision lives here so the node suite can pin the contract
// behaviorally: on mobile the dock api is NEVER called, and the drawer
// flags flip independent of it.

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

/** route one menubar toggle. Desktop: flips the dock tree, returns the
    state untouched. Mobile: flips the overlay flags (one drawer at a
    time), NEVER calls the dock - the tree keeps its desktop shape for
    when the viewport returns above the breakpoint. */
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