#!/bin/bash
# 76-message-variants - the branching journey (t_17365247): the retry fix
# is user-visible, so it rides the journeys suite as an API-surface check
# (the variant nav UI belongs to the engine/webui and engine/tui child cards - this recipe
# drives the same endpoints they consume). Invariants, in the customer's
# words:
#   send          -> one exchange, NO variants block on the wire (linear
#                    sessions render byte-compatible with the pre-tree API)
#   retry         -> the old response does NOT stack below - the active
#                    path carries the FRESH response and the variants
#                    block reports the sibling pair (no duplicate user turn)
#   cancel        -> the cancelled run keeps its partial as its OWN
#                    variant (next sibling), never an appended bubble
#   select        -> variant 1 re-activates and SURVIVES a fresh reload;
#                    the first run's own items render on the path again
#   send after    -> the turn continues the SELECTED branch
#
# Structural asserts, deliberately: with tools enabled the model may
# answer any prompt with a tool run (several rows per response), so
# per-kind item counting is NOT stable; ids and branch metadata are.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session variants | tail -1)
JRN_ID=$SID
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) message-variants $TAG"

BASE="$JOURNEY_BASE_URL"
# tx: the transcript envelope (items + running + optional variants)
tx() { curl -s "$BASE/sessions/$SID/transcript"; }
txcount() { tx | grep -o "\"kind\":\"$1\"" | wc -l | tr -d ' '; }
txint() { tx | grep -o "\"$1\":[0-9]*" | head -1 | grep -o '[0-9]*$'; }
# path_ids <kind>: the active path's store row ids of that kind
path_ids() { tx | grep -o "\"kind\":\"$1\",\"id\":\"[0-9]*\"" | sed 's/.*"id":"\([0-9]*\)".*/\1/' | sort -u; }
# wait_idle: poll until the session has no live run (the envelope always
# carries "running"; the detached run outlives the origin /chat stream).
wait_idle() {
  local i=0
  while [ "$i" -lt 120 ]; do
    tx | grep -q '"running":false' && return 0
    sleep 1; i=$((i + 1))
  done
  die "run never left running (120s)"
}

# --- 1. the first exchange: an old-style LINEAR session -------------------
send_journey_message "$SID" "Count from 1 to 15, printing one number per line. Just the numbers, one per line, no tools."
wait_idle
case "$(tx)" in *'"variants"'*) die "linear session carried a variants block";; esac
FIRST_PATH=$(path_ids message)
[ -n "$FIRST_PATH" ] || die "first exchange produced no response items: $(tx | head -c 500)"
step "ok: first exchange complete (response ids: $(printf '%s' "$FIRST_PATH" | tr '\n' ' '))"

# the user bubble id - the branch-point address surface
BUBBLE=$(tx | grep -o '"kind":"user","id":"[0-9]*"' | head -1 | sed 's/.*"id":"\([0-9]*\)".*/\1/')
[ -n "$BUBBLE" ] || die "transcript user item carries no id (variants cannot address it): $(tx | head -c 500)"

# --- 2. retry AS A SIBLING: fresh response on the path, old one parked ----
curl -s -X POST "$BASE/chat" -H 'Content-Type: application/json' \
  -d "{\"session_id\": \"$SID\", \"retry\": true}" >/dev/null
wait_idle
assert_eq "$(txcount user)" "1" "no duplicate user bubble after retry"
assert_eq "$(txint count)" "2" "variants block reports 2 siblings"
assert_eq "$(txint active)" "1" "the fresh retry is the active variant"
# the first run's own items are OFF the active path (no stacking), and
# the path still carries fresh response content
for id in $FIRST_PATH; do
  case "$(path_ids message)" in *"$id"*) die "first run still on the active path after retry (id $id)";; esac
done
assert_ne "$(path_ids message)" "" "the retry response renders on the active path"
step "ok: one active response (fresh), first run parked as a variant"

# --- 3. cancel -> the partial is its OWN variant (ruling 2) ---------------
curl -s -X POST "$BASE/chat" -H 'Content-Type: application/json' \
  -d "{\"session_id\": \"$SID\", \"retry\": true}" >/dev/null &
retry_pid=$!
# wait until the retry streams content (a live tail message item - tail
# items carry NO id, persisted variants' items do, so this matches only
# the running run's own content), so the stop lands on a real partial
i=0
while ! tx | grep -q '"kind":"message","content"'; do
  if ! kill -0 "$retry_pid" 2>/dev/null; then
    # the run completed inside a poll gap: it is already the third
    # sibling and the stop is a no-op - proceed on that branch
    break
  fi
  if [ "$i" -ge 60 ]; then
    curl -s -X POST "$BASE/sessions/$SID/stop" -H 'Content-Type: application/json' >/dev/null
    kill "$retry_pid" 2>/dev/null; wait "$retry_pid" 2>/dev/null
    die "retry never streamed content in 30s (model lane too slow for a cancel stage)"
  fi
  sleep 0.5; i=$((i + 1))
done
if kill -0 "$retry_pid" 2>/dev/null; then
  sleep 1
  curl -s -X POST "$BASE/sessions/$SID/stop" -H 'Content-Type: application/json' >/dev/null
fi
wait "$retry_pid" 2>/dev/null
wait_idle
assert_eq "$(txint count)" "3" "third run kept as its own variant (not an append)"
assert_eq "$(txint active)" "2" "the cancelled/partial run is the active variant"
for id in $FIRST_PATH; do
  case "$(path_ids message)" in *"$id"*) die "first run still on the active path (id $id)";; esac
done
step "ok: the cancelled run lives as a sibling, active on the path"

# --- 4. select variant 1: flip + reload persistence ----------------------
SIBS=$(tx | grep -o '"siblings":\[[^]]*\]' | head -1 | sed 's/.*\[\(.*\)\].*/\1/')
FIRST=$(printf '%s' "$SIBS" | cut -d, -f1 | tr -d '"')
[ -n "$FIRST" ] || die "variants block carries no sibling ids"
code=$(curl -s -o /tmp/omegaj76-sel -w '%{http_code}' -X POST \
  "$BASE/sessions/$SID/variants" -H 'Content-Type: application/json' \
  -d "{\"message_id\": \"$BUBBLE\", \"select\": 0}")
assert_eq "$code" "200" "select variant 1 returns 200"
assert_eq "$(cat /tmp/omegaj76-sel | grep -o '"active":[0-9]*' | head -1 | grep -o '[0-9]*$')" "0" \
  "select response reports variant 1 active"
rm -f /tmp/omegaj76-sel
# a fresh reload (new fetch, same server-side state) still selects variant 1
assert_eq "$(txint active)" "0" "selection survives reload"
# the first run renders on the active path again (its whole conversation)
assert_eq "$(path_ids message)" "$FIRST_PATH" "the first response renders on the active path"
step "ok: variant 1 selected and persisted"

# --- 5. send after select: the turn continues the SELECTED branch --------
send_journey_message "$SID" "How many numbers did you write? Answer with just the number."
wait_idle
assert_eq "$(path_ids user | wc -l | tr -d ' ')" "2" "the follow-up user turn joined the selected branch"
assert_eq "$(txint active)" "0" "the branch point still selects variant 1 after the turn"
assert_eq "$(txint count)" "3" "the follow-up added no variant (it continued the branch)"
step "ok: send after select continued the selected branch"
# (an API-surface recipe: no browser steps, so no console assertion -
# the variant nav UI's presentation check lives in the frontend cards)