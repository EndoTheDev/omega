<template>
  <!-- card t_180db2cb: the toast stack - the SURVIVOR of the bell ruling.
       The bell button + dropdown died (the durable feed is the
       notifications dock tab); the transient toasts STAY for live events
       (Endo ruling 2026-10-29, toast merge 12833fd behavior intact): one
       toast per live canvas frame, failed verdicts stay longer
       (toastLifetime). A toast's click is a row click: mark read +
       hand the open intent to app.vue. position:fixed keeps the stack
       bottom-right regardless of the host. All state is the
       page-lifetime store's - this component only starts the feed (the
       first mount lights it) and renders. -->
  <transition-group name="toast" tag="div" class="notif-toasts">
    <div
      v-for="t in notificationToasts"
      :key="t.key"
      class="notif-toast"
      :class="{ failed: t.record.verdict === 'failed' }"
      role="status"
      @click="open(t)">
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
</template>

<script setup lang="ts">
  import { onMounted, onUnmounted } from "vue";
  import {
    notificationToasts,
    notificationAge,
    dismissToast,
    openToastRecord,
    startNotificationFeed,
    stopNotificationFeed,
  } from "./store";
  import type { OmegaNotification } from "../../composables/notifications";

  const emit = defineEmits<{
    // navigation intent: app.vue routes task -> board, cron -> the
    // cron inspector tab, chat -> the session when one exists (the
    // openNotification router the bell's rows used; the toast keeps it)
    (e: "open", n: OmegaNotification): void;
  }>();

  async function open(t: { key: number; record: OmegaNotification }) {
    emit("open", await openToastRecord(t));
  }

  // kindGlyph is the source chip (the old bell row's vocabulary): one
  // fixed glyph per kind - the icons are decoration; the kind word is
  // the screen-reader channel.
  function kindGlyph(kind: string): string {
    switch (kind) {
      case "task": return "▦";
      case "cron": return "⏱";
      case "chat": return "◈";
      default: return "·";
    }
  }

  function age(ts: string): string {
    return notificationAge(ts);
  }

  onMounted(() => {
    // the feed's first mount usually lands here (the menubar is shell
    // chrome - always present); the panel's mount is a no-op then
    startNotificationFeed();
  });
  onUnmounted(() => {
    stopNotificationFeed();
  });
</script>

<style scoped>
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