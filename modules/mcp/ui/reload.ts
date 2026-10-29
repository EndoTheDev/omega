// omega webui: the MCP tab's reload chain (card t_d6e94e79). The
// refresh button's behavior lives here so the node suite probes it
// with a fetch log - the SFC (McpServers.vue) only wires the click,
// module-UI style. serverRows.ts stays pure: no fetch.
import type { McpStatusWire } from "./serverRows";

// reloadMcpServers refreshes the tab the ruled way: POST the module's
// reload route (the server re-reads the mcp config and swaps its live
// bridge - no restart), then re-fetch the listing over the fresh
// bridge. Returns the fresh listing; failure throws so the panel's
// error line shows it and the prior rows stand.
export async function reloadMcpServers(): Promise<McpStatusWire> {
  const reload = await fetch("/mcp/reload", { method: "POST" });
  if (!reload.ok) throw new Error(`POST /mcp/reload: ${reload.status}`);
  // the reload body IS the fresh StatusReport; the listing is the
  // re-fetch (the ruled two-step)
  await reload.json();
  const res = await fetch("/mcp/servers");
  if (!res.ok) throw new Error(`GET /mcp/servers: ${res.status}`);
  return (await res.json()) as McpStatusWire;
}