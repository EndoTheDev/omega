// omega webui: the MCP tab's per-server toggle chain.
import type { McpStatusWire } from "./serverRows";

// The POST body IS the fresh StatusReport (one round trip). A refused write throws so the
// panel shows it and the prior rows stand - never a faked swap.
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