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
    <!-- a tool CALL row can be a branch's active head, so it hosts the switcher -->
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
    // server-measured, from restored history: beats the live meter
    finalMs?: number;
    elapsedMs?: number;
    answerMs?: number;
    gate?: string;
    variant?: VariantBlock | null;
    // this row's live tail: the chips store repaints this row alone on a landed
    // chunk, which keeps the transcript derivation pure. Absent => no tail.
    callId?: string;
    // this row's DURABLE fold address (toolGroup.ts foldAddress): the tab keys
    // the open/close pin by it, so the pin outlives the unmounting dock switch.
    foldKey?: string;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  // The gate is the SHARED live-set predicate (composables/toolStatus.ts), never a
  // local spelling: the row's own word and its meter must key on the same set.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => isLive(props.status),
    // a restored STILL-RUNNING call counts on from the server's elapsed-so-far
    () => props.elapsedMs ?? 0,
  );

  const answerLabel = computed(() =>
    props.answerMs != null ? formatDurationMs(props.answerMs) : "",
  );

  // the live tail: read from the chips store here, not merged onto the item by
  // the derivation; shown only while the row has no settled result.
  const liveChip = computed(() => chipOf(props.callId));
  const liveText = computed(() => chipDisplay(liveChip.value) || undefined);
  const liveSettled = computed(() => liveChip.value?.settled === true);

  // The settings knob (auto | expanded | collapsed) decides what a row does on
  // its own; a manual summary click PINS it and status/mode changes recompute
  // around the pin, never over it. The element stays uncontrolled (no :open
  // binding) - the browser owns the toggle, this side applies the pin or the
  // mode on mount and on status/mode change only.
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
    // a pin - instance-local or restored - WINS and must be APPLIED here: the
    // DOM is fresh on a remount, or every restored row would render collapsed.
    el.value.open = pin !== null ? pin : toolRowWantOpen(props.status, display.value);
  }

  // nextTick: the status flip and the result's v-if land in the same render pass.
  watch(
    [() => props.status, display],
    () => void nextTick(applyAuto),
  );
  onMounted(applyAuto);

  // Click fires BEFORE the toggle default, so !open is the state it will produce.
  function onSummaryClick() {
    if (!el.value) return;
    rememberPin(!el.value.open);
  }
</script>
