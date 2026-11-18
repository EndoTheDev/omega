<template>
  <!--
    the mobile drawer.
    That decoupling is the REPRO 1 fix: a desktop-closed sidebar (the pane pruned from
    the tree, page-lifetime by design) must not gate the mobile drawer - the drawer
    reads no dock-tree presence.
  -->
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
        :aria-label="t.title"
        :title="t.title"
        @click="$emit('update:modelValue', i)">
        <Icon
          v-if="t.icon"
          :name="t.icon"
          size="13" />
      </button>
    </div>
    <div class="mobile-drawer-content">
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
  // side: which edge the drawer slides from.
  import Icon from "../ui/Icon.vue";

  defineProps<{
    open: boolean;
    side: "left" | "right" | "bottom";
    tabs: { id: string; title: string; icon: string | null }[];
    modelValue: number;
    ariaLabel: string;
  }>();
  defineEmits<{
    (e: "update:modelValue", i: number): void;
    (e: "close"): void;
  }>();
</script>