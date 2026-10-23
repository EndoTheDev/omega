<template>
  <section class="canvas-panel">
    <header class="canvas-panel-head">
      <span class="canvas-title">{{ panel.title }}</span>
      <span class="canvas-kind">{{ panel.kind }}</span>
    </header>
    <pre class="canvas-payload">{{ payloadText }}</pre>
    <div
      v-if="panel.actions.length"
      class="canvas-actions">
      <button
        v-for="a in panel.actions"
        :key="a.id"
        class="canvas-action"
        :disabled="stateOf(panel.id, a.id).status === 'pending'"
        @click="$emit('invoke', panel, a)">
        {{ stateOf(panel.id, a.id).status === 'pending' ? pendingLabel : a.label }}
      </button>
    </div>
    <p
      v-if="errorText"
      class="canvas-error">{{ errorText }}</p>
  </section>
</template>

<script setup lang="ts">
  import { computed } from "vue";
  import { formatCanvasPayload, type CanvasActionState } from "../../composables/canvas";
  import type { CanvasPanel, CanvasPanelAction } from "../../composables/omega";

  // The GENERIC panel renderer (arc 3c): consumes one panel descriptor
  // (schema v1) and renders title + payload view + action buttons. Kind
  // handling for v1 is the plain/default rendering ONLY (settled Q2b) -
  // rich kinds land as real migrations, each owning its panel UI. The
  // component owns NO state: pending/error come from the canvas store
  // through the stateOf bridge (per panel+action), and an action click
  // emits to the owner, which routes it through the one action pipe.
  const props = defineProps<{
    panel: CanvasPanel;
    stateOf: (panelId: string, actionId: string) => CanvasActionState;
  }>();

  defineEmits<{
    invoke: [panel: CanvasPanel, action: CanvasPanelAction];
  }>();

  const payloadText = computed(() => formatCanvasPayload(props.panel.payload));
  const pendingLabel = "pending";

  // the LAST errored action's reason renders - actions are independent
  // pipe calls; each failed button's error shows under the action row
  const errorText = computed(() => {
    for (let i = props.panel.actions.length - 1; i >= 0; i--) {
      const st = props.stateOf(props.panel.id, props.panel.actions[i]!.id);
      if (st.status === "error" && st.error) return st.error;
    }
    return null;
  });
</script>