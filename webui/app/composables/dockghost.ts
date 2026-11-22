// omega webui: the ghost preview.
import { predictDockDrop, type DockDropPrediction } from "./dock.ts";
import { onGestureEnd, zoneSplitOf, type DragZone } from "./dockdnd.ts";
import type { DockNode } from "./dock.ts";

export interface GhostHalf {
  px: number;
  pct: number;
}

export interface GhostPreview {
  targetPaneId: string;
  zone: DragZone;
  direction: "row" | "column" | null;
  refused: string | null;
  halves: GhostHalf[] | null;
}

/** PURE ghost read: the exact result a release would land. */
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

// one page-lifetime record per gesture.

interface GhostRectEntry {
  source: { paneId: string; index: number } | null;
  rects: Map<string, { x: number; y: number; w: number; h: number }>;
}

let registry: GhostRectEntry | null = null;

// the registry joins the COMPOSABLE-owned gesture end. (It also clears on the next
// begin, so a poisoned record never reaches the next gesture either way.)
onGestureEnd(() => {
  registry = null;
});

export const ghostRegistry = {
  /** the gesture began: the SOURCE stamps first (its dragstart) */
  begin(paneId: string, index: number): void {
    registry = { source: { paneId, index }, rects: new Map() };
  },
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
