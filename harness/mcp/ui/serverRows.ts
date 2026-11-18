// omega webui: the MCP tab's row model.
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

export function serverEnabled(s: McpServerWire): boolean {
  return s.enabled !== false;
}

// the per-server truth line, in priority order: a disabled server says exactly that.
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

// one panel row per configured server, config order kept (the config file's order IS
// the inventory's order), tools sorted as the wire delivers them.
export function serverRows(rep: McpStatusWire): McpServerWire[] {
  return Array.isArray(rep.servers) ? rep.servers : [];
}