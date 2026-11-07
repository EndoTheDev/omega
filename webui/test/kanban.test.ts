// Module-owned kanban board: logic + composition wiring. The board lives
// in harness/kanban/ui/ and rides the module-UI composition - the shell
// knows only the mount slot. Logic is pure (boardLogic.ts), so these
// tests are plain node:test like everything else in the suite.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

import {
  BOARD_FETCH_ATTEMPTS,
  BOARD_FETCH_BACKOFF_MS,
  BOARD_STATUSES,
  BOARD_PATH,
  DEFAULT_BOARD_NAME,
  age,
  bodyPreview,
  boardDeletePath,
  canRemoveBoard,
  columns,
  fetchBoardsRetry,
  hasPriorityChip,
  isBoardPath,
  isSettled,
  pickBootBoard,
  priorityLabel,
  priorityOptions,
  reorderAfter,
  step,
  taskDeletePath,
  withBoard,
  type BoardInfo,
  type BoardTask,
} from "../../harness/kanban/ui/board/boardLogic.ts";

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

test("isSettled + isBoardPath", () => {
  assert.ok(isSettled("done"));
  assert.ok(isSettled("archived"));
  assert.ok(!isSettled("review"));
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

// --- structural: the shell mounts the board through the composition

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
  // /board anchors in history like /chat/<id>
  assert.match(appVue, /\/board/);

  // card t_49d0b308: the board JOINED the inspector - "kanban" is a real
  // dock tab now (pinned right, unclosable), rendered through the
  // composition mount. The main-pane board view keeps its URL anchor.
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
// --- board switcher (card t_d7a7beaa): named boards, pointer boot, URL wiring

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
  assert.match(boardVue, /fetch\(withBoard\("\/tasks\?status=all", boardName\.value\)\)/, "list calls carry ?board= (status=all: the board grid must see the archived column)");
  assert.match(boardVue, /fetchBoards\(\)/, "boot reads the registry");
  assert.match(boardVue, /selected\.value = null/, "the drawer resets on board switch");
  // every task fetch goes through withBoard - no raw /task path left behind
  const rawTaskFetches = boardVue.match(/fetch\(`?\/tasks/g) ?? [];
  assert.equal(rawTaskFetches.length, 0, "all fetches must be withBoard-wrapped");
  // the drawer (inspector) has no self-contained fetch: it receives props
  const drawer = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "DetailDrawer.vue"), "utf8");
  assert.doesNotMatch(drawer, /fetch\(/, "the inspector follows the switch via props");
});

test("board head keeps the fillet-tuck top spacing", () => {
  // card t_308e6d9b: the head row is the shared TabBand now - the band
  // carries the 14px lead-in that survives the 8px tab-strip tuck (the
  // Files-band contract); the switcher rides the band's actions.
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const band = readFileSync(join(uiRoot, "app", "components", "ui", "TabBand.vue"), "utf8");
  assert.match(boardVue, /<TabBand title="Kanban">/, "the kanban head is the universal band");
  assert.ok(boardVue.indexOf("<TabBand") < boardVue.indexOf("board-scroll"), "the band renders ABOVE the scroll columns");
  assert.doesNotMatch(band, /position: sticky/, "non-scrolling is STRUCTURE, not CSS sticky");
  assert.doesNotMatch(boardVue, /padding:\s*8px 12px 0/,
    "the tuck-consumed 8px spacer stays out");
});

// --- board removal (card t_95c5527f): guarded delete beside the switcher

test("canRemoveBoard + boardDeletePath: the default board never carries the control", () => {
  assert.equal(DEFAULT_BOARD_NAME, "main");
  assert.ok(canRemoveBoard("alpha"), "named boards are removable");
  assert.ok(!canRemoveBoard("main"), "the default board is never removable");
  assert.ok(!canRemoveBoard(""), "the empty registry selection is not removable");
  assert.equal(boardDeletePath("alpha"), "/boards/alpha");
  assert.equal(boardDeletePath("my_2nd"), "/boards/my_2nd");
  // names are path-safe payloads: encoded, never raw
  assert.equal(boardDeletePath("a b"), "/boards/a%20b");
});

test("board view carries the remove control: armed confirm, DELETE route, notice, default fallback", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /id="board-remove-btn"/, "the remove button exists beside the switcher");
  assert.match(
    boardVue,
    /:disabled="!canRemoveBoard\(boardName\)"/,
    "the control keys off the removable name (default board disabled)",
  );
  assert.match(boardVue, /confirm remove/, "the armed click relabels: inline confirm row");
  assert.doesNotMatch(boardVue, /window\.confirm/, "no native confirm - the panel confirms inline");
  assert.match(boardVue, /method: "DELETE"/, "the removal fires the DELETE route");
  assert.match(boardVue, /boardDeletePath\(name\)/, "the delete goes to the encoded registry path");
  assert.match(boardVue, /board-notice/, "a visible notice reports the removal or refusal");
  assert.match(boardVue, /DEFAULT_BOARD_NAME/, "selection falls back to the default board");
  assert.match(boardVue, /disarmRemove/, "board switches disarm an armed confirm");
});

// --- drag-and-drop reorder (card t_09b44af1)

test("reorderAfter: slots a card after a sibling inside the same column", () => {
  const list = [task({ id: "t_a", title: "a" }), task({ id: "t_b", title: "b" }), task({ id: "t_c", title: "c" })];
  // c comes after a: [a, c, b]
  const moved = reorderAfter(list, "t_c", "triage", "t_a");
  if (!moved) throw new Error("reorderAfter returned null");
  assert.deepEqual(moved.map((t) => t.title), ["a", "c", "b"]);
  // c keeps its fields, its status rides
  assert.equal(moved[1].id, "t_c");
  assert.equal(moved[1].status, "triage");
  // the input list is untouched (pure)
  assert.equal(list[0].title, "a");
  assert.equal(list.length, 3);
});

test("reorderAfter: crossing a column stamps the new status at the drop slot", () => {
  const list = [
    task({ id: "t_a" }),
    task({ id: "t_b" }),
    task({ id: "t_r", status: "running" }),
  ];
  // b drops into running after the card already there: [a, r(=b running), b-status...]
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
  assert.match(boardVue, /if \(drag\.value && drag\.value\.moved\) return/, "the poll pauses while a drag is live");
  assert.match(boardVue, /suppressNextClick/, "the click trailing a real drag is suppressed");
  assert.match(boardVue, /closest\("button, input"\)/, "the card's own buttons never start a drag");
});

// --- the mounted /boards fetch retry (nightlies 42268953671 and
// 42260775149, cards t_7cdac186 / t_82e5d149): the switcher chip gates
// on `boards.length > 1 || boardName`, so a fetch that misses under
// suite load left no chip for the whole page life. The retry is pure
// logic in boardLogic - the fetch is stubbed, the sleep is injectable.

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

// --- round t_f81e845e: the CRUD trio (create/edit/delete/comment) + the
// event trail, all board-native. Source pins (these are wiring contracts
// the board must carry) + unit tests for the pure helpers.

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

test("CRUD: the board builds cards from a full form, edits in the drawer, deletes armed", () => {
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
  assert.match(boardVue, /await loadDetail\(id\)/, "a save refetches the drawer view");
  // delete: armed second click, DELETE through the capability route
  assert.match(drawerVue, /confirmingDelete/, "the card delete is an armed two-click");
  assert.match(boardVue, /@delete="onDrawerDelete"/);
  assert.match(boardVue, /method: "DELETE"/);
  // events: the GET payload's events reach the drawer as a prop
  assert.match(boardVue, /:events="detailEvents"/);
  assert.match(boardVue, /detailEvents\.value = d\.events \?\? \[\]/, "the detail payload's events land in the drawer");
  assert.match(drawerVue, /events: BoardEvent\[\]/);
  assert.match(drawerVue, /class="event-list"/);
});
