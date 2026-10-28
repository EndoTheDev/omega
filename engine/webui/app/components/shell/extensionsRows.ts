// omega webui: pure row model for the EXTENSIONS inspector tab
// (card t_14f73742 - the webui parity of the TUI /extensions command;
// card t_210a0d19 - the tab becomes the module lifecycle cockpit).
//
// Composition contract note: this inventory tab is a SHELL tab over the
// GET /extensions endpoint, NOT a module-owned panel - the shell owns
// the listing (it aggregates every module, including the modules that
// carry no panel UI). has-ui and the panel list derive at shell build
// time from the composition manifest (modulePanels: a module shipped
// modules/<name>/ui/ with at least one panel component = it has UI);
// the endpoint itself stays source-agnostic for the runtime.
//
// Lifecycle (card t_210a0d19): the row model MERGES three sources into
// one list. GET /extensions covers every MOUNTED module (tools,
// commands, seams, expansion names). GET /modules covers the registry
// introspection (live state, toggleable, dependents) - the toggle
// cockpit's data - and filters the engine ring, so a row the
// introspection does not know is ENGINE ring (http_channel, tui):
// always on by tier law, no toggle. A module the inventory does not
// know is DISABLED (it unmounted, so /extensions never listed it): it
// rides the tail with empty detail. Pure on purpose: no Vue, no fetch
// - the unit suite pins the merge.
//
// One row per module, never two: the merge keys on the module name and
// the inventory (mount order) owns the position; disabled rows append
// in registry order after the mounted ones.

import type { OmegaExtension, ModuleRow } from "../../composables/omega";

// PanelRef is one composition-manifest hit: the owning module and the
// panel's dockable title (the shell builds these from modulePanels; a
// manifest entry without a dockable declaration titles by module name).
export interface PanelRef {
  module: string;
  title: string;
}

export interface ExtensionRow {
  name: string;
  description: string;
  tools: number;
  commands: number;
  // the expansion detail: verbatim tool names (alphabetical, as the
  // registry sorts them) and the module's slash commands in
  // registration order
  toolNames: string[];
  commandNames: string[];
  seams: string[];
  // panel titles this module ships per the composition manifest
  panels: string[];
  // true when the module ships a panel UI (composition manifest hit):
  // the row's action opens the module's dock tab
  hasUi: boolean;
  // lifecycle from the registry introspection. "engine" = engine ring
  // (never in the /modules list; always on by tier law), "mounted" and
  // "disabled" are the live registry states. A host without the toggle
  // seam (501) leaves every row its inventory state with no toggles -
  // the tab renders no checkbox then (the old settings-group fallback).
  state: "mounted" | "disabled" | "engine";
  toggleable: boolean;
  dependents: string[];
}

// extensionsRows merges the endpoint payload, the composition manifest
// and the registry introspection into renderable rows: mounted rows in
// payload order (mount order - the same facts the TUI renders), then
// the introspection-only rows (disabled modules) in registry order.
export function extensionsRows(
  extensions: readonly OmegaExtension[],
  panels: readonly PanelRef[],
  modules: readonly ModuleRow[],
): ExtensionRow[] {
  const panelTitles = new Map<string, string[]>();
  for (const p of panels) {
    const list = panelTitles.get(p.module) ?? [];
    list.push(p.title);
    panelTitles.set(p.module, list);
  }
  // registry introspection keyed by name; consumed entries delete so
  // the leftovers are exactly the disabled (inventory-absent) modules.
  const introspection = new Map(modules.map((m) => [m.name, m]));

  const rows: ExtensionRow[] = extensions.map((e) => {
    const m = introspection.get(e.name);
    introspection.delete(e.name);
    return {
      name: e.name,
      description: e.description ?? "",
      tools: e.tools,
      commands: e.commands,
      toolNames: e.tool_names ?? [],
      commandNames: e.command_names ?? [],
      seams: e.seams ?? [],
      panels: panelTitles.get(e.name) ?? [],
      hasUi: panelTitles.has(e.name),
      state: m ? m.state : "engine",
      toggleable: m?.toggleable ?? false,
      dependents: m?.dependents ?? [],
    };
  });

  // the mounted inventory never lists disabled modules (they unmounted):
  // the introspection leftovers ride the tail - still toggleable, with
  // no tool or command detail to expand (nothing is mounted to expand).
  for (const m of introspection.values()) {
    rows.push({
      name: m.name,
      description: m.description ?? "",
      tools: 0,
      commands: 0,
      toolNames: [],
      commandNames: [],
      seams: [],
      panels: panelTitles.get(m.name) ?? [],
      hasUi: panelTitles.has(m.name),
      state: m.state === "disabled" ? "disabled" : "mounted",
      toggleable: m.toggleable,
      dependents: m.dependents ?? [],
    });
  }
  return rows;
}

// extensionsSummary is the tab's header line: N modules, T tools,
// C commands across the load. Disabled rows contribute zero tools and
// commands (nothing mounted to inventory), so the counts stay honest.
// Zero modules reads "no extensions loaded" upstream (the tab's honest
// empty state).
export function extensionsSummary(rows: readonly ExtensionRow[]): string {
  const tools = rows.reduce((n, r) => n + r.tools, 0);
  const commands = rows.reduce((n, r) => n + r.commands, 0);
  return `${rows.length} module${rows.length === 1 ? "" : "s"} - ${tools} tool${tools === 1 ? "" : "s"}, ${commands} command${commands === 1 ? "" : "s"}`;
}