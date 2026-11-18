// omega webui: THE DOCKABLE CONTRACT. Global chrome (MenuBar, StatusBar) is NOT in this
// registry: they are the window, not content - never dockable. The dock (dock.ts)
// re-exports the component universe from here, the menus render the inventory, and the
// unit suite pins every contract rule.

import type { PaneZone } from "./panes.ts";

// component names a dock pane may host. The shell keeps the composition decision (which
// modules the dock hosts as tabs, in this order - the registry order below): the
// capsule ships the component in harness/<name>/ui/ and the build's module-tabs scan
// (nuxt.config.ts) generates the import manifest (module-tabs.generated.ts); app.vue's
// outlets bind the component from that manifest, never a hardcoded import.
export const TAB_MODULES = [
  // the integrated terminal - VS Code-style, no restrictions.
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
  // the never-zero placeholder LEFT this list - see LEGACY_DOCK_COMPONENTS. One
  // terminal per pane (the singleton invariant reads one per host); more terminals =
  // more panes/splits. Known ceiling, built honestly around it: sites that send
  // X-Frame-Options refuse the embed, and the tab answers with a clean fallback panel
  // (open in a new tab + copy-URL), never a silent blank frame.
  ...TAB_MODULES,
] as const;
export type DockComponentName = (typeof DOCK_COMPONENTS)[number];

// pre-reset trees persist tabs {component. It was the never-zero placeholder a close
// seated in a pane's last-tab slot; retired the approach - the blank "New
// Tab" body WAS the empty bad looking space, and the law it carried is now THE LOCKED
// LAST TAB.
export const LEGACY_DOCK_COMPONENTS = ["inspector", "newtab"] as const;

export type DockInstancePolicy = "singleton" | "multi";

// THE CLASS LAW (canvas C1, the seat map) ------------------------------
//
// Every view has ONE class and ONE default home. The class IS its home's
// zone - there is no separate "area" truth to drift from it: `area` on a
// registry entry is DERIVED from the class (DOCKABLES composes it), so a
// component cannot be classed one place and seated another.
//
// primary-sidebar sessions, agents, crons -> the left shell
// secondary-sidebar files, skills, tools, mcp, -> the right shell
//                     kanban, insights, notifications
// main chat, viewer (file preview), -> the main column
//                     browser, settings (the gear's
//                     on-demand tab, never a sidebar view)
// bottom logs (observability), -> the bottom panel
//                     terminal
//
// THE ZONE LAW: a view may OCCUPY
// exactly the zones its class allows - zonesOfClass below is the one
// matrix, and the dock's drag legality reads it (seatAllowsIn). Main
// members never leave the main pane; bottom members visit the two sidebars
// but never the main pane; sidebar views move freely between the two
// sidebars, may visit the main pane (a temporary promote) and the bottom
// pane. A visit is never a move-out: the view's home stands, so close
// returns it home. No permanent move-out via drag exists.
export type ViewClass = "primary-sidebar" | "secondary-sidebar" | "main" | "bottom";

/**
 * the four seats, in the pane vocabulary the layout record persists.
 */
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

/** THE ZONE LAW as one matrix: the zones a view of class `c` may OCCUPY. */
export function zonesOfClass(c: ViewClass): readonly DockArea[] {
  switch (c) {
    case "main":
      // main members NEVER leave the main pane.
      return ["main"];
    case "bottom":
      // bottom members may visit BOTH sidebars, never the main pane
      return ["left", "right", "bottom"];
    case "primary-sidebar":
    case "secondary-sidebar":
      return ["left", "right", "main", "bottom"];
    default:
      // runtime data outside the union (a stale entry, a fixture) occupies
      // nothing - the safe refusal, never a throw inside a drag
      return [];
  }
}

// `icon` is a semantic NAME in the webui iconset (composables/icons.ts ICONS. The names
// stay PURE strings here: the iconset test pins every registry entry as a key of ICONS,
// and the Icon wrapper throws at render time on a name this registry never declared.

// the honest floors. The chat family keeps the 320px wall (a pane that might show a
// chat must keep chat room); every tool component floors at the shared 128px pane
// minimum - Endo's screenshot proved a 250px tool pane renders fine, so a tool-only tab
// never counts 320 against the "no room" math.
export const MIN_PANE_PX = 128;
export const CHAT_FLOOR_PX = 320;

/** the registry entry. */
// (Title Case for shell tabs.
export interface DockableComponent {
  id: DockComponentName;
  title: string;
  icon: string;
  policy: DockInstancePolicy;
  class: ViewClass;
  /**
   * the view's default home zone - DERIVED from `class` by DOCKABLES (areaOfClass),
   * never declared twice.
   */
  area: DockArea;
  /**
   * the hosts that keep this component alive BEYOND its `area` home.
   * Two instances may coexist across hosts; the singleton "at most once" invariant
   * reads one per host.
   */
  homes?: readonly DockArea[];
  defaultHome?: DockArea;
  closable: boolean;
  /**
   * the component's honest px floor - a pane whose WORST hosted tab is this component
   * may never be starved below it (the reveal's "no room" math and the divider drags
   * read the same floor).
   */
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
    // (Endo's in-app comments batch 2). multi policy (one tab per opened file/skill,
    // like chat), closable like chat; it never appears in the '+' menus (an EMPTY
    // viewer tab is meaningless - content is the tab, there is no empty viewer), so its
    // only entry point is a row click.
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
    // settings is an ON-DEMAND main-area tab. It is NOT a permanent inspector tab
    // anymore; the inspector strip and its boot repair never seat it
    // (retireInspectorSettings drops a legacy seat on load), which is the whole point
    // of the rule.
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
    // the general web browser - a MAIN-pane content tab like chat and the viewer. The
    // panel body is the browser capsule's own harness/browser/ui/BrowserTab.vue over
    // harness/browser's GET/PUT /browser/last (the resume URL); sites that refuse
    // embedding get the tab's fallback panel, never a silent blank frame.
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

// the per-component floors. Expressed as one map so a new registry entry joins the
// floor law in exactly one place - the composed DOCKABLES below attaches it.
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

// THE registry, floors AND homes attached.
export const DOCKABLES: readonly DockableComponent[] = DOCKABLE_INVENTORY.map(
  (d) => ({ ...d, area: areaOfClass(d.class), minPx: DOCKABLE_MIN_PX[d.id] }),
);

/**
 * WHAT a viewer tab shows.
 * The tab's identity for focus-reuse: the same ref focuses the existing tab, never
 * mints a duplicate.
 */
export interface ViewerRef {
  kind: "file" | "skill" | "dir";
  name: string;
}

/** validate a persisted viewer payload (the strict reader's law). */
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

/** a viewer tab's chip title. */
export function viewerTabTitle(v: ViewerRef): string {
  if (v.kind === "skill") return v.name;
  const base = v.name.split("/").pop() ?? "";
  return base !== "" ? base : v.name;
}

/** registry entry for an openable component (panes only host these) */
export function dockableOf(id: DockComponentName): DockableComponent {
  const d = DOCKABLES.find((x) => x.id === id);
  if (!d) throw new Error(`dockable registry: unknown component "${id}"`);
  return d;
}
/** lenient lookup for RUNTIME data (persisted trees can carry pre-reset ids like. */
function safeEntry(id: string): DockableComponent | null {
  if (!(DOCK_COMPONENTS as readonly string[]).includes(id)) return null;
  return dockableOf(id as DockComponentName);
}

/**
 * the component's honest floor, lenient like safeEntry: a tab id outside the registry
 * (persisted legacy ids) counts as a tool tab - 128 keeps the pane usable and never
 * invokes the chat wall for data the registry never declared.
 */
export function dockableMinPx(id: string): number {
  return safeEntry(id)?.minPx ?? MIN_PANE_PX;
}

/**
 * the tab chip's iconset name - lenient like safeEntry: a tab carrying an id outside
 * the openable registry renders no icon, never throws / /
 */
export function dockableIcon(id: string): string | null {
  return safeEntry(id)?.icon ?? null;
}

/**
 * THE LOCKED TAB - sessions is the one component whose chip may never leave the left.
 */
export function isLockedTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d === null ? true : d.id === "sidebar";
}

// THE SEAT MAP (canvas C1) ------------------------------------------------ One.

/** the class a view belongs to (throws on an unknown id. */
export function viewClassOf(id: DockComponentName): ViewClass {
  return dockableOf(id).class;
}

/** lenient class read for RUNTIME data (persisted tabs can carry legacy ids like. */
export function dockableClass(id: string): ViewClass | null {
  return safeEntry(id)?.class ?? null;
}

/** the zones a view may occupy. */
export function zonesOf(id: string): readonly DockArea[] {
  const d = safeEntry(id);
  if (!d) return [];
  return isLockedTab(d.id) ? [d.area] : zonesOfClass(d.class);
}

/** THE ZONE GATE: may this view occupy a pane in `zone`? */
export function seatAllowsIn(id: string, zone: DockArea): boolean {
  return zonesOf(id).includes(zone);
}

/** the members of a class whose HOME is `zone`, in registry order. */
export function classMembers(zone: DockArea): readonly DockComponentName[] {
  return DOCKABLES.filter((d) => d.area === zone).map((d) => d.id);
}

/** tab-bar X policy: closable components only (chat, viewer, browser, settings). */
export function isClosableTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d !== null && d.closable;
}

/** tab-title fallback */
export const COMPONENT_TITLES: Record<DockComponentName, string> =
  Object.fromEntries(DOCKABLES.map((d) => [d.id, d.title])) as Record<
    DockComponentName,
    string
  >;

/** the MAIN-area dockables (openable into the main pane class. */
export const MAIN_DOCKABLES: readonly DockComponentName[] =
  DOCKABLES.filter((d) => d.area === "main").map((d) => d.id);

/**
 * the '+' / empty-pane MENU inventory.
 * Sessions is excluded: locked (the loader owns it, it can never be menu-opened).
 */
export const MENU_DOCKABLES: readonly DockComponentName[] = DOCKABLES
  .filter(
    (d) =>
      d.id !== "sidebar" &&
      d.id !== "settings" &&
      d.id !== "viewer",
  )
  .map((d) => d.id);

/**
 * the secondary sidebar's singleton set (the inspector pane's view tabs).
 * canvas C1: the set IS the class's members, derived (classMembers), so a seat-map edit
 * cannot leave this list behind.
 */
export const INSPECTOR_TAB_COMPONENTS: readonly DockComponentName[] =
  classMembers("right");

/**
 * the primary sidebar's singleton set (canvas C1): sessions and its class companions.
 */
export const PRIMARY_TAB_COMPONENTS: readonly DockComponentName[] =
  classMembers("left");

// the host homes: the registry decides the default home per host.

/** the components a fresh `host` pane seats as its default tabs. */
export function defaultHostTabs(host: DockArea): DockComponentName[] {
  return DOCKABLES
    .filter((d) => (d.defaultHome ?? d.area) === host)
    .map((d) => d.id);
}

/**
 * which hosts may keep an instance of `id` alive.
 */
export function hostsOf(id: DockComponentName): readonly DockArea[] {
  const d = dockableOf(id);
  return d.homes ?? [d.area];
}

// the composition-manifest side (module-owned components) --------------

// What ui/dockable.json may declare.
export interface DockableSpec {
  title?: string;
  icon?: string;
  policy?: DockInstancePolicy;
}

// default icon name for modules that declare no icon of their own.
const MODULE_ICON = "module";

export interface DockableComponentLike {
  id: string;
  title: string;
  icon: string;
  policy: DockInstancePolicy;
}

/** Lifts the composition manifest's dockable metadata into registry-shaped entries. */
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

// THE MODULE GATE -------------------------------------- A dockable is MODULE-OWNED
// when its pane body ships from a module's ui/ dir instead of the shell's: the
// composition manifests already carry that fact (harness/<name>/ui/<File>.vue under
// either modulePanels or moduleTabs), so the owned set is DERIVED from the manifests -
// never a second hardcoded list that could drift from them. Components whose id is NOT
// a module id (chat, viewer, sidebar, agents, insights, settings) can never be gated
// however the runtime reports them: those panes are the shell's own.

/** the two facts the gate needs. */
export interface ModuleGate {
  owned: ReadonlySet<string>;
  off: ReadonlySet<string>;
}

/** THE GATE'S NO-OP (no manifests, no state): nothing is ever hidden */
export const EMPTY_MODULE_GATE: ModuleGate = {
  owned: new Set<string>(),
  off: new Set<string>(),
};

/** Derives the module-owned id set from the COMPOSITION MANIFESTS (modulePanels +. */
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
 * THE ONE GATE PREDICATE: a component is hidden iff a module OWNS it and that module.
 * Both halves are required - a shell-owned pane whose name happens to sit in the off
 * set stays visible (the runtime never owned it), and a module-owned pane whose module
 * runs stays visible.
 */
export function moduleGated(dockableId: string, gate: ModuleGate): boolean {
  return gate.owned.has(dockableId) && gate.off.has(dockableId);
}
