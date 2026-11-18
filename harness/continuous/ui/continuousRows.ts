// continuousRows.ts: the pure view-model helpers for the continuous roster pane.

export interface OmegaContinuousRow {
  name: string;
  description?: string;
  provider_type?: string;
  model_name?: string;
  skill_count: number;
  memory_size: number;
  dir: string;
  // the wake state (the profile facts never carry these)
  last_report?: string;
  last_report_at?: string;
  next_wake?: string;
  unread: boolean;
}

// OmegaEvidenceRow is one ledger row (GET /continuous/{name}/evidence).
export interface OmegaEvidenceRow {
  id: number;
  ts: string;
  run_id: string;
  tool: string;
  args_hash: string;
  ok: boolean;
  verifier?: string;
}

// OmegaChatMessage is one canonical-chat tail message.
export interface OmegaChatMessage {
  content: string;
  timestamp?: string;
}

// rowStatus is the list-row status voice.
export function rowStatus(row: OmegaContinuousRow): string {
  if (row.unread) return "unread report";
  if (row.next_wake) return `next wake ${shortWhen(row.next_wake)}`;
  return "sleeping";
}

// shortWhen renders an ISO timestamp as its local HH:MM.
export function shortWhen(iso: string): string {
  const cut = iso.slice(11, 16);
  return cut || "-";
}

// The under-row last-report preview helper is DELETED.

// evidenceSummary is the detail strip's evidence line: the run's
// verdict counts in one voice ("4 ok, 1 failed" style), empty when no
// rows exist (never a fabricated "verified" for an empty ledger).
export function evidenceSummary(rows: OmegaEvidenceRow[]): string {
  if (rows.length === 0) return "no evidence rows";
  const ok = rows.filter((r) => r.ok).length;
  const failed = rows.length - ok;
  return failed === 0 ? `${ok} ok` : `${ok} ok, ${failed} failed`;
}

// verifierVoice renders one row's verifier verdict or a dash.
export function verifierVoice(row: OmegaEvidenceRow): string {
  return row.verifier || "-";
}