// omega webui: the registry rows' in-band search model (card
// t_529628d5, the band-search ruling). The TOOLS tab's filter: a row
// survives when the trimmed, lowercased query is a case-insensitive
// substring of the row's NAME or DESCRIPTION. The search respects the
// tools tab's own slice law (card t_c2da3379) by construction: the
// filter runs on the tab's rows, which are already the native-only
// inventory - MCP-origin tools belong to the MCP tab alone and nothing
// the search does can resurrect them. The filter runs on the GROUPED
// output rows (toolRows), so it only HIDES whole rows - the one-domain
// grouping intact, no re-shaping (the row anatomy law, card
// t_561dc4e0). Empty (or whitespace-only) query = the full list
// unchanged. Pure - no Vue, no fetch - the node suite pins it without
// compiling the SFC (the toolRows.ts recipe).

import type { ToolRow } from "./toolRows";

export function filterToolRows(
  rows: readonly ToolRow[],
  query: string,
): ToolRow[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...rows];
  return rows.filter(
    (r) =>
      r.name.toLowerCase().includes(q) ||
      r.description.toLowerCase().includes(q),
  );
}