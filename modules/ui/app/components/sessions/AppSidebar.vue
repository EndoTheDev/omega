<template>
  <aside
    class="sessions"
    :class="{ open: open }">
    <div class="sessions-head">
      <!-- card t_aee5199f: the new-chat control is a FULL-WIDTH split
           button - the body mints the default (persisted) chat, the
           arrow-down opens the type picker (card t_55a571ff: New
           Session / Ephemeral / Continuous). All styling stays scoped so
           this surface never grows main.css (SAME-FILE discipline:
           t_33f8756c owns the shared sheet) - only .new-btn's shared
           chip look from main.css is consumed. -->
      <div class="head-actions">
        <div
          class="new-btn"
          @keydown.esc="newMenuOpen = false">
          <button
            class="new-main"
            title="New conversation"
            @click="pickNew(false)">
            + New Session
          </button>
          <button
            class="new-caret"
            aria-haspopup="true"
            aria-label="Choose new chat type"
            :aria-expanded="newMenuOpen"
            title="New chat type"
            @click="newMenuOpen = !newMenuOpen">
            <svg
              class="new-caret-chevron"
              :class="{ open: newMenuOpen }"
              width="12"
              height="12"
              viewBox="0 0 12 12"
              aria-hidden="true">
              <path
                d="M2.5 4L6 7.5L9.5 4"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5" />
            </svg>
          </button>
        </div>
        <div
          v-if="newMenuOpen"
          class="new-menu"
          @click.stop>
          <!-- the picker is DATA (card t_55a571ff): each entry is a label
               + intent; a disabled entry renders inert until its design
               round lands - no behavior is built here. The future
               continuous-agents card wires its `run`, no template change. -->
          <button
            v-for="entry in newMenuEntries"
            :key="entry.label"
            class="new-menu-item"
            :class="{ 'new-menu-disabled': entry.disabled }"
            :disabled="entry.disabled"
            :title="entry.title"
            :aria-disabled="entry.disabled || undefined"
            @click="entry.run?.()">
            {{ entry.label }}
          </button>
        </div>
      </div>
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
        <li
          class="bucket-row"
          :class="{ 'bucket-archived': section.archived }">
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
            <!-- branch badge (t_6bf696e4): parented sessions carry the
                 subtle ↳ mark - the lineage is the server-side
                 parent_id, the label text is the pure sessionRowBadge -->
            <span
              v-if="badge(s)"
              class="session-branch">↳ branch</span>
            <!-- inline rename: the pencil edit swaps the title for an
                 input; commit on Enter/blur, cancel on Esc - the pure
                 commitSessionRename drops no-op drafts (no PUT) -->
            <input
              v-if="renamingId === s.id"
              class="session-rename"
              aria-label="Rename session"
              v-model="renameDraft"
              autofocus
              @click.stop
              @keyup.enter="commitRename(s)"
              @keyup.esc="renamingId = null"
              @blur="commitRename(s)" />
            <span
              v-if="renamingId !== s.id && meta(s)"
              class="session-meta">{{ meta(s) }}</span>
            <!-- hover action cluster: branch (fork) + rename (pencil) +
                 export (download), then archive (box) + delete (trash),
                 inset LEFT of the time slot - the age text's slot stays
                 empty during hover (the row's 34px right padding band) -->
            <span
              class="row-actions"
              @mouseenter="hoveredId = s.id">
              <button
                class="icon-btn row-act"
                title="Branch session (fork, inherits the history)"
                aria-label="Branch session"
                @click.stop="$emit('branch', s.id)">
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 16 16"
                  aria-hidden="true">
                  <path
                    d="M4 2v6a3 3 0 0 0 3 3h5M9 8l3 3-3 3"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.2" />
                </svg>
              </button>
              <button
                class="icon-btn row-act"
                title="Rename session"
                aria-label="Rename session"
                @click.stop="startRename(s.id)">
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 16 16"
                  aria-hidden="true">
                  <path
                    d="M11.5 2.5l2 2L6 12l-2.7.7L4 10z"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.2" />
                </svg>
              </button>
              <a
                class="icon-btn row-act row-export"
                :href="exportUrl(s.id)"
                :download="s.id + '.jsonl'"
                title="Export session (JSONL)"
                aria-label="Export session as JSONL">
                <svg
                  width="13"
                  height="13"
                  viewBox="0 0 16 16"
                  aria-hidden="true">
                  <path
                    d="M8 2v7M5 6.5L8 9.5l3-3M3 12.5h10"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.2" />
                </svg>
              </a>
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
                    d="M2 4.5h12v3H2zM3.5 7.5v5h9v-5M6.25 10h3.5"
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
                    d="M2 4.5h12v3H2zM3.5 7.5v5h9v-5M6.25 10h3.5M8 10V6.5"
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
  </aside>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onUnmounted, ref, watch } from "vue";
  import {
    bucketSessions,
    commitSessionRename,
    exportSessionUrl,
    relativeAge,
    rowMeta,
    sessionRowBadge,
    withSearchHits,
    searchSessions,
    SESSION_SEARCH_DEBOUNCE_MS,
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
  }>();

  const emit =
    defineEmits<{
    (e: "open", id: string): void;
    (e: "new"): void;
    // new-ephemeral (card t_d96a5cd5): the dropdown's no-persist entry
    // - app.vue mints a composer-first ephemeral tab
    (e: "new-ephemeral"): void;
    (e: "pin", id: string, pinned: boolean): void;
    // archive/unarchive: the row moves to (or out of) the archived group
    // at the list bottom instantly - the optimistic patch flips the flag
    // on the row the list already holds
    (e: "archive", id: string, archived: boolean): void;
    // branch forks the session (t_6bf696e4): app.vue POSTs, refreshes
    // the list and opens the child like any session
    (e: "branch", id: string): void;
    // rename commits the inline draft; app.vue PUTs the label (the
    // /label parity) and patches the row it already holds
    (e: "rename", id: string, label: string): void;
    // export renders nothing here - the download anchor goes straight
    // to GET /sessions/{id}/export (the /export parity); no emit needed
    // delete is destructive and unconfirmed here - app.vue owns the
    // confirm dialog and only then calls the API
    (e: "remove", id: string): void;
  }>();

  const query = ref("");
  // the new-chat dropdown's open bit + click-outside close (document
  // listener bound on open, torn down on close/unmount)
  const newMenuOpen = ref(false);
  // card t_55a571ff: the type picker is data. The placeholder entry is
  // DATA, not dead template - a future continuous-agents card flips
  // `disabled` off and supplies `run` (Endo ruling: ship the label now
  // so the shape is visible, no behavior until the design round lands).
  interface NewMenuEntry {
    label: string;
    title: string;
    disabled?: boolean;
    run?: () => void;
  }
  const newMenuEntries: NewMenuEntry[] = [
    { label: "New Session", title: "New conversation", run: () => pickNew(false) },
    {
      label: "Ephemeral",
      title: "No-persist chat (nothing stored, gone when closed)",
      run: () => pickNew(true),
    },
    {
      label: "Continuous",
      title: "design pending - coming with the continuous agents arc",
      disabled: true,
    },
  ];
  function pickNew(ephemeral: boolean) {
    newMenuOpen.value = false;
    if (ephemeral) emit("new-ephemeral");
    else emit("new");
  }
  function onDocClick() {
    newMenuOpen.value = false;
  }
  watch(newMenuOpen, (open) => {
    if (open) document.addEventListener("click", onDocClick);
    else document.removeEventListener("click", onDocClick);
  });
  onBeforeUnmount(() => document.removeEventListener("click", onDocClick));
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

  // non-empty sections in render order; empty groups render no header.
  // archived is LAST - after the newest time bucket, dimmed (the
  // bucket-archived class on the header).
  const visibleSections = computed(() => {
    const b = buckets.value;
    const sections: {
      label: string;
      rows: Session[];
      archived?: boolean;
    }[] = [
      { label: "pinned", rows: b.pinned },
      { label: "today", rows: b.today },
      { label: "yesterday", rows: b.yesterday },
      { label: "earlier", rows: b.earlier },
      { label: "archived", rows: b.archived, archived: true },
    ].filter((section) => section.rows.length > 0);
    return sections;
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
  function badge(s: Session): string {
    return sessionRowBadge(s);
  }
  function exportUrl(id: string): string {
    return exportSessionUrl(id);
  }

  // --- inline rename ---
  // renamingId holds the row whose title is an input right now; the
  // draft starts from the row's label (id fallback) on start and the
  // commit routes through the pure commitSessionRename (null = no-op -
  // empty or unchanged drafts just close the edit). Escape cancels;
  // both paths clear the editing state first so the blur never double
  // commits.
  const renamingId = ref<string | null>(null);
  const renameDraft = ref("");
  function startRename(id: string) {
    const s = props.sessions.find((x) => x.id === id);
    renamingId.value = id;
    renameDraft.value = s?.label || s?.id || "";
  }
  function commitRename(s: Session) {
    if (renamingId.value !== s.id) return;
    renamingId.value = null;
    const label = commitSessionRename(renameDraft.value, s.label);
    if (label !== null) emit("rename", s.id, label);
  }
</script>
<!-- scoped: the split new-chat control (card t_aee5199f) and the type
     picker live HERE - this surface never grows main.css (SAME-FILE
     discipline: t_e76ce7b6 / t_33f8756c own the shared sheet's open
     blocks). Only .new-btn's shared chip look (border, radius, dim
     text + accent hover) is consumed from main.css. -->
<style scoped>
.head-actions {
  position: relative;
  width: 100%;
}
/* the split wrapper keeps the shared .new-btn chip (border, radius,
   dim text, accent hover). The halves inside are borderless - the
   wrapper's border draws the pill, a hairline divides body from
   arrow; hovering either half lights the whole chip (wrapper :hover
   rules from main.css + color: inherit). */
.new-btn {
  display: flex;
  align-items: stretch;
  width: 100%;
  padding: 0;
}
.new-main {
  flex: 1;
  text-align: left;
  padding: 5px 8px 5px 10px;
  background: none;
  border: 0;
  font: inherit;
  color: inherit;
  cursor: pointer;
}
/* card t_a0a466c6: the caret is its own zone - the hairline divider
   (the chip's own border color) reads the split, the hover lights ONLY
   the caret zone so the two halves answer the pointer separately. */
.new-caret {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 8px;
  background: none;
  border: 0;
  border-left: 1px solid var(--stroke);
  font: inherit;
  color: inherit;
  cursor: pointer;
}
.new-caret:hover {
  background: var(--z2);
}
.new-main:focus-visible,
.new-caret:focus-visible {
  outline: 1px solid var(--accent);
  outline-offset: -1px;
}
/* the picker's arrow: closed (down) when the menu is shut, rotated
   open (up) while the type choice shows - same 0.12s ease as the
   bucket chevrons */
.new-caret-chevron {
  transition: transform 0.12s ease;
}
.new-caret-chevron.open {
  transform: rotate(180deg);
}
.new-menu {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  z-index: 30;
  min-width: 180px;
  margin-top: 4px;
  padding: 4px;
  border: 1px solid var(--stroke);
  border-radius: var(--r-card);
  background: var(--z1-raised);
  box-shadow: 0 8px 24px rgb(0 0 0 / 35%);
  display: flex;
  flex-direction: column;
}
.new-menu-item {
  text-align: left;
  padding: 7px 10px;
  border: 0;
  border-radius: 6px;
  background: none;
  color: var(--text);
  font: inherit;
  cursor: pointer;
  white-space: nowrap;
}
.new-menu-item:hover {
  background: var(--z2);
}
/* card t_55a571ff: the disabled placeholder (Continuous) renders dim and
   inert - no hover lift, no pointer affordance until its future card
   wires the entry. */
.new-menu-item:disabled {
  color: var(--text-dim);
  cursor: default;
}
.new-menu-item:disabled:hover {
  background: none;
}
</style>
