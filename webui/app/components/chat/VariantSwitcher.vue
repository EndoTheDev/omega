<template>
  <!-- ONE variant switcher chip: « n/n » with prev/next arrows. -->
  <div class="variant-switcher">
    <button
      class="v-arrow"
      type="button"
      title="previous variant"
      :disabled="vLabel.index <= 0"
      @click.stop="emit('select', vLabel.index - 1)">«</button>
    <span class="v-pos">{{ vLabel.index + 1 }}/{{ vLabel.count }}</span>
    <button
      class="v-arrow"
      type="button"
      title="next variant"
      :disabled="vLabel.index >= vLabel.count - 1"
      @click.stop="emit('select', vLabel.index + 1)">»</button>
  </div>
</template>

<script setup lang="ts">
  import { computed } from "vue";

  import { variantLabel, type VariantBlock } from "../../composables/omega";

  const props = defineProps<{
    variant: VariantBlock;
    itemId?: string;
  }>();
  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  const vLabel = computed(() => variantLabel(props.variant, props.itemId));
</script>