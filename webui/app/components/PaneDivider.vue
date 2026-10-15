<template>
  <div
    class="divider"
    :class="{ active: dragging }"
    @pointerdown="start"
    @pointermove="move"
    @pointerup="stop"
    @pointercancel="stop"
    role="separator"
    aria-orientation="vertical" />
</template>

<script setup lang="ts">
  import { onMounted, ref } from "vue";

  const props = defineProps<{
    width: number;
    min: number;
    invert?: boolean;
    storageKey?: string;
  }>();

  const emit = defineEmits<{
    (e: "update:width", w: number): void;
    (e: "collapsed"): void;
  }>();

  const dragging = ref(false);
  let startX = 0;
  let startWidth = 0;

  const key = () => props.storageKey || "omega.sidebar-width";

  // Hand-rolled drag: width delta = pointer delta from drag start, clamped to
  // [min, max]. Dragging below min emits 'collapsed' so the parent can unmount
  // the pane entirely (VS Code panel behavior); the pane's toggle button
  // reopens it.
  // invert: right-side divider — dragging left widens the pane.
  // ponytail: one divider component used twice via storageKey — for 3+ panes
  // upgrade to the splitpanes package instead of more props here.
  function start(e: PointerEvent) {
    dragging.value = true;
    startX = e.clientX;
    // drag resumes from the RENDERED pane width, not the stored basis: the
    // squeeze chain can flex a pane below its desired width, and a delta
    // computed from the stale basis looks frozen at the clamp.
    const el = e.target as HTMLElement;
    const pane =
      props.invert ? el.nextElementSibling : el.previousElementSibling;
    startWidth = pane instanceof HTMLElement ? pane.offsetWidth : props.width;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }

  function move(e: PointerEvent) {
    if (!dragging.value) return;
    const delta = props.invert ? -(e.clientX - startX) : e.clientX - startX;
    const next = startWidth + delta;
    if (next < props.min) {
      emit("collapsed");
      dragging.value = false;
      return;
    }
    // no max: sidebars are unbounded; the pane-area squeeze chain keeps the
    // chat at its 320px floor and collapses the far sidebar when starved
    emit("update:width", next);
  }

  function stop() {
    dragging.value = false;
  }

  onMounted(() => {
    const saved = localStorage.getItem(key());
    if (saved) emit("update:width", Number(saved));
  });

  function persist(w: number) {
    localStorage.setItem(key(), String(w));
  }

  defineExpose({ persist });
</script>
