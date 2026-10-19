<template>
  <section class="logs-list">
    <p
      v-if="error"
      class="panel-status">
      {{ error }}
    </p>
    <p
      v-else-if="!loaded"
      class="panel-status">
      loading
    </p>
    <template v-else>
      <div
        ref="bodyEl"
        class="logs-body">
        <template v-if="lines.length">
          <p
            v-for="(line, i) in lines"
            :key="i"
            class="logs-line">
            {{ line }}
          </p>
        </template>
        <p
          v-else
          class="panel-status">
          log file is empty
        </p>
      </div>
      <p class="logs-meta">polling every {{ POLL_MS / 1000 }}s · {{ lines.length }} lines</p>
    </template>
  </section>
</template>

<script setup lang="ts">
  import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

  // Transport-owned UI (card t_db430fea): the tail of the omega log
  // the channel's server itself writes, from GET /logs?lines=200, and
  // LIVE: a 2.5s poll re-reads the tail; no websocket infrastructure
  // for this (settled v1 decision). READ-ONLY: the endpoint is a tail
  // read - the poll never mutates anything.
  interface LogPayload {
    path: string;
    lines: string[];
  }
  const POLL_MS = 2500;
  const LINES = 200;

  const lines = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const bodyEl = ref<HTMLElement | null>(null);

  let timer: ReturnType<typeof setInterval> | null = null;

  async function poll() {
    let nextError = "";
    let next: string[] = [];
    try {
      const res = await fetch(`/logs?lines=${LINES}`);
      if (!res.ok) throw new Error(`GET /logs: ${res.status}`);
      const data: LogPayload = await res.json();
      next = data.lines ?? [];
    } catch (e) {
      nextError = e instanceof Error ? e.message : String(e);
    }
    // a transient failure never wipes the view - the error state
    // renders alongside the last good tail
    if (!nextError || lines.value.length === 0) {
      error.value = nextError;
    }
    if (next.length) lines.value = next;
    loaded.value = true;
  }

  // logs follow: the tail sticks to the bottom while the reader is at
  // the bottom; scrolled up (reading history), the update does not
  // yank the viewport
  async function follow() {
    const el = bodyEl.value;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 20;
    await nextTick();
    if (atBottom) el.scrollTop = el.scrollHeight;
  }

  watch(lines, follow, { flush: "post" });

  onMounted(() => {
    void poll();
    timer = setInterval(poll, POLL_MS);
  });

  onBeforeUnmount(() => {
    if (timer !== null) clearInterval(timer);
  });
</script>

<style scoped>
  .logs-list {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    padding: 0;
  }
  .panel-status {
    margin: 0;
    padding: 10px 12px;
    font-size: 11px;
    opacity: 0.6;
  }
  .logs-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 10px 12px;
  }
  .logs-line {
    margin: 0;
    font-family: var(--mono);
    font-size: 10.5px;
    line-height: 1.5;
    white-space: break-spaces;
    word-break: break-all;
    opacity: 0.75;
  }
  .logs-meta {
    margin: 0;
    padding: 4px 12px 8px;
    font-size: 10px;
    opacity: 0.45;
  }
</style>