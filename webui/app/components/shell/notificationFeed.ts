// omega webui: the notifications feed store.. Module-level reactive singleton: the
// badge must stay live wherever the notifications tab chip renders (DockPane),
// independent of whether the feed panel or the toast stack is mounted.
// startNotificationFeed is idempotent - the first mount (usually NotificationToasts)
// lights the feed; every later mount is a no-op.: the store owns THREE emitters now,
// and the third one is the approval surface. What it never had was a VOICE: the badge
// rides the notifications chip, which is invisible when that tab is not on screen, so
// an approval from a session the page never opened looked like nothing happening until
// the panel's activation read.

import { computed, ref } from "vue";
import {
  clearAllNotifications,
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
} from "../../composables/notifications";
import { onToastPanel } from "../../composables/canvas";
import { fetchVerdicts, type PendingVerdictCard } from "../../composables/omega";

// the age renderer rides the one surface (the panel rows import the store, not the.
export { notificationAge };

// the durable feed (newest first) + the server's unread count
const list = ref<OmegaNotification[]>([]);
const unread = ref(0);
// pending agent questions. The hub holds them in memory and pushes nothing of its own:
// this list is a REST read (GET /verdicts) refreshed on the shell's event hints, never
// a poll, and its ARRIVALS toast - see newlyPendingVerdicts.
const pendingVerdicts = ref<PendingVerdictCard[]>([]);
// the card ids this page has already announced, and whether the boot read has seeded.
const announcedVerdicts = new Set<string>();
let verdictsSeeded = false;

// NotificationToast is one row of the transient stack. The kind discriminates so a
// renderer (or a click) never has to guess which shape it holds.
export type NotificationToast =
  | { key: number; kind: "record"; record: OmegaNotification }
  | { key: number; kind: "verdict"; card: PendingVerdictCard };
// the transient toast stack
const toasts = ref<NotificationToast[]>([]);

// THE badge: the dockable's tab chip reads this.
export const notificationsBadge = computed(
  () => unread.value + pendingVerdicts.value.length,
);
// the chip text: the cap voice (99+), "" at zero (DockPane renders no badge element.
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

// refresh pulls the REST truth: the full feed + the pending cards. The two reads are
// INDEPENDENT: they answer different seams, so one broken route must not blank the
// other - the pending list is the half a user is BLOCKED on. A failed read degrades to
// "state unchanged" (the store starts empty and a broken lane never breaks the panel -
// it just renders stale).
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

// newlyPendingVerdicts (the arrival diff this read feeds) lives in
// composables/notifications.ts. Every caller of refresh shares the bookkeeping law -
// the boot read, an event hint, the panel's activation, a reconnect catch-up.

// pushVerdictToast announces one newly pending approval in the shell.
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

// the funnel: one live frame -> the feed row, the unread count, the toast.
function ingest(payload: unknown): void {
  const n = decodeToast(payload);
  if (!n) return;
  list.value = upsertRecord(list.value, n);
  if (!n.read) unread.value += 1;
  pushToast(n);
}

// markRecordRead: a row (or toast) click un-badges that one record.
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

// markAllRead: every record read in one POST.
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

// dismissToast: one toast out of the stack (its click or its expiry).
export function dismissToast(key: number): void {
  toasts.value = toasts.value.filter((t) => t.key !== key);
}

// clearToasts: every toast out of the stack at once - the expiry
// timers die with it (a toast that never renders fires no timer).
export function clearToasts(): void {
  for (const id of timers) clearTimeout(id);
  timers.clear();
  toasts.value = [];
}

// clearAll: every record DELETES (the tab's clear-all button).
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

// a toast click: mark the record read (the old openRecord contract),
// drop the toast, return the record for the caller's navigation intent.
export async function openToastRecord(t: {
  key: number;
  record: OmegaNotification;
}): Promise<OmegaNotification> {
  void markRecordRead(t.record);
  dismissToast(t.key);
  return t.record;
}

// openToastVerdict: an approval toast's click. The toast drops and the card comes back
// for the caller's navigation intent - the answering surface is the notifications
// panel's pending list.
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

// startNotificationFeed lights the page-lifetime store. Idempotent; every mount calls
// it, only the first one pays. The chat topic is NOT pane-scoped: the hub fans every
// run out to every page, so an approval from a session this page never opened still
// arrives as a frame - and the arrival toasts on the spot. refresh never rejects (each
// half catches to null), so a broken route can strand neither the read nor the marker
// that awaits it.
let bootRead: Promise<void> | null = null;

export function startNotificationFeed(): void {
  if (started) return;
  started = true;
  bootRead = refresh();
  releaseFeed = onToastPanel(ingest);
}

// notificationFeedSettled: the boot refresh's promise. Resolves at once
// when the feed never started (a shell with no toast host has no boot
// read to wait on). It never RE-triggers the read - exactly one boot pull
// per page, the same one the panel and the hints then keep fresh.
export function notificationFeedSettled(): Promise<void> {
  return bootRead ?? Promise.resolve();
}

// stopNotificationFeed: drop the funnel + toast timers so a store lit under a disposed
// page (HMR, tests) never leaks them across a module re-evaluation.
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

// refreshNotifications: an activation pull.
export function refreshNotifications(): Promise<void> {
  return refresh();
}