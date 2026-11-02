<template>
  <!-- card t_180db2cb: the notifications tab - the DURABLE feed/history
       side of the bell ruling. The bell popover died; its panel body
       (head, pending verdicts, the feed list) rehomed here as the
       inspector pane's content. Same data (GET /notifications), same
       rendering voice, same row contract: a click marks the record read
       and hands the open intent to app.vue. Opening/focusing the tab
       marks the feed read (markAllRead) - the tab chip's unread badge
       clears on open. Transient toasts are NOT this component's job:
       they stay shell-level in NotificationToasts.vue. -->
  <div class="notif-pane">
    <!-- card t_308e6d9b: the head row is the UNIVERSAL band now (the
         shared TabBand) - the mark-all-read action rides the band and
         the band renders before the .notif-list scrollport: scrolling
         the feed never moves it. -->
    <TabBand title="Notifications">
      <button
        class="notif-clear-btn band-tool"
        :disabled="notificationsUnread === 0"
        @click="markAll">
        mark all read
      </button>
    </TabBand>
    <ul
      v-if="pendingVerdictList.length"
      class="notif-verdicts"
      role="list">
      <li
        v-for="p in pendingVerdictList"
        :key="p.id"
        class="notif-row verdict"
        @click="openVerdict(p)">
        <span class="notif-kind" data-kind="verdict">?</span>
        <span class="notif-row-main">
          <span class="notif-row-title">{{ p.title }}</span>
          <span class="notif-row-meta">
            <!-- card t_63204dfd: the notification surface answers a gate
                 question with approve/deny only (the pair contract on
                 the card's options). The row click still jumps to the
                 session; the buttons answer the SAME pending card the
                 chat card answers - first resolution wins, a losing
                 click collapses (no error, no double-apply). -->
            <template v-if="resolvedVerdicts.has(p.id)">
              <span class="notif-verdict resolved">{{ resolvedVerdicts.get(p.id) }}</span>
            </template>
            <template v-else-if="answerOptionsFor(p)">
              <span class="notif-verdict pending">pending question</span>
              <span class="notif-answer-btns">
                <button
                  class="notif-answer-btn"
                  :disabled="answeringId === p.id"
                  @click.stop="answerQuestion(p, 'approve')">approve</button>
                <button
                  class="notif-answer-btn"
                  :disabled="answeringId === p.id"
                  @click.stop="answerQuestion(p, 'deny')">deny</button>
              </span>
            </template>
            <template v-else>
              <span class="notif-verdict pending">pending question</span>
            </template>
            <span class="notif-age">session {{ shortId(p.session_id) }}</span>
          </span>
        </span>
      </li>
    </ul>

    <ul class="notif-list" role="list">
      <li
        v-for="n in notificationsList"
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
      <li v-if="notificationsList.length === 0" class="notif-empty">
        nothing finished yet
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
  import { onMounted, reactive, ref } from "vue";
  import {
    notificationsList,
    notificationsUnread,
    pendingVerdictList,
    markAllRead,
    markRecordRead,
    notificationAge,
    refreshNotifications,
    startNotificationFeed,
  } from "./store";
  import {
    approveDenyOptions,
    fetchVerdicts,
    isSettledRace,
    postVerdict,
    type PendingVerdictCard,
  } from "../../composables/omega";
  import type { OmegaNotification } from "../../composables/notifications";

  const emit = defineEmits<{
    // navigation intent: app.vue routes task -> board, cron -> the
    // cron inspector tab, chat -> the session when one exists (the
    // openNotification router the bell's rows used)
    (e: "open", n: OmegaNotification): void;
  }>();

  // shortId trims a session id to its first 8 chars for the verdict row.
  function shortId(id: string): string {
    return id && id.length > 8 ? id.slice(0, 8) : id;
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

  // openVerdict routes a pending-card click to the card's chat session,
  // reusing the existing chat-nav intent (kind=chat, source_id=session).
  function openVerdict(p: PendingVerdictCard) {
    emit("open", {
      kind: "chat",
      source_id: p.session_id,
      title: p.title,
    } as OmegaNotification);
  }

  // --- gate questions, answerable (card t_63204dfd): approve/deny on
  // the notification surface --- the pair contract lives on the card's
  // options; buttons render only when the pending card IS an
  // approve/deny question. The body click (unstopped) still jumps to
  // the session - the buttons answer in place.
  // resolvedVerdicts: this surface's flip after a POST (win or a lost
  // race) - the render collapses to the resolved state until the poll
  // drops the settled card out of the pending list.
  function answerOptionsFor(p: PendingVerdictCard) {
    return approveDenyOptions(p.options);
  }
  const resolvedVerdicts = reactive(new Map<string, string>());
  const answeringId = ref("");
  async function answerQuestion(p: PendingVerdictCard, kind: "approve" | "deny"): Promise<void> {
    const ix = approveDenyOptions(p.options);
    if (!ix || resolvedVerdicts.has(p.id) || answeringId.value) return;
    answeringId.value = p.id;
    try {
      await postVerdict(p.session_id, {
        card_id: p.id,
        option_index: kind === "approve" ? ix.approve : ix.deny,
      });
      resolvedVerdicts.set(p.id, "resolved: " + kind);
    } catch (e) {
      // a late click on an already-resolved question renders the
      // resolved stamp, no error, no double-apply (first resolution
      // won on the chat surface, another tab, or the CLI)
      const msg = e instanceof Error ? e.message : String(e);
      if (isSettledRace(msg)) resolvedVerdicts.set(p.id, "resolved (another surface)");
    } finally {
      answeringId.value = "";
    }
    void refreshNotifications();
  }

  async function openRecord(n: OmegaNotification) {
    await markRecordRead(n);
    emit("open", n);
  }

  async function markAll() {
    await markAllRead();
  }

  onMounted(async () => {
    // the activation: the outlet mounts this panel exactly when the
    // notifications tab gains focus (DockPane renders only the active
    // tab), so the mount IS the open/focus event. The badge clears on
    // open: pull the fresh truth first (the feed may have moved since
    // the store's poll), then mark everything read in one POST.
    startNotificationFeed();
    await refreshNotifications();
    if (notificationsUnread.value > 0) {
      await markAllRead();
    }
  });
</script>

<style scoped>
  .notif-pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
  }
  /* card t_308e6d9b: the head is the shared TabBand - the band's own
     geometry replaced .notif-head/.notif-title; the mark-all-read
     button keeps its text-button look (the band-tool font rules ride
     it, its accent hover stays). */
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
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .notif-verdicts {
    list-style: none;
    margin: 0;
    padding: 0;
    border-bottom: 1px solid var(--stroke);
    flex: none;
  }
  .notif-row.verdict .notif-kind {
    color: var(--accent, #e8833a);
    font-weight: 700;
  }
  .notif-verdict.pending {
    color: var(--accent, #e8833a);
  }
  /* card t_63204dfd: the notification surface's approve/deny buttons
     and its resolved flip */
  .notif-answer-btns {
    display: inline-flex;
    gap: 4px;
  }
  .notif-answer-btn {
    font: inherit;
    font-size: 10.5px;
    font-family: var(--mono);
    color: var(--text);
    background: var(--z0);
    border: 1px solid var(--stroke, #444);
    border-radius: 4px;
    padding: 1px 8px;
    cursor: pointer;
  }
  .notif-answer-btn:hover:not(:disabled) {
    border-color: var(--accent, #e8833a);
  }
  .notif-answer-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .notif-verdict.resolved {
    color: var(--text-dim);
    font-style: italic;
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
  .notif-age {
    font-family: var(--mono);
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
</style>