// Module-owned kanban board: logic + composition wiring.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

import {
  BOARD_FETCH_ATTEMPTS,
  BOARD_FETCH_BACKOFF_MS,
  BOARD_MENU_REMOVE,
  BOARD_MENU_RENAME,
  BOARD_STATUSES,
  BOARD_PATH,
  BOARDS_PATH,
  CARD_CLAMP_LINES,
  DEFAULT_BOARD_NAME,
  HEARTBEAT_STALE_MS,
  HEARTBEAT_TICK_MS,
  LANE_EASE_MS,
  LANE_RAIL_PX,
  LANE_TONES,
  age,
  bodyPreview,
  boardCount,
  boardDeletePath,
  boardMenuOptions,
  boardMenuPick,
  boardOf,
  boardRenamePath,
  canRemoveBoard,
  columns,
  fetchAgentOptions,
  fetchBoardsRetry,
  filterTasks,
  hasPriorityChip,
  heartbeatChip,
  isBoardPath,
  laneFolded,
  laneTone,
  minuteLabel,
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
  type BoardInfo,
  type BoardTask,
  type LanePins,
} from "../../harness/kanban/ui/board/boardLogic.ts";

import {
  CANVAS_DELTA_EVENT,
  CANVAS_RESET_ID,
  announceCanvasReset,
  createCanvasStore,
} from "../app/composables/canvas.ts";

const uiRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function task(over: Partial<BoardTask>): BoardTask {
  return {
    id: "t_testid",
    title: "a task",
    body: "",
    assignee: "",
    status: "triage",
    priority: 0,
    session_id: "",
    source: "",
    created_at: "2026-10-19T00:00:00Z",
    started_at: "",
    completed_at: "",
    ...over,
  };
}

test("columns: canonical order, one bucket per status", () => {
  const cols = columns([
    task({ id: "1", status: "running" }),
    task({ id: "2", status: "triage" }),
    task({ id: "3", status: "triage" }),
  ]);
  assert.equal(cols.length, 9);
  assert.equal(cols[0].length, 2); // triage
  assert.equal(cols[4].length, 1); // running
  assert.equal(cols[5].length, 0); // blocked
  // unknown statuses land nowhere and do not throw
  assert.doesNotThrow(() => columns([task({ status: "bogus" })]));
});

test("step: clamped arrows over the 9-status sequence", () => {
  assert.equal(step("triage", -1), null); // left edge renders no button
  assert.equal(step("archived", +1), null); // right edge
  assert.equal(step("triage", +1), "todo");
  assert.equal(step("todo", -1), "triage");
  assert.equal(step("review", +1), "done");
  assert.equal(step("bogus", +1), null);
});

test("age: compact relative age, empty for garbage", () => {
  const now = new Date("2026-10-19T12:00:00Z");
  assert.equal(age("2026-10-19T11:59:40Z", now), "now");
  assert.equal(age("2026-10-19T11:18:00Z", now), "42m");
  assert.equal(age("2026-10-19T08:00:00Z", now), "4h");
  assert.equal(age("2026-10-14T12:00:00Z", now), "5d");
  assert.equal(age("not a date", now), "");
});

test("isBoardPath: /board and its trailing slash", () => {
  assert.ok(isBoardPath("/board"));
  assert.ok(isBoardPath("/board/"));
  assert.ok(!isBoardPath("/chat/abc"));
  assert.equal(BOARD_PATH, "/board");
});

test("statuses: the exact hermes grammar, terminal set last", () => {
  assert.deepEqual(BOARD_STATUSES, [
    "triage", "todo", "scheduled", "ready", "running",
    "blocked", "review", "done", "archived",
  ]);
});

// lane rails: the Hermes lane-collapse model.

test("lane rails: ANY lane folds, not just the two settled ones", () => {
  for (const status of BOARD_STATUSES) {
    assert.equal(laneFolded(status, 3, {}), false, `${status}: an unpinned lane with cards stays open`);
    assert.equal(
      laneFolded(status, 3, { [status]: "collapsed" }),
      true,
      `${status}: a collapsed pin folds it to a rail`,
    );
    assert.equal(
      laneFolded(status, 3, { [status]: "open" }),
      false,
      `${status}: an open pin keeps it a column`,
    );
  }
  assert.equal(laneFolded("done", 2, {}), laneFolded("triage", 2, {}));
  assert.equal(laneFolded("archived", 2, {}), laneFolded("review", 2, {}));
});

test("lane rails: a CLICK expands a collapsed lane - the empty lane too (0.17.3)", () => {
  // an EMPTY lane still folds itself (reality: nothing to show).
  assert.equal(laneFolded("triage", 0, {}), true);
  assert.equal(laneFolded("blocked", 0, { blocked: "collapsed" }), true);
  // .but a CLICK outranks reality now: the rail opens, empty or not
  const opened = toggleLanePin("triage", 0, {});
  assert.equal(opened.triage, "open", "a click on an empty rail writes an open pin");
  assert.equal(laneFolded("triage", 0, opened), false, "an empty lane pinned open stays open");
  const closed = toggleLanePin("triage", 0, opened);
  assert.equal(closed.triage, "collapsed");
  assert.equal(laneFolded("triage", 0, closed), true);

  assert.equal(laneFolded("review", 1, { review: "open" }), false);
  assert.equal(laneFolded("review", 1, { review: "collapsed" }), true);

  let lanes: LanePins = { review: "open" };
  assert.equal(laneFolded("review", 2, lanes), false);
  assert.equal(laneFolded("review", 0, lanes), false, "the pin survives the drain");
  assert.equal(laneFolded("review", 2, lanes), false, "and the refill");

  const folded = toggleLanePin("todo", 2, {});
  assert.equal(folded.todo, "collapsed", "a column folds");
  assert.equal(laneFolded("todo", 2, folded), true);
  const unfolded = toggleLanePin("todo", 2, folded);
  assert.equal(unfolded.todo, "open", "a rail unfolds");
  assert.equal(laneFolded("todo", 2, unfolded), false);
});

test("lane rails: the per-lane dot tone is the Hermes status mapping", () => {
  assert.deepEqual(LANE_TONES, {
    triage: "#8f8f8f",
    todo: "#cfcfcf",
    scheduled: "#a78bfa",
    ready: "#60a5fa",
    running: "#34d399",
    blocked: "#f87171",
    review: "#fbbf24",
    done: "#6f6f6f",
    archived: "#4a4a4a",
  });
  for (const status of BOARD_STATUSES) assert.match(laneTone(status), /^#[0-9a-f]{6}$/);
  assert.equal(laneTone("scheduled"), "#a78bfa", "violet, per the ruling");
  assert.equal(laneTone("running"), "#34d399", "green, per the ruling");
  assert.equal(laneTone("bogus"), laneTone("triage"), "an unknown status degrades to the triage tone");
});

test("lane rails: the rail renders the dot, the name and the count", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /class="lane-rail"/, "a folded lane renders the rail");
  assert.match(boardVue, /class="lane-dot-row"/, "the rail opens with its dot row");
  assert.match(boardVue, /class="lane-rail-name">\{\{ BOARD_STATUSES\[i\] \}\}/, "the lane name rides the rail");
  assert.match(
    boardVue,
    /v-if="col\.length"\s*\n\s*class="lane-rail-count">\{\{ col\.length \}\}/,
    "the count renders on the rail only when the lane holds cards",
  );
  assert.match(
    boardVue,
    /:style="\{ background: laneTone\(BOARD_STATUSES\[i\]\) \}"/,
    "the dot takes its tone from the lane",
  );
  assert.match(boardVue, /\.lane-rail-name \{\s*\n\s*writing-mode: vertical-rl;/, "the rail name is vertical");
  assert.match(boardVue, /text-transform: uppercase;/, "and uppercase");
  // the dot row sits at the EXPANDED header row's height - that is the
  // alignment contract across folded and unfolded lanes
  assert.match(boardVue, /\.lane-dot-row \{[\s\S]*?height: var\(--lane-head-h/, "the rail's dot row is a header row tall");
  assert.match(boardVue, /\.col-head \{[\s\S]*?height: var\(--lane-head-h/, "the header row is that same height");
  assert.match(boardVue, /--lane-head-h: 24px;/, "one constant drives both");
  assert.match(boardVue, /class="lane-rail"[\s\S]{0,400}@click="toggleLane\(BOARD_STATUSES\[i\], col\.length\)"/, "the rail is the unfold control");
  assert.match(boardVue, /:aria-label="`unfold \$\{BOARD_STATUSES\[i\]\} lane`"/, "and names what it does");
  // .AND a drop target: the rail lives inside the same.board-col the drag machinery.
  assert.match(boardVue, /closest\("\.board-col"\)/, "drops resolve the lane element");
  const railAt = boardVue.indexOf('class="lane-rail"');
  const colAt = boardVue.indexOf(':data-status="BOARD_STATUSES[i]"');
  const drawerAt = boardVue.indexOf("<DetailDrawer");
  assert.ok(colAt >= 0 && colAt < railAt && railAt < drawerAt, "the rail renders INSIDE the .board-col[data-status] lane (its own drop target)");
});

test("lane rails: every lane carries the fold control and no settled branch survives", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const logic = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "boardLogic.ts"), "utf8");
  assert.match(boardVue, /class="col-fold-btn"\s*\n\s*:aria-expanded="true"/, "the header fold control renders on every lane");
  assert.doesNotMatch(boardVue, /isSettled/, "the settled-only branch is gone from the view");
  assert.doesNotMatch(logic, /isSettled/, "and from the logic module");
  assert.doesNotMatch(boardVue, /col-folded-note/, "the 'N folded' note died with the settled fold");
  assert.doesNotMatch(boardVue, /settled:/, "no settled class rides the lane");
  assert.match(boardVue, /:class="\{ folded: isLaneFolded\(BOARD_STATUSES\[i\], col\.length\) \}"/, "the lane's folded state is the one fold law");
  assert.doesNotMatch(boardVue, /reconcileLanePins/, "no reconcile takes the click's pin back");
  assert.doesNotMatch(logic, /export function reconcileLanePins/, "and the helper is gone, not orphaned");
  assert.match(boardVue, /lanePins\.value = toggleLanePin\(status, count, lanePins\.value\)/, "the click goes through the pure toggle");
  // an empty lane can still GROW: the rail carries the same + the header
  // does, and an open create form holds the lane in its column shape (a
  // rail cannot be unfolded, so without this the create path would dead-end)
  assert.match(
    boardVue,
    /class="icon-btn col-create-btn rail-create-btn"[\s\S]{0,300}@click\.stop="toggleCreate\(BOARD_STATUSES\[i\]\)"/,
    "the rail keeps the create control",
  );
  assert.match(boardVue, /if \(createOpen\.value === status\) return false;/, "an open create form holds the lane open");
  // a live drag freezes the lane shapes: the optimistic reorder must never re-lay-out.
  assert.match(boardVue, /const frozenLanes = ref<Record<string, boolean> \| null>\(null\)/, "the drag holds a fold snapshot");
  assert.match(boardVue, /d\.moved = true;\s*\n\s*freezeLaneLayout\(\);/, "the snapshot is taken when a gesture turns into a drag");
  assert.match(boardVue, /frozenLanes\.value = null;/, "and released on the drop");
  assert.match(boardVue, /if \(frozen !== null\) return frozen\[status\] \?\? false;/, "the snapshot outranks the live counts while it holds");
});

test("lane rails: ~180ms width ease, and rails are off in the narrow stack", () => {
  // the two numbers are ruled constants, pinned here so a refactor cannot drift them
  assert.equal(LANE_EASE_MS, 180);
  assert.ok(LANE_RAIL_PX >= 32 && LANE_RAIL_PX <= 40, `the rail is a 32-40px strip (got ${LANE_RAIL_PX})`);
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /transition:\s*\n\s*flex-basis var\(--lane-ease, 180ms\) cubic-bezier\(/, "the fold animates the lane's width on an ease");
  assert.match(boardVue, /'--lane-ease': `\$\{LANE_EASE_MS\}ms`/, "the duration comes from the constant");
  assert.match(boardVue, /'--lane-rail-width': `\$\{LANE_RAIL_PX\}px`/, "and so does the rail width");
  assert.match(boardVue, /\.board-col\.folded \{\s*\n\s*flex: 0 0 var\(--lane-rail-width, 36px\);/, "a folded lane is the thin strip");
  assert.match(boardVue, /function isLaneFolded\(status: string, count: number\): boolean \{\s*\n\s*if \(narrow\.value\) return false;/, "the narrow stack renders every lane expanded");
  assert.match(boardVue, /function toggleLane\(status: string, count: number\) \{\s*\n\s*if \(narrow\.value\) return;/, "and refuses the fold gesture");
  assert.match(boardVue, /:disabled="narrow"/, "the fold control is disabled there, not a dead click");
  assert.match(boardVue, /\.board\.narrow \.board-scroll \{/, "the narrow layout is untouched");
});

// structural: the shell mounts the board through the composition

test("board view ships module-owned and the shell mounts it", () => {
  const boardVue = join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue");
  const logic = join(uiRoot, "..", "harness", "kanban", "ui", "board", "boardLogic.ts");
  const drawer = join(uiRoot, "..", "harness", "kanban", "ui", "board", "DetailDrawer.vue");
  assert.ok(existsSync(boardVue), "harness/kanban/ui/Board.vue missing");
  assert.ok(existsSync(logic), "harness/kanban/ui/board/boardLogic.ts missing");
  assert.ok(existsSync(drawer), "harness/kanban/ui/board/DetailDrawer.vue missing");

  // the shell's only kanban knowledge: the panel-list lookup in app.vue
  const appVue = readFileSync(join(uiRoot, "app", "app.vue"), "utf8");
  assert.match(appVue, /modulePanels\.find\(\(p\) => p\.module === "kanban"\)/);
  assert.match(appVue, /mainView === 'board'/);
  assert.match(appVue, /openBoardSession/);
  assert.match(appVue, /\/board/);

  // the board JOINED the inspector.
  const registry = readFileSync(
    join(uiRoot, "app", "composables", "dockables.ts"),
    "utf8",
  );
  assert.match(registry, /"kanban"/, "kanban is a dock tab now");
  assert.match(appVue, /#pane-kanban/, "the inspector outlet exists");
  assert.match(
    appVue,
    /module="kanban"/,
    "the outlet hosts the composition panel",
  );
  // the module declares its dockable title (the DOCK_TAB_TITLE resolver)
  const kanbanDockable = join(uiRoot, "..", "harness", "kanban", "ui", "dockable.json");
  assert.ok(existsSync(kanbanDockable), "harness/kanban/ui/dockable.json missing");
  assert.deepEqual(JSON.parse(readFileSync(kanbanDockable, "utf8")), { title: "Kanban" });
  assert.match(
    readFileSync(join(uiRoot, "app", "composables", "omega.ts"), "utf8"),
    /mainViewModules = new Set<string>\(\["kanban"\]\)/,
  );
});
// board switcher: named boards, pointer boot, URL wiring

test("withBoard: appends the picker, keeps existing queries, passes empty through", () => {
  assert.equal(withBoard("/tasks", "alpha"), "/tasks?board=alpha");
  assert.equal(withBoard("/tasks?status=todo", "alpha"), "/tasks?status=todo&board=alpha");
  assert.equal(withBoard("/tasks", ""), "/tasks"); // server default
  assert.equal(withBoard("/tasks", "  "), "/tasks");
  assert.equal(withBoard("/tasks/t_1/comments", "my_2nd"), "/tasks/t_1/comments?board=my_2nd");
  // names are path-safe payloads: encoded, never raw
  assert.equal(withBoard("/tasks", "a b"), "/tasks?board=a%20b");
});

test("pickBootBoard: current flagged row wins, first row is the fallback", () => {
  const registry = (rows: BoardInfo[]) => rows;
  assert.equal(pickBootBoard(registry([{ name: "main", current: false }, { name: "alpha", current: true }])), "alpha");
  assert.equal(pickBootBoard(registry([{ name: "alpha", current: false }])), "alpha"); // no flag: first row
  assert.equal(pickBootBoard(registry([])), ""); // empty registry: no switcher
  assert.equal(pickBootBoard(undefined as unknown as BoardInfo[]), ""); // noisy payload
});

test("board view wires the switcher through every board call", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /<BandSelect/, "the switcher rides the shared themed listbox (card t_acad02ec)");
  assert.match(boardVue, /fetch\(tasksPath\(boardName\.value, scope\.value\)\)/, "list calls carry the board + scope through the pure path builder");
  assert.match(boardVue, /fetchBoards\(\)/, "boot reads the registry");
  assert.match(boardVue, /selected\.value = null/, "the drawer resets on board switch");
  // every task fetch goes through withBoard/tasksPath - no raw /task path left behind
  const rawTaskFetches = boardVue.match(/fetch\(`?\/tasks/g) ?? [];
  assert.equal(rawTaskFetches.length, 0, "all fetches must be withBoard-wrapped");
  const drawer = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "DetailDrawer.vue"), "utf8");
  assert.doesNotMatch(drawer, /fetch\(/, "the inspector follows the switch via props");
});

test("board head keeps the fillet-tuck top spacing", () => {
  // the head row is the shared TabBand now - the band carries the 14px lead-in that
  // survives the 8px tab-strip tuck (the Files-band contract); the switcher rides the
  // band's actions.
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const band = readFileSync(join(uiRoot, "app", "components", "ui", "TabBand.vue"), "utf8");
  assert.match(boardVue, /<TabBand title="Kanban">/, "the kanban head is the universal band");
  assert.ok(boardVue.indexOf("<TabBand") < boardVue.indexOf("board-scroll"), "the band renders ABOVE the scroll columns");
  assert.doesNotMatch(band, /position: sticky/, "non-scrolling is STRUCTURE, not CSS sticky");
  assert.doesNotMatch(boardVue, /padding:\s*8px 12px 0/,
    "the tuck-consumed 8px spacer stays out");
});

// -- the board's own surface.

test("canRemoveBoard + the board paths: NOTHING is reserved any more (0.17.3)", () => {
  assert.equal(DEFAULT_BOARD_NAME, "main");
  assert.ok(canRemoveBoard("alpha"), "named boards are removable");
  assert.ok(canRemoveBoard("main"), "the default board is removable too - delete-all includes main");
  assert.ok(!canRemoveBoard(""), "an empty selection has nothing to remove");
  assert.equal(boardDeletePath("alpha"), "/boards/alpha");
  assert.equal(boardDeletePath("my_2nd"), "/boards/my_2nd");
  // names are path-safe payloads: encoded, never raw
  assert.equal(boardDeletePath("a b"), "/boards/a%20b");
  assert.equal(boardRenamePath("alpha"), "/boards/alpha");
  assert.equal(boardRenamePath("a b"), "/boards/a%20b");
  assert.equal(BOARDS_PATH, "/boards");
});

test("board view carries the board surface: armed remove, rename seat, zero-board empty state", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  // the pair lives in the board dropdown now, so the pins name the MENU rows.
  assert.match(boardVue, /confirm remove/, "the armed click relabels: inline confirm row");
  assert.doesNotMatch(boardVue, /window\.confirm/, "no native confirm - the panel confirms inline");
  assert.match(boardVue, /method: "DELETE"/, "the removal fires the DELETE route");
  assert.match(boardVue, /boardDeletePath\(name\)/, "the delete goes to the encoded registry path");
  assert.match(boardVue, /if \(!canRemoveBoard\(boardName\.value\)\) return;/, "the remove row keys off the removable name");
  assert.match(boardVue, /board-notice/, "a visible notice reports the removal or refusal");
  assert.match(boardVue, /disarmRemove/, "board switches disarm an armed confirm");
  // the selection falls to whatever SURVIVES.
  assert.match(boardVue, /boardName\.value = pickBootBoard\(boards\.value\)/, "the post-delete selection comes from the registry");
  assert.doesNotMatch(boardVue, /boardName\.value = DEFAULT_BOARD_NAME/, "no hardcoded default fallback");
  // rename: the menu row swaps the selector for an input, PATCHes the row
  assert.match(boardVue, /:options="boardMenu"/, "rename + remove are rows of the board dropdown");
  assert.match(boardVue, /method: "PATCH"/, "the rename PATCHes the board row");
  assert.match(boardVue, /boardRenamePath\(from\)/, "through the pure path helper");
  assert.match(boardVue, /@keydown\.enter\.prevent="submitRename"/, "Enter saves the rename");
  assert.match(boardVue, /@keydown\.esc\.prevent="cancelRename"/, "Esc keeps the old name");
  assert.match(boardVue, /v-if="registryKnown && boards\.length === 0"/, "the empty state renders only when the registry ANSWERED empty");
  assert.match(boardVue, /class="board-empty"/, "the empty state block exists");
  assert.match(boardVue, /@submit\.prevent="submitCreateBoard"/, "the empty state's create form");
  assert.match(boardVue, /fetch\(BOARDS_PATH, \{/, "the create POSTs /boards");
});

// the dropdown's VERB rows: the ladder is boards + the two commands, and a pick
// classifies as a switch or a verb - never.

test("boardMenuOptions + boardMenuPick: the verbs ride the board ladder", () => {
  const boards = [
    { name: "main", current: true },
    { name: "alpha", current: false },
  ] as BoardInfo[];
  const menu = boardMenuOptions(boards);
  assert.deepEqual(
    menu.map((o) => o.value),
    ["main", "alpha", BOARD_MENU_RENAME, BOARD_MENU_REMOVE],
    "the boards first, the verbs last (the list the user picks boards from stays on top)",
  );
  assert.equal(menu[2]!.label, "rename board");
  assert.equal(menu[3]!.label, "remove board");
  assert.equal(menu[3]!.cls, undefined, "an unarmed remove row carries no marker class");
  // the armed row: same VALUE (the pick still reads it), relabelled + marked
  const armed = boardMenuOptions(boards, true);
  assert.equal(armed[3]!.value, BOARD_MENU_REMOVE);
  assert.equal(armed[3]!.label, "confirm remove");
  assert.equal(armed[3]!.cls, "armed");
  assert.deepEqual(boardMenuPick("alpha"), { kind: "board", name: "alpha" });
  assert.deepEqual(boardMenuPick(BOARD_MENU_RENAME), { kind: "rename" });
  assert.deepEqual(boardMenuPick(BOARD_MENU_REMOVE), { kind: "remove" });
  assert.deepEqual(boardMenuPick("!renamed"), { kind: "board", name: "!renamed" }, "a near-miss is a board name, never a verb");
  assert.deepEqual(boardMenuPick(""), { kind: "board", name: "" });
  // the sentinels cannot collide with a board name.
  for (const sentinel of [BOARD_MENU_RENAME, BOARD_MENU_REMOVE]) {
    assert.doesNotMatch(sentinel, /^[a-z0-9]/, "a verb sentinel is outside the board-name grammar");
  }
  // every row is addressable: the ladder carries no duplicate values
  const values = menu.map((o) => o.value);
  assert.equal(new Set(values).size, values.length, "no duplicate rows in the ladder");
});

test("boardCount: the whole board at rest, the match tally while filtering", () => {
  assert.equal(boardCount(12, 12, false), "12 cards");
  assert.equal(boardCount(1, 1, false), "1 card");
  assert.equal(boardCount(0, 0, false), "0 cards");
  assert.equal(boardCount(3, 12, true), "3/12", "a live filter reports the matches");
  assert.equal(boardCount(0, 12, true), "0/12");
});

// -- the make-over's search seat (0.17.3): one input, two scopes --------

test("filterTasks: case-insensitive over title, body, assignee and id", () => {
  const rows = [
    task({ id: "t_aaa", title: "Fix the composer", body: "enter sends", assignee: "vera" }),
    task({ id: "t_bbb", title: "Board rail", body: "empty lane opens", assignee: "cody" }),
  ];
  assert.equal(filterTasks(rows, "").length, 2, "an empty query is every card");
  assert.equal(filterTasks(rows, "   ").length, 2, "whitespace is still empty");
  assert.deepEqual(filterTasks(rows, "COMPOSER").map((t) => t.id), ["t_aaa"], "title, case-insensitive");
  assert.deepEqual(filterTasks(rows, "empty lane").map((t) => t.id), ["t_bbb"], "body");
  assert.deepEqual(filterTasks(rows, "cody").map((t) => t.id), ["t_bbb"], "assignee");
  assert.deepEqual(filterTasks(rows, "t_aaa").map((t) => t.id), ["t_aaa"], "id");
  assert.equal(filterTasks(rows, "nothing matches this").length, 0);
});

test("tasksPath: the board read vs the all-boards read", () => {
  assert.equal(tasksPath("alpha", "board"), "/tasks?status=all&board=alpha");
  assert.equal(tasksPath("", "board"), "/tasks?status=all");
  assert.equal(tasksPath("alpha", "all"), "/tasks?status=all&scope=all", "the all-boards scope ignores the selection");
});

test("boardOf: a mixed row names its own board; a scoped row falls back to the view", () => {
  assert.equal(boardOf(task({ board: "alpha" }), "main"), "alpha");
  assert.equal(boardOf(task({ board: "" }), "main"), "main");
  assert.equal(boardOf(task({}), "main"), "main");
});

test("board view carries the search seat: one input, a scope chip that rides the query", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /class="board-search"/, "the filter seat");
  assert.match(boardVue, /aria-label="filter cards"/, "the seat is labelled");
  assert.match(boardVue, /@click="toggleScope"/, "the scope chip flips the scope");
  assert.match(boardVue, /scope === "all" \? "all boards" : "this board"/, "the chip says which scope is live");
  // the scope UI is GATED on the query.
  assert.match(boardVue, /v-if="filtering"[\s\S]{0,120}board-scope-chip/, "the scope chip renders only while a query runs");
  assert.match(boardVue, /const filtering = computed\(\(\) => query\.value\.trim\(\) !== ""\)/, "one predicate for the chip + the count");
  assert.match(boardVue, /watch\(query,/, "clearing the seat returns the read to this board");
  assert.match(boardVue, /const visibleTasks = computed\(\(\) => filterTasks\(tasks\.value, query\.value\)\)/, "the grid renders the filtered list");
  assert.match(boardVue, /const cols = computed\(\(\) => columns\(visibleTasks\.value\)\)/, "columns bucket the filtered list");
  assert.match(boardVue, /v-if="scope === 'all' && t\.board"/, "a mixed row shows which board it came from");
  assert.match(boardVue, /v-if="scope !== 'all' && step\(BOARD_STATUSES\[i\], -1\)"/, "no move arrows in the mixed scope");
  assert.match(boardVue, /if \(scope\.value === "all"\) return;/, "no drag in the mixed scope");
  // every drawer call addresses the card's OWN board
  assert.match(boardVue, /const board = boardOf\(selected\.value, boardName\.value\)/, "the drawer reads/writes the card's board");
});

// -- the assignee combobox (0.17.3 make-over): the agents registry ------

test("fetchAgentOptions: reads GET /agents, skips nameless rows, keeps the roster hints", async () => {
  const stub = (rows: unknown) =>
    (async (url: string) => {
      assert.equal(url, "/agents", "one source: the agents registry");
      return { ok: true, json: async () => rows } as unknown as Response;
    }) as (u: string) => Promise<Response>;
  const opts = await fetchAgentOptions(
    stub([
      { name: "default", description: "the main agent", continuous: false },
      { name: "vera", continuous: true },
      { name: "  ", description: "nameless" },
      { description: "no name at all" },
    ]),
  );
  assert.deepEqual(opts.map((o: AgentOption) => o.value), ["default", "vera"], "only named rows");
  assert.equal(opts[0].hint, "the main agent", "the description rides as the hint");
  assert.equal(opts[1].hint, "agent home", "a continuous home with no description says so");
  // a failed GET is an error the view degrades on - never a fabricated list
  await assert.rejects(
    () =>
      fetchAgentOptions(
        (async () => ({ ok: false, status: 500, json: async () => [] }) as unknown as Response) as (
          u: string,
        ) => Promise<Response>,
      ),
    /GET \/agents: 500/,
  );
});

test("board view carries the assignee combobox: registry-fed list, typing stays legal", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /fetchAgentOptions\(fetch\)/, "the create form reads the agents registry");
  assert.match(boardVue, /role="combobox"/, "the assignee field is a combobox");
  assert.match(boardVue, /role="listbox"/, "with a real listbox");
  assert.match(boardVue, /@mousedown\.prevent="pickAssignee\(o\.value\)"/, "a row picks without stealing focus");
  // typing stays legal: the input is BOUND to the free-text value, and a registry.
  assert.match(boardVue, /v-model="createAssignee"/, "the field's value IS what the user typed");
  assert.match(boardVue, /typing a name still works/, "an empty registry says typing still works");
  assert.doesNotMatch(boardVue, /disabled="[^"]*assignee/, "the assignee field is never disabled by the source");
});

// -- the band's lead seat (0.17.3), re-settled to the family contract.

test("board band: title + chip + count | filter - the cron/agents family shape", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const band = readFileSync(join(uiRoot, "app", "components", "ui", "TabBand.vue"), "utf8");
  assert.match(band, /<slot name="lead"/, "the shared band grew the lead seat");
  assert.match(band, /class="tab-band-lead"/, "and its container");

  const bandAt = boardVue.indexOf('<TabBand title="Kanban">');
  const bandEndAt = boardVue.indexOf("</TabBand>");
  const leadAt = boardVue.indexOf("<template #lead>");
  assert.ok(bandAt >= 0 && leadAt > bandAt && leadAt < bandEndAt, "the board fills the lead seat");
  const chipAt = boardVue.indexOf('<BandSelect', leadAt);
  const countAt = boardVue.indexOf('class="board-switch-count"', leadAt);
  assert.ok(chipAt > leadAt && chipAt < bandEndAt, "the board selector is inside the lead seat");
  assert.ok(countAt > chipAt, "the count follows the chip (name, then how many)");

  // the actions slot: the filter seat and nothing else persistent.
  const actionsAt = boardVue.indexOf('class="board-search"');
  assert.ok(actionsAt > leadAt && actionsAt < bandEndAt, "the filter is the band's only action seat");
  for (const gone of [
    "board-rename-btn",
    "board-remove-btn",
    "board-scope-btn",
    "board-switch-label",
  ]) {
    assert.ok(
      !boardVue.slice(bandAt, bandEndAt).includes(gone),
      `no permanent "${gone}" control in the band (card t_2b644603)`,
    );
  }
  // ..
  assert.match(boardVue, /boardMenuOptions\(boards\.value, confirmingRemove\.value\)/, "the ladder carries the verbs + the armed state");
  assert.match(boardVue, /if \(want\.kind === "rename"\) \{[\s\S]{0,40}startRename\(\)/, "the rename row starts the inline rename");
  assert.match(boardVue, /if \(want\.kind === "remove"\) \{[\s\S]{0,40}removeBoardClick\(\)/, "the remove row runs the armed confirm");
  assert.match(boardVue, /\{\{ countLabel \}\}/, "the lead count renders the computed label");
});

test("reorderAfter: slots a card after a sibling inside the same column", () => {
  const list = [task({ id: "t_a", title: "a" }), task({ id: "t_b", title: "b" }), task({ id: "t_c", title: "c" })];
  const moved = reorderAfter(list, "t_c", "triage", "t_a");
  if (!moved) throw new Error("reorderAfter returned null");
  assert.deepEqual(moved.map((t) => t.title), ["a", "c", "b"]);
  assert.equal(moved[1].id, "t_c");
  assert.equal(moved[1].status, "triage");
  assert.equal(list[0].title, "a");
  assert.equal(list.length, 3);
});

test("reorderAfter: crossing a column stamps the new status at the drop slot", () => {
  const list = [
    task({ id: "t_a" }),
    task({ id: "t_b" }),
    task({ id: "t_r", status: "running" }),
  ];
  const moved = reorderAfter(list, "t_b", "running", "t_r");
  if (!moved) throw new Error("reorderAfter returned null");
  const running = moved.filter((t) => t.status === "running");
  assert.deepEqual(running.map((t) => t.id), ["t_r", "t_b"]);
  assert.equal(moved[0].id, "t_a");
});

test("reorderAfter: empty afterId drops at the top, unknown id returns null", () => {
  const list = [task({ id: "t_a" }), task({ id: "t_b" })];
  const moved = reorderAfter(list, "t_b", "todo", "");
  if (!moved) throw new Error("reorderAfter returned null");
  assert.deepEqual(moved.map((t) => t.id), ["t_b", "t_a"]);
  assert.equal(reorderAfter(list, "t_zzz", "todo", ""), null);
});

test("board view drives drag reorder through the pointer + the server slot", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /@pointerdown="onCardPointerDown\(/, "cards take pointer drags");
  assert.match(boardVue, /DRAG_THRESHOLD_PX/, "a drag must clear a threshold to stay a click or a drag");
  assert.match(boardVue, /reorderAfter\(tasks\.value/, "the live view reorders while dragging");
  assert.match(boardVue, /after_id: d\.afterId/, "the drop PATCHes the same slot the view rendered");
  assert.match(boardVue, /status: d\.status/, "a crossed column rides the same PATCH");
  assert.match(boardVue, /elementsFromPoint/, "the drop target survives the dragged card under the cursor");
  assert.match(boardVue, /if \(drag\.value && drag\.value\.moved\) return/, "a live drag still refuses an incoming refresh (the paused-poll rule under the push shape)");
  assert.match(boardVue, /suppressNextClick/, "the click trailing a real drag is suppressed");
  assert.match(boardVue, /closest\("button, input"\)/, "the card's own buttons never start a drag");
});

// -- the mounted /boards fetch retry.

function boardsResponse(boards: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => ({ boards }),
  } as unknown as Response;
}

test("boards retry: a fetch that fails once then answers still populates", async () => {
  let calls = 0;
  const sleeps: number[] = [];
  const fetchImpl = async () => {
    calls++;
    if (calls === 1) throw new TypeError("Failed to fetch");
    return boardsResponse([{ name: "main", current: true }, { name: "j98", current: false }]);
  };
  const boards = await fetchBoardsRetry(fetchImpl, {
    sleep: async (ms) => {
      sleeps.push(ms);
    },
  });
  assert.equal(calls, 2, "one miss, one retry - no third attempt");
  assert.ok(boards, "the retry lands the registry");
  assert.deepEqual(boards, [{ name: "main", current: true }, { name: "j98", current: false }]);
  assert.deepEqual(sleeps, [BOARD_FETCH_BACKOFF_MS], "one backoff between the two attempts");
});

test("boards retry: every attempt missing degrades silently", async () => {
  let calls = 0;
  const fetchImpl = async () => {
    calls++;
    throw new TypeError("Failed to fetch");
  };
  const boards = await fetchBoardsRetry(fetchImpl, { attempts: 3, sleep: async () => {} });
  assert.equal(calls, BOARD_FETCH_ATTEMPTS, "bounded - it stops after the last attempt");
  assert.equal(boards, null, "final miss keeps the silent board-still-renders behavior");
});

test("boards retry: 404 is not retried (single-board installs never heal)", async () => {
  let calls = 0;
  const fetchImpl = async () => {
    calls++;
    return boardsResponse(undefined, 404);
  };
  const boards = await fetchBoardsRetry(fetchImpl, { sleep: async () => {} });
  assert.equal(calls, 1, "the capability-absent answer exits on the first miss");
  assert.equal(boards, null);
});

// -- round: the CRUD trio (create/edit/delete/comment) + the event trail, all. Source
// pins (these are wiring contracts the board must carry) + unit tests for the pure
// helpers.

test("boardLogic: priority helpers", () => {
  assert.equal(hasPriorityChip(0), false, "normal priority renders no chip");
  assert.equal(hasPriorityChip(2), true);
  assert.equal(priorityLabel(0), "P0");
  assert.equal(priorityLabel(3), "P3");
  const opts = priorityOptions();
  assert.deepEqual(opts.map((o) => o.value), [0, 1, 2, 3]);
  assert.equal(opts[0].label, "P0 (normal)");
  assert.equal(taskDeletePath("t_a/b"), "/tasks/t_a%2Fb");
});

test("boardLogic: bodyPreview takes the first non-empty line, truncated", () => {
  assert.equal(bodyPreview(""), "");
  assert.equal(bodyPreview("\n   \n"), "");
  assert.equal(bodyPreview("\n second is first"), "second is first");
  const long = "x".repeat(120);
  assert.equal(bodyPreview(long).endsWith("…"), true);
  assert.ok(bodyPreview(long).length <= 81);
});

test("CRUD: the board builds cards from a full form, edits in the drawer, deletes from the drawer", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const drawerVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "DetailDrawer.vue"), "utf8");
  // create: full form POSTing title/body/assignee/priority with the column preset
  assert.match(boardVue, /@submit\.prevent="submitCreate"/, "a column hosts the create form");
  assert.match(boardVue, /toggleCreate\(BOARD_STATUSES\[i\]\)/, "every column grows cards, not just triage/todo");
  assert.match(boardVue, /title,\n\s+body: createBody\.value,/, "create POSTs the body");
  assert.match(boardVue, /priority: createPriority\.value,/, "create POSTs the priority");
  assert.match(boardVue, /status,/, "the host column is the preset status");
  // edit: the drawer's save PATCHes; the board refetches both views
  assert.match(drawerVue, /\(e: "save", patch: \{ title\?: string; body\?: string; priority\?: number \}\)/, "the drawer reports save patches");
  assert.match(boardVue, /@save="onDrawerSave"/, "the board wires the drawer's saves");
  assert.match(boardVue, /await loadDetail\(id, board\)/, "a save refetches the drawer view on the card's own board");
  // events: the GET payload's events reach the drawer as a prop
  assert.match(boardVue, /:events="detailEvents"/);
  assert.match(boardVue, /detailEvents\.value = d\.events \?\? \[\]/, "the detail payload's events land in the drawer");
  assert.match(drawerVue, /events: BoardEvent\[\]/);
  assert.match(drawerVue, /class="event-list"/);
});

// -- card delete: the wire tool kanban.delete (and its REST face, DELETE /tasks/{id}).
// The delete now rides the drawer's own danger zone, never the board canvas (no stray
// click on the grid), the confirm NAMES the card (id + title), and the board drops the
// row optimistically.
test("delete: drawer-level control, a confirm that names the card, optimistic removal, no undo", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const drawerVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "DetailDrawer.vue"), "utf8");

  assert.match(drawerVue, /class="drawer-danger"/, "the delete rides the drawer's own danger zone");
  assert.match(drawerVue, /@click="armDelete"/, "the first click arms the confirm");
  assert.match(drawerVue, /@click="confirmDelete"/, "only the confirm emits the delete");
  assert.doesNotMatch(drawerVue, /drawer-discard/, "the delete no longer hides in the body-edit form");
  assert.doesNotMatch(boardVue, /class="[^"]*card-delete/, "no delete control on the card canvas");
  assert.match(
    drawerVue,
    /const deleteConfirmLabel = computed\(\s*\(\) => `delete \$\{props\.task\.id\} - \$\{props\.task\.title\}\?`/,
    "the confirm names the card's id AND title",
  );
  assert.match(drawerVue, /class="drawer-confirm-text">\{\{ deleteConfirmLabel \}\}/, "the named confirm is rendered");
  // the emit is gated: emit("delete") appears only inside confirmDelete
  assert.equal(
    (drawerVue.match(/emit\("delete"\)/g) ?? []).length,
    1,
    "exactly one delete emit, in the confirm handler",
  );
  assert.match(drawerVue, /function confirmDelete\(\) \{\s*disarmDelete\(\);\s*emit\("delete"\);/, "the confirm disarms, then emits");
  assert.match(drawerVue, /There is deliberately NO undo/, "the no-undo ruling is stated where the control lives");
  assert.match(drawerVue, /cannot resurrect a deleted card/, "and names WHY undo cannot exist");
  assert.doesNotMatch(drawerVue, /undo-|showUndo|emit\("undo"\)/i, "no half-built undo affordance");

  // the wire: DELETE through the capability route, card leaves the list first
  assert.match(boardVue, /@delete="onDrawerDelete"/, "the board wires the drawer's delete");
  const deleteFn = boardVue.slice(boardVue.indexOf("async function onDrawerDelete"));
  assert.ok(deleteFn.length > 0, "the board has the delete handler");
  const optimisticAt = deleteFn.indexOf("tasks.value = tasks.value.filter((t) => t.id !== id)");
  const requestAt = deleteFn.indexOf('method: "DELETE"');
  assert.ok(optimisticAt >= 0, "the confirmed card is dropped from the list");
  assert.ok(requestAt > optimisticAt, "the row leaves the grid before the round trip (optimistic)");
  assert.match(deleteFn, /await fetchBoard\(\);\s*return;/, "a refused DELETE snaps the board back to the server's truth");
});

// -- the push shape: the 5s poll is dead, the shell's canvas delta event keeps.

test("board renders from the mount fetch + the shell's canvas delta event, never a poll", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.doesNotMatch(boardVue, /setInterval/, "the fetching timer is gone");
  assert.doesNotMatch(boardVue, /POLLS_MS/, "the poll cadence is gone with it");
  assert.match(boardVue, /const CANVAS_DELTA_EVENT = "omega-canvas-delta"/, "the event literal is duplicated on purpose (module UI imports no shell code)");
  assert.match(boardVue, /addEventListener\(CANVAS_DELTA_EVENT, onCanvasDelta\)/, "the board listens for the shell's announcement");
  assert.match(boardVue, /removeEventListener\(CANVAS_DELTA_EVENT, onCanvasDelta\)/, "the listener leaves with the component");
  assert.match(boardVue, /if \(detail\.id === "tasks"\) \{\s*\n\s*scheduleBoardRefetch\(\);/, "a tasks delta queues the board's own REST read");
  assert.match(boardVue, /if \(detail\.id === "boards"\) void refreshRegistry\(\);/, "a boards delta re-reads the registry only");
  assert.match(boardVue, /fetchBoards\(\)\.then\(\(\) => fetchBoard\(\)\)/, "the mount fetch is still the initial render");
  assert.match(boardVue, /const DELTA_COALESCE_MS = \d+/, "a delta burst coalesces into one read");
  assert.match(boardVue, /if \(deltaTimer !== null\) return;/, "an already-queued read absorbs the rest of the burst");
  assert.match(boardVue, /clearTimeout\(deltaTimer\)/, "a queued read leaves with the component");
  // the request epoch: an event-triggered read must never lose to an older one
  assert.match(boardVue, /const seq = \+\+fetchSeq/, "each list read carries its request epoch");
  assert.match(boardVue, /if \(seq !== fetchSeq\) return/, "a late answer never overwrites a newer list");
});

// -- the reconnect reset: the socket replays nothing, so a delta that lands.

test("the board re-reads its whole view on the shell's reconnect reset pulse", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /const CANVAS_RESET_ID = "reconnect"/, "the reset literal is duplicated on purpose");
  assert.match(boardVue, /if \(detail\.id === CANVAS_RESET_ID\) \{\s*\n\s*\/\/[^\n]*\n(?:\s*\/\/[^\n]*\n)*\s*void reloadAll\(\);/, "a reset pulse re-reads the pane");
  assert.match(boardVue, /async function reloadAll\(\) \{\s*\n\s*await refreshRegistry\(\);\s*\n\s*await fetchBoard\(\);/, "the registry is read first, then the list (never a stale boardName)");
  assert.match(boardVue, /if \(drag\.value && drag\.value\.moved\) return/, "the reset obeys the drag guard like every other read");
});

test("the canvas store publishes the reconnect reset on the same event, and only when asked", () => {
  const g = globalThis as unknown as { window?: unknown; CustomEvent?: unknown };
  const priorWindow = g.window;
  const priorCustomEvent = g.CustomEvent;
  const seen: { type?: string; detail?: unknown }[] = [];
  g.CustomEvent = class {
    type: string;
    detail: unknown;
    constructor(type: string, opts?: { detail?: unknown }) {
      this.type = type;
      this.detail = opts?.detail;
    }
  };
  g.window = {
    dispatchEvent: (ev: { type?: string; detail?: unknown }) => {
      seen.push(ev);
      return true;
    },
  };
  try {
    const store = createCanvasStore();
    // the boot path: a snapshot refresh says nothing, so a page load never storms.
    store.applySnapshot([{ id: "tasks", title: "Tasks", kind: "list", payload: { board: "main", total: 0, rows: [] }, actions: [] }]);
    assert.equal(seen.length, 0, "the snapshot (boot + catch-up) announces nothing");
    announceCanvasReset();
    assert.equal(seen.length, 1, "the reset pulse is exactly one announcement");
    assert.equal(seen[0].type, CANVAS_DELTA_EVENT, "the reset rides the ONE canvas event - no second channel");
    assert.equal((seen[0].detail as { id?: string }).id, CANVAS_RESET_ID, "the reset id rides as detail");
    const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
    assert.ok(
      boardVue.includes(`const CANVAS_RESET_ID = "${CANVAS_RESET_ID}"`),
      "the board's duplicated reset literal matches the shell's constant",
    );
  } finally {
    if (priorWindow === undefined) delete g.window;
    else g.window = priorWindow;
    if (priorCustomEvent === undefined) delete g.CustomEvent;
    else g.CustomEvent = priorCustomEvent;
  }
});


test("card polish: the 2-line clamp is ONE constant the CSS renders", () => {
  assert.equal(CARD_CLAMP_LINES, 2, "the ruled clamp is two lines");
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /class="card-title card-clamp">\{\{ t\.title \}\}/, "the title clamps");
  assert.match(boardVue, /class="card-preview card-clamp">\{\{ bodyPreview\(t\.body\) \}\}/, "the body teaser clamps");
  assert.match(boardVue, /\.card-clamp \{\s*\n\s*display: -webkit-box;/, "the clamp is a box clamp");
  assert.match(boardVue, /-webkit-box-orient: vertical;/, "vertical lines");
  assert.match(boardVue, /-webkit-line-clamp: var\(--card-clamp, 2\);/, "the line count reads the custom property");
  assert.match(boardVue, /line-clamp: var\(--card-clamp, 2\);/, "and the standard property agrees");
  assert.match(boardVue, /'--card-clamp': `\$\{CARD_CLAMP_LINES\}`/, "the constant feeds the property");
  assert.doesNotMatch(boardVue, /max-height: 2\.6em/, "the teaser clamps by line, not by an em guess");
});

test("card polish: the assignee is a chip on one Hermes-style meta row", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /class="card-assignee-chip"/, "the assignee renders as a chip");
  assert.match(boardVue, /:title="`assignee: \$\{t\.assignee\}`"/, "the chip names what it is");
  assert.doesNotMatch(boardVue, /class="card-assignee"/, "the old bare text span is gone");
  assert.match(boardVue, /\.card-assignee-chip \{[\s\S]*?border-radius: var\(--r-chip\);/, "the chip wears the shell's chip radius");
  const metaAt = boardVue.indexOf('class="card-meta"');
  assert.ok(metaAt > 0, "the meta row exists");
  const meta = boardVue.slice(metaAt, boardVue.indexOf("</article>", metaAt));
  for (const face of ["card-assignee-chip", "card-heartbeat", "card-age", "card-moves"]) {
    assert.ok(meta.includes(face), `${face} rides the one meta row`);
  }
  // ONE time telling: the running chip replaces the created-age
  assert.match(
    meta,
    /<span\s*\n\s*v-else\s*\n\s*class="card-age">\{\{ age\(t\.created_at\) \}\}/,
    "the age stands down on a card that wears the heartbeat",
  );
});

test("heartbeat chip: a running card counts its working minutes", () => {
  const now = new Date("2026-11-12T12:00:00Z");
  const running = task({ status: "running", started_at: "2026-11-12T11:57:00Z" });
  const chip = heartbeatChip(running, now);
  if (!chip) throw new Error("a running card must carry a chip");
  assert.equal(chip.label, "working - 3m");
  assert.equal(chip.stale, false);
  assert.equal(chip.runningMs, 3 * 60 * 1000);
  assert.equal(minuteLabel(0), "0m");
  assert.equal(minuteLabel(59_999), "0m", "minutes floor - a chip never rounds itself older");
  assert.equal(minuteLabel(125 * 60_000), "125m", "the count keeps growing (no d/h switch - it is a stopwatch)");
  assert.equal(heartbeatChip(task({ status: "running", started_at: "2026-11-12T12:00:00Z" }), now)?.label, "working - 0m");
  for (const status of ["triage", "todo", "scheduled", "ready", "blocked", "review", "done", "archived"]) {
    assert.equal(heartbeatChip(task({ status, started_at: "2026-11-12T11:00:00Z" }), now), null, `${status} has no chip`);
  }
  const noStart = heartbeatChip(task({ status: "running", started_at: "" }), now);
  assert.equal(noStart?.label, "working", "no start renders the bare working face");
  assert.equal(noStart?.runningMs, 0);
  assert.equal(noStart?.stale, false);
});

test("heartbeat chip: stale is an AGED beat, never mere silence", () => {
  const now = new Date("2026-11-12T12:00:00Z");
  const running = (over: Partial<BoardTask>) =>
    task({ status: "running", started_at: "2026-11-12T11:50:00Z", ...over });

  // a fresh beat: working, and the duration is the START clock
  const fresh = heartbeatChip(running({ last_heartbeat_at: "2026-11-12T11:59:30Z" }), now);
  assert.equal(fresh?.stale, false);
  assert.equal(fresh?.label, "working - 10m", "a beat does not reset the working clock");

  // exactly at the threshold is still inside it - the flip is PAST the age
  const edge = new Date(now.getTime() - HEARTBEAT_STALE_MS).toISOString();
  assert.equal(heartbeatChip(running({ last_heartbeat_at: edge }), now)?.stale, false);
  const late = new Date(now.getTime() - HEARTBEAT_STALE_MS - 1000).toISOString();
  const stale = heartbeatChip(running({ last_heartbeat_at: late }), now);
  assert.equal(stale?.stale, true);
  assert.equal(stale?.label, "stale - 2m quiet");
  assert.equal(stale?.runningMs, 10 * 60 * 1000, "the working clock keeps its own count while stale");

  // NO heartbeat field at all is not staleness: silence is not evidence
  assert.equal(heartbeatChip(running({}), now)?.stale, false, "a wire with no heartbeat field never reads stale");
  assert.equal(heartbeatChip(running({ last_heartbeat_at: "not a date" }), now)?.stale, false);
});

test("heartbeat chip: the board ticks the chip on a clock, not a fetch", () => {
  assert.equal(HEARTBEAT_TICK_MS, 5000, "the chip clock is 5s");
  assert.ok(HEARTBEAT_STALE_MS > HEARTBEAT_TICK_MS * 10, "the threshold is many ticks away, not one");
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  // the clock: one re-dated `now`, chained on setTimeout
  assert.match(boardVue, /const now = ref\(new Date\(\)\)/, "the board holds its clock");
  assert.match(boardVue, /now\.value = new Date\(\);/, "a tick re-dates it");
  assert.match(boardVue, /heartbeatTimer = setTimeout\(tickHeartbeat, HEARTBEAT_TICK_MS\)/, "the tick re-arms itself");
  assert.match(boardVue, /clearTimeout\(heartbeatTimer\)/, "and leaves with the component");
  assert.doesNotMatch(boardVue, /setInterval/, "still no interval: the fetching poll stays dead");
  // the tick only re-dates - a timer that fetched would be the old poll back
  const tickAt = boardVue.indexOf("function tickHeartbeat");
  const tickBody = boardVue.slice(tickAt, boardVue.indexOf("const heartbeats", tickAt));
  assert.ok(tickBody.length > 0, "the tick handler exists");
  assert.doesNotMatch(tickBody, /fetch/, "the chip clock never fetches");
  assert.match(boardVue, /const heartbeats = computed\(/, "the chips derive once per render");
  assert.match(boardVue, /const chip = heartbeatChip\(t, at\);/, "through the pure helper");
  assert.match(boardVue, /v-if="heartbeats\[t\.id\]"/, "a card wears a chip only when it has one");
  assert.match(boardVue, /class="card-heartbeat"[\s\S]{0,120}:class="\{ stale: heartbeats\[t\.id\]\.stale \}"/, "the stale state rides the chip class");
  assert.match(boardVue, /\{\{ heartbeats\[t\.id\]\.label \}\}/, "the chip renders the computed face");
  assert.match(boardVue, /\.card-heartbeat\.stale \{\s*\n\s*color: color-mix\(in srgb, #f87171 85%, currentcolor\);/, "stale takes the red tone");
  assert.match(boardVue, /\.card-heartbeat\.stale \.card-heartbeat-dot \{\s*\n\s*animation: none;/, "a dead beat stops pulsing");
});

test("the canvas store announces every applied delta; a snapshot is not an announcement", () => {
  const g = globalThis as unknown as { window?: unknown; CustomEvent?: unknown };
  const priorWindow = g.window;
  const priorCustomEvent = g.CustomEvent;
  const seen: { type?: string; detail?: unknown }[] = [];
  g.CustomEvent = class {
    type: string;
    detail: unknown;
    constructor(type: string, opts?: { detail?: unknown }) {
      this.type = type;
      this.detail = opts?.detail;
    }
  };
  g.window = {
    dispatchEvent: (ev: { type?: string; detail?: unknown }) => {
      seen.push(ev);
      return true;
    },
  };
  try {
    const store = createCanvasStore();
    store.applyDelta({ id: "tasks", title: "Tasks", kind: "list", payload: { board: "main", total: 1, rows: [] }, actions: [] });
    assert.equal(seen.length, 1, "one applied delta = exactly one announcement");
    assert.equal(seen[0].type, CANVAS_DELTA_EVENT);
    assert.equal((seen[0].detail as { id?: string }).id, "tasks", "the panel descriptor rides as detail");
    // the snapshot is the catch-up, and the boot refresh must not storm consumers
    store.applySnapshot([{ id: "tasks", title: "Tasks", kind: "list", payload: { board: "main", total: 0, rows: [] }, actions: [] }]);
    assert.equal(seen.length, 1, "applySnapshot announces nothing");
    // a malformed delta never announces
    store.applyDelta({ nope: true });
    assert.equal(seen.length, 1, "normalize's reject is silent");
  } finally {
    if (priorWindow === undefined) delete g.window;
    else g.window = priorWindow;
    if (priorCustomEvent === undefined) delete g.CustomEvent;
    else g.CustomEvent = priorCustomEvent;
  }
});
