#!/bin/bash
# 117-right-click-surfaces - the right-click arc's step 2: sidebar rows, the statusbar
# and the sidebar tabs.
#
# The shared menu (composable contextMenu.ts + the one ui/ContextMenu.vue surface) already
# had dock tabs as its first surface (recipe 116). This recipe proves the three surfaces
# this step adds, through the REAL input pipeline - a real right-button press, the real
# rows, and a real verb fired from each:
#
#   rows      a session row menus New session / Branch / Export / Copy id (Stop only on a
#             running row), an agent row menus New agent (Delete only off the locked
#             default), a cron row menus Pause-or-Resume / Delete - and Branch, Pause,
#             Delete and New agent each DO their thing.
#   statusbar every stat is a checked row, then the Reset row; a click hides one, a reload
#             keeps it hidden, and the menu that stays on the bar shows it again.
#   tabs      a secondary-sidebar chip hides (the strip keeps the rest), the hide
#             survives a reload, and the strip's own right-click lists it for restore.
#
# The pure laws (item lists, check states, the hidden-set record) are pinned in
# webui/test/rowmenus.test.ts, statusstats.test.ts and hiddentabs.test.ts. This recipe is
# the browser half.
#
# Residue: one jrn- session (the suite's own reaper owns it), one jrn- cron job (deleted
# BY THE RECIPE through the row menu - the delete row is part of the proof), no agent home.
. "${JROOT:?run must export JROOT}/lib.sh"
# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep leaves at the first match, so a needle that IS present
# reads as missing - and the negated form reads it as absent, which is a false
# green that hides a defect. A herestring has no writer, so grep's early exit
# costs nothing. This file scans its own live lines; comments may name the
# shape, so they are skipped.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] \
	|| { printf '%s: pipes a writer into grep -q (SIGPIPE under pipefail): %s\n' "${_PIN_SELF##*/}" "$_pin_hits" >&2; exit 1; }


JOB="jrn-rc$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"
LABEL="jrn-rightclick-$(date +%d%b%H%M%S)"

# a red must never leave a job firing on the lane. CHAINED to the residue path:
# a bare trap here would replace run.sh's journey_exit_cleanup and the recipe
# would reap neither its session nor its terminal.
cleanup() {
  curl -s -X POST "$JOURNEY_BASE_URL/cron/delete" \
    -H 'Content-Type: application/json' \
    -d "{\"name\": \"$JOB\"}" >/dev/null 2>&1 || true
}
trap 'cleanup; journey_exit_cleanup' EXIT

menu_rows() {
  local r
  r=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-label")].map(e=>e.textContent.trim()).join("|")')
  printf '%s' "${r//\"/}"
}

menu_count() { jsq 'document.querySelectorAll(".context-menu-item").length'; }

right_click_at() {
  ab mouse move "$1" "$2" >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real pointer
               # pipeline coalesces a move and a press that land in one task
  ab mouse down right >/dev/null 2>&1
  ab mouse up right >/dev/null 2>&1
}

# right_click_js <expr returning "x y">
right_click_js() {
  local box
  box=$(jsq "$1")
  box=${box//\"/}
  [ -n "$box" ] || die "the right-click target never rendered: $1"
  right_click_at $box
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

# THE TAB NAME: a chip's own word, whatever the strip renders (aria-label on the icon-only
# sidebar chips, the title text on a main-pane tab) - matched case-insensitively.
TAB_NAME_JS="const name=(t)=>((t.getAttribute('aria-label')||t.title||((t.querySelector('.dock-tab-title')||{textContent:''}).textContent)||'')).trim().toLowerCase();"

# chip_box <name>: the chip's centre, empty when the chip is not rendered
chip_box() {
  jsq "(() => {${TAB_NAME_JS} const t=[...document.querySelectorAll('.dock-tab')].find((x)=>name(x)==='$(printf '%s' "$1" | tr 'A-Z' 'a-z')'); if(!t) return ''; const r=t.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
}

chip_click() {
  local clicked
  clicked=$(jsq "(() => {${TAB_NAME_JS} const t=[...document.querySelectorAll('.dock-tab')].find((x)=>name(x)==='$(printf '%s' "$1" | tr 'A-Z' 'a-z')'); if(!t) return 0; t.click(); return 1})()")
  assert_eq "${clicked//\"/}" "1" "the $1 chip is on the strip"
}

open_app
ab set viewport 1440 900 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
wait_booted

# --- seed: one labeled session and one cron job --------------------------
JRN_ID=$(new_journey_session rightclick)
label_journey_session "$JRN_ID" "$LABEL"
saved=$(curl -s -X POST "$JOURNEY_BASE_URL/cron/save" -H 'Content-Type: application/json' \
  -d "{\"name\":\"$JOB\",\"kind\":\"interval\",\"minutes\":600,\"prompt\":\"journey right-click probe - say nothing and finish\",\"deliver\":\"new-session\",\"enabled\":true}")
grep -q "\"name\":\"$JOB\"" <<<"$saved" || die "POST /cron/save did not land $JOB"
ab reload >/dev/null 2>&1
wait_booted
step "seed: session $JRN_ID and cron job $JOB on the lane"

# --- 1. a session row ----------------------------------------------------
wait_js "(() => [...document.querySelectorAll('.session-list li')].some((r)=>(r.innerText||'').includes('$LABEL')))()" 20
row_js="(() => {const row=[...document.querySelectorAll('.session-list li')].find(r=>(r.innerText||'').includes('$LABEL')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$row_js"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
rows=$(menu_rows)
assert_eq "$rows" "New session|Branch|Export|Copy id" "an idle session row menus New session, Branch, Export, Copy id - no Stop"
step "ok: the session row's right-click opens the shared menu"

ab press Escape >/dev/null 2>&1
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "Escape did not close the row menu"
fi
step "ok: Escape closes it"

right_click_js "$row_js"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
click_row "Branch"
if ! wait_js '(() => !!document.querySelector(".session-branch"))()' 20; then
  die "the Branch row forked no session"
fi
if ! wait_js '(() => !document.querySelector(".context-menu"))()' 10; then
  die "the menu stayed open after its row ran"
fi
step "ok: Branch forked the session (the row carries its branch mark)"

# --- 2. the statusbar ----------------------------------------------------
right_click_js "(() => {const b=document.querySelector('.statusbar'); const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_count)" "8" "the bar lists its seven stats plus the Reset row"
checks=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-check")].map(e=>e.textContent.trim()==="\u2713"?1:0).join("")')
assert_eq "${checks//\"/}" "1111111" "every stat opens checked - none is hidden yet"
step "ok: the statusbar's right-click lists every stat with its check state"

click_row "Message count"
if ! wait_js '(() => !(document.querySelector(".statusbar .zone.right").innerText||"").includes("messages"))()' 10; then
  die "hiding the message count left it on the bar"
fi
step "ok: a click hides the stat it names"

ab reload >/dev/null 2>&1
wait_booted
if ! wait_js '(() => !(document.querySelector(".statusbar .zone.right").innerText||"").includes("messages"))()' 10; then
  die "the hidden stat came back on reload - the set did not persist"
fi
step "ok: the hidden set survives a reload"

right_click_js "(() => {const b=document.querySelector('.statusbar'); const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
uncheck=$(jsq '(() => {const row=[...document.querySelectorAll(".context-menu-item")].find(x=>((x.querySelector(".context-menu-label")||{}).textContent||"").trim()==="Message count"); if(!row) return "missing"; const c=row.querySelector(".context-menu-check"); if(!c) return "nocheck"; const t=(c.textContent||"").trim(); return t===""?"empty":t;})()')
assert_eq "${uncheck//\"/}" "empty" "the hidden stat reads unchecked in the menu that stays"
click_row "Message count"
if ! wait_js '(() => (document.querySelector(".statusbar .zone.right").innerText||"").includes("messages"))()' 10; then
  die "the restore click did not put the stat back"
fi
step "ok: the bar's own menu restores it"

# --- 3. a secondary-sidebar tab hides and comes back ---------------------
chip=$(chip_box "Files")
[ -n "${chip//\"/}" ] || die "the Files chip never rendered"
right_click_js "$(chip_box "Files")"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Hide this tab" "a secondary-sidebar chip menus Hide this tab"
click_row "Hide this tab"
if ! wait_js "(() => {${TAB_NAME_JS} return ![...document.querySelectorAll('.dock-tab')].some((x)=>name(x)==='files')})()" 10; then
  die "the hidden chip is still on the strip"
fi
if ! wait_js '(() => {const p=[...document.querySelectorAll(".dock-pane")].find(x=>x.className.includes("kind-skills")||x.className.includes("kind-tools")); return !!p && !!p.querySelector(".dock-content")})()' 10; then
  die "hiding a chip blanked its pane - the body did not follow the next visible tab"
fi
step "ok: Hide takes the chip and the pane keeps rendering"

ab reload >/dev/null 2>&1
wait_booted
if ! wait_js "(() => {${TAB_NAME_JS} return ![...document.querySelectorAll('.dock-tab')].some((x)=>name(x)==='files')})()" 10; then
  die "the hidden chip came back on reload - the flag did not persist"
fi
step "ok: the hidden chip survives a reload"

# the strip IS the visible surface that keeps the restore door: a right-click on the
# secondary sidebar's own bar (not a chip) lists what it hid
strip_box="(() => {${TAB_NAME_JS} const p=[...document.querySelectorAll('.dock-pane')].find((x)=>[...x.querySelectorAll('.dock-tab')].some((t)=>name(t)==='skills')); if(!p) return ''; const bar=p.querySelector('.dock-tabbar'); if(!bar) return ''; const r=bar.getBoundingClientRect(); return Math.round(r.right-8)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$strip_box"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Show Files" "the strip lists the hidden tab for restore"
click_row "Show Files"
if ! wait_js "(() => {${TAB_NAME_JS} return [...document.querySelectorAll('.dock-tab')].some((x)=>name(x)==='files')})()" 10; then
  die "the restore row put no chip back"
fi
step "ok: the strip's right-click restores the chip"

# --- 4. an agent row -----------------------------------------------------
chip_click "Agents"
wait_js '(() => !!document.querySelector(".registry-rows"))()' 15
right_click_js "(() => {const row=[...document.querySelectorAll('.registry-rows li')].find(r=>!!r.querySelector('.agents-main-badge')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "New agent" "the locked default agent offers New agent and no Delete"
click_row "New agent"
if ! wait_js '(() => !!document.querySelector(".agents-create"))()' 10; then
  die "the New agent row opened no create form"
fi
step "ok: an agent row menus New agent, and it opens the create form"

# --- 5. a cron row -------------------------------------------------------
chip_click "cron"
wait_js "(() => {const c=document.querySelector('.cron-list'); return !!c && (c.innerText||'').includes('$JOB')})()" 15
row_js="(() => {const row=[...document.querySelectorAll('.cron-row')].find(r=>(r.innerText||'').includes('$JOB')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$row_js"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Pause|Delete" "an enabled cron row menus Pause and Delete"
click_row "Pause"
if ! wait_js "(() => [...document.querySelectorAll('.cron-row.disabled')].some((r)=>(r.innerText||'').includes('$JOB')))()" 15; then
  die "the Pause row did not pause $JOB"
fi
step "ok: Pause runs the row's own toggle route"

right_click_js "$row_js"
wait_js '(() => !!document.querySelector(".context-menu.measured"))()' 10
assert_eq "$(menu_rows)" "Resume|Delete" "a paused cron row menus Resume - the verb follows the state"
click_row "Delete"
if ! wait_js '(() => {const b=document.querySelector(".cron-editor-btn.danger"); return !!b && (b.textContent||"").includes("confirm delete?")})()' 10; then
  die "the Delete row did not open the editor's armed confirm"
fi
jsq '(() => {const b=[...document.querySelectorAll(".cron-editor-btn.danger")].find((x)=>(x.textContent||"").includes("confirm delete?")); if(!b) return 0; b.click(); return 1})()' >/dev/null
i=0
gone=0
while [ "$i" -lt 20 ]; do
  if ! grep -q "\"name\":\"$JOB\"" <<<"$(curl -s "$JOURNEY_BASE_URL/cron/jobs")"; then gone=1; break; fi
  sleep 0.5
  i=$((i + 1))
done
[ "$gone" = "1" ] || die "the deleted job still rides the wire"
step "ok: Delete arms the existing confirm and removes the job"

step "done: the row menus, the statusbar stat menu and the sidebar tab hide/restore all act"
