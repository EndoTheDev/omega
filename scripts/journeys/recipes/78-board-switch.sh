#!/bin/bash
# 78-board-switch - multi-board kanban journey (t_d7a7beaa): the
# switcher in the board view reads GET /boards (the registry), then a
# board switch re-renders the columns from that board only - a card on
# board A never bleeds into board B. The inspector follows the switch.
#
# One kanban.db owns all boards (t_feae19d3): the registry is the
# boards TABLE - the board rows answer GET /boards, cards ride ?board=
# on POST /tasks. The boards are PINNED (fixed names j78-a/j78-b,
# reused across runs - a second run must create zero new boards) and
# the throwaway cards are DELETED at exit (see the reap law below); the
# isolation probe stays visible on every later run.
. "${JROOT:?run must export JROOT}/lib.sh"

BTAG="j78"
OMEGA_BIN="${OMEGA_BIN:-$JROOT/../../bin/omega}"
BOARD_A="$BTAG-a"
BOARD_B="$BTAG-b"
A_IDS=""
B_ID=""

# ensure_board <name>: the pinned board exists on every run after the
# first - create only when genuinely missing (create on an existing
# board is a refusal; a listed board proves the reuse).
ensure_board() {
  if "$OMEGA_BIN" kanban board create "$1" >/dev/null 2>&1; then
    return 0
  fi
  if "$OMEGA_BIN" kanban board list 2>/dev/null | sed 's/^[* ] \+//' | awk '{print $1}' | grep -qx "$1"; then
    return 0
  fi
  die "board ensure $1"
}

step "pinning registry boards: $BOARD_A / $BOARD_B"
ensure_board "$BOARD_A" || die "board ensure a"
ensure_board "$BOARD_B" || die "board ensure b"

# board_card_ids <board>: the ids of every row this recipe family plants
# (title prefix "card on "), archived rows included - the default list hides them.
board_card_ids() {
  curl -s "${JOURNEY_BASE_URL}/tasks?board=$1&status=all" | python3 -c '
import json, sys
sys.stdout.reconfigure(newline="")
try:
    rows = json.load(sys.stdin).get("tasks") or []
except Exception:
    sys.exit(0)
for t in rows:
    if str(t.get("title") or "").startswith("card on "):
        print(str(t.get("id") or ""))' 2>/dev/null || true
}

# count_cards <selector>: how many rendered cards in that scope carry this
# recipe's title - the red below names which lane they came from.
count_cards() {
  js "(()=>{return Array.from(document.querySelectorAll('$1 .card-title')).filter(c=>c.textContent.includes('card on $BOARD_A')).length})()" 2>&1 | tail -1 | tr -d '"'
}

# delete_board_cards <board>: DELETE every id on stdin (one per line). The
# DELETE route is the reap; the archived lane is NOT one - the grid reads
# ?status=all (Board.vue fetchBoard -> tasksPath) and renders archived cards as
# ordinary `.card-title` rows, so a tombstone on a PINNED board accumulates into
# the exact count asserted below (run N reads n=2N) and only a virgin db can
# ever satisfy it. lib.sh's residue contract states the same law.
delete_board_cards() {
  local board="$1" id
  while IFS= read -r id; do
    [ -n "$id" ] || continue
    curl -sf -X DELETE "${JOURNEY_BASE_URL}/tasks/${id}?board=${board}" >/dev/null 2>&1 || true
  done
}

# clear_board <board>: reap every card this recipe family planted, then PROVE
# the board is empty. A clear that quietly missed is exactly the red the count
# assertion below cannot explain, so it dies here with the leftover ids instead.
clear_board() {
  local board="$1" left
  board_card_ids "$board" | delete_board_cards "$board"
  left="$(board_card_ids "$board")"
  [ -z "$left" ] || die "board $board still carries a leftover card after the reap (DELETE refused): $left"
}

# reap_boards: the EXIT path (a trap must never die - die runs the cleanup that
# fires this trap).
reap_boards() {
  local b
  for b in "$BOARD_A" "$BOARD_B"; do
    board_card_ids "$b" | delete_board_cards "$b"
  done
  return 0
}

for _b in "$BOARD_A" "$BOARD_B"; do
  clear_board "$_b"
done

# seed: two cards on a, one on b (via the ?board= picker)
card_id() {
  curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=$1" \
    -H 'Content-Type: application/json' -d "{\"title\": \"card on $1\"}" |
    grep -oE 't_[0-9a-f]+' | head -1
}
A_IDS="$(card_id "$BOARD_A") $(card_id "$BOARD_A")"
B_ID="$(card_id "$BOARD_B")"
trap 'reap_boards; journey_exit_cleanup' EXIT
[ -n "$A_IDS" ] && [ -n "$B_ID" ] || die "card seeding failed"

# ---- boot ----------------------------------------------------------------
open_app
ab open "${JOURNEY_BASE_URL}/board" >/dev/null 2>&1 || die "cannot open /board"
ab wait --load networkidle >/dev/null 2>&1 || true

# the switcher must list the registry boards (card t_acad02ec: the
# native select is gone - the switcher rides the BandSelect listbox;
# click the chip, read the pop's options). poll_js, not wait_js: wait_js dies
# inside itself, so a `if ! wait_js` branch is unreachable and the red would
# only ever carry the raw timeout.
if ! poll_js "(()=>{return !!document.querySelector('.board-switch-select .model-chip')})()" 15; then
  die "board switcher missing (no BandSelect chip; GET /boards: $(curl -s "${JOURNEY_BASE_URL}/boards" | head -c 200))"
fi
js "(()=>{document.querySelector('.board-switch-select .model-chip').click();return 1})()" >/dev/null
if ! poll_js "(()=>{return [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD_A')})()" 15; then
  die "board switcher registry not listed ($BOARD_A absent; GET /boards: $(curl -s "${JOURNEY_BASE_URL}/boards" | head -c 300))"
fi
step "ok: switcher lists the registry"

# ---- switch ---------------------------------------------------------------
# default boot = the current (is_current) board; switch to a and count
# its two cards (the pop may be open from the registry check - clicking
# the chip toggles; open only when closed, then pick the option)
js "(()=>{const c=document.querySelector('.board-switch-select .model-chip'); if(c.getAttribute('aria-expanded')!=='true') c.click(); return 1})()" >/dev/null
if ! poll_js "(()=>{return [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD_A')})()" 15; then
  die "switch options never rendered ($BOARD_A)"
fi
js "(()=>{const o=[...document.querySelectorAll('.board-switch-select .model-list li')].find(l=>(l.textContent||'').trim()==='$BOARD_A'); if(!o) return 0; o.click(); return 1})()" >/dev/null

if ! poll_js "(()=>{const n=Array.from(document.querySelectorAll('.board .card-title')).filter(c=>c.textContent.includes('card on $BOARD_A')).length;return n===2})()" 15; then
  die "board a did not render its two cards after the switch (n=$(count_cards '.board'), of which archived=$(count_cards '.board-col[data-status=archived]'))"
fi
if jsq "(()=>{return document.body.textContent.includes('card on $BOARD_B')})()" 2>&1 | tail -1 | grep -q true; then
  die "board b's card bled into board a's view"
fi
step "ok: switch renders only board a's cards, no bleed"

# ---- inspector follows -----------------------------------------------------
ab find text "card on $BOARD_A" click >/dev/null 2>&1 || die "could not open a card"
if ! poll_js "(()=>{const d=document.querySelector('.drawer');return !!d && d.textContent.includes('card on $BOARD_A')})()" 15; then
  die "inspector did not follow the board switch"
fi
step "ok: inspector follows the switch"

assert_no_console_errors
step "78-board-switch GREEN"