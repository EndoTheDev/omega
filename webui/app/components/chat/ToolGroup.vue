<template>
  <!-- consecutive tool calls collapse into ONE group block; a group is only a
       container - each child row renders what ChatItem's tool branch renders. -->
  <details
    ref="el"
    class="toolgroup">
    <summary @click="onSummaryClick">
      <Icon
        name="tools"
        class="tool-ic"
        :class="state" />
      <span class="toolgroup-label">{{ items.length }} tool calls</span>
      <span
        v-if="live"
        class="toolgroup-live"
        >…</span
      >
    </summary>
    <div class="toolgroup-body">
      <!-- children key on the CALL's identity (toolChildKey), never the slot. -->
      <template
        v-for="(item, i) in items"
        :key="toolChildKey(item, i)">
        <ToolCallLine
          :name="item.name"
          :status="item.status"
          :final-ms="item.finalMs"
          :elapsed-ms="item.elapsedMs"
          :answer-ms="item.answerMs"
          :output="item.output"
          :result="item.result"
          :call-id="item.callId"
          :fold-key="foldAddress(item)"
          :gate="item.gate" />
        <!-- index-aligned with items: the gate question bound to THAT tool row -->
        <GateCard
          v-if="gateCards[i]"
          :card="gateCards[i]!"
          :session-id="sessionId" />
      </template>
    </div>
  </details>
</template>

<script setup lang="ts">
  import { computed, inject, nextTick, onMounted, ref, watch } from "vue";

  import GateCard from "./GateCard.vue";
  import Icon from "../ui/Icon.vue";
  import ToolCallLine from "./ToolCallLine.vue";
  import type { PendingVerdictCard, ToolItem } from "../../composables/omega";
  import { CHAT_FOLD_PINS_KEY } from "../../composables/chat";
  import { foldAddress, toolChildKey, toolGroupWantOpen } from "../../composables/toolGroup";
  import { useToolDisplay } from "../../composables/toolDisplay";
  import { isLive } from "../../composables/toolStatus";

  const props = defineProps<{
    items: ToolItem[];
    gateCards: (PendingVerdictCard | null)[];
    sessionId?: string;
    live?: boolean;
    // this group's DURABLE fold address: ChatTab derives it from the head row -
    // the row the container is keyed by - so the pin survives the dock switch
    // that unmounts the pane. Absent = auto only.
    foldKey?: string;
  }>();

  const anyError = computed(() => props.items.some((it) => it.status === "error"));
  const anyActive = computed(() => props.items.some((it) => isLive(it.status)));
  const state = computed(() =>
    anyError.value ? "error" : anyActive.value ? "" : "done",
  );

  // The shared mode decides what the group does on its own; a manual summary
  // click PINS it and mode/status changes recompute around the pin, never over
  // it. The pin is stored on the tab's view record - a dock-tab switch unmounts
  // the pane, so an instance pin would die - by the group's durable address.
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
    // a pin WINS and must be re-applied to the fresh DOM on remount - a
    // <details> mounts closed - or every restored group renders collapsed.
    el.value.open =
      pin !== null
        ? pin
        : toolGroupWantOpen(anyActive.value, props.live ?? false, display.value);
  }

  // nextTick: child statuses, the live gate and the mode flip land in render passes.
  watch(
    [anyActive, () => props.live, display],
    () => void nextTick(applyAuto),
  );
  onMounted(applyAuto);

  // Click fires BEFORE the toggle default, so !open is the state it will produce.
  function onSummaryClick() {
    if (!el.value) return;
    rememberPin(!el.value.open);
  }
</script>
