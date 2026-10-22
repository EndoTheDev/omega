<template>
  <!-- card t_5d9032d3: the bell. Lives at the head of the menubar's
       right cluster; the badge counts unread finished-things, the
       dropdown lists the records (source icon, title, verdict color,
       age) and the lower-right toast stack pops one record per live
       SSE frame - the webui never polls for what it can stream. -->
  <div ref="root" class="notif-root">
    <button
      id="notif-bell"
      class="icon-btn notif-bell"
      aria-label="Notifications"
      :title="unread > 0 ? `${unread} unread notifications` : 'Notifications'"
      @click="toggle">
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </g>
      </svg>
      <span
        v-if="unread > 0"
        class="notif-badge"
        role="status"
        :aria-label="`${unread} unread`">{{ badgeText }}</span>
    </button>

    <!-- dropdown panel: newest first, capped list, mark-all on top -->
    <div
      v-if="open"
      class="notif-panel"
      role="dialog"
      aria-label="Notifications">
      <div class="notif-head">
        <span class="notif-title">notifications</span>
        <button
          class="notif-clear-btn"
          :disabled="unread === 0"
          @click="markAll">
          mark all read
        </button>
      </div>
      <ul class="notif-list" role="list">
        <li
          v-for="n in list"
          :key="n.id"
          class="notif-row"
          :class="{ unread: !n.read, failed: n.verdict === 'failed' }"
          @click="openRecord(n)">
          <span
            class="notif-kind"
            :data-kind="n.kind">{{ kindGlyph(n.kind) }}</span>
          <span class="notif-row-main">
            <span class="notif-row-title">{{ n.title }}</span>
            <span class="notif-row-meta">
              <span class="notif-verdict" :class="n.verdict">{{ n.verdict }}</span>
              <span class="notif-age">{{ age(n.created_at) }}</span>
            </span>
          </span>
        </li>
        <li v-if="list.length === 0" class="notif-empty">
          nothing finished yet
        </li>
      </ul>
    </div>

    <!-- toast stack: one toast per live frame, failed verdicts stay
         longer (toastLifetime). A toast's click is the row click. -->
    <transition-group name="toast" tag="div" class="notif-toasts">
      <div
        v-for="t in toasts"
        :key="t.key"
        class="notif-toast"
        :class="{ failed: t.record.verdict === 'failed' }"
        role="status"
        @click="openRecord(t.record, true)">
        <span class="notif-kind" :data-kind="t.record.kind">{{ kindGlyph(t.record.kind) }}</span>
        <span class="notif-toast-body">
          <span class="notif-row-title">{{ t.record.title }}</span>
          <span class="notif-row-meta">{{ t.record.verdict }} · {{ age(t.record.created_at) }}</span>
        </span>
        <button
          class="notif-toast-x"
          aria-label="Dismiss"
          @click.stop="dismiss(t.key)">×</button>
      </div>
    </transition-group>
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, ref } from "vue";
  import {
    fetchNotifications,
    markNotificationRead,
    markAllNotificationsRead,
    notificationAge,
    subscribeNotifications,
    upsertRecord,
    toastLifetime,
    type OmegaNotification,
  } from "../../composables/notifications";

  const emit = defineEmits<{
    // navigation intent: app.vue routes task -> board, cron -> the
    // cron inspector tab, chat -> the session when one exists
    (e: "open", n: OmegaNotification): void;
  }>();

  const open = ref(false);
  const unread = ref(0);
  const list = ref<OmegaNotification[]>([]);
  const toasts = ref<{ key: number; record: OmegaNotification }[]>([]);
  const root = ref<HTMLElement | null>(null);

  let toastKey = 0;
  const timers = new Set<ReturnType<typeof setTimeout>>();
  let closeStream: (() => void) | null = null;

  const badgeText = computed(() => (unread.value > 99 ? "99+" : String(unread.value)));

  function age(ts: string): string {
    return notificationAge(ts);
  }

  // kindGlyph is the source chip: one fixed glyph per kind - the icons
  // are decoration; the kind word is the screen-reader channel.
  function kindGlyph(kind: string): string {
    switch (kind) {
      case "task": return "▦";
      case "cron": return "⏱";
      case "chat": return "◈";
      default: return "·";
    }
  }

  function toggle() {
    open.value = !open.value;
    if (open.value) void refresh();
  }

  async function refresh() {
    try {
      const page = await fetchNotifications(false);
      list.value = page.notifications;
      unread.value = page.unread;
    } catch {
      // the bell degrades to an empty panel on a failed fetch
    }
  }

  async function openRecord(n: OmegaNotification, fromToast = false) {
    if (!n.read) {
      // optimistic: the row un-badges instantly, the server confirms
      const before = unread.value;
      n.read = true;
      unread.value = Math.max(0, before - 1);
      markNotificationRead(n.id).catch(() => {
        n.read = false;
        unread.value = before;
      });
    }
    if (fromToast) {
      toasts.value = toasts.value.filter((t) => t.record.id !== n.id);
    }
    open.value = false;
    emit("open", n);
  }

  async function markAll() {
    // optimistic clear (same shape as the pin/archive toggles); a
    // failed POST re-syncs from the server
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

  function pushToast(n: OmegaNotification) {
    const key = ++toastKey;
    toasts.value = [...toasts.value, { key, record: n }];
    const id = setTimeout(() => {
      timers.delete(id);
      dismiss(key);
    }, toastLifetime(n.verdict));
    timers.add(id);
  }

  function dismiss(key: number) {
    toasts.value = toasts.value.filter((t) => t.key !== key);
  }

  function onDocClick(e: MouseEvent) {
    if (open.value && root.value && !root.value.contains(e.target as Node)) {
      open.value = false;
    }
  }

  onMounted(async () => {
    void refresh();
    document.addEventListener("click", onDocClick);
    closeStream = subscribeNotifications((n) => {
      // reload truth stays with the server's unread count; this client
      // counts only NEW unread frames (a read record seen live just
      // re-renders its row)
      list.value = upsertRecord(list.value, n);
      if (!n.read) unread.value += 1;
      pushToast(n);
    });
  });

  onUnmounted(() => {
    document.removeEventListener("click", onDocClick);
    for (const id of timers) clearTimeout(id);
    closeStream?.();
  });
</script>

<style scoped>
  .notif-root {
    position: relative;
    display: inline-flex;
  }
  .notif-bell {
    position: relative;
  }
  .notif-badge {
    position: absolute;
    top: -2px;
    right: -4px;
    background: var(--accent);
    color: #181818;
    border-radius: 8px;
    font-family: var(--mono);
    font-size: 9px;
    line-height: 12px;
    min-width: 12px;
    padding: 0 3px;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }
  .notif-panel {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    z-index: 40;
    width: 300px;
    max-width: 92vw;
    background: var(--z1-raised, var(--z1));
    border: 1px solid var(--stroke);
    border-radius: var(--r-card, 8px);
    box-shadow: 0 8px 24px #00000059;
    display: flex;
    flex-direction: column;
  }
  .notif-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 10px;
    border-bottom: 1px solid var(--stroke);
  }
  .notif-title {
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 10.5px;
    font-weight: 600;
    opacity: 0.6;
  }
  .notif-clear-btn {
    font: inherit;
    font-size: 11px;
    color: var(--text-dim);
    background: 0 0;
    border: none;
    cursor: pointer;
    padding: 2px 4px;
  }
  .notif-clear-btn:hover:not(:disabled) {
    color: var(--accent);
  }
  .notif-clear-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .notif-list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 320px;
    overflow-y: auto;
  }
  .notif-row {
    display: flex;
    gap: 8px;
    align-items: baseline;
    padding: 7px 10px;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 8%, transparent);
    cursor: pointer;
  }
  .notif-row:last-child {
    border-bottom: none;
  }
  .notif-row:hover {
    background: var(--z2);
  }
  .notif-row.unread .notif-row-title {
    font-weight: 700;
  }
  .notif-row-main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }
  .notif-row-title {
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .notif-row-meta {
    display: flex;
    gap: 8px;
    font-size: 10.5px;
    font-family: var(--mono);
    opacity: 0.65;
  }
  .notif-kind {
    font-family: var(--mono);
    font-size: 12px;
    opacity: 0.7;
    flex: none;
  }
  .notif-verdict.ok {
    color: var(--ok);
    opacity: 1;
  }
  .notif-verdict.failed {
    color: var(--err);
    opacity: 1;
  }
  .notif-empty {
    padding: 14px 10px;
    text-align: center;
    opacity: 0.4;
    font-size: 11px;
  }
  .notif-toasts {
    position: fixed;
    right: 12px;
    bottom: 40px;
    z-index: 60;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .notif-toast {
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--z1-raised, var(--z1));
    border: 1px solid var(--stroke);
    border-left: 3px solid var(--ok, var(--accent));
    border-radius: var(--r-card, 8px);
    box-shadow: 0 8px 24px #00000040;
    padding: 8px 10px;
    width: 280px;
    cursor: pointer;
  }
  .notif-toast.failed {
    border-left-color: var(--err);
  }
  .notif-toast-body {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }
  .notif-toast-x {
    color: var(--text-dim);
    background: 0 0;
    border: none;
    cursor: pointer;
    font-size: 13px;
    padding: 0 2px;
  }
  .notif-toast-x:hover {
    color: var(--text);
  }
  .toast-enter-active,
  .toast-leave-active {
    transition: transform 0.15s ease-out, opacity 0.15s ease-out;
  }
  .toast-enter-from,
  .toast-leave-to {
    opacity: 0;
    transform: translateY(6px);
  }
</style>