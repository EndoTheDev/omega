// omega webui: the tool-status vocabulary.

// isLive: the call is in flight, its result not landed yet - a PENDING call counts.
export function isLive(status: string): boolean {
  return status === "running" || status === "pending";
}

// isStreaming: this call's bytes are arriving NOW (the live set minus the yielded half)
// - the ONE deliberate divergence from isLive, never drift: a yielded call emits no
// delta until its wake turn, so narrating it would claim work with no bytes behind it.
export function isStreaming(status: string): boolean {
  return status === "running";
}

// isYielded: the other named half - a status awaiting a wake that is not `pending`
// lands here, never a fifth inline comparison.
export function isYielded(status: string): boolean {
  return status === "pending";
}
