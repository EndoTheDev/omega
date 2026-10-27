// omega webui: the divider drag's pointer bookkeeping, as a pure reducer
// (card t_38c0e960). dragSplitDivider's deltaPx is CUMULATIVE from the
// measured baseline (req = startPx[k] + delta): DockSplit measures ONCE per
// gesture and every move carries raw - base. The v0.9.0 rework sent
// per-event increments instead - each move reset the pane to baseline plus
// one increment and the divider looked dead. This reducer owns that
// reduction so unit tests receive the shape production actually sends
// (the divider's emit: a delta cumulative from its pointerdown origin,
// plus the raw pointer position).

export interface DividerPointerState {
  /** the raw position the measured baseline belongs to; null = no gesture */
  base: number | null;
}

/** Reduces one divider emit to the cumulative delta the store math wants
    (null = nothing to move). Mutates `state.base`:
    - `rebase` (a squeeze push-back re-measured the layout): the baseline is
      this raw's geometry, deltas resume from here - no move this event.
    - gesture start (base null): the baseline is at-rest geometry and the
      emitted delta is cumulative from the divider's pointerdown origin -
      apply it now and adopt the origin as the base. */
export function dividerDelta(
  state: DividerPointerState,
  delta: number,
  raw: number,
  rebase: boolean,
): number | null {
  if (rebase) {
    state.base = raw;
    return null;
  }
  if (state.base === null) {
    state.base = raw - delta;
    return delta;
  }
  return raw - state.base;
}