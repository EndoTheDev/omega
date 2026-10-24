<template>
  <section class="canvas-panel">
    <header class="canvas-panel-head">
      <span class="canvas-title">{{ panel.title }}</span>
      <span class="canvas-kind">{{ panel.kind }}</span>
    </header>
    <div
      v-if="rows.length"
      class="canvas-rows">
      <div
        v-for="(r, i) in rows"
        :key="i"
        class="canvas-row"
        :class="{ 'canvas-row-read': r.read === true }">
        <span class="canvas-row-text">{{ rowText(r, i) }}</span>
        <button
          v-if="rowBinding(panel, r)"
          class="canvas-row-action"
          :disabled="stateOf(panel.id, rowBinding(panel, r)!.action.id).status === 'pending'"
          @click="$emit('invoke', panel, rowBinding(panel, r)!.action, rowBinding(panel, r)!.args)">
          {{ rowBinding(panel, r)!.action.label }}
        </button>
      </div>
    </div>
    <pre
      v-else
      class="canvas-payload">{{ payloadText }}</pre>
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
  import { formatCanvasPayload, toastRowOf, type CanvasActionState } from "../../composables/canvas";
  import type { CanvasPanel, CanvasPanelAction } from "../../composables/omega";

  // The GENERIC panel renderer (arc 3c): consumes one panel descriptor
  // (schema v1) and renders title + payload view + action buttons. Kind
  // handling v1 is the plain/default rendering; the toast kind joined in
  // card t_aca53fee (one read-only record row - the JSON dump is debug
  // output, not a panel body). The
  // component owns NO state: pending/error come from the canvas store
  // through the stateOf bridge (per panel+action), and an action click
  // emits to the owner, which routes it through the one action pipe.
  const props = defineProps<{
    panel: CanvasPanel;
    stateOf: (panelId: string, actionId: string) => CanvasActionState;
  }>();

  defineEmits<{
    invoke: [panel: CanvasPanel, action: CanvasPanelAction, args?: unknown];
  }>();

  const payloadText = computed(() => formatCanvasPayload(props.panel.payload));
  const pendingLabel = "pending";

  // Rows (arc 4b vocabulary growth): a payload carrying a `rows` array
  // renders row lines instead of the JSON dump; a row may carry one
  // action binding (`action: {action, label, args}`) - the renderer
  // stays dumb: it renders the declared label, resolves the call
  // against the panel's declared actions (module comes from the
  // descriptor, never from the row) and passes the declared args.
  // Notifications is the first consumer; nothing here knows the word.
  interface CanvasRowBinding {
    action: string;
    label?: string;
    args?: unknown;
  }
  interface CanvasRow {
    id?: unknown;
    kind?: unknown;
    title?: unknown;
    verdict?: unknown;
    read?: unknown;
    action?: unknown;
  }
  const rows = computed<CanvasRow[]>(() => {
    // the toast panel (arc 4b, card t_aca53fee): the payload IS one
    // record row - render it read-only through the same row line as the
    // list panel (kind + title, width-truncated), never the raw JSON
    // dump. The pure gate (toastRowOf) decides; nothing here knows the
    // record shape beyond the row vocabulary below.
    const toast = toastRowOf(props.panel.kind, props.panel.payload);
    if (toast) return [toast as CanvasRow];
    const p = props.panel.payload;
    if (!p || typeof p !== "object") return [];
    const rs = (p as { rows?: unknown }).rows;
    if (!Array.isArray(rs)) return [];
    return rs.filter((r): r is CanvasRow => !!r && typeof r === "object");
  });

  function bindingOf(r: CanvasRow): CanvasRowBinding | null {
    const b = r.action;
    if (!b || typeof b !== "object") return null;
    const cast = b as CanvasRowBinding;
    if (typeof cast.action !== "string" || !cast.action) return null;
    return cast;
  }

  // rowBinding resolves the row's declared call against the panel's
  // actions: the descriptor owns module + state key, the row owns
  // args. An unknown action id renders no button (the row binding
  // cannot invent a module).
  function rowBinding(panel: CanvasPanel, r: CanvasRow): { action: CanvasPanelAction; args?: unknown } | null {
    const b = bindingOf(r);
    if (!b) return null;
    const a = panel.actions.find((x) => x.id === b.action);
    if (!a) return null;
    return { action: a, args: b.args };
  }

  function rowText(r: CanvasRow, i: number): string {
    const kind = typeof r.kind === "string" && r.kind ? r.kind : "";
    const title = typeof r.title === "string" ? r.title : "";
    const verdict = r.verdict === "failed" ? " (failed)" : "";
    const id = typeof r.id === "number" || typeof r.id === "string" ? String(r.id) : String(i + 1);
    return `#${id} ${kind ? kind + ": " : ""}${title}${verdict}`.trim() || `#${id}`;
  }

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