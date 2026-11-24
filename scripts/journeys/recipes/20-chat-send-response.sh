#!/bin/bash
# 20-chat-send-response - the deepest probe: new chat -> send -> the
# customer's expected outcome. Asserts: the user bubble renders, the
# model's response streams in, markdown in that response renders as
# HTML (the "correct data, lying presentation" tripwire), the stream
# finishes (composer un-cancels), and the console stays silent.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session send | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) send-render $TAG"

open_app

# open the throwaway session: click its sidebar row by unique label
open_journey_row "(journey) send-render $TAG"
step "ok: opened throwaway session $SID"

# act like the customer: focus, type, Enter (the composer's send key).
# The composer's editor is a contenteditable .composer-input (card
# t_e67c372b): no textarea, no placeholder attribute - and the focus is
# PANE-SCOPED (the t_7adf2e7f law: a document-wide query can land in a
# ghost tab's pane whose composer the focused pane never shows).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) send-render')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}
# panejs_expr <expr over p>: the same pane-scoped expression, for a poll.
# poll_js_value runs bare jsq (panejs is a runner, not an expression), so the
# scope wrapper rides the string and returns the bare value - panejs
# stringifies for its callers, a poll compares the number itself.
panejs_expr() {
  printf '%s' "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) send-render')));if(!p)return 'NO_PANE';return ($1);})()"
}
focus=$(poll_js_value "$(panejs_expr '(p.querySelector(".composer .composer-input")||{}).focus ? 1 : 0')" -eq 1)
if [ "$focus" = "1" ]; then
  panejs 'p.querySelector(".composer .composer-input").focus(), 1' >/dev/null 2>&1
fi
assert_eq "$focus" "1" "focused pane's composer editor reachable"
ab keyboard type "Reply with exactly one line and nothing else: ## JOURNEYHEAD sendside"
ab press Enter
ab press Enter
step "typed"

# the user bubble: a correct echo of what the customer typed
hits=$(poll_js_value '(()=>{let n=0; document.querySelectorAll(".msg.user").forEach(u => {if ((u.innerText || "").includes("JOURNEYHEAD")) n++;}); return n;})()' -ge 1)
[ "${hits:-0}" -ge 1 ] 2>/dev/null || die "user bubble did not render after send (hits=$hits)"
step "ok: user bubble renders"

# wait for the run to finish (composer face leaves stop/cancel)
wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 90
step "ok: stream finished"

# the response: markdown rendered as an H2 (the ## heading is HTML, never
# the raw '##' text) and it belongs to an assistant bubble
h2=$(jsq '(()=>{const a = document.querySelector(".msg.assistant"); if (!a) return 0; const h = a.querySelector("h2"); return h && h.innerText.includes("JOURNEYHEAD") ? 1 : 0;})()' 2>&1)
assert_eq "$h2" "1" "markdown renders (h2 in assistant bubble)"

# and the console is silent the whole way through
assert_no_console_errors