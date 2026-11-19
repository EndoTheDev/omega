// omega webui: pure browser-tab logic. A cross-origin iframe's DOM is unreadable, and
// modern Chromium fires the load event for a REFUSED embed, so the client cannot detect
// refusal itself; the honest signal is harness/browser's probe (GET /browser/frameable),
// which reads the target's X-Frame-Options / CSP frame-ancestors server-side. Refuse ONLY on a
// negative verdict (cannot-say keeps the late-load window); the probe reads headers, never content.

/** the late-load window: a load this late is a refused/failed embed. */
export const FALLBACK_AFTER_MS = 4000;

/** a frame-load waits this many ms before writing the resume point (a refused embed's error page fires load too). */
export const PERSIST_GRACE_MS = 300;

/** the verdict's wire shape (harness/browser's GET /browser/frameable) */
export interface ProbeVerdictBody {
  frameable?: boolean | null;
}

/** null/absent = cannot-say (offline probe, exotic host): keep the late-load window, do not refuse. */
export function classifyProbe(
  body: unknown,
): "refused" | "ok" | "unknown" {
  if (body && typeof body === "object" && (body as ProbeVerdictBody).frameable === false)
    return "refused";
  if (body && typeof body === "object" && (body as ProbeVerdictBody).frameable === true)
    return "ok";
  return "unknown";
}

/** schemeless input gets https://, anything else passes verbatim - the server route validates, never here. */
export function normalizeUrl(input: string): string {
  const u = input.trim();
  if (u === "") return "";
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u)) return u;
  return "https://" + u;
}

/** cross-origin frame history cannot be read, so the pane keeps its OWN stack; consecutive duplicates collapse. */
export interface NavHistory {
  urls: string[];
  idx: number;
}

export function emptyNav(): NavHistory {
  return { urls: [], idx: -1 };
}

/** the forward tail dies on a fresh navigation from a back step; a consecutive duplicate never stacks twice. */
export function pushNav(h: NavHistory, url: string): NavHistory {
  if (url === "") return h;
  if (h.urls[h.idx] === url) return h;
  const urls = h.urls.slice(0, h.idx + 1);
  urls.push(url);
  return { urls, idx: urls.length - 1 };
}

/** null when the step would leave the stack: the buttons render disabled. */
export function stepNav(h: NavHistory, delta: number): NavHistory | null {
  const i = h.idx + delta;
  if (i < 0 || i >= h.urls.length) return null;
  return { urls: h.urls, idx: i };
}

export function canStep(h: NavHistory, delta: number): boolean {
  const i = h.idx + delta;
  return i >= 0 && i < h.urls.length;
}