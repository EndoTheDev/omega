<template>
  <!-- Session workspace picker (card t_9de5b009). A chip in the composer
       toolbar showing the session's remembered folder; the popover
       browses real directories (/workspaces/browse, dirs only) or takes
       a typed absolute path, and applies the choice to the session row.
       No session binding -> the chip still renders but applies get an
       error note (nothing to pin on).
       v0.14.3 (user screenshot findings): the chip rides the same
       interactive treatment as its row neighbors (.model-chip: full
       contrast, chevron, shared narrow squeeze) and shows the CURRENT
       workspace value - "no workspace" is the empty state, never the
       static word. The popover keeps its distance from the composer
       (10px), the close affordance sits in the head (top-right), and
       the confirm row is one clean footer; rows no longer clip at the
       popup edge and the muted text carries real contrast. -->
  <div class="ws-select">
    <button
      class="model-chip ws-btn"
      type="button"
      :disabled="disabled"
      :title="btnTitle"
      :aria-expanded="open"
      @click="toggle">
      <span
        v-if="workspace"
        class="chip-name">{{ baseName }}</span>
      <span
        v-else
        class="chip-name ws-unset">no workspace</span>
      <span
        class="chev"
        aria-hidden="true">▾</span>
    </button>
    <div
      v-if="open"
      class="ws-pop">
      <div class="ws-pop-head">
        <span
          class="ws-crumb"
          :title="current.path">{{ current.path || "/" }}</span>
        <button
          v-if="current.parent"
          class="ws-up"
          type="button"
          title="parent directory"
          @click="go(current.parent)">&uarr;</button>
        <button
          class="ws-close"
          type="button"
          title="close the workspace picker"
          aria-label="close the workspace picker"
          @click="toggle">×</button>
      </div>
      <div class="ws-dirs">
        <button
          v-for="d in listing?.dirs ?? []"
          :key="d.path"
          class="ws-dir"
          type="button"
          :title="d.path"
          @click="go(d.path)">{{ d.name }}<span class="ws-slash">/</span></button>
        <p
          v-if="listing && !listing.dirs.length"
          class="ws-empty">no visible subdirectories</p>
        <p
          v-if="error"
          class="ws-error">{{ error }}</p>
      </div>
      <div class="ws-apply-row">
        <button
          class="ws-apply"
          type="button"
          @click="applyCurrent">use {{ current.path || "/" }}</button>
        <button
          v-if="workspace"
          class="ws-clear"
          type="button"
          @click="clear">clear</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { browseWorkspaces, type WorkspaceListing } from "../../composables/omega";

  const props = defineProps<{
    // the session's remembered workspace ("": none - the chip renders
    // the unset label and the session has no folder root)
    workspace?: string;
    // no session bound: a folder pins on a session row, a draft has
    // nothing to pin on - the button is inert and says so
    disabled?: boolean;
  }>();

  const emit = defineEmits<{
    (e: "update:workspace", value: string): void;
  }>();

  const open = ref(false);
  const listing = ref<WorkspaceListing | null>(null);
  const error = ref("");

  const baseName = computed(() => {
    const p = props.workspace ?? "";
    return p.split("/").filter(Boolean).pop() ?? p;
  });

  // the chip's tooltip (script keeps the literal strings - apostrophes
  // in template expressions break the vue template parser)
  const btnTitle = computed(() => {
    if (props.disabled) return "send a first message to bind a session, then pin a folder";
    return props.workspace ? `workspace: ${props.workspace}` : "pick this session's working folder";
  });

  function toggle() {
    if (props.disabled) return;
    open.value = !open.value;
    if (open.value) {
      // the picker opens where the session is rooted now - or at "/",
      // the only honest start for a document-unrooted chooser
      go(props.workspace || "/");
    }
  }

  async function go(path: string) {
    error.value = "";
    try {
      listing.value = await browseWorkspaces(path === "/" ? undefined : path);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  const current = computed<WorkspaceListing>(
    () => listing.value ?? { path: "", parent: "", dirs: [] },
  );

  function applyCurrent() {
    if (!current.value.path) {
      error.value = "pick a directory first";
      return;
    }
    emit("update:workspace", current.value.path);
    open.value = false;
  }

  function clear() {
    emit("update:workspace", "");
    open.value = false;
  }

  // outside click closes the popover (same behavior family as the
  // model select's dropdown affordances)
  function onDocClick(e: MouseEvent) {
    if (!open.value) return;
    if (!(e.target instanceof Element) || !e.target.closest(".ws-select")) {
      open.value = false;
    }
  }
  onMounted(() => document.addEventListener("mousedown", onDocClick));
  onBeforeUnmount(() => document.removeEventListener("mousedown", onDocClick));
</script>

<style scoped>
  .ws-select {
    position: relative;
  }
  /* the chip borrows the global .model-chip treatment (full contrast,
     chevron, hover, the narrow squeeze); the scoped layer adds only
     this row's own deltas. Disabled (no session bound) stays dimmed. */
  .ws-btn:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .ws-unset {
    opacity: 0.55;
    font-style: italic;
  }
  .ws-pop {
    position: absolute;
    bottom: calc(100% + 10px);
    left: 0;
    z-index: 30;
    width: 320px;
    max-height: 280px;
    display: flex;
    flex-direction: column;
    background: var(--pane-bg, #1c1d21);
    border: 1px solid var(--dock-border, #333438);
    border-radius: 8px;
    padding: 8px;
    gap: 6px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    overflow: hidden;
  }
  .ws-pop-head {
    display: flex;
    align-items: center;
    gap: 4px;
    padding-bottom: 4px;
    border-bottom: 1px solid var(--dock-border, #333438);
  }
  .ws-crumb {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    color: var(--text, #e8e8ea);
    opacity: 0.95;
    font-family: var(--mono, monospace);
  }
  .ws-dirs {
    min-height: 0;
    overflow: auto;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1px;
    /* the breathing room at both ends keeps the first and last row
       clear of the scroll clipping edge (screenshot finding) */
    padding: 2px 2px;
  }
  .ws-dir {
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    font-size: 12px;
    padding: 4px 8px;
    border-radius: 4px;
    color: inherit;
  }
  .ws-dir:hover {
    background: rgba(255, 255, 255, 0.08);
  }
  .ws-slash {
    opacity: 0.6;
  }
  .ws-empty {
    font-size: 11px;
    color: var(--text-dim, #9a9ba1);
    padding: 4px 8px;
    margin: 0;
  }
  .ws-error {
    font-size: 11px;
    color: #e66;
    padding: 4px 8px;
    margin: 0;
  }
  .ws-apply-row {
    display: flex;
    gap: 6px;
    align-items: center;
    padding-top: 6px;
    border-top: 1px solid var(--dock-border, #333438);
  }
  .ws-apply,
  .ws-clear,
  .ws-close,
  .ws-up {
    background: none;
    border: 1px solid var(--dock-border, #333438);
    border-radius: 5px;
    cursor: pointer;
    font-size: 11px;
    padding: 4px 9px;
    color: inherit;
  }
  .ws-apply {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ws-apply:hover,
  .ws-clear:hover,
  .ws-up:hover {
    border-color: var(--text-dim, #9a9ba1);
    color: var(--text, #e8e8ea);
  }
  .ws-close {
    flex: none;
    font-size: 12px;
    line-height: 1;
    padding: 3px 8px;
    color: var(--text-dim, #9a9ba1);
  }
  .ws-close:hover {
    color: var(--text, #e8e8ea);
    border-color: var(--text-dim, #9a9ba1);
  }
</style>