// omega webui: the sessions sidebar row's right-click rows.
//
// EXISTING VERBS ONLY: every row here is a route or a client intent that already exists -
// New session (the sidebar's own opener), Rename (the row's inline editor), Branch
// (POST /sessions/{id}/branch), Export (GET /sessions/{id}/export), Copy id (the row's
// hover copy affordance, one clipboard path), Archive or Unarchive (the row's own PATCH
// {archived}), Delete (the shell's one confirm + DELETE /sessions/{id}), and Stop
// (POST /sessions/{id}/stop). Stop rides a live-run refusal (404 "session not active"),
// so it is offered only on a row that is running.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface SessionMenuDeps {
  newSession: () => void;
  rename: () => void;
  branch: () => void;
  exportSession: () => void;
  copyId: () => void;
  archive: () => void;
  remove: () => void;
  stop: () => void;
}

export function sessionMenuItems(
  session: { id: string; running: boolean; archived?: boolean },
  deps: SessionMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [
    { id: "session:new", label: "New session", run: deps.newSession },
    { id: "session:rename", label: "Rename", run: deps.rename },
    { kind: "separator" },
    { id: "session:branch", label: "Branch", run: deps.branch },
    { id: "session:export", label: "Export", run: deps.exportSession },
    { id: "session:copy-id", label: "Copy id", run: deps.copyId },
    { kind: "separator" },
    {
      id: "session:archive",
      label: session.archived ? "Unarchive" : "Archive",
      run: deps.archive,
    },
    { id: "session:delete", label: "Delete", danger: true, run: deps.remove },
  ];
  if (session.running) {
    items.push({ kind: "separator" });
    items.push({ id: "session:stop", label: "Stop", danger: true, run: deps.stop });
  }
  return items;
}
