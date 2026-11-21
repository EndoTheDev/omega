// omega webui: the MCP tab row's right-click rows.
//
// EXISTING VERBS ONLY: Open unfolds the row's detail (the recorded failure in full, and
// where the entry is authored), Enable or Disable is the row's own pill (POST
// /mcp/toggle), and Delete arms the detail's one confirm (POST /mcp/delete).
//
// NO Edit row: the panel has no edit surface, and POST /mcp/save upserts a WHOLE entry -
// the wire (GET /mcp/servers) carries no args/env/headers, so a form built on it would
// silently drop them. A verb that cannot act without destroying config is absent.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface McpMenuDeps {
  open: () => void;
  toggle: () => void;
  remove: () => void;
}

export function mcpMenuItems(
  server: { enabled: boolean },
  deps: McpMenuDeps,
): ContextMenuEntry[] {
  return [
    { id: "mcp:open", label: "Open", run: deps.open },
    {
      id: "mcp:toggle",
      label: server.enabled ? "Disable" : "Enable",
      run: deps.toggle,
    },
    { kind: "separator" },
    { id: "mcp:delete", label: "Delete", danger: true, run: deps.remove },
  ];
}
