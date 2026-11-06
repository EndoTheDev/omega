#!/bin/bash
# 89-menubar-wordmark - the menubar's center segment (card t_3aed367e).
# Endo's ruling (2026-11-06, revised in chat): the PLAIN 'Omega' wordmark
# seats dead center in the menubar - NO version up top ("we already got
# that at the bottom"), the bottom status bar keeps owning version +
# uptime. The nav cluster (back/forward/palette) vacated the central seat
# it held since card t_6215474b and seats inline after the brand; the
# edge toggles stay pinned right. Customer-visible invariants, asserted
# here at TWO widths (1280, 880) so a left-width skew can never pass:
#   brand    - .menubar .brand renders, left of the wordmark.
#   center   - .menubar-wordmark text is exactly 'Omega', its center
#              within 1.5px of the menubar's full-width center (absolute
#              middle, not spacer-balanced - robust to any left/right).
#   toggles  - the first edge toggle (left sidebar) hugs the bar's right
#              edge.
#   version  - zero version text anywhere in the bar.
#   cluster  - .nav-cluster is NOT absolutely positioned (it left the
#              center) and sits left of the wordmark.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
wait_js '!!(document.querySelector(".menubar") && document.querySelector(".menubar-wordmark"))' 15 \
  || die "the menubar or its center wordmark never rendered"

# one eval returns the whole geometry verdict for the CURRENT width:
# the wordmark centers on the MENUBAR's own box (the app bar), whatever
# the viewport. 1 = green, anything else names the miss.
probe_menubar() {
  jsq '(()=>{const bar=document.querySelector(".menubar"),w=document.querySelector(".menubar-wordmark"),b=document.querySelector(".menubar .brand"),t=document.querySelector("button.edge-toggle");if(!bar||!w||!b||!t)return "missing element(s)";const br=bar.getBoundingClientRect(),wr=w.getBoundingClientRect();const c=br.left+br.width/2,wc=wr.left+wr.width/2;if(Math.abs(wc-c)>1.5)return "wordmark off center: "+wc.toFixed(2)+" vs "+c.toFixed(2);if(w.textContent!=="Omega")return "wordmark text ["+w.textContent+"]";if(wr.width===0)return "wordmark collapsed";const tr=t.getBoundingClientRect();if(tr.right<br.right-20)return "toggles not pinned right";if(b.getBoundingClientRect().right>wr.left)return "brand collides with wordmark";return "1"})()'
}

v=$(probe_menubar)
[ "$v" = "1" ] || die "wide probe (1280x800): $v"
step "ok: 1280x800 - 'Omega' dead center, brand left, toggles right"

bar_text=$(jsq 'document.querySelector(".menubar").textContent')
case "$bar_text" in
  *[a-zA-Z]*[0-9]*.*[0-9]*) die "version-like text in the menubar: [$bar_text]" ;;
esac
# the wordmark itself carries no digits - the status bar owns the version
wordmark=$(jsq 'document.querySelector(".menubar-wordmark").textContent')
[ "$wordmark" = "Omega" ] || die "wordmark text [$wordmark], want [Omega]"
step "ok: no version text in the menubar"

v=$(jsq '(()=>{const n=document.querySelector(".nav-cluster"),st=getComputedStyle(n);if(st.position==="absolute")return "cluster still absolutely centered";if(n.getBoundingClientRect().left>=document.querySelector(".menubar-wordmark").getBoundingClientRect().left)return "cluster not left of the wordmark";return "1"})()')
[ "$v" = "1" ] || die "inline-after-brand probe: $v"

# --- the SAME shape at a second width (narrower, more left/right skew) ---
ab set viewport 880 720 >/dev/null 2>&1
sleep 0.3
v=$(probe_menubar)
[ "$v" = "1" ] || die "narrow probe (880x720): $v"
step "ok: 880x720 - same shape; centering robust to the width change"

# restore desktop for the next recipe in the run
ab set viewport 1280 800 >/dev/null 2>&1 || true
