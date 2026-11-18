// omega webui: width-driven layout classes. A zero-width read (unmounted measure noise)
// is never narrow.

import { onBeforeUnmount, onMounted, ref, watch, type Ref } from "vue";

/** narrow below this rendered width */
export const NARROW_MAX_PX = 360;

// the pure width comparison, unit-directly (the class binding is a source-pinned.
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}

/** watch `el`'s rendered width; the ref flips true while it is narrow. */
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
    // the class flip changes the observed panel's own layout.
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