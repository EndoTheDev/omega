<template>
  <aside
    ref="asideEl"
    class="sessions"
    :class="{ open: open, stacked: stacked }">
    <!--
      the head row is the UNIVERSAL band (the shared TabBand).
      The band, the button row and the list sit ABOVE the.session-list scrollport -
      scrolling the list never moves them.
    -->
    <TabBand title="Sessions">
      <!-- the search's SEAT moved here; the BEHAVIOR stays where it was. -->
      <div class="band-search">
        <SearchInput
          v-model="query"
          placeholder="Search sessions" />
      </div>
    </TabBand>
    <!-- the new-session control's OWN row - below the band. -->
    <div class="new-row">
      <div
        class="new-btn"
        @keydown.esc="newMenuOpen = false">
        <!-- the NEW-CONVERSATION DIM. Never removed. -->
        <button
          class="new-main"
          :class="{ 'door-dimmed': draftFocused }"
          :aria-disabled="draftFocused || undefined"
          :tabindex="draftFocused ? -1 : undefined"
          :title="draftFocused ? 'the draft conversation already has the focus' : 'New conversation'"
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
          <Icon
            class="new-caret-chevron"
            :class="{ open: newMenuOpen }"
            name="chevron-down"
            :size="12" />
        </button>
      </div>
      <div
        v-if="newMenuOpen"
        class="new-menu"
        @click.stop>
        <!-- the picker is DATA: each entry is a label + intent. -->
        <button
          v-for="entry in newMenuEntries"
          :key="entry.label"
          class="new-menu-item"
          :class="{ 'new-menu-disabled': entry.disabled, 'door-dimmed': entry.dimmed }"
          :disabled="entry.disabled"
          :aria-disabled="entry.disabled || entry.dimmed || undefined"
          :tabindex="entry.dimmed ? -1 : undefined"
          :title="entry.title"
          @click="entry.run?.()">
          {{ entry.label }}
        </button>
      </div>
    </div>
    <ul class="session-list">
      <!--
        sections: PINNED first (the keep-at-top group), then the Today / Yesterday /.
      -->
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
            <Icon
              class="bucket-chevron"
              :class="{ open: !collapsed.has(section.label) }"
              name="chevron-down"
              :size="12" />
            {{ section.label }}
          </button>
        </li>
        <template v-if="!collapsed.has(section.label)">
          <li
            v-for="s in section.rows"
            :key="s.id"
            :class="rowClasses(s)"
            @click="$emit('open', s.id)"
            @contextmenu="onRowMenu($event, s)"
            @mouseenter="hoveredId = s.id"
            @mouseleave="clearHovered(s.id)">
            <!--
              the ONE status element: shape = pin (dot, morphing to a star when the ROW
              is hovered; click pins/unpins,.stop never opens the row), color = state
              (idle grey / streaming orange / unread blue).
            -->
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
                aria-hidden="true"><Icon
                  name="star"
                  :size="12" /></span>
            </button>
            <span class="session-title">{{ s.label || s.id }}</span>
            <span
              v-if="badge(s)"
              class="session-branch">↳ branch</span>
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
            <span
              class="row-actions"
              @mouseenter="hoveredId = s.id">
              <button
                class="icon-btn row-act"
                title="Branch session (fork, inherits the history)"
                aria-label="Branch session"
                @click.stop="$emit('branch', s.id)">
                <Icon
                  name="branch"
                  :size="13" />
              </button>
              <button
                class="icon-btn row-act"
                title="Rename session"
                aria-label="Rename session"
                @click.stop="startRename(s.id)">
                <Icon
                  name="pencil"
                  :size="13" />
              </button>
              <a
                class="icon-btn row-act row-export"
                :href="exportUrl(s.id)"
                :download="s.id + '.jsonl'"
                title="Export session (JSONL)"
                aria-label="Export session as JSONL">
                <Icon
                  name="download"
                  :size="13" />
              </a>
              <!--
                the copy-id affordance (Endo's 0.17.2 batch item 3).
                Hover-revealed with its siblings (the.row-actions cluster),.stop so a
                copy never opens the row, and the click writes the id through the ONE
                copy path (composables/clipboard.ts) with the confirmation on the
                shell's local toast funnel.
              -->
              <button
                class="icon-btn row-act row-copy-id"
                title="Copy session id"
                aria-label="Copy session id"
                @click.stop="copySessionId(s.id)">
                <Icon
                  name="copy"
                  :size="13" />
              </button>
              <button
                class="icon-btn row-act"
                :title="s.archived ? 'Unarchive session' : 'Archive session'"
                :aria-label="s.archived ? 'Unarchive session' : 'Archive session'"
                @click.stop="$emit('archive', s.id, !s.archived)">
                <Icon
                  v-if="!s.archived"
                  name="archive"
                  :size="13" />
                <Icon
                  v-else
                  name="unarchive"
                  :size="13" />
              </button>
              <button
                class="icon-btn row-act row-delete"
                title="Delete session"
                aria-label="Delete session"
                @click.stop="$emit('remove', s.id)">
                <Icon
                  name="trash"
                  :size="13" />
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
  import { computed, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from "vue";
  import Icon from "~/components/ui/Icon.vue";
  import { copyText } from "~/composables/clipboard";
  import { emitLocalToast } from "~/composables/canvas";
  import { openContextMenu } from "~/composables/contextMenu";
  import {
    bucketSessions,
    commitSessionRename,
    exportSessionUrl,
    relativeAge,
    rowMeta,
    sessionRowBadge,
    stopSessionRun,
    withSearchHits,
    searchSessions,
    SESSION_SEARCH_DEBOUNCE_MS,
    createRenderScheduler,
    parseCollapsedLabels,
    serializeCollapsedKeys,
    sessionRowStatus,
  } from "~/composables/omega";
  import type { Session, SessionBuckets } from "~/composables/omega";
  import { sessionMenuItems } from "./sessionMenu";

  const props = defineProps<{
    sessions: Session[];
    activeId: string | null;
    streamingIds: ReadonlySet<string>;
    open: boolean;
    draftFocused?: boolean;
  }>();

  const BAND_STACK_PX = 190;
  const asideEl = ref<HTMLElement | null>(null);
  const stacked = ref(false);
  let bandObserver: ResizeObserver | null = null;
  onMounted(() => {
    const node = asideEl.value;
    if (!node || typeof ResizeObserver === "undefined") return;
    bandObserver = new ResizeObserver((entries) => {
      const pending = entries
        .map((e) => [e.target as HTMLElement, Math.round(e.contentRect.width)] as const)
        .filter(([, w]) => w > 0); // unmounted measure noise never flips the class
      if (pending.length === 0) return;
      requestAnimationFrame(() => {
        for (const [n, w] of pending) {
          if (asideEl.value === n) stacked.value = w < BAND_STACK_PX;
        }
      });
    });
    bandObserver.observe(node);
    const w = node.clientWidth;
    if (w > 0) stacked.value = w < BAND_STACK_PX;
  });
  onBeforeUnmount(() => {
    bandObserver?.disconnect();
    bandObserver = null;
  });

  const emit =
    defineEmits<{
    (e: "open", id: string): void;
    (e: "new"): void;
    (e: "new-ephemeral"): void;
    (e: "pin", id: string, pinned: boolean): void;
    (e: "archive", id: string, archived: boolean): void;
    (e: "branch", id: string): void;
    (e: "rename", id: string, label: string): void;
    (e: "remove", id: string): void;
  }>();

  const query = ref("");
  const newMenuOpen = ref(false);
  interface NewMenuEntry {
    label: string;
    title: string;
    disabled?: boolean;
    /**
     * the new-conversation DIM (never the native `disabled` - that kills the mouse
     * events and with them the title that explains the state, exactly why the last-tab
     * close X uses aria-disabled instead) / /
     */
    dimmed?: boolean;
    run?: () => void;
  }
  // a COMPUTED so the dim follows the focus fact live.
  const newMenuEntries = computed<NewMenuEntry[]>(() => [
    {
      label: "New Session",
      title: "New conversation",
      dimmed: props.draftFocused,
      run: () => pickNew(false),
    },
    {
      label: "Ephemeral",
      title: "No-persist chat (nothing stored, gone when closed)",
      run: () => pickNew(true),
    },
  ]);
  function pickNew(ephemeral: boolean) {
    newMenuOpen.value = false;
    if (ephemeral) emit("new-ephemeral");
    else emit("new");
  }
  function onDocClick(e: MouseEvent) {
    // the OPEN path is a race - the watch adds this listener while the opening click. A
    // pointer inside the split button belongs to the toggle - it never counts as
    // clickaway.
    if ((e.target as Element | null)?.closest?.(".new-btn")) return;
    newMenuOpen.value = false;
  }
  watch(newMenuOpen, (open) => {
    if (open) document.addEventListener("click", onDocClick);
    else document.removeEventListener("click", onDocClick);
  });
  onBeforeUnmount(() => document.removeEventListener("click", onDocClick));
  const hitIds = ref<string[] | null>(null);
  const filtered = computed(() => withSearchHits(props.sessions, hitIds.value));
  const buckets = computed<SessionBuckets>(() => bucketSessions(filtered.value));

  // Search is debounced (trailing edge via the same injectable scheduler as the render
  // tick) and raced: responses for a query the user already moved past never paint.
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

  // hoveredId drives the pin affordance. Cleared on leave - only the exact row's id
  // clears, so pointer noise between rows never flickers a foreign row's star.
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

  // copySessionId: the row's copy-id click. The awaited verdict is the copy-path
  // contract - a refused/unsupported clipboard must never read as "copied" - so the
  // confirmation's title and verdict both follow copyText's answer.
  async function copySessionId(id: string) {
    const ok = await copyText(id);
    emitLocalToast({
      id: 0,
      kind: "chat",
      source_id: "session-copy",
      title: ok ? `copied session id ${id}` : "copy failed - clipboard unavailable",
      verdict: ok ? "ok" : "failed",
      created_at: new Date().toISOString(),
      read: true,
    });
  }

  // the row's Export row uses the same JSONL URL as the hover download link.
  function exportSession(id: string) {
    const a = document.createElement("a");
    a.href = exportUrl(id);
    a.download = id + ".jsonl";
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  async function stopSession(id: string) {
    try {
      await stopSessionRun(id);
    } catch (e) {
      emitLocalToast({
        id: 0,
        kind: "chat",
        source_id: "session-stop",
        title: `stop failed: ${e instanceof Error ? e.message : String(e)}`,
        verdict: "failed",
        created_at: new Date().toISOString(),
        read: true,
      });
    }
  }

  // the row's right-click: the SAME verbs its hover cluster and clicks already carry, so
  // the menu is a second door to the existing paths, never a second implementation.
  function onRowMenu(e: MouseEvent, s: Session) {
    e.preventDefault();
    void openContextMenu(
      e.clientX,
      e.clientY,
      sessionMenuItems(
        { id: s.id, running: props.streamingIds.has(s.id), archived: !!s.archived },
        {
          newSession: () => emit("new"),
          rename: () => startRename(s.id),
          branch: () => emit("branch", s.id),
          exportSession: () => exportSession(s.id),
          copyId: () => void copySessionId(s.id),
          archive: () => emit("archive", s.id, !s.archived),
          remove: () => emit("remove", s.id),
          stop: () => void stopSession(s.id),
        },
      ),
    );
  }

  // inline rename --- renamingId holds the row whose title is an input right now.
  // Escape cancels; both paths clear the editing state first so the blur never double
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
<!--
  scoped: the split new-chat control and the type picker live HERE - this surface never
  grows main.css.
-->
<style scoped>
/*
 * the head row is the shared TabBand - the band's own geometry replaces.sessions-head.
 */
/* the band row carries the title ALONE. */
.new-row {
  position: relative;
  padding: 8px;
}
/*
 * the search seats IN the band (the band-tool slot).
 * The seat's geometry is TabBand's now, this scoped block keeps ONLY the sidebar's
 * narrow stack: below the drag wall the stacked full-width row wins over the shared
 * 128px seat (the wrapper's width: 100% must out-weigh it).
 */
.sessions.stacked :deep(.tab-band) {
  flex-wrap: wrap;
  row-gap: 4px;
  padding-bottom: 4px;
}
.sessions.stacked :deep(.tab-band-actions) {
  width: 100%;
}
.sessions.stacked .band-search {
  width: 100%;
}
/*
 * the split wrapper keeps the shared.new-btn chip.
 */
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
/* the caret is its own zone. */
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
/* the picker's arrow: closed (down) when the menu is shut, rotated open (up) while. */
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
/*
 * a disabled picker entry renders dim and inert - no hover lift, no pointer affordance.
 */
.new-menu-item:disabled {
  color: var(--text-dim);
  cursor: default;
}
.new-menu-item:disabled:hover {
  background: none;
}
</style>
