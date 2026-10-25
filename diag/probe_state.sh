#!/bin/bash
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export JOURNEY_BASE_URL="http://127.0.0.1:18431"
export JOURNEY_CHROME_PORT="${DIAG_CDP:-9433}"
export JOURNEY_SESSION="cody-diag30-probe"
export AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
ab() { "$AB" --session "$JOURNEY_SESSION" "$@"; }
jsq() { ab eval "$1"; }
echo "== root containers:"
jsq "(()=>{const roots=[...document.querySelectorAll('body *')].slice(0,60);const hits=roots.filter(e=>e.__vue_app__||e._vnode||e.__vnode);return (document.querySelector('#app')||{id:'no#app'}).id+' / '+hits.map(h=>h.id||h.className+'[has]vnode').join(',').slice(0,300);})()"
echo "== canvas panel payload ids (live):"
jsq "fetch('/ui/canvas').then(r=>r.json()).then(c=>{const p=c.find(x=>x.kind==='chat');return JSON.stringify({session:p&&p.payload.session,rows:(p&&(p.payload.rows||[])).map(r=>[r.id,r.role,r.status])});})"
echo "== variants api (live):"
jsq "fetch('/sessions/jrn-25Oct173814-retry/variants').then(r=>r.json()).then(j=>JSON.stringify(j.variants))"