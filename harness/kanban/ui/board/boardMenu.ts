// omega webui: the kanban board surface's right-click rows.
//
// EXISTING VERBS ONLY: Rename and Delete are the board dropdown's own two picks (PATCH and
// DELETE /boards/{name}) behind the ONE arm state both doors share, so the same destructive
// verb never grows a second confirm. No board selected means no rows: an empty list opens
// no menu rather than rows that cannot act.

import type { ContextMenuEntry } from "../../../../webui/app/composables/contextMenu.ts";
import { canRemoveBoard } from "./boardLogic.ts";

export interface BoardMenuDeps {
  rename: () => void;
  remove: () => void;
}

export function boardMenuItems(
  board: { name: string; armed?: boolean },
  deps: BoardMenuDeps,
): ContextMenuEntry[] {
  if (!canRemoveBoard(board.name)) return [];
  return [
    { id: "board:rename", label: "Rename", run: deps.rename },
    { kind: "separator" },
    {
      id: "board:delete",
      label: board.armed ? "Confirm delete" : "Delete",
      danger: true,
      run: deps.remove,
    },
  ];
}
