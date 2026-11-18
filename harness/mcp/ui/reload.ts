// omega webui: the MCP tab's reload chain.
import type { McpStatusWire } from "./serverRows";

// reloadMcpServers refreshes the tab. The POST body IS the fresh StatusReport - the one
// round-trip the v0.14.1 review lens collected (the old two-step re-GET was dead weight
// the route contract never needed).
export async function reloadMcpServers(): Promise<McpStatusWire> {
  const reload = await fetch("/mcp/reload", { method: "POST" });
  if (!reload.ok) throw new Error(`POST /mcp/reload: ${reload.status}`);
  return (await reload.json()) as McpStatusWire;
}