#!/bin/bash
# 30-retry-single-user - the replay invariant: retry the LAST user
# message mid/post-run and assert the customer-visible outcome is ONE
# user bubble whose response is re-run as a sibling variant - never a
# doubled bubble, never a lost transcript. Since t_17365247 the replay
# does not append a second response below the old one: the fresh answer
# becomes the active variant under the same user turn and the previous
# response stays reachable through the « n/n » switcher.
#
# Every DOM assertion is scoped to the FOCUSED chat tab's pane: a
# long-lived chrome carries earlier journeys' tabs (dock state restores
# across reloads), and a document-wide bubble count reads ghost tabs
# the focused pane never shows (the suite red's got-[2]).
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session retry | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) retry-single $TAG"

# panejs <expr over p>: evaluate expr against the focused chat pane p -
# the .dock-pane whose aria-selected tab title carries this run's label.
# Prints NO_PANE when the pane is absent (kept as a value so callers can
# poll while the sidebar list still loads).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) retry-single')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1
}

# scoped_wait <expr> <timeout_s>: poll the focused pane; timeout dies
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

open_app
open_journey_row "(journey) retry-single $TAG"

# type into THIS pane's composer: a document-wide placeholder click
# would land wherever an earlier journey's draft tab sits in the DOM.
# Polled: focusing the row mounts the chat pane and the tab title picks
# the session label up from the list refresh.
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
step "ok: first exchange complete"

# post-run retry: the composer's replay face (empty composer + a last
# exchange = the send button morphs into retry)
face=$(panejs '(p.querySelector("button.send")||{className:""}).className.includes("retry") ? 1 : 0')
assert_eq "$face" "1" "composer offers retry post-run"

# click the retry face inside the pane
panejs 'p.querySelector("button.send").click(), 1' >/dev/null 2>&1 || die "retry click failed"
step "retry clicked"

scoped_wait 'p.querySelector(".composer button.send.cancel") ? 0 : 1' 120
step "ok: replay finished"

# THE invariant: exactly ONE user bubble in the focused tab - the replay
# never persists (nor renders) a duplicate user turn
count=$(panejs 'p.querySelectorAll(".msg.user").length')
assert_eq "$count" "1" "user bubble count after retry"

# the sibling contract (t_17365247 semantics): the active path renders
# the re-run response and the prior response stays as a sibling variant
# - the switcher pins « 2/2 » on the response block
answers=$(panejs 'p.querySelectorAll(".msg.assistant").length')
[ "$answers" -ge 1 ] 2>/dev/null || die "no response rendered after retry (assistant bubbles: $answers)"
sw=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  sw=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:""}).textContent||"").replace(/\s+/g,"")')
  [ "$sw" = "2/2" ] && break
  sleep 1
done
assert_eq "$sw" "2/2" "retry re-ran the turn as a sibling variant (switcher « 2/2 »)"

# the previous response is not lost: flipping to it renders the sibling
# (the select endpoint refetch swaps the active path's tail)
panejs 'p.querySelector(".variant-switcher .v-arrow").click(), 1' >/dev/null 2>&1
flipped=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  flipped=$(panejs '((p.querySelector(".variant-switcher .v-pos")||{textContent:""}).textContent||"").replace(/\s+/g,"")')
  [ "$flipped" = "1/2" ] && break
  sleep 1
done
assert_eq "$flipped" "1/2" "sibling response stays reachable (switch back renders « 1/2 »)"

assert_no_console_errors