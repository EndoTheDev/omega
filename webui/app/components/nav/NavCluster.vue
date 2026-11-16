<template>
  <!-- card t_6215474b: the nav cluster - back / forward / command palette.
       card t_20fd8745 (Endo ruling 2026-11-13): it seats DEAD CENTER on
       the menubar's full width (the absolute-middle trick the deleted
       'Omega' wordmark used), and its middle element is a palette SEAT -
       closed, a button dressed as an input whose placeholder is the live
       clock; card t_cc6d0920 (HIS 2026-11-16 reconfirmation): OPEN, the
       palette's own field teleports into that seat (see the slot below),
       so the time field becomes the input in place. VS Code states: the
       ends DIM via the disabled attribute - a real disabled button, never
       a click-swallowing lookalike. Buttons, the seat and the Alt+Left /
       Alt+Right keys share ONE path (the nav composable's back/forward;
       app.vue's one palette toggle). -->
  <div
    class="nav-cluster"
    :class="{ 'palette-open': paletteOpen }"
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
    <!-- card t_20fd8745: the affordance. Its "placeholder" is a
         LIVE clock (re-rendered on every minute boundary, never a frozen
         label). aria-hidden on the clock: the affordance's NAME is the
         command-palette hint - the time is decoration, not information.
         data-palette-trigger: CommandPalette's outside-click handler
         exempts it, so opening the palette with this click cannot close
         it again on the way out.
         card t_cc6d0920 (the TAKEOVER): while the palette is open this
         button STANDS DOWN and the palette's real input teleports into
         the seat below - `.nav-palette-slot` is that seat, and
         `display: contents` makes the slot transparent to the cluster's
         flex layout, so the live field lands in this exact box.
         The clock keeps ticking while hidden: the timer belongs to this
         component, not to the button's visibility, so Esc brings back a
         current time, never a stale one. -->
    <button
      v-if="!paletteOpen"
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
    <!-- the palette's seat: CommandPalette teleports its input here.
         Empty while the palette is closed (display: contents = no box,
         no gap). -->
    <span
      id="nav-palette-slot"
      class="nav-palette-slot" />
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
  // the computed's values down - cluster stays a dumb four-control strip).
  // card t_cc6d0920: `paletteOpen` is the takeover's one bit - the cluster
  // hides its chrome (the arrows + the Ctrl+K toggle, via .palette-open's
  // visibility rule, so the seat never moves) and the affordance stands
  // down for the teleported field.
  defineProps<{
    canBack: boolean;
    canForward: boolean;
    paletteOpen: boolean;
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
