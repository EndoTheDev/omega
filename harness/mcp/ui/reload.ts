// omega webui: the MCP tab's reload chain.
import type { McpStatusWire } from "./serverRows";

// reloadMcpServers refreshes the tab: the POST body IS the fresh StatusReport, so one
// round trip suffices.
export async function reloadMcpServers(): Promise<McpStatusWire> {
  const reload = await fetch("/mcp/reload", { method: "POST" });
  if (!reload.ok) throw new Error(`POST /mcp/reload: ${reload.status}`);
  return (await reload.json()) as McpStatusWire;
}