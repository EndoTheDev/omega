#!/bin/bash
# 30-retry-single-user - the replay invariant: retry the LAST user
# message mid/post-run and assert the customer-visible outcome is ONE
# user bubble with a sibling response - never a doubled bubble, never a
# lost transcript. (Once branching lands: variant switch +
# continue-from-selected extend this recipe.)
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session retry | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) retry-single $TAG"

open_app
open_journey_row "(journey) retry-single $TAG"
ab find placeholder "message omega" click >/dev/null
ab keyboard type "Reply with exactly one word: READY"
ab press Enter
ab press Enter
wait_js 'document.body.innerText.includes("READY")' 15
wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 90
step "ok: first exchange complete"

# post-run retry: the composer's replay face (empty composer + a last
# exchange = the send button morphs into retry)
face=$(jsq '(()=>{const b = document.querySelector("button.send"); return b && b.className.includes("retry") ? 1 : 0;})()' 2>&1)
assert_eq "$face" "1" "composer offers retry post-run"

# click the retry face
jsq 'document.querySelector("button.send").click()' >/dev/null 2>&1 || die "retry click failed"
step "retry clicked"

wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 120
step "ok: replay finished"

# THE invariant: exactly ONE user bubble, and the replayed answer beside it
count=$(jsq 'document.querySelectorAll(".msg.user").length' 2>&1)
assert_eq "$count" "1" "user bubble count after retry"
answers=$(jsq 'document.querySelectorAll(".msg.assistant").length' 2>&1)
[ "$answers" -ge 2 ] 2>/dev/null || die "retry did not add a sibling response (assistant bubbles: $answers)"
step "ok: one user bubble, sibling response ($answers assistant bubbles)"

assert_no_console_errors