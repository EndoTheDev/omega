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
      title. The / reference set carries it (a command list is a reference:
      a name alone does not say what the verb does) - never the hint column,
      which stays keybindings-only. */
  description?: string;
  /** extra filter fuel ("browser" finds "open Browser" the same either
      way; keywords rescue odd voice titles like the modules cockpit) */
  keywords?: string;
  dimmed?: boolean;
  /**
   * the action.
   * Local only - the palette never talks to the agent.
   */
  run: () => void | Promise<void>;
}

/**
 * the deps the app wires.
 * One seam, built from app.vue's own flows - the registry never imports app state, so
 * tests fake it whole.
 */
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
  /**
   * the focused tab IS the draft conversation, so "New chat" would only focus it again.
   * The row renders dimmed + aria-disabled (never removed) and the action stays the
   * honest one.
   */
  draftFocused?: boolean;
  /**
      command REFERENCE (name + description per command, from commands.ts -
      one list, never a copy), and a picked row runs that verb against the
      active chat tab. Absent = the rows render but picking one is a no-op
      (the composer's own slash palette is still the executor). */
  runSlashCommand?: (name: string) => void;
}

// the openable dockable set.
const PALETTE_EXCLUDED = new Set<string>(["sidebar", "viewer"]);

/** the dockable ids the palette derives "open <title>" commands from */
export function paletteOpenableIds(): DockComponentName[] {
  return DOCKABLES
    .filter((d) => !PALETTE_EXCLUDED.has(d.id))
    .map((d) => d.id);
}

/** build the v1 command set. */
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
  //
  // name + description per command, straight from commands.ts (the ONE
  // shell set: the composer's slash palette and this list can never
  // drift). /help opens this palette, so the list it opens must BE the
  // reference it claims to be. Titles stay the literal tokens (never
  // Title Case): a slash verb is a thing you type, not a phrase.
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

/** substring filter (the settled lazy default; subsequence later if it
    ever annoys): case-insensitive over title + id + description +
    keywords. The description is reference fuel, not filler - "theme" finds
    /theme either way, "clipboard" finds it only through the description. */
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

// the overlay's keyboard grammar is the composer palette's (clampIndex, commands.ts).
export { clampIndex };