// omega webui: finished-things notifications client (card t_5d9032d3).
// One small record per finished thing - kanban task done, cron fire
// finished, dispatched chat run ended - served by GET /notifications and
// POST /notifications/read. RECORDS ARE METADATA ONLY (kind, source id,
// title line, verdict, read flag); the UI renders exactly what the
// server sent and never invents content for the title line.
//
// Arc 4b (t_9339ad05): the toast moved to the canvas - the notifications
// capsule ships a toast panel and the dock renders toasts from the
// canvas feed (composables/canvas.ts onToastPanel), not from a private
// SSE stream. What stays here is the REST client, the pure helpers the
// dock and the toast path keep using, and the board-select seam.

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

// fetchNotifications lists records newest-first. unreadOnly=1 pins the
// list to unread rows; the payload's unread count is computed over the
// whole set either way (the badge survives pagination).
export async function fetchNotifications(unreadOnly = false): Promise<NotificationsPage> {
  const url = unreadOnly ? "/notifications?unread=1" : "/notifications";
  const r = await fetch(url);
  if (!r.ok) throw new Error(`notifications: ${r.status}`);
  return r.json();
}

// markNotificationRead flags one record read (POST {"id": n}).
export async function markNotificationRead(id: number): Promise<void> {
  const r = await fetch("/notifications/read", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id }),
  });
  if (!r.ok) throw new Error(`read: ${r.status}`);
}

// markAllNotificationsRead flags every record read (POST {"all": true})
// - the panel's clear-all button; the badge drops to zero on success.
export async function markAllNotificationsRead(): Promise<void> {
  const r = await fetch("/notifications/read", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ all: true }),
  });
  if (!r.ok) throw new Error(`read-all: ${r.status}`);
}

// --- pure helpers (node-tested: test/notifications.test.ts) ---

// notificationAge renders the age line: seconds under a minute,
// minutes under an hour, hours under a day, days beyond. A garbage or
// missing timestamp renders "" (an age that lies is worse than none).
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

// upsertRecord folds one live frame into the bell's list: the record
// lands on top, a duplicate (the same id seen twice - a reconnect
// replay) replaces its row instead of stacking, and the list stays
// capped at cap so a chatty lane cannot grow it unbounded. Pure so the
// node test pins the dedupe + the cap.
export function upsertRecord(
  list: OmegaNotification[],
  n: OmegaNotification,
  cap = 50,
): OmegaNotification[] {
  const exists = list.some((x) => x.id === n.id);
  if (exists) return list.map((x) => (x.id === n.id ? n : x));
  return [n, ...list].slice(0, cap);
}

// toastsFromFrame picks the auto-dismiss lifetime: failed runs stay
// longer (an operator needs time to read the red), everything else
// 6s. Pure so the test pins the asymmetry.
export function toastLifetime(verdict: "ok" | "failed"): number {
  return verdict === "failed" ? 12000 : 6000;
}

// --- the board-select seam (card t_556ee727) -----------------------------

// A task notification's click focuses the kanban tab; the BOARD the task
// lives on crosses into the module-owned Board component through THIS
// seam (module UI imports no shell code, so the selection is a window
// CustomEvent, not a prop or an import). The stash covers the mount
// race: the tab activation mounts the Board on the tick after the tree
// swap, so the component re-reads the stash on mount and consumes it.
export const BOARD_SELECT_EVENT = "omega-select-board";

declare global {
  interface Window {
    __omegaBoardSelect?: string;
  }
}

// boardOfTask pins which registered board holds the task: the FIRST
// board whose task list carries the id (boards are isolated, a card
// lives on exactly one). null when the scan comes up empty (an
// archived task, a missing record, a single-board install - the click
// still focuses the pane, the board stays where it is). Selecting by
// name instead of leaving "" is deliberate: an explicit pick re-renders
// the same columns for the default board, so the switch is a no-op.
// Pure so the node test pins the scan order.
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

// selectBoardFor publishes the select intent: the stash lands first
// (the component may not be mounted yet - the mount reads it), the
// event second. No-op on an empty board (the server default is already
// what the board boots to).
export function selectBoardFor(board: string): void {
  if (!board) return;
  if (typeof window === "undefined") return; // node tests
  window.__omegaBoardSelect = board;
  window.dispatchEvent(
    new CustomEvent(BOARD_SELECT_EVENT, { detail: { board } }),
  );
}

// resolveTaskBoard scans the registry for the board holding `taskId`:
// GET /boards, then a GET /tasks per board looking for the id. Any
// failure (the seam absent, a 501, a broken registry) resolves null -
// the click still focuses the pane, the board just stays where it is.
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
        const rt = await fetch(`/tasks?board=${encodeURIComponent(board)}`);
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