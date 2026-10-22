// omega webui: tab DnD gesture state (card t_d6b657a7). The DOM drag
// events hide the dragged data from dragover (only `types` are readable),
// but the hit-test needs the dragged tab's identity DURING the gesture
// (sessions is locked: it may only drop into its own left sidebar). One
// page-lifetime record, written on dragstart, cleared on dragend - the
// panes read it live; the drop handlers still re-read the dataTransfer
// payload as the source of truth.
import { isLockedTab, type DockComponentName } from "./dockables.ts";

export interface TabDragInfo {
  /** source pane id */
  paneId: string;
  /** source tab index */
  index: number;
  component: DockComponentName;
}

let current: TabDragInfo | null = null;

export const tabDrag = {
  current: (): TabDragInfo | null => current,
  start(info: TabDragInfo): void {
    current = info;
  },
  end(): void {
    current = null;
  },
};

/** PURE hit-test: may the active drag drop into a pane whose edge is
    `targetEdge` (null = main pane)? sessions is LOCKED to the left
    sidebar's own bar - everywhere else refuses (the chip under the
    cursor renders the no-drop affordance while it hovers). card
    t_67fc1905: the bottom shell joins the edge class - free content
    like the sidebars, every non-locked tab drops in/out. */
export function dropAllowedFor(
  targetEdge: "left" | "right" | "bottom" | null,
  drag: TabDragInfo | null,
): boolean {
  if (!drag) return false;
  return !isLockedTab(drag.component) || targetEdge === "left";
}