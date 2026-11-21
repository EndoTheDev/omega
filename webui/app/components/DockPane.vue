<template>
  <div
    ref="paneEl"
    class="dock-pane dock-cell"
    :class="[
      kindClass,
      {
        'drop-hover': dropHover,
        'drop-refused': refusedIdx !== null,
        pinned,
        'edge-bottom': node.edge === 'bottom',
        // Endo's comment: the fullscreen VIEW rides the pane class - bottompanel.css
        // owns the takeover; the sidebars never hide (Endo: they stay as-is, still
        // self-toggled)
        'bottom-fullscreen': node.edge === 'bottom' && api.bottomFullscreen.value,
      },
    ]"
    :style="cellStyle"
    :data-pane-id="node.id"
    @contextmenu="onPaneContextMenu"
    @dragover="onPaneDragOver"
    @dragleave="onPaneDragLeave"
    @drop="onPaneDrop">
    <!-- the pane's tab bar IS the header (35px, MenuBar contract). -->
    <div
      ref="tabsEl"
      class="dock-tabbar"
      v-if="!node.grown"
      @contextmenu="onStripContextMenu">
      <template v-if="node.tabs.length">
        <div
          class="dock-tabs"
          role="tablist"
          aria-label="pane tabs">
          <template
            v-for="(t, i) in node.tabs"
            :key="i">
          <div
            v-if="!tabHidden(t)"
            class="dock-tab"
            :class="{
              active: i === activeIndex,
              'caret-before': dropIdx === i,
              'drop-refused': refusedIdx === i,
              /*
               * sidebar strips (icon-only) get their own 28px centered chip shape - the
               * text-tab rules never move / /
               */
              'ic-only': iconTabs,
            }"
            :data-index="i"
            role="tab"
            :aria-selected="i === activeIndex"
            :aria-label="iconTabs ? tabTitle(t) : undefined"
            :title="tabTitle(t)"
            :draggable="true"
            @click="api.focus(node.id, i)"
            @contextmenu.prevent.stop="onTabContextMenu($event, i)"
            @dragstart="onTabDragStart($event, i)"
            @dragover="onTabbarDragOver"
            @drop="onTabbarDrop">
            <Icon
              v-if="tabIcon(t)"
              :name="tabIcon(t)"
              class="dock-tab-ic" />
            <span
              v-if="tabBadge(t)"
              class="dock-tab-badge"
              role="status"
              :aria-label="tabBadge(t) + ' unread'">{{ tabBadge(t) }}</span>
            <span
              v-if="!iconTabs"
              class="dock-tab-title">{{ tabTitle(t) }}</span>
            <button
              v-if="tabClosable(t)"
              class="dock-tab-close"
              :class="{ locked: lastTabLocked }"
              :aria-disabled="lastTabLocked || undefined"
              :tabindex="lastTabLocked ? -1 : undefined"
              :aria-label="'close ' + tabTitle(t) + ' tab'"
              :title="lastTabLocked ? 'the last tab cannot close' : 'close tab'"
              @click.stop="onTabClose(i)">
              ×
            </button>
          </div>
          </template>
          <button
            v-if="!pinned"
            class="dock-tab-new"
            :class="{ 'caret-before': dropIdx === node.tabs.length, 'door-dimmed': draftFocused }"
            aria-label="new session"
            :aria-disabled="draftFocused || undefined"
            :tabindex="draftFocused ? -1 : undefined"
            :title="draftFocused ? 'the draft conversation already has the focus' : 'new session'"
            @click.stop="newSession()">
              +
            </button>
        </div>
        <button
          v-if="node.edge === 'bottom'"
          class="dock-tab-btn"
          :aria-label="api.bottomFullscreen.value ? 'exit logs fullscreen' : 'logs fullscreen'"
          :title="api.bottomFullscreen.value ? 'exit fullscreen' : 'fullscreen'"
          @click.stop="api.toggleBottomFullscreen()">
          ⛶
        </button>
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

    <!-- a GROWN pinned root renders its local split tree in place of its body -->
    <DockSplitNode
      v-if="node.grown"
      :node="node.grown"
      :cell-style="{ width: '100%', height: '100%' }">
      <template
        v-for="(_, name) in $slots"
        #[name]="slotProps">
        <slot
          :name="name"
          v-bind="slotProps ?? {}" />
      </template>
    </DockSplitNode>

    <!-- the pane's active tab: the slot app.vue forwards by outlet name -->
    <div
      v-if="outletName"
      class="dock-content">
      <slot
        :name="outletName"
        :pane="node.id"
        :tab="activeTab" />
    </div>

    <!--
      an empty pane's component menu is gone on purpose: the canvas class-law + menu work
      owns adding views; until then a drop (or a menubar/palette opener) refills the pane.
    -->
    <div
      v-else
      class="dock-empty" />

    <!-- corner affordance: split / remove (never on a pinned pane) -->
    <div
      v-if="menuOpen"
      class="dock-menu"
      role="menu"
      aria-label="pane actions"
      @click.stop>
      <button
        v-if="canSplit('row')"
        class="dock-menu-btn"
        role="menuitem"
        @click="api.split(node.id, 'row', 'after')">
        split right
      </button>
      <button
        v-if="canSplit('column')"
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
    <div
      v-if="dropZone"
      class="dock-drop-overlay"
      :data-zone="dropZone">
      <div
        class="dock-drop-zone"
        :class="{ refused: predicted?.refused }"
        :style="zoneStyle">
        <!-- the ghost: the exact result a release lands (direction, target) -->
        <DockGhost
          v-if="predicted"
          :preview="predicted"
          :drag="dragGhost" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, defineAsyncComponent, inject, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    DOCK_DRAFT_FOCUS_KEY,
    // the LOCKED LAST TAB's one condition.
    lastTabLocked as lastTabLockedIn,
    type DockPane as DockPaneNode,
    type DockTab,
  } from "../composables/dock";
  import {
    COMPONENT_TITLES,
    dockTabMenuItems,
    dockableIcon,
    dockableOf,
    hiddenTabMenuItems,
    isClosableTab,
    type DockComponentName,
  } from "../composables/dockables";
  import { openContextMenu } from "../composables/contextMenu";
  import { endGesture, onGestureEnd, tabDrag, zoneForPointer, zoneRectFractions, type DragZone, type ZoneRect } from "../composables/dockdnd";
  import { splitFitsZone, paneMenuItems, type SplitDirection } from "../composables/panes";
  import { ghostPreview, ghostRegistry, type GhostPreview } from "../composables/dockghost";
  import DockGhost from "./DockGhost.vue";
  import Icon from "./ui/Icon.vue";
  import { glyphKindFor, glyphIcon } from "../../../harness/files/ui/rowicon";
  import { notificationsBadgeText } from "./shell/notificationFeed";
  const DockSplitNode = defineAsyncComponent(() => import("./DockSplit.vue"));

  const props = defineProps<{
    node: DockPaneNode;
    cellStyle?: Record<string, string>;
  }>();

  const api = inject(DOCK_API_KEY)!;
  const openChat = inject(DOCK_CHAT_OPEN_KEY, null);
  const draftFocus = inject(DOCK_DRAFT_FOCUS_KEY, null);
  const draftFocused = computed(() => draftFocus?.value === true);
  const tabTitleFor = inject(DOCK_TAB_TITLE_KEY, null);

  function tabTitle(tab: DockTab): string {
    return tabTitleFor?.(tab) ?? COMPONENT_TITLES[tab.component];
  }

  // the pinned identity is the EDGE MARKER; the SIDEBAR ZONE carries the same identity, so
  // a pane inside a sidebar's stack reads as a sidebar pane too - one strip shape, no split
  // actions.
  const paneZone = computed(() => api.zoneOf(props.node.id));
  const inSidebarZone = computed(() => {
    const z = paneZone.value;
    return z === "left" || z === "right";
  });
  const pinned = computed(() => props.node.edge !== undefined || inSidebarZone.value);

  // THE HIDDEN CHIP: hiding is a visibility flag, never a close - the tab keeps its seat,
  // its ref and its pane. The strip renders only visible chips, and the pane body follows
  // the ACTIVE chip when it is visible, else the first that is, so hiding never blanks a pane.
  const hiddenTabs = computed(() => api.hiddenTabs.value);
  function tabHidden(tab: DockTab): boolean {
    return hiddenTabs.value.includes(tab.component);
  }
  const visibleCount = computed(() => props.node.tabs.filter((t) => !tabHidden(t)).length);
  const activeIndex = computed(() => {
    const active = props.node.tabs[props.node.active];
    if (active && !tabHidden(active)) return props.node.active;
    const first = props.node.tabs.findIndex((t) => !tabHidden(t));
    return first >= 0 ? first : props.node.active;
  });
  const activeTab = computed(() => props.node.tabs[activeIndex.value]);
  // THE SHAPE LAW: which split actions this pane may OFFER. A sidebar stacks, so its panes
  // offer no side-by-side split at all - the entry point is gone, never a greyed-out button.
  function canSplit(direction: SplitDirection): boolean {
    const z = paneZone.value;
    return z !== null && splitFitsZone(z, direction);
  }
  // the X renders on EVERY closable-class tab: the last-main-pane law protects the PANE
  // (removePane), never the tabs.
  function tabClosable(t: DockTab): boolean {
    return isClosableTab(t.component);
  }
  // the SAME predicate closeTab's guard reads, so the dimmed surface cannot drift from it
  const lastTabLocked = computed(() => lastTabLockedIn(props.node));
  function onTabClose(index: number) {
    if (lastTabLocked.value) return;
    api.closeTab(props.node.id, index);
  }

  // The right-click SELECTS the tab it menus, so the verbs land on the tab the user pointed
  // at. The item closures capture that index, never the active tab. Hide is the secondary
  // sidebar's own axis: the strip keeps one chip, and that chip's right-click restores the rest.
  function onTabContextMenu(e: MouseEvent, index: number) {
    api.focus(props.node.id, index);
    const secondary = paneZone.value === "right";
    void openContextMenu(
      e.clientX,
      e.clientY,
      dockTabMenuItems(props.node, index, {
        close: () => api.closeTab(props.node.id, index),
        closeOthers: () => closeOtherTabs(index),
        ...(secondary
          ? {
              hide: () => api.setTabHidden(props.node.tabs[index]!.component, true),
              visibleCount: visibleCount.value,
            }
          : {}),
      }),
    );
  }

  // THE PANE AREA: the pane's own chrome (its strip's empty band, an empty pane's body)
  // menus the verbs the pane's actions popup already carries. Content keeps the browser's
  // native menu (chat text, previews, links - the chrome line), a tab chip opens its own,
  // and a pinned pane (a sidebar, the bottom shell) has no close and no split action at
  // all, so it registers nothing.
  function onPaneContextMenu(e: MouseEvent) {
    if (e.defaultPrevented) return; // an inner chrome surface (the terminal) already claimed it
    if (pinned.value) return;
    const el = e.target as HTMLElement | null;
    if (el?.closest?.(".dock-content, .dock-tab, .dock-menu")) return;
    const rows = paneMenuItems({
      closePane: api.canRemovePane(props.node.id) ? () => api.remove(props.node.id) : null,
      splitRight: canSplit("row") ? () => api.split(props.node.id, "row", "after") : null,
      splitDown: canSplit("column") ? () => api.split(props.node.id, "column", "after") : null,
    });
    if (rows.length === 0) return;
    if (openContextMenu(e.clientX, e.clientY, rows)) e.preventDefault();
  }

  // THE SIDEBAR TITLE BAR: a sidebar's strip is the surface that stays visible when its
  // chips go, so it holds the restore list. Nothing hidden = no menu (the browser's own).
  function onStripContextMenu(e: MouseEvent) {
    if (paneZone.value !== "right") return;
    const rows = hiddenTabMenuItems(
      props.node,
      api.hiddenTabs.value,
      (component) => COMPONENT_TITLES[component],
      (component) => api.setTabHidden(component, false),
    );
    if (rows.length === 0) return;
    e.preventDefault();
    void openContextMenu(e.clientX, e.clientY, rows);
  }

  // Descending, so every index not yet visited is still the one the store holds: closeTab
  // refuses an uncloseable tab and the locked last tab, so the pane never empties.
  function closeOtherTabs(index: number) {
    const count = props.node.tabs.length;
    for (let i = count - 1; i >= 0; i -= 1) {
      if (i === index) continue;
      api.closeTab(props.node.id, i);
    }
  }

  const outletName = computed(() => {
    const c = activeTab.value?.component;
    // an EMPTY pane (a fresh split group, a shell drained by a drag) has no outlet.
    if (!c) return null;
    return "pane-" + c as `pane-${DockComponentName}`;
  });

  const kindClass = computed(
    () => "kind-" + (activeTab.value?.component ?? "empty"),
  );

  function newSession() {
    if (openChat) openChat(props.node.id, null);
    else api.setComponent(props.node.id, "chat"); // unreachable: app always provides the opener
  }

  // the sidebar shells (left/right edges) render their tab strips as ICON-ONLY chips.
  const iconTabs = computed(() => inSidebarZone.value);
  function tabIcon(tab: DockTab): string | null {
    // the viewer's chip icon follows the CONTENT type.
    if (tab.component === "viewer" && tab.viewer) {
      if (tab.viewer.kind === "dir") return "folder";
      return tab.viewer.kind === "skill"
        ? "skills"
        : glyphIcon(glyphKindFor(tab.viewer.name));
    }
    return dockableIcon(tab.component);
  }

  // the tab chip's unread badge TEXT
  function tabBadge(tab: DockTab): string {
    return tab.component === "notifications" ? notificationsBadgeText.value : "";
  }

  // tab DnD: EVERY tab bar is a drop target.
  const TAB_MIME = "application/x-omega-dock-tab";

  interface DragPayload {
    p: string; // pane id
    t: number; // tab index in the source pane
    c?: string; // component id (drop-time re-check belt for the lock)
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
    // EVERY tab drags now - sessions too (it may reorder inside its own bar).
    const tab = props.node.tabs[index]!;
    e.dataTransfer?.setData(
      TAB_MIME,
      JSON.stringify({ p: props.node.id, t: index, c: tab.component }),
    );
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
    // identity for the LIVE hit-test (data is protected until drop):
    tabDrag.start({ paneId: props.node.id, index, component: tab.component });
    // v0.14.4 the ghost gesture begins.
    ghostRegistry.begin(props.node.id, index);
  }

  // one caret per pane: the index BEFORE which to insert.
  const dropIdx = ref<number | null>(null);
  const refusedIdx = ref<number | null>(null);
  const dropHover = ref(false);
  // the docking prediction - the landing zone a dragged tab would drop.
  const dropZone = ref<DragZone | null>(null);
  // v0.14.4 the GHOST preview.
  const predicted = ref<GhostPreview | null>(null);
  const dragGhost = computed(() => {
    if (!dropZone.value) return null;
    const d = tabDrag.current();
    return d ? dockableOf(d.component) : null;
  });
  const zoneStyle = computed(() => {
    const f = zoneRectFractions(dropZone.value ?? "center");
    return {
      left: f.x * 100 + "%",
      top: f.y * 100 + "%",
      width: f.w * 100 + "%",
      height: f.h * 100 + "%",
    };
  });
  // the zone math runs on the pane BODY (under the tab bar) - VS Code's read.
  function bodyRect(): ZoneRect {
    const pane = paneEl.value;
    if (!pane) return { x: 0, y: 0, w: 0, h: 0 };
    const pr = pane.getBoundingClientRect();
    const bar = pane.querySelector(".dock-tabbar");
    const top = bar ? bar.getBoundingClientRect().bottom : pr.top;
    return { x: pr.left, y: top, w: pr.width, h: Math.max(0, pr.bottom - top) };
  }
  const menuOpen = ref(false); // the pane-actions (split / remove) menu
  const tabsEl = ref<HTMLElement | null>(null);
  const paneEl = ref<HTMLElement | null>(null); // the pane root (zone-math measurements)

  // active-tab reachability: the strip scrolls at narrow widths, so a selection can land
  // outside the visible band. Scroll just enough to bring the chip fully into view and never
  // touch the vertical axis.
  function scrollActiveIntoView(): void {
    const bar = tabsEl.value;
    if (!bar) return;
    const strip = bar.querySelector<HTMLElement>(".dock-tabs");
    if (!strip || strip.scrollWidth <= strip.clientWidth) return;
    const el = strip.querySelector<HTMLElement>(".dock-tab.active");
    if (!el) return;
    const sr = strip.getBoundingClientRect();
    const r = el.getBoundingClientRect();
    if (r.left < sr.left) strip.scrollLeft += r.left - sr.left;
    else if (r.right > sr.right) strip.scrollLeft += r.right - sr.right;
  }
  watch(
    () => [props.node.active, props.node.tabs.map((t) => t.component).join("\u0000")],
    () => scrollActiveIntoView(),
    { flush: "post" },
  );
  // tabs carry a readable floor, so a narrow pane SCROLLS; scrolling never changes the
  // strip's box, so this observer cannot re-enter itself.
  let stripObserver: ResizeObserver | null = null;
  onMounted(() => {
    scrollActiveIntoView();
    const strip = tabsEl.value?.querySelector<HTMLElement>(".dock-tabs");
    if (strip && typeof ResizeObserver !== "undefined") {
      stripObserver = new ResizeObserver(() => scrollActiveIntoView());
      stripObserver.observe(strip);
    }
  });

  // the LIVE hit-test: is the current gesture allowed to drop into THIS pane? THE ZONE LAW
  // answers for every tab - the store's seatOk reads the class law over this pane's zone.
  function dropAllowed(): boolean {
    const d = tabDrag.current();
    if (!d) return false;
    return api.seatOk(d.component, props.node.id);
  }

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

  // toIndex contract (moveTab).
  function postRemoval(idx: number, fromIndex: number): number {
    return fromIndex < idx ? idx - 1 : idx;
  }

  function refuse(e: DragEvent): void {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "none";
    refusedIdx.value = caretIndexFor(e);
    dropHover.value = false;
    dropIdx.value = null;
    dropZone.value = null;
    predicted.value = null;
  }

  function onTabbarDragOver(e: DragEvent) {
    if (!isTabDrag(e)) return;
    e.stopPropagation(); // a grown root must not re-handle its leaf's bar
    if (!dropAllowed()) return refuse(e); // locked drag, wrong shell
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dropHover.value = true;
    refusedIdx.value = null;
    dropZone.value = null; // the bar's band: the caret predicts, zones clear
    predicted.value = null;
    dropIdx.value = caretIndexFor(e);
  }

  function onTabbarDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    const d = payloadOf(e);
    if (d && dropAllowed()) {
      const idx = dropIdx.value ?? caretIndexFor(e);
      const adj = d.p === props.node.id ? postRemoval(idx, d.t) : idx;
      api.moveTab(d.p, d.t, props.node.id, adj);
    }
    clearDrop();
  }

  function onPaneDragOver(e: DragEvent) {
    if (!isTabDrag(e)) return;
    e.stopPropagation(); // a grown root must not re-handle its leaf's body
    if (!dropAllowed()) return refuse(e); // locked drag, wrong shell
    e.preventDefault(); // a drop anywhere in the pane lands
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    // the TAB BAR band rides through (the per-tab handlers own it).
    const el = e.target as HTMLElement | null;
    if (el?.closest?.(".dock-tabbar")) {
      dropHover.value = true;
      refusedIdx.value = null;
      dropZone.value = null;
      predicted.value = null;
      return;
    }
    const d = tabDrag.current();
    // the split zones self-drop; THE SHAPE LAW bounds the read by the pane's ZONE - a
    // sidebar has no left/right half at all, so that split cannot even be entered.
    const zone = paneZone.value;
    if (zone === null) return;
    const rawZone = zoneForPointer(zone, e.clientX, e.clientY, bodyRect());
    const selfRaw = d && d.paneId === props.node.id ? rawZone : null;
    if (d && d.paneId === props.node.id && selfRaw === "center") {
      dropHover.value = true;
      refusedIdx.value = null;
      dropZone.value = null;
      predicted.value = null;
      return;
    }
    // VS Code zone read on the pane body: halves + center.
    const raw = selfRaw ?? rawZone;
    dropHover.value = false; // the overlay carries the body affordance
    refusedIdx.value = null;
    dropZone.value = raw;
    // the GHOST reads the commit's own formula over the registry's frozen tree
    const drag = tabDrag.current();
    const rect = bodyRect();
    ghostRegistry.record(props.node.id, rect);
    const axisPx =
      raw === "left" || raw === "right" ? rect.w
      : raw === "top" || raw === "bottom" ? rect.h
      : null;
    predicted.value = ghostPreview(api.tree.value, drag!.paneId, drag!.index, props.node.id, raw, axisPx);
  }

  function onPaneDragLeave(e: DragEvent) {
    if (e.target !== e.currentTarget) return; // bubbling leaves ignore
    if (e.relatedTarget && (e.currentTarget as Node).contains(e.relatedTarget as Node)) return;
    // a leave clears ONLY this pane's local hover state: a mid-gesture pane exit must not
    // kill the global gesture record the next pane's dragover still reads.
    dropHover.value = false;
    dropIdx.value = null;
    refusedIdx.value = null;
    dropZone.value = null;
    predicted.value = null;
  }

  // body drop: same pane + a SPLIT zone = the pane splits around it. The pure op refuses its
  // illegal landings as no-ops, so a release never tears the layout.
  function onPaneDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation(); // the leaf owns the drop; a grown root re-handles nothing
    const d = payloadOf(e);
    const zone = dropZone.value;
    if (d && dropAllowed()) {
      const samePane = d.p === props.node.id;
      if (zone && (!samePane || zone !== "center")) {
        api.dockDropToZone(d.p, d.t, props.node.id, zone);
      } else if (samePane) {
        api.focus(props.node.id, d.t);
      } else {
        api.moveTab(d.p, d.t, props.node.id, props.node.tabs.length);
      }
    }
    clearDrop();
  }

  function clearDrop() {
    // the global record and every pane's local highlights clear through the COMPOSABLE's
    // endGesture, never through instance-owned state.
    endGesture();
  }
  // dragend/drop/Esc/blur cleanup is composable-owned; this pane holds only its local
  // highlight state, so an instance dying mid-gesture cannot orphan the record.
  const clearLocal = () => {
    dropHover.value = false;
    dropIdx.value = null;
    refusedIdx.value = null;
    dropZone.value = null;
    predicted.value = null;
  };
  const unbindGesture = onGestureEnd(clearLocal);
  onMounted(() => {
    document.addEventListener("click", onDocClick);
  });
  function onDocClick() {
    menuOpen.value = false;
  }
  onBeforeUnmount(() => {
    stripObserver?.disconnect();
    stripObserver = null;
    document.removeEventListener("click", onDocClick);
    unbindGesture();
  });
</script>