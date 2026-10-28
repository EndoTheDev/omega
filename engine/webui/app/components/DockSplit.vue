<template>
  <div
    ref="rootEl"
    class="dock-split"
    :class="[node.direction, cellStyle ? 'dock-cell' : 'dock-root']"
    :style="cellStyle">
    <!-- root-pane case (the dock-blank fix): removePane's hoist collapses
         split[last] into a bare pane as the TREE root - DockSplit is the
         unconditional root renderer, so it must also render a pane-rooted
         tree: one cell filling the whole area, no dividers, same slots. -->
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
    <!-- v0.14.4 ruling 5: the live snap readout under the dragged divider -->
    <div
      v-if="dragging !== null && readout"
      class="dock-resize-readout"
      :class="{ snap: readout.rung !== null }"
      :style="readoutStyle()"
      role="status">
      {{ readout.text }}
    </div>
  </div>
</template>

<script setup lang="ts">
  // The recursive layout renderer (card t_63253264): walks the dock tree,
  // rendering splits (rows/columns of cells sized by the tree's fixed/sizes
  // data) and panes. Named slots flow down so app.vue's region templates
  // surface inside whichever pane hosts their component. Resize: the
  // divider reports raw pointer positions; THIS component measures the
  // rendered cells at gesture start and hands everything to the store,
  // whose pure math owns floors (128 drag / 320 chat) and the snap
  // ladder (+320, panes.ts).
  import { inject, ref } from "vue";
  import { snapCandidateAt, SNAP_STEP, BOTTOM_SNAP_STEP } from "../composables/panes";
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
    /** set on nested cells by the PARENT split; the ROOT gets dock-root CSS */
    cellStyle?: Record<string, string>;
  }>();

  const api = inject(DOCK_API_KEY)!;
  const rootEl = ref<HTMLElement | null>(null);

  // how this child is sized: fixed px panes keep their size (exactly the
  // pre-dock sidebar behavior); flexible children share the remainder
  // proportionally to their fraction (flex-grow, basis 0). The fractions
  // normalize over ALL children (persistence), but flex-grow is a RATIO
  // only among the flexible ones - a sum < 1 leaves free space
  // UNDISTRIBUTED (flex spec), so grow renormalizes among flexible cells.
  function cellStyleFor(child: DockNode, idx: number): Record<string, string> {
    const node = props.node as DockSplit; // root may be a bare pane; this runs for splits only
    const axis = node.direction === "row" ? "width" : "height";
    if (child.kind === "pane" && child.fixed !== null) {
      // Endo's comment (2026-10-26): the render clamp for column-fixed
      // panes (the bottom shell today). A stored height larger than the
      // content box (a window shrank after the drag) anchors the
      // bottom pane so tall its TOP edge - the divider - climbs
      // offscreen: ungrabbable, the exact "i cant drag the bottom pane
      // anymore" report. max-height caps the RENDER at the chat's
      // floor, so the divider always sits inside view and the next
      // drag self-heals the stored value.
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

  // --- drag ---
  // dragging = divider index during a gesture; the cells are measured ONCE
  // per gesture and frozen (the old divider's startWidth rule: deltas
  // rebase from the rendered layout at gesture start, and every move event
  // recomputes the whole split from that frozen baseline). The raw->delta
  // reduction (cumulative-from-baseline) lives in dividerGesture.ts so its
  // contract is unit-tested - card t_38c0e960 found it silently sending
  // per-event increments (dead-looking dividers) after the snap-ladder
  // rework. `pending` is the squeeze push-back's re-measure flag: set when
  // api.drag reports a pushed pane, consumed (re-measure + base reset) on
  // the NEXT event with the layout already re-rendered (card t_f6d5a771).
  const dragging = ref<number | null>(null);
  let startPx: number[] | null = null;
  const pointerState: DividerPointerState = { base: null };
  let pending = false;
  const snappedIdx = ref<number | null>(null);

  // the divider's live numerals (v0.14.4 ruling 5): the controlled pane's
  // px + share of the content box, and the snap rung when the drag sits
  // inside one - the badge rides the divider while the gesture runs.
  import { resizeReadout, type ResizeReadout } from "../composables/dockghost";
  const readout = ref<ResizeReadout | null>(null);
  const readoutPos = ref<{ x: number; y: number } | null>(null);
  const readoutStyle = () =>
    readoutPos.value ? { left: readoutPos.value.x + "px", top: readoutPos.value.y + "px" } : {};

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
    pending = false;
    snappedIdx.value = null;
    readout.value = null;
    readoutPos.value = null;
  }

  function onDrag(dividerIndex: number, delta: number, raw: number) {
    if (dragging.value !== dividerIndex) {
      if (dragging.value !== null) return; // another divider's gesture owns this pointer
      dragging.value = dividerIndex;
      startPx = measure();
      pointerState.base = null; // fresh gesture: the reducer re-derives the base
    }
    if (!startPx) return;
    // the re-measure flag: the pushed pane freed px and the layout
    // re-rendered - the new baseline is the current geometry, deltas
    // resume from the just-seen raw (the reducer resets the base with it)
    const wasPending = pending;
    if (wasPending) startPx = measure();
    const cumulative = dividerDelta(pointerState, delta, raw, wasPending);
    pending = false;
    if (cumulative === null) return;
    // content box for the pure math: the split's client px minus the
    // gutters its dividers occupy = the sum of the measured cells
    const contentPx = startPx.reduce((a, p) => a + p, 0);
    const res = api.drag((props.node as DockSplit).id, dividerIndex, startPx, cumulative, contentPx);
    if (res.pushedPaneId) pending = true; // freed px becomes layout: re-measure next event
    if (res.collapsedPaneId) {
      // the controlled pane (and this divider with it) is gone - the
      // gesture ends; the menubar toggle reopens the hidden sidebar
      dropGesture();
      return;
    }
    // the controlled pane is left (fixed) or, when it flexes, the fixed
    // right one - mirror the pure fn's read to light the snap guide.
    // card t_67fc1905 (sign fixed t_be305778): the bottom shell anchors to
    // the viewport bottom, so drag UP grows it (-cumulative: the divider
    // is the shell's TOP edge); its ladder is +200 (panes.ts).
    const node = props.node as DockSplit;
    const li = dividerIndex - 1;
    const i = dividerIndex;
    const bottomCase =
      node.direction === "column" &&
      (node.children[i] as DockPaneNode).fixed !== null;
    const px = bottomCase
      ? startPx[i] - cumulative
      : (node.children[li] as DockPaneNode).fixed !== null
        ? startPx[li] + cumulative
        : (node.children[i] as DockPaneNode).fixed !== null
          ? startPx[i] - cumulative
          : startPx[li] + cumulative;
    const step = bottomCase ? BOTTOM_SNAP_STEP : SNAP_STEP;
    snappedIdx.value = snapCandidateAt(px, step) !== null ? dividerIndex : null;
    // v0.14.4 ruling 5: the LIVE snap readout - the same px the drag math
    // applied, as px + % of the content box, with the rung it snapped to
    readout.value = resizeReadout(px, contentPx, step);
    const dividers = [...(rootEl.value?.children ?? [])].filter((el) =>
      (el as HTMLElement).classList.contains("dock-divider"),
    ) as HTMLElement[];
    const divEl = dividers[dividerIndex - 1];
    if (divEl && rootEl.value) {
      const dr = divEl.getBoundingClientRect();
      const rr = rootEl.value.getBoundingClientRect();
      readoutPos.value = { x: dr.left - rr.left, y: dr.top - rr.top };
    }
  }

  function onEnd() {
    dropGesture();
  }
</script>