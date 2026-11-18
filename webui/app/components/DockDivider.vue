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
  import { ref } from "vue";

  const props = defineProps<{
    column?: boolean;
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