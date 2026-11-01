// omega webui: pure browser-tab logic (card t_95c91f9c). The
// BrowserTab.vue's decisions, import-safe for the node suite: URL
// normalization, the in-frame history model, the refused-embed
// fallback rule, and the probe verdict's classification.
//
// The honesty note the card demands (do not paper over): a cross-origin
// iframe's DOM is unreadable AND modern Chromium fires the load event
// for a REFUSED embed (the frame renders Chrome's own error surface),
// so neither "did it load" nor "can I read it" can say "refused" from
// the client side. The honest signal is the modules/browser probe
// (GET /browser/frameable): the capsule reads the TO-BE-EMBEDDED page's
// X-Frame-Options / CSP frame-ancestors headers server-side and answers
// refuse / allow / cannot-say. The tab refuses ONLY on a negative
// verdict; a cannot-say (offline probe, exotic host) keeps the frame
// and the late-load window as the slow path. No proxy exists in v1:
// the probe reads headers, never content.

/** the fallback timer: a load this late is a refused/failed embed
    (the probe-less slow path only - the probe answers refusals fast) */
export const FALLBACK_AFTER_MS = 4000;

/** the persist grace: a frame-load waits this many ms before writing
    the resume point, so the probe's negative can still veto a refused
    embed's error-page load (the refused URL persisted NOTHING) */
export const PERSIST_GRACE_MS = 300;

/** the verdict's wire shape (modules/browser's GET /browser/frameable) */
export interface ProbeVerdictBody {
  frameable?: boolean | null;
}

/** classify the probe: refused -> the fallback now, ok -> the frame
    proceeds, unknown (null/absent - a probe that cannot say) -> the
    iframe path keeps its late-load window. Only a definitive negative
    may flip the fallback: the honest rule the card settled. */
export function classifyProbe(
  body: unknown,
): "refused" | "ok" | "unknown" {
  if (body && typeof body === "object" && (body as ProbeVerdictBody).frameable === false)
    return "refused";
  if (body && typeof body === "object" && (body as ProbeVerdictBody).frameable === true)
    return "ok";
  return "unknown";
}

/** normalize what the user typed into a navigable URL. Empty input
    stays empty (the bar does nothing); a schemeless input gets the
    https:// prefix (the address bar law every real browser uses);
    anything else passes through verbatim - validation is the server
    route's shape check, never a gate here. */
export function normalizeUrl(input: string): string {
  const u = input.trim();
  if (u === "") return "";
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u)) return u;
  return "https://" + u;
}

/** the in-frame history (basic back/forward): the pane keeps its OWN
    stack of committed addresses, because cross-origin frame history
    cannot be read. Consecutive duplicates collapse (a reload never
    grows the stack). Pure - the component owns the reactive copy. */
export interface NavHistory {
  urls: string[];
  idx: number;
}

/** the fresh stack (one shape, never a null dance) */
export function emptyNav(): NavHistory {
  return { urls: [], idx: -1 };
}

/** push a navigated address: the forward tail dies (a fresh navigation
    from a back step rewrites it, the browser-law behavior) and a
    consecutive duplicate never stacks twice. */
export function pushNav(h: NavHistory, url: string): NavHistory {
  if (url === "") return h;
  if (h.urls[h.idx] === url) return h;
  const urls = h.urls.slice(0, h.idx + 1);
  urls.push(url);
  return { urls, idx: urls.length - 1 };
}

/** step idx by delta over the stack; null at the edges (the button is
    disabled there by law, this is the belt). */
export function stepNav(h: NavHistory, delta: number): NavHistory | null {
  const i = h.idx + delta;
  if (i < 0 || i >= h.urls.length) return null;
  return { urls: h.urls, idx: i };
}

/** whether back/forward render enabled: the edges are dead buttons. */
export function canStep(h: NavHistory, delta: number): boolean {
  const i = h.idx + delta;
  return i >= 0 && i < h.urls.length;
}