// omega webui: the observability tab's narrow threshold (card t_e703180e,
// the module-UI narrow recipe t_a2fdc630). A module component measures
// its OWN rendered container - not a viewport media query - and flips a
// `narrow` class below the threshold. The observer wiring lives inline
// in ObservabilityTab.vue (the Board precedent: the .vue carries the
// only DOM-measuring part, this file stays import-safe for the node
// suite).
//
// The threshold must agree with the shell's NARROW_MAX_PX = 360
// (webui/app/composables/narrow.ts) - pinned by
// webui/test/modulenarrow.test.ts.

/** narrow below this rendered width */
export const NARROW_MAX_PX = 360;

/** narrow below this rendered width; zero (unmounted measure noise) never is */
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}
