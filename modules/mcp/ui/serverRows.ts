// omega webui: the MCP tab's row model (card t_c7136c50). PURE on
// purpose - no Vue, no fetch - the node suite pins the wire-to-row
// mapping and the empty state's exact voice; the component
// (McpServers.vue) only wires fetch + render, module-UI style.
//
// The wire shape GET /mcp/servers serves (modules/mcp/status.go).
export interface McpServerWire {
  name: string;
  transport?: string;
  command?: string;
  url?: string;
  connected?: boolean;
  error?: string;
  tools?: string[];
}

export interface McpStatusWire {
  source?: string;
  servers?: McpServerWire[];
}

// the empty state's voice (the card's contract): what the config is,
// where to fix it, never a blank pane.
export const MCP_EMPTY_TEXT = "no MCP servers configured";
export const MCP_EMPTY_HINT =
  "add a servers: array to mcp.yaml (or set MCP_SERVERS)";

// where the config came from, in the panel's voice: the source echo
// beats guessing at a file path.
const SOURCE_LABELS: Record<string, string> = {
  env: "MCP_SERVERS",
  "mcp.yaml": "mcp.yaml",
  "mcp.json": "mcp.json",
  none: "no MCP config found",
};

export function sourceLabel(source: string | undefined): string {
  return source !== undefined && SOURCE_LABELS[source] !== undefined
    ? SOURCE_LABELS[source]
    : "no MCP config found";
}

// the per-server truth line: the recorded failure wins, then the
// mount-time connection state. The listing reads state as of the
// bridge's connect - it refreshes never (read-only, no live probe).
export function statusLabel(s: McpServerWire): string {
  if (s.error && s.error.length > 0) return s.error;
  return s.connected ? "connected" : "not connected";
}

// one panel row per configured server, config order kept (the config
// file's order IS the inventory's order), tools sorted as the wire
// delivers them.
export function serverRows(rep: McpStatusWire): McpServerWire[] {
  return Array.isArray(rep.servers) ? rep.servers : [];
}