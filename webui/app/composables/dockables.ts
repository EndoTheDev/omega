// THE DOCKABLE CONTRACT. Global chrome (MenuBar, StatusBar) is the window, not content -
// never dockable; dock.ts re-exports this inventory.

import type { PaneZone } from "./panes.ts";

// Component names a dock pane may host, in tab order. nuxt.config.ts's module-tabs scan
// generates the manifest app.vue's outlets bind from.
export const TAB_MODULES = [
  "terminal",
  "browser",
  "notifications",
  "observability",
] as const;
export const DOCK_COMPONENTS = [
  "sidebar",
  "chat",
  "viewer",
  "files",
  "cron",
  "skills",
  "tools",
  "agents",
  "settings",
  "kanban",
  "insights",
  "mcp",
  // One terminal per pane (the singleton invariant reads one per host). Known ceiling:
  // X-Frame-Options sites refuse the embed; the tab shows a fallback panel, never blank.
  ...TAB_MODULES,
] as const;
export type DockComponentName = (typeof DOCK_COMPONENTS)[number];

// Ids a pre-reset tree can persist; their law is now THE LOCKED LAST TAB.
export const LEGACY_DOCK_COMPONENTS = ["inspector", "newtab"] as const;

export type DockInstancePolicy = "singleton" | "multi";

// THE CLASS LAW: one class, one default home, and the class IS the home's zone - `area`
// is DERIVED from it, never a second truth that could drift.
//
//   primary-sidebar    sessions, agents, crons             -> left
//   secondary-sidebar  files, skills, tools, mcp, kanban,  -> right
//                      insights, notifications
//   main               chat, viewer, browser, settings     -> main
//   bottom             logs (observability), terminal      -> bottom
//
// THE ZONE LAW: a view may OCCUPY exactly the zones its class allows (zonesOfClass is
// the one matrix). A visit is never a move-out - close returns the view to its home.
export type ViewClass = "primary-sidebar" | "secondary-sidebar" | "main" | "bottom";

export type DockArea = PaneZone;

/** THE CLASS <-> ZONE MAP (the one adapter; nothing else may spell it) */
export function areaOfClass(c: ViewClass): DockArea {
  switch (c) {
    case "primary-sidebar":
      return "left";
    case "secondary-sidebar":
      return "right";
    case "main":
      return "main";
    case "bottom":
      return "bottom";
  }
}

export function zonesOfClass(c: ViewClass): readonly DockArea[] {
  switch (c) {
    case "main":
      // main members NEVER leave the main pane.
      return ["main"];
    case "bottom":
      // bottom members may visit BOTH sidebars, never the main pane.
      return ["left", "right", "bottom"];
    case "primary-sidebar":
    case "secondary-sidebar":
      return ["left", "right", "main", "bottom"];
    default:
      // runtime data outside the union occupies nothing - the safe refusal, never a
      // throw inside a drag
      return [];
  }
}

// `icon` is a semantic NAME in composables/icons.ts ICONS: the iconset test pins every
// registry entry as a key and the Icon wrapper throws at render on an undeclared name.

// The honest floors: a pane that might show chat keeps the 320px wall, every tool
// component floors at MIN_PANE_PX and so never counts 320 against the "no room" math.
export const MIN_PANE_PX = 128;
export const CHAT_FLOOR_PX = 320;

export interface DockableComponent {
  id: DockComponentName;
  title: string;
  icon: string;
  policy: DockInstancePolicy;
  class: ViewClass;
  /** default home zone - DERIVED from `class` by DOCKABLES, never declared twice */
  area: DockArea;
  /** hosts keeping it alive BEYOND `area`: the singleton invariant reads one per host */
  homes?: readonly DockArea[];
  defaultHome?: DockArea;
  closable: boolean;
  /** the honest px floor a pane holding this tab may never be starved below */
  minPx: number;
}

// THE pinned identity: sidebar = the sessions pane.
export const DOCKABLE_INVENTORY: readonly (Omit<DockableComponent, "minPx" | "area">)[] = [
  {
    id: "sidebar",
    title: "Sessions",
    icon: "sidebar",
    policy: "singleton",
    class: "primary-sidebar",
    closable: false,
  },
  {
    id: "chat",
    title: "chat",
    icon: "chat",
    policy: "multi",
    class: "main",
    closable: true,
  },
  {
    // never in the '+' menus - an EMPTY viewer tab is meaningless, so its only entry
    // point is a row click
    id: "viewer",
    title: "viewer",
    icon: "file-text",
    policy: "multi",
    class: "main",
    closable: true,
  },
  {
    id: "files",
    title: "Files",
    icon: "files",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
  {
    id: "cron",
    title: "cron",
    icon: "cron",
    policy: "singleton",
    class: "primary-sidebar",
    closable: false,
  },
  {
    id: "skills",
    title: "skills",
    icon: "skills",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
  {
    id: "tools",
    title: "tools",
    icon: "tools",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
  {
    id: "agents",
    title: "Agents",
    icon: "agents",
    policy: "singleton",
    class: "primary-sidebar",
    closable: false,
  },
  {
    // ON-DEMAND main-area tab, never a permanent inspector seat: retireInspectorSettings
    // drops a legacy seat on load.
    id: "settings",
    title: "Settings",
    icon: "settings",
    policy: "singleton",
    class: "main",
    closable: true,
  },
  {
    id: "kanban",
    title: "kanban",
    icon: "kanban",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
  {
    id: "insights",
    title: "Insights",
    icon: "insights",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
  {
    id: "mcp",
    title: "MCP",
    icon: "mcp",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
  {
    id: "terminal",
    title: "Terminal",
    icon: "terminal",
    policy: "singleton",
    class: "bottom",
    closable: false,
  },
  {
    id: "observability",
    title: "Logs",
    icon: "activity",
    policy: "singleton",
    class: "bottom",
    closable: false,
  },
  {
    // the body is harness/browser/ui/BrowserTab.vue over GET/PUT /browser/last (the
    // resume URL); embed-refusing sites get the fallback panel, never a blank frame
    id: "browser",
    title: "Browser",
    icon: "browser",
    policy: "singleton",
    class: "main",
    closable: true,
  },
  {
    id: "notifications",
    title: "Notifications",
    icon: "inbox",
    policy: "singleton",
    class: "secondary-sidebar",
    closable: false,
  },
];

const DOCKABLE_MIN_PX: Record<DockComponentName, number> = {
  sidebar: MIN_PANE_PX,
  chat: CHAT_FLOOR_PX,
  viewer: MIN_PANE_PX,
  files: MIN_PANE_PX,
  cron: MIN_PANE_PX,
  skills: MIN_PANE_PX,
  tools: MIN_PANE_PX,
  agents: MIN_PANE_PX,
  settings: MIN_PANE_PX,
  kanban: MIN_PANE_PX,
  insights: MIN_PANE_PX,
  mcp: MIN_PANE_PX,
  browser: MIN_PANE_PX,
  notifications: MIN_PANE_PX,
  terminal: MIN_PANE_PX,
  observability: MIN_PANE_PX,
};

export const DOCKABLES: readonly DockableComponent[] = DOCKABLE_INVENTORY.map(
  (d) => ({ ...d, area: areaOfClass(d.class), minPx: DOCKABLE_MIN_PX[d.id] }),
);

/** WHAT a viewer tab shows; same ref = same tab (the focus-reuse identity). */
export interface ViewerRef {
  kind: "file" | "skill" | "dir";
  name: string;
}

export function validViewerRef(
  raw: unknown,
): raw is ViewerRef {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return false;
  const v = raw as { kind?: unknown; name?: unknown };
  return (
    (v.kind === "file" || v.kind === "skill" || v.kind === "dir") &&
    typeof v.name === "string" &&
    v.name !== ""
  );
}

export function viewerTabTitle(v: ViewerRef): string {
  if (v.kind === "skill") return v.name;
  const base = v.name.split("/").pop() ?? "";
  return base !== "" ? base : v.name;
}

/** throws on an unknown id - panes host only registry components */
export function dockableOf(id: DockComponentName): DockableComponent {
  const d = DOCKABLES.find((x) => x.id === id);
  if (!d) throw new Error(`dockable registry: unknown component "${id}"`);
  return d;
}
/** lenient for RUNTIME data: persisted trees carry pre-reset ids, so unknown -> null */
function safeEntry(id: string): DockableComponent | null {
  if (!(DOCK_COMPONENTS as readonly string[]).includes(id)) return null;
  return dockableOf(id as DockComponentName);
}

/** lenient like safeEntry: an undeclared id counts as a tool tab and never invokes the chat wall */
export function dockableMinPx(id: string): number {
  return safeEntry(id)?.minPx ?? MIN_PANE_PX;
}

/** lenient like safeEntry: an undeclared id renders no icon, never throws */
export function dockableIcon(id: string): string | null {
  return safeEntry(id)?.icon ?? null;
}

/** THE LOCKED TAB: sessions is the one component whose chip may never leave the left. */
export function isLockedTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d === null ? true : d.id === "sidebar";
}

export function viewClassOf(id: DockComponentName): ViewClass {
  return dockableOf(id).class;
}

export function dockableClass(id: string): ViewClass | null {
  return safeEntry(id)?.class ?? null;
}

export function zonesOf(id: string): readonly DockArea[] {
  const d = safeEntry(id);
  if (!d) return [];
  return isLockedTab(d.id) ? [d.area] : zonesOfClass(d.class);
}

/** THE ZONE GATE read by the dock's drag legality */
export function seatAllowsIn(id: string, zone: DockArea): boolean {
  return zonesOf(id).includes(zone);
}

export function classMembers(zone: DockArea): readonly DockComponentName[] {
  return DOCKABLES.filter((d) => d.area === zone).map((d) => d.id);
}

/** tab-bar X policy: closable components only (chat, viewer, browser, settings). */
export function isClosableTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d !== null && d.closable;
}

export const COMPONENT_TITLES: Record<DockComponentName, string> =
  Object.fromEntries(DOCKABLES.map((d) => [d.id, d.title])) as Record<
    DockComponentName,
    string
  >;

export const MAIN_DOCKABLES: readonly DockComponentName[] =
  DOCKABLES.filter((d) => d.area === "main").map((d) => d.id);

// the '+' / empty-pane MENU inventory; sessions is locked, so the loader alone seats it
export const MENU_DOCKABLES: readonly DockComponentName[] = DOCKABLES
  .filter(
    (d) =>
      d.id !== "sidebar" &&
      d.id !== "settings" &&
      d.id !== "viewer",
  )
  .map((d) => d.id);

/** the set IS classMembers, so a seat-map edit cannot leave these lists behind */
export const INSPECTOR_TAB_COMPONENTS: readonly DockComponentName[] =
  classMembers("right");

export const PRIMARY_TAB_COMPONENTS: readonly DockComponentName[] =
  classMembers("left");

export function defaultHostTabs(host: DockArea): DockComponentName[] {
  return DOCKABLES
    .filter((d) => (d.defaultHome ?? d.area) === host)
    .map((d) => d.id);
}

export function hostsOf(id: DockComponentName): readonly DockArea[] {
  const d = dockableOf(id);
  return d.homes ?? [d.area];
}

export interface DockableSpec {
  title?: string;
  icon?: string;
  policy?: DockInstancePolicy;
}

const MODULE_ICON = "module";

export interface DockableComponentLike {
  id: string;
  title: string;
  icon: string;
  policy: DockInstancePolicy;
}

export function dockablesFromPanels(
  panels: readonly { module: string; dockable?: DockableSpec | null }[],
): DockableComponentLike[] {
  const openable = new Set<string>(DOCK_COMPONENTS);
  const out: DockableComponentLike[] = [];
  for (const p of panels) {
    if (!p.dockable || openable.has(p.module)) continue;
    out.push({
      id: p.module,
      title: p.dockable.title ?? p.module,
      icon: p.dockable.icon ?? MODULE_ICON,
      policy: p.dockable.policy ?? "singleton",
    });
  }
  return out;
}

// THE MODULE GATE: a dockable is MODULE-OWNED when its pane body ships from a module's
// ui/ dir; the owned set is DERIVED from the composition manifests, never a second list
// that could drift. Ids that are not module ids (chat, viewer, sidebar, agents, insights,
// settings) can never be gated: those panes are the shell's own.

export interface ModuleGate {
  owned: ReadonlySet<string>;
  off: ReadonlySet<string>;
}

/** no manifests, no state: nothing is ever hidden */
export const EMPTY_MODULE_GATE: ModuleGate = {
  owned: new Set<string>(),
  off: new Set<string>(),
};

export function compositionModuleIds(
  ...manifests: readonly (readonly { module: string }[])[]
): Set<string> {
  const ids = new Set<string>();
  for (const manifest of manifests) {
    for (const entry of manifest) {
      if (typeof entry?.module === "string" && entry.module !== "") ids.add(entry.module);
    }
  }
  return ids;
}

/**
 * THE ONE GATE PREDICATE: hidden iff a module OWNS it and its module is off.
 */
export function moduleGated(dockableId: string, gate: ModuleGate): boolean {
  return gate.owned.has(dockableId) && gate.off.has(dockableId);
}
