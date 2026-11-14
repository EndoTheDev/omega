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
    // navigation intent: app.vue routes task -> board, cron -> the
    // cron inspector tab, chat -> the session when one exists (the
    // openNotification router the bell's rows used; the toast keeps it).
    // ONE event carries both toast shapes - the payload discriminates
    // (`kind` on a record, `session_id` on a pending approval, card
    // t_a2ed8412), so the menubar keeps its single forward and app.vue
    // stays the only router.
    (e: "open", n: OmegaNotification | PendingVerdictCard): void;
  }>();

  async function open(t: NotificationToast) {
    // the approval toast (card t_a2ed8412): its click opens the
    // notifications tab, where the pending card is answerable
    if (t.kind === "verdict") {
      emit("open", openToastVerdict(t));
      return;
    }
    emit("open", await openToastRecord(t));
  }

  function title(t: NotificationToast): string {
    return t.kind === "verdict" ? t.card.title : t.record.title;
  }

  // meta is the row's second line: a record reports its verdict + age, an
  // approval ASKS ("approval requested") - there is no verdict to print
  // yet, and the age of a question is the wrong fact.
  function meta(t: NotificationToast): string {
    return t.kind === "verdict"
      ? "approval requested"
      : `${t.record.verdict} · ${notificationAge(t.record.created_at)}`;
  }

  // glyph is the source chip (the old bell row's vocabulary): one fixed
  // glyph per kind - the icons are decoration; the kind word is the
  // screen-reader channel. A pending approval takes the verdict card's own
  // mark ("?", the same head GateCard renders).
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
  /* card t_a2ed8412: an approval is a QUESTION, not a report - the accent
     rail (never the error red), so it reads as needing the user */
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