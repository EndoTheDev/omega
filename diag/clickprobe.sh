#!/bin/bash
# clickprobe.sh - real v-arrow click on the live settled pane, with
# 100ms DOM sampling before/after + server snapshots by raw curl.
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export JOURNEY_BASE_URL="http://127.0.0.1:18431"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
jsq() { ab eval "$1"; }
SID="${1:?session id}"

panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) retry-single')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}

echo "== pre-click bubbles:"
panejs 'JSON.stringify([...p.querySelectorAll(".msg.assistant")].map(m=>({pos:(m.querySelector(".v-pos")||{t:""}).textContent,cursor:!!m.querySelector(".cursor"),elapsed:(m.querySelector(".msg-elapsed")||{t:""}).textContent})))'

echo "== pre-click canvas (page fetch):"
jsq "fetch('/ui/canvas').then(r=>r.json()).then(c=>{const p=c.find(x=>x.kind==='chat');return JSON.stringify({session:p.payload.session,ids:(p.payload.rows||[]).map(r=>r.id)});})"

echo "== click:"
panejs '(()=>{const a=p.querySelector(".variant-switcher .v-arrow");if(!a)return "NO_ARROW";const r=a.click();return "clicked connected="+a.isConnected+" disabled="+a.disabled;})()'

for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  st=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:"NONE"}).textContent||"EMPTY").replace(/\s+/g,"")+"|"+p.querySelectorAll(".msg.assistant").length')
  echo "click+$((i*200))ms pos+bubbles=$st"
  sleep 0.2
done

echo "== post canvas (page fetch):"
jsq "fetch('/ui/canvas').then(r=>r.json()).then(c=>{const p=c.find(x=>x.kind==='chat');return JSON.stringify({session:p.payload.session,ids:(p.payload.rows||[]).map(r=>r.id)});})"
echo "== post variants API:"
curl -s "http://127.0.0.1:18431/sessions/$SID/variants"
echo