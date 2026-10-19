// omega webui: THE DOCKABLE CONTRACT (card t_30ee04fb). One registry for
// everything a dock pane can host: id, title, icon, and the INSTANCE
// POLICY. "singleton" components (sessions sidebar, files inspector)
// exist at most once - opening one while an instance is alive FOCUSES
// that instance instead of creating a second; "multi" components (chat)
// mint a real tab every open. Global chrome (MenuBar, StatusBar) is NOT
// in this registry: they are the window, not content - never dockable.
//
// PURE on purpose: no Vue, no fs. The engine (dock.ts) re-exports the
// component universe from here, the menus render the inventory, and the
// unit suite pins every contract rule.
//
// Module-owned components join through the composition manifest
// (t_d984b493): a module may ship ui/dockable.json beside its panels -
// the same declaration shape, read at build time by compose.ts and
// carried on the generated panel list. dockablesFromPanels() lifts
// those into registry-shaped entries; the board's dock-native mount
// (card 5) is the first consumer, so module entries stay OUT of the
// openable DockComponentName union until an outlet slot exists - a menu
// entry without an outlet renders an empty pane, and this card's menu
// lists the migrated inventory only.

// component names a dock pane may host. Single source: the registry
// below IS this list (dock.ts re-exports for engine-side consumers).
export const DOCK_COMPONENTS = ["sidebar", "chat", "inspector", "settings"] as const;
export type DockComponentName = (typeof DOCK_COMPONENTS)[number];

export type DockInstancePolicy = "singleton" | "multi";

// icon glyphs render at 14px in a 24x24 viewBox, currentColor stroke,
// 2px round-cap joins (the shell's MenuBar icon scale). rect is the
// panel box most panel icons share; paths are the interior strokes.
export interface DockIcon {
  rect?: { x: number; y: number; width: number; height: number; rx?: number };
  paths: string[];
}

// one registry entry. `title` is the tab/menu voice (lowercase, like
// the shell's tab bars); the open flows add their own verb (chat's
// multi policy reads "new chat" in the menus).
export interface DockableComponent {
  id: DockComponentName;
  title: string;
  icon: DockIcon;
  policy: DockInstancePolicy;
}

// THE migrated inventory. Card 1 built the engine, card 2 the tabs/DnD,
// card 3 chat multi-instance, card 4 the contract for sidebar+inspector;
// card 5 (t_35b5594a) closes the list: settings swaps the overlay drawer
// for a dock-tab singleton - the menubar gear OPENS the tab (or FOCUSES
// the live instance per the singleton policy).
export const DOCKABLES: readonly DockableComponent[] = [
  {
    id: "sidebar",
    title: "sidebar",
    icon: {
      rect: { x: 3, y: 3, width: 18, height: 18, rx: 2 },
      paths: ["M9 3v18", "M13 9l3 3-3 3"],
    },
    policy: "singleton",
  },
  {
    id: "chat",
    title: "chat",
    icon: { paths: ["M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"] },
    policy: "multi",
  },
  {
    id: "inspector",
    title: "inspector",
    icon: {
      rect: { x: 3, y: 3, width: 18, height: 18, rx: 2 },
      paths: ["M15 3v18", "M8 9l3 3-3 3"],
    },
    policy: "singleton",
  },
  {
    id: "settings",
    title: "settings",
    icon: {
      // cog: hub circle + eight teeth, same 2px round-join stroke style
      paths: [
        "M12 9a3 3 0 1 0 .0001 0",
        "M12 2v3", "M12 19v3", "M2 12h3", "M19 12h3",
        "M4.9 4.9L7 7", "M17 17l2.1 2.1", "M19.1 4.9L17 7", "M7 17l-2.1 2.1",
      ],
    },
    policy: "singleton",
  },
];

/** registry entry for an openable component (panes only host these) */
export function dockableOf(id: DockComponentName): DockableComponent {
  const d = DOCKABLES.find((x) => x.id === id);
  if (!d) throw new Error(`dockable registry: unknown component "${id}"`);
  return d;
}

/** tab-title fallback (card 2's COMPONENT_TITLES, now derived from the
    registry - the chat session label overrides it at runtime) */
export const COMPONENT_TITLES: Record<DockComponentName, string> =
  Object.fromEntries(DOCKABLES.map((d) => [d.id, d.title])) as Record<
    DockComponentName,
    string
  >;

/** the tab-bar '+' menu inventory: the whole (openable) registry. Card 2
    shipped chat only; card 4's contract opens the full migrated list. */
export const PLUS_TAB_COMPONENTS: readonly DockComponentName[] =
  DOCKABLES.map((d) => d.id);

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
  // openable union when their outlet slot ships (card 5)
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