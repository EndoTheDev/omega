<template>
  <section class="board">
    <div class="board-scroll">
      <div
        v-for="(col, i) in cols"
        :key="BOARD_STATUSES[i]"
        class="board-col"
        :class="{
          settled: isSettled(BOARD_STATUSES[i]),
          folded: isSettled(BOARD_STATUSES[i]) && !expanded.has(BOARD_STATUSES[i]),
        }">
        <header
          class="col-head"
          @click="isSettled(BOARD_STATUSES[i]) && toggleFold(BOARD_STATUSES[i])">
          <span class="col-name">{{ BOARD_STATUSES[i] }}</span>
          <span class="col-count">{{ col.length }}</span>
          <button
            v-if="isSettled(BOARD_STATUSES[i])"
            class="icon-btn col-fold-btn"
            :title="expanded.has(BOARD_STATUSES[i]) ? 'fold column' : 'unfold column'"
            @click.stop="toggleFold(BOARD_STATUSES[i])">
            {{ expanded.has(BOARD_STATUSES[i]) ? "▾" : "▸" }}
          </button>
        </header>

        <form
          v-if="i <= 1"
          class="col-add"
          :class="{ open: drafts[BOARD_STATUSES[i]].length > 0 }"
          @submit.prevent="submitAdd(BOARD_STATUSES[i])">
          <input
            v-model="drafts[BOARD_STATUSES[i]]"
            class="col-add-input"
            :placeholder="`new task in ${BOARD_STATUSES[i]}`"
            :aria-label="`new task in ${BOARD_STATUSES[i]}`" />
          <button
            class="col-add-btn"
            :disabled="drafts[BOARD_STATUSES[i]].trim() === ''">
            +
          </button>
        </form>

        <div
          v-if="!isSettled(BOARD_STATUSES[i]) || expanded.has(BOARD_STATUSES[i])"
          class="col-body">
          <article
            v-for="t in col"
            :key="t.id"
            class="card"
            @click="openDetail(t)">
            <div class="card-title">{{ t.title }}</div>
            <div class="card-meta">
              <span
                v-if="t.assignee"
                class="card-assignee">@{{ t.assignee }}</span>
              <span class="card-age">{{ age(t.created_at) }}</span>
              <span class="card-moves">
                <button
                  v-if="step(BOARD_STATUSES[i], -1)"
                  class="icon-btn"
                  :title="`move to ${step(BOARD_STATUSES[i], -1)}`"
                  @click.stop="moveBoard(t, step(BOARD_STATUSES[i], -1) as string)">
                  ◀
                </button>
                <button
                  v-if="step(BOARD_STATUSES[i], +1)"
                  class="icon-btn"
                  :title="`move to ${step(BOARD_STATUSES[i], +1)}`"
                  @click.stop="moveBoard(t, step(BOARD_STATUSES[i], +1) as string)">
                  ▶
                </button>
                <button
                  v-if="t.session_id"
                  class="icon-btn card-link"
                  title="open the linked conversation"
                  @click.stop="$emit('open-session', t.session_id)">
                  ↗
                </button>
              </span>
            </div>
          </article>
          <p
            v-if="col.length === 0"
            class="col-empty">
            empty
          </p>
        </div>
        <p
          v-else
          class="col-folded-note">
          {{ col.length }} folded
        </p>
      </div>
    </div>

    <DetailDrawer
      v-if="selected"
      :task="selected"
      :comments="detailComments"
      :loading="detailLoading"
      @move="moveSelected"
      @comment="submitComment"
      @open-session="(id: string) => $emit('open-session', id)"
      @close="selected = null" />
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
  import DetailDrawer from "./board/DetailDrawer.vue";
  import {
    BOARD_STATUSES,
    age,
    columns,
    isSettled,
    step,
    type BoardComment,
    type BoardTask,
  } from "./board/boardLogic";

  // The module-owned board view: 9 hermes columns in one horizontal
  // scroll, self-contained API wiring (fetch /tasks) so it rides the
  // desktop / VS Code builds alone, like the tools canary did.
  defineEmits<{
    (e: "open-session", id: string): void;
  }>();

  const tasks = ref<BoardTask[]>([]);
  const error = ref("");

  const POLLS_MS = 5000;
  let timer: ReturnType<typeof setInterval> | undefined;

  async function fetchBoard() {
    try {
      const res = await fetch("/tasks");
      if (!res.ok) throw new Error(`GET /tasks: ${res.status}`);
      const body = await res.json();
      tasks.value = body.tasks ?? [];
      error.value = "";
      // keep the open drawer honest: if the drawn task is gone or moved,
      // refresh the drawer's view of it
      if (selected.value) applyDetailPatch(selected.value.id);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  const cols = computed(() => columns(tasks.value));

  // --- add-task: triage and todo only (a board always has a spillway)
  const drafts = reactive<Record<string, string>>({ triage: "", todo: "" });

  async function submitAdd(status: string) {
    const title = drafts[status].trim();
    if (!title) return;
    try {
      const res = await fetch("/tasks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title, status }),
      });
      if (!res.ok) throw new Error(`POST /tasks: ${res.status}`);
      drafts[status] = "";
      await fetchBoard();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // --- moves: arrow buttons and the drawer's status selector
  async function moveBoard(t: BoardTask, status: string) {
    try {
      const res = await fetch(`/tasks/${encodeURIComponent(t.id)}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
      if (!res.ok) throw new Error(`PATCH /tasks/${t.id}: ${res.status}`);
      await fetchBoard();
      if (selected.value?.id === t.id) await loadDetail(t.id);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // --- detail drawer
  const selected = ref<BoardTask | null>(null);
  const detailComments = ref<BoardComment[]>([]);
  const detailLoading = ref(false);

  function openDetail(t: BoardTask) {
    selected.value = { ...t, body: t.body };
    detailComments.value = [];
    loadDetail(t.id);
  }

  async function loadDetail(id: string) {
    detailLoading.value = true;
    try {
      const res = await fetch(`/tasks/${encodeURIComponent(id)}`);
      if (!res.ok) throw new Error(`GET /tasks/${id}: ${res.status}`);
      const d = await res.json();
      selected.value = d.task;
      detailComments.value = d.comments ?? [];
      // fold the fresh detail back into the board row - a move in the
      // drawer must not wait for the next poll to leave the column
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = d.task;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    detailLoading.value = false;
  }

  // poll-time drawer refresh: pull the drawn task's detail without
  // spinning the loading flag (the user may be typing a comment)
  async function applyDetailPatch(id: string) {
    try {
      const res = await fetch(`/tasks/${encodeURIComponent(id)}`);
      if (!res.ok) return;
      const d = await res.json();
      selected.value = d.task;
      detailComments.value = d.comments ?? [];
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = d.task;
    } catch {
      // poll noise stays silent - the next poll retries
    }
  }

  function moveSelected(status: string) {
    if (selected.value) moveBoard(selected.value, status);
  }

  async function submitComment(body: string) {
    if (!selected.value || !body.trim()) return;
    try {
      const res = await fetch(
        `/tasks/${encodeURIComponent(selected.value.id)}/comments`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ body }),
        },
      );
      if (!res.ok) throw new Error(`POST comments: ${res.status}`);
      await loadDetail(selected.value.id);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // --- settled columns fold to their header; unfolding is a toggle
  const expanded = reactive(new Set<string>());
  function toggleFold(status: string) {
    if (expanded.has(status)) expanded.delete(status);
    else expanded.add(status);
    // reactive(Set) delete/add both trigger - no reassignment needed
    expanded.has(status); // touch: keeps reactivity read honest
  }

  onMounted(() => {
    fetchBoard();
    timer = setInterval(fetchBoard, POLLS_MS);
  });
  onUnmounted(() => {
    if (timer) clearInterval(timer);
  });
</script>

<style scoped>
  .board {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    position: relative;
    overflow: hidden;
  }
  .board-scroll {
    display: flex;
    gap: 10px;
    overflow-x: auto;
    overflow-y: hidden;
    padding: 10px 12px;
    flex: 1;
    min-height: 0;
  }
  .board-col {
    flex: 0 0 236px;
    min-width: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
    border-radius: 8px;
    padding: 8px;
    gap: 8px;
  }
  .board-col.settled {
    opacity: 0.65;
  }
  .board-col.folded .col-count {
    opacity: 0.5;
  }
  .col-head {
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: default;
  }
  .settled .board-col:hover .col-fold-btn,
  .col-fold-btn {
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    font-size: 11px;
    padding: 0 2px;
  }
  .col-name {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.65;
  }
  .col-count {
    margin-left: auto;
    font-size: 11px;
    opacity: 0.5;
  }
  .col-folded-note {
    margin: 0;
    font-size: 11px;
    opacity: 0.4;
  }
  .col-add {
    display: flex;
    gap: 4px;
  }
  .col-add-input {
    flex: 1;
    min-width: 0;
    font: inherit;
    font-size: 12px;
    color: inherit;
    background: color-mix(in srgb, currentcolor 6%, transparent);
    border: 1px dashed color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: 6px;
    padding: 5px 8px;
  }
  .col-add-btn {
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: 6px;
    background: none;
    color: inherit;
    cursor: pointer;
    font: inherit;
    font-size: 12px;
    padding: 4px 8px;
  }
  .col-add-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .col-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-y: auto;
    min-height: 0;
  }
  .col-empty {
    margin: 0;
    font-size: 11px;
    opacity: 0.35;
    text-align: center;
  }
  .card {
    border: 1px solid color-mix(in srgb, currentcolor 12%, transparent);
    border-radius: 6px;
    padding: 7px 9px;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .card:hover {
    border-color: color-mix(in srgb, currentcolor 22%, transparent);
  }
  .card-title {
    font-size: 12px;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
  .card-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 10.5px;
    opacity: 0.65;
  }
  .card-age,
  .card-assignee {
    flex: none;
  }
  .card-moves {
    margin-left: auto;
    display: flex;
    gap: 2px;
  }
  .icon-btn {
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    font-size: 11px;
    padding: 0 3px;
  }
</style>