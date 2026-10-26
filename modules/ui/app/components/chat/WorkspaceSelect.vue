<template>
  <!-- Session workspace picker (card t_9de5b009). A chip in the composer
       toolbar showing the session's remembered folder; the popover
       browses real directories (/workspaces/browse, dirs only) or takes
       a typed absolute path, and applies the choice to the session row.
       No session binding -> the chip still renders but applies get an
       error note (nothing to pin on). -->
  <div class="ws-select">
    <button
      class="ws-btn"
      type="button"
      :disabled="disabled"
      :title="disabled
        ? 'send a first message to bind a session, then pin a folder'
        : workspace ? 'workspace: ' + workspace : 'pick this session's working folder'"
      @click="toggle">
      <span
        v-if="workspace"
        class="ws-set">{{ baseName }}</span>
      <span
        v-else
        class="ws-unset">workspace</span>
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
        <button
          class="ws-close"
          type="button"
          @click="toggle">×</button>
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
  .ws-btn {
    background: none;
    border: 1px solid var(--dock-border, #333438);
    border-radius: 6px;
    cursor: pointer;
    font-size: 11px;
    padding: 2px 8px;
  }
  .ws-btn:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .ws-set {
    max-width: 16ch;
    display: inline-block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: bottom;
  }
  .ws-unset {
    opacity: 0.55;
  }
  .ws-pop {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    z-index: 30;
    width: 320px;
    max-height: 280px;
    display: flex;
    flex-direction: column;
    background: var(--pane-bg, #1c1d21);
    border: 1px solid var(--dock-border, #333438);
    border-radius: 8px;
    padding: 6px;
    gap: 4px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
  }
  .ws-pop-head {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .ws-crumb {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    opacity: 0.8;
  }
  .ws-dirs {
    min-height: 0;
    overflow: auto;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .ws-dir {
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    font-size: 12px;
    padding: 3px 6px;
    border-radius: 4px;
  }
  .ws-dir:hover {
    background: rgba(255, 255, 255, 0.06);
  }
  .ws-slash {
    opacity: 0.4;
  }
  .ws-empty {
    font-size: 11px;
    opacity: 0.5;
    padding: 3px 6px;
  }
  .ws-error {
    font-size: 11px;
    color: #e66;
    padding: 3px 6px;
  }
  .ws-apply-row {
    display: flex;
    gap: 4px;
    align-items: center;
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
    padding: 2px 7px;
  }
  .ws-apply {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ws-close {
    border: none;
    font-size: 13px;
    padding: 0 5px;
  }
</style>