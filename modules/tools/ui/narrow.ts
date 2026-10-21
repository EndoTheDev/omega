// Module-owned narrow composable (card t_a2fdc630): the module-UI
// responsive recipe. A module component measures its OWN rendered
// container (not the viewport media query) and flips a `narrow` class
// below the threshold - the kanban Board/boardLogic.ts pattern, copied
// per-module because module UI never imports shell code (the
// composition contract, modules/ui/AGENTS.md + composition.test.ts).
//
// The threshold must agree with the shell's NARROW_MAX_PX=
// 360 (modules/ui/app/composables/narrow.ts) - pinned by
// modules/ui/test/modulenarrow.test.ts. A component whose real minimum
// readable width differs derives its own value there instead of
// reusing this default.
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
  let node: HTMLElement | null = null;

  function measure() {
    if (!node) return;
    const w = node.clientWidth;
    if (w > 0) narrow.value = isNarrowWidth(w);
  }
  function observe(next: HTMLElement | null) {
    if (node === next) {
      measure();
      return;
    }
    observer?.disconnect();
    observer = null;
    node = null;
    if (!next) return;
    if (typeof ResizeObserver === "undefined") return; // ancient host: class stays off
    node = next;
    // the class flip changes the observed layout itself; delivering on
    // the next frame avoids re-entering the observer (ResizeObserver
    // loop console noise - the shell copy's lesson)
    observer = new ResizeObserver(() => {
      requestAnimationFrame(measure);
    });
    observer.observe(next);
    measure();
  }

  onMounted(() => observe(el.value));
  onBeforeUnmount(() => {
    observer?.disconnect();
    observer = null;
    node = null;
  });
  watch(el, (next) => observe(next));
  return narrow;
}