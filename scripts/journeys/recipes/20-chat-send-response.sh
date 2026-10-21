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

# act like the customer: focus, type, Enter (the composer's send key)
ab find placeholder "message omega" click >/dev/null
ab keyboard type "Reply with exactly one line and nothing else: ## JOURNEYHEAD sendside"
ab press Enter
ab press Enter
step "typed"

# the user bubble: a correct echo of what the customer typed
hits=0
for i in 1 2 3 4 5; do
  hits=$(jsq '(()=>{let n=0; document.querySelectorAll(".msg.user").forEach(u => {if ((u.innerText || "").includes("JOURNEYHEAD")) n++;}); return n;})()' 2>&1)
  case "${hits:-0}" in ''|*[!0-9]*) hits=0 ;; esac
  [ "$hits" -ge 1 ] 2>/dev/null && break
  sleep 1
done
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