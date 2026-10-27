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