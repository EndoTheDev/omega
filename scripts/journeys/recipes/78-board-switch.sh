#!/bin/bash
# 78-board-switch - multi-board kanban journey (t_d7a7beaa): the
# switcher in the board view reads GET /boards (the registry), then a
# board switch re-renders the columns from that board only - a card on
# board A never bleeds into board B. The inspector follows the switch.
#
# Registry boards are CLI-made on purpose (the HTTP surface has no
# board-create endpoint: the registry is a filesystem+pointer, not card
# data). Cards ride ?board= on POST /tasks. The two j78 boards stay on
# the lane with archived cards each - the isolation probe the card's
# VERIFY section asks for, visible on every later run.
. "${JROOT:?run must export JROOT}/lib.sh"

BTAG="j78$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"
OMEGA_BIN="${OMEGA_BIN:-$JROOT/../../bin/omega}"
BOARD_A="$BTAG-a"
BOARD_B="$BTAG-b"
A_IDS=""
B_ID=""

# archive_board_cards <board> <ids...>: the throwaway cleanup - the
# store has no delete, so journey cards arch out quietly.
archive_board_cards() {
  local board="$1"; shift
  for id in "$@"; do
    curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${id}?board=${board}" \
      -H 'Content-Type: application/json' -d '{"status":"archived"}' >/dev/null || true
  done
}

step "creating registry boards: $BOARD_A / $BOARD_B"
"$OMEGA_BIN" kanban board create "$BOARD_A" >/dev/null || die "board create a"
"$OMEGA_BIN" kanban board create "$BOARD_B" >/dev/null || die "board create b"

# seed: two cards on a, one on b (via the ?board= picker)
card_id() {
  curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=$1" \
    -H 'Content-Type: application/json' -d "{\"title\": \"card on $1\"}" |
    grep -oE 't_[0-9a-f]+' | head -1
}
A_IDS="$(card_id "$BOARD_A") $(card_id "$BOARD_A")"
B_ID="$(card_id "$BOARD_B")"
trap 'archive_board_cards "$BOARD_A" $A_IDS; archive_board_cards "$BOARD_B" "$B_ID"' EXIT
[ -n "$A_IDS" ] && [ -n "$B_ID" ] || die "card seeding failed"

# ---- boot ----------------------------------------------------------------
open_app
ab open "${JOURNEY_BASE_URL}/board" >/dev/null 2>&1 || die "cannot open /board"
ab wait --load networkidle >/dev/null 2>&1 || true

# the switcher must list the registry boards (card t_acad02ec: the
# native select is gone - the switcher rides the BandSelect listbox;
# click the chip, read the pop's options)
if ! wait_js "(()=>{return !!document.querySelector('.board-switch-select .model-chip')})()" 15; then
  die "board switcher missing (no BandSelect chip)"
fi
js "(()=>{document.querySelector('.board-switch-select .model-chip').click();return 1})()" >/dev/null
if ! wait_js "(()=>{return [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD_A')})()" 15; then
  die "board switcher registry not listed ($BOARD_A absent)"
fi
step "ok: switcher lists the registry"

# ---- switch ---------------------------------------------------------------
# default boot = pointer board; switch to a and count its two cards
# (the pop may be open from the registry check - clicking the chip
# toggles; open only when closed, then pick the option)
js "(()=>{const c=document.querySelector('.board-switch-select .model-chip'); if(c.getAttribute('aria-expanded')!=='true') c.click(); return 1})()" >/dev/null
if ! wait_js "(()=>{return [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD_A')})()" 15; then
  die "switch options never rendered ($BOARD_A)"
fi
js "(()=>{const o=[...document.querySelectorAll('.board-switch-select .model-list li')].find(l=>(l.textContent||'').trim()==='$BOARD_A'); if(!o) return 0; o.click(); return 1})()" >/dev/null

if ! wait_js "(()=>{const n=Array.from(document.querySelectorAll('.board .card-title')).filter(c=>c.textContent.includes('card on $BOARD_A')).length;return n===2})()" 15; then
  die "board a did not render its two cards after the switch"
fi
if jsq "(()=>{return document.body.textContent.includes('card on $BOARD_B')})()" 2>&1 | tail -1 | grep -q true; then
  die "board b's card bled into board a's view"
fi
step "ok: switch renders only board a's cards, no bleed"

# ---- inspector follows -----------------------------------------------------
ab find text "card on $BOARD_A" click >/dev/null 2>&1 || die "could not open a card"
if ! wait_js "(()=>{const d=document.querySelector('.drawer');return !!d && d.textContent.includes('card on $BOARD_A')})()" 15; then
  die "inspector did not follow the board switch"
fi
step "ok: inspector follows the switch"

assert_no_console_errors
step "78-board-switch GREEN"