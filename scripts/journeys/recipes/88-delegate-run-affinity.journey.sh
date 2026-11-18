#!/bin/bash
# 88-delegate-run-affinity - the customer-level pin for per-run affinity
# of delegate results (t_f3fb7c52).
#
# The bug this fails on: the serve-wide result channel was drained by
# ANY agent loop holding the seam - a concurrent run (the session title
# turn, a sibling /chat) stole another run's child result and that run
# never received it. The customer-visible proof of the fix: two
# concurrent /chat runs, each spawning a delegate task, each sees only
# ITS OWN child's answer on its own call - never a foreign run's.
#
# Curl-only (no browser): /chat twice concurrently, then read both
# session transcripts. The wake shape is a tool_result ON the delegate
# call the run issued (async agent loop phase 3, agent/api/wakes.go:
# the settled core.YieldResult re-enters the run and fills its call
# row) - it is NOT an injected user turn. The expected tokens are read
# from each run's OWN delegate.task calls (the model transcription of
# the instructed token is the parent's actual spawn, mangled or not),
# so the pin is exact against the wire, not against the prompt text.
#
# Requires a lane whose provider can answer (probes are glm-5.3) and
# the delegate module mountable - the child runs the lane's own binary
# via OMEGA_BIN / the exe-sibling fallback. NOT in regression.list: the
# two-child cost is nightly-reviewer policy, not the suite's.
JOURNEY_NEEDS_MODEL=1
# recipe-lint: allow no-browser - this recipe drives two concurrent /chat runs over the wire and never opens a page; its only residue is a
# journey session, reaped by cleanup_journey_residue in run.sh's EXIT trap.
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

# --- transcripts: wait until each run's delegate call carries its RESULT ---
# The wake lands as the CALL's own tool_result row (see the header), so the
# affinity witness is that row matched to the call the run itself issued.
# Budget: 24 polls x 5s = 120s, so a lost wake dies BY NAME here instead of
# burning the suite's 300s wall cap with no verdict (the previous 60-poll
# budget could never finish before the cap, so every failure of this shape
# surfaced as a bare timeout with no diagnosis).
TXA="$JOURNEY_WORKTMP/omega-jrn-affA-tx.json"
TXB="$JOURNEY_WORKTMP/omega-jrn-affB-tx.json"
settled=no
for i in $(seq 1 24); do
  curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_A}/transcript" > "$TXA"
  curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_B}/transcript" > "$TXB"
  settled=$(python3 - "$TXA" "$TXB" <<'PYEOF'
import json, sys


def state(path):
    d = json.load(open(path, encoding="utf-8"))
    items = d.get("items") or []
    calls = [i for i in items
             if i.get("kind") == "tool_call"
             and (i.get("tool_call") or {}).get("name") == "delegate.task"]
    answered = {i.get("tool_call_id") for i in items if i.get("kind") == "tool_result"}
    if not calls:
        return "nocall"
    pending = [c for c in calls if (c.get("tool_call") or {}).get("id") not in answered]
    return "done" if not pending else "waiting"


a, b = state(sys.argv[1]), state(sys.argv[2])
print("yes" if a == "done" and b == "done" else "no:%s/%s" % (a, b))
PYEOF
)
  [ "$settled" = yes ] && break
  [ "$i" = 24 ] && die "a delegate result never filled its run's call row within 120s ($settled) - the wake was lost, see the transcripts in the evidence dir"
  sleep 5
done
step "ok: both runs' delegate calls carry their result"

# --- the affinity assertion ---
# tokens spawned by run X: the prompt exactly as X typed it into
# delegate.task (the SSE wire), not the instructed string
curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_A}/transcript" > "$TXA"
curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_B}/transcript" > "$TXB"
python3 - \
  "$JOURNEY_WORKTMP/omega-jrn-affA.sse" "$JOURNEY_WORKTMP/omega-jrn-affB.sse" \
  "$TXA" "$TXB" \
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


def results(path):
    """each delegate.task call paired with the result row it was given"""
    d = json.load(open(path))
    items = d.get("items") or []
    answered = {i.get("tool_call_id"): i for i in items if i.get("kind") == "tool_result"}
    rows = []
    for it in items:
        call = it.get("tool_call") or {}
        if it.get("kind") != "tool_call" or call.get("name") != "delegate.task":
            continue
        r = answered.get(call.get("id")) or {}
        rows.append({
            "call": call.get("id") or "",
            "content": str(r.get("content") or ""),
            "is_error": bool(r.get("is_error")),
            "answered": bool(r),
        })
    return rows


sseA, sseB, txA, txB, sentA, sentB = sys.argv[1:7]
ownA, ownB = spawned(sseA), spawned(sseB)
rowsA, rowsB = results(txA), results(txB)
print(f"run A spawned  : {sorted(ownA)}")
print(f"run B spawned  : {sorted(ownB)}")
print(f"run A results  : {[(r['call'], r['content'][:60], r['is_error']) for r in rowsA]}")
print(f"run B results  : {[(r['call'], r['content'][:60], r['is_error']) for r in rowsB]}")
assert ownA and ownB, "each run must have spawned a delegate task - the prompt did not reach the tool"
assert rowsA and rowsB, "each run must have a delegate.task call in its transcript"
for row in rowsA:
    assert row["answered"], f"run A's delegate call {row['call']} never got a result back - its child's answer never woke this run (sentinel {sentA})"
    assert not row["is_error"], f"run A's delegate call failed: {row['content'][:120]}"
    assert any(t in row["content"] for t in ownA), f"run A result {row['content'][:80]!r} matches no token A spawned - run affinity broken"
    assert not any(t in row["content"] for t in ownB), f"run A result {row['content'][:80]!r} carries a token B spawned - the steal still happens"
for row in rowsB:
    assert row["answered"], f"run B's delegate call {row['call']} never got a result back - its child's answer never woke this run (sentinel {sentB})"
    assert not row["is_error"], f"run B's delegate call failed: {row['content'][:120]}"
    assert any(t in row["content"] for t in ownB), f"run B result {row['content'][:80]!r} matches no token B spawned - run affinity broken"
    assert not any(t in row["content"] for t in ownA), f"run B result {row['content'][:80]!r} carries a token A spawned - the steal still happens"
print("ok: each concurrent run's delegate result carries only its own child's answer")
PYEOF
