#!/bin/bash
# 23-composer-slash-token - the caret-token slash law (card t_d1a2951d):
# the / command palette opens WHEREVER the caret sits inside a "/" token,
# never only at draft start.
#
# The composer's slash surface used to gate on draft.startsWith("/") - a
# /command typed mid-sentence ("use /skill to do x") could never open the
# palette, no matter where the caret sat. The fix made the gate
# TOKEN-based, like the mention surface next to it: the caret inside a
# token that starts with "/" opens the same palette, the token filters
# the rows, past the token's trailing space (args, plain sentence) the
# palette stands down, Escape dismisses until the next draft write.
# Invariants, in the customer's keypresses:
#
#   "use /hel"  -> the palette opens mid-sentence, the /help row in it
#   Escape      -> the palette closes, the draft keeps its text
#   " ok"       -> the token ended (the space): the palette stays closed
#   "/hel"      -> the LEADING case: the same palette, same rows (the
#                  leading word is just the boundary-free token case)
#
# No model needed: the palette is pure client DOM (the fetchers answer
# locally; /help is a shell row that requires nothing).
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session slash | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) composer-slash $TAG"

# panejs <expr over p>: evaluate expr against the FOCUSED chat pane
# scoped to this run's label (recipe 30's ghost-tab law).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) composer-slash')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^\"//' -e 's/\"$//' -e 's/\\\\\"/\"/g'
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

open_app
open_journey_row "(journey) composer-slash $TAG"

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

# --- 1. mid-sentence: typing "use /hel" opens the palette ------------------
ab keyboard type "use /hel"
scoped_wait 'p.querySelector(".composer-palette") && p.querySelector(".composer-palette").innerText.includes("/help")' 15
assert_eq "$(panejs 'p.querySelector(".composer-palette") ? 1 : 0')" "1" "the palette opens mid-sentence (caret inside the /token)"
assert_eq "$(panejs 'p.querySelector(".composer-palette").innerText.includes("/help") ? 1 : 0')" "1" "the /help row filters in from the mid-sentence token"
step "ok: mid-sentence slash token opens the palette"

# --- 2. Escape dismisses; the draft keeps its text ---------------------------
ab press Escape
scoped_wait '!p.querySelector(".composer-palette")' 15
assert_eq "$(panejs 'p.querySelector(".composer-input").innerText.indexOf("use /hel") === 0 ? 1 : 0')" "1" "Escape closed the palette, the draft survived"
step "ok: Escape dismisses the mid-sentence palette"

# --- 3. the token ends - the palette stays closed ----------------------------
ab keyboard type " ok"
scoped_wait '!p.querySelector(".composer-palette")' 15
assert_eq "$(panejs 'p.querySelector(".composer-input").innerText.includes("/hel ok") ? 1 : 0')" "1" "the sentence typed on past the token"
assert_eq "$(panejs 'p.querySelector(".composer-palette") ? 1 : 0')" "0" "past the token's space the palette stands down"
step "ok: the palette stands down past the token"

# --- 4. the leading case: same palette, same rows -----------------------------
for i in 1 2 3 4 5 6 7 8 9 10 11; do ab press Backspace; done
ab keyboard type "/hel"
scoped_wait 'p.querySelector(".composer-palette") && p.querySelector(".composer-palette").innerText.includes("/help")' 15
assert_eq "$(panejs 'p.querySelector(".composer-palette") ? 1 : 0')" "1" "the leading slash opens the same palette"
ab press Escape
scoped_wait '!p.querySelector(".composer-palette")' 15
step "ok: the leading case behaves identically"

# the editor carried a real user all the way: no console noise
assert_no_console_errors

cleanup_journey_sessions
step "GREEN: the caret-token slash law holds (mid-sentence, dismiss, token end, leading parity)"
