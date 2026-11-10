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

assert_no_console_errors
step "98-kanban-drag-reorder GREEN"
