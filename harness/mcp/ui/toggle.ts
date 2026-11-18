// omega webui: the MCP tab's per-server toggle chain.
import type { McpStatusWire } from "./serverRows";

// setMcpServerEnabled persists the per-server flag. The POST body IS the fresh
// StatusReport - the one round-trip the v0.14.1 review lens collected (the old two-step
// re-GET was dead weight the route contract never needed). Returns the fresh listing;
// failure throws so the panel's error line shows it and the prior rows stand (a refused
// write never fakes a swap).
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
  return (await res.json()) as McpStatusWire;
}