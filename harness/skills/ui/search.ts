// omega webui: the registry rows' in-band search model.

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