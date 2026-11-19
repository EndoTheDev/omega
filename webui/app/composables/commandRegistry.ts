// omega webui: the command palette's command registry.

import {
  DOCKABLES,
  type DockComponentName,
} from "./dockables.ts";
import { WEBUI_SHELL_COMMANDS, clampIndex } from "./commands.ts";

export interface PaletteCommand {
  /** stable id ("open:browser") - the list key, never shown */
  id: string;
  title: string;
  hint?: string;
  /**
   * The / reference set carries it (a command list is a reference: a name alone does not
   * say what the verb does); never the hint column, which stays keybindings-only.
   */
  description?: string;
  /** extra filter fuel, rescuing odd voice titles like the modules cockpit */
  keywords?: string;
  dimmed?: boolean;
  /** the action; local only - the palette never talks to the agent */
  run: () => void | Promise<void>;
}

/** the deps the app wires: one seam built from app.vue's flows; tests fake it whole */
export interface PaletteCommandDeps {
  openDockable: (id: DockComponentName) => void;
  openChat: () => void;
  closeCurrentTab: () => void;
  toggleBottomPanel: () => void;
  toggleSidebar: (side: "left" | "right") => void;
  openSessionsSidebar: () => void;
  openRecentSession: () => void;
  reloadLayout: () => void;
  clearSavedLayout: () => void;
  hiddenDockable?: (id: DockComponentName) => boolean;
  /** the focused tab IS the draft conversation, so "New chat" renders dimmed, never removed */
  draftFocused?: boolean;
  /** the / reference set from commands.ts (one list, never a copy); absent = a no-op pick */
  runSlashCommand?: (name: string) => void;
}

// the openable dockable set: sidebar and viewer have no meaningful empty instance
const PALETTE_EXCLUDED = new Set<string>(["sidebar", "viewer"]);

export function paletteOpenableIds(): DockComponentName[] {
  return DOCKABLES
    .filter((d) => !PALETTE_EXCLUDED.has(d.id))
    .map((d) => d.id);
}

export function buildPaletteCommands(deps: PaletteCommandDeps): PaletteCommand[] {
  const cmds: PaletteCommand[] = [];
  for (const id of paletteOpenableIds()) {
    const d = DOCKABLES.find((x) => x.id === id);
    if (!d) continue;
    if (deps.hiddenDockable?.(id)) continue;
    if (id === "chat") {
      // multi: "open chat" would duplicate the sidebar's '+' semantics.
      cmds.push({
        id: "open:" + id,
        title: "New chat",
        keywords: "chat",
        dimmed: deps.draftFocused === true,
        run: () => deps.openChat(),
      });
      continue;
    }
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
  // name + description per command, straight from commands.ts (the ONE shell set, so the
  // composer's slash palette and this list can never drift). /help opens this palette, so
  // the list must BE the reference it claims; titles stay the literal tokens.
  for (const c of WEBUI_SHELL_COMMANDS) {
    cmds.push({
      id: "slash:" + c.name.slice(1),
      title: c.name,
      description: c.description,
      keywords: "command slash " + c.description,
      run: () => deps.runSlashCommand?.(c.name),
    });
  }
  return cmds;
}

/** substring filter, case-insensitive over title + id + description + keywords: the
    description is reference fuel, not filler ("clipboard" finds /clipboard only through it) */
export function filterPaletteCommands(
  query: string,
  cmds: readonly PaletteCommand[],
): PaletteCommand[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...cmds];
  return cmds.filter((c) =>
    c.title.toLowerCase().includes(q) ||
    c.id.toLowerCase().includes(q) ||
    (c.description ?? "").toLowerCase().includes(q) ||
    (c.keywords ?? "").toLowerCase().includes(q),
  );
}

// the overlay's keyboard grammar is the composer palette's (clampIndex, commands.ts)
export { clampIndex };