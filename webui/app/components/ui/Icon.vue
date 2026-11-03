<!-- omega webui: THE ICON WRAPPER (card t_502b8e81). One component, one
     import path, one place to restyle: every icon in the webui renders
     through here, resolved by semantic name from composables/icons.ts
     (lucide-vue-next below the wrapper - tree-shaken, no icon font).
     Sizes/strokes stay uniform: the default 14px / stroke 2 is the shell
     scale; a call site may pass `size`/`stroke-width` but never ad hoc
     inline attrs. The name lookup throws on a typo at render time - an
     unknown icon is a build bug, not a blank box. -->
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
      /** the shell's icon scale; 16 = menubar chrome, 13 = row actions */
      size?: number;
      /** lucide's stroke - the shell's 2px round-join line style */
      strokeWidth?: number;
    }>(),
    { size: 14, strokeWidth: 2 },
  );

  const icon = computed(() => iconOf(props.name));
</script>