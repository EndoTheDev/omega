// omega webui: the module-UI narrow recipe's module-owned threshold and
// pure comparison (card t_a2fdc630; the Board/boardLogic.ts precedent).
// A module component measures its OWN rendered container - not the
// viewport media query - and flips a `narrow` class below the
// threshold. The observer wiring lives inline in the component (Board
// precedent: the .vue carries the only DOM-measuring part, this file is
// import-safe for the node suite).
//
// The threshold must agree with the shell's NARROW_MAX_PX = 360
// (engine/webui/app/composables/narrow.ts) - pinned by
// engine/webui/test/modulenarrow.test.ts. A component whose real minimum
// readable width differs derives its own value HERE, and documents why
// beside it.

/** narrow below this rendered width */
export const NARROW_MAX_PX = 360;

/** narrow below this rendered width; zero (unmounted measure noise) never is */
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}