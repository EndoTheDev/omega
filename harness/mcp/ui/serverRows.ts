export interface McpServerWire {
  name: string;
  transport?: string;
  command?: string;
  url?: string;
  connected?: boolean;
  enabled?: boolean;
  error?: string;
  tools?: string[];
}

export interface McpStatusWire {
  source?: string;
  servers?: McpServerWire[];
}

// the empty state's voice: what the config is, where to fix it, never a blank pane.
export const MCP_EMPTY_TEXT = "no MCP servers configured";
export const MCP_EMPTY_HINT =
  "add a servers: array to mcp.yaml (or set MCP_SERVERS)";

// the row detail's edit note: the panel owns TWO writes (the enabled flag and a delete),
// and the entry's other keys never reach the wire - so the detail says where to edit
// instead of pretending a form here would be lossless.
export const MCP_DETAIL_NOTE =
  "config: mcp.yaml - args, env and headers are authored there; this panel toggles and deletes entries";

export function serverEnabled(s: McpServerWire): boolean {
  return s.enabled !== false;
}

export function statusLabel(s: McpServerWire): string {
  if (!serverEnabled(s)) return "disabled";
  if (s.error && s.error.length > 0) return s.error;
  return s.connected ? "connected" : "not connected";
}

// the status dot's token class, mirroring statusLabel's priority.
export function statusDot(s: McpServerWire): "ok" | "err" | "off" {
  if (!serverEnabled(s)) return "off";
  if (s.error && s.error.length > 0) return "err";
  return s.connected ? "ok" : "off";
}

export function serverRows(rep: McpStatusWire): McpServerWire[] {
  return Array.isArray(rep.servers) ? rep.servers : [];
}