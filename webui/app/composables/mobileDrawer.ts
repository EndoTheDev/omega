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

// --- the drawer's renderable clamp (card t_572015db) ------------------------
// A tab is FREE CONTENT in the dock: moveTab lets ANY unlocked tab land in
// any pane (only `sidebar` is locked, and only to the left shell), so the
// live edge pane of a narrow viewport can hold a tab its drawer has no
// branch for. Without this clamp the drawer chain fell through to
// InspectorPanel(module="<id>"), read the PANEL manifest and rendered
// "module not loaded" for a tab that was loaded (card t_07ad4fda's
// cross-host residual: agents dragged into the bottom pane, terminal into
// the right one, and the same for every shell-owned view).
//
// A branch per permutation is not available: chat and the viewer render
// from the TAB payload (a session, a viewer ref) a drawer chain does not
// carry. So the drawer CLAMPS - it shows only the ids its own branch chain
// resolves (this map) plus the module panels the generic catch-all really
// resolves. A tab dragged into a non-default host simply stops at the
// drawer; every tab whose registry home is an edge stays reachable from
// THAT edge's drawer, because the drawer's tab list is live pane + registry
// floor (the home strip) clamped the same way.
export const DRAWER_RENDERABLE: Record<MobileShell, readonly DockComponentName[]> = {
  // the sessions shell: its own locked tab, plus the two shell-owned views
  // every drawer branches (files/settings are freely draggable residents)
  sidebar: ["sidebar", "files", "settings"],
  // the inspector shell: its home views, the shell inventory + insights
  // (t_14f73742 / t_d7fd3ad3) and the two capsule/panel views it seats by
  // their own branch (notifications t_180db2cb, agents t_07ad4fda)
  inspector: ["files", "settings", "extensions", "insights", "notifications", "agents"],
  // the bottom shell: the terminal + Logs capsule views (t_07ad4fda,
  // t_e703180e) and the draggable shell views
  bottom: ["files", "settings", "observability", "terminal"],
};

/** the ids NO drawer block branches: chat and the viewer render from a
    per-tab payload, and the browser is a main-pane content tab whose mobile
    home is the page itself. The pin suite fails the next dockable that is
    in none of the three lists (renderable / panel-resolved / declared here),
    so a new view has to pick one on purpose. */
export const DRAWER_UNBRANCHED: readonly DockComponentName[] = ["chat", "viewer", "browser"];

/** card t_572015db: a drawer's tab set. The live edge pane's tabs first (its
    own order), then the registry floor's missing ids - the right drawer's
    t_67fc1905 floor law, applied to all three edges, so every edge-home tab
    stays reachable from its home drawer even when the live pane has drifted,
    and a fully-drifted (or empty) pane still opens a usable drawer.
    `canRender` is the caller's clamp: the branch chain's id set plus the
    module panels the catch-all resolves. An id it refuses never reaches the
    drawer's active-id chain (no lying chip, no "module not loaded"). */
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