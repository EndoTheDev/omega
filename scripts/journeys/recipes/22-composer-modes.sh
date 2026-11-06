#!/bin/bash
# 22-composer-modes - the composer display modes (card t_5752861e): the
# settings' "composer display mode" select flips the chat composer's
# footprint, live in the pane:
#
#   compact (default) -> the card shape (12px side inset, no mode class)
#   wide              -> mode-wide, the side inset tightens to 4px
#   full              -> mode-full, edge to edge (0 inset, no radius)
#
# The pick is a browser preference: localStorage omega.composer-mode
# survives a reload; the recipe restores compact at the end.
#
# No model needed: the mode law is pure client DOM + CSS.
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session composer-modes | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) composer-modes $TAG"

# panejs <expr over p>: evaluate expr against the FOCUSED chat pane
# scoped to this run's label (recipe 30's ghost-tab law).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) composer-modes')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}

# scoped_wait <expr> <timeout_s>: poll the focused pane; timeout dies.
scoped_wait() {
  local expr="$1" t="$2" i=0 out
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(panejs "$expr")
    if [ "$out" = "1" ] || [ "$out" = "true" ]; then return 0; fi
    sleep 0.5
    i=$((i + 1))
  done
  die "scoped_wait timed out after ${t}s: $expr (last: $out)"
}

# pick_mode <mode>: open the settings' composer display mode chip and
# pick <mode> from the themed listbox (recipe 60's grammar).
pick_mode() {
  local hdr
  hdr=$(jsq '(()=>{const c=document.querySelector(".setting-select .model-chip[aria-label=\"composer display mode\"]"); if(!c) return 0; c.click(); return 1})()' 2>&1 | tail -1)
  assert_eq "$hdr" "1" "composer display mode chip clicked (pop opens)"
  if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()==="'$1'")})()' 10; then
    die "composer display mode options never rendered (want $1)"
  fi
  local pick
  pick=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="'$1'"); if(!o) return 0; o.click(); return 1})()' 2>&1 | tail -1)
  assert_eq "$pick" "1" "composer display mode pick to $1"
  # close the popped listbox so the next pick starts clean
  jsq 'document.body.click()' >/dev/null 2>&1 || true
}

open_app
open_journey_row "(journey) composer-modes $TAG"

# the composer must be reachable in THIS pane before any class assert
scoped_wait '!!p.querySelector(".composer")' 15
step "ok: composer reachable in the journey pane"

# the settings surface: the pinned inspector dock-tab chip (recipe 60's
# law - icon-only edge chips are found by aria-label, and a fresh chrome
# profile boots with the inspector edge closed: probe, toggle, poll).
CHIP_POLL='(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Settings");if(!t) return false;t.click();return true})()'
if ! poll_js "$CHIP_POLL" 4; then
  jsq '(()=>{const b=document.querySelector("button[aria-label=\"Toggle right sidebar\"]"); if(!b) return false; b.click(); return true})()' >/dev/null 2>&1 || true
  if ! poll_js "$CHIP_POLL" 10; then
    die "Settings dock-tab chip never rendered"
  fi
fi
if ! poll_js '(()=>{return !!document.querySelector(".setting-select .model-chip[aria-label=\"composer display mode\"]")})()' 10; then
  die "settings never rendered the composer display mode knob"
fi
step "ok: the composer display mode knob renders"

# --- 1. default = compact: no mode class, the card 12px inset ----------
mode_cls=$(panejs '[...p.querySelector(".composer").classList].filter(c=>c.startsWith("mode-")).join(",")')
assert_eq "$mode_cls" "" "compact default carries no mode class"
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "12px" "compact default keeps the card side inset"
step "ok: compact is the default shape (12px inset)"

# --- 2. wide: mode-wide, inset tightens to 4px --------------------------
pick_mode wide
scoped_wait 'p.querySelector(".composer").classList.contains("mode-wide")' 10
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "4px" "wide tightens the side inset"
step "ok: wide applies (inset 12px -> 4px)"

# --- 3. full: mode-full, edge to edge (0 inset, no radius) --------------
pick_mode full
scoped_wait 'p.querySelector(".composer").classList.contains("mode-full")' 10
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "0px" "full drops the side inset"
radius=$(panejs 'getComputedStyle(p.querySelector(".composer")).borderRadius')
assert_eq "$radius" "0px" "full drops the corner radius"
step "ok: full applies (edge to edge)"

# --- 4. the pick is a browser preference: it survives a reload ----------
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "full" "the mode pick persists (omega.composer-mode)"
open_app
open_journey_row "(journey) composer-modes $TAG"
scoped_wait 'p.querySelector(".composer")?.classList.contains("mode-full")' 15
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "0px" "the persisted mode re-applies after reload"
step "ok: full survives a reload"

# --- 5. restore compact (end state = the virgin default) ----------------
if ! poll_js '(()=>{return !!document.querySelector(".setting-select .model-chip[aria-label=\"composer display mode\"]")})()' 10; then
  die "the composer display mode knob never re-rendered for the restore"
fi
pick_mode compact
scoped_wait '![...p.querySelector(".composer").classList].some(c=>c.startsWith("mode-"))' 15
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "12px" "compact restores the card inset"
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "compact" "compact restored in localStorage too"

assert_no_console_errors

cleanup_journey_sessions
step "GREEN: the composer mode law holds (compact default, wide, full, persistence)"
