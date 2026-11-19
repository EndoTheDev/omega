// omega webui: the MCP tab's in-band search model. A server row survives when the
// trimmed, lowercased query is a case-insensitive substring of the server's NAME or its
// TRANSPORT badge - and of any TOOL NAME the row's expansion shows (the chips are part
// of the row, so a search that found a tool cannot hide the server carrying it).

import type { McpServerWire } from "./serverRows";

export function filterMcpServers(
  rows: readonly McpServerWire[],
  query: string,
): McpServerWire[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...rows];
  return rows.filter(
    (s) =>
      s.name.toLowerCase().includes(q) ||
      (s.transport ?? "").toLowerCase().includes(q) ||
      (s.tools ?? []).some((t) => t.toLowerCase().includes(q)),
  );
}