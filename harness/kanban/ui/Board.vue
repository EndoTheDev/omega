<template>
  <section
    ref="boardEl"
    class="board"
    :class="{ narrow: narrow }">
    <!-- card t_308e6d9b: the UNIVERSAL band (the shared TabBand) - the
         board switcher + count + remove ride the band now (the law: the
         tab's actions live in the band, not floating over the body).
         The board scroll columns start BELOW the band - scrolling never
         moves it. -->
    <TabBand title="Kanban">
      <template
        v-if="boards.length > 1 || boardName">
        <span class="board-switch-label">board</span>
        <!-- card t_acad02ec (audit M1): the switcher rides the themed
             listbox (BandSelect) - a native select popup is OS chrome
             over the pane -->
        <BandSelect
          class="board-switch-select"
          :model-value="boardName"
          :options="boardOptions"
          label="board"
          drop="down"
          @update:model-value="switchBoard($event)" />
        <span
          v-if="boardName"
          class="board-switch-count">{{ tasks.length }} card<template v-if="tasks.length !== 1">s</template></span>
        <button
          id="board-remove-btn"
          class="band-tool board-remove-btn"
          :class="{ armed: confirmingRemove }"
          :disabled="!canRemoveBoard(boardName)"
          :title="confirmingRemove ? 'click again to remove this board' : 'remove this board'"
          @click="removeBoardClick">
          {{ confirmingRemove ? "confirm remove" : "remove" }}
        </button>
      </template>
    </TabBand>
    <p
      v-if="notice"
      class="board-notice"
      :class="{ err: noticeErr }"
      role="status">
      {{ notice }}
    </p>
    <div
      class="board-scroll"
      :key="boardName">
      <div
        v-for="(col, i) in cols"
        :key="BOARD_STATUSES[i]"
        class="board-col"
        :data-status="BOARD_STATUSES[i]"
        :data-count="col.length"
        :class="{ folded: isLaneFolded(BOARD_STATUSES[i], col.length) }"
        :style="{ '--lane-rail-width': `${LANE_RAIL_PX}px`, '--lane-ease': `${LANE_EASE_MS}ms` }">
        <!-- card t_50a8c145: ANY lane folds to a rail. The rail is BOTH a
             button (click unfolds) and a live drop target - the drag
             machinery resolves .board-col, so dropping on a rail needs no
             drop code of its own (the card lands at the top of the lane).
             The dot row sits at the header row's height, so dots line up
             across the board whether a lane is folded or not. The rail
             carries the same + the header does: an empty lane is a rail
             forever (its click cannot unfold it), so the create form is
             the ONLY way to grow a lane from nothing. -->
        <div
          v-if="isLaneFolded(BOARD_STATUSES[i], col.length)"
          class="lane-rail-wrap">
          <button
            class="lane-rail"
            :aria-expanded="false"
            :aria-label="`unfold ${BOARD_STATUSES[i]} lane`"
            :title="col.length
              ? `unfold ${BOARD_STATUSES[i]} lane`
              : `${BOARD_STATUSES[i]} is empty - an empty lane folds itself`"
            @click="toggleLane(BOARD_STATUSES[i], col.length)">
            <span class="lane-dot-row">
              <span
                class="lane-dot"
                :style="{ background: laneTone(BOARD_STATUSES[i]) }" />
            </span>
            <span class="lane-rail-name">{{ BOARD_STATUSES[i] }}</span>
            <span
              v-if="col.length"
              class="lane-rail-count">{{ col.length }}</span>
          </button>
          <button
            class="icon-btn col-create-btn rail-create-btn"
            :title="`new card in ${BOARD_STATUSES[i]}`"
            :aria-label="`new card in ${BOARD_STATUSES[i]}`"
            @click.stop="toggleCreate(BOARD_STATUSES[i])">
            +
          </button>
        </div>

        <template v-else>
          <header class="col-head">
            <!-- every lane folds: the dot + name are the fold control. In
                 the narrow stack there are no rails at all, so the control
                 is honestly disabled there rather than a dead click. -->
            <button
              class="col-fold-btn"
              :aria-expanded="true"
              :disabled="narrow"
              :title="narrow
                ? 'lane folding is a horizontal-scroll view affordance'
                : `fold ${BOARD_STATUSES[i]} lane`"
              @click="toggleLane(BOARD_STATUSES[i], col.length)">
              <span
                class="lane-dot"
                :style="{ background: laneTone(BOARD_STATUSES[i]) }" />
              <span class="col-name">{{ BOARD_STATUSES[i] }}</span>
            </button>
            <span class="col-count">{{ col.length }}</span>
            <!-- round t_f81e845e: every column grows cards - the + opens
                 the full create form (title, body, assignee, priority);
                 the column the + sits in is the preset status -->
            <button
              class="icon-btn col-create-btn"
              :title="`new card in ${BOARD_STATUSES[i]}`"
              :aria-label="`new card in ${BOARD_STATUSES[i]}`"
              @click.stop="toggleCreate(BOARD_STATUSES[i])">
              +
            </button>
          </header>

          <!-- the full create form (round t_f81e845e): open under exactly
               one column at a time -->
          <form
            v-if="createOpen === BOARD_STATUSES[i]"
            class="col-create"
            @submit.prevent="submitCreate"
            @keydown.esc="closeCreate">
            <input
              v-model="createTitle"
              class="col-create-title"
              placeholder="title"
              aria-label="new card title" />
            <textarea
              v-model="createBody"
              class="col-create-body"
              placeholder="body (optional)"
              aria-label="new card body" />
            <input
              v-model="createAssignee"
              class="col-create-assignee"
              placeholder="assignee (optional)"
              aria-label="new card assignee" />
            <BandSelect
              class="col-create-prio"
              :model-value="createPriority"
              :options="priorityOptions()"
              label="priority (higher = more urgent)"
              @update:model-value="createPriority = $event" />
            <div class="col-create-actions">
              <button
                class="col-add-btn"
                type="submit"
                :disabled="!createTitle.trim()">add</button>
              <button
                class="col-create-cancel"
                type="button"
                @click="closeCreate">cancel</button>
            </div>
          </form>

          <div class="col-body">
            <article
              v-for="t in col"
              :key="t.id"
              class="card"
              :data-id="t.id"
              :class="{ dragging: isDragging(t.id) }"
              @click="onCardClick(t)"
              @pointerdown="onCardPointerDown($event, t)">
              <div class="card-title-row">
                <span
                  v-if="hasPriorityChip(t.priority)"
                  class="card-prio"
                  :class="`prio-${Math.min(t.priority, 3)}`">{{ priorityLabel(t.priority) }}</span>
                <div class="card-title">{{ t.title }}</div>
              </div>
              <div
                v-if="bodyPreview(t.body)"
                class="card-preview">{{ bodyPreview(t.body) }}</div>
              <div class="card-meta">
                <span
                  v-if="t.assignee"
                  class="card-assignee">@{{ t.assignee }}</span>
                <span class="card-age">{{ age(t.created_at) }}</span>
                <span class="card-moves">
                  <button
                    v-if="step(BOARD_STATUSES[i], -1)"
                    class="icon-btn"
                    :title="`move to ${step(BOARD_STATUSES[i], -1)}`"
                    @click.stop="moveBoard(t, step(BOARD_STATUSES[i], -1) as string)">
                    ◀
                  </button>
                  <button
                    v-if="step(BOARD_STATUSES[i], +1)"
                    class="icon-btn"
                    :title="`move to ${step(BOARD_STATUSES[i], +1)}`"
                    @click.stop="moveBoard(t, step(BOARD_STATUSES[i], +1) as string)">
                    ▶
                  </button>
                  <button
                    v-if="t.session_id"
                    class="icon-btn card-link"
                    title="open the linked conversation"
                    @click.stop="$emit('open-session', t.session_id)">
                    ↗
                  </button>
                </span>
              </div>
            </article>
            <p
              v-if="col.length === 0"
              class="col-empty">
              empty
            </p>
          </div>
        </template>
      </div>
    </div>

    <DetailDrawer
      v-if="selected"
      :task="selected"
      :comments="detailComments"
      :events="detailEvents"
      @move="moveSelected"
      @save="onDrawerSave"
      @comment="submitComment"
      @delete="onDrawerDelete"
      @open-session="(id: string) => $emit('open-session', id)"
      @close="selected = null" />
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from "vue";
  import DetailDrawer from "./board/DetailDrawer.vue";
  import {
    BOARD_STATUSES,
    DEFAULT_BOARD_NAME,
    LANE_EASE_MS,
    LANE_RAIL_PX,
    age,
    boardDeletePath,
    bodyPreview,
    canRemoveBoard,
    columns,
    fetchBoardsRetry,
    hasPriorityChip,
    isNarrowWidth,
    laneFolded,
    laneTone,
    pickBootBoard,
    priorityLabel,
    priorityOptions,
    reconcileLanePins,
    reorderAfter,
    step,
    taskDeletePath,
    toggleLanePin,
    withBoard,
    type BoardComment,
    type BoardEvent,
    type BoardInfo,
    type BoardTask,
    type LanePins,
  } from "./board/boardLogic";

  // The module-owned board view: 9 hermes columns in one horizontal
  // scroll, self-contained API wiring (fetch /tasks) so it rides the
  // desktop / VS Code builds alone, like the tools canary did.
  defineEmits<{
    (e: "open-session", id: string): void;
  }>();

  const tasks = ref<BoardTask[]>([]);
  const error = ref("");

  // --- board switcher (card t_d7a7beaa): the dropdown rides GET
  // /boards; "" means "server default" and renders no switcher when
  // the capability is missing. The inspector (drawer) reads the same
  // board - every call below goes through withBoard.
  const boards = ref<BoardInfo[]>([]);
  const boardName = ref("");

  // the switcher's listbox options (BandSelect): one per board registry
  // row, the name as both value and face
  const boardOptions = computed(() =>
    boards.value.map((b) => ({ value: b.name, label: b.name })),
  );

  async function fetchBoards() {
    // The mounted fetch runs ONCE, so a miss under suite load left no
    // switcher chip for the whole page life (nightlies 42268953671 and
    // 42260775149, cards t_7cdac186 / t_82e5d149). The bounded retry
    // lives in boardLogic (pure, stubbed-fetch test); a final miss
    // keeps the old behavior - silent, the board still renders.
    const fetched = await fetchBoardsRetry(fetch);
    if (!fetched) return; // absent capability or every attempt missed
    boards.value = fetched;
    boardName.value = pickBootBoard(boards.value);
  }

  function switchBoard(name: string) {
    if (name === boardName.value) return;
    disarmRemove(); // an armed confirm points at the old board: reset it
    boardName.value = name;
    selected.value = null; // the drawer's card belongs to the old board
    fetchBoard();
  }

  // --- board removal (card t_95c5527f): destructive, so the confirm is
  // an armed second click on the same button (inline relabel, matching
  // the module toggle arm/apply pattern - no native confirm dialog). The
  // server is the law: DeleteBoard refuses the default board, the
  // current pointer target and boards still holding tasks, and the
  // refusal text surfaces verbatim in the notice line.
  const confirmingRemove = ref(false);
  const notice = ref("");
  const noticeErr = ref(false);
  let disarmTimer: ReturnType<typeof setTimeout> | undefined;

  function setNotice(text: string, isErr = false) {
    notice.value = text;
    noticeErr.value = isErr;
  }

  function disarmRemove() {
    confirmingRemove.value = false;
    if (disarmTimer) {
      clearTimeout(disarmTimer);
      disarmTimer = undefined;
    }
  }

  function removeBoardClick() {
    if (!canRemoveBoard(boardName.value)) return;
    if (!confirmingRemove.value) {
      confirmingRemove.value = true;
      // disarm on timeout: the second click lands only while the
      // warning still shows
      if (disarmTimer) clearTimeout(disarmTimer);
      disarmTimer = setTimeout(disarmRemove, 6000);
      return;
    }
    disarmRemove();
    void deleteBoard();
  }

  async function deleteBoard() {
    const name = boardName.value;
    if (!canRemoveBoard(name)) return;
    try {
      const res = await fetch(boardDeletePath(name), { method: "DELETE" });
      if (!res.ok) {
        // refusal text (default board, pointer target, tasks present)
        // comes back as plain text - the handler wrote it for this line
        const detail = await res.text().catch(() => `DELETE /boards: ${res.status}`);
        setNotice(detail, true);
        return;
      }
      setNotice(`board "${name}" removed`);
      selected.value = null; // the drawer's card belonged to the gone board
      // refresh the registry first; the removed row must leave the
      // switcher even if the boot fetch reads stale
      try {
        const res = await fetch("/boards");
        if (res.ok) {
          const body = await res.json();
          boards.value = (body.boards ?? []).filter((b: BoardInfo) => b.name !== name);
        }
      } catch {
        boards.value = boards.value.filter((b) => b.name !== name);
      }
      boardName.value = DEFAULT_BOARD_NAME; // selection falls back to the default board
      await fetchBoard();
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // --- notification board select (card t_556ee727) ---
  // The shell's task-notification click focuses this tab and wants the
  // task's board picked. The seam is a window event + a pending stash
  // (module UI imports no shell code: no prop, no shell import) - the
  // shell publishes both (composables/notifications.ts). The stash
  // covers the mount race: the tab focus mounts the board component on
  // the tick AFTER the shell published, so the mount consumes the stash
  // once; the event covers the already-mounted case (the consumer
  // clears the stash it supersedes).
  const BOARD_SELECT_EVENT = "omega-select-board";
  function consumeBoardStash(): string | undefined {
    const w = window as unknown as { __omegaBoardSelect?: string };
    const stashed = w.__omegaBoardSelect;
    delete w.__omegaBoardSelect;
    return stashed;
  }
  function applyBoardSelect(board: string | undefined | null) {
    if (typeof board === "string" && board) switchBoard(board);
  }
  function onBoardSelect(ev: Event) {
    consumeBoardStash(); // a live event supersedes any in-flight intent
    applyBoardSelect((ev as CustomEvent<{ board?: string }>).detail?.board);
  }
  onMounted(() => {
    window.addEventListener(BOARD_SELECT_EVENT, onBoardSelect);
    applyBoardSelect(consumeBoardStash());
  });
  onUnmounted(() => {
    window.removeEventListener(BOARD_SELECT_EVENT, onBoardSelect);
  });

  // --- live refresh (card t_b5784de3): the 5s poll is DEAD --------------
  // The board re-reads its own REST view when the shell announces a canvas
  // delta. The tasks panel is NOT enough to render this grid (rows carry no
  // body, no priority, no position, and cap at 20 - see panel.go), so the
  // event is a pulse: it says "task state moved", the board answers with
  // the same GET its mount fetch runs. Nothing here repeats on a cadence.
  const CANVAS_DELTA_EVENT = "omega-canvas-delta";
  // The same event also carries a RESET pulse (id "reconnect"): the shell
  // fires it from its reconnect catch-up, because a delta that landed
  // while this page's socket was down is never replayed - without the
  // pulse the board would stay stale until some later mutation happened
  // to fire. The literal is duplicated from composables/canvas.ts
  // (CANVAS_RESET_ID) on purpose: module UI imports no shell code.
  const CANVAS_RESET_ID = "reconnect";
  interface CanvasDeltaPanel {
    id?: string;
  }
  // A burst of deltas coalesces into ONE list read: a sibling agent
  // touching N tasks must not fan N full-list GETs. The first event
  // queues the read, the rest of the burst ride it.
  const DELTA_COALESCE_MS = 120;
  let deltaTimer: ReturnType<typeof setTimeout> | null = null;
  function scheduleBoardRefetch() {
    if (deltaTimer !== null) return; // a read is already queued
    deltaTimer = setTimeout(() => {
      deltaTimer = null;
      void fetchBoard();
    }, DELTA_COALESCE_MS);
  }
  function onCanvasDelta(ev: Event) {
    const detail = (ev as CustomEvent<CanvasDeltaPanel>).detail;
    if (!detail || typeof detail !== "object") return;
    if (detail.id === CANVAS_RESET_ID) {
      // a reconnect: the registry AND the list may have moved while the
      // socket was down, and no delta survives it - re-read the pane.
      void reloadAll();
      return;
    }
    if (detail.id === "tasks") {
      scheduleBoardRefetch();
      return;
    }
    if (detail.id === "boards") void refreshRegistry();
  }

  // reloadAll is the reset path: the registry first (the selected board
  // may have vanished), then the list - fetchBoard never runs on a stale
  // boardName.
  async function reloadAll() {
    await refreshRegistry();
    await fetchBoard();
  }

  // a boards-panel delta: the registry grew or shrank elsewhere. Re-read
  // the list only - a live selection outranks the pointer target, so the
  // board name stays unless the selected board is the one that vanished.
  async function refreshRegistry() {
    try {
      const res = await fetch("/boards");
      if (!res.ok) return;
      const body = await res.json();
      boards.value = body.boards ?? [];
      if (boardName.value && !boards.value.some((b) => b.name === boardName.value)) {
        boardName.value = pickBootBoard(boards.value);
        selected.value = null;
        await fetchBoard();
      }
    } catch {
      // the next delta retries - the registry GET is the catch-up
    }
  }

  // the list's request epoch: an event-triggered refetch and a mutation's
  // own refetch can overlap, and the older answer must never overwrite the
  // newer one.
  let fetchSeq = 0;

  async function fetchBoard() {
    if (drag.value && drag.value.moved) return; // a live drag owns the list
    const seq = ++fetchSeq;
    try {
      // status=all: the board grid carries the archived lane too (no
      // status is filtered out of the grid), so it can never default to
      // the active-only view the list tool shows (t_02a8beea)
      const res = await fetch(withBoard("/tasks?status=all", boardName.value));
      if (!res.ok) throw new Error(`GET /tasks: ${res.status}`);
      const body = await res.json();
      if (seq !== fetchSeq) return; // a newer read owns the list
      tasks.value = body.tasks ?? [];
      error.value = "";
      // keep the open drawer honest: if the drawn task is gone or moved,
      // refresh the drawer's view of it
      if (selected.value) applyDetailPatch(selected.value.id);
    } catch (e) {
      if (seq !== fetchSeq) return;
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  const cols = computed(() => columns(tasks.value));

  // --- narrow adaptation (card t_49d0b308): the inspector column is
  // ~300px (card ruling); the full-width column strip only works wide,
  // so the rendered width flips the `narrow` class - the columns stack
  // into a read-mostly vertical list instead of scrolling sideways.
  // The threshold is unit-covered in boardLogic.ts; this is the only
  // DOM-measuring part. Same mechanism as the shell's
  // composables/useNarrow (kept in-module: module UI imports no shell
  // code).
  const boardEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !boardEl.value) return;
    // (same rAF deferral as the shell's useNarrow: the flip changes the
    // observed layout itself, so the callback must not mutate this frame)
    resizeObs = new ResizeObserver((entries) => {
      const pending = entries
        .map((e) => Math.round(e.contentRect.width))
        .filter((w) => w > 0);
      if (pending.length === 0) return;
      requestAnimationFrame(() => {
        if (resizeObs === null) return;
        for (const w of pending) narrow.value = isNarrowWidth(w);
      });
    });
    resizeObs.observe(boardEl.value);
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });

  // --- lane rails (card t_50a8c145): any lane folds to a rail, and an
  // EMPTY lane folds itself. The two halves of the law live in
  // boardLogic (pure, unit-pinned): laneFolded decides the state for one
  // lane (reality outranks the override - an empty lane is folded pinned
  // open or not), reconcileLanePins drops an override the moment its
  // lane drains so a refilled lane comes back expanded instead of
  // remembering an override reality already overruled.
  //
  // Pins are component state, not persisted: the lane set is a view of
  // the current board, and a pin that outlived its lane would be exactly
  // the override-fighting-reality shape the law forbids.
  const lanePins = ref<LanePins>({});
  const laneCounts = computed(() => {
    const counts: Record<string, number> = {};
    cols.value.forEach((col, i) => {
      counts[BOARD_STATUSES[i]] = col.length;
    });
    return counts;
  });
  watch(
    laneCounts,
    (counts) => {
      lanePins.value = reconcileLanePins(counts, lanePins.value);
    },
    { immediate: true },
  );

  // A live drag FREEZES the lane fold state. The optimistic reorder seats
  // the dragged card in the lane under the pointer, which drains its
  // source lane and fills its target lane - and an empty lane folds while
  // a filled one unfolds, so without the freeze every lane beside the
  // moving card re-lays-out mid-gesture and the drop lands on whatever
  // slid under the cursor (measured live: a release on the folded
  // `blocked` rail at x=908 persisted into `ready`, 74px to its left).
  // Snapshot when the gesture becomes a drag, release it on pointerup:
  // rails stay rails under the cursor, and the lane the card LANDS in
  // takes its new shape after the commit. Same law as the live-reorder
  // feedback: the geometry a drag is aiming at never moves under it.
  const frozenLanes = ref<Record<string, boolean> | null>(null);

  function freezeLaneLayout() {
    const frozen: Record<string, boolean> = {};
    for (const status of BOARD_STATUSES) {
      frozen[status] = laneFolded(
        status,
        laneCounts.value[status] ?? 0,
        lanePins.value,
      );
    }
    frozenLanes.value = frozen;
  }

  // rails are a horizontal-scroll affordance only (Endo ruling: "not in
  // mobile view") - the narrow-stack mode renders every lane expanded.
  // An open create form also holds the lane in its column shape: a rail
  // cannot be unfolded (an empty lane is always folded), so the create
  // form is the ONE way to grow a lane from nothing - it must be able to
  // open, and it is an editor rather than a resting lane.
  function isLaneFolded(status: string, count: number): boolean {
    if (narrow.value) return false;
    if (createOpen.value === status) return false;
    const frozen = frozenLanes.value;
    if (frozen !== null) return frozen[status] ?? false;
    return laneFolded(status, count, lanePins.value);
  }

  function toggleLane(status: string, count: number) {
    if (narrow.value) return;
    lanePins.value = toggleLanePin(status, count, lanePins.value);
  }

  // --- create form (round t_f81e845e): one column hosts it at a time,
  // the host column is the preset status. Fields: title (required),
  // body, assignee, priority (0..3, the board ladder).
  const createOpen = ref<string | null>(null);
  const createTitle = ref("");
  const createBody = ref("");
  const createAssignee = ref("");
  const createPriority = ref(0);

  function toggleCreate(status: string) {
    if (createOpen.value === status) {
      closeCreate();
      return;
    }
    createOpen.value = status;
    createTitle.value = "";
    createBody.value = "";
    createAssignee.value = "";
    createPriority.value = 0;
    // focus the title once the form mounts (the ref array inside v-for
    // is messy; the scoped query by data-status is straight)
    requestAnimationFrame(() => {
      boardEl.value
        ?.querySelector<HTMLInputElement>(`.board-col[data-status="${status}"] .col-create-title`)
        ?.focus();
    });
  }
  function closeCreate() {
    createOpen.value = null;
  }
  async function submitCreate() {
    const title = createTitle.value.trim();
    const status = createOpen.value;
    if (!title || !status) return;
    try {
      const res = await fetch(withBoard("/tasks", boardName.value), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title,
          body: createBody.value,
          assignee: createAssignee.value.trim(),
          priority: createPriority.value,
          status,
        }),
      });
      if (!res.ok) throw new Error(`POST /tasks: ${res.status}`);
      closeCreate();
      await fetchBoard();
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // --- moves: arrow buttons and the drawer's status selector
  async function moveBoard(t: BoardTask, status: string) {
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(t.id)}`, boardName.value), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
      if (!res.ok) throw new Error(`PATCH /tasks/${t.id}: ${res.status}`);
      await fetchBoard();
      if (selected.value?.id === t.id) await loadDetail(t.id);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // --- drag-and-drop reorder (card t_09b44af1): a card is draggable
  // with the pointer; during the drag the board's list itself is
  // reordered live (the pushed-around cards ARE the drop feedback),
  // and the drop PATCHes the same pair the live view rendered -
  // {status, after_id} - so the optimistic view is exactly what the
  // server persisted. A drag that never clears the threshold stays a
  // click (the drawer). The click that trails a real drag is
  // suppressed, and a canvas delta that arrives mid-drag never
  // re-reads the list (fetchBoard's guard) so the gesture keeps the
  // view it is building.
  interface DragState {
    id: string;
    fromStatus: string;
    status: string;
    afterId: string;
    over: boolean;
    moved: boolean;
  }
  const drag = ref<DragState | null>(null);
  const DRAG_THRESHOLD_PX = 6;

  function isDragging(id: string): boolean {
    return drag.value !== null && drag.value.moved && drag.value.id === id;
  }

  function updateDropTarget(x: number, y: number) {
    const d = drag.value;
    if (!d || !boardEl.value) return;
    // the pointer rides elementsFromPoint - the dragged card itself may
    // be under the cursor, so search the stack, not just the top element
    const colEl = document
      .elementsFromPoint(x, y)
      .flatMap((el) => (el instanceof Element ? [el.closest(".board-col")] : []))
      .find((c): c is HTMLElement => c !== null);
    if (!colEl || !colEl.dataset.status) {
      d.over = false; // released off-board: the drop reverts, no PATCH
      return;
    }
    const cards = Array.from(colEl.querySelectorAll<HTMLElement>(".card")).filter(
      (c) => c.dataset.id !== d.id,
    );
    let index = cards.length;
    for (let i = 0; i < cards.length; i++) {
      const r = cards[i].getBoundingClientRect();
      if (y < r.top + r.height / 2) {
        index = i;
        break;
      }
    }
    const status = colEl.dataset.status;
    const afterId = index > 0 ? (cards[index - 1].dataset.id ?? "") : "";
    d.over = true;
    if (status === d.status && afterId === d.afterId) return; // idempotent slot
    d.status = status;
    d.afterId = afterId;
    tasks.value = reorderAfter(tasks.value, d.id, status, afterId) ?? tasks.value;
  }

  function onCardPointerDown(ev: PointerEvent, t: BoardTask) {
    if (ev.button !== 0) return;
    if ((ev.target as HTMLElement).closest("button, input")) return; // the meta buttons
    const sx = ev.clientX;
    const sy = ev.clientY;
    drag.value = {
      id: t.id,
      fromStatus: t.status,
      status: t.status,
      afterId: "",
      over: false,
      moved: false,
    };
    const move = (e: PointerEvent) => {
      const d = drag.value;
      if (!d) return;
      if (!d.moved && Math.hypot(e.clientX - sx, e.clientY - sy) < DRAG_THRESHOLD_PX) return;
      if (!d.moved) {
        // the gesture is a drag now: pin every lane's shape before the
        // optimistic reorder can move one (see freezeLaneLayout)
        d.moved = true;
        freezeLaneLayout();
      }
      updateDropTarget(e.clientX, e.clientY);
    };
    const up = () => {
      endDragListeners();
      const d = drag.value;
      drag.value = null;
      // the lanes may take their new shapes from here on - the drop is made
      frozenLanes.value = null;
      if (d && d.moved) {
        suppressNextClick = true;
        void commitDrag(d);
      }
    };
    const endDragListeners = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    ev.preventDefault(); // no text selection started by a drag press
  }

  async function commitDrag(d: DragState) {
    if (!d.over) {
      // released off-board: snap the optimistic view back to the server
      await fetchBoard();
      return;
    }
    try {
      const res = await fetch(
        withBoard(`/tasks/${encodeURIComponent(d.id)}`, boardName.value),
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ status: d.status, after_id: d.afterId }),
        },
      );
      if (!res.ok) throw new Error(`PATCH /tasks/${d.id}: ${res.status}`);
    } catch (e) {
      // the server refused the slot: the refetch below snaps the card back
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
    await fetchBoard();
  }

  // click trails pointerup: a real drag suppresses it once, a plain
  // press (below threshold) opens the drawer
  let suppressNextClick = false;
  function onCardClick(t: BoardTask) {
    if (suppressNextClick) {
      suppressNextClick = false;
      return;
    }
    openDetail(t);
  }

  // --- detail drawer
  const selected = ref<BoardTask | null>(null);
  const detailComments = ref<BoardComment[]>([]);
  const detailEvents = ref<BoardEvent[]>([]);

  function openDetail(t: BoardTask) {
    selected.value = { ...t, body: t.body };
    detailComments.value = [];
    detailEvents.value = [];
    loadDetail(t.id);
  }

  async function loadDetail(id: string) {
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(id)}`, boardName.value));
      if (!res.ok) throw new Error(`GET /tasks/${id}: ${res.status}`);
      const d = await res.json();
      selected.value = d.task;
      detailComments.value = d.comments ?? [];
      detailEvents.value = d.events ?? [];
      // fold the fresh detail back into the board row - a move in the
      // drawer must not wait for the next board read to leave the column
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = d.task;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // board-read drawer refresh: pull the drawn task's detail without
  // spinning the loading flag (the user may be typing a comment)
  async function applyDetailPatch(id: string) {
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(id)}`, boardName.value));
      if (!res.ok) return;
      const d = await res.json();
      selected.value = d.task;
      detailComments.value = d.comments ?? [];
      detailEvents.value = d.events ?? [];
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = d.task;
    } catch {
      // a failed patch read stays silent - the next board read retries
    }
  }

  // --- in-place edits (round t_f81e845e): the drawer reports a patch
  // (title/body/priority); the board PATCHes and refetches both views
  async function onDrawerSave(patch: { title?: string; body?: string; priority?: number }) {
    if (!selected.value) return;
    const id = selected.value.id;
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(id)}`, boardName.value), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      });
      if (!res.ok) {
        const detail = await res.text().catch(() => `PATCH /tasks/${id}: ${res.status}`);
        setNotice(detail, true);
        return;
      }
      await fetchBoard();
      await loadDetail(id);
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // armed delete (card t_5a638160): the drawer confirmed a NAMED card; the
  // card leaves the grid on the click, not on the round trip (optimistic),
  // and the drawer closes with it. A refused DELETE (server is the law, and
  // there is no undo anywhere - the confirm is the safety) snaps the board
  // back to the server's truth and says why.
  async function onDrawerDelete() {
    if (!selected.value) return;
    const id = selected.value.id;
    selected.value = null;
    tasks.value = tasks.value.filter((t) => t.id !== id);
    try {
      const res = await fetch(withBoard(taskDeletePath(id), boardName.value), {
        method: "DELETE",
      });
      if (!res.ok) {
        const detail = await res.text().catch(() => `DELETE /tasks/${id}: ${res.status}`);
        setNotice(detail, true);
        await fetchBoard();
        return;
      }
      setNotice(`card ${id} deleted`);
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
      await fetchBoard();
    }
  }

  function moveSelected(status: string) {
    if (selected.value) moveBoard(selected.value, status);
  }

  async function submitComment(body: string) {
    if (!selected.value || !body.trim()) return;
    try {
      const res = await fetch(
        withBoard(`/tasks/${encodeURIComponent(selected.value.id)}/comments`, boardName.value),
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ body }),
        },
      );
      if (!res.ok) throw new Error(`POST comments: ${res.status}`);
      await loadDetail(selected.value.id);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  onMounted(() => {
    fetchBoards().then(() => fetchBoard());
    window.addEventListener(CANVAS_DELTA_EVENT, onCanvasDelta);
  });
  onUnmounted(() => {
    window.removeEventListener(CANVAS_DELTA_EVENT, onCanvasDelta);
    if (deltaTimer !== null) {
      clearTimeout(deltaTimer); // a queued read must not fire into a dead component
      deltaTimer = null;
    }
  });
</script>

<style scoped>
  .board {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    position: relative;
    overflow: hidden;
  }
  /* card t_308e6d9b: the head row is the shared TabBand now - the
     band's own padding/lead replace the old .board-head geometry; only
     the action-group spacing rides here (the band look is shared). */
  .board :deep(.tab-band-actions) {
    gap: 8px;
  }
  .board-switch-label {
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    opacity: 0.5;
  }
  /* card t_acad02ec: the switcher is the shared BandSelect chip - the
     band's dim look (no box at rest, like .band-tool) */
  .board-switch-select :deep(.model-chip) {
    padding: 2px 6px;
    font-size: 11.5px;
    background: transparent;
  }
  .board-switch-count {
    font-size: 10.5px;
    opacity: 0.5;
  }
  /* card t_acad02ec: the remove action rides .band-tool (the shared
     action-button look); only the armed confirm state rides here - the
     tinge is the second-click warning */
  .board-remove-btn.armed {
    border: 1px solid color-mix(in srgb, currentcolor 40%, transparent);
    font-weight: 600;
  }
  .board-notice {
    margin: 0;
    padding: 4px 12px 0;
    font-size: 10.5px;
    opacity: 0.65;
    flex: none;
    overflow-wrap: anywhere;
  }
  .board-notice.err {
    font-weight: 600;
  }
  .board-scroll {
    display: flex;
    gap: 10px;
    overflow-x: auto;
    overflow-y: hidden;
    padding: 10px 12px;
    flex: 1;
    min-height: 0;
  }
  .board-col {
    flex: 0 0 236px;
    min-width: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
    border-radius: var(--r-pane);
    padding: 8px;
    gap: 8px;
    /* the header row and the rail's dot row are the SAME height - that is
       why dots line up across the board in either state (card t_50a8c145).
       24px is the row the header already settles at (its + rides the
       shell's 24px icon button); pinning it keeps a folded lane's dot on
       the same line as an unfolded lane's. */
    --lane-head-h: 24px;
    /* the fold ease: ~180ms width, ruled (Hermes snaps; omega eases) */
    transition:
      flex-basis var(--lane-ease, 180ms) cubic-bezier(0.4, 0, 0.2, 1),
      padding var(--lane-ease, 180ms) cubic-bezier(0.4, 0, 0.2, 1);
  }
  /* a folded lane IS the rail: a thin vertical strip whose dot row sits at
     the expanded lanes' header row height */
  .board-col.folded {
    flex: 0 0 var(--lane-rail-width, 36px);
    padding: 8px 0;
    align-items: center;
    overflow: hidden;
  }
  .lane-rail-wrap {
    flex: 1;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    min-height: 0;
  }
  .lane-rail {
    flex: 1;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    padding: 0;
    font: inherit;
  }
  .lane-rail:hover .lane-rail-name,
  .lane-rail:hover .lane-rail-count {
    opacity: 0.95;
  }
  .lane-dot-row {
    flex: none;
    height: var(--lane-head-h, 20px);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .lane-dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    /* the tone rides an inline background from the registry (laneTone) */
    box-shadow: 0 0 0 1px color-mix(in srgb, currentcolor 12%, transparent);
  }
  .lane-rail-name {
    writing-mode: vertical-rl;
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    opacity: 0.65;
  }
  .lane-rail-count {
    font-size: 10.5px;
    opacity: 0.5;
  }
  .col-head {
    display: flex;
    align-items: center;
    gap: 6px;
    height: var(--lane-head-h, 24px);
    cursor: default;
  }
  /* every lane folds: dot + name are the control (no settled-only branch).
     The dot centers on the header ROW (not on the button box), which is
     what puts it on the same line as a folded lane's dot row. */
  .col-fold-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    font: inherit;
    padding: 0;
  }
  .col-fold-btn:hover .col-name {
    opacity: 0.95;
  }
  .col-fold-btn:disabled {
    cursor: default;
  }
  .col-name {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.65;
  }
  .col-count {
    margin-left: auto;
    font-size: 11px;
    opacity: 0.5;
  }
  .col-create-btn {
    margin-left: 2px;
  }
  .col-create {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .col-create-title,
  .col-create-body,
  .col-create-assignee {
    font: inherit;
    font-size: 12px;
    color: inherit;
    background: color-mix(in srgb, currentcolor 6%, transparent);
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-card);
    padding: 5px 8px;
  }
  .col-create-body {
    min-height: 40px;
    resize: vertical;
  }
  .col-create-actions {
    display: flex;
    gap: 4px;
  }
  .col-create-actions {
    align-items: center;
  }
  .col-create-cancel {
    font: inherit;
    font-size: 11px;
    color: inherit;
    cursor: pointer;
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-chip);
    background: none;
    padding: 4px 8px;
  }
  .col-create-prio {
    flex: none;
  }
  .col-create-prio :deep(.model-chip) {
    background: transparent;
    font-size: 11.5px;
    padding: 2px 6px;
  }
  .col-add-btn {
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-chip);
    background: none;
    color: inherit;
    cursor: pointer;
    font: inherit;
    font-size: 12px;
    padding: 4px 8px;
  }
  .col-add-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .col-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-y: auto;
    min-height: 0;
  }
  .col-empty {
    margin: 0;
    font-size: 11px;
    opacity: 0.35;
    text-align: center;
  }
  .card {
    border: 1px solid color-mix(in srgb, currentcolor 12%, transparent);
    border-radius: var(--r-card);
    padding: 7px 9px;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .card:hover {
    border-color: color-mix(in srgb, currentcolor 22%, transparent);
  }
  /* drag-and-drop reorder: the live-wrestled card dims in place */
  .card.dragging {
    opacity: 0.35;
    cursor: grabbing;
  }
  .card-title-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  /* priority chip: heat rises with the level (0 stays silent entirely) */
  .card-prio {
    flex: none;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.03em;
    border-radius: var(--r-chip);
    padding: 1px 5px;
    border: 1px solid color-mix(in srgb, currentcolor 25%, transparent);
  }
  .card-prio.prio-1 {
    opacity: 0.75;
  }
  .card-prio.prio-2 {
    background: color-mix(in srgb, currentcolor 12%, transparent);
  }
  .card-prio.prio-3 {
    background: color-mix(in srgb, currentcolor 18%, transparent);
    border-color: color-mix(in srgb, currentcolor 40%, transparent);
  }
  .card-preview {
    font-size: 11px;
    line-height: 1.3;
    opacity: 0.55;
    max-height: 2.6em;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .card-title {
    font-size: 12px;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
  .card-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 10.5px;
    opacity: 0.65;
  }
  .card-age,
  .card-assignee {
    flex: none;
  }
  .card-moves {
    margin-left: auto;
    display: flex;
    gap: 2px;
  }
  .icon-btn {
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    font-size: 11px;
    padding: 0 3px;
  }

  /* narrow (card t_49d0b308): the inspector's ~300px column. The wide
     strip flex: 0 0 236px column row only works in the main pane - under
     the measured threshold the root scrolls and the columns stack into
     read-mostly vertical lists, full width, no horizontal scroll. */
  .board.narrow {
    overflow-y: auto;
  }
  .board.narrow .board-scroll {
    flex: none;
    flex-direction: column;
    overflow: visible;
    padding: 8px;
    gap: 8px;
  }
  .board.narrow .board-col {
    flex: none;
    width: 100%;
  }
  .board.narrow .card {
    padding: 6px 8px;
  }
  .board.narrow .col-create-body {
    min-height: 32px;
  }
  /* the drawer overlays the visible column, not the scrollable content */
  .board.narrow :deep(.drawer) {
    position: fixed;
    right: 0;
  }
</style>