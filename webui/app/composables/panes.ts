// omega webui: pure pane math shared by the divider and the app shell.

// the snap ladder is every +320 from 320.
export const SNAP_STEP = 320;
export const SNAP_ZONE = 15;

// open-clamp: a collapsed sidebar with a stale sub-floor width reopens at this width.
export const PANE_OPEN_MIN = 320;

// the bottom panel's ladder is every +200 (the recorded choice over +160).
export const BOTTOM_SNAP_STEP = 200;

/** returns the exact snap size when the drag is inside a zone, else null */
export function snapCandidateAt(px: number, step: number): number | null {
  const n = Math.round(px / step);
  if (n < 1) return null;
  const p = n * step;
  return Math.abs(px - p) <= SNAP_ZONE ? p : null;
}

/** the sidebars' +320 ladder (the original voice snapCandidate keeps) */
export function snapCandidate(px: number): number | null {
  return snapCandidateAt(px, SNAP_STEP);
}

/**
 * the ladder's rungs up to maxPx, inclusive - tests and callers that want the target.
 */
export function snapLadder(maxPx: number): number[] {
  const rungs: number[] = [];
  for (let p = SNAP_STEP; p <= maxPx; p += SNAP_STEP) rungs.push(p);
  return rungs;
}

/** floors a pane width for reopening from collapsed state */
export function openClamp(w: number): number {
  return Math.max(PANE_OPEN_MIN, w);
}

/** travel magnet: the rung a drag is attracted to. */
export function travelSnapAt(startPx: number, intended: number, step: number): number | null {
  const startRung = snapCandidateAt(startPx, step);
  const rung = snapCandidateAt(intended, step);
  return rung !== null && rung !== startRung ? rung : null;
}

// THE PANE ZONES (canvas C1, the class law) -------------------------------.

/** the four seats of the shell. */
export type PaneZone = "left" | "right" | "main" | "bottom";

/** a pane's edge marker - the three pinned zones (a main pane has none) */
export type PaneEdge = "left" | "right" | "bottom";

/** the zone a pane's edge marker names. */
export function zoneOfEdge(edge: PaneEdge | null | undefined): PaneZone {
  return edge ?? "main";
}

/**
 * the two narrow columns (the sidebars): a zone test the class law and the shape law
 * share - a sidebar is a stack, never a row.
 */
export function isSidebarZone(zone: PaneZone): boolean {
  return zone === "left" || zone === "right";
}

/** a split's axis, in the dock's vocabulary. */
export type SplitDirection = "row" | "column";

/**
 * canvas C2 THE SHAPE LAW (the pane side): may a pane in `zone` split in `direction`?
 * A sidebar STACKS - one column of content, tabs above it - and never puts two columns
 * side by side: the shell's two columns ARE the two sidebars.
 */
export function splitFitsZone(zone: PaneZone, direction: SplitDirection): boolean {
  return direction === "column" || !isSidebarZone(zone);
}

/**
 * the zone's name in the UI's voice - a refusal line names the seat it refused ("main
 * pane", "primary sidebar"), never a raw marker / /
 */
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