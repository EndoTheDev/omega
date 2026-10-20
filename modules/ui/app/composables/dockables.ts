// omega webui: THE DOCKABLE CONTRACT (card t_30ee04fb). One registry for
// everything a dock pane can host: id, title, icon, and the INSTANCE
// POLICY. "singleton" components exist at most once - opening one while
// an instance is alive FOCUSES that instance instead of creating a
// second; "multi" components (chat) mint a real tab every open. Global
// chrome (MenuBar, StatusBar) is NOT in this registry: they are the
// window, not content - never dockable.
//
// PURE on purpose: no Vue, no fs. The engine (dock.ts) re-exports the
// component universe from here, the menus render the inventory, and the
// unit suite pins every contract rule.
//
// Module-owned components join through the composition manifest
// (t_d984b493): a module may ship ui/dockable.json beside its panels -
// the same declaration shape, read at build time by compose.ts and
// carried on the generated panel list. dockablesFromPanels() lifts
// those into registry-shaped entries; module entries stay OUT of the
// openable DockComponentName union until an outlet slot exists.

// component names a dock pane may host. Single source: the registry
// below IS this list (dock.ts re-exports for engine-side consumers).
//
// card t_4ae27e0d (the pane-model reset): "inspector" is no longer a
// component - the right sidebar's views are REAL dock tabs living in one
// sidebar-pinned pane. The module ids mirror the shipped module names
// verbatim (cron / http_channel / skills / tools / kanban) so the app's
// outlet templates and the composition lookup agree without an alias map.
// card t_49d0b308: settings left the main-pane tab class for the
// inspector, and the kanban board joined it as the seventh tab.
export const DOCK_COMPONENTS = [
  "sidebar",
  "chat",
  "files",
  "cron",
  "http_channel",
  "skills",
  "tools",
  "settings",
  "kanban",
] as const;
export type DockComponentName = (typeof DOCK_COMPONENTS)[number];

// pre-reset trees persist tabs {component: "inspector"}; deserialize
// still accepts the legacy id (createDock modernizes it to the inspector
// group on load) but nothing may open one again.
export const LEGACY_DOCK_COMPONENTS = ["inspector"] as const;

export type DockInstancePolicy = "singleton" | "multi";

// where a component may live (card t_4ae27e0d): "left" is the
// sidebar-pinned sessions pane, "right" the sidebar-pinned inspector
// pane, "main" the closable main pane class (hover-X).
export type DockArea = "left" | "right" | "main";

// icon glyphs render at 14px in a 24x24 viewBox, currentColor stroke,
// 2px round-cap joins (the shell's MenuBar icon scale). rect is the
// panel box most panel icons share; paths are the interior strokes.
export interface DockIcon {
  rect?: { x: number; y: number; width: number; height: number; rx?: number };
  paths: string[];
}

// one registry entry. `title` is the tab/menu voice (lowercase, like
// the shell's tab bars); the open flows add their own verb (chat's
// multi policy reads "new chat" in the menus). Module-tab titles are
// resolved at runtime from each module's ui/dockable.json (the app's
// DOCK_TAB_TITLE resolver) - the registry title is only the fallback.
export interface DockableComponent {
  id: DockComponentName;
  title: string;
  icon: DockIcon;
  policy: DockInstancePolicy;
  area: DockArea;
  /** tab shows a hover-X. Pinned components (left/right) are unclosable:
      their pane hosts them forever; main tabs close (hover-X). */
  closable: boolean;
}

// THE pinned identity (card t_4ae27e0d): sidebar = the sessions pane
// (singleton, pinned left, unclosable); the inspector group = sidebar-
// pinned right with every view tab unclosable. card t_49d0b308: chat is
// the only closable component left - settings moved into the inspector
// group and the kanban board is born there.
export const DOCKABLES: readonly DockableComponent[] = [
  {
    id: "sidebar",
    title: "sessions",
    icon: {
      rect: { x: 3, y: 3, width: 18, height: 18, rx: 2 },
      paths: ["M9 3v18", "M13 9l3 3-3 3"],
    },
    policy: "singleton",
    area: "left",
    closable: false,
  },
  {
    id: "chat",
    title: "chat",
    icon: { paths: ["M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"] },
    policy: "multi",
    area: "main",
    closable: true,
  },
  {
    id: "files",
    title: "Files",
    icon: {
      rect: { x: 3, y: 3, width: 18, height: 18, rx: 2 },
      paths: ["M15 3v18", "M8 9l3 3-3 3"],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "cron",
    title: "cron",
    icon: {
      paths: [
        "M10.5 10.5a2 2 0 1 0 3 0a2 2 0 1 0 -3 0",
        "M12 3v3m0 12v3M3 12h3m12 0h3",
      ],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "http_channel",
    title: "http_channel",
    icon: {
      rect: { x: 3, y: 5, width: 18, height: 14, rx: 2 },
      paths: ["M7 9h10", "M7 13h10", "M7 16h4"],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "skills",
    title: "skills",
    icon: {
      rect: { x: 2, y: 2, width: 20, height: 20, rx: 2 },
      paths: ["M12 6l6 6-6 6-6-6z"],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "tools",
    title: "tools",
    icon: {
      rect: { x: 2, y: 3, width: 20, height: 18, rx: 2 },
      paths: ["M6 9h12", "M9 13h6"],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "settings",
    title: "Settings",
    icon: {
      // cog: hub circle + eight teeth, same 2px round-join stroke style
      paths: [
        "M12 9a3 3 0 1 0 .0001 0",
        "M12 2v3", "M12 19v3", "M2 12h3", "M19 12h3",
        "M4.9 4.9L7 7", "M17 17l2.1 2.1", "M19.1 4.9L17 7", "M7 17l-2.1 2.1",
      ],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "kanban",
    title: "kanban",
    // the board: three columns in one frame
    icon: {
      rect: { x: 3, y: 3, width: 18, height: 18, rx: 2 },
      paths: ["M9 3v18", "M15 3v18"],
    },
    policy: "singleton",
    area: "right",
    closable: false,
  },
];

/** registry entry for an openable component (panes only host these) */
export function dockableOf(id: DockComponentName): DockableComponent {
  const d = DOCKABLES.find((x) => x.id === id);
  if (!d) throw new Error(`dockable registry: unknown component "${id}"`);
  return d;
}
/** lenient lookup for RUNTIME data (persisted trees can carry pre-reset
    ids like "inspector" until the modernizer runs): unknown ids get the
    safe identity - pinned + unclosable, so no engine op manipulates them */
function safeEntry(id: string): DockableComponent | null {
  if (!(DOCK_COMPONENTS as readonly string[]).includes(id)) return null;
  return dockableOf(id as DockComponentName);
}

/** card t_4ae27e0d: the sidebar-pinned class - a component whose tab may
    never close and whose pane may never be removed. left/right panels.
    Unknown ids count as pinned (the safe refusal). */
export function isPinnedTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d === null ? true : d.area !== "main";
}

/** tab-bar X policy: closable components only (chat, settings). Unknown
    ids get no X. */
export function isClosableTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d !== null && d.closable;
}

/** tab-title fallback (card 2's COMPONENT_TITLES, now derived from the
    registry - the chat session label and the module dockable.json titles
    override it at runtime) */
export const COMPONENT_TITLES: Record<DockComponentName, string> =
  Object.fromEntries(DOCKABLES.map((d) => [d.id, d.title])) as Record<
    DockComponentName,
    string
  >;

/** card t_4ae27e0d: the tab-bar '+' / empty-pane menu inventory - the
    MAIN-area dockables only. The pinned sidebars are fixed chrome (they
    live because the shell puts them there, not because a menu opened
    them), so no menu may mint them in an arbitrary pane. */
export const MAIN_DOCKABLES: readonly DockComponentName[] =
  DOCKABLES.filter((d) => d.area === "main").map((d) => d.id);

/** the pinned-area singleton set (the inspector pane's view tabs): the
    load-time modernizer and the default-shell builder both read this
    list, so a new inspector view joins it in ONE place. */
export const INSPECTOR_TAB_COMPONENTS: readonly DockComponentName[] =
  DOCKABLES.filter((d) => d.area === "right").map((d) => d.id);

// --- the composition-manifest side (module-owned components) -----------------

// What ui/dockable.json may declare. Same shape as DockableComponent,
// minus the id (the module name is the id) and with everything optional
// (registry defaults fill it in). compose.ts validates + reads it at
// build time; the generated panel list carries it to the runtime.
export interface DockableSpec {
  title?: string;
  icon?: DockIcon;
  policy?: DockInstancePolicy;
}

// default glyph for modules that declare no icon of their own
const MODULE_ICON: DockIcon = {
  paths: ["M12 3v3m0 12v3M3 12h3m12 0h3", "M10.5 10.5a2 2 0 1 0 3 0a2 2 0 1 0 -3 0"],
};

export interface DockableComponentLike {
  // module name - NOT a DockComponentName: module dockables gain the
  // openable union when their outlet slot ships
  id: string;
  title: string;
  icon: DockIcon;
  policy: DockInstancePolicy;
}

/**
 * Lifts the composition manifest's dockable metadata into
 * registry-shaped entries. Panels without a declaration (and a null
 * spec) are just not dockable yet; ids colliding with the shell
 * inventory are dropped - a module named like a shell component may
 * not shadow it. Module entries are metadata-only until their outlet
 * exists, so nothing here touches the openable component union.
 */
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