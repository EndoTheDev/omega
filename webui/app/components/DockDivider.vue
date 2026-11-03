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
  // positions (x for row splits, y for columns) as `raw` deltas. ALL
  // geometry lives in the dock tree (dock.ts dragSplitDivider: 128px floor,
  // 320px chat floor, +320 snap ladder) - measurement and math belong to
  // the owner DockSplit; the divider only streams positions and mirrors
  // the snap state it is told about. (The delta it also carries once let
  // DockSplit rebase mid-gesture for the squeeze push-back's re-measure -
  // the chain is gone (t_6a74d5a1 wall law): the reducer keeps its frozen
  // baseline for the whole gesture.)
  import { ref } from "vue";

  const props = defineProps<{
    /** true inside a column split (a horizontal, row-resize handle) */
    column?: boolean;
    /** set by the parent when the current drag sits in a snap attractor */
    snapped?: boolean;
  }>();

  const emit = defineEmits<{
    (e: "drag", delta: number, raw: number): void;
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
    const raw = props.column ? e.clientY : e.clientX;
    emit("drag", raw - origin, raw);
  }
  function stop() {
    if (!dragging.value) return;
    dragging.value = false;
    emit("end");
  }
</script>