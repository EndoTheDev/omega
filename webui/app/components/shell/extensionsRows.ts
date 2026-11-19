// omega webui: pure row model for the MODULES surface. Composition contract note: this
// listing is a SHELL-owned surface over the GET /extensions endpoint, NOT a
// module-owned panel - the shell owns the listing (it aggregates every module,
// including the modules that carry no panel UI). A module the inventory does not know
// is DISABLED (it unmounted, so /extensions never listed it): it rides the tail with
// empty detail. One row per module, never two: the merge keys on the module name and
// the inventory (mount order) owns the position; disabled rows append in registry order
// after the mounted ones.

import type { OmegaExtension, ModuleRow } from "../../composables/omega";

// PanelRef is one composition-manifest hit.
export interface PanelRef {
  module: string;
  title: string;
}

interface ExtensionRow {
  name: string;
  description: string;
  tools: number;
  commands: number;
  toolNames: string[];
  commandNames: string[];
  seams: string[];
  panels: string[];
  hasUi: boolean;
  // lifecycle from the registry introspection. "shell" = the always-on serving shell
  // (never in the /modules list; always on by tier law), "mounted" and "disabled" are
  // the live registry states.
  state: "mounted" | "disabled" | "shell";
  toggleable: boolean;
  dependents: string[];
}

// extensionsRows merges the endpoint payload, the composition manifest.
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
      state: m ? m.state : "shell",
      toggleable: m?.toggleable ?? false,
      dependents: m?.dependents ?? [],
    };
  });

  // the mounted inventory never lists disabled modules (they unmounted).
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

// extensionsSummary is the tab's header line.
export function extensionsSummary(rows: readonly ExtensionRow[]): string {
  const tools = rows.reduce((n, r) => n + r.tools, 0);
  const commands = rows.reduce((n, r) => n + r.commands, 0);
  return `${rows.length} module${rows.length === 1 ? "" : "s"} - ${tools} tool${tools === 1 ? "" : "s"}, ${commands} command${commands === 1 ? "" : "s"}`;
}