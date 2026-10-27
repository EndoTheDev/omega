import { computed, onUnmounted, ref, watch, type ComputedRef } from "vue";

import { createElapsedMeter, formatDurationMs } from "./omega";

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
// running tool counts on from the server's elapsed-so-far).
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

  onUnmounted(() => meter.stop());
  return label;
}