// omega webui: tab DnD gesture state (card t_d6b657a7) + the VS Code-style
// docking-zone prediction (card t_0e833eae). The DOM drag events hide the
// dragged data from dragover (only `types` are readable), but the hit-test
// needs the dragged tab's identity DURING the gesture (sessions is locked:
// it may only drop into its own left sidebar). One page-lifetime record,
// written on dragstart, cleared on dragend - the panes read it live; the
// drop handlers still re-read the dataTransfer payload as the source of
// truth.
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

// --- docking zones (card t_0e833eae) -----------------------------------------
// A tab dragged over ANOTHER pane's body predicts where the tab lands: the
// four half-zones split the target pane around the dragged tab, the center
// zone groups it into the target as a plain tab. VS Code thresholds: the
// pointer sits in a side zone once it leaves the middle band along either
// axis; the nearer edge wins in the corners.

export type DragZone = "left" | "right" | "top" | "bottom" | "center";

/** a content box in viewport px (the pane body under the tab bar) */
export interface ZoneRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** the middle 50%-of-axis band whose overlap on BOTH axes reads "center";
    outside it the nearer edge scores the zone. Same read at every pane
    size - the band is a FRACTION of each axis, not px. */
export const ZONE_CENTER_BAND = 0.25;

/** PURE pointer -> zone. Degenerate rects (zero size: an unmeasured or
    collapsed pane) fold to "center" - group-into-pane stays honest, the
    split zones do. */
export function zoneForPoint(x: number, y: number, rect: ZoneRect): DragZone {
  if (!(rect.w > 0) || !(rect.h > 0)) return "center";
  // clamp: a pointer marginally outside the measured box (dragleave races)
  // still scores against the nearest edge instead of returning nonsense
  const rx = Math.min(1, Math.max(0, (x - rect.x) / rect.w));
  const ry = Math.min(1, Math.max(0, (y - rect.y) / rect.h));
  const inX = rx >= ZONE_CENTER_BAND && rx <= 1 - ZONE_CENTER_BAND;
  const inY = ry >= ZONE_CENTER_BAND && ry <= 1 - ZONE_CENTER_BAND;
  if (inX && inY) return "center";
  // the nearest crossed edge wins: distance to each content-box side
  const scores: readonly [DragZone, number][] = [
    ["left", rx],
    ["top", ry],
    ["right", 1 - rx],
    ["bottom", 1 - ry],
  ];
  let best: DragZone = "center";
  let bestScore = Infinity;
  for (const [zone, score] of scores) {
    if (score < bestScore) {
      best = zone;
      bestScore = score;
    }
  }
  return best;
}

/** the overlay rect of a zone as FRACTIONS of the pane body (left/top +
    width/height, 0..1) - the preview positions itself percent-based, so
    no px math runs in the component. center covers the whole body. */
export function zoneRectFractions(zone: DragZone): ZoneRect {
  switch (zone) {
    case "left":
      return { x: 0, y: 0, w: 0.5, h: 1 };
    case "right":
      return { x: 0.5, y: 0, w: 0.5, h: 1 };
    case "top":
      return { x: 0, y: 0, w: 1, h: 0.5 };
    case "bottom":
      return { x: 0, y: 0.5, w: 1, h: 0.5 };
    case "center":
      return { x: 0, y: 0, w: 1, h: 1 };
  }
}

/** PURE zone gate: which zones may a drop land on for a target pane whose
    edge marker is `targetEdge` (undefined = main pane)? EDGE SHELLS keep
    their fixed shape (card t_d6b657a7: identity, not content) - their
    tabs are free but the pane never splits, so only the center zone
    (group into the shell's bar) lands there. Main panes take every zone. */
export function splitAllowedFor(
  targetEdge: "left" | "right" | "bottom" | undefined,
): boolean {
  return targetEdge === undefined;
}

/** the dock mutation a zone encodes, for the drop handlers: direction +
    side the target pane splits with (null for center = plain move) */
export function zoneSplitOf(zone: DragZone):
  | { direction: "row" | "column"; side: "before" | "after" }
  | null {
  switch (zone) {
    case "left":
      return { direction: "row", side: "before" };
    case "right":
      return { direction: "row", side: "after" };
    case "top":
      return { direction: "column", side: "before" };
    case "bottom":
      return { direction: "column", side: "after" };
    case "center":
      return null;
  }
}