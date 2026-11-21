#!/bin/bash
# 118-board-card-context-menu - the right-click arc's step 3: kanban board cards.
#
# A board card's right-click opens the ONE shared menu (contextMenu.ts behind the single
# ui/ContextMenu.vue surface - recipe 116 proved it on dock tabs) with exactly the verbs
# the tree already carries:
#
#   Open     the card's own click (the detail drawer)
#   Copy id  client-side, through the one clipboard path - the notice follows the copy's
#            own answer, so a refused copy never reads as "copied"
#   Archive  the lane arrow's own PATCH: the card's status moves to the archived lane. A
#            card ALREADY archived carries no Archive row - the verb cannot act.
#   Delete   the drawer's own DELETE /tasks/{id}, armed: the first pick warns, the second
#            (the row reading "Confirm delete") fires. Recipe 119 proves the two picks.
#
# The right-click is a MENU, not a click: the drawer must stay shut until the Open row
# fires it. Escape closes, and the rows are placed inside the viewport.
#
# The row list itself is pinned as a pure builder in webui/test/boardcardmenu.test.ts.
# This recipe is the browser half - a real right-button press through the input pipeline.
#
# The canvas half of this step has NO surface to wire in 0.17.4 (the generic panel
# renderer left with the canvas tab fold, and the panel action vocabulary holds no node
# rename or delete), so this recipe covers the board only; the absence is recorded on the
# card.
#
# Board is PINNED (j118-menu, reused across runs like recipe 98's j98-drag); this run's
# cards carry the run tag in the title and arch out at exit, so the seeded columns stay
# dense from zero.
. "${JROOT:?run must export JROOT}/lib.sh"

BTAG="j118"
BOARD="j118-menu"
TAG="$(date +%s%N | cut -c1-13)"
A=""
B=""

archive_cards() {
  local id
  for id in "$@"; do
    [ -n "$id" ] || continue
    curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${id}?board=${BOARD}" \
      -H 'Content-Type: application/json' -d '{"status":"archived"}' >/dev/null || true
  done
}
trap 'archive_cards "$A" "$B"' EXIT

# ensure_board: the pinned board exists on every run after the first. The create route
# answers 201 on a fresh lane and 400 on an existing name, so both are success - the
# registry read is the authority.
ensure_board() {
  curl -s -X POST "${JOURNEY_BASE_URL}/boards" \
    -H 'Content-Type: application/json' -d "{\"name\":\"$BOARD\"}" >/dev/null 2>&1 || true
  curl -s "${JOURNEY_BASE_URL}/boards" | tr -d '\r' | grep -q "\"name\":\"$BOARD\"" ||
    die "the pinned board $BOARD is neither created nor listed"
}

# col_ids <status>: the server's order for that column
col_ids() {
  curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}&status=$1" \
    | tr -d '\r' | grep -oE '"id":"t_[0-9a-f]+"' | grep -oE 't_[0-9a-f]+' \
    | tr '\n' ' ' | sed 's/ *$//'
}

# card_id <status> <title-suffix>: seed one task, print its id
card_id() {
  curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" \
    -H 'Content-Type: application/json' \
    -d "{\"title\": \"${BTAG} ${TAG} $2\", \"status\": \"$1\"}" \
    | grep -oE 't_[0-9a-f]+' | head -1
}

# card_rect_js <id>: the card's centre, or '' while it is not rendered
card_rect_js() {
  printf '(function(){var c=[...document.querySelectorAll(".card")].find(function(x){return x.dataset.id==="%s"});if(!c) return "";var r=c.getBoundingClientRect();return Math.round(r.x+r.width/2)+" "+Math.round(r.y+r.height/2)})()' "$1"
}

# scroll_lane <status>: bring a lane to the strip's left edge (the archived lane lives
# nine columns out at 1280). The card measure waits for the lane to settle below.
scroll_lane() {
  js "(function(){var c=document.querySelector('.board-col[data-status=$1]');if(c) c.scrollIntoView({block:'nearest',inline:'start'});return 1})()" >/dev/null
}

# wait_card_settled <id>: the card's own top has stopped moving (a lane unfolding from its
# 36px rail eases over ~180ms; a card measured mid-ease moves out from under the press)
wait_card_settled() {
  wait_settled "(function(){var c=[...document.querySelectorAll('.card')].find(function(x){return x.dataset.id==='$1'});return c?Math.round(c.getBoundingClientRect().y):-1})()" 8
}

menu_rows() {
  local r
  r=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-label")].map(e=>e.textContent.trim()).join("|")')
  printf '%s' "${r//\"/}"
}

# right_click_card <id>: a REAL right-button press on the card's centre
right_click_card() {
  local box
  box=$(jsq "$(card_rect_js "$1")")
  box=${box//\"/}
  [ -n "$box" ] || die "card $1 is not rendered"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real pointer
               # pipeline coalesces a move and a press that land in one task
  ab mouse down right >/dev/null 2>&1
  ab mouse up right >/dev/null 2>&1
}

click_row() {
  local label=$1 box
  box=$(jsq "(() => {const b=[...document.querySelectorAll('.context-menu-item')].find(x=>((x.querySelector('.context-menu-label')||{}).textContent||'').trim()==='$label'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()")
  box=${box//\"/}
  [ -n "$box" ] || die "no menu row named $label"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: move and press pacing
  ab mouse down >/dev/null 2>&1
  ab mouse up >/dev/null 2>&1
}

open_card_menu() {
  right_click_card "$1"
  wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
    die "card $1: the right-click opened no menu"
}

# switch_to_board: the switcher rides the BandSelect listbox (recipe 98's shape). The
# chip mounts with the /boards read; a miss reloads once - a fresh mount re-fetches.
switch_to_board() {
  if ! poll_js "(() => !!document.querySelector('.board-switch-select .model-chip'))()" 15; then
    ab reload >/dev/null 2>&1 || true
    ab wait --load networkidle >/dev/null 2>&1 || true
    poll_js "(() => !!document.querySelector('.board-switch-select .model-chip'))()" 10 ||
      die "the board switcher chip never rendered (GET /boards: $(curl -s "${JOURNEY_BASE_URL}/boards" | head -c 200))"
  fi
  js '(() => {const c=document.querySelector(".board-switch-select .model-chip");if(c.getAttribute("aria-expanded")!=="true")c.click();return 1})()' >/dev/null
  wait_js "(() => [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD'))()" 15
  js "(() => {const o=[...document.querySelectorAll('.board-switch-select .model-list li')].find(l=>(l.textContent||'').trim()==='$BOARD');if(!o) return 0;o.click();return 1})()" >/dev/null
  wait_js "(() => document.body.textContent.includes('$TAG'))()" 15 ||
    die "board $BOARD never rendered this run's cards (tag $TAG)"
}

step "pinning board: $BOARD"
ensure_board
archive_cards $(col_ids triage) $(col_ids todo)   # a crashed run's leftovers, dense from zero
B=$(card_id archived b)
A=$(card_id triage a)
[ -n "$A" ] && [ -n "$B" ] || die "card seeding failed"

open_app
ab open "${JOURNEY_BASE_URL}/board" >/dev/null 2>&1 || die "cannot open /board"
ab wait --load networkidle >/dev/null 2>&1 || true
switch_to_board
step "ok: seeded $A (triage) and $B (archived) on $BOARD"

# --- 1. the live card's rows --------------------------------------------
wait_card_settled "$A"
open_card_menu "$A"
rows=$(menu_rows)
assert_eq "$rows" "Open|Copy id|Archive|Delete" "a live card menus Open, Copy id, Archive, Delete"

role=$(jsq '(() => {const m=document.querySelector(".context-menu"); return m?m.getAttribute("role")+"/"+m.getAttribute("aria-label"):""})()')
assert_eq "${role//\"/}" "menu/context menu" "the surface is the shared role=menu dialog"

geom=$(jsq '(() => {const r=document.querySelector(".context-menu").getBoundingClientRect(); return [Math.round(r.left),Math.round(r.top),Math.round(r.right),Math.round(r.bottom),innerWidth,innerHeight].join(",");})()')
geom=${geom//\"/}
IFS=, read -r left top right bottom vw vh <<<"$geom"
[ "$left" -ge 0 ] && [ "$top" -ge 0 ] && [ "$right" -le "$vw" ] && [ "$bottom" -le "$vh" ] ||
  die "the menu left the viewport: $geom"
step "ok: the card's right-click opens the shared menu inside the viewport ($geom)"

drawer=$(jsq '(() => document.querySelector(".drawer")?1:0)()')
assert_eq "${drawer//\"/}" "0" "a right-click is a menu, not the card's click (no drawer)"

ab press Escape >/dev/null 2>&1
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "Escape did not close the card menu"
fi
step "ok: Escape closes the card menu"

# --- 2. Copy id: the notice follows the copy's own answer ----------------
wait_card_settled "$A"
open_card_menu "$A"
click_row "Copy id"
if ! wait_js "(() => {const n=document.querySelector('.board-notice'); return !!n && (n.textContent||'').trim()==='card $A copied' && !n.classList.contains('err')})()" 10; then
  die "Copy id did not report a real copy (notice: $(jsq '(() => {const n=document.querySelector(".board-notice"); return n?n.textContent.trim():"none"})()'))"
fi
step "ok: Copy id copies the card's id and says so"

# --- 3. Open: the card's own click, from the row -------------------------
wait_card_settled "$A"
open_card_menu "$A"
click_row "Open"
if ! wait_js "(() => {const t=document.querySelector('.drawer .drawer-title'); return !!t && (t.textContent||'').includes('$TAG')})()" 10; then
  die "the Open row did not open the card's drawer"
fi
js '(() => {const b=document.querySelector(".drawer .drawer-close");if(b){b.click();return 1}return 0})()' >/dev/null
wait_js '(() => !document.querySelector(".drawer"))()' 10 || die "the drawer did not close"
step "ok: Open opens that card's drawer"

# --- 4. an already-archived card offers no Archive -----------------------
scroll_lane archived
wait_card_settled "$B"
open_card_menu "$B"
rows=$(menu_rows)
assert_eq "$rows" "Open|Copy id|Delete" "an archived card offers no Archive row (the verb cannot act)"
ab press Escape >/dev/null 2>&1
wait_js '(() => !document.querySelector(".context-menu"))()' 10 || die "Escape did not close the archived card's menu"
step "ok: an archived card's menu holds only the verbs that can act"

# --- 5. Archive: the lane arrow's PATCH, from the row --------------------
scroll_lane triage
wait_card_settled "$A"
open_card_menu "$A"
click_row "Archive"
if ! wait_js "(() => {const c=document.querySelector('.board-col[data-status=triage]'); return !!c && ![...c.querySelectorAll('.card')].some(x=>x.dataset.id==='$A')})()" 10; then
  die "the Archive row left card $A in the triage lane"
fi
poll_js "(() => {const c=document.querySelector('.board-col[data-status=archived]'); return !!c && [...c.querySelectorAll('.card')].some(x=>x.dataset.id==='$A')})()" 10 ||
  die "card $A never rendered in the archived lane"
i=0
while [ "$i" -lt 20 ]; do
  case " $(col_ids archived) " in *" $A "*) break ;; esac
  sleep 0.5
  i=$((i + 1))
done
case " $(col_ids archived) " in
  *" $A "*) ;;
  *) die "the server never moved $A to archived (archived: [$(col_ids archived)])" ;;
esac
step "ok: Archive moved the card to the archived lane (server agrees)"

# --- 6. the archived card's own menu now offers no Archive either -------
scroll_lane archived
wait_card_settled "$A"
open_card_menu "$A"
rows=$(menu_rows)
assert_eq "$rows" "Open|Copy id|Delete" "the just-archived card dropped its Archive row"
ab press Escape >/dev/null 2>&1
step "ok: the menu follows the card's state, not a cached row list"

assert_no_console_errors
step "118-board-card-context-menu GREEN"
