// omega webui: pure row model for the EXTENSIONS inspector tab
// (card t_14f73742 - the webui parity of the TUI /extensions command).
//
// Composition contract note: this inventory tab is a SHELL tab over the
// GET /extensions endpoint, NOT a module-owned panel - the shell owns
// the listing (it aggregates every module, including the modules that
// carry no panel UI). has-ui is derived at shell build time from the
// composition manifest (modulePanels: a module shipped modules/<name>/ui/
// with at least one panel component = it has UI); the endpoint itself
// stays source-agnostics for the runtime. Modules with UI resolve to a
// switch action (open their dock tab); the rest render "no UI".
// Pure on purpose: no Vue, no fetch - the unit suite pins the rows.

import type { OmegaExtension } from "../../composables/omega";

export interface ExtensionRow {
  name: string;
  description: string;
  tools: number;
  commands: number;
  seams: string[];
  // true when the module ships a panel UI (composition manifest hit):
  // the row's action opens the module's dock tab
  hasUi: boolean;
}

// extensionsRows maps the endpoint payload to renderable rows, in
// payload order (mount order - the same facts the TUI renders).
// hasUi = the module name hits the module-panel list.
export function extensionsRows(
  extensions: readonly OmegaExtension[],
  uiModules: readonly string[],
): ExtensionRow[] {
  const ui = new Set(uiModules);
  return extensions.map((e) => ({
    name: e.name,
    description: e.description ?? "",
    tools: e.tools,
    commands: e.commands,
    seams: e.seams ?? [],
    hasUi: ui.has(e.name),
  }));
}

// extensionsSummary is the tab's header line: N modules, T tools,
// C commands across the load. Zero modules reads "no extensions loaded"
// upstream (the tab's honest empty state).
export function extensionsSummary(rows: readonly ExtensionRow[]): string {
  const tools = rows.reduce((n, r) => n + r.tools, 0);
  const commands = rows.reduce((n, r) => n + r.commands, 0);
  return `${rows.length} module${rows.length === 1 ? "" : "s"} - ${tools} tool${tools === 1 ? "" : "s"}, ${commands} command${commands === 1 ? "" : "s"}`;
}