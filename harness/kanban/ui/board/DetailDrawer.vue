<template>
  <aside
    class="drawer"
    role="dialog"
    aria-modal="false">
    <header class="drawer-head">
      <button
        v-if="prevStatus"
        class="icon-btn"
        :title="`move to ${prevStatus}`"
        @click="$emit('move', prevStatus)">
        ◀
      </button>
      <!-- card t_acad02ec (audit M1): the status selector rides the themed
           listbox (BandSelect) - a native select popup is OS chrome over
           the drawer -->
      <BandSelect
        class="drawer-status"
        :model-value="task.status"
        :options="statusOptions"
        label="task status"
        drop="down"
        @update:model-value="$emit('move', $event)" />
      <button
        v-if="nextStatus"
        class="icon-btn"
        :title="`move to ${nextStatus}`"
        @click="$emit('move', nextStatus)">
        ▶
      </button>
      <button
        class="icon-btn drawer-close"
        title="close"
        @click="$emit('close')">
        ✕
      </button>
    </header>

    <!-- round t_f81e845e: the title and body are editable in place -
         the pencil flips the title into an input, a click on the body
         flips it into a textarea; save PATCHes, esc reverts. -->
    <div
      v-if="!editingTitle"
      class="drawer-title-row">
      <h3 class="drawer-title">{{ task.title }}</h3>
      <button
        class="icon-btn drawer-edit"
        title="edit title"
        @click="beginTitleEdit">
        ✎
      </button>
    </div>
    <form
      v-else
      class="drawer-edit-form"
      @submit.prevent="saveTitle">
      <input
        ref="titleEl"
        v-model="titleDraft"
        class="drawer-edit-input"
        aria-label="edit title"
        @keydown.esc.prevent="cancelTitleEdit" />
      <button
        type="submit"
        class="drawer-save"
        :disabled="!titleDraft.trim()">save</button>
      <button
        type="button"
        class="drawer-cancel"
        @click="cancelTitleEdit">cancel</button>
    </form>

    <div class="drawer-meta">
      <BandSelect
        class="drawer-priority"
        :model-value="task.priority"
        :options="priorityOptions()"
        label="priority (higher = more urgent)"
        drop="down"
        @update:model-value="$emit('save', { priority: $event })" />
      <span
        v-if="task.assignee"
        class="drawer-assignee">@{{ task.assignee }}</span>
      <span class="drawer-age">{{ age(task.created_at) }}</span>
      <button
        v-if="task.session_id"
        class="drawer-session"
        title="open the linked conversation"
        @click="$emit('open-session', task.session_id)">
        ↗ transcript
      </button>
    </div>

    <!-- body: read-view until clicked, then the edit form (which also
         hosts the armed card-delete control, the one destructive act
         the drawer carries) -->
    <form
      v-if="editingBody"
      class="drawer-edit-body"
      @submit.prevent="saveBody">
      <textarea
        ref="bodyEl"
        v-model="bodyDraft"
        class="drawer-edit-input drawer-body-input"
        aria-label="edit body"
        @keydown.esc.prevent="cancelBodyEdit" />
      <div class="drawer-body-actions">
        <button
          type="submit"
          class="drawer-save">save</button>
        <button
          type="button"
          class="drawer-cancel"
          @click="cancelBodyEdit">cancel</button>
        <button
          type="button"
          class="drawer-discard"
          :class="{ armed: confirmingDelete }"
          @click="deleteClick">
          {{ confirmingDelete ? "confirm delete" : "delete card" }}
        </button>
      </div>
    </form>
    <pre
      v-else-if="task.body"
      class="drawer-body"
      title="click to edit the body"
      @click="beginBodyEdit">{{ task.body }}</pre>
    <p
      v-else
      class="drawer-empty drawer-empty-edit"
      title="click to add a body"
      @click="beginBodyEdit">no body (click to edit)</p>

    <div class="drawer-events">
      <p class="panel-title">events</p>
      <ol
        v-if="events.length"
        class="event-list">
        <li
          v-for="e in events"
          :key="e.id"
          class="event-row">
          <span class="event-kind">{{ e.kind }}</span>
          <span
            v-if="e.detail"
            class="event-detail">{{ e.detail }}</span>
          <span class="event-age">{{ age(e.created_at) }}</span>
        </li>
      </ol>
      <p
        v-else
        class="drawer-empty">no events</p>
    </div>

    <div class="drawer-comments">
      <p class="panel-title">comments</p>
      <ol
        v-if="comments.length"
        class="comment-list">
        <li
          v-for="c in comments"
          :key="c.id"
          class="comment-row">
          <span class="comment-author">@{{ c.author }}</span>
          <p class="comment-body">{{ c.body }}</p>
        </li>
      </ol>
      <p
        v-else
        class="drawer-empty">no comments</p>
      <form
        class="comment-form"
        @submit.prevent="$emit('comment', commentDraft), (commentDraft = '')">
        <input
          v-model="commentDraft"
          class="comment-input"
          placeholder="add comment"
          aria-label="add comment" />
        <button
          class="comment-send"
          :disabled="!commentDraft.trim()">
          reply
        </button>
      </form>
    </div>
  </aside>
</template>

<script setup lang="ts">
  import { computed, nextTick, ref, watch } from "vue";
  import {
    BOARD_STATUSES as STATUSES,
    age,
    priorityOptions,
    step,
    type BoardComment,
    type BoardEvent,
    type BoardTask,
  } from "./boardLogic";

  // The detail drawer: full body + comment thread + event trail +
  // status/priority selectors, with in-place title and body editing
  // and armed card delete (round t_f81e845e). The board owns the API
  // wiring; this piece renders one task and reports intent - moves,
  // saves, comments, delete, transcript jumps, close. BandSelect is
  // the shared themed listbox look (a shell LOOK ride like every
  // module UI keeps; no shell code imported).
  const props = defineProps<{
    task: BoardTask;
    comments: BoardComment[];
    events: BoardEvent[];
  }>();

  const emit = defineEmits<{
    (e: "move", status: string): void;
    (e: "save", patch: { title?: string; body?: string; priority?: number }): void;
    (e: "comment", body: string): void;
    (e: "delete"): void;
    (e: "open-session", id: string): void;
    (e: "close"): void;
  }>();

  // Local comment draft; a NEW task in the drawer resets it - a stale
  // draft (or a stale edit) must not leak between tasks.
  const commentDraft = ref("");
  watch(
    () => props.task.id,
    () => {
      commentDraft.value = "";
      cancelTitleEdit();
      cancelBodyEdit();
      disarmDelete();
    },
  );
  watch(
    () => props.task.title,
    // a title saved by any route (this drawer, the tool, another pane)
    // must not leave the editor holding a stale draft
    () => {
      if (editingTitle.value) cancelTitleEdit();
    },
  );

  // arrow moves are the status sequence one step left/right; at the
  // edges the buttons do not render (step returns null)
  const prevStatus = computed(() => step(props.task.status, -1));
  const nextStatus = computed(() => step(props.task.status, +1));

  // the status selector's listbox options (BandSelect): the same 9-status
  // sequence the columns carry
  const statusOptions = computed(() =>
    STATUSES.map((s) => ({ value: s, label: s })),
  );

  // --- title editing
  const editingTitle = ref(false);
  const titleDraft = ref("");
  const titleEl = ref<HTMLInputElement | null>(null);
  function beginTitleEdit() {
    titleDraft.value = props.task.title;
    editingTitle.value = true;
    void nextTick(() => titleEl.value?.focus());
  }
  function cancelTitleEdit() {
    editingTitle.value = false;
    titleDraft.value = "";
  }
  function saveTitle() {
    // the draft is captured BEFORE the editor closes (cancel empties it)
    const title = titleDraft.value.trim();
    if (!title || title === props.task.title) {
      cancelTitleEdit();
      return;
    }
    cancelTitleEdit();
    emit("save", { title });
  }

  // --- body editing
  const editingBody = ref(false);
  const bodyDraft = ref("");
  const bodyEl = ref<HTMLTextAreaElement | null>(null);
  function beginBodyEdit() {
    bodyDraft.value = props.task.body;
    editingBody.value = true;
    disarmDelete(); // the armed delete belongs to the edit form
    void nextTick(() => bodyEl.value?.focus());
  }
  function cancelBodyEdit() {
    editingBody.value = false;
    bodyDraft.value = "";
  }
  function saveBody() {
    const body = bodyDraft.value; // captured BEFORE the editor closes
    if (body === props.task.body) {
      cancelBodyEdit();
      return;
    }
    cancelBodyEdit();
    emit("save", { body });
  }

  // --- armed delete (the board-remove pattern: second click inside the
  // warning window is the trigger, the server is the law)
  const confirmingDelete = ref(false);
  let disarmTimer: ReturnType<typeof setTimeout> | undefined;
  function disarmDelete() {
    confirmingDelete.value = false;
    if (disarmTimer) {
      clearTimeout(disarmTimer);
      disarmTimer = undefined;
    }
  }
  function deleteClick() {
    if (!confirmingDelete.value) {
      confirmingDelete.value = true;
      if (disarmTimer) clearTimeout(disarmTimer);
      // disarm on timeout: the second click lands only while the
      // warning still shows
      disarmTimer = setTimeout(disarmDelete, 6000);
      return;
    }
    disarmDelete();
    emit("delete");
  }
</script>

<style scoped>
  .drawer {
    position: absolute;
    inset: 0 0 0 auto;
    width: min(360px, 92%);
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    overflow-y: auto;
    /* card t_acad02ec (audit M4): --omega-bg was a phantom token - the
       fallback said `canvas` and the drawer rendered BROWSER white in
       both themes. The real surface token (a layer-1 pane) */
    background: var(--z1);
    border-left: 1px solid color-mix(in srgb, currentcolor 12%, transparent);
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.12);
    z-index: 5;
  }
  .drawer-head {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .drawer-title-row {
    display: flex;
    align-items: baseline;
    gap: 4px;
  }
  .drawer-edit {
    opacity: 0.4;
  }
  .drawer-edit:hover {
    opacity: 0.9;
  }
  .drawer-title {
    margin: 0;
    font-size: 15px;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .drawer-edit-form {
    display: flex;
    gap: 6px;
    align-items: stretch;
  }
  .drawer-edit-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .drawer-edit-input {
    flex: 1;
    min-width: 0;
    font: inherit;
    font-size: 12px;
    color: inherit;
    background: color-mix(in srgb, currentcolor 6%, transparent);
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-card);
    padding: 5px 8px;
  }
  .drawer-body-input {
    min-height: 96px;
    resize: vertical;
  }
  .drawer-body-actions {
    display: flex;
    gap: 6px;
  }
  .drawer-save,
  .drawer-cancel {
    font: inherit;
    font-size: 11.5px;
    color: inherit;
    cursor: pointer;
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-chip);
    background: none;
    padding: 3px 8px;
  }
  .drawer-save:disabled {
    opacity: 0.4;
    cursor: default;
  }
  /* armed delete: the heavier border is the second-click warning, the
     same tinge the board-remove button carries */
  .drawer-discard {
    margin-left: auto;
    font: inherit;
    font-size: 11.5px;
    color: inherit;
    cursor: pointer;
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-chip);
    background: none;
    padding: 3px 8px;
  }
  .drawer-discard.armed {
    border-color: color-mix(in srgb, currentcolor 45%, transparent);
    font-weight: 600;
  }
  .drawer-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 11px;
    opacity: 0.75;
  }
  .drawer-priority {
    flex: none;
  }
  .drawer-priority :deep(.model-chip) {
    background: transparent;
    font-size: 11.5px;
    padding: 2px 6px;
  }
  .drawer-age,
  .drawer-assignee {
    flex: none;
  }
  .drawer-session {
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    font-size: 11px;
    cursor: pointer;
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .drawer-body {
    margin: 0;
    padding: 8px 10px;
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    border: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
    border-radius: var(--r-card);
    max-height: 40%;
    overflow-y: auto;
    cursor: text;
  }
  .drawer-empty {
    margin: 0;
    font-size: 11px;
    opacity: 0.5;
  }
  .drawer-empty-edit {
    cursor: text;
  }
  .panel-title {
    margin: 0 0 4px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.5;
  }
  .event-list {
    margin: 0 0 8px;
    padding: 0;
    list-style: none;
    max-height: 132px;
    overflow-y: auto;
  }
  .event-row {
    display: flex;
    gap: 6px;
    align-items: baseline;
    padding: 3px 0;
    font-size: 10.5px;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 6%, transparent);
  }
  .event-row:last-child {
    border-bottom: none;
  }
  .event-kind {
    flex: none;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    opacity: 0.6;
  }
  .event-detail {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .event-age {
    flex: none;
    opacity: 0.45;
  }
  .comment-list {
    margin: 0 0 8px;
    padding: 0;
    list-style: none;
  }
  .comment-row {
    padding: 6px 0;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 8%, transparent);
  }
  .comment-row:last-child {
    border-bottom: none;
  }
  .comment-author {
    font-size: 11px;
    opacity: 0.6;
  }
  .comment-body {
    margin: 2px 0 0;
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .comment-form {
    display: flex;
    gap: 6px;
    margin-top: auto;
    padding-top: 6px;
  }
  .comment-input {
    flex: 1;
    font: inherit;
    font-size: 12px;
    color: inherit;
    background: color-mix(in srgb, currentcolor 6%, transparent);
    border: 1px solid color-mix(in srgb, currentcolor 15%, transparent);
    border-radius: var(--r-card);
    padding: 5px 8px;
  }
  .comment-send {
    font: inherit;
    font-size: 12px;
    color: inherit;
    cursor: pointer;
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-chip);
    background: none;
  }
  .comment-send:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .icon-btn {
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    font-size: 12px;
    padding: 2px 4px;
  }
  /* card t_acad02ec: the status/priority selectors are the shared
     BandSelect chip - seat only (the chip keeps the shared .model-chip
     look; the pop-down escapes this root, so the root carries no box of
     its own) */
  .drawer-status {
    margin: 0 auto;
  }
  .drawer-status :deep(.model-chip) {
    background: transparent;
    font-size: 12px;
    padding: 2px 6px;
  }
</style>
