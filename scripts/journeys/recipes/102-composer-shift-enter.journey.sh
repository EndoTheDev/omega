#!/bin/bash
# 102-composer-shift-enter - the composer's Shift+Enter newline law (card
# t_eb07b315). One Shift+Enter = one visible newline, immediately, in BOTH
# composer modes (compact + full).
#
# The bug: the editor claims every Enter and Shift+Enter inserts the
# newline as a TEXT node (t_e67c372b - the browser's own newline would
# insert div nodes the draft reader cannot read). But Chromium hangs a
# block-final newline in pre-wrap: the break is real in the draft yet
# generates NO line box - the caret stayed glued to line 1 and a second
# press looked required. The fix: capTail settles ONE sentinel <br> after
# a draft-final newline (draft-invisible: a non-chip element weighs zero
# in draftTextOf/caretFromDom/placeCaretAt); the br forces the line box
# and seats the caret on the new line.
#
# Red detector baked in (the journey-98 lesson): after ONE press the
# editor's scrollHeight must grow by the line-height (the line box
# exists) - on the broken composer this stuck at the one-line height.
#
# agent-browser CAN send the modifier: `press Shift+Enter` (real typing,
# no synthetic dispatch). No model needed: the newline law is pure
# client DOM + CSS.
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session shift-enter | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) shift-enter $TAG"

# panejs/scoped_wait: the focused chat pane scoped to THIS run's label
# (recipe 22's ghost-tab law - the default pane would hit another lane's
# composer in a live browser).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) shift-enter')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}
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

# press_shift_enter_in_pane: focus THIS pane's composer, seed the text,
# press Shift+Enter ONCE with real typing, and print the editor's
# post-press report: scroll-height | draft-node kids | the sentinel count.
# Everything runs INSIDE the page eval so agent-browser's own focus does
# not race the press.
shift_enter_probe() {
  local report
  report=$(panejs '(()=>{
    const t=p.querySelector(".composer-input");
    if(!t) return "NO_INPUT";
    t.textContent="";
    t.focus();
    return "focused "+t.scrollHeight;
  })()' 2>&1 | sed -e 's/^"//' -e 's/"$//')
  case "$report" in focused*) ;; *) die "composer input unreachable in pane: $report" ;; esac
  "$AB" --session "$JOURNEY_SESSION" keyboard type "hello" >/dev/null 2>&1 \
    || die "keyboard type into the focused composer failed"
  "$AB" --session "$JOURNEY_SESSION" press Shift+Enter >/dev/null 2>&1 \
    || die "press Shift+Enter failed"
  panejs '(()=>{
    const t=p.querySelector(".composer-input");
    const sh=t.scrollHeight;
    const text=t.textContent;
    const kids=[...t.childNodes].map(n=>n.nodeType===3?(n.nodeValue&&n.nodeValue.includes("\n")?"NL":"t"):(n.tagName||"?"));
    const brs=[...t.childNodes].filter(n=>n.tagName==="BR").length;
    return sh+"|"+(text.endsWith("\n")?"ENDS-NL":"NO-ENDNL")+"|len="+text.length+"|kids="+kids.join("")+"|brs="+brs;
  })()' 2>&1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}

# one_shift_enter_in_mode <mode>: the law inside ONE composer mode
one_shift_enter_in_mode() {
  scoped_wait "p.querySelector('.composer').classList.contains('mode-$1')" 15
  local base=0 probe=""
  step "--- mode: $1 ---"
  # baseline: empty editor height (placeholder seat)
  base=$(panejs '(()=>{
    const t=p.querySelector(".composer-input");
    t.textContent=""; t.focus();
    t.style.height="auto";
    return t.scrollHeight;
  })()' 2>&1 | sed -e 's/^"//' -e 's/"$//')
  probe=$(shift_enter_probe)
  step "report: $probe"
  local sh tailmark brs len
  sh=$(echo "$probe" | cut -d'|' -f1 | tr -d ' ')
  tailmark=$(echo "$probe" | cut -d'|' -f2)
  len=$(echo "$probe" | sed -n 's/.*|len=\([0-9]*\).*/\1/p')
  brs=$(echo "$probe" | sed -n 's/.*|brs=\([0-9]*\)$/\1/p')
  assert_eq "$tailmark" "ENDS-NL" "$1: ONE press writes one newline (draft ends with it)"
  assert_eq "$len" "6" "$1: exactly one newline char landed (hello=5 + one \\n)"
  assert_eq "$brs" "1" "$1: exactly one sentinel <br> after the newline"
  # the line box: on the broken composer the trailing newline rendered
  # NOTHING (sh stayed at the one-line baseline) - here it must grow
  grew=$((sh - base))
  if [ "$grew" -lt 21 ]; then
    die "$1: ONE press grew the editor by ${grew}px (< 21px: the newline never rendered)"
  fi
  step "ok: $1 - the newline is VISIBLE (editor grew ${grew}px on one press)"
}

open_app
open_journey_row "(journey) shift-enter $TAG"
scoped_wait '!!p.querySelector(".composer")' 15

# --- a: compact -----------------------------------------------------------
one_shift_enter_in_mode compact

# --- b: full (the settings knob, recipe 22's grammar) ----------------------
CHIP_POLL='(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Settings");if(!t) return false;t.click();return true})()'
if ! poll_js "$CHIP_POLL" 4; then
  jsq '(()=>{const b=document.querySelector("button[aria-label=\"Toggle right sidebar\"]"); if(!b) return false; b.click(); return true})()' >/dev/null 2>&1 || true
  poll_js "$CHIP_POLL" 10 || die "Settings dock-tab chip never rendered"
fi
if ! poll_js '(()=>{return !!document.querySelector(".setting-select .model-chip[aria-label=\"composer display mode\"]")})()' 10; then
  die "settings never rendered the composer display mode knob"
fi
pick() {
  local chip sel
  chip=$(jsq '(()=>{const c=document.querySelector(".setting-select .model-chip[aria-label=\"composer display mode\"]"); if(!c) return 0; c.click(); return 1})()' 2>&1 | tail -1)
  assert_eq "$chip" "1" "composer display mode chip clicked"
  sel=1
  while [ "$sel" -le 20 ]; do
    pick=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="'$1'"); if(!o) return 0; o.click(); return 1})()' 2>&1 | tail -1)
    if [ "$pick" = "1" ]; then jsq 'document.body.click()' >/dev/null 2>&1 || true; return 0; fi
    sleep 0.5; sel=$((sel + 1))
  done
  die "composer display mode option $1 never rendered"
}
pick full
one_shift_enter_in_mode full

# restore compact (end state = the virgin default)
pick compact
scoped_wait "p.querySelector('.composer').classList.contains('mode-compact')" 15

assert_no_console_errors
cleanup_journey_sessions
step "GREEN: one Shift+Enter = one visible line box, both composer modes; the sentinel rides the draft invisibly"
