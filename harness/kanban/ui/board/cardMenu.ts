// omega webui: the board card's right-click rows.
//
// EXISTING VERBS ONLY. Open is the card's own click (the detail drawer), Copy id is
// client-side through the one clipboard path, Stop is the lane move's own PATCH
// {status:"blocked"} (a running card's work ends; the shell also stops the card's live
// session run), Archive is the PATCH the lane arrow already sends (the card's status
// moved to the archived lane) and Delete is the drawer's own DELETE /tasks/{id}. Stop is
// offered only while the card is running, Archive never on an already-archived card: a
// verb that cannot act is absent. Delete is destructive and irreversible, so it arms on
// the first pick and only fires on the second - the same confirm shape the drawer and
// the board menu use.

import type { ContextMenuEntry } from "../../../../webui/app/composables/contextMenu.ts";

export interface CardMenuDeps {
  open: () => void;
  copyId: () => void;
  stop: () => void;
  archive: () => void;
  remove: () => void;
}

export function cardMenuItems(
  card: { id: string; status: string },
  deps: CardMenuDeps,
  armedDelete = false,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [
    { id: "card:open", label: "Open", run: deps.open },
    { id: "card:copy-id", label: "Copy id", run: deps.copyId },
    { kind: "separator" },
  ];
  if (card.status === "running") {
    items.push({ id: "card:stop", label: "Stop", danger: true, run: deps.stop });
  }
  if (card.status !== "archived") {
    items.push({ id: "card:archive", label: "Archive", run: deps.archive });
  }
  items.push({
    id: "card:delete",
    label: armedDelete ? "Confirm delete" : "Delete",
    danger: true,
    run: deps.remove,
  });
  return items;
}
