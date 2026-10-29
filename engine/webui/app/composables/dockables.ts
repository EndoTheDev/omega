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
  "viewer",
  "files",
  "cron",
  "http_channel",
  "skills",
  "tools",
  "agents",
  "settings",
  "kanban",
  "extensions",
  "insights",
  "mcp",
  // card t_c7222095 (deletability law): "continuous" left the dock
  // registry with its tab - the roster folded into the Agents tab
  // (one agent = one row: config + wake state + report preview). A
  // persisted pre-merge layout that still carries a continuous chip
  // drops it with the usual repair note, like "canvas" above.
] as const;
export type DockComponentName = (typeof DOCK_COMPONENTS)[number];

// pre-reset trees persist tabs {component: "inspector"}; deserialize
// still accepts the legacy id (createDock modernizes it to the inspector
// group on load) but nothing may open one again.
export const LEGACY_DOCK_COMPONENTS = ["inspector"] as const;

export type DockInstancePolicy = "singleton" | "multi";

// where a component may live (card t_4ae27e0d): "left" is the
// sidebar-pinned sessions pane, "right" the sidebar-pinned inspector
// pane, "main" the closable main pane class (hover-X). card t_67fc1905:
// "bottom" joins the pinned class - the horizontal panel under the main
// panel (logs home today, the terminal's future tab host).
export type DockArea = "left" | "right" | "main" | "bottom";

// card t_502b8e81: `icon` is a semantic NAME in the webui iconset
// (composables/icons.ts ICONS - lucide-vue-next behind the Icon.vue
// wrapper). The names stay PURE strings here: the iconset test pins every
// registry entry as a key of ICONS, and the Icon wrapper throws at render
// time on a name this registry never declared.

// one registry entry. `title` is the display voice for the tab/menu chip
// (Title Case for shell tabs - Files, Agents, Extensions, Insights - the
// module dockable.json titles follow the same voice); the open flows add
// their own verb (chat's multi policy reads "new chat" in the menus).
// Module-tab titles are resolved at runtime from each module's
// ui/dockable.json (the app's DOCK_TAB_TITLE resolver) - the registry
// title is only the fallback. card t_556ee727: identifiers, routes and
// file paths stay lowercase everywhere - the casing lives in this label
// alone.
export interface DockableComponent {
  id: DockComponentName;
  title: string;
  /** the iconset name - see the iconset comment above the registry */
  icon: string;
  policy: DockInstancePolicy;
  area: DockArea;
  /** card t_67fc1905: the hosts that keep this component alive BEYOND its
      `area` home. Most components are single-host ([area] by omission);
      the machinery stays for a future multi-home domain (the terminal
      tab). Logs was the first dual-host domain (right + bottom) until
      its sidebar copy was ruled a duplicate and removed (Endo, 2026-10-26
      - logs is bottom-only now). Two instances may coexist across hosts;
      the singleton "at most once" invariant reads one per host. */
  homes?: readonly DockArea[];
  /** card t_67fc1905: where a FRESH built host seats this component -
      its default home. null/omitted = `area`. The bottom host's builder
      reads this: every component with defaultHome "bottom" joins the
      fresh bottom panel's tab strip (logs today, one place to grow). */
  defaultHome?: DockArea;
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
    title: "Sessions",
    icon: "sidebar",
    policy: "singleton",
    area: "left",
    closable: false,
  },
  {
    id: "chat",
    title: "chat",
    icon: "chat",
    policy: "multi",
    area: "main",
    closable: true,
  },
  {
    // card t_36ceb99b (Endo's in-app comments batch 2): the content
    // viewer - clicking a SKILL row or a FILE tree row opens a main-pane
    // tab hosting the content. multi policy (one tab per opened
    // file/skill, like chat), closable like chat; it never appears in
    // the '+' menus (an EMPTY viewer tab is meaningless - content is the
    // tab, there is no empty viewer), so its only entry point is a row
    // click. The per-tab payload (ViewerRef: file path or skill name)
    // rides the tab; the tab chip's icon follows the CONTENT type
    // (DockPane's tabIcon reads it) - file rows keep the extension
    // glyphs, skill rows the skills book. Deletability: remove the
    // viewer registry entry + ViewerTab.vue + the small openers and the
    // feature is gone (no vocabulary left elsewhere).
    id: "viewer",
    title: "viewer",
    icon: "file-text",
    policy: "multi",
    area: "main",
    closable: true,
  },
  {
    id: "files",
    title: "Files",
    icon: "files",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "cron",
    title: "cron",
    icon: "cron",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "http_channel",
    title: "http_channel",
    icon: "logs",
    policy: "singleton",
    // Endo's comment (2026-10-26): logs' home is the BOTTOM panel only -
    // the secondary-sidebar copy duplicated it. The dual-host shape was
    // t_67fc1905's transition; rehomeLogsTab (dock.ts) lifts persisted
    // right-shell copies on load.
    area: "bottom",
    closable: false,
  },
  {
    id: "skills",
    title: "skills",
    icon: "skills",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "tools",
    title: "tools",
    icon: "tools",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    // card t_c7222095: the agents panel - now the MERGED agent surface
    // (config roster + the continuous wake state). Card t_72ecca67's
    // "continuous" dockable FOLDED in here per the deletability law: one
    // agent is one row - the config facts, the runtime badge, the report
    // preview and the wake/sleep strip live in the same tab.
    id: "agents",
    title: "Agents",
    icon: "agents",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "settings",
    title: "Settings",
    icon: "settings",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    id: "kanban",
    title: "kanban",
    icon: "kanban",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    // card t_14f73742: the extensions inventory joins the inspector - the
    // webui parity of the TUI /extensions command. A SHELL tab over
    // GET /extensions (the composition-contract ruling is recorded in
    // extensionsRows.ts): the shell owns the listing because it
    // aggregates every module, so no module can own this panel. has-ui
    // rows emit open-module - the tab only routes, the module panels
    // mount. Unclosable like every inspector view: the loader restores
    // the full strip, and the outlet template must always exist.
    id: "extensions",
    // card t_210a0d19 rename law: the tab label is "Modules" (the
    // lifecycle cockpit); the dock id stays "extensions" - stored dock
    // layouts persist ids, and a rename there would orphan them. The
    // /modules palette name dispatches here; /extensions keeps working.
    title: "Modules",
    icon: "extensions",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    // card t_d7fd3ad3 (Endo's eyeball round 2): insights' real home is the
    // SECONDARY sidebar - a default-visible inspector tab, label "Insights"
    // (the kanban inspector tab is the pattern). This supersedes the
    // main-area width-pressure ruling (t_e1e61680): the dashboard's grid
    // stacks in the ~300px column, and the loader (restoreInspectorSet)
    // seats it in the right shell on every boot. Singleton + unclosable
    // like every inspector view; the main '+' menu entry stays (the
    // singleton opener FOCUSES the sidebar instance - clean coexistence).
    id: "insights",
    title: "Insights",
    icon: "insights",
    policy: "singleton",
    area: "right",
    closable: false,
  },
  {
    // card t_c7136c50: the MCP tab - the configured MCP servers as the
    // bridge sees them (name, transport, mount-time connection state,
    // per-server failure, discovered server-qualified tools), listed
    // from GET /mcp/servers. The panel body is the module's own
    // (modules/mcp/ui, the composition mount); the registry entry only
    // opens the inspector tab - no mcp vocabulary beyond that. Read-
    // only: enable/disable stays config.yaml domain. Unclosable like
    // every inspector view.
    id: "mcp",
    title: "MCP",
    icon: "mcp",
    policy: "singleton",
    area: "right",
    closable: false,
  },
];

/** card t_36ceb99b: WHAT a viewer tab shows. `kind` "file" reads the
    name as a working-directory relative path (GET /files/read), "skill"
    as a skill folder name (GET /skills/content). The tab's identity for
    focus-reuse: the same ref focuses the existing tab, never mints a
    duplicate. PURE plain data - the dock engine serializes it with the
    tree. */
export interface ViewerRef {
  kind: "file" | "skill";
  /** file: a relative path from the workspace root; skill: the folder name */
  name: string;
}

/** validate a persisted viewer payload (the strict reader's law): kind
    one of the two, name a non-empty string; anything else is invalid */
export function validViewerRef(
  raw: unknown,
): raw is ViewerRef {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return false;
  const v = raw as { kind?: unknown; name?: unknown };
  return (
    (v.kind === "file" || v.kind === "skill") &&
    typeof v.name === "string" &&
    v.name !== ""
  );
}

/** a viewer tab's chip title: the base name (a file's last path
    segment, VS Code's read; a skill's name has no segments). Pure. */
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
/** lenient lookup for RUNTIME data (persisted trees can carry pre-reset
    ids like "inspector" until the modernizer runs): unknown ids get the
    safe identity - pinned + unclosable, so no engine op manipulates them */
function safeEntry(id: string): DockableComponent | null {
  if (!(DOCK_COMPONENTS as readonly string[]).includes(id)) return null;
  return dockableOf(id as DockComponentName);
}

/** the tab chip's iconset name - lenient like safeEntry: a tab carrying
    an id outside the openable registry renders no icon, never throws */
export function dockableIcon(id: string): string | null {
  return safeEntry(id)?.icon ?? null;
}

/** card t_d6b657a7: THE LOCKED TAB - sessions is the one component whose
    chip may never leave the left sidebar's own bar (draggable inside it,
    refused everywhere else). Everything else moves freely now: the
    sidebars are pinned LOCATIONS with free CONTENT. Unknown ids count as
    locked (the safe refusal). */
export function isLockedTab(id: DockComponentName): boolean {
  const d = safeEntry(id);
  return d === null ? true : d.id === "sidebar";
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

/** card t_4ae27e0d: the MAIN-area dockables (openable into the main pane
    class; chat's multi policy reads "new chat"). Kept for engine-side
    consumers - the MENUS use MENU_DOCKABLES below since t_d6b657a7. */
export const MAIN_DOCKABLES: readonly DockComponentName[] =
  DOCKABLES.filter((d) => d.area === "main").map((d) => d.id);

/** card t_d6b657a7: the '+' / empty-pane MENU inventory - the refill
    source for ANY pane, pinned or main (free content). Sessions is
    excluded: locked (the loader owns it, it can never be menu-opened).
    Settings is excluded by ruling - it has NO entry point; persistence
    and the default arrangement fallback are its only paths. */
export const MENU_DOCKABLES: readonly DockComponentName[] = DOCKABLES
  .filter(
    // viewer joins the exclusions (t_36ceb99b): an empty viewer tab is
    // meaningless - content IS the tab; its only entry point is a row
    // click handing the content over
    (d) => d.id !== "sidebar" && d.id !== "settings" && d.id !== "viewer",
  )
  .map((d) => d.id);

/** the pinned-area singleton set (the inspector pane's view tabs): the
    load-time modernizer and the default-shell builder both read this
    list, so a new inspector view joins it in ONE place. */
export const INSPECTOR_TAB_COMPONENTS: readonly DockComponentName[] =
  DOCKABLES.filter((d) => d.area === "right").map((d) => d.id);

// --- the host homes (card t_67fc1905): the registry decides the default
// home per host. A built host (the bottom panel today) seeds its fresh
// tab strip from every component whose defaultHome names it. ----

/** the components a fresh `host` pane seats as its default tabs.
    "bottom" today: logs (http_channel) - the terminal joins here later
    by registering the same way. Registered in ONE place (the registry). */
export function defaultHostTabs(host: DockArea): DockComponentName[] {
  return DOCKABLES
    .filter((d) => (d.defaultHome ?? d.area) === host)
    .map((d) => d.id);
}

/** which hosts may keep an instance of `id` alive (the singleton
    invariant now reads ONE PER HOST for dual-host domains like logs) */
export function hostsOf(id: DockComponentName): readonly DockArea[] {
  const d = dockableOf(id);
  return d.homes ?? [d.area];
}

// --- the composition-manifest side (module-owned components) -----------------

// What ui/dockable.json may declare. Same shape as DockableComponent,
// minus the id (the module name is the id) and with everything optional
// (registry defaults fill it in). compose.ts validates + reads it at
// build time; the generated panel list carries it to the runtime.
export interface DockableSpec {
  title?: string;
  /** the iconset name (composables/icons.ts ICONS); omitted = "module" */
  icon?: string;
  policy?: DockInstancePolicy;
}

// default icon name for modules that declare no icon of their own
// (the iconset "module" entry - Box)
const MODULE_ICON = "module";

export interface DockableComponentLike {
  // module name - NOT a DockComponentName: module dockables gain the
  // openable union when their outlet slot ships
  id: string;
  title: string;
  icon: string;
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