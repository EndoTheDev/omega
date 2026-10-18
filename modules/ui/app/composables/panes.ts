// omega webui: pure pane-width math shared by the divider and the app shell.
// Snap geometry from the dock design round (pre-dock quick win): releasing a
// sidebar drag inside a 15px attract zone sticks the pane to the exact snap
// size. Snap is an attractor, NOT a clamp - dragging outside the zones stays
// free and the 128px drag floor stands.

export const SNAP_POINTS = [320, 640];
export const SNAP_ZONE = 15;

// open-clamp: a collapsed sidebar with a stale sub-floor width reopens at
// this width (manual resize below it still works afterwards)
export const PANE_OPEN_MIN = 320;

/** returns the exact snap size when the drag is inside a zone, else null */
export function snapCandidate(w: number): number | null {
  for (const p of SNAP_POINTS) {
    if (Math.abs(w - p) <= SNAP_ZONE) return p;
  }
  return null;
}

/** floors a pane width for reopening from collapsed state */
export function openClamp(w: number): number {
  return Math.max(PANE_OPEN_MIN, w);
}