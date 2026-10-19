<template>
  <div
    class="dock-pane dock-cell"
    :class="[kindClass, { 'drop-hover': dropHover, pinned }]"
    :style="cellStyle"
    :data-pane-id="node.id"
    @dragover="onPaneDragOver"
    @dragleave="onPaneDragLeave"
    @drop="onPaneDrop">
    <!-- the pane's tab bar IS the header (35px, MenuBar contract).
         card t_4ae27e0d: pinned panes (sessions / inspector) host
         unclosable tabs - no X on any tab, no '+', no actions menu; main
         panes' tabs show the X on hover only. -->
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
            :title="tabTitle(t)"
            :draggable="tabClosable(t) ? 'true' : 'false'"
            @click="api.focus(node.id, i)"
            @dragstart="onTabDragStart($event, i)"
            @dragover="onTabbarDragOver"
            @drop="onTabbarDrop">
            <span class="dock-tab-title">{{ tabTitle(t) }}</span>
            <!-- the close X: closable tabs of MAIN panes only, and it
                 surfaces on hover (CSS); pinned sidebars never render one -->
            <button
              v-if="tabClosable(t)"
              class="dock-tab-close"
              :aria-label="'close ' + tabTitle(t) + ' tab'"
              title="close tab"
              @click.stop="api.closeTab(node.id, i)">
              ×
            </button>
          </div>
          <!-- insertion caret "after the last tab" rides the '+' button's
               left edge (absolute pseudo-element, no reflow) -->
          <button
            v-if="!pinned"
            class="dock-tab-new"
            :class="{ 'caret-before': dropIdx === node.tabs.length }"
            aria-label="new tab"
            title="new tab"
            @click.stop="newTabMenu = !newTabMenu">
            +
          </button>
        </div>
        <button
          v-if="!pinned"
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
          v-if="api.canRemovePane(node.id)"
          class="dock-tab-btn"
          aria-label="remove pane"
          title="remove pane"
          @click.stop="api.remove(node.id)">
          ×
        </button>
      </template>
    </div>

    <!-- the pane's active tab: rendered by app.vue via the forwarded slot
         named pane-<component>; the slot carries WHERE it renders (pane)
         and WHICH tab - so the chat outlet renders the tab's own session
         instance (multiple panes MAY host the same component - both
         attach, the events endpoint is multi-subscriber) -->
    <div
      v-if="outletName"
      class="dock-content">
      <slot
        :name="outletName"
        :pane="node.id"
        :tab="node.tabs[node.active]" />
    </div>

    <!-- empty pane: the '+' component menu IS the pane's content (VS Code
         empty editor-group watermark); the bar above carries the close-X
         (hidden when the remove would be refused - card t_4ae27e0d).
         Card 4: the dockable registry drives it - MAIN-area dockables
         only now: the pinned sidebars have no menu-open path. -->
    <div
      v-else
      class="dock-empty">
      <div
        class="dock-empty-actions"
        role="menu"
        aria-label="pane components">
        <button
          v-for="d in MAIN_DOCKABLES"
          :key="d.id"
          class="dock-menu-btn"
          role="menuitem"
          @click="openDockable(d)">
          <DockItemIcon :icon="d.icon" />
          {{ menuLabel(d) }}
        </button>
      </div>
    </div>

    <!-- '+' new-tab menu: card 4's dockable registry inventory with icons
         (replaces card 2's chat-only list). A singleton that already lives
         FOCUSES instead of opening a second instance. Main-area inventory
         only (card t_4ae27e0d). -->
    <div
      v-if="newTabMenu"
      class="dock-menu"
      role="menu"
      aria-label="new tab components"
      @click.stop>
      <button
        v-for="d in MAIN_DOCKABLES"
        :key="d.id"
        class="dock-menu-btn"
        role="menuitem"
        @click="openDockable(d)">
        <DockItemIcon :icon="d.icon" />
        {{ menuLabel(d) }}
      </button>
    </div>

    <!-- corner affordance: split / remove (never on a pinned pane) -->
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
        v-if="api.canRemovePane(node.id)"
        class="dock-menu-btn danger"
        role="menuitem"
        @click="api.remove(node.id)">
        remove pane
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, h, inject, onBeforeUnmount, onMounted, ref } from "vue";
  import {
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    type DockPane as DockPaneNode,
    type DockTab,
  } from "../composables/dock";
  import {
    COMPONENT_TITLES,
    MAIN_DOCKABLES,
    isClosableTab,
    isPinnedTab,
    type DockIcon,
    type DockableComponent,
    type DockComponentName,
  } from "../composables/dockables";
  // the re-export path keeps one import surface for engine-side consumers;
  // dockables.ts is the registry's true source (card t_30ee04fb)

  const props = defineProps<{
    node: DockPaneNode;
    cellStyle?: Record<string, string>;
  }>();

  const api = inject(DOCK_API_KEY)!;
  // card 3: the chat tab is multi-instance - opening a chat (the '+'
  // menu, the empty-pane menu's "new chat") mints a REAL tab through the
  // app's opener (this pane's id goes with it). Card 4's dockable
  // registry replaces the openers wholesale.
  const openChat = inject(DOCK_CHAT_OPEN_KEY, null);
  // card 3: chat tabs show their session's title ('new chat' unsaved);
  // other components keep the generic titles
  const tabTitleFor = inject(DOCK_TAB_TITLE_KEY, null);

  function tabTitle(tab: DockTab): string {
    return tabTitleFor?.(tab) ?? COMPONENT_TITLES[tab.component];
  }

  // card t_4ae27e0d: the pinned identity. A pane hosting any sidebar-
  // pinned tab (sessions / the inspector's five views) is fixed chrome -
  // no X, no '+', no actions, and no tab may drag into it.
  const pinned = computed(() => props.node.tabs.some((t) => isPinnedTab(t.component)));
  function tabClosable(t: DockTab): boolean {
    return isClosableTab(t.component);
  }

  const outletName = computed(() => {
    const c = props.node.tabs[props.node.active]?.component;
    return c ? ("pane-" + c as `pane-${DockComponentName}`) : null;
  });

  const kindClass = computed(
    () => "kind-" + (props.node.tabs[props.node.active]?.component ?? "empty"),
  );

  // menu labels: the registry's voice; chat's multi policy reads as a verb
  function menuLabel(d: DockableComponent): string {
    return d.policy === "multi" ? "new " + d.title : d.title;
  }
  // THE open path (card 4): the singleton policy decides - at most one
  // instance, so opening while it lives FOCUSES it wherever it sits; chat
  // (multi) always mints a fresh tab through the app's opener
  function openDockable(d: DockableComponent) {
    newTabMenu.value = false;
    if (d.policy === "multi") {
      if (openChat) openChat(props.node.id, null);
      else api.setComponent(props.node.id, d.id); // unreachable: app always provides the opener
      return;
    }
    api.openSingleton(props.node.id, d.id);
  }

  // the registry's icon renderer: 14px in the shell's 24-box stroke style
  const DockItemIcon = (p: { icon: DockIcon }) =>
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
    // card t_4ae27e0d: pinned tabs never leave their sidebar pane
    if (pinned.value) return;
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
    if (pinned.value) return; // a pinned pane is never a drop target
    if (!isTabDrag(e)) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dropHover.value = true;
    dropIdx.value = caretIndexFor(e);
  }

  // tab drops bubble here from the bar; the pane-level handler skips them
  function onTabbarDrop(e: DragEvent) {
    if (pinned.value) return; // engine refuses anyway; no highlight was shown
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
    if (pinned.value) return; // a pinned pane is never a drop target
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