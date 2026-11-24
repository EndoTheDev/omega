#!/bin/bash
# 119-wire-only-menus - the right-click arc's step 5a: the should-have rows that needed no
# new backend verb (card t_4ff8c0d7).
#
# One recipe, four surfaces, each proven through the REAL input pipeline (a real
# right-button press, the real rows, a real verb fired from the row):
#
#   board    the board surface menus Rename / Delete - the switcher's own two verbs. Delete
#            arms on the first pick (the notice warns) and fires on the second, so one
#            confirm governs both doors. Rename swaps the band chip for its input.
#   card     Delete joins the card menu behind the same arm, and the DELETE really removes
#            the card (server agrees).
#   session  a session row menus Copy id; the row writes the session's id through the ONE
#            clipboard path and the toast names it.
#   statusbar the last row is Reset statusbar: one click clears the whole persisted hidden
#            set, so a stat hidden before a reload is back after it.
#
# Order matters: the statusbar step is LAST because it reloads the SPA, and a reload
# re-boots onto the registry's current board - so every board step runs before it.
#
# Escape closes every menu. The pure laws (row lists, the arm labels, the reset write) are
# pinned in webui/test/boardmenu.test.ts, boardcardmenu.test.ts, rowmenus.test.ts and
# statusstats.test.ts - this recipe is the browser half.
#
# Residue: the pinned board is CREATED here and DELETED here (the recipe's board-delete
# step is part of the proof); its cards go with it, and the trap repeats the delete on a
# red. One jrn- session (the suite's own reaper owns it).
. "${JROOT:?run must export JROOT}/lib.sh"

BOARD="j119-menu"
TAG="$(date +%s%N | cut -c1-13)"
CARD=""
JRN_ID=""
LABEL=""

cleanup() {
  # the board delete is the last step of the proof, so a red may leave it standing: clear
  # the live cards first (the route refuses a board that still holds any), then the board.
  local id
  for id in $(curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" 2>/dev/null |
    tr -d '\r' | grep -oE '"id":"t_[0-9a-f]+"' | grep -oE 't_[0-9a-f]+'); do
    curl -s -X DELETE "${JOURNEY_BASE_URL}/tasks/${id}?board=${BOARD}" >/dev/null 2>&1 || true
  done
  curl -s -X DELETE "${JOURNEY_BASE_URL}/boards/${BOARD}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

menu_rows() {
  local r
  r=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-label")].map(e=>e.textContent.trim()).join("|")')
  printf '%s' "${r//\"/}"
}

# right_click_js <expr returning "x y">
right_click_js() {
  local box
  box=$(jsq "$1")
  box=${box//\"/}
  [ -n "$box" ] || die "the right-click target never rendered: $1"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real pointer
               # pipeline coalesces a move and a press that land in one task
  ab mouse down right >/dev/null 2>&1
  ab mouse up right >/dev/null 2>&1
}

click_row() {
  local label=$1 box
  # the label span, never the row's whole text: a checked row carries its state glyph first
  box=$(jsq "(() => {const b=[...document.querySelectorAll('.context-menu-item')].find(x=>((x.querySelector('.context-menu-label')||{}).textContent||'').trim()==='$label'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()")
  box=${box//\"/}
  [ -n "$box" ] || die "no menu row named $label"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: move and press pacing
  ab mouse down >/dev/null 2>&1
  ab mouse up >/dev/null 2>&1
}

board_rect_js() {
  printf '(function(){var b=document.querySelector(".board-col[data-status=todo]");if(!b) return "";var r=b.getBoundingClientRect();return Math.round(r.x+r.width/2)+" "+Math.round(r.y+18)})()'
}

card_rect_js() {
  printf '(function(){var c=[...document.querySelectorAll(".card")].find(function(x){return x.dataset.id==="%s"});if(!c) return "";var r=c.getBoundingClientRect();return Math.round(r.x+r.width/2)+" "+Math.round(r.y+r.height/2)})()' "$1"
}

notice_text() {
  local r
  r=$(jsq '(() => {const n=document.querySelector(".board-notice"); return n?n.textContent.trim():"none"})()')
  printf '%s' "${r//\"/}"
}

# switch_to_board: the switcher is the shared BandSelect listbox (recipe 98/118's shape).
# The chip mounts with the /boards read; a miss reloads once - a fresh mount re-fetches.
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
    die "board $BOARD never rendered this run's card (tag $TAG)"
}

# --- seed: one board, one card, one labeled session -----------------------
step "seeding board $BOARD"
# a lane whose registry is EMPTY has no boot board and no switcher chip, so the run would
# die before the first menu: give it the default back (the recipe deletes only its own).
curl -s "${JOURNEY_BASE_URL}/boards" | tr -d '\r' | grep -q '"name"' ||
  curl -s -X POST "${JOURNEY_BASE_URL}/boards" -H 'Content-Type: application/json' \
    -d '{"name":"main"}' >/dev/null 2>&1 || true
curl -s -X POST "${JOURNEY_BASE_URL}/boards" -H 'Content-Type: application/json' \
  -d "{\"name\":\"$BOARD\"}" >/dev/null 2>&1 || true
curl -s "${JOURNEY_BASE_URL}/boards" | tr -d '\r' | grep -q "\"name\":\"$BOARD\"" ||
  die "the board $BOARD is neither created nor listed"
CARD=$(curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" \
  -H 'Content-Type: application/json' \
  -d "{\"title\": \"j119 $TAG card\", \"status\": \"todo\"}" |
  grep -oE 't_[0-9a-f]+' | head -1)
[ -n "$CARD" ] || die "card seeding failed"
JRN_ID=$(new_journey_session wiremenus | tail -1)
LABEL="(journey) wiremenus $TAG"
label_journey_session "$JRN_ID" "$LABEL"
step "ok: board $BOARD, card $CARD, session $JRN_ID"

open_app
ab open "${JOURNEY_BASE_URL}/board" >/dev/null 2>&1 || die "cannot open /board"
ab wait --load networkidle >/dev/null 2>&1 || true
switch_to_board
step "ok: board $BOARD is the open board"

# --- 1. the board surface's rows -----------------------------------------
right_click_js "$(board_rect_js)"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the board surface's right-click opened no menu"
rows=$(menu_rows)
assert_eq "$rows" "Rename|Delete" "the board surface menus Rename and Delete"
step "ok: the board's right-click opens Rename and Delete"

ab press Escape >/dev/null 2>&1
wait_js '(() => !document.querySelector(".context-menu"))()' 10 ||
  die "Escape did not close the board menu"
step "ok: Escape closes the board menu"

# --- 2. the card menu's Delete: arm, then confirm -------------------------
right_click_js "$(card_rect_js "$CARD")"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "card $CARD: the right-click opened no menu"
assert_eq "$(menu_rows)" "Open|Copy id|Archive|Delete" "a live card menus Delete after Archive"
click_row "Delete"
if ! wait_js "(() => {const n=document.querySelector('.board-notice'); return !!n && (n.textContent||'').includes('Confirm delete')})()" 10; then
  die "the first Delete pick did not arm (notice: $(notice_text))"
fi
step "ok: the first Delete pick only warns (arm)"

right_click_js "$(card_rect_js "$CARD")"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "card $CARD: the second right-click opened no menu"
assert_eq "$(menu_rows)" "Open|Copy id|Archive|Confirm delete" "the armed menu relabels the row"
click_row "Confirm delete"
if ! wait_js "(() => {const c=document.querySelector('.board-col[data-status=todo]'); return !!c && ![...c.querySelectorAll('.card')].some(x=>x.dataset.id==='$CARD')})()" 10; then
  die "Confirm delete left card $CARD on the grid"
fi
i=0
while [ "$i" -lt 20 ]; do
  case " $(curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" | tr -d '\r' | grep -oE 't_[0-9a-f]+' | tr '\n' ' ') " in
    *" $CARD "*) sleep 0.5 ; i=$((i + 1)) ;;
    *) break ;;
  esac
done
case " $(curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" | tr -d '\r' | grep -oE 't_[0-9a-f]+' | tr '\n' ' ') " in
  *" $CARD "*) die "the server still holds card $CARD after Confirm delete" ;;
esac
step "ok: Delete needed two picks and the server dropped the card"
CARD=""

# --- 3. the session row's Copy id ----------------------------------------
wait_js "(() => [...document.querySelectorAll('.session-list li')].some((r)=>(r.innerText||'').includes('$LABEL')))()" 20 ||
  die "the journey session row never rendered"
right_click_js "(() => {const row=[...document.querySelectorAll('.session-list li')].find(r=>(r.innerText||'').includes('$LABEL')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the session row's right-click opened no menu"
assert_eq "$(menu_rows)" "New session|Rename|Branch|Export|Copy id|Archive|Delete" "the session row menus Copy id after Export, with Rename, Archive and Delete around it"

# the ONE copy path is instrumented (the app's own call is the receipt - a clipboard READ
# is permission-gated on this lane, recipe 115's law)
jsq '(()=>{
  window.__copyArgs=[];
  const c=navigator.clipboard;
  if(c&&typeof c.writeText==="function"){
    const o=c.writeText.bind(c);
    Object.defineProperty(c,"writeText",{configurable:true,value:(t)=>{window.__copyArgs.push(t);return o(t);}});
  }
  return "instrumented";
})()' >/dev/null 2>&1
click_row "Copy id"
if ! wait_js "(() => [...document.querySelectorAll('.notif-toast')].some(t=>(t.innerText||'').includes('copied session id $JRN_ID')))()" 15; then
  die "the Copy id row never confirmed the copy"
fi
payload=$(jsq '(() => {const a=(window.__copyArgs||[]); return a.length?a[a.length-1]:""})()')
assert_eq "${payload//\"/}" "$JRN_ID" "Copy id wrote the row's own session id"
step "ok: Copy id is on the session row and writes the id"

# --- 4. the board's Delete: arm, then confirm ----------------------------
# the board under the pointer must be THIS run's board: a reload (the statusbar step below)
# re-boots the SPA onto the registry's current board, and a menu fired at the wrong board
# would delete the wrong row.
wait_js "(() => {const c=document.querySelector('.board-switch-select .model-chip'); return !!c && (c.textContent||'').includes('$BOARD')})()" 10 ||
  die "the open board is not $BOARD - refusing to menu another board's Delete"
right_click_js "$(board_rect_js)"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the board surface's right-click opened no menu"
click_row "Delete"
if ! wait_js "(() => {const n=document.querySelector('.board-notice'); return !!n && (n.textContent||'').includes('Confirm delete')})()" 10; then
  die "the board Delete did not arm (notice: $(notice_text))"
fi
step "ok: the first board Delete pick only warns (arm)"

right_click_js "$(board_rect_js)"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the board surface's second right-click opened no menu"
assert_eq "$(menu_rows)" "Rename|Confirm delete" "the armed board menu relabels the row"
click_row "Confirm delete"
if ! wait_js "(() => {const n=document.querySelector('.board-notice'); return !!n && (n.textContent||'').trim()==='board \"$BOARD\" removed'})()" 15; then
  die "the board delete reported: $(notice_text)"
fi
probe="$JOURNEY_WORKTMP/j119-boards.json"
i=0
while [ "$i" -lt 20 ]; do
  curl -s "${JOURNEY_BASE_URL}/boards" >"$probe" 2>/dev/null || true
  if ! grep -q "\"name\":\"$BOARD\"" "$probe" 2>/dev/null; then
    break
  fi
  sleep 0.5
  i=$((i + 1))
done
if grep -q "\"name\":\"$BOARD\"" "$probe" 2>/dev/null; then
  die "GET /boards still lists $BOARD after Confirm delete"
fi
step "ok: Delete needed two picks and the board left the registry"

# --- 5. the statusbar's Reset row ----------------------------------------
# LAST, because this step reloads the SPA - and a reload re-boots onto the registry's
# current board, which is no longer this run's board.
right_click_js "(() => {const b=document.querySelector('.statusbar'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the statusbar's right-click opened no menu"
rows=$(menu_rows)
case "|$rows|" in
  *"|Reset statusbar|"*) ;;
  *) die "the stat menu carries no Reset row: $rows" ;;
esac
assert_eq "${rows##*|}" "Reset statusbar" "Reset is the LAST row of the stat menu"

click_row "Message count"
if ! wait_js '(() => !(document.querySelector(".statusbar .zone.right").innerText||"").includes("messages"))()' 10; then
  die "hiding the message count left it on the bar"
fi
ab reload >/dev/null 2>&1 || true
ab wait --load networkidle >/dev/null 2>&1 || true
wait_booted
wait_js '(() => !(document.querySelector(".statusbar .zone.right").innerText||"").includes("messages"))()' 10 ||
  die "the hidden stat came back on reload - the set did not persist"
step "ok: one stat is hidden and the hide survived a reload"

right_click_js "(() => {const b=document.querySelector('.statusbar'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the statusbar's second right-click opened no menu"
click_row "Reset statusbar"
if ! wait_js '(() => (document.querySelector(".statusbar .zone.right").innerText||"").includes("messages"))()' 10; then
  die "Reset statusbar did not put the hidden stat back"
fi
step "ok: Reset statusbar clears the persisted hidden set"

right_click_js "(() => {const b=document.querySelector('.statusbar'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10 ||
  die "the statusbar's third right-click opened no menu"
dim=$(jsq '(() => {const b=[...document.querySelectorAll(".context-menu-item")].find(x=>((x.querySelector(".context-menu-label")||{}).textContent||"").trim()==="Reset statusbar"); return b?String(b.disabled):"missing"})()')
assert_eq "${dim//\"/}" "true" "with nothing hidden the Reset row is dimmed, never a dead verb"
ab press Escape >/dev/null 2>&1
step "ok: the reset row follows the set it clears"

assert_no_console_errors
step "119-wire-only-menus GREEN"
