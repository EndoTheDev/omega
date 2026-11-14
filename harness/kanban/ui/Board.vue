<template>
  <section
    ref="boardEl"
    class="board"
    :class="{ narrow: narrow }"
    :style="{ '--card-clamp': `${CARD_CLAMP_LINES}` }">
    <!-- card t_308e6d9b: the UNIVERSAL band (the shared TabBand) - the
         board switcher + count + remove ride the band now (the law: the
         tab's actions live in the band, not floating over the body).
         The board scroll columns start BELOW the band - scrolling never
         moves it.
         0.17.3 make-over: the SELECTOR moved LEFT into the band's lead
         seat, beside the title - the board names the panel, so its name
         sits with the tab's name. The destructive pair (rename, remove)
         stays far right. -->
    <TabBand title="Kanban">
      <template #lead>
        <template v-if="boards.length > 0">
          <span class="board-switch-label">board</span>
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
          <!-- card t_acad02ec (audit M1): the switcher rides the themed
               listbox (BandSelect) - a native select popup is OS chrome
               over the pane -->
          <BandSelect
            v-else
            class="board-switch-select"
            :model-value="boardName"
            :options="boardOptions"
            label="board"
            drop="down"
            @update:model-value="switchBoard($event)" />
          <span
            v-if="boardName && !renaming"
            class="board-switch-count">{{ tasks.length }} card<template v-if="tasks.length !== 1">s</template></span>
        </template>
      </template>
      <template v-if="boards.length > 0">
        <!-- 0.17.3 make-over: ONE filter seat, TWO scopes -->
        <input
          v-model="query"
          class="board-search"
          type="search"
          placeholder="filter cards"
          aria-label="filter cards" />
        <button
          class="band-tool board-scope-btn"
          :class="{ on: scope === 'all' }"
          :title="scope === 'all'
            ? 'filtering every board - click to filter this board only'
            : 'filtering this board - click to filter every board'"
          @click="toggleScope">
          {{ scope === "all" ? "all boards" : "this board" }}
        </button>
        <button
          class="band-tool board-rename-btn"
          :title="`rename board ${boardName}`"
          @click="startRename">rename</button>
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
    <!-- the zero-board empty state (0.17.3 make-over): zero boards is a
         LEGAL registry, so the panel must not render a grid over nothing -
         it offers the one way back, a board name. -->
    <div
      v-if="registryKnown && boards.length === 0"
      class="board-empty">
      <p class="board-empty-title">no boards</p>
      <p class="board-empty-note">
        A board holds the cards. Create one to start; every board can be
        renamed or removed from the band above, and so can this one.
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
        <!-- card t_50a8c145: ANY lane folds to a rail. The rail is BOTH a
             button (click unfolds) and a live drop target - the drag
             machinery resolves .board-col, so dropping on a rail needs no
             drop code of its own (the card lands at the top of the lane).
             The dot row sits at the header row's height, so dots line up
             across the board whether a lane is folded or not. 0.17.3: a
             click expands a collapsed lane whether it holds cards or not -
             an empty lane opens to its create form instead of being a dead
             strip. -->
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
            <!-- 0.17.3 make-over: the assignee is a COMBOBOX fed by the
                 agent registry - GET /agents, the same source the cron
                 editor's agent field reads. Typing stays legal: a card may
                 name a worker the registry no longer lists, and the form
                 must never strand that card. A failed GET degrades to no
                 options (the field stays a plain text input). -->
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
              <!-- the meta row (card t_65ea61a1): assignee as a chip, the
                   running heartbeat, the age - the Hermes card grammar. -->
              <div class="card-meta">
                <!-- the all-boards scope renders a mixed grid: a row says
                     which board it came from, or the card would look like
                     it lives in the lane's current board -->
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
                <!-- one time telling per card: a running card's story is
                     its heartbeat, so the created-age stands down there
                     (both would compete for a 218px row and the chip
                     would clip) -->
                <span
                  v-else
                  class="card-age">{{ age(t.created_at) }}</span>
                <span class="card-moves">
                  <!-- a mixed list (all-boards scope) is a READING surface:
                       the arrows would move a card inside the wrong board,
                       and the cross-board-move ban is the store's law -->
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
  import { computed, onBeforeUnmount, onMounted, onUnmounted, ref } from "vue";
  import DetailDrawer from "./board/DetailDrawer.vue";
  import {
    BOARD_STATUSES,
    BOARDS_PATH,
    CARD_CLAMP_LINES,
    HEARTBEAT_TICK_MS,
    LANE_EASE_MS,
    LANE_RAIL_PX,
    age,
    boardDeletePath,
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

  // The module-owned board view: 9 hermes columns in one horizontal
  // scroll, self-contained API wiring (fetch /tasks) so it rides the
  // desktop / VS Code builds alone, like the tools canary did.
  defineEmits<{
    (e: "open-session", id: string): void;
  }>();

  const tasks = ref<BoardTask[]>([]);
  const error = ref("");

  // --- board switcher (card t_d7a7beaa): the dropdown rides GET
  // /boards; "main" is only the seeded first board's name. "" means the
  // registry has no board at all (0.17.3: a LEGAL state - the panel then
  // renders its create-board empty state). The inspector (drawer) reads
  // the same board - every call below goes through withBoard.
  const boards = ref<BoardInfo[]>([]);
  const boardName = ref("");
  // registryKnown separates "the registry answered, and it is EMPTY" from
  // "the registry never answered" - only the first one is the empty state,
  // the second is the old silent degrade (the board still renders).
  const registryKnown = ref(false);
  const newBoardName = ref("");

  // --- the filter seat (0.17.3 make-over) ------------------------------
  // ONE input, TWO scopes: this board, or every board. The all-boards
  // scope is a SERVER read (?scope=all) whose rows carry their board name,
  // so a mixed grid still addresses each card correctly.
  const query = ref("");
  const scope = ref<SearchScope>("board");
  const visibleTasks = computed(() => filterTasks(tasks.value, query.value));

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
    registryKnown.value = true;
    boardName.value = pickBootBoard(boards.value);
  }

  function switchBoard(name: string) {
    if (!name || name === boardName.value) return;
    disarmRemove(); // an armed confirm points at the old board: reset it
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

  // --- create board (0.17.3): the empty state's only control -----------
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

  // --- rename (0.17.3): the band's rename control swaps the selector for
  // an input; Enter/PATCH saves, Esc keeps the old name. The row keeps its
  // id server-side, so the cards never move - only the name they are
  // reached under.
  const renaming = ref(false);
  const renameDraft = ref("");

  function startRename() {
    if (!boardName.value) return;
    disarmRemove();
    renaming.value = true;
    renameDraft.value = boardName.value;
    requestAnimationFrame(() => {
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
      // the selection falls to whatever survives the delete - and to
      // NOTHING when the last board just left, which is the legal
      // zero-board state (the panel renders its empty state)
      boardName.value = pickBootBoard(boards.value);
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
      // the active-only view the list tool shows (t_02a8beea).
      // The scope picks the address: this board, or (0.17.3) every board
      // in one answer whose rows each carry their board name.
      const res = await fetch(tasksPath(boardName.value, scope.value));
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

  // the grid renders the FILTERED list; the counts in the band stay the
  // board's own (a filter is a lens, not a second truth)
  const cols = computed(() => columns(visibleTasks.value));

  // --- the running heartbeat chip (card t_65ea61a1): the board already
  // receives everything the chip needs (status, started_at, and
  // last_heartbeat_at when the store stamps it - see boardLogic's
  // BoardTask note), so this is a CLOCK, not a wire call. One tick moves
  // the whole board's chips: the labels read whole minutes, so a 5s
  // cadence advances them on the minute. The tick NEVER fetches - it
  // only re-dates `now`, which the chip map derives from. (The pin that
  // killed the old 5s fetch poll still holds: the only timer here is
  // this self-chaining clock.)
  const now = ref(new Date());
  let heartbeatTimer: ReturnType<typeof setTimeout> | undefined;
  function tickHeartbeat() {
    now.value = new Date();
    heartbeatTimer = setTimeout(tickHeartbeat, HEARTBEAT_TICK_MS);
  }
  // one pass per render, keyed by card id: the template reads the map
  // instead of re-deriving a chip per element (and per re-render).
  const heartbeats = computed(() => {
    const at = now.value;
    const out: Record<string, HeartbeatChip> = {};
    for (const t of tasks.value) {
      const chip = heartbeatChip(t, at);
      if (chip) out[t.id] = chip;
    }
    return out;
  });

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

  // --- lane rails (card t_50a8c145, 0.17.3 make-over): any lane folds to
  // a rail, and an EMPTY lane folds itself. The click is what outranks
  // reality now: a collapsed lane - empty or not - EXPANDS on click, so
  // the pin below is the only override and nothing reconciles it away
  // (the old reconcile dropped a drained lane's pin, which would have
  // re-folded the lane the user just opened).
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
    closeAssigneeCombo();
    void ensureAgentOptions(); // the assignee's source, loaded on first use
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

  // --- moves: arrow buttons and the drawer's status selector. The board a
  // card is moved IN is the card's own (the all-boards read stamps it), not
  // the board being viewed - the cross-board-move ban holds.
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
      // fold the fresh detail back into the board row - a move in the
      // drawer must not wait for the next board read to leave the column
      const i = tasks.value.findIndex((x) => x.id === id);
      if (i >= 0) tasks.value[i] = { ...d.task, board: tasks.value[i].board };
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  // board-read drawer refresh: pull the drawn task's detail without
  // spinning the loading flag (the user may be typing a comment)
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
      // a failed patch read stays silent - the next board read retries
    }
  }

  // --- in-place edits (round t_f81e845e): the drawer reports a patch
  // (title/body/priority); the board PATCHes and refetches both views
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

  // armed delete (card t_5a638160): the drawer confirmed a NAMED card; the
  // card leaves the grid on the click, not on the round trip (optimistic),
  // and the drawer closes with it. A refused DELETE (server is the law, and
  // there is no undo anywhere - the confirm is the safety) snaps the board
  // back to the server's truth and says why.
  async function onDrawerDelete() {
    if (!selected.value) return;
    const id = selected.value.id;
    const board = boardOf(selected.value, boardName.value);
    selected.value = null;
    tasks.value = tasks.value.filter((t) => t.id !== id);
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

  // --- the assignee combobox (0.17.3 make-over) ------------------------
  // The create form's assignee reads the AGENT REGISTRY (GET /agents) - the
  // same source the cron editor's agent field reads. Picking is the fast
  // path; TYPING stays legal both ways: a card may name a worker the
  // registry no longer lists, and the form must never strand that card.
  // The source is loaded lazily (first create form open) and cached; a
  // failed GET keeps the field a plain text input and says why.
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
    // the heartbeat clock: a running card's "working - Xm" must advance
    // without a fetch (the card body moved a minute older, nothing
    // changed on the wire)
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
  /* 0.17.3 make-over: the rename seat takes the selector's place - the same
     compact in-band rhythm, wide enough for a board name */
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
  /* the filter seat: one input, two scopes. The chip beside it says which
     scope is live; the input itself is the seat (no icon expander). */
  .board-search {
    font: inherit;
    font-size: 11.5px;
    color: inherit;
    width: 132px;
    min-width: 96px;
    padding: 2px 6px;
    background: transparent;
    border: 1px solid color-mix(in srgb, currentcolor 16%, transparent);
    border-radius: var(--r-chip);
  }
  .board-search::placeholder {
    opacity: 0.5;
  }
  .board-scope-btn.on {
    border: 1px solid color-mix(in srgb, currentcolor 34%, transparent);
    font-weight: 600;
    opacity: 0.95;
  }
  /* the zero-board empty state (0.17.3): the panel's whole body when the
     registry holds nothing - one sentence and the one way back */
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
  /* the assignee combobox (0.17.3 make-over): the input keeps the field
     look; the list hangs under it like the cron editor's combo */
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
    overflow-wrap: anywhere;
  }
  /* the card text clamp (card t_65ea61a1): two lines, fed by the
     CARD_CLAMP_LINES constant. A long title no longer pushes the meta
     row off the card - the full text lives in the drawer. */
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
    /* one row never spills the card: the chips shrink, the move
       controls keep their place (card t_65ea61a1 - the heartbeat chip
       pushed the row past the column and the LANE grew a scrollbar) */
    overflow: hidden;
  }
  .card-age,
  .card-moves {
    flex: none;
  }
  /* the assignee as a CHIP (card t_65ea61a1): a bordered token reads as
     a person, not as stray "@name" text in the meta line */
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
  /* the board a mixed-scope row came from (0.17.3): dimmer than the
     assignee - it names a place, not a person */
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
  /* the running heartbeat chip (card t_65ea61a1): a dot that breathes
     while the worker beats, a flat red dot once it goes quiet. The tone
     is the running lane's own green; stale is the blocked lane's red. */
  .card-heartbeat {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    /* the chip is the row's shrinker: a long duration truncates before
       anything is pushed off the card (card t_65ea61a1) */
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
  /* a dead beat does not pretend to pulse */
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