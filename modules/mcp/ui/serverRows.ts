// omega webui: the MCP tab's row model (card t_c7136c50, toggle state
// joined at t_83f37525). PURE on purpose - no Vue, no fetch - the
// node suite pins the wire-to-row mapping, the status semantics, and
// the empty state's exact voice; the component (McpServers.vue) only
// wires fetch + render + toggle, module-UI style.
//
// The wire shape GET /mcp/servers serves (modules/mcp/status.go).
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

// card t_83f37525: the per-server toggle. The wire always carries
// enabled on a live module; an undefined flag is an OLD wire - and
// neither disables a server implicitly, so the absent key reads as on
// (the backend's isEnabled() ruling, mirrored here).
export function serverEnabled(s: McpServerWire): boolean {
  return s.enabled !== false;
}

// the per-server truth line, in priority order: a disabled server
// says exactly that (the toggle state IS the status - a config
// decision outranks the failed connection its skip caused), then the
// recorded failure, then the mount-time connection truth.
export function statusLabel(s: McpServerWire): string {
  if (!serverEnabled(s)) return "disabled";
  if (s.error && s.error.length > 0) return s.error;
  return s.connected ? "connected" : "not connected";
}

// the status dot's token class, mirroring statusLabel's priority:
// "ok" rides --ok (a live connection), "err" rides --err (a recorded
// failure), "off" is the neutral dim (disabled, or configured but
// not connected at mount). The component maps the class, the model
// owns the semantics.
export function statusDot(s: McpServerWire): "ok" | "err" | "off" {
  if (!serverEnabled(s)) return "off";
  if (s.error && s.error.length > 0) return "err";
  return s.connected ? "ok" : "off";
}

// one panel row per configured server, config order kept (the config
// file's order IS the inventory's order), tools sorted as the wire
// delivers them.
export function serverRows(rep: McpStatusWire): McpServerWire[] {
  return Array.isArray(rep.servers) ? rep.servers : [];
}