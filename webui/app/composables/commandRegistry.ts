// omega webui: the command palette's command registry.

import {
  DOCKABLES,
  type DockComponentName,
} from "./dockables.ts";
import { clampIndex } from "./commands.ts";

export interface PaletteCommand {
  /** stable id ("open:browser") - the list key, never shown */
  id: string;
  title: string;
  hint?: string;
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
    // the iconset name rides the keyword fuel too.
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

/** substring filter (the settled lazy default; subsequence later if it ever annoys). */
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

// the overlay's keyboard grammar is the composer palette's (clampIndex, commands.ts).
export { clampIndex };