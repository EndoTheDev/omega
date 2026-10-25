#!/bin/bash
# backflip.sh - select variant server-side via curl on a QUIET client.
# The app's 6s canvas poller MUST repaint the switcher if the
# canvas apply chain is healthy end-to-end.
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JOURNEY_BASE_URL="http://127.0.0.1:18431"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
jsq() { ab eval "$1"; }
SID="${1:?}"
MSGID="${2:?message_id of branch point}"
TO="${3:-1}"   # target active index

panejs() {
  ab eval "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) retry-single')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}
echo "pre pos: $(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:"NONE"}).textContent||"")')"
curl -s -X POST "$JOURNEY_BASE_URL/sessions/$SID/variants" -H 'Content-Type: application/json' -d "{\"message_id\":\"$MSGID\",\"select\":$TO}"; echo
sleep 1
echo "+1s: $(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:"NONE"}).textContent||"")')"
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  sleep 1
  pos=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:"NONE"}).textContent||"")')
  ids=$(jsq "fetch('/ui/canvas').then(r=>r.json()).then(c=>{const p=c.find(x=>x.kind==='chat');return JSON.stringify((p.payload.rows||[]).map(r=>r.id));})")
  echo "+$((i+1))s pos=$pos canvas=$ids"
done