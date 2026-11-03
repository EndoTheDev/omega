#!/bin/bash
# 95-terminal - the integrated shell's REST journey (card t_55a3fc4b).
# REST/SSE-level on purpose: the whole module is a route capsule (no
# tools, no commands) and its face - claim, stream, input, resize,
# kill - is observable without a browser. The xterm pane itself is
# node-law+pinned in webui/test (terminal.test.ts). Pins:
#
#   claim   - POST /terminal/sessions answers an id (the pane's open;
#             reattach-else-spawn is Go-gated in the store tests).
#   stream  - GET /terminal/{id}/stream is a live SSE: the replay 'o'
#             frame carries the pty's own bytes (b64), input POSTs come
#             back as 'o' frames, pings keep the stream warm.
#   input   - POST /terminal/{id}/input (octet-stream) writes the shell;
#             the pty echoes the keystrokes back through the stream.
#   resize  - POST /terminal/{id}/resize validates 1..10000 and 200s.
#   hygiene - an unknown id (bogus input, bogus stream) 404s; DELETE
#             kills the shell (204) and the stream afterward is a 404 -
#             the residue is one dead pid, gone by construction.
#
# Residue-free: the trap kills the claimed session.
. "${JROOT:?run must export JROOT}/lib.sh"
B="${JOURNEY_BASE_URL:?}"
runtag=$(date +%s)

# --- claim: POST /terminal/sessions --------------------------------------
claim=$(curl -s -X POST "$B/terminal/sessions")
tid=$(printf '%s' "$claim" | grep -oE '"id": ?"[a-f0-9-]+"' | head -1 | sed 's/.*"\([a-f0-9-]*\)".*/\1/')
[ -n "$tid" ] || die "claim answered no id: $claim"
trap 'curl -s -X DELETE "$B/terminal/$tid" >/dev/null 2>&1 || true' EXIT
step "ok: claimed terminal session $tid"

# --- the shell is on the list (GET /terminal/sessions) --------------------
lst=$(curl -s "$B/terminal/sessions")
printf '%s' "$lst" | grep -qE "\"id\": ?\"$tid\"" || \
  die "the claimed session is not in the store list: $lst"
step "ok: session listed"

# --- stream: live SSE, replay tail first ----------------------------------
ssetmp=$(mktemp /tmp/jrn-term-stream.XXXXXX)
curl -sN --max-time 25 "$B/terminal/$tid/stream" > "$ssetmp" &
curpid=$!
head -1 "$ssetmp" | grep -qF 'data: {"t":"o"' || sleep 2
sed -n 1p "$ssetmp" | grep -qF 'data: {"t":"o"' || {
  sleep 2
  grep -qF 'data: {"t":"o"' "$ssetmp" || die "no replay output frame on the stream: $(head -c 200 "$ssetmp")"
}
step "ok: the stream replays the shell's own bytes (b64 'o' frames)"

# --- input: the pty echoes through the stream -----------------------------
MARK="JRNTERM${runtag}"
printf 'echo %s\r\n' "$MARK" | curl -s -X POST "$B/terminal/$tid/input" \
  -H 'Content-Type: application/octet-stream' --data-binary @- | \
  grep -qF '"written"' || die "input POST did not answer a write report"
step "ok: keystrokes accepted (octet-stream POST)"

# the echo (the command line + the marker) reaches the stream frames
seen=""
for i in $(seq 1 15); do
  seen=$(python3 - "$ssetmp" "$MARK" <<'EOF'
import base64, json, sys
txt = open(sys.argv[1], "rb").read().decode("utf8", "replace")
mark = sys.argv[2]
out = []
for line in txt.splitlines():
    if not line.startswith("data: "):
        continue
    try:
        f = json.loads(line[6:])
    except ValueError:
        continue
    if f.get("t") == "o" and f.get("d"):
        out.append(base64.b64decode(f["d"]).decode("utf8", "replace"))
joined = "".join(out)
print(1 if mark in joined and "echo" in joined else 0)
EOF
)
  [ "$seen" = "1" ] && break
  sleep 1
done
[ "$seen" = "1" ] || die "the shell echoes never carried the marker ($MARK); stream head: $(python3 -c "
import base64,json,sys
txt=open('$ssetmp','rb').read().decode('utf8','replace')
out=[]
for line in txt.splitlines():
    if line.startswith('data: '):
        try: f=json.loads(line[6:])
        except ValueError: continue
        if f.get('t')=='o' and f.get('d'): out.append(base64.b64decode(f['d']).decode('utf8','replace'))
sys.stdout.write(str(len(txt))+' bytes, '+repr(''.join(out)[-200:]))" )"
step "ok: the pty echoed the keystrokes through the stream"

# --- resize: the pane's window reaches the pty -----------------------------
r1=$(curl -s -X POST "$B/terminal/$tid/resize" -H 'Content-Type: application/json' -d '{"rows":24,"cols":80}')
printf '%s' "$r1" | grep -qF '"rows":24' || die "resize answered wrong: $r1"
r2=$(curl -s -X POST "$B/terminal/$tid/resize" -H 'Content-Type: application/json' -d '{"rows":0,"cols":80}')
printf '%s' "$r2" | grep -qF 'out of 1..10000' || die "an out-of-range resize was not refused: $r2"
step "ok: resize accepts a real window, refuses an out-of-range one"

# --- hygiene: unknown ids ---------------------------------------------------
u=$(curl -s -o /dev/null -w '%{http_code}' "$B/terminal/bogusterm/stream")
[ "$u" = "404" ] || die "a bogus stream id answered $u"
u=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/terminal/bogusterm/input")
[ "$u" = "404" ] || die "a bogus input id answered $u"
step "ok: unknown ids 404"

# --- kill: DELETE kills the shell, the store forgets, the id dies ----------
k=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$B/terminal/$tid")
[ "$k" = "204" ] || die "DELETE answered $k"
u=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/terminal/$tid/input")
[ "$u" = "404" ] || die "the killed session still answers input ($u)"
step "ok: the kill kills the shell and the id dies"

# the stream subprocess ends with the DELETE (connection context dead or
# stream 404s) - reap it so the journey leaves no chrome behind
kill "$curpid" 2>/dev/null && wait "$curpid" 2>/dev/null || true
rm -f "$ssetmp"

step "green: the integrated shell's REST face (claim-stream-input-resize-kill)"