// omega webui: width-driven layout classes (card t_49d0b308).
//
// Panels designed for the ~main-pane width now live in the inspector
// (settings, and the kanban board's own in-module copy): the same
// component renders wide in the main pane and ~300px wide in the
// sidebar column. One boolean drives the adaptation: a ResizeObserver
// on the panel's ROOT watches the REAL rendered width and flips a
// `narrow` class below the threshold (JS-measured rather than a
// container query because the unit suite pins the class presence, and
// the SFC styles stay plain class rules).
//
// Threshold: above the 320 snap point + slack - the inspector's default
// 240-300 and the 128 floor all read narrow; a drag past the 320/640
// attractor widens back. A zero-width read (unmounted measure noise)
// is never narrow.

import { onBeforeUnmount, onMounted, ref, watch, type Ref } from "vue";

/** narrow below this rendered width */
export const NARROW_MAX_PX = 360;

// the pure width comparison, unit-directly (the class binding is a
// source-pinned assertion)
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}

/** watch `el`'s rendered width; the ref flips true while it is narrow.
    Re-observes when the ref moves (hosted tabs remount freely). */
export function useNarrow(el: Ref<HTMLElement | null>): Ref<boolean> {
  const narrow = ref(false);
  let observer: ResizeObserver | null = null;
  let observed: Element | null = null;

  function stop() {
    observer?.disconnect();
    observer = null;
    observed = null;
  }
  function observe(node: HTMLElement | null) {
    if (!node || observed === node) {
      if (node) measure(node);
      return;
    }
    stop();
    if (typeof ResizeObserver === "undefined") return; // ancient host: class stays off
    observed = node;
    // the class flip changes the observed panel's own layout; applying it
    // INSIDE the observer callback can re-enter the resize cycle and
    // surface the benign "ResizeObserver loop" console error - deliver
    // on the next frame instead
    observer = new ResizeObserver((entries) => {
      const pending = entries
        .map((e) => [e.target as HTMLElement, Math.round(e.contentRect.width)] as const)
        .filter(([, w]) => w > 0);
      if (pending.length === 0) return;
      requestAnimationFrame(() => {
        for (const [node, w] of pending) {
          if (observed === node) narrow.value = isNarrowWidth(w);
        }
      });
    });
    observer.observe(node);
    measure(node);
  }
  function measure(node: HTMLElement) {
    const w = node.clientWidth;
    if (w > 0) narrow.value = isNarrowWidth(w);
  }

  onMounted(() => observe(el.value));
  onBeforeUnmount(stop);
  watch(el, (n) => observe(n));
  return narrow;
}