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
            <!-- hover action cluster: archive (box) + delete (trash),
                 same right-edge slot the age text vacates on hover -->
            <span
              class="row-actions"
              @mouseenter="hoveredId = s.id">
              <button
                class="icon-btn row-act"
                :title="s.archived ? 'Unarchive session' : 'Archive session'"
                :aria-label="s.archived ? 'Unarchive session' : 'Archive session'"
                @click.stop="$emit('archive', s.id, !s.archived)">
                <svg
                  v-if="!s.archived"
                  width="13"
                  height="13"
                  viewBox="0 0 16 16"
                  aria-hidden="true">
                  <path
                    d="M2 4.5h12M5.5 4.5V3h5v1.5M4 4.5l.8 8h6.4l.8-8M8 8v3.5"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.2" />
                </svg>
                <svg
                  v-else
                  width="13"
                  height="13"
                  viewBox="0 0 16 16"
                  aria-hidden="true">
                  <path
                    d="M2 4.5h12M5.5 4.5V3h5v1.5M4 4.5l.8 8h6.4l.8-8M8 7.5v4M6 9.5h4"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.2" />
                </svg>
              </button>
              <button
                class="icon-btn row-act row-delete"
                title="Delete session"
                aria-label="Delete session"
                @click.stop="$emit('remove', s.id)">
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 16 16"
                  aria-hidden="true">
                  <path
                    d="M3.5 4.5h9M6.5 4.5V3h3v1.5M4.5 4.5l.7 8.5h5.6l.7-8.5M6.8 7v4M9.2 7v4"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.2" />
                </svg>
              </button>
            </span>
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
    <div class="sessions-foot">
      <!-- archived filter toggle: off (default) hides archived rows;
           on reveals them, muted, still searchable, unarchive from the
           same hover affordance -->
      <label class="archived-toggle">
        <input
          type="checkbox"
          :checked="showArchived"
          @change="$emit('toggle-archived', !showArchived)" />
        archived
      </label>
    </div>
  </aside>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from "vue";
  import {
    bucketSessions,
    relativeAge,
    rowMeta,
    filterSessions,
    withContentMatches,
    searchSessionContent,
    CONTENT_SEARCH_DEBOUNCE_MS,
    createRenderScheduler,
    parseCollapsedLabels,
    serializeCollapsedKeys,
    sessionRowStatus,
  } from "../../composables/omega";
  import type { Session, SessionBuckets } from "../../composables/omega";

  const props = defineProps<{
    sessions: Session[];
    activeId: string | null;
    // ids of sessions whose stream is live - the pulse edge renders per row
    streamingIds: ReadonlySet<string>;
    open: boolean;
    // archived filter: on = the list holds (only) archived rows, muted
    showArchived: boolean;
  }>();

  defineEmits<{
    (e: "open", id: string): void;
    (e: "new"): void;
    (e: "pin", id: string, pinned: boolean): void;
    (e: "archive", id: string, archived: boolean): void;
    // delete is destructive and unconfirmed here - app.vue owns the
    // confirm dialog and only then calls the API
    (e: "remove", id: string): void;
    (e: "toggle-archived", on: boolean): void;
  }>();

  const query = ref("");
  // ids returned by the store's FTS5 content search (null = no search
  // yet / none). The union keeps title matches and adds content-only
  // hits, so a session matching by CONTENT shows up even with a
  // non-matching title.
  const contentIds = ref<string[] | null>(null);
  const filtered = computed(() =>
    withContentMatches(
      filterSessions(props.sessions, query.value),
      props.sessions,
      contentIds.value,
    ),
  );
  const buckets = computed<SessionBuckets>(() => bucketSessions(filtered.value));

  // Content search is debounced (trailing edge via the same injectable
  // scheduler as the render tick) and raced: responses for a query the
  // user already moved past never paint. Failures - 400s from FTS
  // syntax, network blips - fall back to the title filter alone.
  let searchSeq = 0;
  const searchScheduler = createRenderScheduler(
    (q) => void runContentSearch(q),
    CONTENT_SEARCH_DEBOUNCE_MS,
  );

  async function runContentSearch(q: string) {
    const seq = ++searchSeq;
    try {
      const hits = await searchSessionContent(q, props.showArchived);
      if (seq === searchSeq) contentIds.value = hits.map((h) => h.id);
    } catch {
      if (seq === searchSeq) contentIds.value = null;
    }
  }

  watch(query, (q) => {
    const term = q.trim();
    if (!term) {
      searchSeq++; // invalidate any in-flight response
      searchScheduler.cancel();
      contentIds.value = null;
      return;
    }
    searchScheduler.schedule(term);
  });
  // the toggle changes the backend's hit set (archived=include) - a
  // query live when it flips re-runs the content search
  watch(
    () => props.showArchived,
    () => {
      const term = query.value.trim();
      if (!term) return;
      searchScheduler.schedule(term);
    },
  );
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
      archived: !!s.archived,
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