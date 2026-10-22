// omega webui: finished-things notifications client (card t_5d9032d3).
// One small record per finished thing - kanban task done, cron fire
// finished, dispatched chat run ended - served by GET /notifications,
// POST /notifications/read and a live SSE toast stream on
// /notifications/events. Records are METADATA ONLY (kind, source id,
// title line, verdict, read flag); the UI renders exactly what the
// server sent and never invents content for the title line.

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

// subscribeNotifications opens the live stream and hands every
// "notification" frame to the callback. Returns a close fn (the
// EventSource survives route changes; the bell's unmount closes it).
export function subscribeNotifications(
  onFrame: (n: OmegaNotification) => void,
): () => void {
  const es = new EventSource("/notifications/events");
  let stopped = false;
  es.addEventListener("notification", (ev) => {
    try {
      onFrame(JSON.parse((ev as MessageEvent<string>).data) as OmegaNotification);
    } catch {
      // a malformed frame is skipped, not fatal
    }
  });
  return () => {
    if (stopped) return;
    stopped = true;
    es.close();
  };
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