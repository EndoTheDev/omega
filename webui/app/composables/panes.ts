// omega webui: pure pane math shared by the divider and the app shell - the
// WIDTH ladders below, and the pane-side ZONE vocabulary at the end of the
// file (canvas C1: the class law's seat map speaks it).
// Snap geometry from the dock design round (pre-dock quick win): releasing a
// sidebar drag inside a 15px attract zone sticks the pane to the exact snap
// size. Snap is an attractor, NOT a clamp - dragging outside the zones stays
// free and the 128px drag floor stands.

// card t_f6d5a771: the snap ladder is every +320 from 320 (320, 640, 960,
// 1280, ... unlimited - the attract zone rides each rung, generated, not a
// fixed list; window px bounds it in practice).
export const SNAP_STEP = 320;
export const SNAP_ZONE = 15;

// open-clamp: a collapsed sidebar with a stale sub-floor width reopens at
// this width (manual resize below it still works afterwards)
export const PANE_OPEN_MIN = 320;

// card t_67fc1905: the bottom panel's ladder is every +200 (the recorded
// choice over +160). The panel's practical heights run ~160-480px, where
// 200px rungs land meaningfully; the sidebars' +320 rungs would rarely be
// hittable on a horizontal pane. Fresh bottom panels open ON rung 200.
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

/** the ladder's rungs up to maxPx, inclusive - tests and callers that want
    the target set; snapCandidate itself consults the generated formula */
export function snapLadder(maxPx: number): number[] {
  const rungs: number[] = [];
  for (let p = SNAP_STEP; p <= maxPx; p += SNAP_STEP) rungs.push(p);
  return rungs;
}

/** floors a pane width for reopening from collapsed state */
export function openClamp(w: number): number {
  return Math.max(PANE_OPEN_MIN, w);
}

/** t_f7201e02 travel magnet: the rung a drag is attracted to. A start px
    already sitting inside a rung's zone is a PASSIVE hold - a previous
    gesture parked the divider at a free width, nobody traveled there - so
    snapping it on grab would teleport on the first move. The magnet only
    engages when the drag BRINGS the intended px into a zone (a rung the
    start was not already held by): the fixed lanes' no-jump-on-grab feel.
    Returns the rung to attract to, or null (follow the pointer). */
export function travelSnapAt(startPx: number, intended: number, step: number): number | null {
  const startRung = snapCandidateAt(startPx, step);
  const rung = snapCandidateAt(intended, step);
  return rung !== null && rung !== startRung ? rung : null;
}

// --- THE PANE ZONES (canvas C1, the class law) -------------------------------
//
// A pane's seat in the shell, in the vocabulary the layout record persists
// (a pane's `edge` marker IS its zone; an unmarked pane is the main column).
// The class law (dockables.ts) names the same four seats after their VIEW
// classes - primary sidebar, secondary sidebar, main, bottom - and decides
// which of them a view may occupy. This file owns only the pane side: which
// zone a pane is, given its marker. PURE, no imports.

/** the four seats of the shell. `left` is the PRIMARY sidebar (sessions'
    home), `right` the SECONDARY (the inspector views), `bottom` the
    horizontal panel; a pane carrying no marker is the MAIN zone. */
export type PaneZone = "left" | "right" | "main" | "bottom";

/** a pane's edge marker - the three pinned zones (a main pane has none) */
export type PaneEdge = "left" | "right" | "bottom";

/** the zone a pane's edge marker names. An unmarked pane is main - the
    default column everything else is laid out around. */
export function zoneOfEdge(edge: PaneEdge | null | undefined): PaneZone {
  return edge ?? "main";
}

/** the two narrow columns (the sidebars): a zone test the class law and the
    shape law share - a sidebar is a stack, never a row. */
export function isSidebarZone(zone: PaneZone): boolean {
  return zone === "left" || zone === "right";
}

/** the zone's name in the UI's voice - a refusal line names the seat it
    refused ("main pane", "primary sidebar"), never a raw marker */
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