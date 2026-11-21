// omega webui: the board card's right-click rows.
//
// EXISTING VERBS ONLY. Open is the card's own click (the detail drawer), Copy id is
// client-side through the one clipboard path, and Archive is the PATCH the lane arrow
// already sends - the card's status moved to the archived lane. A card already archived
// has no Archive row: the verb would be a no-op edit.

import type { ContextMenuEntry } from "../../../../webui/app/composables/contextMenu.ts";

export interface CardMenuDeps {
  open: () => void;
  copyId: () => void;
  archive: () => void;
}

export function cardMenuItems(
  card: { id: string; status: string },
  deps: CardMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [
    { id: "card:open", label: "Open", run: deps.open },
    { id: "card:copy-id", label: "Copy id", run: deps.copyId },
  ];
  if (card.status !== "archived") {
    items.push({ kind: "separator" });
    items.push({ id: "card:archive", label: "Archive", run: deps.archive });
  }
  return items;
}
