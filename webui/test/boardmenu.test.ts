// omega webui: the kanban board surface's right-click rows - registration law and wiring
// pins (right-click arc 5a).
//
// EXISTING VERBS ONLY: Rename is the switcher's rename pick (PATCH /boards/{name}) and
// Delete is its delete pick (DELETE /boards/{name}) - one arm state, shared by both doors,
// so the same delete never grows a second confirm. A board with no name has nothing to act
// on, and an empty list opens no menu at all. The browser half is recipe 119.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { boardMenuItems } from "../../harness/kanban/ui/board/boardMenu.ts";
import { boardMenuOptions, type BoardInfo } from "../../harness/kanban/ui/board/boardLogic.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

function calls() {
  const log: string[] = [];
  return {
    log,
    rename: () => log.push("rename"),
    remove: () => log.push("remove"),
  };
}

describe("board surface: Rename, Delete", () => {
  it("offers Rename, then the destructive Delete below a rule", () => {
    const items = boardMenuItems({ name: "main" }, calls());
    assert.deepEqual(ids(items), ["board:rename", "separator", "board:delete"]);
    const del = items[items.length - 1] as { label: string; danger?: boolean };
    assert.equal(del.label, "Delete");
    assert.equal(del.danger, true, "a delete wears the danger color");
  });

  it("the armed list relabels Delete as Confirm delete", () => {
    const items = boardMenuItems({ name: "main", armed: true }, calls());
    const del = items[items.length - 1] as { label: string; id: string };
    assert.equal(del.id, "board:delete", "the same row, one arm later");
    assert.equal(del.label, "Confirm delete");
  });

  it("each row runs the writer it names, and only that one", () => {
    const d = calls();
    for (const item of boardMenuItems({ name: "main" }, d)) {
      if ((item as { id?: string }).id) (item as { run: () => void }).run();
    }
    assert.deepEqual(d.log, ["rename", "remove"]);
  });

  it("no board selected = no rows at all (the empty list opens no menu)", () => {
    assert.deepEqual(boardMenuItems({ name: "" }, calls()), []);
  });

  it("the dropdown's delete row speaks the same two words", () => {
    const boards = [{ name: "main", current: true }] as BoardInfo[];
    const plain = boardMenuOptions(boards).at(-1) as { label: string };
    const armed = boardMenuOptions(boards, true).at(-1) as { label: string };
    assert.equal(plain.label, "delete board");
    assert.equal(armed.label, "confirm delete");
  });
});

describe("Board.vue wiring", () => {
  const boardVue = read("harness", "kanban", "ui", "Board.vue");

  it("registers the menu on the board surface, not on the shell", () => {
    assert.match(boardVue, /class="board"[\s\S]{0,200}@contextmenu="onBoardMenu"/);
    assert.match(boardVue, /import \{ boardMenuItems \} from "\.\/board\/boardMenu"/);
  });

  it("hands the board's own two writers and the ONE arm state", () => {
    const fn = boardVue.slice(boardVue.indexOf("function onBoardMenu"));
    const body = fn.slice(0, fn.indexOf("\n  }"));
    assert.match(body, /e\.preventDefault\(\)/, "the browser menu never wins on the board");
    assert.match(body, /\{ name: boardName\.value, armed: confirmingRemove\.value \}/);
    assert.match(body, /\{ rename: startRename, remove: removeBoardClick \}/);
    assert.doesNotMatch(body, /method: "(DELETE|PATCH)"/, "the rows reuse the writers, never a second route call");
  });

  it("a card, the drawer or a field under the pointer keeps its own menu", () => {
    const fn = boardVue.slice(boardVue.indexOf("function onBoardMenu"));
    const body = fn.slice(0, fn.indexOf("\n  }"));
    assert.match(body, /closest\?\.\("\.card, \.drawer, input, textarea"\)/);
  });
});
