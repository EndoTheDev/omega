// omega webui: the file tree row's right-click rows.
//
// EXISTING VERBS ONLY: New opens the tab's own new-node form (POST /files/write through
// the form's file mode; the band's add-folder button flips the same form to POST
// /files/mkdir with the target kept), Rename opens that form in rename mode (POST
// /files/rename with the same leaf target), Open is the row's own click (the viewer), Copy
// reads the file's text through GET /files/read and Copy path takes the relative path -
// both through the one clipboard path - and Add file to chat rides the composer's own
// attach lane (GET /files/raw bytes -> POST /attach-file). Delete (POST /files/delete) is
// irreversible, so it arms on the first pick and fires on the second - the card menu's
// one shape.
//
// A verb that cannot act is ABSENT: Open/Copy/Add file to chat need a FILE row, and the
// chat lanes need a focused chat tab to land in. A directory row keeps New, Rename, Copy
// path and Delete.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface FileMenuDeps {
  create: () => void;
  rename: () => void;
  open: () => void;
  copy: () => void;
  copyPath: () => void;
  addToChat: () => void;
  remove: () => void;
}

export function fileMenuItems(
  entry: { isDir: boolean; chatBound: boolean; armedDelete?: boolean },
  deps: FileMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [{ id: "file:new", label: "New", run: deps.create }];
  items.push({ id: "file:rename", label: "Rename", run: deps.rename });
  if (!entry.isDir) {
    items.push({ id: "file:open", label: "Open", run: deps.open });
  }
  items.push({ kind: "separator" });
  if (!entry.isDir) {
    items.push({ id: "file:copy", label: "Copy", run: deps.copy });
  }
  items.push({ id: "file:copy-path", label: "Copy path", run: deps.copyPath });
  if (!entry.isDir && entry.chatBound) {
    items.push({ kind: "separator" });
    items.push({ id: "file:chat", label: "Add file to chat", run: deps.addToChat });
  }
  items.push({ kind: "separator" });
  items.push({
    id: "file:delete",
    label: entry.armedDelete ? "Confirm delete" : "Delete",
    danger: true,
    run: deps.remove,
  });
  return items;
}
