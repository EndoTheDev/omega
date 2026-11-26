// omega webui: the terminal pane's keystroke writer.
//
// The pane's input is one HTTP POST per burst, and ORDER is the invariant: at most one
// POST is in flight, so the server reads the bytes in push order and the connection
// pool never holds two input POSTs that could land out of order. Printable ASCII
// collects under a short window; anything else (Enter, Esc, arrows, ctrl-chords, DEL, an
// IME char, a bracketed paste) leaves at once.

export interface InputWriterOptions {
  /** one POST of raw bytes; a refusal or a rejection drops that burst (never replayed) */
  send: (bytes: Uint8Array) => Promise<unknown>;
  windowMs?: number;
  maxBurst?: number;
  schedule?: (fn: () => void, ms: number) => unknown;
  cancel?: (handle: unknown) => void;
}

export interface InputWriter {
  push(s: string): void;
  /** kill, unmount, session change: pending bytes belong to a dying session, not a POST */
  reset(): void;
}

const PRINT_LO = 0x20;
const PRINT_HI = 0x7e;

/** the pane's wire mapping: one byte per UTF-16 unit, low octet */
export function strBytes(s: string): Uint8Array {
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i) & 0xff;
  return out;
}

/** a run the window may hold: printable ASCII only */
function printable(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < PRINT_LO || c > PRINT_HI) return false;
  }
  return true;
}

function concat(chunks: readonly string[]): Uint8Array {
  const parts = chunks.map(strBytes);
  let len = 0;
  for (const p of parts) len += p.length;
  const out = new Uint8Array(len);
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

export function createInputWriter(opts: InputWriterOptions): InputWriter {
  const windowMs = opts.windowMs ?? 5;
  const maxBurst = opts.maxBurst ?? 32 * 1024;
  const schedule =
    opts.schedule ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
  const cancel =
    opts.cancel ??
    ((handle: unknown) => clearTimeout(handle as ReturnType<typeof setTimeout>));

  let pending: string[] = [];
  let armed = false;
  let timer: unknown = null;
  let inFlight = false;
  let generation = 0;

  function arm(): void {
    if (armed) return;
    armed = true;
    const gen = generation;
    timer = schedule(() => {
      armed = false;
      timer = null;
      if (gen === generation) void pump(gen);
    }, windowMs);
  }

  function flushNow(): void {
    if (armed) {
      cancel(timer);
      armed = false;
      timer = null;
    }
    void pump(generation);
  }

  async function pump(gen: number): Promise<void> {
    if (inFlight) return; // the live drain carries the rest, in push order
    inFlight = true;
    try {
      while (gen === generation && pending.length > 0) {
        const bytes = concat(pending);
        pending = [];
        for (let at = 0; at < bytes.length && gen === generation; at += maxBurst) {
          try {
            // the answer is not inspected: a refusal and a rejection mean the same
            // thing here - that burst is gone, and a replay would type it into the
            // shell twice. Later bytes still leave.
            await opts.send(bytes.slice(at, at + maxBurst));
          } catch {
            // dropped on purpose, never retried
          }
        }
        // bytes that landed during the flight leave now as ONE POST, no window wait
      }
    } finally {
      inFlight = false;
    }
  }

  function push(s: string): void {
    if (!s) return;
    pending.push(s);
    if (!printable(s)) {
      flushNow();
      return;
    }
    if (inFlight) return; // the live drain picks it up the moment its POST settles
    arm();
  }

  function reset(): void {
    generation++;
    pending = [];
    if (armed) {
      cancel(timer);
      armed = false;
      timer = null;
    }
  }

  return { push, reset };
}
