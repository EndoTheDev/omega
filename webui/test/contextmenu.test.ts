// omega webui: THE context menu store - one menu for every chrome surface.
//
// The composable is pure where it can be (placement, ordering, the key grammar) so this file
// is a real unit test; the SFC's own wiring is pinned in docktabmenu.test.ts (the node suite
// has no DOM).
import { beforeEach, describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  CONTEXT_MENU_MARGIN_PX,
  actionIndexes,
  closeContextMenu,
  contextMenuActive,
  contextMenuEntries,
  contextMenuMeasured,
  contextMenuOpen,
  contextMenuPlacement,
  handleContextMenuKey,
  menuHasActions,
  normalizeMenuEntries,
  openContextMenu,
  placeContextMenu,
  runContextMenuItem,
  setContextMenuActive,
  setContextMenuSize,
  stepActiveIndex,
  type ContextMenuEntry,
  type ContextMenuItem,
} from "../app/composables/contextMenu.ts";

function item(id: string, extra: Partial<ContextMenuItem> = {}): ContextMenuItem {
  return { id, label: id, run: () => {}, ...extra };
}

const SEP: ContextMenuEntry = { kind: "separator" };

function ids(entries: readonly ContextMenuEntry[]): string[] {
  return entries.map((e) => ("kind" in e ? "(sep)" : e.id));
}

describe("placeContextMenu: open at the cursor, flip at the edges, clamp into the viewport", () => {
  const viewport = { w: 1000, h: 700 };
  const size = { w: 200, h: 120 };

  it("opens down-right of the cursor with room to spare", () => {
    const p = placeContextMenu({ x: 300, y: 200 }, size, viewport);
    assert.deepEqual(p, { left: 300, top: 200, flippedX: false, flippedY: false });
  });

  it("flips left when the box would run past the right edge", () => {
    const p = placeContextMenu({ x: 900, y: 200 }, size, viewport);
    assert.equal(p.flippedX, true);
    assert.equal(p.left, 700, "grows leftwards from the pointer");
    assert.equal(p.top, 200);
  });

  it("flips up when the box would run past the bottom edge", () => {
    const p = placeContextMenu({ x: 300, y: 650 }, size, viewport);
    assert.equal(p.flippedY, true);
    assert.equal(p.top, 530);
    assert.equal(p.left, 300);
  });

  it("flips both ways near the bottom-right corner", () => {
    const p = placeContextMenu({ x: 990, y: 695 }, size, viewport);
    assert.deepEqual(
      { left: p.left, top: p.top },
      { left: 790, top: 575 },
    );
  });

  it("holds the margin floor at the top-left corner", () => {
    // a cursor in the corner would put the box flush against it; the margin is the floor
    const p = placeContextMenu({ x: 0, y: 0 }, { w: 40, h: 30 }, viewport);
    assert.deepEqual(
      { left: p.left, top: p.top },
      { left: CONTEXT_MENU_MARGIN_PX, top: CONTEXT_MENU_MARGIN_PX },
    );
    assert.equal(p.flippedX, false);
  });

  it("clamps a menu bigger than the viewport at the margin, never past it", () => {
    const p = placeContextMenu({ x: 500, y: 400 }, { w: 1200, h: 900 }, viewport);
    assert.equal(p.flippedX, true);
    assert.equal(p.flippedY, true);
    assert.equal(p.left, CONTEXT_MENU_MARGIN_PX);
    assert.equal(p.top, CONTEXT_MENU_MARGIN_PX);
  });

  it("stays inside the viewport on a box that nearly fills it", () => {
    const p = placeContextMenu({ x: 0, y: 0 }, { w: viewport.w - 8, h: viewport.h - 8 }, viewport);
    assert.equal(p.flippedX, false);
    assert.equal(p.left, CONTEXT_MENU_MARGIN_PX);
    assert.equal(p.top, CONTEXT_MENU_MARGIN_PX);
  });
});

describe("normalizeMenuEntries: a menu never opens on a rule", () => {
  it("drops leading, trailing and doubled separators", () => {
    const out = normalizeMenuEntries([
      SEP,
      item("a"),
      SEP,
      SEP,
      item("b"),
      SEP,
    ]);
    assert.deepEqual(ids(out), ["a", "(sep)", "b"]);
  });

  it("returns an empty list when only separators were registered", () => {
    assert.deepEqual(normalizeMenuEntries([SEP, SEP]), []);
  });
});

describe("action rows: separators and disabled rows are never targets", () => {
  const list: ContextMenuEntry[] = [
    SEP,
    item("a"),
    SEP,
    item("b", { disabled: true }),
    item("c"),
  ];

  it("lists only fireable rows", () => {
    assert.deepEqual(actionIndexes(list), [1, 4]);
  });

  it("steps with a wrap in both directions", () => {
    assert.equal(stepActiveIndex(list, 1, 1), 4);
    assert.equal(stepActiveIndex(list, 4, 1), 1, "wraps past the end");
    assert.equal(stepActiveIndex(list, 1, -1), 4, "wraps past the start");
  });

  it("lands on the first/last row from a non-row start", () => {
    assert.equal(stepActiveIndex(list, -1, 1), 1);
    assert.equal(stepActiveIndex(list, -1, -1), 4);
    assert.equal(stepActiveIndex(list, 2, 1), 1, "a separator index is not a row");
  });

  it("answers -1 when nothing can fire", () => {
    assert.equal(stepActiveIndex([SEP, item("x", { disabled: true })], 0, 1), -1);
  });

  it("menuHasActions is the empty-list law", () => {
    assert.equal(menuHasActions([SEP]), false);
    assert.equal(menuHasActions([]), false);
    assert.equal(menuHasActions([item("a", { disabled: true })]), false);
    assert.equal(menuHasActions([item("a")]), true);
  });
});

describe("the store: open, close, run", () => {
  beforeEach(() => {
    closeContextMenu();
  });

  it("opens at the cursor with the first action active", () => {
    const ran = openContextMenu(120, 80, [SEP, item("a"), item("b")]);
    assert.equal(ran, true);
    assert.equal(contextMenuOpen.value, true);
    assert.deepEqual(ids(contextMenuEntries.value), ["a", "b"], "the leading rule is trimmed");
    assert.equal(contextMenuActive.value, 0, "the first row is the selection");
  });

  it("an empty verb list opens nothing at all", () => {
    assert.equal(openContextMenu(10, 10, []), false);
    assert.equal(contextMenuOpen.value, false);
  });

  it("separators alone are not a menu", () => {
    assert.equal(openContextMenu(10, 10, [SEP, SEP]), false);
    assert.equal(contextMenuOpen.value, false);
  });

  it("a disabled-only list opens nothing", () => {
    assert.equal(openContextMenu(10, 10, [item("a", { disabled: true })]), false);
    assert.equal(contextMenuOpen.value, false);
  });

  it("close clears the rows, the position and the measure", () => {
    openContextMenu(10, 20, [item("a")]);
    setContextMenuSize(180, 90);
    assert.equal(contextMenuMeasured.value, true);
    closeContextMenu();
    assert.equal(contextMenuOpen.value, false);
    assert.deepEqual(contextMenuEntries.value, []);
    assert.equal(contextMenuMeasured.value, false);
    assert.equal(contextMenuActive.value, -1);
    assert.equal(contextMenuPlacement.value, null);
  });

  it("re-opening at a new spot replaces the rows and the cursor", () => {
    openContextMenu(10, 20, [item("a")]);
    setContextMenuSize(180, 90);
    openContextMenu(700, 500, [item("b")]);
    assert.deepEqual(ids(contextMenuEntries.value), ["b"]);
    assert.equal(contextMenuMeasured.value, false, "the new box is measured again");
  });

  it("runs a row and closes first", () => {
    let ran = 0;
    let openWhenRun = true;
    openContextMenu(0, 0, [
      item("a", {
        run: () => {
          ran += 1;
          openWhenRun = contextMenuOpen.value;
        },
      }),
    ]);
    assert.equal(runContextMenuItem(0), true);
    assert.equal(ran, 1);
    assert.equal(openWhenRun, false, "a verb that opens a dialog never lands under the menu");
    assert.equal(contextMenuOpen.value, false);
  });

  it("refuses a separator, a disabled row, an out-of-range index and a closed menu", () => {
    openContextMenu(0, 0, [item("a"), SEP, item("b", { disabled: true })]);
    assert.equal(runContextMenuItem(1), false);
    assert.equal(runContextMenuItem(2), false);
    assert.equal(runContextMenuItem(9), false);
    assert.equal(contextMenuOpen.value, true, "a refused row leaves the menu up");
    closeContextMenu();
    assert.equal(runContextMenuItem(0), false);
  });

  it("hover only selects a fireable row", () => {
    openContextMenu(0, 0, [item("a"), SEP, item("b")]);
    setContextMenuActive(1);
    assert.equal(contextMenuActive.value, 0, "a separator is not a selection");
    setContextMenuActive(2);
    assert.equal(contextMenuActive.value, 2);
  });
});

describe("the key grammar: escape, arrows, enter", () => {
  beforeEach(() => {
    closeContextMenu();
    openContextMenu(0, 0, [item("a"), SEP, item("b"), item("c", { disabled: true })]);
  });

  it("ArrowDown/ArrowUp cycle the fireable rows", () => {
    assert.equal(contextMenuActive.value, 0);
    assert.equal(handleContextMenuKey("ArrowDown"), true);
    assert.equal(contextMenuActive.value, 2);
    assert.equal(handleContextMenuKey("ArrowDown"), true);
    assert.equal(contextMenuActive.value, 0, "skips the trailing disabled row and wraps");
    assert.equal(handleContextMenuKey("ArrowUp"), true);
    assert.equal(contextMenuActive.value, 2);
  });

  it("Enter fires the active row", () => {
    let ran = "";
    closeContextMenu();
    openContextMenu(0, 0, [
      item("a", { run: () => { ran = "a"; } }),
      item("b", { run: () => { ran = "b"; } }),
    ]);
    handleContextMenuKey("ArrowDown");
    assert.equal(handleContextMenuKey("Enter"), true);
    assert.equal(ran, "b");
    assert.equal(contextMenuOpen.value, false);
  });

  it("Escape closes", () => {
    assert.equal(handleContextMenuKey("Escape"), true);
    assert.equal(contextMenuOpen.value, false);
  });

  it("any other key is left to the page", () => {
    assert.equal(handleContextMenuKey("a"), false);
    assert.equal(contextMenuOpen.value, true);
  });

  it("a closed menu consumes nothing", () => {
    closeContextMenu();
    assert.equal(handleContextMenuKey("Escape"), false);
    assert.equal(handleContextMenuKey("ArrowDown"), false);
    assert.equal(handleContextMenuKey("Enter"), false);
  });
});
