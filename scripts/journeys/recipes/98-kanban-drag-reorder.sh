#!/bin/bash
# 98-kanban-drag-reorder - the board drag seam live in a browser
# (t_09b44af1's journey step, card t_b86f709d). A card dragged through
# the REAL pointer pipeline (agent-browser mouse down/move/up feeding
# the board's window-level pointermove/pointerup listeners - synthetic
# PointerEvents from eval cannot witness the gesture) must:
#
#   a. reorder its own column with live mid-drag feedback, and the
#      GET /tasks order must survive a reload;
#   b. cross columns with the drop slot's {status, after_id} riding ONE
#      PATCH - the card lands exactly at the slot the live view showed;
#   c. revert when released off the board - no PATCH, the server order
#      unchanged, the optimistic move snapped back.
#   d. the push shape: an API edit/create reaches the idle board with no
#      reload and no poll (the canvas-delta refetch).
#   e. lane rails: any lane folds to a 36px rail, the rail is a drop
#      target and the unfold control, an empty lane folds itself.
#   f. card polish + the running heartbeat chip (card t_65ea61a1): a long
#      title clamps at two lines without spilling its lane, the assignee
#      is a chip, a running card wears "working - Xm", and an AGED
#      last_heartbeat_at flips that chip to its stale face.
#
# The board is PINNED (j98-drag, reused across runs like recipe 78's
# j78-a); this run's cards carry the run tag in the title and arch out
# at exit; non-archived leftovers of a crashed earlier run are archived
# first so the seeded order is dense from zero.
#
# Card t_2bfd52ac: the switcher's chip wait is TWO-MOUNT (poll, reload,
# re-poll) - the nightly red 42166671721 was one flaky /boards fetch
# burning the cap, not a provider gate (78 waits the same chip on the
# same no-provider lane and is green). The die names the API answer.
#
# Card t_54dd7215: the fetch-retry theory is FALSIFIED - the nightly
# reds 4-for-4 WITH the bounded retry merged (d73402e0). The second-miss
# die now also carries dom/body/console evidence (switch_chip_miss_die)
# so the next red answers Board.vue-never-mounted vs
# fetch-died-before-chip vs SPA-never-rendered by itself.
. "${JROOT:?run must export JROOT}/lib.sh"

BTAG="j98"
OMEGA_BIN="${OMEGA_BIN:-$JROOT/../../bin/omega}"
BOARD="$BTAG-drag"
TAG="$(date +%s%N | cut -c1-13)"
A=""; B=""; C=""; D=""; E=""

# ensure_board <name>: the pinned board exists on every run after the
# first - create only when genuinely missing (recipe 78's shape).
ensure_board() {
  if "$OMEGA_BIN" kanban board create "$1" >/dev/null 2>&1; then
    return 0
  fi
  if "$OMEGA_BIN" kanban board list 2>/dev/null | sed 's/^[* ] \+//' | awk '{print $1}' | grep -qx "$1"; then
    return 0
  fi
  die "board ensure $1"
}

# archive_cards <ids...>: the throwaway cleanup - the store has no
# delete, so journey cards arch out quietly.
archive_cards() {
  local id
  for id in "$@"; do
    [ -n "$id" ] || continue
    curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${id}?board=${BOARD}" \
      -H 'Content-Type: application/json' -d '{"status":"archived"}' >/dev/null || true
  done
}

# col_ids <status>: the server's dense order for that column (the same
# GET /tasks?board=&status= read the board UI renders from).
col_ids() {
  curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}&status=$1" \
    | tr -d '\r' | grep -oE '"id":"t_[0-9a-f]+"' | grep -oE 't_[0-9a-f]+' \
    | tr '\n' ' ' | sed 's/ *$//'
}

# card_id <status> <title>: seed one task, print its id.
card_id() {
  curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" \
    -H 'Content-Type: application/json' \
    -d "{\"title\": \"${BTAG}-drag ${TAG} $2\", \"status\": \"$1\"}" \
    | grep -oE 't_[0-9a-f]+' | head -1
}

# card_full <status> <title> <assignee> <body>: seed one task carrying
# the card-polish fields (card t_65ea61a1), print its id. The run tag
# stays in the title (the board-switch wait keys on it).
card_full() {
  curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" \
    -H 'Content-Type: application/json' \
    -d "{\"title\": \"${BTAG}-drag ${TAG} $2\", \"assignee\": \"$3\", \"body\": \"$4\", \"status\": \"$1\"}" \
    | grep -oE 't_[0-9a-f]+' | head -1
}

# card_geo <id>: sets CX CY TOP H - the card's viewport center, top and
# height (single eval, quotes stripped).
card_geo() {
  local v
  v=$(jsq "$(printf '(()=>{const c=[...document.querySelectorAll(".card")].find(x=>x.dataset.id==="%s");if(!c) return "0,0,0,0";const r=c.getBoundingClientRect();return Math.round(r.x+r.width/2)+","+Math.round(r.y+r.height/2)+","+Math.round(r.y)+","+Math.round(r.height)})()' "$1")" 2>&1 | tail -1 | tr -d '"')
  CX="${v%%,*}"; CY="$(echo "$v" | cut -d, -f2)"; TOP="$(echo "$v" | cut -d, -f3)"; H="$(echo "$v" | cut -d, -f4)"
  [ "$CX" != "0" ] || die "card $1 not rendered (geo: $v)"
}

# dom_order_expr <status> <ids>: JS returning true when the rendered
# card id order inside that column matches.
dom_order_expr() {
  printf '(()=>{const col=[...document.querySelectorAll(".board-col")].find(c=>c.dataset.status==="%s");if(!col) return false;return [...col.querySelectorAll(".card")].map(c=>c.dataset.id).join(" ")==="%s"})()' "$1" "$2"
}

# wait_dom_order <status> <ids> <timeout_s>: poll the rendered order.
wait_dom_order() {
  wait_js "$(dom_order_expr "$1" "$2")" "$3"
}

# assert_dragging <id>: the dragged card renders the live-wrestled state.
assert_dragging() {
  local out
  out=$(jsq "$(printf '(function(){const c=[...document.querySelectorAll(".card")].find(x=>x.dataset.id==="%s");return c&&c.classList.contains("dragging")})()' "$1")" 2>&1 | tail -1)
  [ "$out" = "true" ] || die "card $1 does not render mid-drag state (got: $out)"
}

# drag_to <fromX> <fromY> <toX> <toY>: press, walk the pointer in six
# steps, settle on the target. The release is separate - a mid-drag
# assert needs the gesture still live.
drag_to() {
  local fx="$1" fy="$2" tx="$3" ty="$4" dx dy x y i
  dx=$(( (tx - fx) / 6 )); dy=$(( (ty - fy) / 6 ))
  [ "$dx" -ne 0 ] || [ "$dy" -ne 0 ] || die "drag target equals source ($fx,$fy)"
  ab mouse move "$fx" "$fy" >/dev/null 2>&1
  sleep 0.1
  ab mouse down >/dev/null 2>&1
  sleep 0.1
  x=$(( fx + dx )); y=$(( fy + dy ))
  i=1
  while [ "$i" -lt 6 ]; do
    ab mouse move "$x" "$y" >/dev/null 2>&1
    sleep 0.06
    x=$(( x + dx )); y=$(( y + dy )); i=$(( i + 1 ))
  done
  ab mouse move "$tx" "$ty" >/dev/null 2>&1
  sleep 0.3
}

drag_release() {
  ab mouse up >/dev/null 2>&1
  sleep 0.2
}

# switch_chip_miss_die: the two-mount die WITH its own diagnosis
# (t_54dd7215 - the fetch-retry theory is falsified: CI reds 4-for-4
# WITH the bounded /boards retry merged, while GET /boards answers
# 5 boards; the local lane is green on the same tip). Runs in the die
# path only. Each field is fault-tolerant: a dead browser must not
# blank the other fields' evidence.
#   dom  - url + .board container + .board-col/.card counts + switcher
#          root: tells Board.vue-never-mounted apart from
#          fetch-died-before-chip (board/cards rendered, switch shell
#          there, chip gated) and from SPA-never-rendered.
#   body - document.body.innerText sliced to 300 chars: what the SPA
#          actually shows on the red.
#   console - the session console bucket: a crashed hydrate or a chunk
#          load failure shows up here before any assertion.
switch_chip_miss_die() {
  local errs body dom answer
  errs=$(ab console 2>&1 | tr '\n' ' ' | tail -c 600)
  [ -n "$errs" ] || errs="(ab console returned nothing)"
  body=$(ab eval 'document.body.innerText.replace(/\s+/g," ").slice(0,300)' 2>&1 | tail -1)
  [ -n "$body" ] || body="(body eval failed)"
  dom=$(ab eval '(()=>{return "url="+location.pathname+" board="+(document.querySelector(".board")?1:0)+" cols="+document.querySelectorAll(".board-col").length+" cards="+document.querySelectorAll(".card").length+" switchroot="+(document.querySelector(".board-switch-select")?1:0)+" chip="+(document.querySelector(".board-switch-select .model-chip")?1:0)})()' 2>&1 | tail -1)
  [ -n "$dom" ] || dom="(dom eval failed)"
  answer=$(curl -s "${JOURNEY_BASE_URL}/boards" | head -c 200)
  die "board switcher chip never rendered on two mounts | GET /boards: ${answer:-unavailable} | dom: ${dom} | body: ${body} | console: ${errs}"
}

switch_to_board() {
  # the switcher rides the BandSelect listbox (recipe 78's shape): open
  # the pop, then pick the pinned board by exact option text.
  #
  # The chip rides ONE mounted fetch: Board.vue gates the switcher on
  # `boards.length > 1 || boardName` and fetchBoards() runs once per
  # mount with no retry - a fetch that misses (lane hiccup under suite
  # load: nightly run 42166671721 red at the hard wait here, unrepro on
  # two zero-provider full-suite runs) leaves no chip for the whole page
  # life. So: poll first; on a miss reload - a fresh mount re-fetches -
  # and poll again; die only naming what the registry answered, so the
  # next red carries its own diagnosis instead of a blind timeout.
  if ! poll_js "(()=>{return !!document.querySelector('.board-switch-select .model-chip')})()" 15; then
    ab reload >/dev/null 2>&1 || true
    ab wait --load networkidle >/dev/null 2>&1 || true
    poll_js "(()=>{return !!document.querySelector('.board-switch-select .model-chip')})()" 10 ||
      switch_chip_miss_die
  fi
  js '(()=>{const c=document.querySelector(".board-switch-select .model-chip");if(c.getAttribute("aria-expanded")!=="true")c.click();return 1})()' >/dev/null
  wait_js "(()=>{return [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD')})()" 15
  js "$(printf '(()=>{const o=[...document.querySelectorAll(".board-switch-select .model-list li")].find(l=>(l.textContent||"").trim()==="%s");if(!o) return 0;o.click();return 1})()' "$BOARD")" >/dev/null
  wait_js "(()=>{return document.body.textContent.includes('$TAG')})()" 15
}

step "pinning board: $BOARD"
ensure_board "$BOARD"

# clear any non-archived leftovers of a crashed earlier run
for st in triage todo scheduled ready running blocked review done; do
  archive_cards $(col_ids "$st")
done

# seed: three in triage, one in todo. Fresh cards land on TOP of a column
# (topPosition is MIN-1), so seed in REVERSE creation order - c, then b,
# then a - leaving the display order a,b,c dense from zero.
C=$(card_id triage c)
B=$(card_id triage b)
A=$(card_id triage a)
D=$(card_id todo d)
trap 'archive_cards "$A" "$B" "$C" "$D" "$E"' EXIT
[ -n "$A" ] && [ -n "$B" ] && [ -n "$C" ] && [ -n "$D" ] || die "card seeding failed"

# ---- boot ----------------------------------------------------------------
# open_app comes BEFORE the seed-order assert (and before every other
# early die): the browser here is agent-browser's own auto-launched
# chrome (no CDP attach ran), and the first ab call that goes through a
# pipe - die's `ab screenshot | tail` - HANGS while the daemon is still
# launching that chrome (the spawned tree holds the pipe's write-end,
# proven live on winbtw: a fresh-session piped first call never returns).
# open_app's unpiped ab open starts the chrome; every later piped call
# hits a warm daemon that spawns nothing.
open_app
ab open "${JOURNEY_BASE_URL}/board" >/dev/null 2>&1 || die "cannot open /board"
ab wait --load networkidle >/dev/null 2>&1 || true
switch_to_board

[ "$(col_ids triage)" = "$A $B $C" ] || die "triage seed order not dense (got: $(col_ids triage))"
[ "$(col_ids todo)" = "$D" ] || die "todo seed order not dense (got: $(col_ids todo))"
step "ok: seeded triage a/b/c, todo d on $BOARD"

# ---- a: drag C into the gap between A and B (in-column) --------------------
card_geo "$C"; afx=$CX; afy=$CY
card_geo "$B"; atx=$CX; aty=$(( TOP + 6 ))   # B's top edge: the upper half
drag_to "$afx" "$afy" "$atx" "$aty"

# mid-drag: the pushed-around list IS the feedback
wait_dom_order triage "$A $C $B" 6
assert_dragging "$C"
step "ok: in-column drag reorders live while the gesture is down"

drag_release
sleep 1
got="$(col_ids triage)"
[ "$got" = "$A $C $B" ] || die "in-column drop did not persist (got [$got] want [$A $C $B])"
wait_dom_order triage "$A $C $B" 8
step "ok: in-column drop persisted (GET order a,c,b)"

# and it survives a reload
ab reload >/dev/null 2>&1 || true
ab wait --load networkidle >/dev/null 2>&1 || true
switch_to_board
got="$(col_ids triage)"
[ "$got" = "$A $C $B" ] || die "order did not survive reload (got [$got])"
wait_dom_order triage "$A $C $B" 8
step "ok: order survives a reload"

# ---- b: drag B into the todo column, below D (cross-column) -----------------
card_geo "$B"; bfx=$CX; bfy=$CY
card_geo "$D"; btx=$CX; bty=$(( TOP + H - 6 ))   # D's bottom edge
drag_to "$bfx" "$bfy" "$btx" "$bty"

# mid-drag: the optimistic move already shows B below D in todo
wait_dom_order todo "$D $B" 6
assert_dragging "$B"
step "ok: cross-column drag reorders live while the gesture is down"

drag_release
sleep 1
got_t="$(col_ids triage)"; got_d="$(col_ids todo)"
[ "$got_t" = "$A $C" ] || die "triage did not lose b (got [$got_t])"
[ "$got_d" = "$D $B" ] || die "todo did not gain b at the slot below d (got [$got_d])"
wait_dom_order todo "$D $B" 8
step "ok: cross-column drop rides one PATCH (status+after_id) and lands at the slot"

# ---- c: release a drag off the board - revert, no server change ------------
card_geo "$A"; cfx=$CX; cfy=$CY
card_geo "$B"; ctx=$CX; cty=$(( TOP + 6 ))   # between d and b in todo
drag_to "$cfx" "$cfy" "$ctx" "$cty"

# mid-drag: the optimistic move shows a in the todo column
wait_dom_order todo "$D $A $B" 6
assert_dragging "$A"
step "ok: optimistic move renders the card in the target column"

# then walk the pointer OFF the board (the band: no .board-col ancestor)
offx=$(jsq '(()=>{const c=document.querySelector(".board-switch-select .model-chip");if(!c) return 40;return Math.round(c.getBoundingClientRect().x+c.getBoundingClientRect().width/2)})()' 2>&1 | tail -1 | tr -d '"')
offy=$(jsq '(()=>{const c=document.querySelector(".board-switch-select .model-chip");if(!c) return 40;return Math.round(c.getBoundingClientRect().y+c.getBoundingClientRect().height/2)})()' 2>&1 | tail -1 | tr -d '"')
[ "$offy" != "-1" ] && [ -n "$offx" ] || { offx=40; offy=40; }
ab mouse move "$offx" "$offy" >/dev/null 2>&1
sleep 0.3
drag_release

# the optimistic move must snap back; the server never saw a PATCH
wait_dom_order triage "$A $C" 10
wait_dom_order todo "$D $B" 10
got_t="$(col_ids triage)"; got_d="$(col_ids todo)"
[ "$got_t" = "$A $C" ] || die "off-board release changed the server triage order (got [$got_t])"
[ "$got_d" = "$D $B" ] || die "off-board release changed the server todo order (got [$got_d])"
step "ok: off-board release reverts, server order unchanged"

# ---- d: the push shape (card t_b5784de3) -----------------------------------
# The 5s GET /tasks poll is GONE: the board renders from its mount fetch and
# re-reads its own REST view when the shell announces a canvas delta. The
# chain: a REST mutation pushes the module's tasks panel (rest.go
# pushAfterMutation) -> the canvas emit fans a ViewModelDelta onto the ws
# canvas topic (agent/api/server_canvas.go) -> the SPA's canvas store
# applies it and publishes window "omega-canvas-delta" (composables/
# canvas.ts) -> Board.vue listens and refetches. Prove it end to end: mark
# the page (a reload wipes the mark), edit through the API, watch the idle
# board absorb it.
js '(()=>{window.__j98NoReload=1;return 1})()' >/dev/null
curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${A}?board=${BOARD}" \
  -H 'Content-Type: application/json' \
  -d "{\"title\": \"${BTAG}-drag ${TAG} a-pushed\"}" | grep -q "\"id\":\"$A\"" || \
  die "PATCH title $A failed"
poll_js "$(printf '(()=>{return [...document.querySelectorAll(".card")].some(c=>c.dataset.id==="%s"&&(c.textContent||"").includes("a-pushed"))})()' "$A")" 10 || \
  die "an API edit never reached the idle board - the canvas delta event did not refresh it (card t_b5784de3)"
marker=$(jsq 'window.__j98NoReload===1?1:0' 2>&1 | tail -1)
[ "$marker" = "1" ] || die "the board picked the API edit up by RELOADING - the push shape is fake"
step "ok: an API edit lands on the idle board by push (no reload, no poll)"

# a create rides the same seam: the panel delta follows the POST
E=$(card_id review e)
[ -n "$E" ] || die "pushed-card seeding failed"
poll_js "$(printf '(()=>{const col=[...document.querySelectorAll(".board-col")].find(c=>c.dataset.status==="%s");if(!col) return false;return [...col.querySelectorAll(".card")].some(c=>c.dataset.id==="%s")})()' review "$E")" 10 || \
  die "an API-created card ($E) never reached the idle board - the push shape is dead for creates"
marker=$(jsq 'window.__j98NoReload===1?1:0' 2>&1 | tail -1)
[ "$marker" = "1" ] || die "a create only showed after a reload - the push shape is fake"
step "ok: an API create lands on the idle board by push (no reload, no poll)"

# ---- e: lane rails (card t_50a8c145) ---------------------------------------
# ANY lane folds to a thin rail (~36px): the lane's dot on the header row's
# line, the name in vertical-rl uppercase, the count when it holds cards. The
# rail is a BUTTON (click unfolds) and a DROP TARGET (the drag machinery
# resolves .board-col, and the rail renders inside it). An EMPTY lane folds
# itself, and its override dies with it. Rails are a horizontal-scroll
# affordance: open_app sets the 1280x800 desktop viewport, so the narrow
# stack (where rails do not exist at all) never applies here.
#
# Every drag below runs between ADJACENT lanes: scroll_start puts the source
# column at the strip's left edge, so its 236px column plus the target's
# 36px rail fit inside the pane at 1280.
#
# The probes read data-status with an UNQUOTED attribute value on purpose
# (all nine statuses are simple identifiers): a quoted selector has to
# survive two levels of shell quoting, and the CLI's unescape behaviour
# differs across agent-browser versions (worker-onboarding, t_2454597e).

# scroll_start <status>: the source lane at the strip's left edge.
scroll_start() {
  js "(function(){var c=document.querySelector('.board-col[data-status=$1]');if(c) c.scrollIntoView({block:'nearest',inline:'start'});return 1})()" >/dev/null
  sleep 0.3
}

# lane_folded_expr <status>: a BOOLEAN for wait_js (never a ?1:0 answer)
lane_folded_expr() {
  printf "(function(){var c=document.querySelector('.board-col[data-status=%s]');return !!c&&c.classList.contains('folded')})()" "$1"
}

# lane_state <status> -> "folded|count|rail|name|railcount" (dash = absent)
lane_state() {
  jsq "(function(){var c=document.querySelector('.board-col[data-status=$1]');if(!c) return 'missing';var r=c.querySelector('.lane-rail');return (c.classList.contains('folded')?'folded':'open')+'|'+c.dataset.count+'|'+(r?'rail':'no-rail')+'|'+(r?((r.querySelector('.lane-rail-name')||{}).textContent||'-'):'-')+'|'+(r?((r.querySelector('.lane-rail-count')||{}).textContent||'-'):'-')})()" 2>&1 | tail -1 | tr -d "\"'"
}

lane_width() {
  jsq "(function(){var c=document.querySelector('.board-col[data-status=$1]');return c?Math.round(c.getBoundingClientRect().width):-1})()" 2>&1 | tail -1 | tr -d "\"'"
}

# dot_row <status>: the lane dot's viewport centre y (folded OR expanded)
dot_row() {
  jsq "(function(){var c=document.querySelector('.board-col[data-status=$1]');var d=c&&c.querySelector('.lane-dot');if(!d) return -1;var b=d.getBoundingClientRect();return Math.round(b.y+b.height/2)})()" 2>&1 | tail -1 | tr -d "\"'"
}

fold_lane() {
  js "(function(){var b=document.querySelector('.board-col[data-status=$1] .col-fold-btn');if(!b) return 0;b.click();return 1})()" >/dev/null
}

unfold_lane() {
  js "(function(){var r=document.querySelector('.board-col[data-status=$1] .lane-rail');if(!r) return 0;r.click();return 1})()" >/dev/null
}

# rail_geo <status>: sets RX RY to the rail's centre, 40px below its top
rail_geo() {
  local v
  v=$(jsq "(function(){var c=document.querySelector('.board-col[data-status=$1]');var r=c&&c.querySelector('.lane-rail');if(!r) return '0,0';var b=r.getBoundingClientRect();return Math.round(b.x+b.width/2)+','+Math.round(b.y+40)})()" 2>&1 | tail -1 | tr -d "\"'")
  RX="${v%%,*}"; RY="${v#*,}"
  [ "$RX" != "0" ] || die "lane $1 renders no rail (geo: $v)"
}

# rail_box <status> -> "x|width" of the rail button
rail_box() {
  jsq "(function(){var c=document.querySelector('.board-col[data-status=$1]');var r=c&&c.querySelector('.lane-rail');if(!r) return '0|0';var b=r.getBoundingClientRect();return Math.round(b.x)+'|'+Math.round(b.width)})()" 2>&1 | tail -1 | tr -d "\"'"
}

# card_inview <id>: sets CX CY like card_geo, and DIES unless that centre
# is inside the board's scroll port. A card scrolled behind the sidebar is
# still in the DOM with a plausible rect, but a pointer press there lands on
# the pane painted over it - the drag then starts on the wrong element and
# the recipe measures a gesture nobody made.
card_inview() {
  card_geo "$1"
  local ok
  ok=$(jsq "(function(){var p=document.querySelector('.board-scroll').getBoundingClientRect();return $CX>Math.round(p.x)&&$CX<Math.round(p.x+p.width)&&$CY>Math.round(p.y)&&$CY<Math.round(p.y+p.height)})()" 2>&1 | tail -1)
  [ "$ok" = "true" ] || die "card $1 is not inside the board's scroll port at ($CX,$CY) - scroll the SOURCE lane first"
}

step "e1: folding a card-holding lane renders the rail (dot + vertical name + count)"
scroll_start todo
fold_lane todo
wait_js "$(lane_folded_expr todo)" 6
sleep 0.4
st="$(lane_state todo)"
[ "$st" = "folded|2|rail|todo|2" ] || die "todo rail shape wrong (got [$st] want [folded|2|rail|todo|2])"
w="$(lane_width todo)"
[ "$w" = "36" ] || die "a folded lane is not the 36px rail (got ${w}px)"
# the fold's ease: the ~180ms cubic-bezier transition rides the lane itself
dur="$(jsq "(function(){var s=getComputedStyle(document.querySelector('.board-col[data-status=todo]'));return s.transitionDuration+'|'+s.transitionTimingFunction})()" 2>&1 | tail -1 | tr -d "\"'")"
case "$dur" in
  0.18s*cubic-bezier*) ;;
  *) die "the fold does not ease over ~180ms on a cubic-bezier (got [$dur])" ;;
esac
step "ok: todo folded to a 36px rail, name+count rendered, width ease [$dur]"

# the dot row is ONE line across the board: a folded lane's dot sits exactly
# on an expanded lane's header dot
rail_dot="$(dot_row todo)"; head_dot="$(dot_row review)"
[ "$rail_dot" != "-1" ] && [ "$head_dot" != "-1" ] || die "a lane dot is missing (rail=$rail_dot head=$head_dot)"
[ "$rail_dot" = "$head_dot" ] || die "dots do not align across lanes: rail row $rail_dot vs header row $head_dot"
step "ok: rail dots align with expanded header dots (both at y=$rail_dot)"

step "e2: the rail is a live drop target, and a live drag never reshapes the strip"
# the SOURCE lane is scrolled to the strip's left edge (never the target: a
# card scrolled out of the pane is not draggable - its rect sits behind a
# sidebar and the gesture starts on whatever is painted there)
scroll_start triage
card_inview "$A"; afx=$CX; afy=$CY
rail_geo todo
box_before="$(rail_box todo)"
drag_to "$afx" "$afy" "$RX" "$RY"
box_mid="$(rail_box todo)"
[ "$box_mid" = "$box_before" ] || die "the strip reshaped under the cursor mid-drag (rail $box_before -> $box_mid) - the drop would land on a neighbour"
drag_release
sleep 1
[ "$(col_ids todo | awk '{print $1}')" = "$A" ] || die "a card dropped on the rail did not land at the top of todo (got [$(col_ids todo)])"
st="$(lane_state todo)"
[ "$st" = "folded|3|rail|todo|3" ] || die "a pinned rail must keep its shape when a card lands on it (got [$st] want [folded|3|rail|todo|3])"
step "ok: drop on the rail moved $A into todo; the rail kept its shape and its count grew"

unfold_lane todo
wait_js "!$(lane_folded_expr todo)" 6
sleep 0.4
w="$(lane_width todo)"
[ "$w" = "236" ] || die "clicking the rail did not unfold the lane (width ${w}px)"
st="$(lane_state todo)"
case "$st" in open\|3\|no-rail*) ;; *) die "unfolded lane shape wrong (got [$st])" ;; esac
step "ok: the rail is the unfold control (todo back to a 236px column)"

step "e3: an empty lane folds itself and its override dies with it"
S=$(card_id scheduled s)
[ -n "$S" ] || die "scheduled-card seeding failed"
trap 'archive_cards "$A" "$B" "$C" "$D" "$E" "$S"' EXIT
poll_js "(function(){var c=document.querySelector('.board-col[data-status=scheduled]');return !!c&&c.dataset.count==='1'})()" 10 ||
  die "seeded card $S never rendered in scheduled"
# pin it COLLAPSED while it holds one card, then drain it through the API: a
# folded lane's cards are not rendered in the DOM, so another surface takes
# the last one and the board absorbs the canvas delta
scroll_start scheduled
fold_lane scheduled
wait_js "$(lane_folded_expr scheduled)" 6
sleep 0.3
st="$(lane_state scheduled)"
[ "$st" = "folded|1|rail|scheduled|1" ] || die "scheduled rail shape wrong (got [$st])"
curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${S}?board=${BOARD}" \
  -H 'Content-Type: application/json' -d '{"status":"todo"}' | grep -q "\"id\":\"$S\"" ||
  die "PATCH $S out of scheduled failed"
poll_js "(function(){var c=document.querySelector('.board-col[data-status=scheduled]');return !!c&&c.dataset.count==='0'&&c.classList.contains('folded')})()" 10 ||
  die "a drained lane did not fold itself (count/folded: $(lane_state scheduled))"
st="$(lane_state scheduled)"
[ "$st" = "folded|0|rail|scheduled|-" ] || die "drained rail shape wrong (got [$st] want [folded|0|rail|scheduled|-])"
step "ok: the drained lane is a rail with no count, even though it was pinned collapsed"

step "e4: the dropped override does not come back when the lane refills"
scroll_start todo
card_inview "$D"; rfx=$CX; rfy=$CY
rail_geo scheduled
drag_to "$rfx" "$rfy" "$RX" "$RY"
drag_release
sleep 1
[ "$(col_ids scheduled)" = "$D" ] || die "the refill drop did not land in scheduled (got [$(col_ids scheduled)])"
st="$(lane_state scheduled)"
# a surviving 'collapsed' pin would keep this a rail; the override died with
# the empty lane, so the refilled lane takes its DEFAULT shape (expanded)
[ "$st" = "open|1|no-rail|-|-" ] || die "a refilled lane kept a dead override (got [$st] want [open|1|no-rail|-|-])"
step "ok: the refilled lane came back expanded - no override outlived its empty lane"

# the card seeded for this section is this recipe's to clean up (the trap
# above owns the others)
archive_cards "$S"
step "e: lane rails GREEN"

# ---- f: card polish + the running heartbeat chip (card t_65ea61a1) ---------
# The ruling: "the running card dont have a heartbeat". These are the EYE
# probes - the wire already carries status + started_at (and carries
# last_heartbeat_at when the store stamps it; omega's store does not yet,
# so the stale case is driven here by wrapping the page's fetch and
# letting the REAL canvas-delta refetch render it).
#
#   f1. a long title clamps at two lines and the meta row never spills
#       (the clamp constant is 2; a card that overflowed its column
#       grew a horizontal scrollbar before the layout fix);
#   f2. the assignee renders as a chip;
#   f3. a running card wears the "working - Xm" chip;
#   f4. an AGED last_heartbeat_at flips that chip to the stale face.

# card_state <id> -> "clamp|titleLines|clipped|assignee|heartbeat"
card_state() {
  jsq "(function(){var c=[...document.querySelectorAll('.card')].find(x=>x.dataset.id==='$1');if(!c) return 'missing';var t=c.querySelector('.card-title');var cs=t?getComputedStyle(t):null;var lh=cs?parseFloat(cs.lineHeight):0;var h=t?t.getBoundingClientRect().height:0;var a=c.querySelector('.card-assignee-chip');var hb=c.querySelector('.card-heartbeat');return (cs?cs.getPropertyValue('-webkit-line-clamp').trim():'-')+'|'+Math.round(h/lh)+'|'+(t&&t.scrollHeight>t.clientHeight+1?'clipped':'fits')+'|'+(a?'chip':'none')+'|'+(hb?hb.textContent.trim()+(hb.classList.contains('stale')?' STALE':''):'-')})()" 2>&1 | tail -1 | tr -d "\"'"
}

# col_hscroll <id>: 1 when the card's lane grew a horizontal scrollbar
# (an over-wide meta row spills the card - the pre-fix bug)
col_hscroll() {
  jsq "(function(){var c=[...document.querySelectorAll('.card')].find(x=>x.dataset.id==='$1');if(!c) return -1;var b=c.closest('.col-body');return b&&b.scrollWidth>b.clientWidth+1?1:0})()" 2>&1 | tail -1 | tr -d "\"'"
}

step "f1: a long title clamps at two lines, the meta row stays inside the card"
LONG="a card title long enough to run past two lines on a 218px card which is exactly the case the clamp exists for"
L=$(card_full triage "$LONG" "j98cody" "first body line is the dimmed teaser")
R=$(card_full todo "running card" "j98cody" "a worker is beating on this card")
[ -n "$L" ] && [ -n "$R" ] || die "card-polish seeding failed"
trap 'archive_cards "$A" "$B" "$C" "$D" "$E" "$S" "$L" "$R"' EXIT
# the running lane's start stamp is the store's job: this transition sets it
curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${R}?board=${BOARD}" \
  -H 'Content-Type: application/json' -d '{"status":"running"}' | grep -q "\"id\":\"$R\"" ||
  die "PATCH $R to running failed"

poll_js "(function(){return [...document.querySelectorAll('.card')].some(c=>c.dataset.id==='$L')})()" 10 ||
  die "the long-title card $L never rendered"
poll_js "(function(){return [...document.querySelectorAll('.card')].some(c=>c.dataset.id==='$R')})()" 10 ||
  die "the running card $R never rendered"

st="$(card_state "$L")"
[ "$st" = "2|2|clipped|chip|-" ] ||
  die "the long title does not clamp at two lines (got [$st] want [2|2|clipped|chip|-])"
[ "$(col_hscroll "$L")" = "0" ] ||
  die "the clamped card spilled its lane into a horizontal scrollbar (card-polish layout bug)"
step "ok: the long title clamps to 2 lines, no lane overflow"

step "f2/f3: the assignee is a chip, the running card wears its working chip"
case "$st" in *"|chip|-") ;; *) die "no assignee chip on the long-title card (got [$st])" ;; esac
stR="$(card_state "$R")"
case "$stR" in
  2*"|chip|working - "[0-9]*m) ;;
  *) die "the running card wears no working chip (got [$stR])" ;;
esac
step "ok: running card chip reads [$(echo "$stR" | awk -F'|' '{print $5}')]"

step "f4: an aged heartbeat flips the chip to the stale face"
# wrap the page's own fetch: every /tasks read stamps a running row with
# an AGED last_heartbeat_at (5 min old). The board's next REAL refetch -
# the canvas delta from a REST edit - renders it. The original is kept
# so the recipe hands the page back unwrapped (a later recipe in the
# same suite must not inherit the simulation).
js '(function(){var of=window.fetch;window.__j98Of=of;window.fetch=function(u,o){return of(u,o).then(function(r){var url=typeof u==="string"?u:((u&&u.url)||"");if(url.indexOf("/tasks")<0)return r;return r.clone().json().then(function(j){if(!j||!j.tasks)return r;var now=Date.now();j.tasks.forEach(function(t){if(t.status==="running"){t.last_heartbeat_at=new Date(now-300000).toISOString();t.started_at=new Date(now-900000).toISOString();}});return new Response(JSON.stringify(j),{status:r.status,headers:{"Content-Type":"application/json"}});}).catch(function(){return r;});});};return 1})()' >/dev/null
curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${R}?board=${BOARD}" \
  -H 'Content-Type: application/json' -d "{\"title\": \"${BTAG}-drag ${TAG} running card stale-sim\"}" |
  grep -q "\"id\":\"$R\"" || die "the stale-sim PATCH failed"
poll_js "(function(){var c=[...document.querySelectorAll('.card')].find(x=>x.dataset.id==='$R');var hb=c&&c.querySelector('.card-heartbeat');return !!hb&&hb.classList.contains('stale')})()" 10 ||
  die "an aged heartbeat did not flip the chip (state: $(card_state "$R"))"
stR="$(card_state "$R")"
case "$stR" in
  *"|stale - "[0-9]*m" quiet STALE") ;;
  *) die "the stale chip face is wrong (got [$stR])" ;;
esac
step "ok: the chip reads [$(echo "$stR" | awk -F'|' '{print $5}')] under an aged beat"
# hand the page back: the simulation must not outlive this recipe
js '(function(){if(window.__j98Of){window.fetch=window.__j98Of;delete window.__j98Of;}return 1})()' >/dev/null
step "f: card polish + running heartbeat chip GREEN"

assert_no_console_errors
step "98-kanban-drag-reorder GREEN"
