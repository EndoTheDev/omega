#!/bin/bash
# 22-composer-modes - the composer display modes (card t_5752861e, reshape
# t_f9fd3820 - the width ladder t_4a6431f8 was the wrong shape and is
# superseded). The settings' "composer display mode" select flips the chat
# composer's footprint, live in the pane:
#
#   compact (default) -> mode-compact: ONE single row (plus-btn, input,
#                        chips, send all inline, nowrap) AND centered in
#                        the pane (auto margins; the 720px cap is the
#                        centered card's width)
#   wide              -> the pre-modes DEFAULT: the unmodified .composer
#                        (full-width card, 12px inset, no cap, no class)
#   full              -> mode-full, edge to edge (no cap, inset 0, no radius)
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

# --- 1. default = compact: ONE line, centered, the 720px cap -----------
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
assert_eq "$flexdir" "row" "compact flips the composer to ONE horizontal row"
ch=$(panejs 'p.querySelector(".composer").clientHeight')
assert_eq "1" "$(awk "BEGIN{print ($ch > 0 && $ch < 56) ? 1 : 0}")" \
  "compact is one line tall (${ch}px - anything wrapped would exceed 56px)"
wc=$(panejs 'p.querySelector(".composer").getBoundingClientRect().width')
step "ok: compact is the default shape (one line ${ch}px tall, centered, 720px cap)"

# --- 2. wide: NO class, the pre-modes default (full-width, 12px inset) --
pick_mode wide
scoped_wait '![...p.querySelector(".composer").classList].some(c=>c.startsWith("mode-"))' 10
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "12px" "wide keeps the pre-modes 12px side inset"
mcap=$(panejs 'getComputedStyle(p.querySelector(".composer")).maxWidth')
assert_eq "$mcap" "none" "wide lifts the width cap entirely (the base shape)"
ww=$(panejs 'p.querySelector(".composer").getBoundingClientRect().width')
assert_eq "1" "$(awk "BEGIN{print ($wc < $ww) ? 1 : 0}")" \
  "wide widens for real (compact ${wc}px < wide ${ww}px)"
step "ok: wide applies (no class, no cap, 12px inset; compact < wide proven)"

# --- 3. full: mode-full, no cap, edge to edge (0 inset, no radius) ------
pick_mode full
scoped_wait 'p.querySelector(".composer").classList.contains("mode-full")' 10
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
assert_eq "$mleft" "0px" "full drops the side inset"
mcap=$(panejs 'getComputedStyle(p.querySelector(".composer")).maxWidth')
assert_eq "$mcap" "none" "full lifts the width cap entirely"
radius=$(panejs 'getComputedStyle(p.querySelector(".composer")).borderRadius')
assert_eq "$radius" "0px" "full drops the corner radius"
wf=$(panejs 'p.querySelector(".composer").getBoundingClientRect().width')
assert_eq "1" "$(awk "BEGIN{print ($ww < $wf) ? 1 : 0}")" \
  "full widens for real (wide ${ww}px < full ${wf}px)"
step "ok: full applies (no cap, edge to edge)"

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
scoped_wait 'p.querySelector(".composer").classList.contains("mode-compact")' 15
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "row" "compact restores the single-line row shape"
ch=$(panejs 'p.querySelector(".composer").clientHeight')
assert_eq "1" "$(awk "BEGIN{print ($ch > 0 && $ch < 56) ? 1 : 0}")" \
  "compact restores one-line height (${ch}px)"
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "compact" "compact restored in localStorage too"

assert_no_console_errors

cleanup_journey_sessions
step "GREEN: the composer mode law holds (compact default one-line+centered, wide pre-modes default, full, persistence)"
