// omega webui: the notifications feed row's right-click rows.
//
// EXISTING VERBS ONLY: Clear is DELETE /notifications/{id} (this record), Clear all is
// the tab's own DELETE /notifications (the band's trash button). The per-record verb is
// the row's, so it acts on the thing the operator pointed at; Clear all stays the whole
// feed's.
//
// An empty feed registers nothing (both routes would clear nothing), so the right-click
// leaves the browser's own menu alone. The rows carry no danger tint: they are the same
// actions the band button offers, already one click and no confirm.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface NotificationMenuDeps {
  clearOne: () => void;
  clearAll: () => void;
}

export function notificationMenuItems(
  record: { id: number },
  feed: { count: number; clearing: boolean },
  deps: NotificationMenuDeps,
): ContextMenuEntry[] {
  if (feed.count === 0 || feed.clearing) return [];
  return [
    { id: "notif:clear", label: "Clear", run: deps.clearOne },
    { kind: "separator" },
    { id: "notif:clear-all", label: "Clear all", run: deps.clearAll },
  ];
}
