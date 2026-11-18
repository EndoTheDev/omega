// must agree with the shell's NARROW_MAX_PX = 360
// (webui/app/composables/narrow.ts) - pinned by webui/test/modulenarrow.test.ts.

/** narrow below this rendered width */
export const NARROW_MAX_PX = 360;

export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}
