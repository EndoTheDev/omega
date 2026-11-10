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

// mergeTail folds pushed lines onto the tail the pane holds and keeps the
// last `max` of them.
//
// The overlap rule: a pushed run can start INSIDE the snapshot (the pane
// subscribes before it re-reads, so a line written during that read is in
// both). The longest run where the base's suffix equals the incoming
// prefix is dropped, so a line is never rendered twice - and when the runs
// share nothing (the ordinary append), nothing is dropped.
export function mergeTail(base: readonly string[], incoming: readonly string[], max = MAX_LINES): string[] {
  let skip = 0;
  if (incoming.length) {
    const limit = Math.min(base.length, incoming.length);
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
  }
  const merged = base.concat(incoming.slice(skip));
  return merged.length > max ? merged.slice(merged.length - max) : merged;
}

// logStreamDetail is the shell -> pane payload shape.
export interface LogStreamDetail {
  lines?: string[];
  reset?: boolean;
}
