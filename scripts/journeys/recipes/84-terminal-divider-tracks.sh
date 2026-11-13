#!/bin/bash
# 84-terminal-divider-tracks - the terminal pane's width tracks the divider
# pointer on ALL FOUR LEGS (card t_8098aa06).
#
# Endo's report (2026-11-13): dragging the divider at the terminal's RIGHT
# side INWARD shrank the terminal visibly slow, while the same divider
# outward and both legs of its LEFT divider were correct. The cause was the
# fixed lanes' absorb: only the fixed pane's px was written and the flex
# reflow spread the delta over EVERY flexible child, so the terminal (a
# small pane beside the big chat) took only its share of the drag - and the
# chat, which the gesture never touched, moved too. The pin is geometry, so
# the witness is the browser: a measured px delta per leg.
#
# The layout is seeded as the persisted v:3 record (the user's dock state -
# the reader validates it) because the gesture under test is a DIVIDER
# drag, not a tab move: dragging the Terminal tab out first would make this
# recipe about HTML5 DnD instead. At 1280x800: pane area 1272 minus three
# 4px gutters = 1260 content px; sidebar 200 + inspector 400 fixed, the
# chat and the terminal split the remaining 660 by their recorded 440:220.
#
# Residue: the terminal pane claims a pty on mount; the trap DELETEs every
# session id this run minted (the before/after diff of GET
# /terminal/sessions) so the suite leaves no shell behind.
. "${JROOT:?run must export JROOT}/lib.sh"
B="${JOURNEY_BASE_URL:?}"

term_ids() {
  curl -s "$B/terminal/sessions" | grep -oE '"id": ?"[a-f0-9-]+"' | sed 's/.*"\([a-f0-9-]*\)"/\1/' | sort
}
TERM_BEFORE=$(term_ids)
cleanup_term() {
  local id
  for id in $(comm -13 <(printf '%s\n' "$TERM_BEFORE") <(term_ids)); do
    curl -s -X DELETE "$B/terminal/$id" >/dev/null 2>&1 || true
  done
}
trap cleanup_term EXIT

open_app
ab set viewport 1280 800 >/dev/null 2>&1

SEED='{"v":3,"tree":{"kind":"split","id":"n6","direction":"column","children":[{"kind":"split","id":"n2","direction":"row","children":[{"kind":"pane","id":"n3","tabs":[{"component":"sidebar"}],"active":0,"fixed":200,"edge":"left"},{"kind":"pane","id":"n4","tabs":[{"component":"chat","key":"c1"}],"active":0,"fixed":null},{"kind":"pane","id":"n7","tabs":[{"component":"terminal"}],"active":0,"fixed":null},{"kind":"pane","id":"n1","tabs":[{"component":"files"},{"component":"cron"},{"component":"skills"},{"component":"tools"}],"active":0,"fixed":400,"edge":"right"}],"px":[200,440,220,400]},{"kind":"pane","id":"n5","tabs":[{"component":"observability"}],"active":0,"fixed":200,"edge":"bottom"}],"px":[800,200]},"closedEdges":[]}'

js "localStorage.setItem('omega.dock-layout', '$SEED')" >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

# helpers ------------------------------------------------------------------
# pw <pane-id>: the pane's rendered width
pw() {
  jsq "(()=>{const p=document.querySelector('[data-pane-id=\"$1\"]');return p?Math.round(p.getBoundingClientRect().width):-1})()" | tail -1
}
# hostw: the terminal's own host box (the pane minus its 2px frame)
hostw() {
  jsq '(()=>{const e=document.querySelector(".terminal-host");return e?Math.round(e.getBoundingClientRect().width):-1})()' | tail -1
}
# divx <index>: the divider's grab centre (it moves as panes resize)
divx() {
  jsq "(()=>{const d=document.querySelectorAll('.dock-divider')[$1];if(!d)return -1;const r=d.getBoundingClientRect();return Math.round(r.x+r.width/2)})()" | tail -1
}
# drag_x <x> <to> <y>: a stepped press / move / release along x
drag_x() {
  local cur=$1 to=$2 y=$3
  ab mouse move "$cur" "$y" >/dev/null 2>&1
  ab mouse down >/dev/null 2>&1
  while [ "$cur" -ne "$to" ]; do
    if [ "$to" -gt "$cur" ]; then
      cur=$((cur + 10)); [ "$cur" -gt "$to" ] && cur=$to
    else
      cur=$((cur - 10)); [ "$cur" -lt "$to" ] && cur=$to
    fi
    ab mouse move "$cur" "$y" >/dev/null 2>&1
    sleep 0.03
  done
  ab mouse up >/dev/null 2>&1
  sleep 0.3
}
# near <got> <want> <tol> <name>
near() {
  local d=$(( $1 - $2 ))
  [ "$d" -lt 0 ] && d=$(( -d ))
  [ "$d" -le "$3" ] || die "${4:-near}: got [$1] want [$2] (tol $3)"
  step "ok: ${4:-near} ($1)"
}

# the terminal pane is real: the pty claimed and xterm painted rows
wait_js '(()=>{const h=document.querySelector(".terminal-host");return !!h && document.querySelectorAll(".xterm-rows>div").length>0})()' 20 || \
  die "the terminal pane never painted (no .terminal-host rows)"
step "ok: the terminal pane is live"

T_PANE=$(jsq '(()=>{const p=[...document.querySelectorAll("[data-pane-id]")].find(e=>e.classList.contains("kind-terminal"));return p?p.getAttribute("data-pane-id"):""})()' | tail -1 | tr -d '"')
[ -n "$T_PANE" ] || die "no terminal pane in the seeded tree"
TERM0=$(pw "$T_PANE")
CHAT=$(jsq '(()=>{const p=document.querySelector(".dock-pane.kind-chat");return p?p.getAttribute("data-pane-id"):""})()' | tail -1 | tr -d '"')
INSP=$(jsq '(()=>{const p=document.querySelector(".dock-pane.kind-files");return p?p.getAttribute("data-pane-id"):""})()' | tail -1 | tr -d '"')
CHAT0=$(pw "$CHAT")
INSP0=$(pw "$INSP")
HOST0=$(hostw)
[ "$TERM0" -gt 150 ] || die "the terminal pane rendered ${TERM0}px (want > 150)"
[ "$CHAT0" -gt 400 ] || die "the chat pane rendered ${CHAT0}px (want > 400)"
step "ok: seeded dock [sidebar 200, chat $CHAT0, terminal $TERM0, inspector $INSP0]"

# --- leg 1: the terminal's RIGHT divider, INWARD (the reported break) ------
# drag LEFT 60: the inspector grows 60, the terminal gives exactly 60
x=$(divx 2)
[ "$x" -gt 0 ] || die "no third divider for the terminal's right side"
drag_x "$x" "$((x - 60))" 400
near "$(pw "$T_PANE")" "$((TERM0 - 60))" 2 "right divider inward: the terminal shrinks with the pointer"
near "$(hostw)" "$((HOST0 - 60))" 4 "right divider inward: the terminal host follows"
near "$(pw "$INSP")" "$((INSP0 + 60))" 2 "right divider inward: the inspector takes the 60"
near "$(pw "$CHAT")" "$CHAT0" 1 "right divider inward: the chat (not adjacent) never moves"

# --- leg 2: the same divider, OUTWARD --------------------------------------
x=$(divx 2)
drag_x "$x" "$((x + 60))" 400
near "$(pw "$T_PANE")" "$TERM0" 2 "right divider outward: the terminal grows back 1:1"
near "$(pw "$INSP")" "$INSP0" 2 "right divider outward: the inspector restores"
near "$(pw "$CHAT")" "$CHAT0" 1 "right divider outward: the chat still holds"

# --- leg 3: the terminal's LEFT divider, both directions --------------------
x=$(divx 1)
drag_x "$x" "$((x - 60))" 400
near "$(pw "$T_PANE")" "$((TERM0 + 60))" 2 "left divider left: the terminal grows with the pointer"
drag_x "$((x - 60))" "$x" 400
near "$(pw "$T_PANE")" "$TERM0" 2 "left divider right: the terminal shrinks back 1:1"

# --- leg 4: the sidebar divider (fixed pane LEFT, terminal neighbour) ------
x=$(divx 0)
drag_x "$x" "$((x + 40))" 400
near "$(pw "$CHAT")" "$((CHAT0 - 40))" 2 "sidebar divider: the adjacent chat gives the 40"
near "$(pw "$T_PANE")" "$TERM0" 1 "sidebar divider: the terminal two panes away does not move"
drag_x "$((x + 40))" "$x" 400
near "$(pw "$CHAT")" "$CHAT0" 2 "sidebar divider out: the chat restores 1:1"

# the record round-trips (the sizes the drag wrote re-derive the px)
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
near "$(pw "$T_PANE")" "$TERM0" 4 "the layout survives a reload at the dragged widths"
near "$(pw "$CHAT")" "$CHAT0" 4 "the chat survives the reload too"

assert_no_console_errors
step "green: the terminal tracks the divider on all four legs (t_8098aa06)"
