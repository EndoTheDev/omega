#!/bin/bash
# 86-dock-round5 - dock round 5 journey (t_556ee727).
# Three fixes, one journey:
#   1 tab labels: the agents + extensions tab chips render Title Case
#     (Agents / Extensions) like their siblings - display labels only,
#     ids/routes/module names stay lowercase
#   2 notification nav: a task notification's click focuses the KANBAN
#     pane (opens the hidden inspector if needed, activates the kanban
#     tab), selects the board the task lives on in the board switcher,
#     and NEVER touches the main pane (chat content unchanged, no
#     /board route on the bell's path)
#   3 aligned bottom: with the bottom panel open (the default aligned
#     width mode) the pinned sidebars keep their full height - both run
#     down to the band's bottom edge beside it (no dead space); "full"
#     mode restores the sidebars-above-the-band layout
# No model needed: pure shell UI. Runs against JOURNEY_BASE_URL.
. "${JROOT:?run must export JROOT}/lib.sh"

BTAG="j86$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"
OMEGA_BIN="${OMEGA_BIN:-$JROOT/../../bin/omega}"
BOARD="$BTAG-b"
TID=""

trap 'curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${TID}?board=${BOARD}" \
  -H "Content-Type: application/json" -d "{\"status\":\"archived\"}" >/dev/null 2>&1 || true' EXIT

# ---- seed: a done task on a NON-default board (the notification) ------
step "creating registry board: $BOARD"
"$OMEGA_BIN" kanban board create "$BOARD" >/dev/null || die "board create"
TID=$(curl -s -X POST "${JOURNEY_BASE_URL}/tasks?board=$BOARD" \
  -H 'Content-Type: application/json' -d "{\"title\": \"card on $BOARD\"}" |
  grep -oE 't_[0-9a-f]+' | head -1)
[ -n "$TID" ] || die "task create failed"
curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${TID}?board=$BOARD" \
  -H 'Content-Type: application/json' -d '{"status":"done"}' >/dev/null || die "task done"

# ---- boot -----------------------------------------------------------------
open_app
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1

# ---- 1: tab labels --------------------------------------------------------
# card t_210a0d19 rename law: the extensions inventory tab's label is
# "Modules"; /extensions keeps working as the command alias.
# card t_502b8e81: edge shells render ICON-ONLY chips - the name rides
# aria-label (the tab has no .dock-tab-title); main/bottom tabs keep text.
chipName='((e.getAttribute("aria-label")||"")||e.title||((e.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim()'
for want in Agents Modules; do
  if ! wait_js "(()=>{const t=[...document.querySelectorAll('.dock-tabbar .dock-tab')];return t.some((e)=>($chipName)==='$want')})()" 10; then
    die "tab label '$want' missing from the tab bars"
  fi
done
low=$(jsq "(()=>{const t=[...document.querySelectorAll(\".dock-tabbar .dock-tab\")].map((e)=>$chipName);return t.some((n)=>n===\"agents\"||n===\"extensions\")?1:0})()" 2>/dev/null | tail -1)
assert_eq "$low" "0" "no lowercase agents/extensions tab chips remain"
step "ok: Agents + Modules tab labels render Title Case"

# ---- 2: the notification click focuses the kanban pane --------------------
# chat pane state BEFORE the click: the main pane's identity + content
before_url=$(jsq 'location.pathname' 2>/dev/null | tail -1)
before_pane=$(jsq 'document.querySelector(".dock-pane:not(.pinned)")?.dataset.paneId ?? ""' 2>/dev/null | tail -1)
assert_eq "$(jsq 'document.querySelector(".composer")?1:0' 2>/dev/null | tail -1)" \
  "1" "chat composer renders before the click"

js 'document.querySelector("#notif-bell").click()' >/dev/null
wait_js '(()=>{const r=[...document.querySelectorAll(".notif-list .notif-row")];return r.some((e)=>e.textContent.includes("'"$TID"'"))})()' 10 \
  || die "the task notification row never rendered"
# click exactly the task's row (not the board, not a verdict row)
js "(()=>{const r=[...document.querySelectorAll('.notif-list .notif-row')].find((e)=>e.textContent.includes('$TID'));r&&r.click();return r?1:0})()" >/dev/null

# the kanban tab is foregrounded in the inspector (icon chip: aria-label)
wait_js "(()=>{const t=[...document.querySelectorAll(\".dock-pane.pinned:not(.edge-bottom) .dock-tab.active\")];return t.some((e)=>($chipName)===\"Kanban\")})()" 10 \
  || die "the kanban tab never came to the foreground after the click"
if ! wait_js '(()=>{return document.querySelector(".dock-pane .board")?true:false})()' 10; then
  die "the board panel never rendered (kanban tab active but no board body)"
fi
step "ok: the click foregrounds the kanban pane"

# the board switcher picked the task's board
if ! wait_js "(()=>{const s=document.querySelector('#board-switcher');return !!s && s.value==='$BOARD'})()" 10; then
  die "the board switcher never selected the notification's board ($BOARD)"
fi
step "ok: the notification's board is selected in the switcher"

# the main pane NEVER changed: same pane id, composer intact, no
# .board-pane overlay, URL untouched (the dead /board main-view route)
assert_eq "$(jsq 'location.pathname' 2>/dev/null | tail -1)" "$before_url" "URL unchanged after the click"
assert_eq "$(jsq 'document.querySelector(".dock-pane:not(.pinned)")?.dataset.paneId ?? ""' 2>/dev/null | tail -1)" \
  "$before_pane" "main pane identity unchanged"
assert_eq "$(jsq 'document.querySelector(".composer")?1:0' 2>/dev/null | tail -1)" \
  "1" "chat composer still renders after the click"
assert_eq "$(jsq 'document.querySelector(".board-pane")?1:0' 2>/dev/null | tail -1)" \
  "0" "no main-pane board overlay rendered"

# the hidden-inspector path: hide the inspector, then the click reopens it
js 'document.querySelector("[aria-label=\"Toggle right sidebar\"]")?.click()' >/dev/null
sleep 1
assert_eq "$(jsq 'document.querySelector(".dock-pane.kind-files")?1:0' 2>/dev/null | tail -1)" \
  "0" "the inspector is hidden before the reopen"
js 'document.querySelector("#notif-bell").click()' >/dev/null
sleep 0.5
js "(()=>{const r=[...document.querySelectorAll('.notif-list .notif-row')].find((e)=>e.textContent.includes('$TID'));r&&r.click();return r?1:0})()" >/dev/null
# the hidden inspector reopen: chip name via aria-label (same law)
wait_js "(()=>{const t=[...document.querySelectorAll(\".dock-pane .dock-tab.active\")];return t.some((e)=>($chipName)===\"Kanban\")})()" 10 \
  || die "the click never reopened the hidden inspector"
step "ok: a hidden inspector reopens on the click"

# ---- 3: aligned bottom - the sidebars keep full height --------------------
band=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0' 2>/dev/null | tail -1)
assert_eq "$band" "1" "the bottom panel renders (the default boot)"

# aligned: both pinned sidebars end AT the band's bottom edge (their
# overhang runs past the divider, beside the band, into the dead space)
if ! wait_js '(()=>{const b=document.querySelector(".dock-pane.edge-bottom");if(!b)return false;const sides=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom)")];return sides.length===2 && sides.every((s)=>Math.abs(s.getBoundingClientRect().bottom - b.getBoundingClientRect().bottom)<=3)})()' 10; then
  die "aligned mode: a pinned sidebar does not reach the band's bottom edge (dead space or overlap)"
fi
# sidebars never overlap the band horizontally: the band starts right of
# the left sidebar's right edge and ends left of the right sidebar's left
overlap=$(jsq '(()=>{const b=document.querySelector(".dock-pane.edge-bottom").getBoundingClientRect();const s=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom)")].map((x)=>x.getBoundingClientRect());const l=s.find((x)=>x.left<=window.innerWidth/2),r=s.find((x)=>x.left>window.innerWidth/2);return b.left>=l.right-1&&b.right<=r.left+1?1:0})()' 2>/dev/null | tail -1)
assert_eq "$overlap" "1" "the band stands clear of both sidebars (no horizontal overlap)"
step "ok: aligned mode - sidebars full height, band between them"

# full: the pre-card read - the sidebars terminate above the band
js 'localStorage.setItem("omega.dock-bottom-width-mode","full")' >/dev/null
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
full=$(jsq '(()=>{const b=document.querySelector(".dock-pane.edge-bottom");if(!b)return -1;const sides=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom)")];return sides.length===2 && sides.every((s)=>s.getBoundingClientRect().bottom <= b.getBoundingClientRect().top+1)?1:0})()' 2>/dev/null | tail -1)
assert_eq "$full" "1" "full mode: the sidebars end above the band"

# back to aligned: the overhang re-applies after the flip (no stale full)
js 'localStorage.setItem("omega.dock-bottom-width-mode","aligned")' >/dev/null
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
back=$(jsq '(()=>{const b=document.querySelector(".dock-pane.edge-bottom");if(!b)return -1;const sides=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom)")];return sides.length===2 && sides.every((s)=>Math.abs(s.getBoundingClientRect().bottom - b.getBoundingClientRect().bottom)<=3)?1:0})()' 2>/dev/null | tail -1)
assert_eq "$back" "1" "aligned re-applies after the full flip"

assert_no_console_errors
step "86-dock-round5 GREEN"