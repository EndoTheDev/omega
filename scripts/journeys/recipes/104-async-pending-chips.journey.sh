#!/bin/bash
# 104-async-pending-chips - the async agent loop's visible half
# (phase 3, card t_163d1e87; ADR Workshop/omega/async-agent-loop.md
# rulings 6/7/8 - result-at-once v1).
#
# The customer-visible shape this pins, end to end and IN THE BROWSER:
#   1. a turn that ends on a yielded tool call renders a PENDING CHIP
#      in the chat pane - the tool row pends, it is never an orphaned
#      completed call with no output;
#   2. the run ENDED while the chip pends (the composer's stop face is
#      gone - the home sleeps, nothing streams);
#   3. a reload inside the sleep window renders the pending chip from
#      HISTORY (the restore path derives it), and the pane re-arms -
#   4. the result re-enters as a WAKE RUN in the SAME pane: the chip
#      fills in place (done + the command's output), the wake's
#      answer streams as a new assistant turn.
#
# Drives a real shell.run yield (phase 2 made it yield-class): a
# sleep-N command the model must run through the tool - the sleep IS
# the home-sleeps window. Requires a lane whose provider can answer
# (JOURNEY_NEEDS_MODEL=1). NOT in regression.list: the model-turn cost
# is nightly policy; the node suite (webui/test/pendingchips.test.ts)
# pins the pure derivations this recipe drives live.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

runtag=$(date +%d%b%H%M%S)
TAG="ASYNC-${runtag}"
JRN_ID=$(new_journey_session "async-${runtag}" | tail -1)
LABEL="(journey) async-chips ${runtag}"
label_journey_session "$JRN_ID" "$LABEL"
trap cleanup_journey_residue EXIT
step "session: $JRN_ID"

open_app
open_journey_row "$LABEL"
step "ok: opened the journey session"

# --- pane-scoped composer drive (the t_7adf2e7f law, recipe 20 shape) ---
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('$LABEL'));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//'
}
for i in 1 2 3 4 5 6 7 8 9 10; do
  focus=$(panejs '(p.querySelector(".composer .composer-input")||{}).focus ? 1 : 0')
  if [ "$focus" = "1" ]; then
    panejs 'p.querySelector(".composer .composer-input").focus(), 1' >/dev/null 2>&1
    break
  fi
  sleep 1
done
assert_eq "$focus" "1" "focused pane's composer editor reachable"

# the send: a shell.run the model must call with the exact command -
# the sleep is the home-sleeps window the chip pends through
ab keyboard type "Use the shell.run tool to run exactly this command: sleep 20 && echo ${TAG}. After the command completes, reply with its exact output."
ab press Enter
ab press Enter
step "sent (tag ${TAG})"

# --- 1+2: the pending chip, on an ENDED run --------------------------
wait_js \
  "(()=>{return [...document.querySelectorAll('.toolcall')].some(d=>{const ic=d.querySelector('.tool-ic');return ic&&ic.classList.contains('pending')})?1:0})()" \
  90
step "ok: the yielded turn's call renders as a PENDING chip"

# the run is over (ruling 6: the turn ENDED on the yielded call): the
# composer left its stop face - the home sleeps, nothing streams
wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 30
step "ok: the run ended while the chip pends (home sleeps)"

# --- 3: reload inside the sleep window: history renders the chip -----
# the restore path derives the pending chip from the transcript, and
# the pane re-arms (the watcher) so the wake still streams in live
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
open_journey_row "$LABEL"
wait_js \
  "(()=>{return [...document.querySelectorAll('.toolcall')].some(d=>{const ic=d.querySelector('.tool-ic');return ic&&ic.classList.contains('pending')})?1:0})()" \
  30
step "ok: history renders the pending chip after reload"

# --- 4: the wake re-enters the same pane, the chip fills in ---------
wait_js \
  "(()=>{return [...document.querySelectorAll('.toolcall')].some(d=>{const ic=d.querySelector('.tool-ic');return ic&&ic.classList.contains('done')&&(d.innerText||'').includes('$TAG')})?1:0})()" \
  150
step "ok: the chip filled in - the result re-entered as a wake turn"

# the wake's answer is a NEW assistant turn in the same pane, and its
# stream finished (composer face leaves stop again)
wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 120
step "ok: the wake turn streamed and finished in the same pane"

# --- the wire truth: the transcript carries the call and its result -
curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_ID}/transcript" > "$JOURNEY_WORKTMP/omega-jrn-async-tx.json"
python3 - "$JOURNEY_WORKTMP/omega-jrn-async-tx.json" "$TAG" <<'PYEOF' \
  || die "the async transcript shape is wrong (see the assertion above)"
import json, sys
d = json.load(open(sys.argv[1]))
tag = sys.argv[2]
items = d.get("items") or []
calls = [i for i in items if i.get("kind") == "tool_call" and (i.get("tool_call") or {}).get("name") == "shell.run"]
results = [i for i in items if i.get("kind") == "tool_result"]
assert calls, "the shell.run call never landed in the transcript - the model skipped the tool"
assert any(tag in (r.get("content") or "") for r in results), \
    "the command's output never re-entered as a result row (the wake never settled)"
print("ok: transcript carries the yielded call and its re-entered result")
PYEOF

# and the console stayed silent through the whole arc
assert_no_console_errors
