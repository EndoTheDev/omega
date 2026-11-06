#!/bin/bash
# 21-composer-chips - the chip law (card t_e67c372b): a reference pill
# REPLACES the text it represents, inline in the composer's editor.
#
# The composer's text region is a contenteditable .composer-input; a
# reference token materializes as a non-editable .ref-chip span
# (data-token carries the raw token) and the raw token leaves the
# editor's plain text. Invariants, in the customer's keypresses:
#
#   type       -> a standalone @path token chips once the mention
#                 palette closes (caret past the trailing text) and
#                 the editor's plain text no longer carries the raw
#                 "@path"
#   Backspace  -> caret walked back to the chip's end reopens the
#                 mention query by design (chipFor's palette rule
#                 renders the token raw); Escape dismisses it, and ONE
#                 more Backspace fires the DISSOLVE: the chip dies and
#                 the raw token text re-materializes (a reveal, not a
#                 delete)
#   space      -> the caret leaves the span: the chip re-forms
#   the x      -> the chip's remove button deletes the token outright
#
# No model needed: the chip law is pure client DOM (a file token stands
# on its syntax alone - no workspace, no palette rows required).
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session chips | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) composer-chips $TAG"

# panejs <expr over p>: evaluate expr against the FOCUSED chat pane
# scoped to this run's label (recipe 30's ghost-tab law).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) composer-chips')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}

# scoped_wait <expr> <timeout_s>: poll the focused pane; timeout dies.
# Accepts both "1" and "true" (lib.sh's wait_js speaks "true", recipe
# 30's panejs speaks "1" - the expressions here return bare booleans)
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

open_app
open_journey_row "(journey) composer-chips $TAG"

# focus THIS pane's editor (the contenteditable composer input)
focus=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  focus=$(panejs '(p.querySelector(".composer .composer-input")||{}).focus ? 1 : 0')
  if [ "$focus" = "1" ]; then
    panejs 'p.querySelector(".composer .composer-input").focus(), 1' >/dev/null 2>&1
    break
  fi
  sleep 1
done
assert_eq "$focus" "1" "focused pane's composer editor reachable"

# --- 1. the chip: a standalone @path replaces its text ----------------------
ab keyboard type "read @docs/webui.md ok"
# the chip materializes once the mention palette closes (caret past
# the trailing text - the plan watch syncs on the next tick)
scoped_wait 'p.querySelectorAll(".composer .ref-chip").length === 1' 15
assert_eq "$(panejs 'p.querySelectorAll(".composer .ref-chip").length')" "1" "one chip renders for the @path token"
assert_eq "$(panejs '(p.querySelector(".ref-chip")||{dataset:{}}).dataset.token')" "@docs/webui.md" "chip carries the raw token in data-token"
assert_eq "$(panejs 'p.querySelector(".composer-input").innerText.includes("@docs") ? 1 : 0')" "0" "the raw token left the editor's plain text (the chip replaced it)"
step "ok: chip replaces the token text"

# --- 2. the dissolve: Backspace behind the chip reveals the raw text --------
# walk the caret back to the chip's end seat: the mention query
# REOPENS there by design (chipFor's palette rule - the token renders
# raw while the query is being edited), Escape dismisses it, and the
# next Backspace fires the dissolve law
ab press Left
ab press Left
ab press Escape
ab press Backspace
scoped_wait 'p.querySelectorAll(".composer .ref-chip").length === 0 && (p.querySelector(".composer-input")||{innerText:""}).innerText.includes("@docs/webui.md")' 15
assert_eq "$(panejs 'p.querySelectorAll(".composer .ref-chip").length')" "0" "Backspace behind the chip dissolves it"
assert_eq "$(panejs 'p.querySelector(".composer-input").innerText.includes("@docs/webui.md") ? 1 : 0')" "1" "the raw token text re-materialized (reveal, not delete)"
step "ok: Backspace dissolves the chip back to raw text"

# --- 3. the re-chip: the caret leaves the span, the chip re-forms ------------
ab keyboard type " "
scoped_wait 'p.querySelectorAll(".composer .ref-chip").length === 1' 15
assert_eq "$(panejs '(p.querySelector(".ref-chip")||{dataset:{}}).dataset.token')" "@docs/webui.md" "the chip re-forms once the caret leaves the span"
step "ok: chip re-forms away from the span"

# --- 4. the x: outright removal ----------------------------------------------
panejs 'p.querySelector(".ref-chip .ref-remove").click(), 1' >/dev/null 2>&1
scoped_wait 'p.querySelectorAll(".composer .ref-chip").length === 0' 15
assert_eq "$(panejs 'p.querySelector(".composer-input").innerText.includes("@docs") ? 1 : 0')" "0" "the chip's x removed the token outright"
step "ok: the x removes the token"

# the editor carried a real user all the way: no console noise
assert_no_console_errors

cleanup_journey_sessions
step "GREEN: the chip law holds (chip, dissolve, re-form, remove)"
