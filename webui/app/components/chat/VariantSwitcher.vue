<template>
  <!-- ONE variant switcher chip (t_a62eeb43 markup, extracted
       t_0df4efd8): « n/n » with prev/next arrows. It mounts on the
       branch's anchor item - the active head bubble's actions row, or
       the head block's own row when a multi-row replay's head is a
       thinking / tool call row without a stored text bubble. -->
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
    // this item's branch block - resolved by ChatTab (variantAnchors)
    variant: VariantBlock;
    // the anchor item's store id when it is itself a sibling (the head
    // bubble); absent anchors fall back to the block's active index
    itemId?: string;
  }>();
  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  const vLabel = computed(() => variantLabel(props.variant, props.itemId));
</script>