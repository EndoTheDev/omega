// EXISTING VERBS ONLY: Open unfolds the row's detail (the recorded failure in full, and
// where the entry is authored), Edit opens the detail's entry form, Enable or Disable is
// the row's own pill (POST /mcp/toggle), and Delete arms the detail's one confirm (POST
// /mcp/delete).
//
// Edit is lossless because the wire now carries the whole entry: GET /mcp/servers answers
// args plain and env/header values masked, and POST /mcp/save - the route the CLI's
// `omega mcp edit` rides - swaps a masked value back for the one on disk, so a form built
// on the wire cannot drop an env-carried token.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface McpMenuDeps {
  open: () => void;
  edit: () => void;
  toggle: () => void;
  remove: () => void;
}

export function mcpMenuItems(
  server: { enabled: boolean },
  deps: McpMenuDeps,
): ContextMenuEntry[] {
  return [
    { id: "mcp:open", label: "Open", run: deps.open },
    { id: "mcp:edit", label: "Edit", run: deps.edit },
    {
      id: "mcp:toggle",
      label: server.enabled ? "Disable" : "Enable",
      run: deps.toggle,
    },
    { kind: "separator" },
    { id: "mcp:delete", label: "Delete", danger: true, run: deps.remove },
  ];
}
