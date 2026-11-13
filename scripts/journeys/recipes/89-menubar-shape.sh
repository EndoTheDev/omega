#!/bin/bash
# 89-menubar-shape - the menubar's shape (card t_20fd8745, Endo's
# 2026-11-13 reshape; supersedes the t_3aed367e center wordmark this
# recipe used to pin).
#
# THE RULINGS this witnesses, at TWO desktop widths plus the mobile
# breakpoint, because a left/right width skew must never pass:
#   brand   - ONE brand element, far left: the glyph and the word in a
#             single span, text 'Ωmega', left edge hugging the bar.
#   wordmark- .menubar-wordmark is DEAD (no element, no CSS).
#   center  - .nav-cluster is absolutely positioned, its center within
#             1.5px of the menubar's FULL-width center, and it carries
#             exactly four buttons: Back, Forward, the fake-input
#             palette affordance, the Ctrl+K trigger.
#   fake    - the affordance is a BUTTON (no <input>, no typing path)
#             whose placeholder is a LIVE HH:MM clock equal to the
#             browser's current minute - and it TICKS: the label changes
#             across a real minute boundary, so a frozen label reds.
#   palette - clicking the affordance opens the ctrl+k overlay; a
#             pointerdown outside it closes it (the outside-click rider);
#             the Ctrl+K trigger + Escape still round-trip.
#   toggles - the three edge toggles render at every width, pinned right.
#   mobile  - below 768px the CLUSTER hides and the three toggles STAY
#             (they drive the drawers).
#   version - zero version text anywhere in the bar.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
wait_js '!!(document.querySelector(".menubar") && document.querySelector(".nav-cluster"))' 15 \
  || die "the menubar or its nav cluster never rendered"

# one eval returns a NUMBER verdict for the CURRENT width: 1 = green,
# 0 = the miss (the reason re-eval only runs on red). Numbers serialize
# unquoted through ab eval - strings arrive JSON-quoted and would break
# the exact compare.
probe_menubar() {
  jsq '(()=>{const bar=document.querySelector(".menubar");if(!bar)return 0;const b=bar.querySelector(".brand"),c=document.querySelector(".nav-cluster"),inp=c?c.querySelector(".nav-palette-input"):null,tg=[...bar.querySelectorAll("button.edge-toggle")];if(!b||!c||!inp||tg.length<3)return 0;if(document.querySelector(".menubar-wordmark"))return 0;if(bar.querySelectorAll(".brand").length!==1)return 0;if(b.textContent!=="\u03A9mega")return 0;if(c.querySelector("input"))return 0;const cs=getComputedStyle(c);if(cs.display==="none")return 0;if(cs.position!=="absolute")return 0;const br=bar.getBoundingClientRect(),cr=c.getBoundingClientRect();if(cr.width===0)return 0;if(Math.abs((cr.left+cr.width/2)-(br.left+br.width/2))>1.5)return 0;if(b.getBoundingClientRect().left-br.left>24)return 0;if(c.querySelectorAll("button").length!==4)return 0;if(!c.querySelector("button[aria-label=\"Back\"]"))return 0;if(!c.querySelector("button[aria-label=\"Forward\"]"))return 0;if(!c.querySelector("button.nav-palette-input[data-palette-trigger]"))return 0;if(inp.tagName!=="BUTTON")return 0;const t=(inp.textContent||"").trim();if(!/^[0-9][0-9]:[0-9][0-9]$/.test(t))return 0;const d=new Date(),p=n=>String(n).padStart(2,"0");if(t!==p(d.getHours())+":"+p(d.getMinutes()))return 0;const tr=tg[tg.length-1].getBoundingClientRect();if(tr.right<br.right-20)return 0;return 1})()'
}

probe_reason() {
  jsq '(()=>{const bar=document.querySelector(".menubar");if(!bar)return "no menubar";const b=bar.querySelector(".brand"),c=document.querySelector(".nav-cluster"),inp=c?c.querySelector(".nav-palette-input"):null,tg=[...bar.querySelectorAll("button.edge-toggle")];if(!b)return "no .brand";if(!c)return "no .nav-cluster";if(!inp)return "no .nav-palette-input";if(tg.length<3)return "edge toggles: "+tg.length;if(document.querySelector(".menubar-wordmark"))return "the dead wordmark still renders";if(bar.querySelectorAll(".brand").length!==1)return "brand count "+bar.querySelectorAll(".brand").length;if(b.textContent!=="\u03A9mega")return "brand text ["+b.textContent+"]";if(c.querySelector("input"))return "the cluster carries a REAL input";const cs=getComputedStyle(c);if(cs.display==="none")return "cluster is display:none at this width";if(cs.position!=="absolute")return "cluster position ["+cs.position+"]";const br=bar.getBoundingClientRect(),cr=c.getBoundingClientRect();if(cr.width===0)return "cluster collapsed";const off=(cr.left+cr.width/2)-(br.left+br.width/2);if(Math.abs(off)>1.5)return "cluster off center by "+off.toFixed(2);if(b.getBoundingClientRect().left-br.left>24)return "brand not far left: "+b.getBoundingClientRect().left;if(c.querySelectorAll("button").length!==4)return "cluster buttons: "+c.querySelectorAll("button").length;if(!c.querySelector("button[aria-label=\"Back\"]"))return "no Back button";if(!c.querySelector("button[aria-label=\"Forward\"]"))return "no Forward button";if(inp.tagName!=="BUTTON")return "affordance tag ["+inp.tagName+"]";const t=(inp.textContent||"").trim();if(!/^[0-9][0-9]:[0-9][0-9]$/.test(t))return "clock text ["+t+"]";const d=new Date(),p=n=>String(n).padStart(2,"0");const want=p(d.getHours())+":"+p(d.getMinutes());if(t!==want)return "clock ["+t+"] vs live ["+want+"] (frozen?)";const tr=tg[tg.length-1].getBoundingClientRect();if(tr.right<br.right-20)return "toggles not pinned right (last ends "+tr.right+" of "+br.right+")";return "unknown"})()'
}

check_menubar() {
  v=$(probe_menubar)
  [ "$v" = "1" ] || die "$1: $(probe_reason)"
}

# --- 1. the wide desktop shape ---------------------------------------------
check_menubar "wide probe (1280x800)"
step "ok: 1280x800 - one brand far left, four-control cluster dead center, toggles right"

bar_text=$(jsq 'document.querySelector(".menubar").textContent')
case "$bar_text" in
  *[a-zA-Z]*[0-9]*.*[0-9]*) die "version-like text in the menubar: [$bar_text]" ;;
esac
step "ok: no version text in the menubar"

# --- 2. the placeholder clock TICKS ----------------------------------------
# The only honest witness of "never frozen": the label changes across a
# REAL minute boundary. Bounded to 70s (a full minute plus slack); the
# probe above already proved the label equals the live minute, so a tick
# here cannot be a timezone/format coincidence.
clock_before=$(jsq '(()=>{const e=document.querySelector(".nav-palette-input");return e?e.textContent.trim():""})()' | tr -d '"')
[ -n "$clock_before" ] || die "the affordance carries no clock text"
ticked=0
for _ in $(seq 1 70); do
  sleep 1
  now_label=$(jsq '(()=>{const e=document.querySelector(".nav-palette-input");return e?e.textContent.trim():""})()' | tr -d '"')
  if [ "$now_label" != "$clock_before" ]; then ticked=1; break; fi
done
[ "$ticked" = "1" ] || die "the placeholder clock never ticked past [$clock_before] in 70s - frozen label"
case "$now_label" in
  [0-9][0-9]:[0-9][0-9]) step "ok: the placeholder clock ticked [$clock_before] -> [$now_label]" ;;
  *) die "ticked to a non-clock label [$now_label]" ;;
esac
check_menubar "post-tick probe"
step "ok: the ticking label stays the live minute"

# --- 3. the affordance opens the palette; an OUTSIDE click closes it -------
js 'document.querySelector(".nav-palette-input").click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".command-palette")' 5 \
  || die "clicking the fake-input affordance did not open the command palette"
step "ok: the affordance opens the ctrl+k palette"

# the rider: a pointerdown anywhere outside the overlay closes it (no
# scrim exists, so the document seam owns it). pointerdown, not click -
# the same seam the overlay listens on.
js 'document.body.dispatchEvent(new PointerEvent("pointerdown",{bubbles:true}))' >/dev/null 2>&1
wait_js '!document.querySelector(".command-palette")' 5 \
  || die "an outside pointerdown did not close the palette (the rider is dead)"
step "ok: an outside click closes the palette"

# the icon trigger + Escape still round-trip (ONE open state, one path)
js 'document.querySelector(".nav-cluster button[aria-label=\"Command palette\"]").click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".command-palette")' 5 \
  || die "the Ctrl+K trigger did not open the palette"
ab press Escape >/dev/null 2>&1 || true
wait_js '!document.querySelector(".command-palette")' 5 \
  || die "Escape no longer closes the palette"
step "ok: the trigger opens / Escape closes (one open state)"

# --- 4. the SAME shape at a narrower desktop width -------------------------
ab set viewport 880 720 >/dev/null 2>&1
sleep 0.3
check_menubar "narrow desktop probe (880x720)"
step "ok: 880x720 - the same seats, no left/right skew"

# --- 5. below the drawer breakpoint: the cluster HIDES, the toggles STAY ---
ab set viewport 700 800 >/dev/null 2>&1
wait_js '!!(()=>{const c=document.querySelector(".nav-cluster");return c&&getComputedStyle(c).display==="none"})()' 10 \
  || die "below 768px the nav cluster still renders"
mobile=$(jsq '(()=>{const tg=[...document.querySelectorAll(".menubar button.edge-toggle")],b=document.querySelector(".menubar .brand");if(tg.length<3)return 0;if(!b||b.offsetParent===null)return 0;return tg.every(t=>t.offsetParent!==null)?1:0})()')
assert_eq "$mobile" "1" "mobile: cluster hidden, the three edge toggles + brand still render"

# restore desktop for the next recipe in the run
ab set viewport 1280 800 >/dev/null 2>&1 || true
