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
      <!-- shared look (SearchInput); the BEHAVIOR stays here: debounced
           FTS5 content union (runContentSearch below) on top of the
           client-side title filter -->
      <SearchInput
        v-model="query"
        placeholder="Search sessions" />
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
            @click="$emit('open', s.id)"
            @mouseenter="hoveredId = s.id"
            @mouseleave="clearHovered(s.id)">
            <!-- the ONE status element: shape = pin (dot, morphing to a
                 star when the ROW is hovered; click pins/unpins, .stop
                 never opens the row), color = state (idle grey /
                 streaming orange / unread blue). Derivation is the pure
                 sessionRowStatus. -->
            <button
              class="row-status"
              :class="statusClass(s)"
              :aria-label="s.pinned ? 'Unpin session' : 'Pin session'"
              :title="s.pinned ? 'Unpin session' : 'Pin session'"
              @click.stop="$emit('pin', s.id, !s.pinned)">
              <span
                class="glyph glyph-dot"
                aria-hidden="true"></span>
              <span
                class="glyph glyph-star"
                aria-hidden="true">&#9733;</span>
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
  import { computed, onUnmounted, ref, watch } from "vue";
  import {
    bucketSessions,
    relativeAge,
    rowMeta,
    withSearchHits,
    searchSessions,
    SESSION_SEARCH_DEBOUNCE_MS,
    createRenderScheduler,
    parseCollapsedLabels,
    serializeCollapsedKeys,
    sessionRowStatus,
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
  // ids and rank order returned by the backend's unified session
  // search (null = no query / first response not in yet). The endpoint
  // owns BOTH title and content hits - no in-memory filter runs
  // alongside it; ids the session list no longer holds drop out in
  // withSearchHits.
  const hitIds = ref<string[] | null>(null);
  const filtered = computed(() => withSearchHits(props.sessions, hitIds.value));
  const buckets = computed<SessionBuckets>(() => bucketSessions(filtered.value));

  // Search is debounced (trailing edge via the same injectable
  // scheduler as the render tick) and raced: responses for a query the
  // user already moved past never paint. Failures - 400s from FTS
  // syntax, network blips - clear the hits: the undecorated list
  // stands rather than a stale, wrong filter.
  let searchSeq = 0;
  const searchScheduler = createRenderScheduler(
    (q) => void runSearch(q),
    SESSION_SEARCH_DEBOUNCE_MS,
  );

  async function runSearch(q: string) {
    const seq = ++searchSeq;
    try {
      const hits = await searchSessions(q);
      if (seq === searchSeq) hitIds.value = hits.map((h) => h.id);
    } catch {
      if (seq === searchSeq) hitIds.value = null;
    }
  }

  watch(query, (q) => {
    const term = q.trim();
    if (!term) {
      searchSeq++; // invalidate any in-flight response
      searchScheduler.cancel();
      hitIds.value = null;
      return;
    }
    searchScheduler.schedule(term);
  });
  onUnmounted(() => searchScheduler.cancel());

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
      localStorage.setItem(COLLAPSED_KEY, serializeCollapsedKeys(next));
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

  // hoveredId drives the pin affordance: hovering the session ROW
  // anywhere (title, meta, time - not just the status element) morphs
  // an unpinned row's dot into a dim star (see sessionRowStatus).
  // Cleared on leave - only the exact row's id clears, so pointer noise
  // between rows never flickers a foreign row's star.
  const hoveredId = ref<string | null>(null);
  function clearHovered(id: string) {
    if (hoveredId.value === id) hoveredId.value = null;
  }

  function rowClasses(s: Session): Record<string, boolean> {
    return {
      active: s.id === props.activeId,
    };
  }

  function statusClass(s: Session): string[] {
    const st = sessionRowStatus(
      s,
      props.streamingIds,
      hoveredId.value === s.id,
      props.activeId,
    );
    return [`color-${st.color}`, `shape-${st.shape}`];
  }

  function age(s: Session): string {
    return relativeAge(s.updated_at);
  }
  function meta(s: Session): string {
    return rowMeta(s);
  }
</script>