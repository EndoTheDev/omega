#!/bin/bash
# probe30 - instrumented variant of journey 30's switch-back step.
# Records: POST /variants result, GET /ui/canvas chat payload, and the
# DOM .v-pos / response text, as a time series around the click.
JOURNEY_TIMEOUT=60
JOURNEY_BASE_URL=http://127.0.0.1:18200
JOURNEY_CHROME_PORT=9434
JOURNEY_SESSION=cody-probe30
export JOURNEY_TIMEOUT JOURNEY_BASE_URL JOURNEY_CHROME_PORT JOURNEY_SESSION

JROOT=/home/endo/dev/omega/.worktrees/t_1c9793a8/scripts/journeys
. "$JROOT/lib.sh"

# the long-lived probe chrome on 9434 is started by the operator script
# (chrome9434.sh background job) - connect per probe run, never relaunch
AB="$(command -v agent-browser 2>/dev/null | head -1)"
[ -n "$AB" ] || AB=$(find "$HOME/.hermes/tools" -maxdepth 5 -type f \( -name 'agent-browser-linux-arm64' -o -name 'agent-browser-linux-x64' \) 2>/dev/null | head -1)
export AB
ab connect 9434 >/dev/null 2>&1 || { echo "probe: chrome connect failed"; exit 2; }
trap 'cleanup_journey_sessions >/dev/null 2>&1' EXIT

SID=$(new_journey_session probe | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) retry-single $TAG"

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
  echo "PROBE scoped_wait TIMED OUT after ${t}s: $expr (last: $out)" >&2
  return 1
}

snap() { # ts label -> canvas payload + DOM state
  curl -s http://127.0.0.1:18200/ui/canvas > /dev/null 2>&1 || true
  local cv rows sw txt att
  rows=$(python3 - <<EOF
import json,urllib.request
try:
  d=json.load(urllib.request.urlopen("http://127.0.0.1:18200/ui/canvas",timeout=5))
  for p in d:
    if p.get("id")=="chat":
      pl=p.get("payload") or {}
      rr=pl.get("rows") or []
      print(json.dumps({"session":pl.get("session"),"running":pl.get("running"),
        "rows":[(r.get("id"),r.get("role"),(r.get("blocks") or [{}])[0].get("type"),((r.get("blocks") or [{}])[0].get("content") or "")[:38]) for r in rr]}))
except Exception as e:
  print("CANVAS_ERR", e)
EOF
)
  sw=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:"-"}).textContent||"-").replace(/\s+/g,"")')
  txt=$(panejs '(p.querySelector(".msg.assistant .md")||p.querySelector(".msg.assistant")||{innerText:"-"}).innerText.slice(0,38)')
  att=$(panejs 'p.querySelectorAll(".msg.assistant,.msg.user").length')
  printf '%s %s canvas=%s dom_vpos=%s dom_responsive=%s dom_msgs=%s\n' "$(date +%H:%M:%S)" "$1" "$rows" "$sw" "$txt" "$att"
}

open_app
open_journey_row "(journey) retry-single $TAG"

focus=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  focus=$(panejs '(p.querySelector(".composer textarea")||{}).focus ? 1 : 0')
  if [ "$focus" = "1" ]; then panejs 'p.querySelector(".composer textarea").focus(), 1' >/dev/null 2>&1; break; fi
  sleep 1
done
[ "$focus" = "1" ] || { echo PROBE_NO_COMPOSER; exit 1; }

ab keyboard type "Reply with exactly one word: READY"
ab press Enter; ab press Enter
scoped_wait '(p.querySelector(".msg.user")||{innerText:""}).innerText.includes("READY") ? 1 : 0' 20 || exit 1
scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 90 || exit 1
snap after_first_exchange

face=$(panejs '(p.querySelector("button.send")||{className:""}).className.includes("retry") ? 1 : 0')
[ "$face" = "1" ] || { echo PROBE_NO_RETRY_FACE; exit 1; }
panejs 'p.querySelector("button.send").click(), 1' >/dev/null 2>&1
scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 120 || exit 1
snap after_replay

sw=""
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  sw=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:""}).textContent||"").replace(/\s+/g,"")')
  [ "$sw" = "2/2" ] && break
  sleep 0.5
done
echo "switcher reached: $sw (waited $((i/2))s)"
snap at_click

echo "--- CLICK $(date +%H:%M:%S)"
panejs 'p.querySelector(".variant-switcher .v-arrow").click(), 1' >/dev/null 2>&1
for d in 0.3 0.6 1.2 2.0 3.5 6.0 10.0; do
  sleep $d
  snap "t+$d"
done
echo "PROBE_SID=$SID"
curl -s "http://127.0.0.1:18200/sessions/$SID/variants" | head -c 600; echo
ab console 2>&1 | tail -5
cleanup_journey_sessions >/dev/null 2>&1 || true