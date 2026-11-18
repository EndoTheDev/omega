// omega webui: the composer's progressive collapse ladder
// the Hermes-desktop composer pattern, Endo's ruling ).
//
// THE LADDER - one rung per width band, in order, never two at once:
//
// full the model chip shows its WHOLE label, one toolbar row
// icon the label is DELIBERATELY ABSENT (the chevron stays), one row
//   stacked the model + effort chips take row one, send keeps row two
//
// No rung truncates. The pre-ladder squeeze shrank the chip first and the
// label died mid-word ("deepse.."), then the row wrapped by accident and
// stranded controls on a second line (composer 82px -> 112px). A label now
// fits whole or is gone - the whole law, no ellipsis anywhere.
//
// WHY JS AND NOT CONTAINER QUERIES: the shed rung depends on the label's
// OWN cost, and the label is DATA (a model name of any length). A CSS
// breakpoint would have to bake one label's width in, and the next model
// would overflow the rung it was tuned for. So the fit is measured, the
// same house pattern as composables/narrow.ts (ResizeObserver + one pure
// predicate the node suite can call directly). The constants below are the
// box costs around the label; the label, the effort chip and the send come
// from the live DOM (readComposerRow).
//
// MEASURED BUDGETS (dev lane, 1440x900, model "deepseek-v4.1-flash",
// effort "default", send "send", JetBrains Mono 12px):
//
// model chip, whole label..... 178.8 (label 136.8 + 42 chrome)
// model chip, icon only...... 36.0 (the 6px seat left of the
//                                             label dies with the label)
// effort chip, "default"..... 92.4 (label 50.4 + 42 chrome)
// send, "send".......... 62.3 (padding 6/16, no border)
// toolbar gap........... 8
// toolbar padding......... 8 + 8
// composer inset / border..... 12 / 1
//
// so the one-row bands need (the number is the composer's own client
// width: pane - 24 inset - 2 border):
//
// full: 136.8 + 42 + 8 + 92.4 + 8 + 62.3 + 16 + 8 slack = 373.5 -> 374
// icon: 36.0 + 8 + 92.4 + 8 + 62.3 + 16 + 8 slack = 230.7 -> 231
// stack: max(36.0 + 8 + 92.4, 62.3) + 16 + 8 slack = 160.4 -> 161
//
// and the ladder ENDS there: the stacked rung holds down to 161px, which
// is narrower than any real surface (a 200px pane gives the composer 176,
// the mobile drawer floors it at 302). A fourth rung - Hermes folds its
// secondary toggles into a menu at 260px - has nothing to eat here, so
// omega does not get one. Re-probe before adding it.

import {
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
  type Ref,
} from "vue";

/** the ladder's rungs, narrowest last */
export type ComposerStage = "full" | "icon" | "stacked";

// --- the measured box costs (see the table above) ---
/** the model chip around a full label: 14+14 padding, 6 gap, 6 chevron, 2 border */
export const MODEL_CHIP_FULL_CHROME_PX = 42;
/** the model chip around the chevron alone: the label's 6px gap seat goes too */
export const MODEL_CHIP_ICON_PX = 36;
/** between the model chip and the effort chip inside.model-group */
export const MODEL_GROUP_GAP_PX = 8;
/** the toolbar row's item gap */
export const TOOLBAR_GAP_PX = 8;
/** the toolbar row's own left/right padding, one side */
export const TOOLBAR_PAD_PX = 8;
/**
 * the fit test keeps one toolbar gap of air: a stage never lands on a pixel-exact fit.
 */
export const LADDER_SLACK_PX = 8;

/**
 * the live widths the fit is computed from.
 * `rest` is everything on the row besides.model-group (the send, plus the expanded
 * seat's + when fullscreen mounts it into the toolbar) - both are measured, never
 * assumed, because the send's face morphs send <-> stop and the + comes and goes with
 * the expanded state.
 */
export interface ComposerRowCosts {
  label: number;
  effort: number;
  rest: number;
  gaps: number;
}

/**
 * THE LADDER's band edges, in the same px unit as `composerStageFor` (the composer's
 * own client width).
 */
export interface ComposerStageBands {
  full: number;
  icon: number;
  stacked: number;
}

/** the three band edges for a row that currently costs `costs` */
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

/**
 * THE LADDER, pure: which rung a composer of `container` px (its own client width).
 * Monotone in width, so a drag crosses one rung at a time - never two firing at once.
 */
export function composerStageFor(
  container: number,
  costs: ComposerRowCosts,
): ComposerStage {
  const bands = composerStageBands(costs);
  if (container >= bands.full) return "full";
  if (container >= bands.icon) return "icon";
  return "stacked";
}

/**
 * the row's measured costs, or null while the composer is not mounted (or a host
 * stripped a chip - then the caller keeps the rung it has)
 */
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
 * the live rung for a composer root: measures now, then on every resize (the pane drag,
 * the drawer, the fullscreen toggle) - and re-measures whenever `touch` changes, which
 * the caller bumps after a label swap (the model / effort props) since those change the
 * row's cost without changing its width.
 * Delivered on the next frame, never inside the observer's own callback: the rung flips
 * the row's layout, and applying it synchronously re-enters the resize cycle (the
 * benign "ResizeObserver loop" console error - same guard as narrow.ts).
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
