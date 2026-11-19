// finished-things notifications client. RECORDS ARE METADATA ONLY (kind, source id, title
// line, verdict, read flag); the UI renders exactly what the server sent and never invents
// content for the title line.

export interface OmegaNotification {
  id: number;
  kind: "task" | "cron" | "chat";
  source_id: string;
  title: string;
  verdict: "ok" | "failed";
  created_at: string;
  read: boolean;
}

export interface NotificationsPage {
  notifications: OmegaNotification[];
  unread: number;
}

export async function fetchNotifications(unreadOnly = false): Promise<NotificationsPage> {
  const url = unreadOnly ? "/notifications?unread=1" : "/notifications";
  const r = await fetch(url);
  if (!r.ok) throw new Error(`notifications: ${r.status}`);
  return r.json();
}

export async function markNotificationRead(id: number): Promise<void> {
  const r = await fetch("/notifications/read", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id }),
  });
  if (!r.ok) throw new Error(`read: ${r.status}`);
}

export async function markAllNotificationsRead(): Promise<void> {
  const r = await fetch("/notifications/read", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ all: true }),
  });
  if (!r.ok) throw new Error(`read-all: ${r.status}`);
}

export async function clearAllNotifications(): Promise<number> {
  const r = await fetch("/notifications", { method: "DELETE" });
  if (!r.ok) throw new Error(`clear-all: ${r.status}`);
  const body = await r.json();
  return typeof body.cleared === "number" ? body.cleared : 0;
}

// pure helpers (node-tested: test/notifications.test.ts)
export function notificationAge(created_at: string, nowMs = Date.now()): string {
  const t = Date.parse(created_at);
  if (!created_at || Number.isNaN(t)) return "";
  const s = Math.max(0, Math.floor((nowMs - t) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

// upsertRecord folds one live frame into the feed: the record lands on top, a duplicate id
// (a reconnect replay) replaces its row instead of stacking, and the list stays capped so a
// chatty lane cannot grow it unbounded.
export function upsertRecord(
  list: OmegaNotification[],
  n: OmegaNotification,
  cap = 50,
): OmegaNotification[] {
  const exists = list.some((x) => x.id === n.id);
  if (exists) return list.map((x) => (x.id === n.id ? n : x));
  return [n, ...list].slice(0, cap);
}

// the auto-dismiss lifetime of a verdict toast
export function toastLifetime(verdict: "ok" | "failed"): number {
  return verdict === "failed" ? 12000 : 6000;
}

export function notificationBadgeText(count: number): string {
  if (count <= 0) return "";
  return count > 99 ? "99+" : String(count);
}

// the approval toast's dwell
export const VERDICT_TOAST_MS = 15_000;

// newlyPendingVerdicts diffs one pending-verdict read against the ids this page already
// announced and returns the ARRIVALS. The shell pulls GET /verdicts on event hints, never on
// a poll, so a card that appears between two pulls earns one toast.
//
// The FIRST successful read SEEDS (a reload must not replay a toast for an approval that was
// already pending); a card that leaves the list leaves the set, so an id re-issued after an
// expiry toasts again; a malformed row (no non-empty string id) is never announced.
export function newlyPendingVerdicts<T extends { id: string }>(
  seen: Set<string>,
  cards: readonly T[],
  opts: { seed?: boolean } = {},
): T[] {
  const arrivals: T[] = [];
  const live = new Set<string>();
  for (const card of cards) {
    if (!card || typeof card.id !== "string" || !card.id) continue;
    live.add(card.id);
    if (!seen.has(card.id)) arrivals.push(card);
  }
  seen.clear();
  for (const id of live) seen.add(id);
  return opts.seed ? [] : arrivals;
}

// the board-select seam: a task notification's click focuses the kanban tab, and the stash
// covers the mount race (the Board re-reads it on mount and consumes it).
export const BOARD_SELECT_EVENT = "omega-select-board";

declare global {
  interface Window {
    __omegaBoardSelect?: string;
  }
}

// which registered board holds the task, in registry order
export function boardOfTask(
  taskId: string,
  lists: readonly { board: string; tasks: readonly { id: string }[] }[],
): string | null {
  for (const l of lists) {
    if (!l.board) continue; // the default board is never a switch target
    if (l.tasks.some((t) => t.id === taskId)) return l.board;
  }
  return null;
}

// publishes the select intent
export function selectBoardFor(board: string): void {
  if (!board) return;
  if (typeof window === "undefined") return; // node tests
  window.__omegaBoardSelect = board;
  window.dispatchEvent(
    new CustomEvent(BOARD_SELECT_EVENT, { detail: { board } }),
  );
}

// Scans the registry for the board holding `taskId`; ?status=all is explicit because the
// default /tasks list hides archived cards, whose notifications must still resolve a board.
export async function resolveTaskBoard(taskId: string): Promise<string | null> {
  let lists: { board: string; tasks: { id: string }[] }[];
  try {
    const rb = await fetch("/boards");
    if (!rb.ok) return null; // single-board install: nothing to select
    const body = await rb.json();
    const names: string[] = (body.boards ?? [])
      .map((b: { name?: string }) => (typeof b.name === "string" ? b.name : ""))
      .filter(Boolean);
    lists = [];
    for (const board of names) {
      try {
        const rt = await fetch(`/tasks?status=all&board=${encodeURIComponent(board)}`);
        if (!rt.ok) continue;
        const tb = await rt.json();
        lists.push({ board, tasks: (tb.tasks ?? []) as { id: string }[] });
      } catch {
        // one noisy board never breaks the scan
      }
    }
  } catch {
    return null;
  }
  return boardOfTask(taskId, lists);
}