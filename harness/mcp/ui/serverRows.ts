export interface McpServerWire {
  name: string;
  transport?: string;
  command?: string;
  url?: string;
  // the whole entry rides so the panel can edit it: args plain, env and header VALUES
  // masked (the write swaps a mask back for the stored value).
  args?: string[];
  env?: Record<string, string>;
  headers?: Record<string, string>;
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

// the row detail's edit note: Edit saves the WHOLE entry through POST /mcp/save, so the
// detail says what a masked value means instead of letting a silent overwrite happen.
export const MCP_DETAIL_NOTE =
  "config: mcp.yaml - env and header values read as ****, a masked value is kept on save; clear the line to remove the key";

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