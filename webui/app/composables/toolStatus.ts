// omega webui: the tool-status vocabulary.

// isLive: the call is in flight - its result has not landed yet. A PENDING call counts:
// it was dispatched, its result re-enters as a later wake turn, the contract is not
// finished.
export function isLive(status: string): boolean {
  return status === "running" || status === "pending";
}

// isStreaming: the narrower set - this call's bytes are arriving in THIS
// process right now (live set minus the yielded half).
//
// This is the ONE deliberate divergence from isLive, and it is a named
// decision, never drift. Two consumers need it:
// the activity line narrates what the run is DOING at this instant
//     ("running shell.run"); a yielded call emits NO delta until its wake
//     turn arrives, so narrating it would claim work with no bytes behind
//     it (audit F9 - the phase reads "thinking" instead, which is true);
// the gate binding attaches a pending verdict card to the row whose
//     command is streaming now, not to a chip that already ran.
// Any other consumer that reaches for this half must say why in a comment
// next to the call site; isLive is the default, not this.
export function isStreaming(status: string): boolean {
  return status === "running";
}

// isYielded: the OTHER named half. A status that awaits a wake but is not `pending`
// must land HERE, not in a fifth inline comparison.
export function isYielded(status: string): boolean {
  return status === "pending";
}
