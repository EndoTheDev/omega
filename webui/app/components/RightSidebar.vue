<template>
  <aside
    class="aside pane"
    :class="{ open: open }">
    <div class="placeholder">
      <span class="placeholder-label">inspector</span>
      <span class="placeholder-note"
        >this pane is reserved — a file manager, git view, or tool settings will
        live here.</span
      >
    </div>
  </aside>
</template>

<script setup lang="ts">
  // Right sidebar placeholder: deliberately empty until its purpose is decided
  // (candidates: file manager, git, tool toggles). The `open` class is the
  // mobile overlay state - unused on desktop.
  defineProps<{
    open?: boolean;
  }>();
</script>
