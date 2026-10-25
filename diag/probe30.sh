#!/bin/bash
# probe30.sh - t_7ab2bc47 LIVE interactive probe, deterministic phases.
# Phase 1: build a settled 2/2 state (send READY, retry, settle).
# Phase 2: probe Vue internals reachability + in-page store dumps.
# Phase 3: N real v-arrow clicks with 50ms DOM sampling + store dumps.
# Reuses the lane already up at $DIAG_BASE with chrome CDP at $DIAG_CDP.
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export DIAG_BASE="${DIAG_BASE:-http://127.0.0.1:18431}"
export JOURNEY_BASE_URL="$DIAG_BASE"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
PROBE_ROOT="$HROOT/diag/probe-out"
mkdir -p "$PROBE_ROOT"

. "$JROOT/lib.sh"

AB="$(command -v agent-browser 2>/dev/null | head -1)"
[ -n "$AB" ] || AB="$(find "$HOME/.hermes/tools" -maxdepth 5 -type f -name 'agent-browser-linux-arm64' | head -1)"
export AB

timeout 30 agent-browser-linux-arm64 --session "$JOURNEY_SESSION" connect "$JOURNEY_CHROME_PORT" >/dev/null 2>&1 || { echo "cannot attach CDP $JOURNEY_CHROME_PORT"; exit 2; }

JOURNEY_NEEDS_MODEL=1
SID=$(new_journey_session retry | tail -1)
TAG=$(journey_tag)
PR="$PROBE_ROOT/probe-$TAG"
label_journey_session "$SID" "(journey) retry-single $TAG"
step "probe session=$SID out=$PR"

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
  echo "STEP_WAIT_TIMEOUT: $expr (last: $out)"; return 1
}

open_app
open_journey_row "(journey) retry-single $TAG"

focus=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  focus=$(panejs '(p.querySelector(".composer textarea")||{}).focus ? 1 : 0')
  if [ "$focus" = "1" ]; then panejs 'p.querySelector(".composer textarea").focus(), 1' >/dev/null 2>&1; break; fi
  sleep 1
done
[ "$focus" = "1" ] || { echo "STEP_FAIL: composer focus"; exit 2; }

ab keyboard type "Reply with exactly one word: READY"
ab press Enter
ab press Enter
scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 120
echo "OK first exchange complete"

face=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  face=$(panejs '(p.querySelector("button.send")||{className:""}).className.includes("retry") ? 1 : 0')
  [ "$face" = "1" ] && break
  sleep 1
done
[ "$face" = "1" ] || { echo "STEP_FAIL: no retry face"; exit 2; }
panejs 'p.querySelector("button.send").click(), 1' >/dev/null 2>&1
scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 120

sw=""
for i in 1 2 3 4 5 6 7 8 9 10; do
  sw=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:""}).textContent||"").replace(/\s+/g,"")')
  [ "$sw" = "2/2" ] && break
  sleep 1
done
echo "OK settled switcher=$sw"

# Phase 2: Vue internals reachability.
jsq '(()=>{const c=document.querySelector("#app");const a=c&&c.__vue_app__;if(!a)return "NO_VUE_APP";return "VUE_APP "+(typeof a._instance);})()' > "$PR/vue-app.txt" 2>&1
cat "$PR/vue-app.txt"
# dump the canvas store + chat states through the instance tree
jsq '
(()=>{
  const a=document.querySelector("#app").__vue_app__;
  if(!a)return "NO_VUE_APP";
  const seen=new Set();const out=[];
  const walk=(inst,depth)=>{
    if(!inst||depth>12)return;
    const o=inst.setupState||inst.data;
    if(o){for(const k in o){try{const v=o[k];if(v&&typeof v==="object"){const j=JSON.stringify(v,(kk,vv)=>{if(kk==="panels"){return "[PANELS]"}return vv});if(/variant|canvas|panel|restored|sessionId/.test(k)){out.push(k+" = "+String(j).slice(0,300));}}}catch(e){}}}
    const s=inst.subTree;
    if(s&&s.component)walk(s.component,depth+1);
    const ch=(s&&Array.isArray(s.children))?s.children:[];
  };
  try{walk(a._instance,0)}catch(e){out.push("walk error: "+e)}
  return out.slice(0,40).join("\n");
})()' > "$PR/inst-walk.txt" 2>&1
head -c 3000 "$PR/inst-walk.txt"

echo "=== phase markers done; SID=$SID stays alive ==="