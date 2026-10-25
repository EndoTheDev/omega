#!/bin/bash
# redpane_probe.sh - probe the LIVE RED pane (paused redloop).
set -u
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
panejs() {
  ab eval "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) retry-single')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1
}
echo "== pane found: $(panejs 'p.className')"
echo "== DOM bubbles: $(panejs 'JSON.stringify([...p.querySelectorAll(".msg.assistant")].map(m=>({pos:(m.querySelector(".v-pos")||{t:""}).textContent,cursor:!!m.querySelector(".cursor"),elapsed:(m.querySelector(".msg-elapsed")||{t:""}).textContent})))')"
echo "== srv canvas: $(curl -s http://127.0.0.1:18431/ui/canvas | python3 -c 'import json,sys;c=json.load(sys.stdin);p=[x for x in c if x.get("kind")=="chat"][0];print(p["payload"]["session"], [(r["id"],r["role"],r["status"]) for r in p["payload"]["rows"]])')"
echo "== srv variants:"
curl -s "http://127.0.0.1:18431/sessions/jrn-25Oct174816-retry/variants"; echo
echo "== now click the v-arrow on the red pane (select already; watch repaint):"
echo "$(panejs '(()=>{const a=p.querySelector(".variant-switcher .v-arrow");if(!a)return "NO_ARROW";a.click();return "clicked";})()')"
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 0.4
  echo "+$((i*400))ms pos=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:"NONE"}).textContent||"")') bub=$(panejs 'p.querySelectorAll(".msg.assistant").length')"
done
echo "== post click srv variants:"
curl -s "http://127.0.0.1:18431/sessions/jrn-25Oct174816-retry/variants"; echo