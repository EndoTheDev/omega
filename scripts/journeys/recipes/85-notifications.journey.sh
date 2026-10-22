#!/bin/bash
# 85-notifications - the finished-things surface journey (t_5d9032d3).
# REST/SSE-level on purpose: the emitter path (a worker completion) is
# what the product does live, and it needs no LLM. Pins:
#
#   lifecycle   - a kanban task PATCHed into done surfaces as one
#                 notification record: kind=task, verdict=ok, unread+1.
#   read        - POST /notifications/read {id} drops the unread count;
#                 {"all":true} clears it. A ghost id 404s.
#   live        - GET /notifications/events streams a notification
#                 frame for a subsequent emit (the webui toast source).
#   hygiene     - 501s only when the store is unmounted (covered in the
#                 Go gates); here the seam is mounted by definition.
#
# Residue-free: the task it creates is archived in the cleanup trap
# (jrn- title marker), no session is created.
. "${JROOT:?run must export JROOT}/lib.sh"

# --- lane alive; the seam answers (a foreign lane 200s with the SPA -
# the payload check is what proves THIS lane has the seam) ---
payload=$(curl -s "${JOURNEY_BASE_URL}/notifications")
printf '%s' "$payload" | grep -qF '"notifications"' || \
  die "lane answered non-JSON to /notifications (wrong lane?): ${JOURNEY_BASE_URL}"
step "ok: notifications seam mounted"

# --- lifecycle: one done transition = one record ---
before=$(printf '%s' "$payload" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["notifications"]));')

runtag=$(date +%s)
JRN_TASK="jrn-${runtag}-notify"
created=$(curl -s -X POST "${JOURNEY_BASE_URL}/tasks" -H 'Content-Type: application/json' \
  -d "{\"title\":\"${JRN_TASK}-1\"}")
tid1=$(printf '%s' "$created" | grep -oE '"id": ?"t_[a-f0-9]+"' | head -1 | sed 's/.*"\(t_[a-f0-9]*\)".*/\1/')
[ -n "$tid1" ] || die "task create failed: $created"
done_move=$(curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${tid1}" -H 'Content-Type: application/json' -d '{"status":"done"}')
printf '%s' "$done_move" | grep -Eq '"status": ?"done"' || die "done move failed: $done_move"
trap 'curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${tid1}" -H "Content-Type: application/json" -d "{\"status\":\"archived\"}" >/dev/null 2>&1 || true' EXIT
step "ok: worker completion path fired ($tid1 -> done)"

# the record rides the list with the right shape
rec=$(curl -s "${JOURNEY_BASE_URL}/notifications")
nid=$(printf '%s' "$rec" | python3 -c "
import json,sys
d=json.load(sys.stdin)
rows=[n for n in d['notifications'] if n['source_id']=='${tid1}']
print(rows[0]['id'] if rows else '')")
[ -n "$nid" ] || die "no notification row for $tid1"
printf '%s' "$rec" | python3 -c "
import json,sys
d=json.load(sys.stdin)
rows=[n for n in d['notifications'] if n['source_id']=='${tid1}']
n=rows[0]
assert n['kind']=='task' and n['verdict']=='ok' and n['read'] is False, n
assert n['title'].startswith('task ') and n['title'].endswith(' done'), n" \
  || die "record shape wrong: $rec"
# metadata only: the title line carries no prompt/session content
if printf '%s' "$rec" | grep -qF "${JRN_TASK}"; then
  die "title leaked card text (privacy law): $rec"
fi
step "ok: record kind=task verdict=ok, title metadata only"

# the unread count grew by exactly this record
after=$(curl -s "${JOURNEY_BASE_URL}/notifications" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d["notifications"]));' 2>/dev/null)
assert_eq "$after" "$((before + 1))" "one emit, one record (list $before -> $after)"

# --- read: one id, then everything ---
# mark-one: the record flips read, the unread filter drops it
curl -s -X POST "${JOURNEY_BASE_URL}/notifications/read" \
  -H 'Content-Type: application/json' -d "{\"id\": ${nid}}" >/dev/null
still=$(curl -s "${JOURNEY_BASE_URL}/notifications?unread=1" | python3 -c "
import json,sys
print(len([n for n in json.load(sys.stdin)['notifications'] if n['id']==${nid}]))")
assert_eq "$still" "0" "mark one read (unread filter drops the row)"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${JOURNEY_BASE_URL}/notifications/read" \
  -H 'Content-Type: application/json' -d '{"id": 99999999}')
assert_eq "$code" "404" "ghost id 404s"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${JOURNEY_BASE_URL}/notifications/read" \
  -H 'Content-Type: application/json' -d '{"all": true}')
assert_eq "$code" "200" "mark all read"
rest=$(curl -s "${JOURNEY_BASE_URL}/notifications" | python3 -c 'import json,sys; print(json.load(sys.stdin)["unread"])')
assert_eq "$rest" "0" "mark all clears the badge count"

# --- live: the events stream carries the next emit ---
# second throwaway task, still in triage until the frame lands
created2=$(curl -s -X POST "${JOURNEY_BASE_URL}/tasks" -H 'Content-Type: application/json' \
  -d "{\"title\":\"${JRN_TASK}-2\"}")
tid2=$(printf '%s' "$created2" | grep -oE '"id": ?"t_[a-f0-9]+"' | head -1 | sed 's/.*"\(t_[a-f0-9]*\)".*/\1/')
[ -n "$tid2" ] || die "second task create failed: $created2"
trap 'curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${tid1}" -H "Content-Type: application/json" -d "{\"status\":\"archived\"}" >/dev/null 2>&1; curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${tid2}" -H "Content-Type: application/json" -d "{\"status\":\"archived\"}" >/dev/null 2>&1 || true' EXIT

ssecmd=$(mktemp "${TMPDIR:-/tmp}/jrn-sse-XXXXXX.log")
curl -s -N "${JOURNEY_BASE_URL}/notifications/events" > "$ssecmd" 2>&1 &
ssepid=$!
sleep 1
curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${tid2}" -H 'Content-Type: application/json' -d '{"status":"done"}' >/dev/null

sse_ok=""
for _ in $(seq 1 20); do
  if grep -q "event: notification" "$ssecmd" 2>/dev/null; then
    frame=$(grep -A1 "event: notification" "$ssecmd" | grep "data:" | head -1)
    case "$frame" in
      *"$tid2"*) printf '%s' "$frame" | grep -qF '"kind":"task"' && sse_ok="yes" ;;
    esac
    [ "$sse_ok" = "yes" ] || sse_ok="bad-frame: $frame"
    break
  fi
  sleep 0.5
done
kill "$ssepid" 2>/dev/null; wait "$ssepid" 2>/dev/null
rm -f "$ssecmd"
[ "$sse_ok" != "" ] || die "SSE stream got no notification frame within 10s"
[ "$sse_ok" = "yes" ] || die "live frame wrong: $sse_ok"
step "ok: /notifications/events streamed the live emit"

step "GREEN: notifications journey"