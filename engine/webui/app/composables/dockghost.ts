// omega webui: the ghost preview (v0.14.4 milestone 5, ruling 5). ONE pure
// formula renders the before-drop result - ghostPreview delegates to
// predictDockDrop, the same function the commit runs; a refusal comes back
// with its reason so the overlay hatches red BEFORE the drop lands. The
// split's halves are exact by the insert law: insertPane always mints the
// fresh 2-child split at 0.5/0.5, so the measured axis px halves cleanly.
// The pointer->zone math and the drag gesture record stay in dockdnd.ts;
// the divider's snap-readout helper lives here with the ghost numerals.
import { predictDockDrop, type DockDropPrediction } from "./dock.ts";
import { zoneSplitOf, type DragZone } from "./dockdnd.ts";
import type { DockNode } from "./dock.ts";

export interface GhostHalf {
  px: number;
  pct: number;
}

export interface GhostPreview {
  targetPaneId: string;
  zone: DragZone;
  /** the landing's split direction; null = center (a plain group move) */
  direction: "row" | "column" | null;
  /** the predictor's refusal reason - the overlay hatches red on it. The
      ghost and the commit run the SAME call, so a refusal here is a
      refusal at release (ruling 5's load-bearing law). */
  refused: string | null;
  /** the fresh split's halves in measured axis px (null: an unmeasured
      pane, or a center drop - no split, no halves) */
  halves: GhostHalf[] | null;
}

/** PURE ghost read: the exact result a release would land. `axisPx` is the
    TARGET pane's measured body size along the zone's split axis (width for
    left/right, height for top/bottom) - null leaves the halves unrendered
    rather than guessing. Pure. */
export function ghostPreview(
  tree: DockNode,
  fromPaneId: string,
  fromIndex: number,
  toPaneId: string,
  zone: DragZone,
  axisPx: number | null,
): GhostPreview {
  const p: DockDropPrediction = predictDockDrop(tree, fromPaneId, fromIndex, toPaneId, zone);
  const direction = zoneSplitOf(zone)?.direction ?? null;
  if ("refused" in p) {
    return { targetPaneId: toPaneId, zone, direction, refused: p.reason, halves: null };
  }
  if (!direction) {
    return { targetPaneId: toPaneId, zone, direction: null, refused: null, halves: null };
  }
  const halves =
    axisPx && axisPx > 0 && Number.isFinite(axisPx)
      ? (() => {
          const first = Math.round(axisPx / 2);
          return [
            { px: first, pct: 0.5 },
            { px: axisPx - first, pct: 0.5 },
          ];
        })()
      : null;
  return { targetPaneId: toPaneId, zone, direction, refused: null, halves };
}

// --- the gesture rect registry (ruling 5: snapshotted at dragstart) ----------
// One page-lifetime record per gesture: each pane stamps its body rect the
// first time the gesture touches it (dragstart for the source, first
// dragover for the rest); the values hold because no op mutates the tree
// mid-gesture, so every preview reads the SAME geometry the drop will land
// on. DocksGhost reads this for the hover pane; the drop handlers re-derive
// from the live DOM as the source of truth.

interface GhostRectEntry {
  source: { paneId: string; index: number } | null;
  rects: Map<string, { x: number; y: number; w: number; h: number }>;
}

let registry: GhostRectEntry | null = null;

export const ghostRegistry = {
  /** the gesture began: the SOURCE stamps first (its dragstart) */
  begin(paneId: string, index: number): void {
    registry = { source: { paneId, index }, rects: new Map() };
  },
  /** stamp one pane's body rect (lazily at first touch - the layout is
      frozen during a gesture, so first-touch == dragstart geometry) */
  record(paneId: string, rect: { x: number; y: number; w: number; h: number }): void {
    if (!registry) return;
    if (!registry.rects.has(paneId)) registry.rects.set(paneId, rect);
  },
  current(): GhostRectEntry | null {
    return registry;
  },
  end(): void {
    registry = null;
  },
};

// --- the divider's snap readout (ruling 5: px/% numerals near snap rungs) ---

export interface ResizeReadout {
  /** the controlled pane's live px (the pure drag math's own value) */
  px: number;
  /** its share of the content box, 0..1 */
  pct: number;
  /** the snap rung the drag sits inside (null = free drag) */
  rung: number | null;
  /** the one-line badge text */
  text: string;
}

/** PURE readout formatter: the numerals under the divider's drag. Pure. */
export function resizeReadout(px: number, contentPx: number, step: number): ResizeReadout {
  const pct = contentPx > 0 && Number.isFinite(contentPx) ? px / contentPx : 0;
  const n = Math.round(px / step);
  const cand = n >= 1 ? n * step : null;
  const rung = cand !== null && Math.abs(px - cand) <= 15 ? cand : null;
  const base = `${px}px (${Math.round(pct * 100)}%)`;
  return {
    px,
    pct,
    rung,
    text: rung !== null ? `${base} \u2192 snap ${rung}px` : base,
  };
}