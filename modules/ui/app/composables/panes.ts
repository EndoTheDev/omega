// omega webui: pure pane-width math shared by the divider and the app shell.
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

/** returns the exact snap size when the drag is inside a zone, else null */
export function snapCandidate(w: number): number | null {
  const n = Math.round(w / SNAP_STEP);
  if (n < 1) return null;
  const p = n * SNAP_STEP;
  return Math.abs(w - p) <= SNAP_ZONE ? p : null;
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