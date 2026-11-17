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
#
# Card t_6a6c3355: nightly 43807490891 read "the terminal did not shrink"
# with the panes still at the seeded 200/440/220/400 - i.e. the press never
# engaged a handle, and nothing said whether the pointer event was ever
# DELIVERED. The recipe now grades every press against the document-level
# pointerdown receipt before it drags: a press the page never saw is a LANE
# input fault (retried ONCE, then it dies saying so), while a pointerdown
# that landed without a handle taking it stays a divider regression.
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
# hit_at <x> <y>: what OWNS the press point - the answer a missed gesture
# needs and cannot get from a pane-width reading. tag.class of the element
# under the point, "none" for nothing.
hit_at() {
  jsq "(()=>{const e=document.elementFromPoint($1,$2);if(!e)return 'none';const c=(e.className&&String(e.className))||e.tagName;return (c+'').split(' ').slice(0,3).join('.')})()" 2>&1 | tail -1 | tr -d '"'
}
# the press point must OWN a divider before the gesture starts: a red that
# says "the pane did not move" cannot tell a missed press from a dead
# drag, and the overnight 43807490891 red was exactly that (gesture-active=0
# with the panes still at their seeded 200/440/220/400 - the layout the
# recipe asked for, so nothing was stale). This precondition turns the next
# such red into the thing that blocked the press.
assert_press_target() {
  local x=$1 y=$2 hit
  hit=$(hit_at "$x" "$y")
  case "$hit" in
    *dock-divider*) return 0 ;;
  esac
  die "the press point ($x,$y) is not a divider - it is owned by [$hit] | $(geom_report)"
}
# drag_x <x> <to> <y>: a stepped press / move / release along x
# DRAG_ACTIVE is the gesture's own witness (see geom_report): sampled while
# the button is down, because the release clears the handle's `.active`.
DRAG_ACTIVE=""
PRESS_AT=""
PRESS_ACK=""
# ptr_n: how many pointerdowns the DOCUMENT has seen so far (the receipt
# installed before leg 1). A press that leaves this number unchanged never
# reached the page at all.
ptr_n() {
  jsq '(()=>{return (window.__ptrLog||[]).length})()' 2>&1 | tail -1 | tr -d '"'
}
# press_verify <x> <y>: press the point and grade what came back.
#   0 (engaged) - the handle took the gesture (some divider is `.active`)
#   1 (refused) - a pointerdown LANDED but no handle took it: an overlay,
#                 a dead handler, a product question
#   2 (lost)    - no pointerdown at all: the INPUT never reached the page,
#                 which is a lane/browser fault, not a divider regression
# The distinction is the whole point of nightly 43807490891 (card
# t_6a6c3355): that red read "the pane did not move" and could not say
# whether the gesture was never delivered.
press_verify() {
  local x=$1 y=$2 n0 n1
  n0=$(ptr_n)
  ab mouse move "$x" "$y" >/dev/null 2>&1
  ab mouse down >/dev/null 2>&1
  # two EVENTS decide this: a handle takes the capture (.active), or the
  # document at least saw the pointerdown. Wait for either, then grade - a
  # 100ms pause read "refused" for a press that was merely in flight
  # (card t_fdd12843).
  poll_js "(()=>{const act=[...document.querySelectorAll('.dock-divider')].some(d=>d.classList.contains('active'));return act||(window.__ptrLog||[]).length!==$n0})()" 3 >/dev/null 2>&1 || true
  DRAG_ACTIVE=$(jsq '(()=>{return [...document.querySelectorAll(".dock-divider")].filter(d=>d.classList.contains("active")).length})()' 2>&1 | tail -1 | tr -d '"')
  n1=$(ptr_n)
  PRESS_ACK="pointerdowns=$n0->$n1"
  [ "$DRAG_ACTIVE" = "0" ] || return 0
  [ "$n1" = "$n0" ] && return 2
  return 1
}
# press_or_die <x> <y>: one press, one bounded retry when the input was
# LOST (a lane input fault may drop a single press; a lost press must not
# read as a broken divider), and a loud lane-fault verdict when even the
# second press never arrives. A press that LANDED is never retried: that
# is the product's answer and the recipe reds on it.
press_or_die() {
  local x=$1 y=$2 rc
  press_verify "$x" "$y"; rc=$?
  if [ "$rc" = "2" ]; then
    ab mouse up >/dev/null 2>&1
    sleep 0.2   # recipe-lint: allow fixed-sleep - input pacing: the up must be its own task before the re-press or the pipeline coalesces them
    step "input-fault: the press at ($x,$y) never reached the page ($PRESS_ACK) - re-pressing once"
    press_verify "$x" "$y"; rc=$?
  fi
  case "$rc" in
    0) return 0 ;;
    2) ab mouse up >/dev/null 2>&1
       die "the press never reaches the page on this lane (2 presses at ($x,$y), pointerdowns frozen at $(ptr_n), owns=[$(hit_at "$x" "$y")]) - a browser input fault, not a divider regression | $(geom_report)" ;;
    *) ab mouse up >/dev/null 2>&1
       die "a pointerdown reached the page at ($x,$y) but no handle took it (owns=[$(hit_at "$x" "$y")]) | $(geom_report)" ;;
  esac
}
drag_x() {
  local cur=$1 to=$2 y=$3
  PRESS_AT="$cur,$y"
  assert_press_target "$cur" "$y"
  press_or_die "$cur" "$y"
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
  wait_layout   # the drag resized panes: hand back once the geometry holds
}
# geom_report: what a failed leg measured AGAINST - every divider's centre x
# (a `*` marks the handle holding the pointer capture) plus every pane's
# width. A leg that reads "no movement" is answered here: no `*` anywhere
# means the gesture never landed on a handle (a press that missed, an
# overlay in the way), while a `*` on the wrong divider means the handle
# order moved. Nightly 43586645271 red on this recipe's first leg with no
# way to tell those apart - the report is the missing witness.
geom_report() {
  local divs panes ptr
  divs=$(jsq '(()=>{return [...document.querySelectorAll(".dock-divider")].map(d=>{const r=d.getBoundingClientRect();return Math.round(r.x+r.width/2)+(d.classList.contains("active")?"*":"")}).join(",")})()' 2>&1 | tail -1 | tr -d '"')
  panes=$(jsq '(()=>{return [...document.querySelectorAll("[data-pane-id]")].map(p=>((p.className.match(/kind-([a-z_]+)/)||[])[1]||"?")+":"+Math.round(p.getBoundingClientRect().width)).join(" ")})()' 2>&1 | tail -1 | tr -d '"')
  # ptr: how many pointerdowns the DOCUMENT has seen, and the last one's
  # point. 0 next to a divider-owned press point = the input never reached
  # the page (a lane/browser input fault); >0 with gesture-active=0 = the
  # event landed but the handle did not take it (an overlay, a dead handler).
  ptr=$(jsq '(()=>{const l=window.__ptrLog||[];const last=l[l.length-1];return l.length+(last?"@"+Math.round(last.x)+","+Math.round(last.y)+"->"+last.tgt:"")})()' 2>&1 | tail -1 | tr -d '"')
  printf 'gesture-active=%s press=[%s] pointerdowns=[%s] dividers=[%s] panes=[%s]' \
    "${DRAG_ACTIVE:-?}" "${PRESS_AT:-?}" "${ptr:-?}" "$divs" "$panes"
}
# near <got> <want> <tol> <name>
near() {
  local d=$(( $1 - $2 ))
  [ "$d" -lt 0 ] && d=$(( -d ))
  [ "$d" -le "$3" ] || die "${4:-near}: got [$1] want [$2] (tol $3) | $(geom_report)"
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

# the gesture's input receipt (see geom_report): every pointerdown the
# DOCUMENT sees, with its point and owning element. Installed once, before
# the first press, on the capture phase so a pane-level stopPropagation
# cannot hide it.
jsq '(()=>{window.__ptrLog=[];document.addEventListener("pointerdown",function(e){var t=e.target||{};var c=(t.className&&String(t.className))||t.tagName||"?";window.__ptrLog.push({x:e.clientX,y:e.clientY,tgt:c.split(" ").slice(0,3).join(".")})},true);return 1})()' >/dev/null 2>&1

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
