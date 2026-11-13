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
  CARD_CLAMP_LINES,
  DEFAULT_BOARD_NAME,
  HEARTBEAT_STALE_MS,
  HEARTBEAT_TICK_MS,
  LANE_EASE_MS,
  LANE_RAIL_PX,
  LANE_TONES,
  age,
  bodyPreview,
  boardDeletePath,
  canRemoveBoard,
  columns,
  fetchBoardsRetry,
  hasPriorityChip,
  heartbeatChip,
  isBoardPath,
  laneFolded,
  laneTone,
  minuteLabel,
  pickBootBoard,
  priorityLabel,
  priorityOptions,
  reconcileLanePins,
  reorderAfter,
  step,
  taskDeletePath,
  toggleLanePin,
  withBoard,
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

// --- lane rails (card t_50a8c145): the Hermes lane-collapse model.
// ANY lane folds to a thin vertical rail, an EMPTY lane folds itself, and
// the fold eases over ~180ms. The old isSettled-only fold (done/archived,
// the "N folded" note) is superseded and gone - these are the pins that
// keep it gone.

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
  // done/archived carry no special fold state - the settled special case is dead
  assert.equal(laneFolded("done", 2, {}), laneFolded("triage", 2, {}));
  assert.equal(laneFolded("archived", 2, {}), laneFolded("review", 2, {}));
});

test("lane rails: the auto-collapse law, and an override dies with its lane", () => {
  // an EMPTY lane collapses to a rail - no pin can hold it open
  assert.equal(laneFolded("triage", 0, {}), true);
  assert.equal(laneFolded("triage", 0, { triage: "open" }), true, "reality outranks a pin");
  assert.equal(laneFolded("blocked", 0, { blocked: "collapsed" }), true);

  // a pin holds while the lane holds cards - both directions
  assert.equal(laneFolded("review", 1, { review: "open" }), false);
  assert.equal(laneFolded("review", 1, { review: "collapsed" }), true);

  // the drop: only a DRAINED lane loses its override
  const pins: LanePins = { review: "open", triage: "collapsed" };
  const kept = reconcileLanePins({ review: 0, triage: 2 }, pins);
  assert.equal("review" in kept, false, "the drained lane's override is dropped");
  assert.equal(kept.triage, "collapsed", "a lane still holding cards keeps its pin");
  assert.equal(pins.review, "open", "the input map is untouched (pure)");
  // nothing drained: the very same map comes back (no needless churn)
  assert.equal(reconcileLanePins({ review: 1, triage: 1 }, pins), pins);

  // the whole law in one round trip: pinned open -> drains (override
  // dropped, rail) -> refills (back to its DEFAULT, not the dead pin)
  let lanes: LanePins = { review: "open" };
  assert.equal(laneFolded("review", 2, lanes), false);
  lanes = reconcileLanePins({ review: 0 }, lanes);
  assert.equal(laneFolded("review", 0, lanes), true, "a drained lane is a rail even when it was pinned open");
  assert.equal(laneFolded("review", 2, lanes), false, "a refilled lane comes back expanded - the pin did not survive the drain");

  // toggling writes the opposite of the state that stands
  const folded = toggleLanePin("todo", 2, {});
  assert.equal(folded.todo, "collapsed", "a column folds");
  assert.equal(laneFolded("todo", 2, folded), true);
  const unfolded = toggleLanePin("todo", 2, folded);
  assert.equal(unfolded.todo, "open", "a rail unfolds");
  assert.equal(laneFolded("todo", 2, unfolded), false);
  // an empty lane cannot be opened - the click is a no-op, the rail stays
  const empty: LanePins = { ready: "open" };
  assert.equal(toggleLanePin("ready", 0, empty), empty);
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
  // every lane has a tone, and every tone is a distinct-looking hex
  for (const status of BOARD_STATUSES) assert.match(laneTone(status), /^#[0-9a-f]{6}$/);
  assert.equal(laneTone("scheduled"), "#a78bfa", "violet, per the ruling");
  assert.equal(laneTone("running"), "#34d399", "green, per the ruling");
  assert.equal(laneTone("bogus"), laneTone("triage"), "an unknown status degrades to the triage tone");
});

test("lane rails: the rail renders the dot, the name and the count", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  // the rail is one element: dot row, vertical name, count-if-nonzero
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
  // the vertical name
  assert.match(boardVue, /\.lane-rail-name \{\s*\n\s*writing-mode: vertical-rl;/, "the rail name is vertical");
  assert.match(boardVue, /text-transform: uppercase;/, "and uppercase");
  // the dot row sits at the EXPANDED header row's height - that is the
  // alignment contract across folded and unfolded lanes
  assert.match(boardVue, /\.lane-dot-row \{[\s\S]*?height: var\(--lane-head-h/, "the rail's dot row is a header row tall");
  assert.match(boardVue, /\.col-head \{[\s\S]*?height: var\(--lane-head-h/, "the header row is that same height");
  assert.match(boardVue, /--lane-head-h: 24px;/, "one constant drives both");
  // the rail is a button (click unfolds)...
  assert.match(boardVue, /class="lane-rail"[\s\S]{0,400}@click="toggleLane\(BOARD_STATUSES\[i\], col\.length\)"/, "the rail is the unfold control");
  assert.match(boardVue, /:aria-label="`unfold \$\{BOARD_STATUSES\[i\]\} lane`"/, "and names what it does");
  // ...AND a drop target: the rail lives inside the same .board-col the
  // drag machinery resolves, so a drop on the rail lands in that lane.
  assert.match(boardVue, /closest\("\.board-col"\)/, "drops resolve the lane element");
  const railAt = boardVue.indexOf('class="lane-rail"');
  const colAt = boardVue.indexOf(':data-status="BOARD_STATUSES[i]"');
  const drawerAt = boardVue.indexOf("<DetailDrawer");
  assert.ok(colAt >= 0 && colAt < railAt && railAt < drawerAt, "the rail renders INSIDE the .board-col[data-status] lane (its own drop target)");
});

test("lane rails: every lane carries the fold control and no settled branch survives", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const logic = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "boardLogic.ts"), "utf8");
  // the fold control is unconditional: no v-if on the button itself
  assert.match(boardVue, /class="col-fold-btn"\s*\n\s*:aria-expanded="true"/, "the header fold control renders on every lane");
  assert.doesNotMatch(boardVue, /isSettled/, "the settled-only branch is gone from the view");
  assert.doesNotMatch(logic, /isSettled/, "and from the logic module");
  assert.doesNotMatch(boardVue, /col-folded-note/, "the 'N folded' note died with the settled fold");
  assert.doesNotMatch(boardVue, /settled:/, "no settled class rides the lane");
  // one fold state, two shapes: the rail and the column are the same element
  assert.match(boardVue, /:class="\{ folded: isLaneFolded\(BOARD_STATUSES\[i\], col\.length\) \}"/, "the lane's folded state is the one fold law");
  // auto-collapse is wired, not re-implemented in the view
  assert.match(boardVue, /lanePins\.value = reconcileLanePins\(counts, lanePins\.value\)/, "every list read reconciles the pins");
  assert.match(boardVue, /\{ immediate: true \}/, "including the first render");
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
  // a live drag freezes the lane shapes: the optimistic reorder must never
  // re-lay-out the strip under the cursor (a rail that unfolds under the
  // pointer moves the drop target - measured live before the freeze)
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
  // the width ease: flex-basis over the ruled duration, on a cubic-bezier
  assert.match(boardVue, /transition:\s*\n\s*flex-basis var\(--lane-ease, 180ms\) cubic-bezier\(/, "the fold animates the lane's width on an ease");
  assert.match(boardVue, /'--lane-ease': `\$\{LANE_EASE_MS\}ms`/, "the duration comes from the constant");
  assert.match(boardVue, /'--lane-rail-width': `\$\{LANE_RAIL_PX\}px`/, "and so does the rail width");
  assert.match(boardVue, /\.board-col\.folded \{\s*\n\s*flex: 0 0 var\(--lane-rail-width, 36px\);/, "a folded lane is the thin strip");
  // the narrow-stack mode keeps today's behavior: no rails there
  assert.match(boardVue, /function isLaneFolded\(status: string, count: number\): boolean \{\s*\n\s*if \(narrow\.value\) return false;/, "the narrow stack renders every lane expanded");
  assert.match(boardVue, /function toggleLane\(status: string, count: number\) \{\s*\n\s*if \(narrow\.value\) return;/, "and refuses the fold gesture");
  assert.match(boardVue, /:disabled="narrow"/, "the fold control is disabled there, not a dead click");
  assert.match(boardVue, /\.board\.narrow \.board-scroll \{/, "the narrow layout is untouched");
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
  assert.match(boardVue, /if \(drag\.value && drag\.value\.moved\) return/, "a live drag still refuses an incoming refresh (the paused-poll rule under the push shape)");
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
  assert.match(boardVue, /await loadDetail\(id\)/, "a save refetches the drawer view");
  // events: the GET payload's events reach the drawer as a prop
  assert.match(boardVue, /:events="detailEvents"/);
  assert.match(boardVue, /detailEvents\.value = d\.events \?\? \[\]/, "the detail payload's events land in the drawer");
  assert.match(drawerVue, /events: BoardEvent\[\]/);
  assert.match(drawerVue, /class="event-list"/);
});

// --- card delete (card t_5a638160): the wire tool kanban.delete (and its
// REST face, DELETE /tasks/{id}) existed while the UI affordance was buried
// inside the body editor - you had to click into the body to even see it.
// The delete now rides the drawer's own danger zone, never the board canvas
// (no stray click on the grid), the confirm NAMES the card (id + title), and
// the board drops the row optimistically. There is NO undo: undoing a delete
// would need card resurrection the tool layer does not have, so the confirm
// is the safety - stated in the code so nobody adds a half undo later.
test("delete: drawer-level control, a confirm that names the card, optimistic removal, no undo", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  const drawerVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "board", "DetailDrawer.vue"), "utf8");

  // the control: its own drawer row, outside the body editor, absent from the grid
  assert.match(drawerVue, /class="drawer-danger"/, "the delete rides the drawer's own danger zone");
  assert.match(drawerVue, /@click="armDelete"/, "the first click arms the confirm");
  assert.match(drawerVue, /@click="confirmDelete"/, "only the confirm emits the delete");
  assert.doesNotMatch(drawerVue, /drawer-discard/, "the delete no longer hides in the body-edit form");
  assert.doesNotMatch(boardVue, /class="[^"]*card-delete/, "no delete control on the card canvas");
  // the confirm names id + title
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
  // no undo, and the reason is in the code
  assert.match(drawerVue, /there is NO undo/, "the no-undo ruling is stated where the control lives");
  assert.match(drawerVue, /would need card resurrection/, "and names WHY undo cannot exist");
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

// --- the push shape (card t_b5784de3): the 5s poll is dead, the shell's
// canvas delta event keeps the board live. The delta is a PULSE, not the
// payload: the tasks panel carries the 20 newest rows of the tools'
// session board (no body, no priority, no position), so the board re-reads
// its own REST view on the event instead of rendering the panel.

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
  // the coalescer: a burst of deltas is one read, not N (round-2 nit)
  assert.match(boardVue, /const DELTA_COALESCE_MS = \d+/, "a delta burst coalesces into one read");
  assert.match(boardVue, /if \(deltaTimer !== null\) return;/, "an already-queued read absorbs the rest of the burst");
  assert.match(boardVue, /clearTimeout\(deltaTimer\)/, "a queued read leaves with the component");
  // the request epoch: an event-triggered read must never lose to an older one
  assert.match(boardVue, /const seq = \+\+fetchSeq/, "each list read carries its request epoch");
  assert.match(boardVue, /if \(seq !== fetchSeq\) return/, "a late answer never overwrites a newer list");
});

// --- the reconnect reset (card t_b5784de3 round 2, vera's outage finding):
// the socket replays nothing, so a delta that lands while THIS page's socket
// is down is lost. Without a pulse the idle board would stay stale until some
// later mutation fired a delta - the 5s poll used to heal that in <=5s. The
// shell's catchUp re-announces the SAME canvas event with the reset id, and
// the board re-reads its whole view on it.

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
    // the boot path: a snapshot refresh says nothing, so a page load
    // never storms its consumers
    store.applySnapshot([{ id: "tasks", title: "Tasks", kind: "list", payload: { board: "main", total: 0, rows: [] }, actions: [] }]);
    assert.equal(seen.length, 0, "the snapshot (boot + catch-up) announces nothing");
    // the reconnect path: the shell asks every consumer to re-read
    announceCanvasReset();
    assert.equal(seen.length, 1, "the reset pulse is exactly one announcement");
    assert.equal(seen[0].type, CANVAS_DELTA_EVENT, "the reset rides the ONE canvas event - no second channel");
    assert.equal((seen[0].detail as { id?: string }).id, CANVAS_RESET_ID, "the reset id rides as detail");
    // and the two sides agree on the literal without importing each other
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

// --- card polish + the running heartbeat chip (card t_65ea61a1) --------
// Endo's ruling: "the running card dont have a heartbeat". The board
// rendered a flat title, stray "@name" text and an age - nothing about
// the work in flight. These pins hold the Hermes card grammar (2-line
// clamps, an assignee chip, one meta row) and the chip's two clocks.

test("card polish: the 2-line clamp is ONE constant the CSS renders", () => {
  assert.equal(CARD_CLAMP_LINES, 2, "the ruled clamp is two lines");
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  // both text surfaces wear the clamp class
  assert.match(boardVue, /class="card-title card-clamp">\{\{ t\.title \}\}/, "the title clamps");
  assert.match(boardVue, /class="card-preview card-clamp">\{\{ bodyPreview\(t\.body\) \}\}/, "the body teaser clamps");
  // the class is the mechanism, fed by the constant (not a magic number)
  assert.match(boardVue, /\.card-clamp \{\s*\n\s*display: -webkit-box;/, "the clamp is a box clamp");
  assert.match(boardVue, /-webkit-box-orient: vertical;/, "vertical lines");
  assert.match(boardVue, /-webkit-line-clamp: var\(--card-clamp, 2\);/, "the line count reads the custom property");
  assert.match(boardVue, /line-clamp: var\(--card-clamp, 2\);/, "and the standard property agrees");
  assert.match(boardVue, /'--card-clamp': `\$\{CARD_CLAMP_LINES\}`/, "the constant feeds the property");
  // the old free-growing preview (max-height) is gone with it
  assert.doesNotMatch(boardVue, /max-height: 2\.6em/, "the teaser clamps by line, not by an em guess");
});

test("card polish: the assignee is a chip on one Hermes-style meta row", () => {
  const boardVue = readFileSync(join(uiRoot, "..", "harness", "kanban", "ui", "Board.vue"), "utf8");
  assert.match(boardVue, /class="card-assignee-chip"/, "the assignee renders as a chip");
  assert.match(boardVue, /:title="`assignee: \$\{t\.assignee\}`"/, "the chip names what it is");
  assert.doesNotMatch(boardVue, /class="card-assignee"/, "the old bare text span is gone");
  assert.match(boardVue, /\.card-assignee-chip \{[\s\S]*?border-radius: var\(--r-chip\);/, "the chip wears the shell's chip radius");
  // one meta row carries assignee + heartbeat + age + the move controls
  const metaAt = boardVue.indexOf('class="card-meta"');
  assert.ok(metaAt > 0, "the meta row exists");
  const meta = boardVue.slice(metaAt, boardVue.indexOf("</article>", metaAt));
  for (const face of ["card-assignee-chip", "card-heartbeat", "card-age", "card-moves"]) {
    assert.ok(meta.includes(face), `${face} rides the one meta row`);
  }
  // ONE time telling: the running chip replaces the created-age (the row
  // is 218px wide - both would clip the chip, and a running card's story
  // is how long it has been working)
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
  // a card that just started reads 0m, not a blank
  assert.equal(heartbeatChip(task({ status: "running", started_at: "2026-11-12T12:00:00Z" }), now)?.label, "working - 0m");
  // only the running lane beats
  for (const status of ["triage", "todo", "scheduled", "ready", "blocked", "review", "done", "archived"]) {
    assert.equal(heartbeatChip(task({ status, started_at: "2026-11-12T11:00:00Z" }), now), null, `${status} has no chip`);
  }
  // an unparseable start is not a duration to invent
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
  // one second past it: stale, and the chip says how long the quiet ran
  const late = new Date(now.getTime() - HEARTBEAT_STALE_MS - 1000).toISOString();
  const stale = heartbeatChip(running({ last_heartbeat_at: late }), now);
  assert.equal(stale?.stale, true);
  assert.equal(stale?.label, "stale - 2m quiet");
  assert.equal(stale?.runningMs, 10 * 60 * 1000, "the working clock keeps its own count while stale");

  // NO heartbeat field at all is not staleness: silence is not evidence
  assert.equal(heartbeatChip(running({}), now)?.stale, false, "a wire with no heartbeat field never reads stale");
  // and an unparseable stamp is not an aged one
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
  // one chip map per render, keyed by card; the template reads it
  assert.match(boardVue, /const heartbeats = computed\(/, "the chips derive once per render");
  assert.match(boardVue, /const chip = heartbeatChip\(t, at\);/, "through the pure helper");
  assert.match(boardVue, /v-if="heartbeats\[t\.id\]"/, "a card wears a chip only when it has one");
  assert.match(boardVue, /class="card-heartbeat"[\s\S]{0,120}:class="\{ stale: heartbeats\[t\.id\]\.stale \}"/, "the stale state rides the chip class");
  assert.match(boardVue, /\{\{ heartbeats\[t\.id\]\.label \}\}/, "the chip renders the computed face");
  // the stale face is visibly different: a flat dot, the blocked tone
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
