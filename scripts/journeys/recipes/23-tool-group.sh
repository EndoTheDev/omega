#!/bin/bash
# 23-tool-group - the RENDERING law (card t_e4280de7): a run with 3+
# tool calls renders its consecutive rows as ONE collapsible group. The
# recipe drives a real run that issues three tool calls in a row (no
# prose between), then asserts:
#   1. the run's rows render inside a details.toolgroup (grouped);
#   2. auto mode has the settled group collapsed (the vertical-noise win);
#   3. clicking the summary expands it (rows visible) and re-collapses;
#   4. the persisted display mode (omega.tool-display) drives the group
#      across a reload - collapsed keeps it shut, expanded opens it.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session toolgroup | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) tool-group $TAG"

open_app

# open the throwaway session: click its sidebar row by unique label
open_journey_row "(journey) tool-group $TAG"
step "ok: opened throwaway session $SID"

panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) tool-group')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}

# panejs_expr <expr over p>: the same pane-scoped expression, for a poll.
# poll_js_value runs bare jsq (panejs is a runner, not an expression), so the
# scope wrapper rides the string and returns the bare value - panejs
# stringifies for its callers, a poll compares the number itself.
panejs_expr() {
  printf '%s' "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) tool-group')));if(!p)return 'NO_PANE';return ($1);})()"
}

# the composer's editor is pane-scoped - focus THIS pane's (recipe 20's trip)
focus=$(poll_js_value "$(panejs_expr '(p.querySelector(".composer .composer-input")||{}).focus ? 1 : 0')" -eq 1)
if [ "$focus" = "1" ]; then
  panejs 'p.querySelector(".composer .composer-input").focus(), 1' >/dev/null 2>&1
fi
assert_eq "$focus" "1" "focused pane's composer editor reachable"

# the prompt: three tool calls back to back, no prose between them -
# groupToolRuns only folds STRICTLY consecutive tool rows (the law).
ab keyboard type "Use the shell.run tool to make exactly three separate tool calls in a row with no text in between: run echo one, then run echo two, then run echo three. Do not write anything between the calls. After the three calls, reply with just: grouped"
ab press Enter
ab press Enter
step "typed"

# wait for the run to START (the cancel face appears, poll - a fast
# text-only answer beats a fixed sleep) then finish
poll_js '!document.querySelector("button.send.cancel, .composer .cancel")' 30 || \
  die "cancel face never appeared - the model answered without a tool run (premise unmet)"
wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 240
step "ok: run finished"

# the transcript must be SETTLED before the premise read: the canvas
# item stream can lag the composer face - poll until the transcript
# reads running:false and holds stable across two samples
settle=0
prev=""
for i in $(seq 1 60); do
  cur=$(curl -s "$JOURNEY_BASE_URL/sessions/$SID/transcript" | python -c "
import json,sys
d=json.load(sys.stdin)
items=d.get('items') if isinstance(d,dict) else (d or [])
n=len([i for i in (items if isinstance(items,list) else [])])
print(('RUNNING' if d.get('running') else ('%d' % n)))
" 2>/dev/null || echo ERR)
  if [ "$cur" != "ERR" ] && [ "$cur" != "RUNNING" ] && [ -n "$cur" ] && [ "$cur" = "$prev" ]; then
    settle=$((settle + 1))
    [ "$settle" -ge 2 ] && break
  else
    settle=0
  fi
  prev="$cur"
  sleep 1
done
[ "$settle" -ge 2 ] || die "transcript never settled (last: $prev)"
step "ok: transcript settled at $prev items"

# the premise, from the SERVER's transcript (GET /sessions/<id>/transcript,
# the same items the pane renders): run lengths of consecutive RENDERED
# tool rows. The wire is not the render: a tool_result item merges onto
# its call's row and paints nothing (omega.ts mapTranscriptItems), and a
# thin item (an assistant message or thinking block with empty content)
# paints nothing either - so neither can break a run of rendered rows.
# A verdict tool call DOES break it: it paints a card, not a tool row.
# The model is real - if it interleaved real prose, the runs say so and
# any red names the premise, never a phantom presentation bug. The
# transcript rides the run's evidence dir.
TRFILE="${JOURNEY_WORKTMP:?no journey worktmp}/transcript-$SID.json"
curl -s "$JOURNEY_BASE_URL/sessions/$SID/transcript" > "$TRFILE"
RUNLEN=$(python - "$TRFILE" <<'PYEOF'
import json, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
items = data.get("items") if isinstance(data, dict) else data
if not isinstance(items, list):
    print("NOITEMS")
else:
    # renders(it) mirrors the client projection (webui/composables/
    # omega.ts mapTranscriptItems + groupToolRuns):
    #   "tool"  -> a rendered tool row
    #   "break" -> a rendered NON-tool row (a verdict card, prose, a user
    #              bubble): the run ends here
    #   None    -> paints nothing at all: transparent to the run
    VERDICT_TOOLS = {"verdict.request"}
    def renders(it):
        kind = it.get("kind")
        if kind == "tool_result":
            # merges onto its call row - no row of its own
            return None
        if kind == "tool_call":
            name = (it.get("tool_call") or {}).get("name")
            return "break" if name in VERDICT_TOOLS else "tool"
        if kind == "user":
            if it.get("content") or it.get("images") or it.get("attachments"):
                return "break"
            return None
        if kind in ("message", "thinking"):
            return "break" if it.get("content") else None
        return "break"
    runs, n = [], 0
    for it in items:
        what = renders(it)
        if what == "tool":
            n += 1
            continue
        if what is None:
            continue
        if n:
            runs.append(n)
            n = 0
    if n:
        runs.append(n)
    print(runs)
PYEOF
)
case "$RUNLEN" in
  NOITEMS|"") die "transcript unreadable ($RUNLEN) - GET /sessions/$SID/transcript did not render items" ;;
esac
step "transcript tool runs: $RUNLEN"

# the DOM must fold EXACTLY the server's runs: every >=2 run renders
# inside a details.toolgroup, every length-1 run stays in flow
groups=$(panejs 'p.querySelectorAll("details.toolgroup").length')
ingrp=$(panejs '[...p.querySelectorAll("details.toolgroup")].reduce((n,g)=>n+g.querySelectorAll(".toolcall").length,0)')
flat=$(panejs '[...p.querySelectorAll(".toolcall")].filter(r=>!r.closest("details.toolgroup")).length')
case "${groups:-x}|${ingrp:-x}|${flat:-x}" in *x*) die "group/row counts unreadable in the pane" ;; esac
EXPECT_GROUPS=$(python -c "import sys; runs=eval(sys.argv[1]); print(sum(1 for n in runs if n>=2))" "$RUNLEN")
EXPECT_INGRP=$(python -c "import sys; runs=eval(sys.argv[1]); print(sum(n for n in runs if n>=2))" "$RUNLEN")
EXPECT_FLAT=$(python -c "import sys; runs=eval(sys.argv[1]); print(sum(n for n in runs if n==1))" "$RUNLEN")
assert_eq "$groups" "$EXPECT_GROUPS" "group count matches the transcript's runs ($RUNLEN)"
assert_eq "$ingrp" "$EXPECT_INGRP" "rows inside groups match the transcript"
assert_eq "$flat" "$EXPECT_FLAT" "flat rows match the transcript's length-1 runs"

# the law's own words: a >=3-run must exist, grouped, in ONE block
HASTHREES=$(python -c "import sys; runs=eval(sys.argv[1]); print(1 if any(n>=3 for n in runs) else 0)" "$RUNLEN")
[ "$HASTHREES" = "1" ] || die "premise unmet: no >=3 consecutive tool run in the transcript ($RUNLEN) - the model interleaved non-tool items, the DOM half of the law is untestable this run"
[ "$groups" -ge 1 ] 2>/dev/null || die "3+ consecutive tool rows rendered UNgrouped - the rendering law is broken"
g3=$(panejs '[...p.querySelectorAll("details.toolgroup")].some(g=>g.querySelectorAll(".toolcall").length>=3)')
assert_eq "$g3" "true" "a >=3-row run renders inside ONE group"

# 2. auto: a SETTLED group collapses (the vertical-noise reduction)
openv=$(panejs 'p.querySelector("details.toolgroup").open')
assert_eq "$openv" "false" "settled group auto-collapses"

# 3. expands on click; the rows render; collapses again on click
panejs 'p.querySelector("details.toolgroup").querySelector("summary").click(), 1' >/dev/null 2>&1
sleep 0.3
openv=$(panejs 'p.querySelector("details.toolgroup").open')
assert_eq "$openv" "true" "summary click expands the group"
panejs 'p.querySelector("details.toolgroup").querySelector("summary").click(), 1' >/dev/null 2>&1
sleep 0.3
openv=$(panejs 'p.querySelector("details.toolgroup").open')
assert_eq "$openv" "false" "second click collapses the group"

# 4. the persisted mode drives the group across a reload - write the
# SAME localStorage key the settings knob writes (omega.tool-display),
# reload, and the group mounts in the persisted state.
jsq 'localStorage.setItem("omega.tool-display","collapsed"), true' >/dev/null 2>&1
ab reload >/dev/null 2>&1
sleep 2
waits=0
until panejs 'p.querySelector("details.toolgroup") ? 1 : 0' 2>/dev/null | tail -1 | grep -q '^1$'; do
  waits=$((waits + 1))
  [ "$waits" -gt 15 ] && die "group never rendered after reload (persisted collapsed check)"
  sleep 1
done
openv=$(panejs 'p.querySelector("details.toolgroup").open')
assert_eq "$openv" "false" "persisted collapsed mode survives the reload"

jsq 'localStorage.setItem("omega.tool-display","expanded"), true' >/dev/null 2>&1
ab reload >/dev/null 2>&1
sleep 2
waits=0
until panejs 'p.querySelector("details.toolgroup") ? 1 : 0' 2>/dev/null | tail -1 | grep -q '^1$'; do
  waits=$((waits + 1))
  [ "$waits" -gt 15 ] && die "group never rendered after reload (persisted expanded check)"
  sleep 1
done
openv=$(panejs 'p.querySelector("details.toolgroup").open')
assert_eq "$openv" "true" "persisted expanded mode survives the reload"

assert_no_console_errors
