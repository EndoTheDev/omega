// omega webui: the MCP tab's reload chain (card t_d6e94e79). The
// refresh button's behavior lives here so the node suite probes it
// with a fetch log - the SFC (McpServers.vue) only wires the click,
// module-UI style. serverRows.ts stays pure: no fetch.
import type { McpStatusWire } from "./serverRows";

// reloadMcpServers refreshes the tab: POST the module's reload route
// (the server re-reads the mcp config and swaps its live bridge - no
// restart). The POST body IS the fresh StatusReport - the one
// round-trip the v0.14.1 review lens collected (the old two-step
// re-GET was dead weight the route contract never needed). Returns
// the fresh listing; failure throws so the panel's error line shows
// it and the prior rows stand.
export async function reloadMcpServers(): Promise<McpStatusWire> {
  const reload = await fetch("/mcp/reload", { method: "POST" });
  if (!reload.ok) throw new Error(`POST /mcp/reload: ${reload.status}`);
  return (await reload.json()) as McpStatusWire;
}