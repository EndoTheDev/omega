// omega webui: the notifications feed store (card t_180db2cb).
//
// The bell died with its popover: the durable feed/history lives in the
// notifications dock tab (NotificationsPanel.vue), the transient toasts
// stay shell-level (NotificationToasts.vue, mounted by MenuBar), and the
// unread badge rides the dockable's tab icon chip (DockPane reads
// notificationsBadgeText from here). ONE page-lifetime store owns the
// canvas toast funnel (arc 4b - onToastPanel, the fingerprint upstream
// dedupes replays), the /notifications REST truth, and the pending-
// verdict poll; the components only read state and call actions.
//
// Module-level reactive singleton: the badge must stay live wherever the
// notifications tab chip renders (DockPane), independent of whether the
// feed panel or the toast stack is mounted. startNotificationFeed is
// idempotent - the first mount (usually NotificationToasts) lights the
// feed; every later mount is a no-op.

import { computed, ref } from "vue";
import {
  fetchNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  notificationAge,
  notificationBadgeText,
  toastLifetime,
  upsertRecord,
  type OmegaNotification,
} from "../../composables/notifications";
import { onToastPanel } from "../../composables/canvas";
import { fetchVerdicts, type PendingVerdictCard } from "../../composables/omega";

// the age renderer rides the one surface (the panel rows import the
// store, not the raw composable)
export { notificationAge };

// the durable feed (newest first) + the server's unread count
const list = ref<OmegaNotification[]>([]);
const unread = ref(0);
// pending agent questions (verdict cards v1, t_2ecf9c51) - no SSE for
// them; the poll below keeps the cadence the bell used
const pendingVerdicts = ref<PendingVerdictCard[]>([]);
// the transient toast stack (one toast per live frame)
const toasts = ref<{ key: number; record: OmegaNotification }[]>([]);

// THE badge: the dockable's tab chip reads this. Sums unread
// finished-things and pending questions - the one place a user looks
// for "something needs me" (the old bell chip's rule, rehomed).
export const notificationsBadge = computed(
  () => unread.value + pendingVerdicts.value.length,
);
// the chip text: the cap voice (99+), "" at zero (DockPane renders no
// badge element at all when the text is empty)
export const notificationsBadgeText = computed(() =>
  notificationBadgeText(notificationsBadge.value),
);

export const notificationsList = list;
export const notificationsUnread = unread;
export const pendingVerdictList = pendingVerdicts;
export const notificationToasts = toasts;

// decodeToast is the canvas feed's record reader: the canvas contract
// hands the payload verbatim (it never knows the record shape); this
// store owns the OmegaNotification read. An unusable payload is
// skipped - a toast that never fires is cleaner than a broken frame.
function decodeToast(payload: unknown): OmegaNotification | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.id !== "number") return null;
  const kind = p.kind;
  if (kind !== "task" && kind !== "cron" && kind !== "chat") return null;
  return {
    id: p.id,
    kind,
    source_id: typeof p.source_id === "string" ? p.source_id : "",
    title: typeof p.title === "string" ? p.title : "",
    verdict: p.verdict === "failed" ? "failed" : "ok",
    created_at: typeof p.created_at === "string" ? p.created_at : "",
    read: p.read === true,
  };
}

// refresh pulls the REST truth: the full feed + the pending cards. A
// failure degrades to "state unchanged" (the store starts empty and a
// broken lane never breaks the panel - it just renders stale).
async function refresh(): Promise<void> {
  try {
    const [page, vpage] = await Promise.all([
      fetchNotifications(false),
      fetchVerdicts("").catch(() => ({ cards: [] as PendingVerdictCard[], count: 0 })),
    ]);
    list.value = page.notifications;
    unread.value = page.unread;
    pendingVerdicts.value = vpage.cards;
  } catch {
    // stale truth beats a broken panel
  }
}

function pushToast(n: OmegaNotification): void {
  const key = ++toastKey;
  toasts.value = [...toasts.value, { key, record: n }];
  const id = setTimeout(() => {
    timers.delete(id);
    dismissToast(key);
  }, toastLifetime(n.verdict));
  timers.add(id);
}

// the funnel: one live frame -> the feed row, the unread count, the
// toast. Reload truth stays with the server's unread count; this store
// counts only NEW unread frames (a read record seen live just
// re-renders its row).
function ingest(payload: unknown): void {
  const n = decodeToast(payload);
  if (!n) return;
  list.value = upsertRecord(list.value, n);
  if (!n.read) unread.value += 1;
  pushToast(n);
}

// markRecordRead: a row (or toast) click un-badges that one record -
// optimistic on the client, server confirms with the POST.
export async function markRecordRead(n: OmegaNotification): Promise<void> {
  if (n.read) return;
  const before = unread.value;
  n.read = true;
  unread.value = Math.max(0, before - 1);
  markNotificationRead(n.id).catch(() => {
    n.read = false;
    unread.value = before;
  });
}

// markAllRead: every record read in one POST - the panel's clear-all
// button AND the tab's open/focus path (the badge clears on open,
// POST {"all": true} is the existing read-all transport).
export async function markAllRead(): Promise<void> {
  const before = { unread: unread.value };
  const beforeList = list.value;
  unread.value = 0;
  list.value = list.value.map((n) => ({ ...n, read: true }));
  try {
    await markAllNotificationsRead();
  } catch {
    unread.value = before.unread;
    list.value = beforeList;
  }
}

export function dismissToast(key: number): void {
  toasts.value = toasts.value.filter((t) => t.key !== key);
}

// a toast click: mark the record read (the old openRecord contract),
// drop the toast, return the record for the caller's navigation intent.
export async function openToastRecord(t: {
  key: number;
  record: OmegaNotification;
}): Promise<OmegaNotification> {
  void markRecordRead(t.record);
  toasts.value = toasts.value.filter((x) => x.key !== t.key);
  return t.record;
}

let started = false;
let toastKey = 0;
const timers = new Set<ReturnType<typeof setTimeout>>();
let releaseFeed: (() => void) | null = null;
let pollTimer: ReturnType<typeof setInterval> | null = null;

// startNotificationFeed lights the page-lifetime store: the initial
// refresh, the canvas toast subscription (ONE subscriber - arc 4b), and
// the verdict poll. Idempotent; every mount calls it, only the first
// one pays.
export function startNotificationFeed(): void {
  if (started) return;
  started = true;
  void refresh();
  releaseFeed = onToastPanel(ingest);
  // verdict cards have no SSE in v1: poll every 15s. Cheap (localhost,
  // a tiny JSON list) and matches the cadence a human-paced question
  // settles at; the card's own live render in chat is the real UI.
  pollTimer = setInterval(() => {
    void refresh();
  }, 15_000);
}

// stopNotificationFeed: drop the funnel + poll + toast timers so a
// store lit under a disposed page (HMR, tests) never leaks them across
// a module re-evaluation.
export function stopNotificationFeed(): void {
  releaseFeed?.();
  releaseFeed = null;
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
  started = false;
  for (const id of timers) clearTimeout(id);
  timers.clear();
}

// refreshNotifications: an activation pull - the panel calls it when
// the tab gains focus (the freshness the popover got on every toggle).
export function refreshNotifications(): Promise<void> {
  return refresh();
}