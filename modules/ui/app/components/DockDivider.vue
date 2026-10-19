<template>
  <div
    class="dock-divider"
    :class="{ active: dragging, snap: snapped, column: column }"
    @pointerdown="start"
    @pointermove="move"
    @pointerup="stop"
    @pointercancel="stop"
    role="separator"
    :aria-orientation="column ? 'horizontal' : 'vertical'" />
</template>

<script setup lang="ts">
  // Dumb resize handle between a split's children: reports RAW pointer
  // deltas (x for row splits, y for columns). ALL geometry lives in the
  // dock tree (dock.ts dragSplitDivider: 128px floor, 320px chat floor,
  // 320/640 snap attractor) - measurement and math belong to the owner
  // DockSplit; the divider only streams deltas and mirrors the snap state
  // it is told about.
  import { ref } from "vue";

  const props = defineProps<{
    /** true inside a column split (a horizontal, row-resize handle) */
    column?: boolean;
    /** set by the parent when the current drag sits in a snap attractor */
    snapped?: boolean;
  }>();

  const emit = defineEmits<{
    (e: "drag", delta: number): void;
    (e: "end"): void;
  }>();

  const dragging = ref(false);
  let origin = 0;

  function start(e: PointerEvent) {
    e.preventDefault();
    dragging.value = true;
    origin = props.column ? e.clientY : e.clientX;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }
  function move(e: PointerEvent) {
    if (!dragging.value) return;
    const delta = props.column ? e.clientY - origin : e.clientX - origin;
    emit("drag", delta);
  }
  function stop() {
    if (!dragging.value) return;
    dragging.value = false;
    emit("end");
  }
</script>