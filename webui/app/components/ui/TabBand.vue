<template>
  <!--
    omega webui: THE UNIVERSAL PANEL TOP BAR.
    NON-SCROLLING BY STRUCTURE: the band renders BEFORE the tab's scroll body so
    scrolling never moves it.
    Geometry rides the Files-band contract.
  -->
  <header
    class="tab-band"
    :class="{ flush: lead === false }">
    <span
      class="tab-band-title"
      :title="titleAttr ?? undefined">{{ title }}</span>
    <!-- the LEAD seat (0.17.3 make-over, the kanban board selector). -->
    <div
      v-if="$slots.lead"
      class="tab-band-lead">
      <slot name="lead" />
    </div>
    <div
      v-if="$slots.default"
      class="tab-band-actions"
      role="toolbar"
      :aria-label="title + ' actions'">
      <!--
        the parent's buttons carry their own aria-labels; the shared.band-tool look is
        the files-tool contract
      -->
      <slot />
    </div>
  </header>
</template>

<script setup lang="ts">
  // `title` = what the tab is.
  withDefaults(
    defineProps<{
      title: string;
      titleAttr?: string;
      lead?: boolean;
    }>(),
    { lead: true },
  );
</script>

<style scoped>
  .tab-band {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 4px;
    /*
     * min-height, not height: a band action taller than the row (the settings filter,
     * the insights window select) grows the row instead of clipping; the Files geometry
     * below is exactly what renders there / /
     */
    min-height: 22px;
    margin-top: 14px;
    padding: 0 8px;
    flex: none;
  }
  .tab-band.flush {
    margin-top: 0;
  }
  .tab-band-title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text);
  }
  .tab-band-actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-shrink: 0;
  }
  /* the lead seat sits BETWEEN the title and the actions, and it is what eats the. */
  .tab-band-lead {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex: 1 1 auto;
  }
  /*
   * the ONE action-button look: FilesTab's files-tool contract promoted to the band.
   */
  .tab-band-actions :deep(.band-tool),
  .tab-band-lead :deep(.band-tool) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 20px;
    padding: 0 4px;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    border-radius: var(--r-chip);
    cursor: pointer;
    font-family: var(--mono);
    font-size: 12px;
  }
  /*
   * the compact SEARCH SEAT, the AppSidebar's pattern promoted here.
   */
  .tab-band-actions :deep(.band-search) {
    width: 128px;
    min-width: 96px;
    flex: 0 0 auto;
  }
  .tab-band-actions :deep(.band-search .search-input input) {
    padding: 2px 20px 2px 6px;
  }
  /*
   * the Narrow-pane adaptation, self-measuring: the band only WRAPS when its single row
   * cannot hold title + seat.
   */
  .tab-band:has(.band-search) {
    flex-wrap: wrap;
    row-gap: 4px;
  }
  .tab-band-actions :deep(.band-tool:hover:not(:disabled)) {
    background: var(--z2);
    color: var(--text);
  }
  .tab-band-lead :deep(.band-tool:hover:not(:disabled)) {
    background: var(--z2);
    color: var(--text);
  }
  .tab-band-actions :deep(.band-tool:disabled) {
    opacity: 0.4;
    cursor: default;
  }
</style>
