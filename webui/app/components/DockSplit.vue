<template>
  <div
    ref="rootEl"
    class="dock-split"
    :class="[node.direction, cellStyle ? 'dock-cell' : 'dock-root']"
    :style="cellStyle">
    <!--
      root-pane case (the dock-blank fix): removePane's hoist collapses split[last]
      into a bare pane as the TREE root - DockSplit is the unconditional root renderer,
      so it must also render a pane-rooted tree: one cell filling the whole area, no
      dividers, same slots.
    -->
    <DockPane
      v-if="node.kind === 'pane'"
      :node="node"
      :cell-style="{ width: '100%', height: '100%' }">
      <template
        v-for="(_, name) in $slots"
        #[name]="slotProps">
        <slot
          :name="name"
          v-bind="slotProps ?? {}" />
      </template>
    </DockPane>
    <template
      v-else
      v-for="(child, idx) in node.children"
      :key="child.id">
      <DockSplit
        v-if="child.kind === 'split'"
        :node="child"
        :cell-style="cellStyleFor(child, idx)">
        <template
          v-for="(_, name) in $slots"
          #[name]="slotProps">
          <slot
            :name="name"
            v-bind="slotProps ?? {}" />
        </template>
      </DockSplit>
      <DockPane
        v-else
        :node="child"
        :cell-style="cellStyleFor(child, idx)">
        <template
          v-for="(_, name) in $slots"
          #[name]="slotProps">
          <slot
            :name="name"
            v-bind="slotProps ?? {}" />
        </template>
      </DockPane>
      <DockDivider
        v-if="idx < node.children.length - 1"
        :column="node.direction === 'column'"
        :snapped="dragging === idx + 1 && snappedIdx === idx + 1"
        @drag="(d, r) => onDrag(idx + 1, d, r)"
        @end="onEnd" />
    </template>

  </div>
</template>

<script setup lang="ts">
  // The recursive layout renderer.
  import { inject, onBeforeUnmount, onMounted, ref } from "vue";
  import { snapCandidateAt, travelSnapAt, SNAP_STEP, BOTTOM_SNAP_STEP } from "../composables/panes";
  import { dividerDelta, type DividerPointerState } from "../composables/dividerGesture";
  import {
    DOCK_API_KEY,
    CHAT_FLOOR_PX,
    type DockNode,
    type DockPane as DockPaneNode,
    type DockSplit,
  } from "../composables/dock";
  import DockPane from "./DockPane.vue";
  import DockDivider from "./DockDivider.vue";

  const props = defineProps<{
    node: DockSplit | DockPaneNode;
    cellStyle?: Record<string, string>;
  }>();

  const api = inject(DOCK_API_KEY)!;
  const rootEl = ref<HTMLElement | null>(null);

  // how this child is sized: fixed px panes keep their size (exactly the pre-dock
  // sidebar behavior).
  function cellStyleFor(child: DockNode, idx: number): Record<string, string> {
    const node = props.node as DockSplit; // root may be a bare pane; this runs for splits only
    const axis = node.direction === "row" ? "width" : "height";
    if (child.kind === "pane" && child.fixed !== null) {
      if (node.direction === "column") {
        return {
          [axis]: child.fixed + "px",
          "max-height": `calc(100% - ${CHAT_FLOOR_PX}px)`,
          flex: "0 0 auto",
        };
      }
      return { [axis]: child.fixed + "px", flex: "0 0 auto" };
    }
    const n = node.children.length;
    const totalFlex = node.children.reduce(
      (a, c, k) =>
        (c.kind !== "pane" || c.fixed === null) ? a + (node.sizes[k] ?? 1 / n) : a,
      0,
    );
    const grow = (node.sizes[idx] ?? 1 / n) / Math.max(totalFlex, 0.000001);
    return { [axis]: "auto", flex: `${grow} 1 0` };
  }

  // -- drag --- dragging = divider index during a gesture. The raw->delta reduction
  // (cumulative-from-baseline) lives in dividerGesture.ts so its contract is
  // unit-tested - found it silently sending per-event increments (dead-looking
  // dividers) after the snap-ladder rework.
  const dragging = ref<number | null>(null);
  let startPx: number[] | null = null;
  const pointerState: DividerPointerState = { base: null };
  const snappedIdx = ref<number | null>(null);

  function measure(): number[] {
    const node = props.node as DockSplit;
    const cells = [...(rootEl.value?.children ?? [])].filter((el) =>
      (el as HTMLElement).classList.contains("dock-cell"),
    ) as HTMLElement[];
    const axis = node.direction === "row" ? "offsetWidth" : "offsetHeight";
    return node.children.map((_, i) => cells[i]?.[axis] ?? 0);
  }

  function dropGesture() {
    dragging.value = null;
    startPx = null;
    pointerState.base = null;
    snappedIdx.value = null;
  }

  function onDrag(dividerIndex: number, delta: number, raw: number) {
    if (dragging.value !== dividerIndex) {
      if (dragging.value !== null) return; // another divider's gesture owns this pointer
      dragging.value = dividerIndex;
      startPx = measure();
      // v0.14.4 a RESIZE is ONE layout op per gesture.
      api.beginResize();
      pointerState.base = null; // fresh gesture: the reducer re-derives the base
    }
    if (!startPx) return;
    const cumulative = dividerDelta(pointerState, delta, raw);
    if (cumulative === null) return;
    // content box for the pure math.
    const contentPx = startPx.reduce((a, p) => a + p, 0);
    const res = api.drag((props.node as DockSplit).id, dividerIndex, startPx, cumulative, contentPx);
    if (res.collapsedPaneId) {
      // the controlled pane (and this divider with it) is gone.
      dropGesture();
      return;
    }
    // the controlled pane is left (fixed) or, when it flexes, the fixed right one.
    const node = props.node as DockSplit;
    const li = dividerIndex - 1;
    const i = dividerIndex;
    // mirror the pure fn's branch read (a nested split child is flexible - kind must.
    const fixedPane = (k: number): boolean =>
      node.children[k]!.kind === "pane" &&
      (node.children[k] as DockPaneNode).fixed !== null;
    const bottomCase = node.direction === "column" && fixedPane(i);
    const px = bottomCase
      ? startPx[i] - cumulative
      : fixedPane(li)
        ? startPx[li] + cumulative
        : fixedPane(i)
          ? startPx[i] - cumulative
          : startPx[li] + cumulative;
    const step = bottomCase ? BOTTOM_SNAP_STEP : SNAP_STEP;
    // the guide mirrors the drag law.
    const bothFlexible = !fixedPane(li) && !fixedPane(i);
    const attract = bothFlexible
      ? travelSnapAt(startPx[li], px, step)
      : snapCandidateAt(px, step);
    snappedIdx.value = attract !== null ? dividerIndex : null;
  }

  function onEnd() {
    dropGesture();
  }

  // v0.14.4 Modifier-Z at the dock root walks the layout back one op.
  function onUndoKey(e: KeyboardEvent) {
    if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== "z" || e.shiftKey) return;
    const t = e.target as HTMLElement | null;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
    e.preventDefault();
    e.stopPropagation();
    api.undoLayout();
  }
  onMounted(() => {
    if (!props.cellStyle) window.addEventListener("keydown", onUndoKey);
  });
  onBeforeUnmount(() => {
    window.removeEventListener("keydown", onUndoKey);
  });
</script>