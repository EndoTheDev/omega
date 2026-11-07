<!-- omega webui: THE ICON WRAPPER - the icon law and the size doctrine
     live in .agents/DESIGN.md (card t_502b8e81). -->
<template>
  <component
    :is="icon"
    :size="size"
    :stroke-width="strokeWidth"
    aria-hidden="true" />
</template>

<script setup lang="ts">
  import { computed } from "vue";
  import { iconOf } from "../../composables/icons";

  const props = withDefaults(
    defineProps<{
      /** a semantic name from the iconset (composables/icons.ts ICONS) */
      name: string;
      /** the size doctrine (16 menubar / 13 row actions) lives in .agents/DESIGN.md */
      size?: number;
      /** lucide's stroke - the shell's 2px round-join line style */
      strokeWidth?: number;
    }>(),
    { size: 14, strokeWidth: 2 },
  );

  const icon = computed(() => iconOf(props.name));
</script>