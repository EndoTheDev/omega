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
  age,
  columns,
  isBoardPath,
  isSettled,
  step,
  type BoardTask,
} from "../../kanban/ui/board/boardLogic.ts";

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
  const boardVue = join(uiRoot, "..", "kanban", "ui", "Board.vue");
  const logic = join(uiRoot, "..", "kanban", "ui", "board", "boardLogic.ts");
  const drawer = join(uiRoot, "..", "kanban", "ui", "board", "DetailDrawer.vue");
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
  const kanbanDockable = join(uiRoot, "..", "kanban", "ui", "dockable.json");
  assert.ok(existsSync(kanbanDockable), "modules/kanban/ui/dockable.json missing");
  assert.deepEqual(JSON.parse(readFileSync(kanbanDockable, "utf8")), { title: "Kanban" });
  assert.match(
    readFileSync(join(uiRoot, "app", "composables", "omega.ts"), "utf8"),
    /mainViewModules = new Set<string>\(\["kanban"\]\)/,
  );
});