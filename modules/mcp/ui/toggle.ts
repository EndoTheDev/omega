// omega webui: the MCP tab's per-server toggle chain (card
// t_83f37525). The toggle button's behavior lives here so the node
// suite probes it with a fetch log - the SFC (McpServers.vue) only
// wires the arming and the click, module-UI style. serverRows.ts
// stays pure: no fetch.
import type { McpStatusWire } from "./serverRows";

// setMcpServerEnabled persists the per-server flag the ruled way:
// POST /mcp/toggle (the server writes the `enabled:` flag into
// mcp.yaml and swaps its live bridge - no restart), then re-fetch the
// listing off the fresh bridge. Returns the fresh listing; failure
// throws so the panel's error line shows it and the prior rows stand
// (a refused write never fakes a swap - the reload.ts ruling).
export async function setMcpServerEnabled(
  name: string,
  enabled: boolean,
): Promise<McpStatusWire> {
  const res = await fetch("/mcp/toggle", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, enabled }),
  });
  if (!res.ok) throw new Error(`POST /mcp/toggle: ${res.status}`);
  // the toggle body IS the fresh StatusReport; the listing is the
  // re-fetch (the ruled two-step)
  await res.json();
  const list = await fetch("/mcp/servers");
  if (!list.ok) throw new Error(`GET /mcp/servers: ${list.status}`);
  return (await list.json()) as McpStatusWire;
}