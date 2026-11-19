export function isLive(status: string): boolean {
  return status === "running" || status === "pending";
}

// isStreaming: bytes are arriving NOW - the ONE deliberate divergence from isLive (a
// yielded call emits no delta until its wake turn, so narrating it would claim work with
// no bytes behind it).
export function isStreaming(status: string): boolean {
  return status === "running";
}

export function isYielded(status: string): boolean {
  return status === "pending";
}
