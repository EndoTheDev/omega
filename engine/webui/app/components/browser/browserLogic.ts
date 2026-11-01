// omega webui: pure browser-tab logic (card t_95c91f9c). The
// BrowserTab.vue's decisions, import-safe for the node suite: URL
// normalization, the in-frame history model, and the refused-embed
// fallback rule.
//
// The honesty note the card demands (do not paper over): the fallback
// fires on a TIMEOUT WITHOUT A LOAD EVENT, not on a read of the
// frame-busting headers. Cross-origin iframe DOM is unreadable and
// Chromium fires no load event when X-Frame-Options refuses the embed,
// so "no load within FALLBACK_AFTER_MS" is the only honest client-side
// signal - it covers the refusal AND a dead host, and it forgives a
// slow site (a late load flips back to the frame). No server-side
// header probe exists in v1, on purpose.

/** the fallback timer: a load this late is a refused/failed embed */
export const FALLBACK_AFTER_MS = 4000;

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