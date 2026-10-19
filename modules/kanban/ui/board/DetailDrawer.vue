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
      <select
        class="drawer-status"
        :value="task.status"
        aria-label="task status"
        @change="$emit('move', ($event.target as HTMLSelectElement).value)">
        <option
          v-for="s in STATUSES"
          :key="s"
          :value="s">
          {{ s }}
        </option>
      </select>
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

    <h3 class="drawer-title">{{ task.title }}</h3>
    <div class="drawer-meta">
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

    <pre
      v-if="task.body"
      class="drawer-body">{{ task.body }}</pre>
    <p
      v-else
      class="drawer-empty">no body</p>

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
  import { computed, ref, watch } from "vue";
  import {
    BOARD_STATUSES as STATUSES,
    age,
    step,
    type BoardComment,
    type BoardTask,
  } from "./boardLogic";

  // The detail drawer: full body + comment thread + status selector.
  // The board owns the API wiring; this piece renders one task and
  // reports intent - moves, comments, transcript jumps, close.
  const props = defineProps<{
    task: BoardTask;
    comments: BoardComment[];
    loading?: boolean;
  }>();

  defineEmits<{
    (e: "move", status: string): void;
    (e: "comment", body: string): void;
    (e: "open-session", id: string): void;
    (e: "close"): void;
  }>();

  // Local comment draft; a NEW task in the drawer resets it - a stale
  // draft must not leak between tasks.
  const commentDraft = ref("");
  watch(
    () => props.task.id,
    () => {
      commentDraft.value = "";
    },
  );

  // arrow moves are the status sequence one step left/right; at the
  // edges the buttons do not render (step returns null)
  const prevStatus = computed(() => step(props.task.status, -1));
  const nextStatus = computed(() => step(props.task.status, +1));
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
    background: var(--omega-bg, canvas);
    border-left: 1px solid color-mix(in srgb, currentcolor 12%, transparent);
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.12);
    z-index: 5;
  }
  .drawer-head {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .drawer-title {
    margin: 0;
    font-size: 15px;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .drawer-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 11px;
    opacity: 0.75;
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
    border-radius: 6px;
    max-height: 40%;
    overflow-y: auto;
  }
  .drawer-empty {
    margin: 0;
    font-size: 11px;
    opacity: 0.5;
  }
  .panel-title {
    margin: 0 0 4px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.5;
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
    border-radius: 6px;
    padding: 5px 8px;
  }
  .comment-send {
    font: inherit;
    font-size: 12px;
    color: inherit;
    cursor: pointer;
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: 6px;
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
  .drawer-status {
    margin: 0 auto;
    font: inherit;
    font-size: 12px;
    color: inherit;
    background: color-mix(in srgb, currentcolor 6%, transparent);
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: 6px;
  }
</style>