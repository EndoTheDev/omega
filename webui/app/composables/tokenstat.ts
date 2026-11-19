// The status bar's token figure has ONE meaning everywhere: the session's
// CUMULATIVE provider-reported usage - the store adds every run's prompt and
// completion to the session row (store.AddSessionTokens), so a live run and
// its reload must render the same number. Every writer paints through here.

export interface SessionTokens {
  /** cumulative tokens used by the session: the store row's prompt + completion */
  total: number;
  /** true when `total` carries a run-time ESTIMATE instead of reported usage */
  estimated: boolean;
}

/** a store-reported total: cumulative and exact, and the base the next run counts onto. */
export function durableTokens(total: number): SessionTokens {
  return { total: Math.max(0, total), estimated: false };
}

/** a run's reported usage laid onto the session's durable total: cumulative and exact. */
export function usageTokens(
  base: number,
  promptTokens: number,
  completionTokens: number,
): SessionTokens {
  return { total: base + promptTokens + completionTokens, estimated: false };
}

/**
 * A run that has reported no usage yet: only its visible output can be counted,
 * so the cumulative figure carries an estimate - and says so.
 */
export function estimateTokens(base: number, runOutputChars: number): SessionTokens {
  return { total: base + Math.ceil(Math.max(0, runOutputChars) / 4), estimated: true };
}
