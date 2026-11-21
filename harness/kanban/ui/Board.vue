<template>
  <section
    ref="boardEl"
    class="board"
    :class="{ narrow: narrow }"
    :style="{ '--card-clamp': `${CARD_CLAMP_LINES}` }"
    @contextmenu="onBoardMenu">
    <!-- the UNIVERSAL band (shared TabBand): the columns start BELOW it, so scrolling never moves it,
         and it keeps the family contract - title + board chip + count | filter. -->
    <TabBand title="Kanban">
      <template #lead>
        <template v-if="boards.length > 0">
          <template v-if="renaming">
            <input
              v-model="renameDraft"
              class="board-rename-input"
              aria-label="new board name"
              placeholder="new name"
              @keydown.enter.prevent="submitRename"
              @keydown.esc.prevent="cancelRename" />
            <button
              class="band-tool board-rename-save"
              title="save the new name"
              @click="submitRename">save</button>
            <button
              class="band-tool board-rename-cancel"
              title="keep the old name"
              @click="cancelRename">cancel</button>
          </template>
          <!-- the switcher is the shared BandSelect listbox. -->
          <BandSelect
            v-else
            class="board-switch-select"
            :model-value="boardName"
            :options="boardMenu"
            label="board"
            drop="down"
            @update:model-value="switchBoard($event)" />
          <span
            v-if="boardName && !renaming"
            class="board-switch-count">{{ countLabel }}</span>
        </template>
      </template>
      <template v-if="boards.length > 0">
        <!-- ONE filter seat, TWO scopes. -->
        <input
          v-model="query"
          class="board-search"
          type="search"
          placeholder="filter cards"
          aria-label="filter cards" />
        <button
          v-if="filtering"
          class="band-tool board-scope-chip"
          :class="{ on: scope === 'all' }"
          :title="scope === 'all'
            ? 'filtering every board - click to filter this board only'
            : 'filtering this board - click to filter every board'"
          @click="toggleScope">
          {{ scope === "all" ? "all boards" : "this board" }}
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
    <!-- zero boards is a LEGAL registry: the panel must not render a grid over nothing, so it
         offers the one way back - a board name. -->
    <div
      v-if="registryKnown && boards.length === 0"
      class="board-empty">
      <p class="board-empty-title">no boards</p>
      <p class="board-empty-note">
        A board holds the cards. Create one to start; every board can be
        renamed or removed from the board menu above, and so can this one.
      </p>
      <form
        class="board-empty-create"
        @submit.prevent="submitCreateBoard">
        <input
          v-model="newBoardName"
          class="board-empty-input"
          placeholder="board name (a-z, 0-9, - or _)"
          aria-label="new board name" />
        <button
          class="board-empty-btn"
          type="submit"
          :disabled="!newBoardName.trim()">create board</button>
      </form>
    </div>
    <div
      v-else
      class="board-scroll"
      :key="`${boardName}:${scope}`">
      <div
        v-for="(col, i) in cols"
        :key="BOARD_STATUSES[i]"
        class="board-col"
        :data-status="BOARD_STATUSES[i]"
        :data-count="col.length"
        :class="{ folded: isLaneFolded(BOARD_STATUSES[i], col.length) }"
        :style="{ '--lane-rail-width': `${LANE_RAIL_PX}px`, '--lane-ease': `${LANE_EASE_MS}ms` }">
        <!-- ANY lane folds to a rail. -->
        <div
          v-if="isLaneFolded(BOARD_STATUSES[i], col.length)"
          class="lane-rail-wrap">
          <button
            class="lane-rail"
            :aria-expanded="false"
            :aria-label="`unfold ${BOARD_STATUSES[i]} lane`"
            :title="`unfold ${BOARD_STATUSES[i]} lane`"
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
            <button
              class="icon-btn col-create-btn"
              :title="`new card in ${BOARD_STATUSES[i]}`"
              :aria-label="`new card in ${BOARD_STATUSES[i]}`"
              @click.stop="toggleCreate(BOARD_STATUSES[i])">
              +
            </button>
          </header>

          <!-- the full create form: open under exactly one column at a time -->
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
            <!-- the assignee is a COMBOBOX fed by the agent registry; typing stays legal, since a
                 card may name a worker the registry no longer lists. -->
            <div class="col-create-combo">
              <input
                v-model="createAssignee"
                class="col-create-assignee"
                role="combobox"
                aria-label="new card assignee"
                aria-autocomplete="list"
                aria-controls="board-assignee-options"
                :aria-expanded="comboOpen"
                autocomplete="off"
                spellcheck="false"
                placeholder="assignee (optional)"
                @focus="openAssigneeCombo"
                @input="onAssigneeInput"
                @keydown="onAssigneeKeydown"
                @blur="closeAssigneeCombo" />
              <ul
                v-if="comboOpen"
                id="board-assignee-options"
                class="col-create-combo-list"
                role="listbox">
                <li
                  v-for="(o, i) in assigneeRows"
                  :key="o.value"
                  class="col-create-combo-row"
                  :class="{ active: i === comboIndex }"
                  role="option"
                  :aria-selected="i === comboIndex"
                  @mousedown.prevent="pickAssignee(o.value)">
                  <span class="col-create-combo-label">{{ o.label }}</span>
                  <span
                    v-if="o.hint"
                    class="col-create-combo-hint">{{ o.hint }}</span>
                </li>
                <li
                  v-if="!assigneeRows.length"
                  class="col-create-combo-empty">
                  {{ agentOptionsError || "no agents in the registry - typing a name still works" }}
                </li>
              </ul>
            </div>
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
              @contextmenu="onCardMenu($event, t)"
              @pointerdown="onCardPointerDown($event, t)">
              <div class="card-title-row">
                <span
                  v-if="hasPriorityChip(t.priority)"
                  class="card-prio"
                  :class="`prio-${Math.min(t.priority, 3)}`">{{ priorityLabel(t.priority) }}</span>
                <div class="card-title card-clamp">{{ t.title }}</div>
              </div>
              <div
                v-if="bodyPreview(t.body)"
                class="card-preview card-clamp">{{ bodyPreview(t.body) }}</div>
              <div class="card-meta">
                <span
                  v-if="scope === 'all' && t.board"
                  class="card-board-chip"
                  :title="`this card lives on board ${t.board}`">{{ t.board }}</span>
                <span
                  v-if="t.assignee"
                  class="card-assignee-chip"
                  :title="`assignee: ${t.assignee}`">@{{ t.assignee }}</span>
                <span
                  v-if="heartbeats[t.id]"
                  class="card-heartbeat"
                  :class="{ stale: heartbeats[t.id].stale }"
                  :title="heartbeats[t.id].stale
                    ? 'this running card stopped beating - the worker may be gone'
                    : 'the card is running and its worker is beating'">
                  <span class="card-heartbeat-dot" />{{ heartbeats[t.id].label }}
                </span>
                <span
                  v-else
                  class="card-age">{{ age(t.created_at) }}</span>
                <span class="card-moves">
                  <!-- a mixed list (all-boards scope) is a READING surface. -->
                  <button
                    v-if="scope !== 'all' && step(BOARD_STATUSES[i], -1)"
                    class="icon-btn"
                    :title="`move to ${step(BOARD_STATUSES[i], -1)}`"
                    @click.stop="moveBoard(t, step(BOARD_STATUSES[i], -1) as string)">
                    ◀
                  </button>
                  <button
                    v-if="scope !== 'all' && step(BOARD_STATUSES[i], +1)"
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
  import { computed, nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from "vue";
  import { copyText } from "~/composables/clipboard";
  import { openContextMenu } from "~/composables/contextMenu";
  import DetailDrawer from "./board/DetailDrawer.vue";
  import { cardMenuItems } from "./board/cardMenu";
  import { boardMenuItems } from "./board/boardMenu";
  import {
    BOARD_STATUSES,
    BOARDS_PATH,
    CARD_CLAMP_LINES,
    HEARTBEAT_TICK_MS,
    LANE_EASE_MS,
    LANE_RAIL_PX,
    age,
    boardCount,
    boardDeletePath,
    boardMenuOptions,
    boardMenuPick,
    boardOf,
    boardRenamePath,
    bodyPreview,
    canRemoveBoard,
    columns,
    fetchAgentOptions,
    fetchBoardsRetry,
    filterTasks,
    hasPriorityChip,
    heartbeatChip,
    isNarrowWidth,
    laneFolded,
    laneTone,
    pickBootBoard,
    priorityLabel,
    priorityOptions,
    reorderAfter,
    step,
    taskDeletePath,
    tasksPath,
    toggleLanePin,
    withBoard,
    type AgentOption,
    type BoardComment,
    type BoardEvent,
    type BoardInfo,
    type BoardTask,
    type HeartbeatChip,
    type LanePins,
    type SearchScope,
  } from "./board/boardLogic";

  defineEmits<{
    (e: "open-session", id: string): void;
  }>();

  const tasks = ref<BoardTask[]>([]);
  const error = ref("");

  // board switcher: the dropdown rides GET /boards.
  const boards = ref<BoardInfo[]>([]);
  const boardName = ref("");
  // registryKnown separates "the registry answered, and it is EMPTY" from "the registry
  // never answered".
  const registryKnown = ref(false);
  const newBoardName = ref("");

  // the filter seat: ONE input, TWO scopes.
  const query = ref("");
  const scope = ref<SearchScope>("board");
  const visibleTasks = computed(() => filterTasks(tasks.value, query.value));
  // the scope UI exists only while the seat holds a query.
  const filtering = computed(() => query.value.trim() !== "");
  const countLabel = computed(() =>
    boardCount(visibleTasks.value.length, tasks.value.length, filtering.value),
  );

  // the chip's dropdown ladder rides BandSelect.
  const boardMenu = computed(() => boardMenuOptions(boards.value, confirmingRemove.value));

  async function fetchBoards() {
    const fetched = await fetchBoardsRetry(fetch);
    if (!fetched) return; // absent capability or every attempt missed
    boards.value = fetched;
    registryKnown.value = true;
    boardName.value = pickBootBoard(boards.value);
  }

  function switchBoard(pick: string) {
    const want = boardMenuPick(pick);
    if (want.kind === "rename") {
      startRename();
      return;
    }
    if (want.kind === "remove") {
      removeBoardClick();
      return;
    }
    const name = want.name;
    if (!name || name === boardName.value) return;
    disarmRemove(); // an armed confirm points at the old board: reset it
    disarmCardDelete(); // and the card menu's arm points at a card of the old board
    cancelRename();
    boardName.value = name;
    selected.value = null; // the drawer's card belongs to the old board
    scope.value = "board"; // a switch answers "show me THAT board"
    fetchBoard();
  }

  function toggleScope() {
    scope.value = scope.value === "all" ? "board" : "all";
    selected.value = null; // the drawer's card belongs to the other scope
    fetchBoard();
  }

  // the scope chip only RENDERS while a query runs, so an emptied seat must fall back to
  // this board rather than leave a stale all-boards scope behind.
  watch(query, (q) => {
    if (q.trim() !== "" || scope.value === "board") return;
    toggleScope();
  });

  // create board: the empty state's only control.
  async function submitCreateBoard() {
    const name = newBoardName.value.trim();
    if (!name) return;
    try {
      const res = await fetch(BOARDS_PATH, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      });
      if (!res.ok) {
        // the refusal text (illegal name, duplicate) is written for this line
        const detail = await res.text().catch(() => `POST /boards: ${res.status}`);
        setNotice(detail, true);
        return;
      }
      newBoardName.value = "";
      setNotice(`board "${name}" created`);
      await fetchBoards();
      boardName.value = name; // the new board is where the eye goes
      await fetchBoard();
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // rename: the band's rename control swaps the selector for an input. The row keeps its
  // id server-side, so the cards never move - only the name they are reached under.
  const renaming = ref(false);
  const renameDraft = ref("");

  function startRename() {
    if (!boardName.value) return;
    disarmRemove();
    renaming.value = true;
    renameDraft.value = boardName.value;
    // the menu pick that starts this UNMOUNTS the chip and mounts the input, so the focus
    // move waits for the patch (nextTick) - an rAF raced the render and lost the selection
    void nextTick(() => {
      boardEl.value?.querySelector<HTMLInputElement>(".board-rename-input")?.select();
    });
  }

  function cancelRename() {
    renaming.value = false;
    renameDraft.value = "";
  }

  async function submitRename() {
    const from = boardName.value;
    const to = renameDraft.value.trim();
    if (!from || !to || to === from) {
      cancelRename();
      return;
    }
    try {
      const res = await fetch(boardRenamePath(from), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: to }),
      });
      if (!res.ok) {
        const detail = await res.text().catch(() => `PATCH /boards/${from}: ${res.status}`);
        setNotice(detail, true);
        return;
      }
      cancelRename();
      setNotice(`board "${from}" renamed to "${to}"`);
      await fetchBoards();
      boardName.value = to;
      await fetchBoard();
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // board removal: destructive, so the confirm is an armed second click. The server is the
  // law - DeleteBoard refuses the default board, the current pointer target and boards
  // still holding tasks - and its refusal text surfaces verbatim in the notice line. The
  // arm rides the notice line too: the popup closes with the pick, but the trail must not.
  const REMOVE_ARM_MS = 10000;
  const confirmingRemove = ref(false);
  const notice = ref("");
  const noticeErr = ref(false);
  let disarmTimer: ReturnType<typeof setTimeout> | undefined;
  let armNotice = "";

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
    // the arm's own hint must not outlive the arm.
    if (armNotice && notice.value === armNotice) setNotice("");
    armNotice = "";
  }

  function removeBoardClick() {
    if (!canRemoveBoard(boardName.value)) return;
    if (!confirmingRemove.value) {
      confirmingRemove.value = true;
      armNotice = `pick "Confirm delete" again to remove "${boardName.value}" - the board and its cards go with it`;
      setNotice(armNotice);
      // disarm on timeout: the second click lands only while the warning still shows
      if (disarmTimer) clearTimeout(disarmTimer);
      disarmTimer = setTimeout(disarmRemove, REMOVE_ARM_MS);
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
        const detail = await res.text().catch(() => `DELETE /boards: ${res.status}`);
        setNotice(detail, true);
        return;
      }
      setNotice(`board "${name}" removed`);
      selected.value = null; // the drawer's card belonged to the gone board
      // refresh the registry first: the removed row must leave the switcher even if the
      // read fails.
      try {
        const res = await fetch("/boards");
        if (res.ok) {
          const body = await res.json();
          boards.value = (body.boards ?? []).filter((b: BoardInfo) => b.name !== name);
        }
      } catch {
        boards.value = boards.value.filter((b) => b.name !== name);
      }
      boardName.value = pickBootBoard(boards.value);
      await fetchBoard();
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // a task-notification click focuses this board. The stash covers the mount race (the tab
  // focus mounts the component on the tick AFTER the shell published, so the mount consumes
  // it once); the event covers the already-mounted case and clears the stash it supersedes.
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

  // live refresh: no poll - the board re-reads on the shell's canvas delta.
  const CANVAS_DELTA_EVENT = "omega-canvas-delta";
  // The same event carries a RESET pulse (id "reconnect"), fired from the shell's reconnect
  // catch-up: a delta that landed while this page's socket was down is never replayed, and
  // without the pulse the board would stay stale until some later mutation. The literal is
  // duplicated from composables/canvas.ts (CANVAS_RESET_ID) on purpose - module UI imports
  // no shell code.
  const CANVAS_RESET_ID = "reconnect";
  interface CanvasDeltaPanel {
    id?: string;
  }
  // A burst of deltas coalesces into ONE list read: a sibling agent touching N tasks
  // must not fan N full-list GETs.
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
      // a reconnect: the registry AND the list may have moved while the socket was down.
      void reloadAll();
      return;
    }
    if (detail.id === "tasks") {
      scheduleBoardRefetch();
      return;
    }
    if (detail.id === "boards") void refreshRegistry();
  }

  // reloadAll is the reset path: the registry first (the selected board may have
  // vanished), then the list - fetchBoard never runs on a stale boardName.
  async function reloadAll() {
    await refreshRegistry();
    await fetchBoard();
  }

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

  // the list's request epoch: an event-triggered refetch and a mutation's own refetch
  // can overlap, and the older answer must never overwrite the newer one.
  let fetchSeq = 0;

  async function fetchBoard() {
    if (drag.value && drag.value.moved) return; // a live drag owns the list
    const seq = ++fetchSeq;
    try {
      // status=all: the grid carries the archived lane too, so it can never default to the
      // active-only view the list tool shows.
      const res = await fetch(tasksPath(boardName.value, scope.value));
      if (!res.ok) throw new Error(`GET /tasks: ${res.status}`);
      const body = await res.json();
      if (seq !== fetchSeq) return; // a newer read owns the list
      tasks.value = body.tasks ?? [];
      error.value = "";
      if (selected.value) applyDetailPatch(selected.value.id);
    } catch (e) {
      if (seq !== fetchSeq) return;
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  const cols = computed(() => columns(visibleTasks.value));

  // the running heartbeat chip. The tick NEVER fetches - it only re-dates `now`, which
  // the chip map derives.
  const now = ref(new Date());
  let heartbeatTimer: ReturnType<typeof setTimeout> | undefined;
  function tickHeartbeat() {
    now.value = new Date();
    heartbeatTimer = setTimeout(tickHeartbeat, HEARTBEAT_TICK_MS);
  }
  const heartbeats = computed(() => {
    const at = now.value;
    const out: Record<string, HeartbeatChip> = {};
    for (const t of tasks.value) {
      const chip = heartbeatChip(t, at);
      if (chip) out[t.id] = chip;
    }
    return out;
  });

  const boardEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !boardEl.value) return;
    // rAF deferred: the flip changes the observed layout, so the callback must not
    // mutate this frame
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

  // any lane folds to a rail, and an EMPTY lane folds itself. Pins are component state, not
  // persisted: a pin that outlived its lane would be the override-fighting-reality shape the
  // law forbids.
  const lanePins = ref<LanePins>({});
  const laneCounts = computed(() => {
    const counts: Record<string, number> = {};
    cols.value.forEach((col, i) => {
      counts[BOARD_STATUSES[i]] = col.length;
    });
    return counts;
  });

  // A live drag FREEZES the lane fold state. Same law as the live-reorder feedback: the
  // geometry a drag is aiming at never moves under it.
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

  // rails are a horizontal-scroll affordance only. An open create form also holds its lane
  // in column shape: a rail cannot be unfolded, so the form is the ONE way to grow a lane
  // from nothing.
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

  // create form: one column hosts it at a time, the host column is the preset.
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
    closeAssigneeCombo();
    void ensureAgentOptions(); // the assignee's source, loaded on first use
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

  async function moveBoard(t: BoardTask, status: string) {
    const board = boardOf(t, boardName.value);
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(t.id)}`, board), {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
      if (!res.ok) throw new Error(`PATCH /tasks/${t.id}: ${res.status}`);
      await fetchBoard();
      if (selected.value?.id === t.id) await loadDetail(t.id, board);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // drag-and-drop reorder: the list is reordered live during the drag, and the drop PATCHes
  // the same pair the live view rendered - {status, after_id} - so the optimistic view is
  // exactly what persisted. A drag that never clears the threshold stays a click, and a
  // canvas delta mid-drag never re-reads the list (fetchBoard's guard).
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
    if (scope.value === "all") return; // a mixed list never drags: the move is a board op
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
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
    await fetchBoard();
  }

  let suppressNextClick = false;
  function onCardClick(t: BoardTask) {
    if (suppressNextClick) {
      suppressNextClick = false;
      return;
    }
    openDetail(t);
  }

  // the meta row's copy path: the clipboard composable's one honest answer decides the
  // notice, so a refused copy never reads as "copied".
  async function copyCardId(id: string) {
    const ok = await copyText(id);
    setNotice(ok ? `card ${id} copied` : "copy failed - the clipboard is unavailable", !ok);
  }

  // armed delete: a card is irreversible (no undo resurrects it), so the menu's Delete row
  // only arms; the second pick - the row reading "Confirm delete" - fires. The drawer's own
  // confirm stays its own door's: one confirm per door, never two stacked on one pick.
  const CARD_DELETE_ARM_MS = 10000;
  const armedCardDelete = ref("");
  let cardDeleteTimer: ReturnType<typeof setTimeout> | undefined;

  function disarmCardDelete() {
    armedCardDelete.value = "";
    if (cardDeleteTimer) {
      clearTimeout(cardDeleteTimer);
      cardDeleteTimer = undefined;
    }
  }

  function onCardDeleteRow(t: BoardTask) {
    if (armedCardDelete.value !== t.id) {
      disarmCardDelete();
      armedCardDelete.value = t.id;
      cardDeleteTimer = setTimeout(disarmCardDelete, CARD_DELETE_ARM_MS);
      setNotice(
        `right-click card ${t.id} again and pick "Confirm delete" to remove it - a deleted card does not come back`,
      );
      return;
    }
    disarmCardDelete();
    setNotice("");
    void deleteCard(t.id, boardOf(t, boardName.value));
  }

  // The right-click menus the card under the pointer (the closures capture it), never the
  // drawer's selection. Archive rides moveBoard, the lane arrow's own PATCH; Delete is the
  // drawer's DELETE, behind the arm above.
  function onCardMenu(e: MouseEvent, t: BoardTask) {
    e.preventDefault();
    void openContextMenu(
      e.clientX,
      e.clientY,
      cardMenuItems(
        { id: t.id, status: t.status },
        {
          open: () => openDetail(t),
          copyId: () => void copyCardId(t.id),
          archive: () => void moveBoard(t, "archived"),
          remove: () => onCardDeleteRow(t),
        },
        armedCardDelete.value === t.id,
      ),
    );
  }

  // The board surface's right-click: Rename and Delete, the board dropdown's own two verbs.
  // A card, the drawer or a text field under the pointer keeps its own menu - the card row
  // handler above, the drawer's content, and paste inside the filter/rename inputs.
  function onBoardMenu(e: MouseEvent) {
    const target = e.target as HTMLElement | null;
    if (target?.closest?.(".card, .drawer, input, textarea")) return;
    e.preventDefault();
    void openContextMenu(
      e.clientX,
      e.clientY,
      boardMenuItems(
        { name: boardName.value, armed: confirmingRemove.value },
        { rename: startRename, remove: removeBoardClick },
      ),
    );
  }

  const selected = ref<BoardTask | null>(null);
  const detailComments = ref<BoardComment[]>([]);
  const detailEvents = ref<BoardEvent[]>([]);

  function openDetail(t: BoardTask) {
    selected.value = { ...t, body: t.body };
    detailComments.value = [];
    detailEvents.value = [];
    loadDetail(t.id, boardOf(t, boardName.value));
  }

  async function loadDetail(id: string, board: string = boardName.value) {
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(id)}`, board));
      if (!res.ok) throw new Error(`GET /tasks/${id}: ${res.status}`);
      const d = await res.json();
      selected.value = d.task;
      detailComments.value = d.comments ?? [];
      detailEvents.value = d.events ?? [];
      // fold the fresh detail back into the board row: a move in the drawer must not leave
      // the grid stale.
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = { ...d.task, board: tasks.value[i].board };
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  async function applyDetailPatch(id: string) {
    const board = selected.value ? boardOf(selected.value, boardName.value) : boardName.value;
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(id)}`, board));
      if (!res.ok) return;
      const d = await res.json();
      selected.value = d.task;
      detailComments.value = d.comments ?? [];
      detailEvents.value = d.events ?? [];
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = { ...d.task, board: tasks.value[i].board };
    } catch {
    }
  }

  async function onDrawerSave(patch: { title?: string; body?: string; priority?: number }) {
    if (!selected.value) return;
    const id = selected.value.id;
    const board = boardOf(selected.value, boardName.value);
    try {
      const res = await fetch(withBoard(`/tasks/${encodeURIComponent(id)}`, board), {
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
      await loadDetail(id, board);
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e), true);
    }
  }

  // THE delete path: DELETE /tasks/{id} on the board the card belongs to, with the row gone
  // from the grid first (the server's refusal text restores it through fetchBoard). The
  // drawer's armed confirm and the card menu's arm both land here.
  async function deleteCard(id: string, board: string) {
    tasks.value = tasks.value.filter((t) => t.id !== id);
    if (selected.value?.id === id) selected.value = null;
    try {
      const res = await fetch(withBoard(taskDeletePath(id), board), {
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

  // armed delete: the drawer confirmed a NAMED card.
  async function onDrawerDelete() {
    if (!selected.value) return;
    const id = selected.value.id;
    const board = boardOf(selected.value, boardName.value);
    selected.value = null;
    disarmCardDelete(); // the menu's arm points at the same card: the drawer just spent it
    await deleteCard(id, board);
  }

  function moveSelected(status: string) {
    if (selected.value) moveBoard(selected.value, status);
  }

  async function submitComment(body: string) {
    if (!selected.value || !body.trim()) return;
    const board = boardOf(selected.value, boardName.value);
    try {
      const res = await fetch(
        withBoard(`/tasks/${encodeURIComponent(selected.value.id)}/comments`, board),
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ body }),
        },
      );
      if (!res.ok) throw new Error(`POST comments: ${res.status}`);
      await loadDetail(selected.value.id, board);
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // the assignee combobox: picking is the fast path, typing stays legal (a card may name a
  // worker the registry no longer lists), and the form must never strand that card.
  const agentOptions = ref<AgentOption[]>([]);
  const agentOptionsError = ref("");
  let agentsInflight: Promise<void> | null = null;

  function ensureAgentOptions(): Promise<void> {
    if (agentsInflight) return agentsInflight;
    if (agentOptions.value.length > 0) return Promise.resolve();
    agentsInflight = fetchAgentOptions(fetch)
      .then((opts) => {
        agentOptions.value = opts;
        agentOptionsError.value = "";
      })
      .catch((e: unknown) => {
        agentOptions.value = [];
        agentOptionsError.value = e instanceof Error ? e.message : String(e);
      })
      .finally(() => {
        agentsInflight = null;
      });
    return agentsInflight;
  }

  const comboOpen = ref(false);
  const comboIndex = ref(-1);
  // the rows on offer: the registry filtered by what has been typed. An
  // empty list is honest (the panel says so) - it never blocks the field.
  const assigneeRows = computed(() => {
    const q = createAssignee.value.trim().toLowerCase();
    if (q === "") return agentOptions.value;
    return agentOptions.value.filter((o) =>
      `${o.value} ${o.hint}`.toLowerCase().includes(q),
    );
  });

  function openAssigneeCombo() {
    comboOpen.value = true;
    comboIndex.value = -1;
    void ensureAgentOptions();
  }
  function closeAssigneeCombo() {
    comboOpen.value = false;
    comboIndex.value = -1;
  }
  function onAssigneeInput() {
    comboOpen.value = true;
    comboIndex.value = -1;
  }
  function pickAssignee(name: string) {
    createAssignee.value = name;
    closeAssigneeCombo();
  }
  function onAssigneeKeydown(ev: KeyboardEvent) {
    if (ev.key === "Escape") {
      closeAssigneeCombo();
      return;
    }
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      const n = assigneeRows.value.length;
      if (!comboOpen.value) comboOpen.value = true;
      if (n === 0) return;
      ev.preventDefault();
      const dir = ev.key === "ArrowDown" ? 1 : -1;
      comboIndex.value = (comboIndex.value + dir + n) % n;
      return;
    }
    if (ev.key === "Enter") {
      const rows = assigneeRows.value;
      if (comboOpen.value && comboIndex.value >= 0 && comboIndex.value < rows.length) {
        ev.preventDefault(); // pick, do not submit the form
        pickAssignee(rows[comboIndex.value].value);
      }
    }
  }

  onMounted(() => {
    fetchBoards().then(() => fetchBoard());
    window.addEventListener(CANVAS_DELTA_EVENT, onCanvasDelta);
    // the heartbeat clock: a running card's "working - Xm" must advance without a fetch
    tickHeartbeat();
  });
  onUnmounted(() => {
    window.removeEventListener(CANVAS_DELTA_EVENT, onCanvasDelta);
    if (heartbeatTimer !== undefined) {
      clearTimeout(heartbeatTimer); // the clock must not tick into a dead component
      heartbeatTimer = undefined;
    }
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
  /*
   * the head row is the shared TabBand: only the action-group spacing rides here. The slot
   * holds the filter seat and its scope chip, so the chip is fixed and the input gives up
   * the width (TabBand ships flex-shrink: 0, which an inline seat must relax to absorb).
   */
  .board :deep(.tab-band-actions) {
    gap: 3px;
    flex-shrink: 1;
    min-width: 0;
  }
  .board-switch-select :deep(.model-chip) {
    padding: 2px 6px;
    font-size: 11.5px;
    background: transparent;
  }
  /* the dropdown's ladder ends with the board VERBS. */
  .board-switch-select :deep(.model-list li:nth-last-child(2)) {
    border-top: 1px solid color-mix(in srgb, currentcolor 12%, transparent);
  }
  /* the armed remove ROW: BandSelect's `cls` seam carries the marker; the tint is the
     second-click warning. */
  .board-switch-select :deep(.model-list li.armed) {
    color: var(--err);
    font-weight: 600;
  }
  .board-switch-count {
    font-size: 10.5px;
    opacity: 0.5;
  }
  /* a NARROW pane with a live filter: the width where "title + chip + count | filter + chip"
     cannot all be one row, so the count drops. */
  .board.narrow:has(.board-scope-chip) .board-switch-count {
    display: none;
  }
  /* the rename seat takes the selector's place. */
  .board-rename-input {
    font: inherit;
    font-size: 11.5px;
    color: inherit;
    width: 116px;
    padding: 2px 6px;
    background: color-mix(in srgb, currentcolor 8%, transparent);
    border: 1px solid color-mix(in srgb, currentcolor 22%, transparent);
    border-radius: var(--r-chip);
  }
  .board-search {
    font: inherit;
    font-size: 11.5px;
    color: inherit;
    width: 132px;
    max-width: 132px;
    min-width: 72px;
    flex: 1 1 auto;
    padding: 2px 6px;
    background: transparent;
    border: 1px solid color-mix(in srgb, currentcolor 16%, transparent);
    border-radius: var(--r-chip);
  }
  .board-search::placeholder {
    opacity: 0.5;
  }
  /* the scope chip: the family.band-tool look, tightened, and no wrap. */
  .board-scope-chip {
    flex: none;
    font-size: 10.5px;
    padding: 0 5px;
    white-space: nowrap;
  }
  .board-scope-chip.on {
    background: var(--z1-raised);
    font-weight: 600;
    opacity: 0.95;
  }
  .board-empty {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
    gap: 8px;
    padding: 10px 16px 24px;
  }
  .board-empty-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.7;
  }
  .board-empty-note {
    margin: 0;
    max-width: 46ch;
    font-size: 11.5px;
    line-height: 1.45;
    opacity: 0.6;
  }
  .board-empty-create {
    display: flex;
    gap: 6px;
    align-items: center;
    margin-top: 2px;
  }
  .board-empty-input {
    font: inherit;
    font-size: 12px;
    color: inherit;
    width: 200px;
    padding: 4px 8px;
    background: color-mix(in srgb, currentcolor 6%, transparent);
    border: 1px solid color-mix(in srgb, currentcolor 18%, transparent);
    border-radius: var(--r-card);
  }
  .board-empty-btn {
    font: inherit;
    font-size: 12px;
    color: inherit;
    cursor: pointer;
    padding: 4px 10px;
    border: 1px solid color-mix(in srgb, currentcolor 22%, transparent);
    border-radius: var(--r-chip);
    background: none;
  }
  .board-empty-btn:disabled {
    opacity: 0.35;
    cursor: default;
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
    /* the header row and the rail's dot row are the SAME height. */
    --lane-head-h: 24px;
    transition:
      flex-basis var(--lane-ease, 180ms) cubic-bezier(0.4, 0, 0.2, 1),
      padding var(--lane-ease, 180ms) cubic-bezier(0.4, 0, 0.2, 1);
  }
  /* a folded lane IS the rail. */
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
  /* the assignee combobox, floating over the card body. */
  .col-create-combo {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .col-create-combo-list {
    position: absolute;
    top: calc(100% + 2px);
    left: 0;
    right: 0;
    z-index: 30;
    margin: 0;
    padding: 2px;
    list-style: none;
    max-height: 180px;
    overflow-y: auto;
    background: var(--z2, #26262a);
    border: 1px solid var(--stroke, #2e2e31);
    border-radius: var(--r-chip);
    box-shadow: var(--shadow-2, 0 8px 30px rgba(0, 0, 0, 0.5));
  }
  .col-create-combo-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding: 3px 6px;
    border-radius: 3px;
    cursor: pointer;
  }
  .col-create-combo-row.active {
    background: color-mix(in srgb, currentcolor 18%, transparent);
  }
  .col-create-combo-label {
    flex: none;
    max-width: 60%;
    font-size: 11px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .col-create-combo-hint {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 10.5px;
    opacity: 0.55;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .col-create-combo-empty {
    padding: 3px 6px;
    font-size: 10.5px;
    opacity: 0.55;
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
    overflow-wrap: anywhere;
  }
  /* the card text clamp: two lines, fed by the CARD_CLAMP_LINES constant. */
  .card-clamp {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: var(--card-clamp, 2);
    line-clamp: var(--card-clamp, 2);
    overflow: hidden;
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
    min-width: 0;
    /* one row never spills the card. */
    overflow: hidden;
  }
  .card-age,
  .card-moves {
    flex: none;
  }
  /* the assignee as a CHIP: a bordered token reads as a person, not as stray "@name". */
  .card-assignee-chip {
    border: 1px solid color-mix(in srgb, currentcolor 22%, transparent);
    border-radius: var(--r-chip);
    padding: 0 5px;
    line-height: 1.6;
    min-width: 0;
    max-width: 96px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* the board a mixed-scope row came from. */
  .card-board-chip {
    flex: none;
    border: 1px dashed color-mix(in srgb, currentcolor 26%, transparent);
    border-radius: var(--r-chip);
    padding: 0 5px;
    line-height: 1.6;
    max-width: 84px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.8;
  }
  .card-heartbeat {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    white-space: nowrap;
    color: color-mix(in srgb, #34d399 80%, currentcolor);
    font-variant-numeric: tabular-nums;
  }
  .card-heartbeat-dot {
    flex: none;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentcolor;
    animation: card-beat 1.6s ease-in-out infinite;
  }
  .card-heartbeat.stale {
    color: color-mix(in srgb, #f87171 85%, currentcolor);
    font-weight: 600;
  }
  .card-heartbeat.stale .card-heartbeat-dot {
    animation: none;
    box-shadow: 0 0 0 1px color-mix(in srgb, currentcolor 35%, transparent);
  }
  @keyframes card-beat {
    0%,
    100% {
      opacity: 0.35;
    }
    50% {
      opacity: 1;
    }
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
  .board.narrow :deep(.drawer) {
    position: fixed;
    right: 0;
  }
</style>