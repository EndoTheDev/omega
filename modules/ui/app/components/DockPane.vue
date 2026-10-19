<template>
  <div
    class="dock-pane dock-cell"
    :class="[kindClass, { 'drop-hover': dropHover }]"
    :style="cellStyle"
    @dragover="onPaneDragOver"
    @dragleave="onPaneDragLeave"
    @drop="onPaneDrop">
    <!-- the pane's tab bar IS the header (35px, MenuBar contract) -->
    <div
      ref="tabsEl"
      class="dock-tabbar">
      <template v-if="node.tabs.length">
        <div
          class="dock-tabs"
          role="tablist"
          aria-label="pane tabs">
          <div
            v-for="(t, i) in node.tabs"
            :key="i"
            class="dock-tab"
            :class="{ active: i === node.active, 'caret-before': dropIdx === i }"
            :data-index="i"
            role="tab"
            :aria-selected="i === node.active"
            :title="tabTitle(t.component)"
            draggable="true"
            @click="api.focus(node.id, i)"
            @dragstart="onTabDragStart($event, i)"
            @dragover="onTabbarDragOver"
            @drop="onTabbarDrop">
            <span class="dock-tab-title">{{ tabTitle(t.component) }}</span>
            <button
              class="dock-tab-close"
              :aria-label="'close ' + tabTitle(t.component) + ' tab'"
              title="close tab"
              @click.stop="api.closeTab(node.id, i)">
              ×
            </button>
          </div>
          <!-- insertion caret "after the last tab" rides the '+' button's
               left edge (absolute pseudo-element, no reflow) -->
          <button
            class="dock-tab-new"
            :class="{ 'caret-before': dropIdx === node.tabs.length }"
            aria-label="new tab"
            title="new tab"
            @click.stop="newTabMenu = !newTabMenu">
            +
          </button>
        </div>
        <button
          class="dock-tab-btn actions"
          aria-label="pane actions"
          title="pane actions"
          @click.stop="menuOpen = !menuOpen">
          ⋯
        </button>
      </template>
      <template v-else>
        <span class="dock-tabs empty" />
        <button
          class="dock-tab-btn"
          aria-label="remove pane"
          title="remove pane"
          @click.stop="api.remove(node.id)">
          ×
        </button>
      </template>
    </div>

    <!-- the pane's active tab: rendered by app.vue via the forwarded slot
         named pane-<component> (multiple panes MAY host the same component -
         both attach, the events endpoint is multi-subscriber) -->
    <div
      v-if="outletName"
      class="dock-content">
      <slot :name="outletName" />
    </div>

    <!-- empty pane: the '+' component menu IS the pane's content (VS Code
         empty editor-group watermark); the bar above carries the close-X -->
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

    <!-- '+' new-tab menu: the card-2 inventory is chat only; card 4's
         dockable registry replaces this list -->
    <div
      v-if="newTabMenu"
      class="dock-menu"
      role="menu"
      aria-label="new tab components"
      @click.stop>
      <button
        v-for="c in PLUS_TAB_COMPONENTS"
        :key="c"
        class="dock-menu-btn"
        role="menuitem"
        @click="onNewTab(c)">
        new {{ COMPONENT_TITLES[c] }}
      </button>
    </div>

    <!-- corner affordance: split / remove -->
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
    COMPONENT_TITLES,
    PLUS_TAB_COMPONENTS,
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

  function tabTitle(c: DockComponentName): string {
    return COMPONENT_TITLES[c];
  }
  const menuLabels: Record<DockComponentName, string> = {
    sidebar: "sidebar",
    chat: "new chat",
    inspector: "inspector",
  };
  function menuLabel(c: DockComponentName): string {
    return menuLabels[c];
  }

  function onNewTab(c: DockComponentName) {
    newTabMenu.value = false;
    api.openTab(props.node.id, { component: c });
  }

  // --- tab DnD (card 2): within-pane reorder + between-pane moves. Drop
  // targets only highlight (outline) and the caret is an absolute bar - no
  // reflow during the gesture (verify (d)). Edge-drops that split a pane
  // on a border are card 3's; v1 drops onto a pane / tab bar only.
  const TAB_MIME = "application/x-omega-dock-tab";

  interface DragPayload {
    p: string; // pane id
    t: number; // tab index in the source pane
  }
  function payloadOf(e: DragEvent): DragPayload | null {
    const raw = e.dataTransfer?.getData(TAB_MIME);
    if (!raw) return null;
    try {
      const d = JSON.parse(raw) as DragPayload;
      return typeof d.p === "string" && Number.isInteger(d.t) ? d : null;
    } catch {
      return null;
    }
  }
  // dragover can READ types (data itself is protected until drop)
  function isTabDrag(e: DragEvent): boolean {
    return e.dataTransfer?.types.includes(TAB_MIME) ?? false;
  }

  function onTabDragStart(e: DragEvent, index: number) {
    e.dataTransfer?.setData(TAB_MIME, JSON.stringify({ p: props.node.id, t: index }));
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
  }

  // one caret per pane: the index BEFORE which to insert (tabs.length = after
  // the last tab). dragover recomputes it live; drop consumes it.
  const dropIdx = ref<number | null>(null);
  const dropHover = ref(false);
  const newTabMenu = ref(false);
  const menuOpen = ref(false); // the pane-actions (split / remove) menu
  const tabsEl = ref<HTMLElement | null>(null);

  function caretIndexFor(e: DragEvent): number {
    const tabs = tabsEl.value?.querySelectorAll<HTMLElement>(".dock-tab") ?? [];
    for (const el of tabs) {
      const r = el.getBoundingClientRect();
      if (e.clientX >= r.left && e.clientX <= r.right) {
        const i = Number.parseInt(el.dataset.index ?? "-1", 10);
        return e.clientX < r.left + r.width / 2 ? i : i + 1;
      }
    }
    return tabs.length; // over the bar but between/beyond tabs
  }

  // toIndex contract (moveTab): the insert position AFTER the tab left its
  // source pane - a same-pane caret right of the drag source shifts back one
  function postRemoval(idx: number, fromIndex: number): number {
    return fromIndex < idx ? idx - 1 : idx;
  }

  function onTabbarDragOver(e: DragEvent) {
    if (!isTabDrag(e)) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dropHover.value = true;
    dropIdx.value = caretIndexFor(e);
  }

  // tab drops bubble here from the bar; the pane-level handler skips them
  function onTabbarDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    const d = payloadOf(e);
    if (d) {
      const idx = dropIdx.value ?? caretIndexFor(e);
      const adj = d.p === props.node.id ? postRemoval(idx, d.t) : idx;
      api.moveTab(d.p, d.t, props.node.id, adj);
    }
    clearDrop();
  }

  function onPaneDragOver(e: DragEvent) {
    if (!isTabDrag(e)) return;
    e.preventDefault(); // a drop anywhere in the pane lands
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dropHover.value = true;
  }

  function onPaneDragLeave(e: DragEvent) {
    if (e.target !== e.currentTarget) return; // bubbling leaves ignore
    if (e.relatedTarget && (e.currentTarget as Node).contains(e.relatedTarget as Node)) return;
    clearDrop();
  }

  // body drop (VS Code): same pane = focus only; another pane = move to end
  function onPaneDrop(e: DragEvent) {
    e.preventDefault();
    const d = payloadOf(e);
    if (d) {
      if (d.p === props.node.id) api.focus(props.node.id, d.t);
      else api.moveTab(d.p, d.t, props.node.id, props.node.tabs.length);
    }
    clearDrop();
  }

  function clearDrop() {
    dropHover.value = false;
    dropIdx.value = null;
  }
  // any drag ending anywhere clears the local highlight (dropped elsewhere,
  // Esc, or a lost pointer); drop on THIS pane already cleared explicitly
  function onDragEnd() {
    clearDrop();
  }
  onMounted(() => {
    document.addEventListener("click", onDocClick);
    window.addEventListener("dragend", onDragEnd);
    window.addEventListener("drop", onDragEnd);
  });
  function onDocClick() {
    menuOpen.value = false;
    newTabMenu.value = false;
  }
  onBeforeUnmount(() => {
    document.removeEventListener("click", onDocClick);
    window.removeEventListener("dragend", onDragEnd);
    window.removeEventListener("drop", onDragEnd);
  });
</script>