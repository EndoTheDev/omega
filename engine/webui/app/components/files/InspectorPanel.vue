<template>
  <aside
    class="aside pane inspector-panel"
    :class="{ open: open }">
    <!-- module-panels: the composition mount (see
         composables/module-panels.generated.ts). card t_4ae27e0d turned
         the inspector's chip strip into REAL dock tabs - the dock tabbar
         hosts the tab, this component only hosts the panel body for its
         module. The dock unmounts the outlet when a sibling tab
         activates, so lazy semantics (fetch/poll only while up) carry
         over from the strip era. -->
    <div
      v-if="panel"
      class="module-panels">
      <component
        :is="panel.component"
        :key="module" />
    </div>
    <p
      v-else
      class="panel-missing">
      module not loaded
    </p>
  </aside>
</template>

<script setup lang="ts">
  import { computed } from "vue";
  import { modulePanels } from "../../composables/module-panels.generated";

  // module = the dock-tab component id (cron / http_channel / skills /
  // tools): the composition's module name IS the tab id, no alias map
  const props = defineProps<{
    module: string;
    open?: boolean;
  }>();

  const panel = computed(() =>
    modulePanels.find((p) => p.module === props.module),
  );
</script>