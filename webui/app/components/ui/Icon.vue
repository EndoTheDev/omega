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
      name: string;
      size?: number;
      strokeWidth?: number;
    }>(),
    { size: 14, strokeWidth: 2 },
  );

  const icon = computed(() => iconOf(props.name));
</script>