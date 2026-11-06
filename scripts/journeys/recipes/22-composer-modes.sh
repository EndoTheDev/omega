#!/bin/bash
# 22-composer-modes - the composer display modes (card t_5752861e, v4
# t_d14c2d14 - the three-mode ladder set is retired; exactly TWO modes,
# and the old 'wide' name belongs to the single-row bar now). The
# settings' "composer display mode" select flips the chat composer's
# footprint, live in the pane:
#
#   compact (default) -> mode-compact: the C5 'footer-send' card - TWO
#                        rows (plus-btn + input on top; chips left +
#                        send right on a hairline-divided bottom row),
#                        centered in the pane (auto margins, 720px cap)
#   full              -> mode-full: the W6 'segmented' one-row bar (the
#                        old 'wide' renamed) - input | chips | send
#                        hairline segments across the pane width with
#                        the card's 12px inset
#
# Migration: a stored localStorage value of 'wide' loads as 'full' and
# the stored key is rewritten to 'full'. Compact is restored at the end.
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

# --- 1. default = compact: the C5 two-row card, centered ---------------
mode_cls=$(panejs '[...p.querySelector(".composer").classList].filter(c=>c.startsWith("mode-")).join(",")')
assert_eq "$mode_cls" "mode-compact" "compact default carries the mode-compact class"
mcap=$(panejs 'getComputedStyle(p.querySelector(".composer")).maxWidth')
assert_eq "$mcap" "720px" "compact pins the centered card's 720px width cap"
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
mright=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginRight')
assert_eq "$mleft" "$mright" "compact centers in the pane (equal resolved side margins)"
assert_eq "1" "$(awk "BEGIN{print ($mleft > 0) ? 1 : 0}")" \
  "compact is really inset from the pane edges (auto margins resolved to ${mleft}px)"
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "column" "compact is the COLUMN card (two rows, C5) - not the one-line bar"
# the two rows: the toolbar (chips + send) sits BELOW the input row
itop=$(panejs 'p.querySelector(".composer .composer-row").offsetTop')
rtop=$(panejs 'p.querySelector(".composer .toolbar").offsetTop')
assert_eq "1" "$(awk "BEGIN{print ($rtop > $itop) ? 1 : 0}")" \
  "compact rows split: the bottom row (${rtop}px) sits below the input row (${itop}px)"
# the C5 hairline divides the rows
btw=$(panejs 'getComputedStyle(p.querySelector(".composer .toolbar")).borderTopWidth')
assert_eq "$btw" "1px" "the toolbar's top hairline divides compact's two rows"
# row 2 stance: chips left, send right (never a centered send)
gapr=$(panejs 'p.querySelector(".composer .send").getBoundingClientRect().right - p.querySelector(".composer").getBoundingClientRect().right')
assert_eq "1" "$(awk -v g="$gapr" 'BEGIN{print (g < 0 && g > -20) ? 1 : 0}')" \
  "the send anchors the bottom row's right edge (gap ${gapr}px)"
gapl=$(panejs '(p.querySelector(".composer .workspace-select")||p.querySelector(".composer .model-chip")).getBoundingClientRect().left - p.querySelector(".composer").getBoundingClientRect().left')
assert_eq "1" "$(awk -v g="$gapl" 'BEGIN{print (g > 0 && g < 30) ? 1 : 0}')" \
  "the chips anchor the bottom row's left edge (gap ${gapl}px)"
# no W6 divider in compact: the chips|send hairline is full-only
segdivs=$(panejs 'p.querySelectorAll(".composer .seg-div").length')
assert_eq "$segdivs" "0" "compact renders no seg-div divider"
ch=$(panejs 'p.querySelector(".composer").clientHeight')
assert_eq "1" "$(awk "BEGIN{print ($ch >= 56 && $ch < 130) ? 1 : 0}")" \
  "compact is TWO rows tall (${ch}px - the one-line bar would sit under 56px)"
step "ok: compact is the default shape (two rows, ${ch}px tall, centered, 720px cap)"

# --- 2. full: mode-full, the W6 one-row segmented bar ------------------
pick_mode full
scoped_wait 'p.querySelector(".composer").classList.contains("mode-full")' 10
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "row" "full is ONE row (the segmented bar)"
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "12px" "full keeps the card's 12px inset (padded inside the pane)"
mcap=$(panejs 'getComputedStyle(p.querySelector(".composer")).maxWidth')
assert_eq "$mcap" "none" "full lifts the width cap (spans the pane width)"
radius=$(panejs 'getComputedStyle(p.querySelector(".composer")).borderRadius')
assert_eq "1" "$(awk -v r="$radius" 'BEGIN{print (r != "0px") ? 1 : 0}')" \
  "full keeps the card radius (a padded card, never edge to edge)"
# the W6 hairline segments: input | chips (toolbar border-left) and
# chips | send (the real seg-div element)
blw=$(panejs 'getComputedStyle(p.querySelector(".composer .toolbar")).borderLeftWidth')
assert_eq "$blw" "1px" "the toolbar's left hairline divides input from chips"
segdivs=$(panejs 'p.querySelectorAll(".composer .seg-div").length')
assert_eq "$segdivs" "1" "full renders the seg-div hairline (chips|send; the input|chips one is the toolbar's border-left)"
# one row really: composer-row and toolbar share the row's top
itop=$(panejs 'p.querySelector(".composer .composer-row").offsetTop')
rtop=$(panejs 'p.querySelector(".composer .toolbar").offsetTop')
assert_eq "1" "$(awk -v i="$itop" -v j="$rtop" 'BEGIN{d=(i>j)?i-j:j-i; print (d < 8) ? 1 : 0}')" \
  "full's segments share one row's top (input ${itop}px vs toolbar ${rtop}px)"
# the input segment grows into ALL the leftover width (flex-grow): it
# ends where the hairline starts, the send anchors the row's right end
rw=$(panejs 'p.querySelector(".composer .composer-row").getBoundingClientRect().right')
tl=$(panejs 'p.querySelector(".composer .toolbar").getBoundingClientRect().left')
assert_eq "1" "$(awk -v i="$rw" -v j="$tl" 'BEGIN{d=(i>j)?i-j:j-i; print (d < 3) ? 1 : 0}')" \
  "the input segment ends at the input|chips hairline (gap ${rw}px vs ${tl}px)"
tr=$(panejs 'p.querySelector(".composer .toolbar").getBoundingClientRect().right')
cr=$(panejs 'p.querySelector(".composer").getBoundingClientRect().right')
assert_eq "1" "$(awk -v i="$tr" -v j="$cr" 'BEGIN{print ((j-i) >= -1 && (j-i) < 6) ? 1 : 0}')" \
  "the send anchors the row's right end (toolbar right ${tr}px vs composer ${cr}px)"
ch=$(panejs 'p.querySelector(".composer").clientHeight')
assert_eq "1" "$(awk "BEGIN{print ($ch > 0 && $ch < 56) ? 1 : 0}")" \
  "full is one row tall (${ch}px - anything wrapped would exceed 56px)"
step "ok: full applies (one row, hairline segments, padded inside the pane)"

# --- 3. migration: a stored 'wide' loads as 'full' ----------------------
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "full" "the full pick persists (omega.composer-mode)"
jsq 'localStorage.setItem("omega.composer-mode", "wide")' >/dev/null 2>&1 || true
open_app
open_journey_row "(journey) composer-modes $TAG"
scoped_wait 'p.querySelector(".composer")?.classList.contains("mode-full")' 15
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "full" "the retired 'wide' value migrated: stored and applied as 'full'"
step "ok: stored wide loads as full with the key rewritten"

# --- 4. restore compact (end state = the virgin default) ----------------
if ! poll_js '(()=>{return !!document.querySelector(".setting-select .model-chip[aria-label=\"composer display mode\"]")})()' 10; then
  die "the composer display mode knob never re-rendered for the restore"
fi
pick_mode compact
scoped_wait 'p.querySelector(".composer").classList.contains("mode-compact")' 15
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "column" "compact restores the two-row column card"
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "compact" "compact restored in localStorage too"

assert_no_console_errors

cleanup_journey_sessions
step "GREEN: the composer mode law holds (compact C5 two-row card, full W6 segmented bar, wide->full migration, persistence)"
