// continuousRows.ts: the pure view-model helpers for the continuous
// roster pane (card t_72ecca67, ruling 18). Render-free; every function
// is unit-tested in modules/ui/test/continuous.test.ts.

// OmegaContinuousRow is one roster payload row (GET /continuous/roster):
// the agent profile facts plus the wake state (rulings 5, 10, 18) - the
// last report preview, when it landed, the next wake, the unread flag.
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

// OmegaEvidenceRow is one ledger row (GET /continuous/{name}/evidence):
// ruling 11's runtime-owned record - tool, args hash, ok/failed, the
// cheap verifier's verdict.
export interface OmegaEvidenceRow {
  id: number;
  ts: string;
  run_id: string;
  tool: string;
  args_hash: string;
  ok: boolean;
  verifier?: string;
}

// OmegaChatMessage is one canonical-chat tail message (the view
// renders only what the store returned - the tail, not an archive).
export interface OmegaChatMessage {
  content: string;
  timestamp?: string;
}

// rowStatus is the list-row status voice: awake (a wake runs), the
// next scheduled wake, or the quiet sleeper. Nothing is invented: an
// empty next_wake renders as the sleeper.
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

// previewLine caps the last-report preview like the server does (the
// server already truncates; this guards a re-fetch race only).
export function previewLine(row: OmegaContinuousRow): string {
  return (row.last_report ?? "").trim() || "- no report yet -";
}

// evidenceSummary is the detail strip's evidence line: the run's
// verdict counts in one voice ("4 ok, 1 failed" style), empty when no
// rows exist (never a fabricated "verified" for an empty ledger).
export function evidenceSummary(rows: OmegaEvidenceRow[]): string {
  if (rows.length === 0) return "no evidence rows";
  const ok = rows.filter((r) => r.ok).length;
  const failed = rows.length - ok;
  return failed === 0 ? `${ok} ok` : `${ok} ok, ${failed} failed`;
}

// verifierVoice renders one row's verifier verdict or a dash (absent
// verifier = the call carried nothing cheap to check).
export function verifierVoice(row: OmegaEvidenceRow): string {
  return row.verifier || "-";
}