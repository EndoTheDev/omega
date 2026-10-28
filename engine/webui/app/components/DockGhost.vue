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
        <Icon
          :name="drag.icon"
          class="dock-item-icon" />
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
  import { computed } from "vue";
  // card t_502b8e81: the registry icon is a NAME in the webui iconset,
  // rendered through the one wrapper (DockPane's DockItemIcon is gone)
  import Icon from "./ui/Icon.vue";
  import type { GhostPreview } from "../composables/dockghost";

  const props = defineProps<{
    preview: GhostPreview;
    /** the dragged tab's registry entry (title + icon name), null = none */
    drag: { icon: string; title: string } | null;
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
</script>