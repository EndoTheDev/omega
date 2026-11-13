<template>
  <!-- card t_6215474b: the nav cluster - back / forward / command palette.
       card t_20fd8745 (Endo ruling 2026-11-13): it seats DEAD CENTER on
       the menubar's full width (the absolute-middle trick the deleted
       'Omega' wordmark used), and its middle element is a fake-input
       palette AFFORDANCE: a button dressed as an input, never a real one
       - no typing, no second focus path (the dialog owns the only search
       field). VS Code states: the ends DIM via the disabled attribute - a
       real disabled button, never a click-swallowing lookalike. Buttons,
       the affordance and the Alt+Left / Alt+Right keys share ONE path
       (the nav composable's back/forward; app.vue's one palette toggle). -->
  <div
    class="nav-cluster"
    role="group"
    aria-label="navigation">
    <button
      class="icon-btn nav-btn"
      aria-label="Back"
      title="Back (Alt+Left)"
      :disabled="!canBack"
      @click="$emit('back')">
      <Icon
        name="nav-back"
        :size="16" />
    </button>
    <button
      class="icon-btn nav-btn"
      aria-label="Forward"
      title="Forward (Alt+Right)"
      :disabled="!canForward"
      @click="$emit('forward')">
      <Icon
        name="nav-forward"
        :size="16" />
    </button>
    <!-- card t_20fd8745: the fake-input affordance. Its "placeholder" is a
         LIVE clock (re-rendered on every minute boundary, never a frozen
         label). aria-hidden on the clock: the affordance's NAME is the
         command-palette hint - the time is decoration, not information.
         data-palette-trigger: CommandPalette's outside-click handler
         exempts it, so opening the palette with this click cannot close
         it again on the way out. -->
    <button
      class="nav-palette-input"
      type="button"
      aria-label="Command palette (Ctrl+K)"
      title="Command palette (Ctrl+K)"
      data-palette-trigger
      @click="$emit('open-palette')">
      <span
        class="nav-palette-clock"
        aria-hidden="true">{{ clock }}</span>
    </button>
    <button
      class="icon-btn nav-btn"
      aria-label="Command palette"
      title="Command palette (Ctrl+K)"
      data-palette-trigger
      @click="$emit('open-palette')">
      <Icon
        name="palette"
        :size="16" />
    </button>
  </div>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import Icon from "../ui/Icon.vue";

  // the dims ride the nav history's canBack/canForward (app.vue passes
  // the computed's values down - cluster stays a dumb four-control strip)
  defineProps<{
    canBack: boolean;
    canForward: boolean;
  }>();

  defineEmits<{
    (e: "back"): void;
    (e: "forward"): void;
    (e: "open-palette"): void;
  }>();

  // --- the fake input's placeholder: a LIVE clock -----------------------
  // 24h HH:MM, the shape the session rows and cron's next-run lines print.
  // The timer re-arms on the NEXT minute boundary, so the label is never
  // more than a minute stale and the bar re-renders once a minute - not
  // once a second, and never at all once the component is gone.
  function formatClock(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  const clock = ref(formatClock(new Date()));
  let clockTimer: ReturnType<typeof setTimeout> | null = null;

  function armClock(): void {
    // ms to the next minute boundary (a whole minute on the exact edge)
    const rest = 60_000 - (Date.now() % 60_000);
    clockTimer = setTimeout(() => {
      clock.value = formatClock(new Date());
      armClock();
    }, rest);
  }

  onMounted(armClock);
  onBeforeUnmount(() => {
    if (clockTimer !== null) clearTimeout(clockTimer);
    clockTimer = null;
  });
</script>
