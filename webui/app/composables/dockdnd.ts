// tab DnD gesture state + docking-zone prediction. The DOM hides the dragged data from
// dragover (only `types` are readable), but the hit-test needs the dragged tab's identity
// DURING the gesture.
import { seatAllowsIn, type DockComponentName } from "./dockables.ts";
import { isSidebarZone, zoneOfEdge, type PaneZone } from "./panes.ts";

export interface TabDragInfo {
  paneId: string;
  index: number;
  component: DockComponentName;
}

let current: TabDragInfo | null = null;

type GestureEndHandler = () => void;
const gestureEndHandlers = new Set<GestureEndHandler>();

/** subscribe to every gesture end (dragend / drop / Esc / blur, or a layout swap) */
export function onGestureEnd(handler: GestureEndHandler): () => void {
  gestureEndHandlers.add(handler);
  return () => {
    gestureEndHandlers.delete(handler);
  };
}

/**
 * the ONE global clear: the record dies and every subscriber rings. Idempotent - running
 * with nothing armed still notifies, so a stale highlight never outlives its gesture.
 */
export function endGesture(): void {
  current = null;
  for (const handler of [...gestureEndHandlers]) handler();
}

interface GuardTarget {
  addEventListener(type: string, listener: (e: Event) => void): void;
  removeEventListener(type: string, listener: (e: Event) => void): void;
}
let guardTarget: GuardTarget | null =
  typeof window !== "undefined" ? (window as unknown as GuardTarget) : null;
let installedAt: GuardTarget | null = null;

/** tests inject a stub target; a target swap re-installs exactly once */
export function setGestureTarget(target: GuardTarget | null): void {
  if (installedAt && installedAt !== (target as unknown as GuardTarget | null)) {
    installedAt.removeEventListener("dragend", onGuardEnd);
    installedAt.removeEventListener("drop", onGuardEnd);
    installedAt.removeEventListener("keydown", onGuardKey);
    installedAt.removeEventListener("blur", onGuardEnd);
    installedAt = null;
  }
  guardTarget = target;
  ensureGuard();
}

function ensureGuard(): void {
  if (!guardTarget || installedAt === guardTarget) return;
  guardTarget.addEventListener("dragend", onGuardEnd);
  guardTarget.addEventListener("drop", onGuardEnd);
  guardTarget.addEventListener("keydown", onGuardKey);
  guardTarget.addEventListener("blur", onGuardEnd);
  installedAt = guardTarget;
}
function onGuardEnd(): void {
  endGesture();
}
function onGuardKey(e: Event): void {
  if ((e as KeyboardEvent).key === "Escape" && current) endGesture();
}

export const tabDrag = {
  current(): TabDragInfo | null {
    return current;
  },
  /**
   * a gesture begins: any stale record dies first, so a new gesture never inherits a
   * poisoned hit-test.
   */
  start(info: TabDragInfo): void {
    endGesture();
    current = info;
    ensureGuard();
  },
  end(): void {
    endGesture();
  },
};

/**
 * PURE hit-test: may the active drag drop into a pane whose edge is `targetEdge`? THE ZONE
 * LAW answers: the dragged view's CLASS decides which seats it may occupy.
 */
export function dropAllowedFor(
  targetEdge: "left" | "right" | "bottom" | null,
  drag: TabDragInfo | null,
): boolean {
  if (!drag) return false;
  return seatAllowsIn(drag.component, zoneOfEdge(targetEdge));
}

export type DragZone = "left" | "right" | "top" | "bottom" | "center";

export interface ZoneRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** the middle 50%-of-axis band whose overlap on BOTH axes reads "center" */
export const ZONE_CENTER_BAND = 0.25;

export function zoneForPoint(x: number, y: number, rect: ZoneRect): DragZone {
  if (!(rect.w > 0) || !(rect.h > 0)) return "center";
  const rx = Math.min(1, Math.max(0, (x - rect.x) / rect.w));
  const ry = Math.min(1, Math.max(0, (y - rect.y) / rect.h));
  const inX = rx >= ZONE_CENTER_BAND && rx <= 1 - ZONE_CENTER_BAND;
  const inY = ry >= ZONE_CENTER_BAND && ry <= 1 - ZONE_CENTER_BAND;
  if (inX && inY) return "center";
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

/** the overlay rect of a zone as FRACTIONS of the pane body */
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

/**
 * PURE pointer -> zone, BOUNDED by the target pane's own zone: a sidebar's x axis is not part
 * of the gesture, so the left/right halves that would split it into columns cannot be
 * entered. The split-sidebar gesture is gone, not refused.
 */
export function zoneForPointer(
  zone: PaneZone,
  x: number,
  y: number,
  rect: ZoneRect,
): DragZone {
  if (!isSidebarZone(zone)) return zoneForPoint(x, y, rect);
  return zoneForPoint(rect.x + rect.w / 2, y, rect);
}

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