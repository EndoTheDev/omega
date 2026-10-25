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
        // card t_67fc1905: the bottom shell gets its own class (the
        // bottom-panel css file owns its look + the mobile zero-collapse)
        'edge-bottom': node.edge === 'bottom',
      },
    ]"
    :style="cellStyle"
    :data-pane-id="node.id"
    @dragover="onPaneDragOver"
    @dragleave="onPaneDragLeave"
    @drop="onPaneDrop">
    <!-- the pane's tab bar IS the header (35px, MenuBar contract).
         card t_d6b657a7: edge shells (sessions / inspector homes) are
         unclosable as panes - no remove X, no actions menu - but host
         FREE tabs: every tab drags (sessions only inside "left"), chat's
         X works wherever it drops. card t_d7fd3ad3: the '+' is MAIN-pane
         chrome - the pinned shells (sidebars + bottom) carry no '+'. -->
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
            :class="{
              active: i === node.active,
              'caret-before': dropIdx === i,
              'drop-refused': refusedIdx === i,
            }"
            :data-index="i"
            role="tab"
            :aria-selected="i === node.active"
            :title="tabTitle(t)"
            draggable="true"
            @click="api.focus(node.id, i)"
            @dragstart="onTabDragStart($event, i)"
            @dragover="onTabbarDragOver"
            @drop="onTabbarDrop">
            <span class="dock-tab-title">{{ tabTitle(t) }}</span>
            <!-- the close X: closable components only (chat today); it
                 surfaces on hover (CSS) wherever the tab lives - pane
                 identity no longer decides tab chrome (t_d6b657a7) -->
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
        <!-- card t_d7fd3ad3 (items 2+5): the '+' is MAIN-pane chrome only -
             the pinned shells (both sidebars AND the bottom panel) are
             closed tab bars: no '+', no menu. The main pane keeps its
             load-bearing '+' (the dockable inventory menu). -->
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
         empty editor-group watermark); the bar above carries the remove
         X (hidden when the remove would be refused - edge shells). The
         MENU inventory (t_d6b657a7) is the refill source for ANY pane,
         pinned or main: the empty shell renders this same '+' panel. -->
    <div
      v-else
      class="dock-empty">
      <div
        class="dock-empty-actions"
        role="menu"
        aria-label="pane components">
        <button
          v-for="d in menuDockables"
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
         FOCUSES instead of opening a second instance. t_d6b657a7: the menu
         rides the MENU inventory - the refill source for any pane. The
         template iterated the never-defined `mainDockables` (empty menu,
         no new tab could open); menuDockables is the pinned inventory. -->
    <div
      v-if="newTabMenu"
      class="dock-menu"
      role="menu"
      aria-label="new tab components"
      @click.stop>
      <button
        v-for="d in menuDockables"
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
    <!-- docking prediction overlay (card t_0e833eae): a tab dragged over
         ANOTHER pane's body lights its landing zone - the four half-zones
         split the target around the dragged tab, center groups it - with
         a ghost of the dragged tab where it lands. pointer-events: none
         (css) keeps the gesture flowing to the pane underneath; the tab
         BAR keeps the caret affordance instead (zones are body-only). -->
    <div
      v-if="dropZone"
      class="dock-drop-overlay"
      :data-zone="dropZone">
      <div
        class="dock-drop-zone"
        :style="zoneStyle">
        <span
          v-if="dragGhost"
          class="dock-drop-ghost">
          <DockItemIcon :icon="dragGhost.icon" />
          {{ dragGhost.title }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, h, inject, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    type DockPane as DockPaneNode,
    type DockTab,
  } from "../composables/dock";
  import {
    COMPONENT_TITLES,
    MENU_DOCKABLES,
    dockableOf,
    isClosableTab,
    type DockIcon,
    type DockableComponent,
    type DockComponentName,
  } from "../composables/dockables";
  import { tabDrag, dropAllowedFor, zoneForPoint, zoneRectFractions, splitAllowedFor, type DragZone, type ZoneRect } from "../composables/dockdnd";
  // the re-export path keeps one import surface for engine-side consumers;
  // dockables.ts is the registry's true source (card t_30ee04fb)

  // MENU_DOCKABLES is the refill inventory (card t_d6b657a7): any pane,
  // edge shell or main, is refilled from it - sessions is excluded (the
  // loader owns it) and settings has no menu entry by ruling. The menus
  // need the full registry entries (title/icon/policy).
  const menuDockables = MENU_DOCKABLES.map(dockableOf);

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

  // card t_d6b657a7: the pinned identity is the EDGE MARKER. A shell pane
  // (left = sessions home, right = inspector home) is fixed chrome as a
  // whole - no remove X, no actions menu - but its TABS are free: any tab
  // may live here or leave (drop into / out of this bar freely), the only
  // lock is sessions leaving "left".
  const pinned = computed(() => props.node.edge !== undefined);
  function tabClosable(t: DockTab): boolean {
    // Endo ruling 2026-10-25: the LAST main pane's tab renders no close
    // X - the button appears only while another main pane lives. Pinned
    // panes are never the last main pane, so a chat tab parked in a
    // shell keeps its X (t_d6b657a7) whatever the main census says.
    return isClosableTab(t.component) && !api.isLastMainPane(props.node.id);
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

  // --- tab DnD (card 2, generalized t_d6b657a7): EVERY tab bar is a drop
  // target - reorder within a bar, cross-pane moves, sidebars <-> main
  // pane, bidirectional. The ONLY refusals: a LOCKED sessions drag may
  // not leave the left shell's own bar (the hit-test refuses, the chip
  // under the cursor renders the no-drop affordance). Drop targets only
  // highlight (outline) and the caret is an absolute bar - no reflow
  // during the gesture (verify (d)).
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
    // EVERY tab drags now - sessions too (it may reorder inside its own
    // bar); the hit-test refuses its drops elsewhere, not the gesture
    const tab = props.node.tabs[index]!;
    e.dataTransfer?.setData(
      TAB_MIME,
      JSON.stringify({ p: props.node.id, t: index, c: tab.component }),
    );
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
    // identity for the LIVE hit-test (data is protected until drop):
    tabDrag.start({ paneId: props.node.id, index, component: tab.component });
  }

  // one caret per pane: the index BEFORE which to insert (tabs.length = after
  // the last tab). dragover recomputes it live; drop consumes it.
  const dropIdx = ref<number | null>(null);
  // the refused-drag affordance: while a locked drag hovers THIS bar (e.g.
  // sessions over the chat pane), the chip under the cursor lights refuse
  const refusedIdx = ref<number | null>(null);
  const dropHover = ref(false);
  // card t_0e833eae: the docking prediction - the landing zone a dragged
  // tab would drop into on THIS pane's body (null = no body hover, or the
  // same-pane/stale case where no docking preview shows)
  const dropZone = ref<DragZone | null>(null);
  // the ghost chip's registry entry: the dragged tab's component, so the
  // preview shows WHAT will land (title + icon) before release
  const dragGhost = computed(() => {
    if (!dropZone.value) return null;
    const d = tabDrag.current();
    return d ? dockableOf(d.component) : null;
  });
  // the zone rectangle as body fractions (pure dockdnd math) - percent
  // positioning, no px bookkeeping in the component
  const zoneStyle = computed(() => {
    const f = zoneRectFractions(dropZone.value ?? "center");
    return {
      left: f.x * 100 + "%",
      top: f.y * 100 + "%",
      width: f.w * 100 + "%",
      height: f.h * 100 + "%",
    };
  });
  // the zone math runs on the pane BODY (under the tab bar) - VS Code's
  // read: the tab strip owns the caret band, the body owns the zones
  function bodyRect(): ZoneRect {
    const pane = paneEl.value;
    if (!pane) return { x: 0, y: 0, w: 0, h: 0 };
    const pr = pane.getBoundingClientRect();
    const bar = pane.querySelector(".dock-tabbar");
    const top = bar ? bar.getBoundingClientRect().bottom : pr.top;
    return { x: pr.left, y: top, w: pr.width, h: Math.max(0, pr.bottom - top) };
  }
  const newTabMenu = ref(false);
  const menuOpen = ref(false); // the pane-actions (split / remove) menu
  const tabsEl = ref<HTMLElement | null>(null);
  const paneEl = ref<HTMLElement | null>(null); // the pane root (zone-math measurements)

  // --- active-tab reachability (card t_aca53fee): the tab strip scrolls
  // at narrow pane widths (css overflow-x on .dock-tabs), so a selection
  // can land on a tab sitting outside the visible band - the ACTIVE tab
  // must never clip or strand off-viewport. Keep it honest: whenever the
  // active tab (or the tab list) changes, scroll the strip just enough
  // to bring the chip fully into view - never scrollLeft further than
  // the fix needs (no centering churn), and never touch the vertical
  // axis. flush:'post' runs after the DOM reflects the new active tab.
  function scrollActiveIntoView(): void {
    const bar = tabsEl.value;
    if (!bar) return;
    // the scroll box is the strip (.dock-tabs), not the bar hosting it -
    // the bar is full-width chrome, the strip is what clips + scrolls
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
  onMounted(() => {
    // a remount restores a persisted tree whose active tab may open out
    // of view - measure once the first layout settles
    scrollActiveIntoView();
  });

  // the LIVE hit-test: is the current gesture allowed to drop into THIS pane?
  // (the window dragend/drop cleanup clears the gesture record)
  function dropAllowed(): boolean {
    return dropAllowedFor(props.node.edge ?? null, tabDrag.current());
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

  // toIndex contract (moveTab): the insert position AFTER the tab left its
  // source pane - a same-pane caret right of the drag source shifts back one
  function postRemoval(idx: number, fromIndex: number): number {
    return fromIndex < idx ? idx - 1 : idx;
  }

  function refuse(e: DragEvent): void {
    // no highlight, no caret: the browser's no-drop cursor plus the
    // chip-level refused outline carry the affordance
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "none";
    refusedIdx.value = caretIndexFor(e);
    dropHover.value = false;
    dropIdx.value = null;
    dropZone.value = null;
  }

  function onTabbarDragOver(e: DragEvent) {
    if (!isTabDrag(e)) return;
    if (!dropAllowed()) return refuse(e); // locked drag, wrong shell
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    dropHover.value = true;
    refusedIdx.value = null;
    dropZone.value = null; // the bar's band: the caret predicts, zones clear
    dropIdx.value = caretIndexFor(e);
  }

  // tab drops bubble here from the bar; the pane-level handler skips them
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
    if (!dropAllowed()) return refuse(e); // locked drag, wrong shell
    e.preventDefault(); // a drop anywhere in the pane lands
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    // the TAB BAR band rides through (the per-tab handlers own it): the
    // caret predicts there, zones stay body-only (card t_0e833eae)
    const el = e.target as HTMLElement | null;
    if (el?.closest?.(".dock-tabbar")) {
      dropHover.value = true;
      refusedIdx.value = null;
      dropZone.value = null;
      return;
    }
    const d = tabDrag.current();
    // zones are CROSS-PANE: a body hover of the pane's own tab keeps the
    // classic focus-on-drop read (no docking preview of itself)
    if (d && d.paneId === props.node.id) {
      dropHover.value = true;
      refusedIdx.value = null;
      dropZone.value = null;
      return;
    }
    // VS Code zone read on the pane body: halves + center. Edge shells
    // never split, so their zones fold to center (group into the bar).
    const raw = zoneForPoint(e.clientX, e.clientY, bodyRect());
    dropHover.value = false; // the overlay carries the body affordance
    refusedIdx.value = null;
    dropZone.value = splitAllowedFor(props.node.edge) ? raw : "center";
  }

  function onPaneDragLeave(e: DragEvent) {
    if (e.target !== e.currentTarget) return; // bubbling leaves ignore
    if (e.relatedTarget && (e.currentTarget as Node).contains(e.relatedTarget as Node)) return;
    clearDrop();
  }

  // body drop (VS Code): same pane = focus only; another pane: the lit
  // zone decides - split the target around the dragged tab (halves, the
  // new half-pane seats the tab) or group it as a plain tab (center).
  // The pure op refuses its illegal landings as no-ops (shells don't
  // split, zones never self-dock), so release never tears the layout.
  function onPaneDrop(e: DragEvent) {
    e.preventDefault();
    const d = payloadOf(e);
    const zone = dropZone.value;
    if (d && dropAllowed()) {
      if (zone && d.p !== props.node.id) {
        api.dockDropToZone(d.p, d.t, props.node.id, zone);
      } else if (d.p === props.node.id) {
        api.focus(props.node.id, d.t);
      } else {
        api.moveTab(d.p, d.t, props.node.id, props.node.tabs.length);
      }
    }
    clearDrop();
  }

  function clearDrop() {
    dropHover.value = false;
    dropIdx.value = null;
    refusedIdx.value = null;
    dropZone.value = null;
    tabDrag.end();
  }
  // any drag ending anywhere clears the local highlight (dropped elsewhere,
  // Esc, or a lost pointer); drop on THIS pane already cleared explicitly
  function onDragEnd() {
    clearDrop();
  }
  // keyboard escape cancels the drag cleanly: the browser fires dragend on
  // Esc for native DnD, and the keydown here backs it (headless/OS variance)
  // so prediction state never outlives the gesture - layout untouched.
  function onEscKey(e: KeyboardEvent) {
    if (e.key === "Escape" && tabDrag.current()) clearDrop();
  }
  onMounted(() => {
    document.addEventListener("click", onDocClick);
    window.addEventListener("dragend", onDragEnd);
    window.addEventListener("drop", onDragEnd);
    window.addEventListener("keydown", onEscKey);
  });
  function onDocClick() {
    menuOpen.value = false;
    newTabMenu.value = false;
  }
  onBeforeUnmount(() => {
    document.removeEventListener("click", onDocClick);
    window.removeEventListener("dragend", onDragEnd);
    window.removeEventListener("drop", onDragEnd);
    window.removeEventListener("keydown", onEscKey);
  });
</script>