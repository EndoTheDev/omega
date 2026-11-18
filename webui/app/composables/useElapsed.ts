import { computed, onUnmounted, ref, watch, type ComputedRef } from "vue";

import { createElapsedMeter, formatDurationMs, type ElapsedMeter } from "./omega.ts";

// useElapsed is the one elapsed-counter for the transcript items (ThinkingBlock. A
// server-measured finalMs wins over the live meter (restored history mounts
// already-final and never starts one). finalMs is the caller's choice of FROZEN number,
// not necessarily one span: the answer row hands over the TURN's wall-clock total,
// because the badge on the last row of a turn is the turn's bottom line - every phase,
// every tool run and every smart-gate wait included - while thinking and tool rows keep
// their own segment's measured duration. Returns the display label: "" until the first
// tick or while the item was never live, so the template's v-if renders nothing. Must
// be called from component setup (it owns a watch and an unmount hook).
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

  // The consumed span is watched too. The meter replaces its base per arrival and
  // ignores a value that does not grow, so this is exactly one jump per real arrival
  // and no double-count on repeats.
  watch(initialMs, () => syncConsumedSpan(meter, initialMs));

  onUnmounted(() => meter.stop());
  return label;
}

// syncConsumedSpan hands the meter the getter's current span.
export function syncConsumedSpan(meter: ElapsedMeter, initialMs: () => number): void {
  meter.reanchor(initialMs());
}