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
    <div class="session-search">
      <input
        v-model="query"
        type="search"
        placeholder="Search sessions"
        spellcheck="false" />
    </div>
    <ul class="session-list">
      <!-- sections: PINNED first (the keep-at-top group), then the
           Today / Yesterday / Earlier time buckets. Bucketing, filtering
           and formatting are pure functions in composables/omega.ts,
           unit-tested there. -->
      <template
        v-for="section in visibleSections"
        :key="section.label">
        <li class="bucket-row">
          <button
            class="bucket-label"
            :aria-expanded="!collapsed.has(section.label)"
            @click="toggleCollapsed(section.label)">
            <svg
              class="bucket-chevron"
              :class="{ open: !collapsed.has(section.label) }"
              width="8"
              height="8"
              viewBox="0 0 8 8"
              aria-hidden="true">
              <path
                d="M2.5 1L6 4L2.5 7"
                fill="none"
                stroke="currentColor"
                stroke-width="1.2" />
            </svg>
            {{ section.label }}
          </button>
        </li>
        <template v-if="!collapsed.has(section.label)">
          <li
            v-for="s in section.rows"
            :key="s.id"
            :class="rowClasses(s)"
            @click="$emit('open', s.id)">
            <button
              class="pin-btn"
              :class="{ pinned: s.pinned }"
              :title="s.pinned ? 'Unpin session' : 'Pin session'"
              @click.stop="$emit('pin', s.id, !s.pinned)">
              &#9733;
            </button>
            <span class="session-title">{{ s.label || s.id }}</span>
            <span
              v-if="meta(s)"
              class="session-meta">{{ meta(s) }}</span>
            <span
              v-if="age(s)"
              class="session-time">{{ age(s) }}</span>
          </li>
        </template>
      </template>
      <li
        v-if="allEmpty"
        class="bucket-empty">
        no matching sessions
      </li>
    </ul>
  </aside>
</template>

<script setup lang="ts">
  import { computed, ref } from "vue";
  import {
    bucketSessions,
    relativeAge,
    rowMeta,
    filterSessions,
    parseCollapsedLabels,
    serializeCollapsedLabels,
  } from "../composables/omega";
  import type { Session, SessionBuckets } from "../composables/omega";

  const props = defineProps<{
    sessions: Session[];
    activeId: string | null;
    // ids of sessions whose stream is live - the pulse edge renders per row
    streamingIds: ReadonlySet<string>;
    open: boolean;
  }>();

  defineEmits<{
    (e: "open", id: string): void;
    (e: "new"): void;
    (e: "pin", id: string, pinned: boolean): void;
  }>();

  const query = ref("");
  const filtered = computed(() => filterSessions(props.sessions, query.value));
  const buckets = computed<SessionBuckets>(() => bucketSessions(filtered.value));

  // --- collapsible sections ---
  // collapsed holds the labels of sections whose rows are hidden. The set
  // is swapped value-semantically on toggle (same mechanic as busySet) so
  // the computed render reacts. Restored from localStorage on mount -
  // unknown/malformed data restores everything expanded (see
  // parseCollapsedLabels); localStorage stays here, the helpers stay pure.
  const COLLAPSED_KEY = "omega.sidebar-collapsed";
  const collapsed = ref<Set<string>>(new Set());

  function restoreCollapsed() {
    if (typeof window === "undefined") return;
    collapsed.value = parseCollapsedLabels(localStorage.getItem(COLLAPSED_KEY));
  }

  function toggleCollapsed(label: string) {
    const next = new Set(collapsed.value);
    if (next.has(label)) next.delete(label);
    else next.add(label);
    collapsed.value = next;
    if (typeof window !== "undefined")
      localStorage.setItem(COLLAPSED_KEY, serializeCollapsedLabels(next));
  }
  restoreCollapsed();

  // non-empty sections in render order; empty groups render no header
  const visibleSections = computed(() => {
    const b = buckets.value;
    return [
      { label: "pinned", rows: b.pinned },
      { label: "today", rows: b.today },
      { label: "yesterday", rows: b.yesterday },
      { label: "earlier", rows: b.earlier },
    ].filter((section) => section.rows.length > 0);
  });
  const allEmpty = computed(() => visibleSections.value.length === 0);

  function rowClasses(s: Session): Record<string, boolean> {
    return {
      active: s.id === props.activeId,
      streaming: props.streamingIds.has(s.id),
    };
  }

  function age(s: Session): string {
    return relativeAge(s.updated_at);
  }
  function meta(s: Session): string {
    return rowMeta(s);
  }
</script>