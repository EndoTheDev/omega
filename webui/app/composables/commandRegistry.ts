// omega webui: the command palette's command registry (card t_6215474b).
// A PLAIN array - the settled ruling says no extensibility framework, no
// plugin machinery: the app builds the list from the dockable inventory
// plus the shell-action set, the palette filters it. The composer's
// slash palette (commands.ts, card t_07aed3f9) is the older sibling: same
// substring-filter + ArrowUp/ArrowDown/Enter/Esc grammar, nothing shared
// with the agent - every command is a LOCAL action via the deps the app
// passes in.
//
// PURE: build + filter are node-tested; the deps interface is the one
// seam the app wires.

import {
  DOCKABLES,
  type DockComponentName,
} from "./dockables.ts";
import { clampIndex } from "./commands.ts";

export interface PaletteCommand {
  /** stable id ("open:browser") - the list key, never shown */
  id: string;
  /** the row's display voice (Title Case, the registry law) */
  title: string;
  /** the keybinding-hint column; absent = no binding (the keybindings
      card t_d9eebf08 owns the real ones - never fake a hint here) */
  hint?: string;
  /** extra filter fuel ("browser" finds "open Browser" the same either
      way; keywords rescue odd voice titles like the modules cockpit) */
  keywords?: string;
  /** the action. Local only - the palette never talks to the agent. */
  run: () => void | Promise<void>;
}

/** the deps the app wires. One seam, built from app.vue's own flows -
    the registry never imports app state, so tests fake it whole. */
export interface PaletteCommandDeps {
  /** open (or focus) any dockable by id - the same openers the menus
      use (openDockable in app.vue) */
  openDockable: (id: DockComponentName) => void;
  /** the chat dockable is MULTI - "open chat" means a fresh chat tab */
  openChat: () => void;
  closeCurrentTab: () => void;
  toggleBottomPanel: () => void;
  toggleSidebar: (side: "left" | "right") => void;
  openSessionsSidebar: () => void;
  openRecentSession: () => void;
  reloadLayout: () => void;
  clearSavedLayout: () => void;
}

// the openable dockable set: sidebar is the locked tab (the toggle owns
// it) and the viewer is content-only (an empty viewer tab is meaningless
// - the t_36ceb99b ruling); everything else is palette-openable incl.
// settings (the + menu's exclusion never applied to the palette - the
// card lists it explicitly).
const PALETTE_EXCLUDED = new Set<string>(["sidebar", "viewer"]);

/** the dockable ids the palette derives "open <title>" commands from */
export function paletteOpenableIds(): DockComponentName[] {
  return DOCKABLES
    .filter((d) => !PALETTE_EXCLUDED.has(d.id))
    .map((d) => d.id);
}

/** build the v1 command set. Order: the dockables (registry voice -
    "open Browser"), then the shell actions. The registry voice keeps
    the chat dockable out of the open-loop (multi - "New chat" is its
    command) but the chat ENTRY still ships, so every dockable is
    reachable. */
export function buildPaletteCommands(deps: PaletteCommandDeps): PaletteCommand[] {
  const cmds: PaletteCommand[] = [];
  for (const id of paletteOpenableIds()) {
    const d = DOCKABLES.find((x) => x.id === id);
    if (!d) continue;
    if (id === "chat") {
      // multi: "open chat" would duplicate the sidebar's '+' semantics -
      // the command is the fresh tab, labeled honestly
      cmds.push({
        id: "open:" + id,
        title: "New chat",
        keywords: "chat",
        run: () => deps.openChat(),
      });
      continue;
    }
    // the iconset name rides the keyword fuel too: it is the concept word
    // the voice may not carry (the logs strip is "http_channel" by id - a
    // user typing "logs" still finds it)
    cmds.push({
      id: "open:" + id,
      title: "Open " + d.title,
      keywords: id + " " + d.icon,
      run: () => deps.openDockable(id),
    });
  }
  cmds.push(
    {
      id: "tab:close",
      title: "Close current tab",
      run: () => deps.closeCurrentTab(),
    },
    {
      id: "panel:toggle-bottom",
      title: "Toggle bottom panel",
      run: () => deps.toggleBottomPanel(),
    },
    {
      id: "sidebar:toggle-left",
      title: "Toggle left sidebar",
      run: () => deps.toggleSidebar("left"),
    },
    {
      id: "sidebar:toggle-right",
      title: "Toggle right sidebar",
      run: () => deps.toggleSidebar("right"),
    },
    {
      id: "sessions:recent",
      title: "Switch to recent session",
      run: () => deps.openRecentSession(),
    },
    {
      id: "layout:reload",
      title: "Reload layout",
      run: () => deps.reloadLayout(),
    },
    {
      id: "layout:clear",
      title: "Clear saved layout",
      run: () => deps.clearSavedLayout(),
    },
  );
  return cmds;
}

/** substring filter (the settled lazy default; subsequence later if it
    ever annoys): case-insensitive over title + id + keywords. */
export function filterPaletteCommands(
  query: string,
  cmds: readonly PaletteCommand[],
): PaletteCommand[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...cmds];
  return cmds.filter((c) =>
    c.title.toLowerCase().includes(q) ||
    c.id.toLowerCase().includes(q) ||
    (c.keywords ?? "").toLowerCase().includes(q),
  );
}

// the overlay's keyboard grammar is the composer palette's (clampIndex,
// commands.ts): ArrowUp/ArrowDown cycle, Enter runs, Esc closes. The
// component binds it; this re-export keeps one arithmetic.
export { clampIndex };