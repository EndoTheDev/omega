<template>
  <transition-group name="toast" tag="div" class="notif-toasts">
    <div
      v-for="t in notificationToasts"
      :key="t.key"
      class="notif-toast"
      :class="{ failed: t.kind === 'record' && t.record.verdict === 'failed', approval: t.kind === 'verdict' }"
      role="status"
      @click="open(t)">
      <span class="notif-kind" :data-kind="t.kind === 'record' ? t.record.kind : 'verdict'">{{ glyph(t) }}</span>
      <span class="notif-toast-body">
        <span class="notif-row-title">{{ title(t) }}</span>
        <span class="notif-row-meta">{{ meta(t) }}</span>
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
    openToastVerdict,
    startNotificationFeed,
    stopNotificationFeed,
    type NotificationToast,
  } from "./notificationFeed";
  import type { OmegaNotification } from "../../composables/notifications";
  import type { PendingVerdictCard } from "../../composables/omega";

  const emit = defineEmits<{
    // navigation intent: app.vue routes task -> board, cron -> the cron inspector.
    (e: "open", n: OmegaNotification | PendingVerdictCard): void;
  }>();

  async function open(t: NotificationToast) {
    if (t.kind === "verdict") {
      emit("open", openToastVerdict(t));
      return;
    }
    emit("open", await openToastRecord(t));
  }

  function title(t: NotificationToast): string {
    return t.kind === "verdict" ? t.card.title : t.record.title;
  }

  function meta(t: NotificationToast): string {
    return t.kind === "verdict"
      ? "approval requested"
      : `${t.record.verdict} · ${notificationAge(t.record.created_at)}`;
  }

  function glyph(t: NotificationToast): string {
    if (t.kind === "verdict") return "?";
    switch (t.record.kind) {
      case "task": return "▦";
      case "cron": return "⏱";
      case "chat": return "◈";
      default: return "·";
    }
  }

  onMounted(() => {
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
  /*
   * an approval is a QUESTION, not a report - the accent rail (never the error red).
   */
  .notif-toast.approval {
    border-left-color: var(--accent);
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