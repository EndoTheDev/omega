// The ONE copy path: navigator.clipboard is secure-context only, so on plain http it is
// undefined - a voided rejection once flipped the affordance to "copied", a silent lie.
// Attempts: writeText; then a hidden textarea + execCommand("copy") (deprecated but works
// in insecure contexts); then false. Returns true only on a real copy, so callers MUST
// branch their affordance on the awaited result.
export async function copyText(text: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && typeof navigator.clipboard?.writeText === "function") {
    try {
      await navigator.clipboard.writeText(text ?? "");
      return true;
    } catch {
      // fall through
    }
  }
  return execCommandCopy(text ?? "");
}

// The ONE clipboard READ path: readText is secure-context and permission gated (it throws
// on plain http and after a refused prompt), so a refusal answers "" - the caller decides
// whether its own deprecated door is worth a try. Never a voided rejection.
export async function readClipboardText(): Promise<string> {
  if (typeof navigator === "undefined" || typeof navigator.clipboard?.readText !== "function") {
    return "";
  }
  try {
    return await navigator.clipboard.readText();
  } catch {
    return "";
  }
}

// The insecure-context fallback; focus is restored for the caller.
export function execCommandCopy(text: string): boolean {
  if (typeof document === "undefined" || !document.body) return false;
  const ta = document.createElement("textarea");
  ta.value = text;
  // readonly keeps mobile keyboards closed; off-screen beats display:none (unselectable)
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
