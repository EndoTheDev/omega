#!/bin/bash
# 96-browser - the general web browser pane's journey (card t_95c91f9c,
# Endo ruling 2026-11-01: "the browser should be like a normal browser
# which also allows me to visit other websites").
#
# The module's whole server surface is the last-URL pair, and the pane is
# pure client side - URL bar + iframe, any origin. Frames are driven
# against LOCAL throwaway origins so the recipe never touches the
# network: python serves two marker pages on the loopback, one WITHOUT
# frame headers (embeds fine) and one WITH X-Frame-Options: DENY (the
# known ceiling - the refusal case made deterministic). Pins:
#
#   a  rest     - GET /browser/last answers 200; PUT refuses a
#                 non-browsable scheme (400); PUT + GET round-trips.
#   b  open     - the main pane's empty-watermark menu carries Browser
#                 (the singleton opener); clicking mounts BrowserTab
#                 (the URL bar input, aria-label "browser url").
#   c  resume   - the tab restores the persisted address on mount (the
#                 PUT from step a) and a local page load keeps the
#                 fallback OUT - load = friendly, the resume is real.
#   d  refused  - the DENY origin flips the fallback panel in (the
#                 no-load-within-4s rule) and the refused address is NOT
#                 persisted - the no-load rule is the write gate too.
#   e  history  - back returns to the previous stack entry (the bar's
#                 own record), forward returns home.
#   f  console  - the presentation-detector: none of this makes noise.
#
# Residue-free: the trap kills the python servers and deletes the
# lane home's data/browser.json (the cheapest loss in the home, per the
# capsule's own law).
. "${JROOT:?run must export JROOT}/lib.sh"
B="$JOURNEY_BASE_URL"

# throwaway browser.json: the home is the serving lane's (the runner
# exports OMEGA_BIN; 92's derivation)
if [ -z "${OMEGA_HOME:-}" ]; then
  [ -n "${OMEGA_BIN:-}" ] || die "set OMEGA_HOME or OMEGA_BIN to the serving lane home"
  OMEGA_HOME="$(dirname "$OMEGA_BIN")"
fi
cleanup() {
  [ -n "${SRVPID:-}" ] && kill "$SRVPID" >/dev/null 2>&1
  rm -f "$OMEGA_HOME/data/browser.json" 2>/dev/null || true
}
trap cleanup EXIT

# --- two local origins: friendly (no frame headers) + deny (XFO) ---------
PORTS=$(python3 - <<'PY'
import socket
def p():
    s = socket.socket(); s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]; s.close(); return port
print(p(), p())
PY
)
P_FRIENDLY=$(echo "$PORTS" | cut -d' ' -f1)
P_DENY=$(echo "$PORTS" | cut -d' ' -f2)
SRV="${TMPDIR:-/tmp}/jrn-browser-srv$$.py"
cat > "$SRV" <<PY
import http.server, sys, threading
p_friendly, p_deny = int(sys.argv[1]), int(sys.argv[2])
PAGE = b"<html><head><title>jrn</title></head><body>jrn-browser-marker</body></html>"
class Friendly(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.end_headers()
        self.wfile.write(PAGE)
    def log_message(self, *a): pass
class Deny(Friendly):
    def end_headers(self):
        self.send_header("X-Frame-Options", "DENY")
        super().end_headers()
    def log_message(self, *a): pass
threading.Thread(target=lambda: http.server.ThreadingHTTPServer(("127.0.0.1", p_deny), Deny).serve_forever(), daemon=True).start()
http.server.ThreadingHTTPServer(("127.0.0.1", p_friendly), Friendly).serve_forever()
PY
python3 "$SRV" "$P_FRIENDLY" "$P_DENY" & SRVPID=$!
FRIENDLY="http://127.0.0.1:$P_FRIENDLY/jrn-marker.html"
DENYURL="http://127.0.0.1:$P_DENY/jrn-marker.html"
up=0; for i in $(seq 1 20); do
  if curl -s -o /dev/null "$FRIENDLY" && curl -s -o /dev/null "$DENYURL"; then up=1; break; fi
  sleep 0.5
done
[ "$up" = "1" ] || die "local frame origins never came up"
# the deny origin really denies (its header is why the embed refuses)
curl -s -D - -o /dev/null "$DENYURL" | grep -qi "^x-frame-options: deny" || die "deny origin lost its X-Frame-Options header"
step "ok: local origins up (friendly :$P_FRIENDLY, deny :$P_DENY)"

# --- a: the REST face -----------------------------------------------------
code=$(curl -s -o /dev/null -w '%{http_code}' "$B/browser/last")
[ "$code" = "200" ] || die "GET /browser/last answered $code"
bad=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$B/browser/last" \
  -H 'Content-Type: application/json' -d '{"url": "javascript:alert(1)"}')
[ "$bad" = "400" ] || die "a non-browsable scheme was not refused ($bad)"
put=$(curl -s -X PUT "$B/browser/last" -H 'Content-Type: application/json' \
  -d "{\"url\": \"$FRIENDLY\"}")
printf '%s' "$put" | grep -qF "127.0.0.1:$P_FRIENDLY" || die "PUT did not answer the stored URL: $put"
got=$(curl -s "$B/browser/last" | grep -oE "127\.0\.0\.1:$P_FRIENDLY" | head -1)
[ -n "$got" ] || die "GET /browser/last lost the persisted URL"
step "ok: the last-URL pair round-trips and refuses junk schemes"

# --- b+c: open the tab (empty-pane watermark), the resume mounts ----------
open_app
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

# close the boot draft chat tab so the main pane empties into the
# watermark menu (79's proven close)
draftclosed=$(jsq '(()=>{const ps=[...document.querySelectorAll(".dock-pane")];const hit=ps.find(p=>!p.classList.contains("pinned")&&[...p.querySelectorAll(".dock-tab-title")].some(t=>/^new chat$/i.test((t.textContent||"").trim())));if(!hit) return 0;const t=[...hit.querySelectorAll(".dock-tab-title")].findIndex(x=>/^new chat$/i.test((x.textContent||"").trim()));if(t<0) return 0;const x=hit.querySelectorAll(".dock-tab-close")[t];if(!x) return 0;x.click();return 1})()')
assert_eq "$draftclosed" "1" "boot draft chat tab closed"
# the watermark's refill inventory carries Browser (singleton voice)
wait_js '(()=>{const b=[...document.querySelectorAll(".dock-empty .dock-menu-btn")].find(x=>(x.textContent||"").trim()==="Browser");return b?1:0})()' 10 || true
bopen=$(jsq '(()=>{const b=[...document.querySelectorAll(".dock-empty .dock-menu-btn")].find(x=>(x.textContent||"").trim()==="Browser");if(!b) return 0;b.click();return 1})()')
assert_eq "$bopen" "1" "Browser opened from the empty-pane menu"
# the pane mounts: url bar input + frame shell
wait_js 'document.querySelector(".browser-tab input[aria-label=\"browser url\"]") ? 1 : 0' 10 \
  || die "BrowserTab never mounted (no url bar)"
step "ok: BrowserTab mounted with the URL bar"

# c: the resume - the tab restores the friendly address on mount and the
# frame LOADS (no fallback): the persisted URL is the mount's address
resumed=0
for i in $(seq 1 30); do
  r=$(jsq "(()=>{const s=document.querySelector('.browser-frame');const f=document.querySelector('.browser-refused');return (s&&s.src.includes('127.0.0.1:$P_FRIENDLY')&&s.style.display!=='none'&&!f)?1:0})()" 2>&1 | tail -1)
  [ "$r" = "1" ] && resumed=1 && break
  sleep 1
done
[ "$resumed" = "1" ] || die "the resume never mounted the friendly frame (last: $r)"
step "ok: the persisted resume mounted and loaded (fallback out)"

# --- d: the refused origin flips the fallback in, persists NOTHING -------
ab fill 'input[aria-label="browser url"]' "$DENYURL" >/dev/null 2>&1
ab press Enter >/dev/null 2>&1
refused=0
for i in $(seq 1 40); do # the fallback window is FALLBACK_AFTER_MS=4000
  r=$(jsq '(()=>{const f=document.querySelector(".browser-refused");return (f&&f.style.display!=="none")?1:0})()' 2>&1 | tail -1)
  [ "$r" = "1" ] && refused=1 && break
  sleep 0.5
done
[ "$refused" = "1" ] || die "the X-Frame-Options origin never flipped the fallback panel in"
# the honesty ceiling text renders (the copy/open affordances with it)
ft=$(jsq '(()=>{const f=document.querySelector(".browser-refused");return f?(f.textContent||"").includes("shown inline")?1:0:0})()')
assert_eq "$ft" "1" "the fallback names the refusal"
last=$(curl -s "$B/browser/last")
printf '%s' "$last" | grep -qF "127.0.0.1:$P_FRIENDLY" || \
  die "a refused frame must not persist: /browser/last became $last"
step "ok: the refusal surfaced and the refused URL persisted NOTHING"

# --- e: the history belt (the pane's own stack) ---------------------------
back=$(jsq '(()=>{const b=document.querySelector("button[aria-label=\"browser back\"]");if(!b||b.disabled) return 0;b.click();return 1})()')
assert_eq "$back" "1" "back clicked"
# back lands on the deny origin (the stack entry before the friendly
# re-navigation) - its fallback re-opens on the same no-load rule
backdenied=0
for i in $(seq 1 40); do
  r=$(jsq "(()=>{const i=document.querySelector('.browser-tab input[aria-label=\"browser url\"]');const f=document.querySelector('.browser-refused');return (i&&i.value.includes('127.0.0.1:$P_DENY')&&f)?1:0})()" 2>&1 | tail -1)
  [ "$r" = "1" ] && backdenied=1 && break
  sleep 0.5
done
[ "$backdenied" = "1" ] || die "back never returned to the deny stack entry (last: $r)"
fwd=$(jsq '(()=>{const b=document.querySelector("button[aria-label=\"browser forward\"]");if(!b||b.disabled) return 0;b.click();return 1})()')
assert_eq "$fwd" "1" "forward clicked"
fwdhome=0
for i in $(seq 1 30); do
  r=$(jsq "(()=>{const s=document.querySelector('.browser-frame');const f=document.querySelector('.browser-refused');return (s&&s.src.includes('127.0.0.1:$P_FRIENDLY')&&s.style.display!=='none'&&!f)?1:0})()" 2>&1 | tail -1)
  [ "$r" = "1" ] && fwdhome=1 && break
  sleep 1
done
[ "$fwdhome" = "1" ] || die "forward never re-mounted the friendly frame (last: $r)"
step "ok: back/forward walk the pane's own stack"

# --- f: console silence ----------------------------------------------------
assert_no_console_errors

rm -f "$SRV" 2>/dev/null || true
step "green: the browser pane's journey (rest + open + resume + refused + history + console)"