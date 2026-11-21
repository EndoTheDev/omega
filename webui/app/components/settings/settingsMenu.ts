// omega webui: the settings tab's right-click rows.
//
// The settings pane is a main-area tab like any other: its one standard row is Close, and
// it is offered only where the tab can close (never the locked last tab), so the right-click
// never shows a verb the tab's own X would refuse.

import type { ContextMenuEntry } from "../../composables/contextMenu";

export function settingsMenuItems(canClose: boolean, close: () => void): ContextMenuEntry[] {
  return canClose ? [{ id: "settings:close", label: "Close", run: close }] : [];
}
