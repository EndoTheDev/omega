#!/bin/bash
# diag30.sh - t_7ab2bc47 evidence harness. ONE journey-30 run per
# invocation. The interaction flow is a byte-for-byte copy of
# recipes/30-retry-single-user.sh's runtime body (same helpers, same
# timings); instrumented only at the edges:
#   - open_app wrapper: page-side observer (fetch log + capture-phase
#     click log with isConnected/disabled facts). No timing change.
#   - die override: snapshot server truth (variants / transcript /
#     canvas), the client observer, the switcher DOM, lane log tail +
#     screenshot, THEN exit 1. The session STAYS ALIVE.
#
# rc: 0 green (session deleted, per recipe), 1 red (session kept)
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export DIAG_BASE="${DIAG_BASE:-http://127.0.0.1:18431}"
export JOURNEY_BASE_URL="$DIAG_BASE"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30"
LANE_LOG="${DIAG_LANEDIR:-$HROOT/diag/lane}/omega.log"
EVROOT="$HROOT/diag/evidence"
mkdir -p "$EVROOT"

. "$JROOT/lib.sh"

JOURNEY_NEEDS_MODEL=1
SID=$(new_journey_session retry | tail -1)
TAG=$(journey_tag)
EVDIR="$EVROOT/run-$TAG"
label_journey_session "$SID" "(journey) retry-single $TAG"

die() {
  local dir="$EVDIR"
  mkdir -p "$dir"
  printf '%s [diag RED] %s\n' "$(date +%H:%M:%S)" "$*" >&2
  {
    echo "msg: $*"
    echo "session: ${SID:-none}"
    echo "date: $(date -Iseconds)"
    echo "page: $(ab url 2>/dev/null || true)"
  } > "$dir/red.txt"
  jsq '(()=>{const d=window.__diag;if(!d)return "NO_OBSERVER";return JSON.stringify({clicks:d.clicks,fetches:d.fetches.slice(-120),samples:(d.samples||[]).slice(-300)});})()' \
    > "$dir/client-obs.txt" 2>&1
  jsq '(()=>{const panes=[...document.querySelectorAll(".dock-pane")];const p=panes.find(x=>[...x.querySelectorAll(".dock-tab")].some(d=>d.getAttribute("aria-selected")==="true" && (d.querySelector(".dock-tab-title")||{textContent:""}).textContent.includes("(journey) retry-single")));if(!p)return "NO_PANE";const chain=e=>{let c=[];while(e&&e!==document.body){c.push(e.className||e.tagName);e=e.parentElement;}return c.join("<");};return JSON.stringify({path:location.hash,asstCount:p.querySelectorAll(".msg.assistant").length,bubbles:[...p.querySelectorAll(".msg.assistant")].map(m=>({chain:chain(m),html:m.outerHTML.slice(0,700)})),switchers:[...p.querySelectorAll(".variant-switcher")].map(s=>({pos:(s.querySelector(".v-pos")||{textContent:""}).textContent,connected:s.isConnected,inDoc:document.contains(s),chain:chain(s),arrows:[...s.querySelectorAll(".v-arrow")].map(a=>({disabled:a.disabled,connected:a.isConnected}))}))});})()' \
    > "$dir/dom.txt" 2>&1
  ab screenshot 2>/dev/null | tail -1 > "$dir/screenshot-path.txt" || true
  curl -s "$JOURNEY_BASE_URL/sessions/$SID/variants" > "$dir/srv-variants.json"
  curl -s "$JOURNEY_BASE_URL/sessions/$SID/transcript" > "$dir/srv-transcript.json"
  curl -s "$JOURNEY_BASE_URL/ui/canvas" > "$dir/srv-canvas.json"
  tail -80 "$LANE_LOG" > "$dir/lane-log-tail.txt" 2>/dev/null || true
  printf '%s [diag] evidence: %s (session %s KEPT)\n' "$(date +%H:%M:%S)" "$dir" "$SID" >&2
  exit 1
}

# panejs / scoped_wait: verbatim from the recipe
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) retry-single')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}
scoped_wait() {
  local expr="$1" t="$2" i=0 out
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(panejs "$expr")
    [ "$out" = "1" ] && return 0
    sleep 0.5
    i=$((i + 1))
  done
  die "scoped_wait timed out after ${t}s: $expr (last: $out)"
}

open_app() {
  ab set viewport 1280 800 >/dev/null 2>&1 || true
  ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || \
    { JOURNEY_CDP_ATTACHED=0; ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || die "cannot open $JOURNEY_BASE_URL"; }
  ab wait --load networkidle >/dev/null 2>&1 || true
  step "open: $JOURNEY_BASE_URL"
  # the diag observer: fetch log + capture-phase click log. Installed
  # once per page load; page reloads (open_app) re-install fresh.
  printf '%s' "$(cat "$HROOT/diag/observer.js")" | ab eval --stdin >/dev/null 2>&1 || true
}

open_app
open_journey_row "(journey) retry-single $TAG"

focus=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  focus=$(panejs '(p.querySelector(".composer textarea")||{}).focus ? 1 : 0')
  if [ "$focus" = "1" ]; then
    panejs 'p.querySelector(".composer textarea").focus(), 1' >/dev/null 2>&1
    break
  fi
  sleep 1
done
assert_eq "$focus" "1" "focused pane's composer reachable"

ab keyboard type "Reply with exactly one word: READY"
ab press Enter
ab press Enter
scoped_wait '(p.querySelector(".msg.user")||{innerText:""}).innerText.includes("READY") ? 1 : 0' 20
scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 90
# mid-diag checkpoint A: first exchange done - snapshot the server truth
# (branch state pre-retry should be no variants)
mkdir -p "$EVDIR"
curl -s "$JOURNEY_BASE_URL/sessions/$SID/variants" > "$EVDIR/checkpointA-variants.json"
curl -s "$JOURNEY_BASE_URL/ui/canvas" > "$EVDIR/checkpointA-canvas.json"
step "ok: first exchange complete"

face=$(panejs '(p.querySelector("button.send")||{className:""}).className.includes("retry") ? 1 : 0')
assert_eq "$face" "1" "composer offers retry post-run"

panejs 'p.querySelector("button.send").click(), 1' >/dev/null 2>&1 || die "retry click failed"
step "retry clicked"

scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 120
# mid-diag checkpoint B: replay finished - the switcher's born state
curl -s "$JOURNEY_BASE_URL/sessions/$SID/variants" > "$EVDIR/checkpointB-variants.json"
curl -s "$JOURNEY_BASE_URL/ui/canvas" > "$EVDIR/checkpointB-canvas.json"
step "ok: replay finished"

count=$(panejs 'p.querySelectorAll(".msg.user").length')
assert_eq "$count" "1" "user bubble count after retry"

answers=$(panejs 'p.querySelectorAll(".msg.assistant").length')
# SOFTENED (diag): the doubled-bubble family must not pre-empt the flip
# step - record it, keep driving to the switch-back assertion.
if [ "$answers" != "1" ]; then
  step "DIAG: doubled-bubble state entered (asst=$answers) - continuing to the flip"
  echo "doubled_bubble_asst=$answers" >> "$EVDIR/red.txt"
fi

row=$(panejs '(r=>r?+(r===r.parentElement.lastElementChild?1:0):0)(p.querySelector(".msg.assistant .msg-actions"))')
if [ "$row" != "1" ]; then
  step "DIAG: action row not last child (row=$row) - continuing to the flip"
fi
sw=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  sw=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:""}).textContent||"").replace(/\s+/g,"")')
  [ "$sw" = "2/2" ] && break
  sleep 1
done
if [ "$sw" != "2/2" ]; then
  step "DIAG: switcher not born 2/2 (sw=$sw) - attempting the flip anyway"
fi

panejs 'p.querySelector(".variant-switcher .v-arrow").click(), 1' >/dev/null 2>&1
# capture the click's immediate DOM aftermath (suspect 1: detached node):
# the arrows the recipe saw at click time vs 200ms later
sleep 0.2
mkdir -p "$EVDIR"
curl -s "$JOURNEY_BASE_URL/sessions/$SID/variants" > "$EVDIR/checkpointC-variants-200ms.json"
curl -s "$JOURNEY_BASE_URL/ui/canvas" > "$EVDIR/checkpointC-canvas-200ms.json"
flipped=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  flipped=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:""}).textContent||"").replace(/\s+/g,"")')
  [ "$flipped" = "1/2" ] && break
  sleep 1
done
# DIAG CHECKPOINT C (the discriminator): captured inside die() on red.

assert_eq "$flipped" "1/2" "sibling response stays reachable (switch back renders « 1/2 »)"

assert_no_console_errors
# green: session deleted per recipe contract
curl -s -X DELETE "$JOURNEY_BASE_URL/sessions/$SID" >/dev/null
exit 0