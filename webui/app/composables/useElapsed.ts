import { computed, onUnmounted, ref, watch, type ComputedRef } from "vue";

import { createElapsedMeter, formatDurationMs, type ElapsedMeter } from "./omega.ts";

// the one elapsed-counter for transcript items. A server-measured finalMs wins over the live
// meter (restored history mounts already-final); finalMs is the caller's FROZEN number, not
// necessarily one span (the answer row hands over the TURN's total, thinking and tool rows
// their own segment). Returns "" until the first tick. Component setup only.
export function useElapsed(
  finalMs: () => number | null | undefined,
  live: () => boolean,
  initialMs: () => number = () => 0,
): ComputedRef<string> {
  const elapsed = ref("");
  const meter = createElapsedMeter(
    (label) => {
      elapsed.value = label;
    },
    undefined,
    undefined,
    initialMs(),
  );
  const label = computed(() => {
    const ms = finalMs();
    return ms != null ? formatDurationMs(ms) : elapsed.value;
  });

  watch(
    live,
    (isLive) => {
      if (isLive) meter.start();
      else meter.stop();
    },
    { immediate: true },
  );

  // The consumed span is watched too: the meter replaces its base per arrival and ignores a
  // value that does not grow, so this is one jump per real arrival, no double-count.
  watch(initialMs, () => syncConsumedSpan(meter, initialMs));

  onUnmounted(() => meter.stop());
  return label;
}

export function syncConsumedSpan(meter: ElapsedMeter, initialMs: () => number): void {
  meter.reanchor(initialMs());
}