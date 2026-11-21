// omega webui: the board card's right-click rows - registration law and wiring pins.
//
// EXISTING VERBS ONLY. Open is the card's click, Copy id is client-side, Archive is
// the PATCH the lane arrow already sends - and an already-archived card offers no Archive,
// since a second move to the archived lane is not a verb. The browser half is recipe 118.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { cardMenuItems } from "../../harness/kanban/ui/board/cardMenu.ts";
import {
  closeContextMenu,
  contextMenuEntries,
  contextMenuOpen,
  openContextMenu,
  runContextMenuItem,
} from "../app/composables/contextMenu.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

function calls() {
  const log: string[] = [];
  return {
    log,
    open: () => log.push("open"),
    copyId: () => log.push("copy"),
    archive: () => log.push("archive"),
  };
}

describe("board cards: Open, Copy id, Archive", () => {
  it("offers the two live verbs plus Archive, below a separator", () => {
    const items = cardMenuItems({ id: "t_1", status: "running" }, calls());
    assert.deepEqual(ids(items), ["card:open", "card:copy-id", "separator", "card:archive"]);
  });

  it("an archived card offers no Archive (the verb cannot act)", () => {
    const items = cardMenuItems({ id: "t_1", status: "archived" }, calls());
    assert.deepEqual(ids(items), ["card:open", "card:copy-id"]);
    assert.ok(
      !items.some((e) => (e as { kind?: string }).kind === "separator"),
      "no orphan rule is left behind",
    );
  });

  it("each row runs the verb it names, and only that one", () => {
    const d = calls();
    for (const item of cardMenuItems({ id: "t_1", status: "todo" }, d)) {
      if ((item as { id?: string }).id) (item as { run: () => void }).run();
    }
    assert.deepEqual(d.log, ["open", "copy", "archive"]);
  });

  it("the row list is a menu the shared store opens (no local menu)", () => {
    const items = cardMenuItems({ id: "t_1", status: "todo" }, calls());
    try {
      assert.equal(openContextMenu(40, 40, items), true, "the store takes the card's rows");
      assert.equal(contextMenuOpen.value, true);
      assert.deepEqual(ids(contextMenuEntries.value), ids(items));
      assert.equal(runContextMenuItem(0), true, "a row fires through the store, not the card");
    } finally {
      closeContextMenu();
    }
  });
});

describe("Board.vue wiring", () => {
  const boardVue = read("harness", "kanban", "ui", "Board.vue");

  it("registers the menu on the card, not on the lane or the drawer", () => {
    assert.match(boardVue, /class="card"[\s\S]{0,200}@contextmenu="onCardMenu\(\$event, t\)"/);
  });

  it("rides the ONE shell store and the ONE clipboard path", () => {
    assert.match(boardVue, /import \{ openContextMenu \} from "~\/composables\/contextMenu"/);
    assert.match(boardVue, /import \{ copyText \} from "~\/composables\/clipboard"/);
    assert.doesNotMatch(boardVue, /new (Mouse)?Event\("contextmenu"\)/, "no synthetic menu");
  });

  it("right-click prevents the native menu and menus the card under the pointer", () => {
    const handler = boardVue.slice(boardVue.indexOf("function onCardMenu"));
    const body = handler.slice(0, handler.indexOf("\n  }"));
    assert.match(body, /e\.preventDefault\(\)/, "the browser menu never wins on a card");
    assert.match(body, /cardMenuItems\(\s*\{ id: t\.id, status: t\.status \}/, "the pointed card, not the selection");
    assert.match(body, /archive: \(\) => void moveBoard\(t, "archived"\)/, "Archive is the lane move's own PATCH");
    assert.match(body, /open: \(\) => openDetail\(t\)/, "Open is the card's own click");
  });

  it("the copy notice follows the clipboard's answer", () => {
    const fn = boardVue.slice(boardVue.indexOf("async function copyCardId"));
    const body = fn.slice(0, fn.indexOf("\n  }"));
    assert.match(body, /const ok = await copyText\(id\)/);
    assert.match(body, /setNotice\(ok \?/, "copied only when the copy really happened");
  });
});
