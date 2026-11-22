// omega webui: the divider drag's pointer bookkeeping, as a pure reducer. The old
// rebase arm (the squeeze push-back's re-measure) died with the chain - wall law: a cap
// is a stop, not a push, so the baseline never re-measures mid-gesture.

export interface DividerPointerState {
  base: number | null;
}

/** Reduces one divider emit to the cumulative delta the store math wants. */
export function dividerDelta(
  state: DividerPointerState,
  delta: number,
  raw: number,
): number | null {
  if (state.base === null) {
    state.base = raw - delta;
    return delta;
  }
  return raw - state.base;
}