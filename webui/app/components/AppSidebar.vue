<template>
  <aside
    class="sessions"
    :class="{ open: open }">
    <div class="sessions-head">
      <span class="head-actions">
        <button
          class="new-btn"
          @click="$emit('new')"
          title="New conversation">
          + new
        </button>
      </span>
    </div>
    <ul class="session-list">
      <li
        v-for="s in sessions"
        :key="s.id"
        :class="{ active: s.id === activeId, streaming: streamingIds.has(s.id) }"
        @click="$emit('open', s.id)">
        <span class="session-title">{{ s.label || s.id }}</span>
        <span class="session-time">{{ shortTime(s.updated_at) }}</span>
      </li>
    </ul>
  </aside>
</template>

<script setup lang="ts">
  defineProps<{
    sessions: Session[];
    activeId: string | null;
    // ids of sessions whose stream is live - the pulse edge renders per row
    streamingIds: ReadonlySet<string>;
    open: boolean;
  }>();

  defineEmits<{
    (e: "open", id: string): void;
    (e: "new"): void;
  }>();

  function shortTime(iso: string): string {
    try {
      return new Date(iso).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
    } catch {
      return "";
    }
  }
</script>
