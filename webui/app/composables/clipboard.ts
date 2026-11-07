// The ONE copy path (card t_a92450ea): navigator.clipboard exists only in
// secure contexts (https or localhost) - on the plain-http lane
// (http://<LAN-IP>:8098) it is undefined, every direct call rejects, and
// a voided rejection still flipped the affordance to "copied" - a silent
// lie. Every copy affordance in the webui goes through copyText() here;
// no source outside this file calls navigator.clipboard directly.
//
// Order of attempts:
//   1. navigator.clipboard.writeText - the async API, works on https and
//      localhost.
//   2. hidden textarea + document.execCommand("copy") - the deprecated
//      but functional in-insecure-contexts fallback; needs user
//      activation, which click-driven callers naturally have.
//   3. false - the caller surfaces an honest failure; nothing here
//      fabricates a "copied".
//
// Returns true only when bytes actually went to the clipboard (as far
// as the platform will say so). Callers must branch their "copied"
// affordance on the awaited result - never void this call.

export async function copyText(text: string): Promise<boolean> {
  // navigator.clipboard is undefined in insecure contexts and on the
  // server (no SSR caller yet, but the guard is cheap).
  if (typeof navigator !== "undefined" && typeof navigator.clipboard?.writeText === "function") {
    try {
      await navigator.clipboard.writeText(text ?? "");
      return true;
    } catch {
      // Denied, undefined API rejection, or any other async failure -
      // fall through to the execCommand path.
    }
  }
  return execCommandCopy(text ?? "");
}

// The insecure-context fallback: a temporarily mounted off-screen
// textarea whose selected value is copied synchronously. Focus is
// preserved for the caller (the composer keeps its caret when a
// keystroke-dispatched copy lands here).
export function execCommandCopy(text: string): boolean {
  if (typeof document === "undefined" || !document.body) return false;
  const ta = document.createElement("textarea");
  ta.value = text;
  // readonly keeps mobile keyboards from opening; the styling keeps the
  // flash invisible without display:none (a hidden element cannot be
  // selected).
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.top = "-1000px";
  ta.style.opacity = "0";
  ta.style.pointerEvents = "none";
  document.body.appendChild(ta);
  const active = document.activeElement as { focus?: () => void } | null;
  try {
    ta.select();
    ta.setSelectionRange(0, ta.value.length);
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    ta.remove();
    active?.focus?.();
  }
}
