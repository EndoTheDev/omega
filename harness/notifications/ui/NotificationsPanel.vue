<template>
  <!--
    the notifications tab - the DURABLE feed/history side (toasts are the transient side).
    Same data (GET /notifications), same rendering voice, same row contract: a click
    marks the record read and hands the open intent to app.vue.
  -->
  <div class="notif-pane">
    <!--
      the head row is the UNIVERSAL band now (the shared TabBand) - the mark-all-read
      action rides the band and the band renders before the.notif-list scrollport:
      scrolling the feed never moves it.
    -->
    <TabBand title="Notifications">
      <!-- both band actions are ICON buttons now. -->
      <button
        class="band-tool"
        title="mark all read"
        aria-label="mark all read"
        :disabled="notificationsUnread === 0"
        @click="markAll">
        <Icon name="mailOpen" :size="13" />
      </button>
      <!-- the clear-all: the DELETE side of the surface. -->
      <button
        class="band-tool"
        title="clear all"
        aria-label="clear all"
        :disabled="clearingAll || notificationsList.length === 0"
        @click="clearAllRecords">
        <Icon name="trash" :size="13" />
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
    clearAll,
    notificationAge,
    refreshNotifications,
    startNotificationFeed,
  } from "~/components/shell/notificationFeed";
  import {
    approveDenyOptions,
    fetchVerdicts,
    isSettledRace,
    postVerdict,
    type PendingVerdictCard,
  } from "~/composables/omega";
  import type { OmegaNotification } from "~/composables/notifications";

  const emit = defineEmits<{
    // navigation intent: app.vue routes task -> board, cron -> the cron inspector.
    (e: "open", n: OmegaNotification): void;
  }>();

  // shortId trims a session id to its first 8 chars for the verdict row.
  function shortId(id: string): string {
    return id && id.length > 8 ? id.slice(0, 8) : id;
  }

  // kindGlyph is the source chip (the old bell row's vocabulary).
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

  // openVerdict routes a pending-card click to the card's chat session, reusing.
  function openVerdict(p: PendingVerdictCard) {
    emit("open", {
      kind: "chat",
      source_id: p.session_id,
      title: p.title,
    } as OmegaNotification);
  }

  // gate questions, answerable: approve/deny on the notification surface --- the pair
  // contract lives on the card's options; buttons render only when the pending card IS
  // an approve/deny question. resolvedVerdicts: this surface's flip after a POST (win
  // or a lost race) - the render collapses to the resolved state until the poll drops
  // the settled card out of the pending list.
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

  const clearingAll = ref(false);
  async function clearAllRecords(): Promise<void> {
    if (clearingAll.value) return;
    clearingAll.value = true;
    try {
      await clearAll();
      await refreshNotifications();
    } finally {
      clearingAll.value = false;
    }
  }

  onMounted(async () => {
    // the activation: the outlet mounts this panel exactly when the notifications tab.
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
  /*
   * the head is the shared TabBand - the band's own geometry
   * replaced.notif-head/.notif-title.: the two actions are icon-only band-tools - no
   * local look survives, the shared.band-tool contract (hover, disabled, centering)
   * covers them; the icons pin their size at the call site.
   */
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