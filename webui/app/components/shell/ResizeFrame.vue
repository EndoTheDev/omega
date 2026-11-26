<template>
  <div
    v-for="edge in RESIZE_EDGES"
    :key="edge"
    :class="['resize-strip', `resize-${edge}`]"
    :data-resize-edge="edge"
    aria-hidden="true"
    @mousedown="grab(edge, $event)" />
</template>

<script setup lang="ts">
  // The frameless window's border in the windowed route: the page names the edge a press
  // grabbed and the host runs the OS size loop. Each strip is a NO-DRAG surface, so
  // WebView2's non-client support hands the press to the page - without it the header's
  // app-region band swallows the top edge and the sides at that height. The strips are
  // direct children of the header, never wrapped in one full-window box: a fixed wrapper
  // inside the drag element makes the whole window answer the caption hit test.
  import { RESIZE_EDGES, resizeMessage, type ResizeEdge, type WailsHost } from "../../composables/desktopChrome";

  function grab(edge: ResizeEdge, event: MouseEvent): void {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    (globalThis as unknown as WailsHost)._wails?.invoke?.(resizeMessage(edge));
  }
</script>
