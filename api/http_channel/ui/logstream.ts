// omega logs tab: the pushed tail's transport shim (card t_0e7bc0bb).
//
// The shell owns the page's ONE websocket (`webui/app/composables/socket.ts`)
// and the `logs` topic is subscription-gated: only a mounted logs pane
// wants a high-frequency tail. Module UI imports no shell code, so the
// two directions meet on window CustomEvents - the same seam the kanban
// board uses for `omega-select-board` (the literals are duplicated on
// purpose on both sides; `webui/test/socket.test.ts` pins the pair).
//
//   pane -> shell: LOG_SUBSCRIBE_EVENT, detail {on: boolean}
//   shell -> pane: LOG_STREAM_EVENT, detail {lines: string[]} to append,
//                  or {reset: true} after a reconnect
//
// The pane's FIRST read stays REST (GET /logs?lines=200): the socket
// carries only what was appended after it, never a snapshot.
export const LOG_SUBSCRIBE_EVENT = "omega-logs-subscribe";
export const LOG_STREAM_EVENT = "omega-log-stream";

// MAX_LINES bounds the rendered tail. The snapshot is 200 lines and the
// pane appends for as long as it stays mounted, so the buffer is trimmed
// from the head at 10x the snapshot - a pane left open all day does not
// grow the DOM without bound.
export const MAX_LINES = 2000;

// SNAPSHOT_OVERLAP is the positional bound for the ONE merge that has a
// real overlap: the one right after a REST snapshot read. The pane
// subscribes before it reads, so the pushes that landed while the read
// was in flight are in the snapshot AND in the buffer - up to the whole
// buffered run may therefore be a duplicate (mergeTail narrows the bound
// to min(base, incoming)). No other merge has one; see the delta law in
// mergeTail.
export const SNAPSHOT_OVERLAP = Number.MAX_SAFE_INTEGER;

// mergeTail folds pushed lines onto the tail the pane holds and keeps the
// last `max` of them.
//
// The overlap rule is POSITIONAL, never content-driven on its own. The
// tailer publishes byte-offset deltas (`api/server_inspector.go`
// pushLogDelta): frame N and frame N+1 are disjoint by construction and
// nothing is replayed, so two consecutive pushes can never duplicate a
// line - a line the log printed twice in a row is TWO lines, not an
// overlap to swallow. `overlapLimit` is the bound that says so: it is the
// number of leading incoming lines that may be dropped as a duplicate of
// the base's tail, and it defaults to 0 - the delta law, nothing dropped.
//
// Inside an allowed window the longest run where the base's suffix equals
// the incoming prefix is dropped, so the snapshot and the push that
// straddles it render the line once. The match never runs past the
// window, and the window only exists where position makes a duplicate
// possible, so a genuine repeat stays.
export function mergeTail(
  base: readonly string[],
  incoming: readonly string[],
  max = MAX_LINES,
  overlapLimit = 0,
): string[] {
  let skip = 0;
  // an overlap is at most min(base, incoming) lines; the caller's window
  // can only narrow that, never widen it
  const limit = Math.floor(Math.min(Math.max(0, overlapLimit), base.length, incoming.length));
  for (let n = limit; n > 0; n--) {
    let same = true;
    for (let i = 0; i < n; i++) {
      if (base[base.length - n + i] !== incoming[i]) {
        same = false;
        break;
      }
    }
    if (same) {
      skip = n;
      break;
    }
  }
  const merged = base.concat(incoming.slice(skip));
  return merged.length > max ? merged.slice(merged.length - max) : merged;
}

// logStreamDetail is the shell -> pane payload shape.
export interface LogStreamDetail {
  lines?: string[];
  reset?: boolean;
}
