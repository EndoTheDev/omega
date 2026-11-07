#!/bin/bash
# 88-delegate-run-affinity - the customer-level pin for per-run affinity
# of injected delegate results (t_f3fb7c52).
#
# The bug this fails on: the serve-wide injected channel was drained by
# ANY agent loop holding the seam - a concurrent run (the session title
# turn, a sibling /chat) stole another run's child result and that run
# never received it. The customer-visible proof of the fix: two
# concurrent /chat runs, each spawning a delegate task, each sees only
# ITS OWN child's answer as the injected user turn - never a foreign
# run's.
#
# Curl-only (no browser): /chat twice concurrently, then read both
# session transcripts - a user row after the caller prompt is an
# injected delegate result turn. The expected tokens are read from
# each run's OWN delegate.task calls (the model transcription of the
# instructed token is the parent's actual spawn, mangled or not), so
# the pin is exact against the wire, not against the prompt text.
#
# Requires a lane whose provider can answer (probes are glm-5.3) and
# the delegate module mountable - the child runs the lane's own binary
# via OMEGA_BIN / the exe-sibling fallback. NOT in regression.list: the
# two-child cost is nightly-reviewer policy, not the suite's.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

runtag=$(date +%d%b%H%M%S)
sentA="JRN-AFFINITY-ALPHA-${runtag}"
sentB="JRN-AFFINITY-BETA-${runtag}"
JRN_A=$(new_journey_session "affa-${runtag}")
JRN_B=$(new_journey_session "affb-${runtag}")
JRN_ID="${JRN_B}"   # cleanup deletes jrn-* leftovers by prefix; this pins the fast path
# residue-free: the jrn- sessions die in the trap (the residue reaper,
# card t_a99ffcd7 - the broad sweep also covers crashed-run leftovers)
trap cleanup_journey_residue EXIT
step "sessions: ${JRN_A} / ${JRN_B}"

# --- the two concurrent runs: each spawns a delegate child with a
# sentinel only IT knows, then polls delegate.status until the child
# finishes (keeps the parent loop alive to drain its own queue) ---
spawn_run() {
  local sid="$1" sentinel="$2" out="$3" prompt
  prompt="Use delegate.task to run a subagent with this exact prompt: Output exactly the token ${sentinel} and nothing else. After it starts, call delegate.status until it shows the task completed, then reply with the subagent's final answer verbatim."
  curl -sN -X POST "${JOURNEY_BASE_URL}/chat" -H 'Content-Type: application/json' \
    -d "{\"session_id\": \"${sid}\", \"messages\": [{\"role\": \"user\", \"content\": \"${prompt}\"}]}" \
    > "$out" 2>&1
}
spawn_run "${JRN_A}" "${sentA}" "$JOURNEY_WORKTMP/omega-jrn-affA.sse" &
spawn_run "${JRN_B}" "${sentB}" "$JOURNEY_WORKTMP/omega-jrn-affB.sse" &
wait
step "both /chat streams settled"

# --- transcripts: wait until each carries the injected user turn ---
for i in $(seq 1 60); do
  A_has=$(curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_A}" | python3 -c \
    'import json,sys; d=json.load(sys.stdin); m=[x for x in (d.get("messages") or [])[1:] if x.get("role")=="user"]; sys.exit(0 if m else 1)' \
    >/dev/null 2>&1 && echo yes || echo no)
  B_has=$(curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_B}" | python3 -c \
    'import json,sys; d=json.load(sys.stdin); m=[x for x in (d.get("messages") or [])[1:] if x.get("role")=="user"]; sys.exit(0 if m else 1)' \
    >/dev/null 2>&1 && echo yes || echo no)
  [ "$A_has" = yes ] && [ "$B_has" = yes ] && break
  [ "$i" = 60 ] && die "an injected user turn never landed in the transcripts ($i polls)"
  sleep 5
done
step "ok: both transcripts carry an injected user turn"

# --- the affinity assertion ---
# tokens spawned by run X: the prompt exactly as X typed it into
# delegate.task (the SSE wire), not the instructed string
curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_A}" > "$JOURNEY_WORKTMP/omega-jrn-affA-tx.json"
curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_B}" > "$JOURNEY_WORKTMP/omega-jrn-affB-tx.json"
python3 - \
  "$JOURNEY_WORKTMP/omega-jrn-affA.sse" "$JOURNEY_WORKTMP/omega-jrn-affB.sse" \
  "$JOURNEY_WORKTMP/omega-jrn-affA-tx.json" "$JOURNEY_WORKTMP/omega-jrn-affB-tx.json" \
  "${sentA}" "${sentB}" <<'PYEOF' \
  || die "run affinity broken (t_f3fb7c52 regression): see the assertion output above"
import json, re, sys

def spawned(sse_path):
    toks = set()
    for line in open(sse_path):
        line = line.strip()
        if not line.startswith("data:"):
            continue
        try:
            d = json.loads(line[5:].strip())
        except Exception:
            continue
        if d.get("type") == "tool_call" and (d.get("tool_call") or {}).get("name") == "delegate.task":
            prompt = str((d.get("tool_call") or {}).get("arguments", {}).get("prompt", ""))
            m = re.search(r"Output exactly the token (\S+)", prompt)
            if m:
                toks.add(m.group(1))
    return toks

def injected(path):
    d = json.load(open(path))
    return [m.get("content", "") for m in (d.get("messages") or [])[1:] if m.get("role") == "user"]

sseA, sseB, txA, txB, sentA, sentB = sys.argv[1:7]
ownA, ownB = spawned(sseA), spawned(sseB)
print(f"run A spawned  : {sorted(ownA)}")
print(f"run B spawned  : {sorted(ownB)}")
print(f"injected A     : {[t[:60] for t in injected(txA)]}")
print(f"injected B     : {[t[:60] for t in injected(txB)]}")
assert ownA and ownB, "each run must have spawned a delegate task - the prompt did not reach the tool"
injA, injB = injected(txA), injected(txB)
assert injA, f"run A received NO injected result - its child's answer was drained by someone else (sentinel {sentA})"
assert injB, f"run B received NO injected result - its child's answer was drained by someone else (sentinel {sentB})"
for row in injA:
    assert any(t in row for t in ownA), f"run A injected row {row[:80]!r} matches no token A spawned - run affinity broken"
    assert not any(t in row for t in ownB), f"run A injected row {row[:80]!r} carries a token B spawned - the steal still happens"
for row in injB:
    assert any(t in row for t in ownB), f"run B injected row {row[:80]!r} matches no token B spawned - run affinity broken"
    assert not any(t in row for t in ownA), f"run B injected row {row[:80]!r} carries a token A spawned - the steal still happens"
print("ok: each concurrent run's injected turns carry only its own delegate results")
PYEOF