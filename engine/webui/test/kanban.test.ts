// Module-owned kanban board: logic + composition wiring. The board lives
// in modules/kanban/ui/ and rides the module-UI composition - the shell
// knows only the mount slot. Logic is pure (boardLogic.ts), so these
// tests are plain node:test like everything else in the suite.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

import {
  BOARD_STATUSES,
  BOARD_PATH,
  DEFAULT_BOARD_NAME,
  age,
  boardDeletePath,
  canRemoveBoard,
  columns,
  isBoardPath,
  isSettled,
  pickBootBoard,
  step,
  withBoard,
  type BoardInfo,
  type BoardTask,
} from "../../../modules/kanban/ui/board/boardLogic.ts";

const uiRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function task(over: Partial<BoardTask>): BoardTask {
  return {
    id: "t_testid",
    title: "a task",
    body: "",
    assignee: "",
    status: "triage",
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
  const boardVue = join(uiRoot, "..", "..", "modules", "kanban", "ui", "Board.vue");
  const logic = join(uiRoot, "..", "..", "modules", "kanban", "ui", "board", "boardLogic.ts");
  const drawer = join(uiRoot, "..", "..", "modules", "kanban", "ui", "board", "DetailDrawer.vue");
  assert.ok(existsSync(boardVue), "modules/kanban/ui/Board.vue missing");
  assert.ok(existsSync(logic), "modules/kanban/ui/board/boardLogic.ts missing");
  assert.ok(existsSync(drawer), "modules/kanban/ui/board/DetailDrawer.vue missing");

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
  const kanbanDockable = join(uiRoot, "..", "..", "modules", "kanban", "ui", "dockable.json");
  assert.ok(existsSync(kanbanDockable), "modules/kanban/ui/dockable.json missing");
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
  const boardVue = readFileSync(join(uiRoot, "..", "..", "modules", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /id="board-switcher"/, "the dropdown exists");
  assert.match(boardVue, /fetch\(withBoard\("\/tasks", boardName\.value\)\)/, "list calls carry ?board=");
  assert.match(boardVue, /fetchBoards\(\)/, "boot reads the registry");
  assert.match(boardVue, /selected\.value = null/, "the drawer resets on board switch");
  // every task fetch goes through withBoard - no raw /task path left behind
  const rawTaskFetches = boardVue.match(/fetch\(`?\/tasks/g) ?? [];
  assert.equal(rawTaskFetches.length, 0, "all fetches must be withBoard-wrapped");
  // the drawer (inspector) has no self-contained fetch: it receives props
  const drawer = readFileSync(join(uiRoot, "..", "..", "modules", "kanban", "ui", "board", "DetailDrawer.vue"), "utf8");
  assert.doesNotMatch(drawer, /fetch\(/, "the inspector follows the switch via props");
});

test("board head keeps the fillet-tuck top spacing", () => {
  // the dock tab strip tucks -var(--r-pane) (8px) over the pane content,
  // so an 8px spacer above the switcher is fully consumed - the select
  // touches the strip. 10px matches the sibling inspector panels
  // (settings.narrow / insights both carry 10px top padding).
  const boardVue = readFileSync(join(uiRoot, "..", "..", "modules", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /\.board-head\s*\{[^}]*padding:\s*10px 12px 0/s,
    "head keeps the 10px top spacing that survives the 8px tuck");
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
  const boardVue = readFileSync(join(uiRoot, "..", "..", "modules", "kanban", "ui", "Board.vue"), "utf8");
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
