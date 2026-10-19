<template>
  <div
    class="dock-pane dock-cell"
    :class="kindClass"
    :style="cellStyle">
    <!-- the pane's active tab: rendered by app.vue via the forwarded slot
         named pane-<component> (multiple panes MAY host the same component -
         both attach, the events endpoint is multi-subscriber) -->
    <div
      v-if="outletName"
      class="dock-content">
      <slot :name="outletName" />
    </div>

    <!-- empty pane: the '+' component menu IS the pane (VS Code empty
         editor-group watermark) -->
    <div
      v-else
      class="dock-empty">
      <div
        class="dock-empty-actions"
        role="menu"
        aria-label="pane components">
        <button
          v-for="c in DOCK_COMPONENTS"
          :key="c"
          class="dock-menu-btn"
          role="menuitem"
          @click="api.setComponent(node.id, c)">
          {{ menuLabel(c) }}
        </button>
      </div>
    </div>

    <!-- corner affordance: split / remove (empty panes are their own menu) -->
    <button
      v-if="outletName"
      class="dock-plus"
      title="pane actions"
      @click.stop="menuOpen = !menuOpen">
      +
    </button>
    <div
      v-if="menuOpen"
      class="dock-menu"
      role="menu"
      aria-label="pane actions"
      @click.stop>
      <button
        class="dock-menu-btn"
        role="menuitem"
        @click="api.split(node.id, 'row', 'after')">
        split right
      </button>
      <button
        class="dock-menu-btn"
        role="menuitem"
        @click="api.split(node.id, 'column', 'after')">
        split down
      </button>
      <button
        class="dock-menu-btn danger"
        role="menuitem"
        @click="api.remove(node.id)">
        remove pane
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, inject, onBeforeUnmount, onMounted, ref } from "vue";
  import {
    DOCK_API_KEY,
    DOCK_COMPONENTS,
    type DockPane as DockPaneNode,
    type DockComponentName,
  } from "../composables/dock";

  const props = defineProps<{
    node: DockPaneNode;
    cellStyle?: Record<string, string>;
  }>();

  const api = inject(DOCK_API_KEY)!;

  const outletName = computed(() => {
    const c = props.node.tabs[props.node.active]?.component;
    return c ? ("pane-" + c as `pane-${DockComponentName}`) : null;
  });

  const kindClass = computed(
    () => "kind-" + (props.node.tabs[props.node.active]?.component ?? "empty"),
  );

  const menuLabels: Record<DockComponentName, string> = {
    sidebar: "sidebar",
    chat: "new chat",
    inspector: "inspector",
  };
  function menuLabel(c: DockComponentName): string {
    return menuLabels[c];
  }

  // menu closes on any outside click; component open state only
  const menuOpen = ref(false);
  function onDocClick() {
    menuOpen.value = false;
  }
  onMounted(() => document.addEventListener("click", onDocClick));
  onBeforeUnmount(() => document.removeEventListener("click", onDocClick));
</script>