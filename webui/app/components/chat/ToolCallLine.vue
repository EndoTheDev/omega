<template>
  <details
    ref="el"
    class="toolcall">
    <summary @click="onSummaryClick">
      <!-- card t_502b8e81: the status dot became the tools-tab wrench -
           one name one meaning; the status colors ride the same class -->
      <Icon
        name="tools"
        class="tool-ic"
        :class="status" />
      <span class="tool-name">{{ name }}</span>
      <!-- card t_1b3e2a8b: the smart gate's decision record - one dim
           line inside the guarded call's entry, a record not a control
           (no buttons, no re-interaction). Sits in the summary row so
           the decision is visible even when the row's details are
           collapsed. -->
      <span
        v-if="gate"
        class="tool-gate">{{ gate }}</span
      >
      <span
        v-if="elapsedLabel || answerLabel"
        class="tool-elapsed"
        :title="answerLabel ? 'call duration + response return' : undefined"
        >{{ elapsedLabel }}<template v-if="answerLabel"> + {{ answerLabel }}</template></span
      >
      <!-- async agent loop phase 3 (ruling 6): the pending chip's word.
           The result re-enters as a later wake turn on the same
           session - this row fills in when it settles. -->
      <span
        v-if="status === 'pending'"
        class="tool-elapsed">pending</span
      >
    </summary>
    <div
      v-if="output"
      class="tool-section">
      <span class="tool-section-label">input</span>
      <pre class="tool-output">{{ output }}</pre>
    </div>
    <div
      v-if="result"
      class="tool-section">
      <span class="tool-section-label">output</span>
      <pre
        class="tool-output tool-result"
        :class="{ 'is-error': status === 'error' }"
        >{{ result }}</pre
      >
    </div>
    <!-- live chip tail (card t_896cf5a8): the chips topic's streamed
         output while the call runs - the pending chip shows work in
         progress, not a silent gap until the fill. The settled result
         wins: a row with a result renders the section above. -->
    <div
      v-else-if="liveText"
      class="tool-section">
      <span class="tool-section-label">{{ liveSettled ? "output" : "live output" }}</span>
      <pre class="tool-output tool-live">{{ liveText }}</pre>
    </div>
    <!-- variant anchor (t_0df4efd8): a tool CALL row can be a branch's
         active head (a text-less multi-row replay) - the switcher
         mounts on the line's tail -->
    <VariantSwitcher
      v-if="variant"
      class="tool-variant"
      :variant="variant"
      @select="emit('select', $event)" />
  </details>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, ref, watch } from "vue";

  // card t_502b8e81: the iconset wrapper - wrench on the tool call line
  import Icon from "../ui/Icon.vue";
  import { toolRowWantOpen, useToolDisplay } from "../../composables/toolDisplay";
  import { formatDurationMs } from "../../composables/omega";
  import { useElapsed } from "../../composables/useElapsed";
  import VariantSwitcher from "./VariantSwitcher.vue";
  import type { VariantBlock } from "../../composables/omega";

  const props = defineProps<{
    name: string;
    status: string;
    output?: string;
    result?: string;
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
    // response-return duration (card t_e27c8347): the server-measured
    // wall clock from this call's result to the answer's completion -
    // the row's second number beside the call's own elapsed. No live
    // meter: it appears once the answer segment is persisted, absent
    // while it streams and when no answer followed the call.
    answerMs?: number;
    // the smart gate's decision record (card t_1b3e2a8b): the verdict
    // line stamped on the guarded call's result, rendered verbatim.
    // Absent for every ungated/manual/undecided call - no line.
    gate?: string;
    // this row's branch block when the call row is the branch's active
    // head (a text-less multi-row replay) - resolved by ChatTab
    variant?: VariantBlock | null;
    // the chips topic's live tail (card t_896cf5a8): the pending chip's
    // streamed output as it lands, truncation marker included. Rendered
    // only while the row has no settled result (the fill/transcript are
    // the truth).
    liveText?: string;
    liveSettled?: boolean;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  // Elapsed counter (tool_call event -> tool_result arrival): ticks while
  // the call is running, freezes at the final value once the result
  // lands - the finished line keeps showing how long the call took.
  // Restored history arrives already-final and renders nothing. It stays
  // honest: dot and status keep saying running. A PENDING chip ticks too
  // (async agent loop phase 3): the call is genuinely in flight - its
  // result re-enters as a later wake turn - and the meter freezes the
  // same way when that result lands.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => props.status === "running" || props.status === "pending",
    // a restored STILL-RUNNING call counts on from the server's
    // elapsed-so-far (live-run merge) - not from the mount moment
    () => props.elapsedMs,
  );

  // the response-return half (card t_e27c8347): server-measured, never
  // metered - the number lands with the restored payload once the
  // answer segment completed, formatted with the same ruler.
  const answerLabel = computed(() =>
    props.answerMs != null ? formatDurationMs(props.answerMs) : "",
  );

  // --- auto open/close (/tools auto parity, card t_868633b3) ---
  // The settings knob (auto | expanded | collapsed) decides what a row
  // does on its own: auto expands a running call and collapses it once
  // it settles. A manual summary click PINS the row - its open state is
  // the user's for the session, and mode flips and status transitions
  // recompute around it, never over it. The details element stays
  // uncontrolled (no :open binding): the browser owns the toggle, this
  // side applies the mode on mount and on status/mode change only.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useToolDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = toolRowWantOpen(props.status, display.value);
  }

  // nextTick: the status flip and the result's v-if land in the same
  // render pass - apply after the DOM settles.
  watch(
    [() => props.status, display],
    () => void nextTick(applyAuto),
  );
  onMounted(applyAuto);

  // Click fires BEFORE the browser's toggle default action, so !open is
  // the state the click is about to produce - pin that, and auto mode
  // never touches this row again this session.
  function onSummaryClick() {
    if (!el.value) return;
    pinned.value = !el.value.open;
  }
</script>
