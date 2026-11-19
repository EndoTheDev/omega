// pure pane math shared by the divider and the app shell.

// the sidebars' snap ladder is every +320 from 320
export const SNAP_STEP = 320;
export const SNAP_ZONE = 15;

// open-clamp: a collapsed sidebar with a stale sub-floor width reopens at this width
export const PANE_OPEN_MIN = 320;

// the bottom panel's ladder is every +200
export const BOTTOM_SNAP_STEP = 200;

/** returns the exact snap size when the drag is inside a zone, else null */
export function snapCandidateAt(px: number, step: number): number | null {
  const n = Math.round(px / step);
  if (n < 1) return null;
  const p = n * step;
  return Math.abs(px - p) <= SNAP_ZONE ? p : null;
}

/** the sidebars' +320 ladder */
export function snapCandidate(px: number): number | null {
  return snapCandidateAt(px, SNAP_STEP);
}

export function snapLadder(maxPx: number): number[] {
  const rungs: number[] = [];
  for (let p = SNAP_STEP; p <= maxPx; p += SNAP_STEP) rungs.push(p);
  return rungs;
}

export function openClamp(w: number): number {
  return Math.max(PANE_OPEN_MIN, w);
}

export function travelSnapAt(startPx: number, intended: number, step: number): number | null {
  const startRung = snapCandidateAt(startPx, step);
  const rung = snapCandidateAt(intended, step);
  return rung !== null && rung !== startRung ? rung : null;
}

export type PaneZone = "left" | "right" | "main" | "bottom";

/** a pane's edge marker - the three pinned zones (a main pane has none) */
export type PaneEdge = "left" | "right" | "bottom";

export function zoneOfEdge(edge: PaneEdge | null | undefined): PaneZone {
  return edge ?? "main";
}

/** the two narrow columns; a sidebar is a stack, never a row */
export function isSidebarZone(zone: PaneZone): boolean {
  return zone === "left" || zone === "right";
}

export type SplitDirection = "row" | "column";

/**
 * THE SHAPE LAW (the pane side): may a pane in `zone` split in `direction`? A sidebar
 * STACKS - one column of content - and never puts two columns side by side: the shell's
 * two columns ARE the two sidebars.
 */
export function splitFitsZone(zone: PaneZone, direction: SplitDirection): boolean {
  return direction === "column" || !isSidebarZone(zone);
}

/** the zone's name in the UI's voice, for refusal lines ("primary sidebar", never "left") */
export function zoneLabel(zone: PaneZone): string {
  switch (zone) {
    case "left":
      return "primary sidebar";
    case "right":
      return "secondary sidebar";
    case "main":
      return "main pane";
    case "bottom":
      return "bottom pane";
  }
}