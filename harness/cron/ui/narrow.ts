// omega webui: the module-UI narrow recipe's module-owned threshold and pure. The
// threshold must agree with the shell's NARROW_MAX_PX = 360
// (webui/app/composables/narrow.ts) - pinned by webui/test/modulenarrow.test.ts.

/** narrow below this rendered width */
export const NARROW_MAX_PX = 360;

/** narrow below this rendered width; zero (unmounted measure noise) never is */
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}