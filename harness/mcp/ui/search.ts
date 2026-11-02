// omega webui: the MCP tab's in-band search model (card t_529628d5,
// the band-search ruling). A server row survives when the trimmed,
// lowercased query is a case-insensitive substring of the server's
// NAME or its TRANSPORT badge - and of any TOOL NAME the row's
// expansion shows (the chips are part of the row, so a search that
// found a tool cannot hide the server carrying it). Filtering only
// HIDES rows (the row anatomy law, card t_561dc4e0): the survivors
// keep their exact row shape. Empty (or whitespace-only) query = the
// full list unchanged. Pure - no Vue, no fetch - the node suite pins
// it without compiling the SFC (the serverRows.ts recipe).

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