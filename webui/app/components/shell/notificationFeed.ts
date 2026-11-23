// the notifications feed store. A module-level reactive singleton, so the badge stays live
// wherever the notifications tab chip renders - and startNotificationFeed is idempotent
// (the first mount usually NotificationToasts lights it). The badge rides the notifications
// chip, which is invisible when that tab is not on screen, so the store also owns the toast
// stack that speaks for an approval from a session this page never opened.

import { computed, ref } from "vue";
import {
  clearAllNotifications,
  clearOneNotification,
  fetchNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  newlyPendingVerdicts,
  notificationAge,
  notificationBadgeText,
  toastLifetime,
  upsertRecord,
  VERDICT_TOAST_MS,
  type OmegaNotification,
} from "../../composables/notifications.ts";
import { onToastPanel } from "../../composables/canvas.ts";
import { fetchVerdicts, type PendingVerdictCard } from "../../composables/omega.ts";

export { notificationAge };

// the durable feed (newest first) + the server's unread count
const list = ref<OmegaNotification[]>([]);
const unread = ref(0);
// pending agent questions: a REST read (GET /verdicts) refreshed on the shell's event hints,
// never a poll, whose ARRIVALS toast (see newlyPendingVerdicts)
const pendingVerdicts = ref<PendingVerdictCard[]>([]);
// the card ids this page has already announced, and whether the boot read has seeded
const announcedVerdicts = new Set<string>();
let verdictsSeeded = false;

// one row of the transient stack; the kind discriminates the shape
export type NotificationToast =
  | { key: number; kind: "record"; record: OmegaNotification }
  | { key: number; kind: "verdict"; card: PendingVerdictCard };
const toasts = ref<NotificationToast[]>([]);

// THE badge: the dockable's tab chip reads it
const notificationsBadge = computed(
  () => unread.value + pendingVerdicts.value.length,
);
export const notificationsBadgeText = computed(() =>
  notificationBadgeText(notificationsBadge.value),
);

export const notificationsList = list;
export const notificationsUnread = unread;
export const pendingVerdictList = pendingVerdicts;
export const notificationToasts = toasts;

// decodeToast is the canvas feed's record reader: the canvas contract hands the payload
// verbatim, so this store owns the OmegaNotification read; an unusable payload is skipped.
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

// refresh pulls the REST truth: the feed + the pending cards. The two reads are
// INDEPENDENT - one broken route must not blank the other, since the pending list is the
// half a user is BLOCKED on. A failed read degrades to "state unchanged".
async function refresh(): Promise<void> {
  const [page, vpage] = await Promise.all([
    fetchNotifications(false).catch(() => null),
    fetchVerdicts("").catch(() => null),
  ]);
  if (page) {
    list.value = page.notifications;
    unread.value = page.unread;
  }
  if (vpage) {
    const arrivals = newlyPendingVerdicts(announcedVerdicts, vpage.cards, {
      seed: !verdictsSeeded,
    });
    verdictsSeeded = true;
    pendingVerdicts.value = vpage.cards;
    for (const card of arrivals) pushVerdictToast(card);
  }
}

function pushVerdictToast(card: PendingVerdictCard): void {
  const key = ++toastKey;
  toasts.value = [...toasts.value, { key, kind: "verdict", card }];
  const id = setTimeout(() => {
    timers.delete(id);
    dismissToast(key);
  }, VERDICT_TOAST_MS);
  timers.add(id);
}

function pushToast(n: OmegaNotification): void {
  const key = ++toastKey;
  toasts.value = [...toasts.value, { key, kind: "record", record: n }];
  const id = setTimeout(() => {
    timers.delete(id);
    dismissToast(key);
  }, toastLifetime(n.verdict));
  timers.add(id);
}

function ingest(payload: unknown): void {
  const n = decodeToast(payload);
  if (!n) return;
  list.value = upsertRecord(list.value, n);
  if (!n.read) unread.value += 1;
  pushToast(n);
}

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

function clearToasts(): void {
  for (const id of timers) clearTimeout(id);
  timers.clear();
  toasts.value = [];
}

export async function clearAll(): Promise<void> {
  const before = { unread: unread.value, list: list.value };
  list.value = [];
  unread.value = 0;
  clearToasts();
  try {
    await clearAllNotifications();
  } catch {
    list.value = before.list;
    unread.value = before.unread;
  }
}

// clearOne drops ONE record optimistically: the row leaves the feed at the click, and a
// refused DELETE puts the feed back (the clearAll posture, one row narrower).
export async function clearOne(id: number): Promise<void> {
  const before = { unread: unread.value, list: list.value };
  const gone = list.value.find((n) => n.id === id);
  list.value = list.value.filter((n) => n.id !== id);
  if (gone && !gone.read) unread.value = Math.max(0, unread.value - 1);
  try {
    await clearOneNotification(id);
  } catch {
    list.value = before.list;
    unread.value = before.unread;
    throw new Error(`clear ${id} refused`);
  }
}

export async function openToastRecord(t: {
  key: number;
  record: OmegaNotification;
}): Promise<OmegaNotification> {
  void markRecordRead(t.record);
  dismissToast(t.key);
  return t.record;
}

export function openToastVerdict(t: {
  key: number;
  card: PendingVerdictCard;
}): PendingVerdictCard {
  dismissToast(t.key);
  return t.card;
}

let started = false;
let toastKey = 0;
const timers = new Set<ReturnType<typeof setTimeout>>();
let releaseFeed: (() => void) | null = null;

// startNotificationFeed lights the page-lifetime store. Idempotent; every mount calls it,
// only the first pays. The chat topic is NOT pane-scoped (the hub fans every run out to
// every page), so an approval from a session this page never opened still arrives as a
// frame and toasts on the spot. refresh never rejects, so a broken route strands nothing.
let bootRead: Promise<void> | null = null;

export function startNotificationFeed(): void {
  if (started) return;
  started = true;
  bootRead = refresh();
  releaseFeed = onToastPanel(ingest);
}

// the boot refresh's promise; resolves at once when the feed never started. It never
// RE-triggers the read - exactly one boot pull per page.
export function notificationFeedSettled(): Promise<void> {
  return bootRead ?? Promise.resolve();
}

// drop the funnel + toast timers, so a store lit under a disposed page (HMR, tests) never
// leaks them across a module re-evaluation.
export function stopNotificationFeed(): void {
  releaseFeed?.();
  releaseFeed = null;
  started = false;
  bootRead = null;
  announcedVerdicts.clear();
  verdictsSeeded = false;
  for (const id of timers) clearTimeout(id);
  timers.clear();
}

export function refreshNotifications(): Promise<void> {
  return refresh();
}
