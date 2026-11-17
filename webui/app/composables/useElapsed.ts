import { computed, onUnmounted, ref, watch, type ComputedRef } from "vue";

import { createElapsedMeter, formatDurationMs, type ElapsedMeter } from "./omega.ts";

// useElapsed is the one elapsed-counter for the transcript items
// (ThinkingBlock, ToolCallLine, ChatMessage): it ticks the label every
// second while `live` is true and freezes at the last value when the
// stream or call ends - the finished item keeps showing how long it
// took. A server-measured finalMs wins over the live meter (restored
// history mounts already-final and never starts one). The meter carries
// no interval across unmount: stop() runs on unmount too (clears the
// timer, keeps the last label).
//
// Returns the display label: "" until the first tick or while the item
// was never live, so the template's v-if renders nothing. Must be
// called from component setup (it owns a watch and an unmount hook).
// initialMs is the elapsed already consumed at start (a restored
// running tool counts on from the server's elapsed-so-far) - the VALUE AT
// SETUP, not the whole story: the meter re-anchors when the getter grows
// (syncConsumedSpan), so a row that mounts before its span exists - a
// restored row flipping to streaming once the stream attaches, an
// id-less call gaining started_at on a later paint - jumps to the real
// elapsed once instead of carrying the setup paint's zero for the whole
// run (card t_556bd2ad).
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

  // The consumed span is watched too (card t_556bd2ad): the canvas apply
  // keeps rewriting item.elapsedMs on every paint, and a restored row's
  // span only exists once the wire hands it over. The meter replaces its
  // base per arrival and ignores a value that does not grow, so this is
  // exactly one jump per real arrival and no double-count on repeats.
  watch(initialMs, () => syncConsumedSpan(meter, initialMs));

  onUnmounted(() => meter.stop());
  return label;
}

// syncConsumedSpan hands the meter the getter's current span - the wiring
// between the reactive anchor and the meter. Split out of the watcher so
// the fake-clock test can push a span through a getter without a
// component (the node suite has no DOM).
export function syncConsumedSpan(meter: ElapsedMeter, initialMs: () => number): void {
  meter.reanchor(initialMs());
}