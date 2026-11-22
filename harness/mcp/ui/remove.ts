// The route answers the FRESH server listing after the bridge swap, so the panel
// re-renders from server truth and never from its own guess - the reload/toggle helpers'
// contract. A refused delete throws; the caller lands it on the panel's error line.
import type { McpStatusWire } from "./serverRows";

export async function deleteMcpServer(name: string): Promise<McpStatusWire> {
  const res = await fetch("/mcp/delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(`POST /mcp/delete: ${res.status}`);
  return (await res.json()) as McpStatusWire;
}
