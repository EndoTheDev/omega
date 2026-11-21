// omega webui: the sessions sidebar row's right-click rows.
//
// EXISTING VERBS ONLY: every row here is a route or a client intent that already exists -
// New session (the sidebar's own opener), Branch (POST /sessions/{id}/branch), Export
// (GET /sessions/{id}/export) and Stop (POST /sessions/{id}/stop). Stop rides a live-run
// refusal (404 "session not active"), so it is offered only on a row that is running.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface SessionMenuDeps {
  newSession: () => void;
  branch: () => void;
  exportSession: () => void;
  stop: () => void;
}

export function sessionMenuItems(
  session: { id: string; running: boolean },
  deps: SessionMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [
    { id: "session:new", label: "New session", run: deps.newSession },
    { id: "session:branch", label: "Branch", run: deps.branch },
    { id: "session:export", label: "Export", run: deps.exportSession },
  ];
  if (session.running) {
    items.push({ kind: "separator" });
    items.push({ id: "session:stop", label: "Stop", danger: true, run: deps.stop });
  }
  return items;
}
