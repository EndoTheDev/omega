// NARROW_MAX_PX must agree with the shell's copy (webui/app/composables/narrow.ts); webui/test/modulenarrow.test.ts pins every copy.

export const NARROW_MAX_PX = 360;

export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}
