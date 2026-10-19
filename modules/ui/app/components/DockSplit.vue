<template>
  <div
    ref="rootEl"
    class="dock-split"
    :class="[node.direction, cellStyle ? 'dock-cell' : 'dock-root']"
    :style="cellStyle">
    <template
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
        @drag="onDrag(idx + 1, $event)"
        @end="onEnd" />
    </template>
  </div>
</template>

<script setup lang="ts">
  // The recursive layout renderer (card t_63253264): walks the dock tree,
  // rendering splits (rows/columns of cells sized by the tree's fixed/sizes
  // data) and panes. Named slots flow down so app.vue's region templates
  // surface inside whichever pane hosts their component. Resize: the
  // divider reports raw pointer deltas; THIS component measures the
  // rendered cells at gesture start and hands everything to the store,
  // whose pure math owns floors (128 drag / 320 chat) and the snap
  // attractor (320/640).
  import { inject, ref } from "vue";
  import { snapCandidate } from "../composables/panes";
  import {
    DOCK_API_KEY,
    type DockNode,
    type DockPane as DockPaneNode,
    type DockSplit,
  } from "../composables/dock";
  import DockPane from "./DockPane.vue";
  import DockDivider from "./DockDivider.vue";

  const props = defineProps<{
    node: DockSplit;
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
    const axis = props.node.direction === "row" ? "width" : "height";
    if (child.kind === "pane" && child.fixed !== null)
      return { [axis]: child.fixed + "px", flex: "0 0 auto" };
    const n = props.node.children.length;
    const totalFlex = props.node.children.reduce(
      (a, c, k) =>
        (c.kind !== "pane" || c.fixed === null) ? a + (props.node.sizes[k] ?? 1 / n) : a,
      0,
    );
    const grow = (props.node.sizes[idx] ?? 1 / n) / Math.max(totalFlex, 0.000001);
    return { [axis]: "auto", flex: `${grow} 1 0` };
  }

  // --- drag ---
  // dragging = divider index during a gesture; the cells are measured ONCE
  // per gesture and frozen (the old divider's startWidth rule: deltas
  // rebase from the rendered layout at gesture start, and every move event
  // recomputes the whole split from that frozen baseline)
  const dragging = ref<number | null>(null);
  let startPx: number[] | null = null;
  const snappedIdx = ref<number | null>(null);

  function measure(): number[] {
    const cells = [...(rootEl.value?.children ?? [])].filter((el) =>
      (el as HTMLElement).classList.contains("dock-cell"),
    ) as HTMLElement[];
    const axis = props.node.direction === "row" ? "offsetWidth" : "offsetHeight";
    return props.node.children.map((_, i) => cells[i]?.[axis] ?? 0);
  }

  function onDrag(dividerIndex: number, delta: number) {
    if (dragging.value === null) {
      dragging.value = dividerIndex;
      startPx = measure();
    }
    if (!startPx) return;
    // content box for the pure math: the split's client px minus the
    // gutters its dividers occupy = the sum of the measured cells
    const contentPx = startPx.reduce((a, p) => a + p, 0);
    api.drag(props.node.id, dividerIndex, startPx, delta, contentPx);
    // the controlled pane is left (fixed) or, when it flexes, the fixed
    // right one - mirror the pure fn's read to light the snap guide
    const li = dividerIndex - 1;
    const i = dividerIndex;
    const px =
      (props.node.children[li] as DockPaneNode).fixed !== null
        ? startPx[li] + delta
        : (props.node.children[i] as DockPaneNode).fixed !== null
          ? startPx[i] - delta
          : startPx[li] + delta;
    snappedIdx.value = snapCandidate(px) !== null ? dividerIndex : null;
  }

  function onEnd() {
    dragging.value = null;
    startPx = null;
    snappedIdx.value = null;
  }
</script>