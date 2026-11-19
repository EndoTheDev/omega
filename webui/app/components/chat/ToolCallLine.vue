<template>
  <details
    ref="el"
    class="toolcall">
    <summary @click="onSummaryClick">
      <Icon
        name="tools"
        class="tool-ic"
        :class="status" />
      <span class="tool-name">{{ name }}</span>
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
    <div
      v-else-if="liveText"
      class="tool-section">
      <span class="tool-section-label">{{ liveSettled ? "output" : "live output" }}</span>
      <pre class="tool-output tool-live">{{ liveText }}</pre>
    </div>
    <!--
      variant anchor: a tool CALL row can be a branch's active head (a text-less.
    -->
    <VariantSwitcher
      v-if="variant"
      class="tool-variant"
      :variant="variant"
      @select="emit('select', $event)" />
  </details>
</template>

<script setup lang="ts">
  import { computed, inject, nextTick, onMounted, ref, watch } from "vue";

  import Icon from "../ui/Icon.vue";
  import { CHAT_FOLD_PINS_KEY } from "../../composables/chat";
  import { toolRowWantOpen, useToolDisplay } from "../../composables/toolDisplay";
  import { formatDurationMs } from "../../composables/omega";
  import { chipDisplay, chipOf } from "../../composables/chips";
  import { isLive } from "../../composables/toolStatus";
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
    elapsedMs?: number;
    answerMs?: number;
    gate?: string;
    variant?: VariantBlock | null;
    // the TOOL CALL id: the row reads its own live
    // tail from the chips store by this key - a landed chunk repaints
    // exactly this row, and the transcript derivation stays pure.
    // Absent on an id-less call (nothing to attach to) - no tail.
    callId?: string;
    // this row's DURABLE fold address: the tab's view
    // record keys the reader's manual open/close pin by it, so the pin
    // survives the dock-tab switch that unmounts the whole pane. Derived
    // once, upstream, by toolGroup.ts foldAddress - absent on a row
    // without a durable identity (nothing to pin against after a remount).
    foldKey?: string;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  // Elapsed counter (tool_call event -> tool_result arrival). The gate is the SHARED
  // live-set predicate (composables/toolStatus.ts), never a local spelling: the row's
  // own word and its meter must key on the same set.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => isLive(props.status),
    // a restored STILL-RUNNING call counts on from the server's elapsed-so-far.
    () => props.elapsedMs ?? 0,
  );

  // the response-return half: server-measured, never metered.
  const answerLabel = computed(() =>
    props.answerMs != null ? formatDurationMs(props.answerMs) : "",
  );

  // the live chip tail (cards +): the chips topic's
  // streamed output for THIS row's call, read from the store here instead
  // of merged onto the item by the transcript derivation. The store is
  // reactive per call id, so a landed chunk repaints exactly the row that
  // owns the call. Rendered only while the row has no settled result (the
  // fill/transcript are the truth); the truncation marker rides the tail.
  const liveChip = computed(() => chipOf(props.callId));
  const liveText = computed(() => chipDisplay(liveChip.value) || undefined);
  const liveSettled = computed(() => liveChip.value?.settled === true);

  // --- auto open/close (/tools auto parity, ---
  // The settings knob (auto | expanded | collapsed) decides what a row
  // does on its own: auto expands a running call and collapses it once
  // it settles. A manual summary click PINS the row - its open state is
  // the USER's, and mode flips and status transitions recompute around
  // it, never over it.
  //
  // The pin is stored, not remembered: the pane unmounts
  // on every dock-tab switch (DockPane renders only the ACTIVE tab), so an
  // instance-local pin died with the instance and the row came back closed.
  // The pin lives on the tab's view record, addressed by the row's DURABLE
  // identity (foldAddress: call id, else store row id) - the address a
  // remount, a refetch and the sliding 40-row window all preserve. A row
  // with no durable address keeps the instance-local pin (a slot pin would
  // land on a different call after the remount). The details element stays
  // uncontrolled (no :open binding): the browser owns the toggle, this side
  // applies the pin or the mode on mount and on status/mode change only.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useToolDisplay();
  const folds = inject(CHAT_FOLD_PINS_KEY, null);
  const localPin = ref<boolean | null>(null);
  const foldKey = computed(() => props.foldKey ?? "");
  const pinned = computed<boolean | null>(() => {
    const key = foldKey.value;
    if (key && folds) return folds.get(key) ?? null;
    return localPin.value;
  });

  function rememberPin(open: boolean) {
    const key = foldKey.value;
    if (key && folds) folds.set(key, open);
    else localPin.value = open;
  }

  function applyAuto() {
    if (!el.value) return;
    const pin = pinned.value;
    // a pin - this instance's or one the tab's view record kept - WINS. On
    // a remount the DOM is fresh (a <details> mounts closed), so the
    // restored pin has to be APPLIED here; skipping a pinned row would
    // render every restored row collapsed.
    el.value.open = pin !== null ? pin : toolRowWantOpen(props.status, display.value);
  }

  // nextTick: the status flip and the result's v-if land in the same render pass.
  watch(
    [() => props.status, display],
    () => void nextTick(applyAuto),
  );
  onMounted(applyAuto);

  // Click fires BEFORE the browser's toggle default action, so !open is
  // the state the click is about to produce - pin that, and auto mode
  // never touches this row again this session (or, with the stored pin,
  // this tab).
  function onSummaryClick() {
    if (!el.value) return;
    rememberPin(!el.value.open);
  }
</script>
