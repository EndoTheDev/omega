// omega webui: the registry rows' in-band search model.

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