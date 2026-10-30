<template>
  <!-- omega webui: THE UNIVERSAL PANEL TOP BAR (card t_308e6d9b, Endo's
       design directive 2026-10-30: the Files explorer's header band is the
       contract for EVERY tab). One shared component (the Icon tier -
       components/ui/), each tab composes it - no copy-paste band per tab.
       Band = the tab's title left, the tab's actions right; tabs with no
       actions still get the band. NON-SCROLLING BY STRUCTURE: the band
       renders BEFORE the tab's scroll body (never inside the scrollport -
       the t_c252b9c6 lesson; CSS sticky inside a scroll body is the flimsy
       cousin) so scrolling never moves it. Geometry rides the Files-band
       contract (t_5c660a42: the 14px lead-in clears the -8px r-pane tuck;
       the tree rows' ~22px rhythm). Deletability: remove this file and
       every band is gone - the per-tab usage IS the feature. -->
  <header
    class="tab-band"
    :class="{ flush: lead === false }">
    <span
      class="tab-band-title"
      :title="titleAttr ?? undefined">{{ title }}</span>
    <div
      v-if="$slots.default"
      class="tab-band-actions"
      role="toolbar"
      :aria-label="title + ' actions'">
      <!-- the parent's buttons carry their own aria-labels; the shared
           .band-tool look is the files-tool contract (t_a5f9d185) -->
      <slot />
    </div>
  </header>
</template>

<script setup lang="ts">
  // `title` = what the tab is (the registry's display voice - title per
  // tab body, not per tab chip). `titleAttr` = the Files band's tooltip
  // (the resolved root) - omit elsewhere. `lead=false` for a host that
  // supplies its own top padding (Settings' 16px pane card): the tuck
  // clear-out is the host's there, not the band's.
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
    /* min-height, not height: a band action taller than the row (the
       settings filter, the insights window select) grows the row instead
       of clipping; the Files geometry below is exactly what renders there */
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
  /* the ONE action-button look: FilesTab's files-tool contract promoted
     to the band vocabulary - dimmed at rest, hover raises, disabled 0.4 */
  .tab-band-actions :deep(.band-tool) {
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
  .tab-band-actions :deep(.band-tool:hover:not(:disabled)) {
    background: var(--z2);
    color: var(--text);
  }
  .tab-band-actions :deep(.band-tool:disabled) {
    opacity: 0.4;
    cursor: default;
  }
</style>
