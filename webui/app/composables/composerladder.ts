// THE LADDER, one rung per width band, never two at once:
//   full    the model chip shows its WHOLE label, one toolbar row
//   icon    the label is DELIBERATELY ABSENT (the chevron stays), one row
//   stacked the model + effort chips take row one, send keeps row two
// No rung truncates: a squeezed label died mid-word and a wrapped row stranded controls
// on a second line, so a label fits whole or is gone - no ellipsis anywhere.
//
// WHY JS, NOT CONTAINER QUERIES: the shed rung depends on the label's OWN cost, and the
// label is DATA (a model name of any length), so a breakpoint would bake one label's
// width in and the next model would overflow the rung tuned for it. The fit is measured
// instead - the narrow.ts pattern: ResizeObserver + one pure predicate the node suite
// calls directly.
// The ladder ENDS at stacked: it holds narrower than any real surface, so a fourth rung
// would have nothing to eat.

import {
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  type Ref,
} from "vue";

export type ComposerStage = "full" | "icon" | "stacked";

/** the model chip around a full label: 14+14 padding, 6 gap, 6 chevron, 2 border */
export const MODEL_CHIP_FULL_CHROME_PX = 42;
/** the model chip around a chevron alone: the label's 6px gap seat goes too */
export const MODEL_CHIP_ICON_PX = 36;
export const MODEL_GROUP_GAP_PX = 8;
export const TOOLBAR_GAP_PX = 8;
export const TOOLBAR_PAD_PX = 8;
/** one toolbar gap of air, so a stage never lands on a pixel-exact fit */
export const LADDER_SLACK_PX = 8;

/**
 * `rest` is everything on the row besides .model-group (send, plus the expanded seat's +):
 * measured, never assumed, because the send morphs send <-> stop and the + comes and goes.
 */
export interface ComposerRowCosts {
  label: number;
  effort: number;
  rest: number;
  gaps: number;
}

/** THE LADDER's band edges, in the same px unit as `composerStageFor` */
export interface ComposerStageBands {
  full: number;
  icon: number;
  stacked: number;
}

export function composerStageBands(
  costs: ComposerRowCosts,
): ComposerStageBands {
  const rowChrome =
    costs.gaps * TOOLBAR_GAP_PX + TOOLBAR_PAD_PX * 2 + LADDER_SLACK_PX;
  return {
    full:
      costs.label +
      MODEL_CHIP_FULL_CHROME_PX +
      MODEL_GROUP_GAP_PX +
      costs.effort +
      costs.rest +
      rowChrome,
    icon:
      MODEL_CHIP_ICON_PX +
      MODEL_GROUP_GAP_PX +
      costs.effort +
      costs.rest +
      rowChrome,
    // stacked: the model + effort pair is row one's widest item, so the send's width.
    stacked:
      Math.max(MODEL_CHIP_ICON_PX + MODEL_GROUP_GAP_PX + costs.effort, costs.rest) +
      TOOLBAR_PAD_PX * 2 +
      LADDER_SLACK_PX,
  };
}

/** THE LADDER, pure: the rung for a composer of `container` px (its own client width); monotone, so a drag crosses one rung at a time */
export function composerStageFor(
  container: number,
  costs: ComposerRowCosts,
): ComposerStage {
  const bands = composerStageBands(costs);
  if (container >= bands.full) return "full";
  if (container >= bands.icon) return "icon";
  return "stacked";
}

export function readComposerRow(
  root: HTMLElement | null,
): { container: number; costs: ComposerRowCosts } | null {
  if (!root) return null;
  const toolbar = root.querySelector<HTMLElement>(".toolbar");
  const label = root.querySelector<HTMLElement>(
    ".model-group .model-select:not(.band-select) .chip-name",
  );
  const effort = root.querySelector<HTMLElement>(
    ".model-group .thinking-select .model-chip",
  );
  if (!toolbar || !label || !effort) return null;
  const kids = Array.from(toolbar.children).filter(
    (el): el is HTMLElement =>
      el instanceof HTMLElement && el.getClientRects().length > 0,
  );
  let rest = 0;
  for (const el of kids) {
    if (el.classList.contains("model-group")) continue;
    rest += el.getBoundingClientRect().width;
  }
  return {
    container: root.clientWidth,
    costs: {
      label: label.getBoundingClientRect().width,
      effort: effort.getBoundingClientRect().width,
      rest,
      gaps: Math.max(0, kids.length - 1),
    },
  };
}

/**
 * The live rung for a composer root: measures now and on every resize, and again when
 * `touch` changes (the caller bumps it after a label swap). Delivered on the next frame,
 * never inside the observer callback: the rung flips the row's layout, so applying it
 * synchronously re-enters the resize cycle (the "ResizeObserver loop" error - same guard
 * as narrow.ts).
 */
export function useComposerLadder(
  root: Ref<HTMLElement | null>,
  touch: Ref<unknown>,
): Ref<ComposerStage> {
  const stage = ref<ComposerStage>("full");
  let observer: ResizeObserver | null = null;
  let tick = 0;

  function apply(): void {
    const row = readComposerRow(root.value);
    if (!row || row.container <= 0) return;
    const next = composerStageFor(row.container, row.costs);
    if (next !== stage.value) stage.value = next;
  }
  function schedule(): void {
    const me = ++tick;
    requestAnimationFrame(() => {
      if (me === tick) apply();
    });
  }
  function observe(node: HTMLElement | null): void {
    observer?.disconnect();
    observer = null;
    if (!node || typeof ResizeObserver === "undefined") {
      apply();
      return;
    }
    observer = new ResizeObserver(schedule);
    observer.observe(node);
    apply();
  }

  onMounted(() => observe(root.value));
  // the root remounts freely (tab switches keep the draft, not the DOM)
  watch(root, (node) => observe(node));
  watch(touch, () => nextTick(schedule));
  onBeforeUnmount(() => {
    tick++;
    observer?.disconnect();
    observer = null;
  });
  return stage;
}
