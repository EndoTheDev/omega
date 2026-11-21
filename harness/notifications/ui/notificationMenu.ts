// omega webui: the notifications feed row's right-click rows.
//
// EXISTING VERBS ONLY: Clear all is the tab's own DELETE /notifications (the band's
// trash button). ONE row on every record row, because the verb is the whole feed's -
// the feed carries no per-entry clear route, so a per-row Delete would be a promise the
// tree cannot keep.
//
// An empty feed registers nothing (the DELETE would clear nothing), so the right-click
// leaves the browser's own menu alone. The row carries no danger tint: it is the same
// action the band button offers, which is already one click and no confirm.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface NotificationMenuDeps {
  clearAll: () => void;
}

export function notificationMenuItems(
  feed: { count: number; clearing: boolean },
  deps: NotificationMenuDeps,
): ContextMenuEntry[] {
  if (feed.count === 0 || feed.clearing) return [];
  return [{ id: "notif:clear-all", label: "Clear all", run: deps.clearAll }];
}
