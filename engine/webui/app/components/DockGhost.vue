<template>
  <!-- the ghost preview (v0.14.4 milestone 5): the landing's direction,
       target and exact resulting sizes live inside the lit drop zone; a
       refusal renders the whole zone hatched-red with its reason. -->
  <span
    class="dock-ghost"
    :data-refused="preview.refused ?? undefined"
    :data-direction="preview.direction ?? 'center'">
    <template v-if="preview.refused">
      <span class="dock-ghost-refused">{{ preview.refused }}</span>
    </template>
    <template v-else>
      <span
        v-if="drag"
        class="dock-drop-ghost">
        <DockItemIcon :icon="drag.icon" />
        {{ drag.title }}
      </span>
      <span class="dock-ghost-meta">
        <span v-if="preview.direction" class="dock-ghost-direction">
          {{ directionLabel }}
        </span>
        <span
          v-if="preview.halves"
          class="dock-ghost-halves">
          {{ halvesLabel }}
        </span>
      </span>
    </template>
  </span>
</template>

<script setup lang="ts">
  // the ONE preview formula (ruling 5): ghostPreview in dockghost.ts runs
  // predictDockDrop - the commit's own call - so what the overlay draws is
  // exactly what a release lands, refusals included.
  import { computed, h, type FunctionalComponent } from "vue";
  import type { DockIcon } from "../composables/dockables";
  import type { GhostPreview } from "../composables/dockghost";

  const props = defineProps<{
    preview: GhostPreview;
    /** the dragged tab's registry entry (title + icon), null = none */
    drag: { icon: DockIcon; title: string } | null;
  }>();

  const directionLabel = computed(() => {
    switch (props.preview.zone) {
      case "left": return "split left";
      case "right": return "split right";
      case "top": return "split above";
      case "bottom": return "split below";
      default: return "move here";
    }
  });

  const halvesLabel = computed(() => {
    const h = props.preview.halves;
    if (!h) return "";
    return h.map((x) => `${x.px}px (${Math.round(x.pct * 100)}%)`).join(" | ");
  });

  // the registry's 14px icon renderer (DockPane owns the same shape)
  const DockItemIcon: FunctionalComponent<{ icon: DockIcon }> = (p) =>
    h(
      "svg",
      {
        class: "dock-item-icon",
        width: "14",
        height: "14",
        viewBox: "0 0 24 24",
        "aria-hidden": "true",
      },
      [
        h("g", {
          fill: "none",
          stroke: "currentColor",
          "stroke-width": "2",
          "stroke-linecap": "round",
          "stroke-linejoin": "round",
        }, [
          ...(p.icon.rect
            ? [h("rect", {
                x: p.icon.rect.x,
                y: p.icon.rect.y,
                width: p.icon.rect.width,
                height: p.icon.rect.height,
                rx: p.icon.rect.rx ?? 2,
              })]
            : []),
          ...p.icon.paths.map((d) => h("path", { d })),
        ]),
      ],
    );
</script>