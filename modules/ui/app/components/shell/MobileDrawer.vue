<template>
  <!-- card t_e76ce7b6: the mobile drawer. The dock's pinned-edge panes
       are desktop chrome - below 768px they collapse to zero-width cells
       and their content is suppressed (app.vue's edge-pane slots carry
       v-if="!isMobile"). This component is the mobile-only affordance
       that renders the same content WITH its own tab bar, independent of
       whether the edge pane currently lives in the dock tree. That
       decoupling is the REPRO 1 fix: a desktop-closed sidebar (the pane
       pruned from the tree, page-lifetime by design) must not gate the
       mobile drawer - the drawer reads no dock-tree presence. REPRO 2:
       the dock's .dock-tabbar is hidden under the breakpoint (main.css),
       so the drawer carries its own strip that switches the content. -->
  <section
    class="mobile-drawer"
    :class="[side, { open }]"
    role="dialog"
    :aria-modal="false"
    :aria-label="ariaLabel">
    <div
      class="mobile-drawer-tabbar"
      role="tablist"
      :aria-label="ariaLabel + ' tabs'">
      <button
        v-for="(t, i) in tabs"
        :key="t.id"
        class="mobile-drawer-tab"
        :class="{ active: i === modelValue }"
        role="tab"
        :aria-selected="i === modelValue"
        :title="t.title"
        @click="$emit('update:modelValue', i)">
        {{ t.title }}
      </button>
    </div>
    <div class="mobile-drawer-content">
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
  // side: which edge the drawer slides from. The scrim is owned by
  // app.vue (a sibling of the dock pane-area) so the frost renders
  // against the shell root, same as the desktop drawer era.
  defineProps<{
    open: boolean;
    side: "left" | "right";
    tabs: { id: string; title: string }[];
    modelValue: number;
    ariaLabel: string;
  }>();
  defineEmits<{
    (e: "update:modelValue", i: number): void;
    (e: "close"): void;
  }>();
</script>