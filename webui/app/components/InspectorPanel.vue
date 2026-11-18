<template>
  <aside
    class="aside pane inspector-panel"
    :class="{ open: open }">
    <!-- module-panels: the composition mount (see. -->
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
  import { modulePanels } from "../composables/module-panels.generated";

  // module = the dock-tab component id (cron / skills / tools).
  const props = defineProps<{
    module: string;
    open?: boolean;
  }>();

  const panel = computed(() =>
    modulePanels.find((p) => p.module === props.module),
  );
</script>