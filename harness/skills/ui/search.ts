// omega webui: the registry rows' in-band search model (card
// t_529628d5, the band-search ruling). The SKILLS tab's filter: a row
// survives when the trimmed, lowercased query is a case-insensitive
// substring of the row's NAME or DESCRIPTION. Filtering only HIDES
// rows (the row anatomy law, card t_561dc4e0): survivors keep their
// exact row shape, the fold flags included. Empty (or whitespace-only)
// query = the full list unchanged. Pure - no Vue, no fetch - the node
// suite pins it without compiling the SFC (the refresh.ts recipe).

export interface SearchableRow {
  name: string;
  description?: string | null;
}

export function filterSearchableRows<T extends SearchableRow>(
  rows: readonly T[],
  query: string,
): T[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...rows];
  return rows.filter(
    (r) =>
      r.name.toLowerCase().includes(q) ||
      (r.description ?? "").toLowerCase().includes(q),
  );
}