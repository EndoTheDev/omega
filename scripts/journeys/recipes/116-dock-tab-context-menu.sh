#!/bin/bash
# 116-dock-tab-context-menu - the shared right-click menu, proven on dock tabs (the
# right-click arc's infrastructure step).
#
# A dock tab's right-click SELECTS the tab and opens the ONE shared menu (the
# contextMenu.ts store behind the single ui/ContextMenu.vue surface), offering exactly
# the verbs that can act: Close (never the locked last tab, never an uncloseable
# component) and Close others (only when another closable tab exists). Escape, the
# arrows and Enter ARE the key grammar; a press outside closes; a chrome surface with
# nothing to offer opens nothing at all.
#
# The placement math (flip at the right/bottom edge, clamp into the viewport) is pinned
# as arithmetic in webui/test/contextmenu.test.ts and the rows as a pure builder in
# webui/test/docktabmenu.test.ts. This recipe is the browser half: a real right-button
# press, the real rows, the real closes.
#
# A second main-pane tab is minted through the menubar gear (Settings is a main-class
# closable tab), so the tab strip holds two rows without a model turn.
. "${JROOT:?run must export JROOT}/lib.sh"

MAIN_TABS='document.querySelectorAll(".dock-pane:not(.pinned) .dock-tab")'

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
wait_booted

tab_count() { jsq "${MAIN_TABS}.length"; }

open_settings_tab() {
  jsq '(() => {const b=document.querySelector("header.menubar button[aria-label=\"Open settings\"]"); if(!b) return 0; b.click(); return 1})()' >/dev/null
  wait_js "${MAIN_TABS}.length >= 2" 10
}

# the right-click rides the REAL input pipeline (a synthetic MouseEvent would prove the
# handler, not the browser's own contextmenu dispatch)
right_click_tab() {
  local i=$1 box
  box=$(jsq "(() => {const t=${MAIN_TABS}[$i]; if(!t) return ''; const r=t.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()")
  box=${box//\"/}
  [ -n "$box" ] || die "the main pane has no tab $i"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real pointer
               # pipeline coalesces a move and a press that land in one task
  ab mouse down right >/dev/null 2>&1
  ab mouse up right >/dev/null 2>&1
}

menu_rows() {
  local r
  r=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-label")].map(e=>e.textContent.trim()).join("|")')
  printf '%s' "${r//\"/}"
}

click_row() {
  local label=$1 box
  box=$(jsq "(() => {const b=[...document.querySelectorAll('.context-menu-item')].find(x=>(x.textContent||'').trim().startsWith('$label')); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()")
  box=${box//\"/}
  [ -n "$box" ] || die "no menu row named $label"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: move and press pacing
  ab mouse down >/dev/null 2>&1
  ab mouse up >/dev/null 2>&1
}

# --- a dock tab's right-click opens the shared menu -----------------------
open_settings_tab
step "ok: the main pane holds two tabs"

settings_idx=$(jsq "[...${MAIN_TABS}].findIndex(t=>(t.getAttribute('title')||'').trim()==='Settings')")
[ "$settings_idx" != "-1" ] 2>/dev/null || die "the Settings tab never rendered ($settings_idx)"
right_click_tab "$settings_idx"
if ! wait_js '!!document.querySelector(".context-menu.measured")' 10; then
  die "the dock tab's right-click opened no menu"
fi
step "ok: right-click opens the shared menu"

rows=$(menu_rows)
assert_eq "$rows" "Close|Close others" "the tab menu offers Close and Close others"

role=$(jsq '(() => {const m=document.querySelector(".context-menu"); return m?m.getAttribute("role")+"/"+m.getAttribute("aria-label"):""})()')
assert_eq "${role//\"/}" "menu/context menu" "the surface is a role=menu dialog named for what it is"

# the menu sits at the cursor and stays inside the viewport
geom=$(jsq '(() => {const r=document.querySelector(".context-menu").getBoundingClientRect(); return [Math.round(r.left),Math.round(r.top),Math.round(r.right),Math.round(r.bottom),innerWidth,innerHeight].join(",");})()')
geom=${geom//\"/}
IFS=, read -r left top right bottom vw vh <<<"$geom"
[ "$left" -ge 0 ] && [ "$top" -ge 0 ] && [ "$right" -le "$vw" ] && [ "$bottom" -le "$vh" ] \
  || die "the menu left the viewport: $geom"
step "ok: the menu is placed inside the viewport ($geom)"

# the right-click SELECTED the tab it menus
active_title=$(jsq "(() => {const t=document.querySelector('.dock-pane:not(.pinned) .dock-tab.active'); return t?(t.getAttribute('title')||'').trim():'';})()")
assert_eq "${active_title//\"/}" "Settings" "the right-click selected the tab it menus"

# --- Escape closes -------------------------------------------------------
ab press Escape >/dev/null 2>&1
if ! wait_js '!document.querySelector(".context-menu")' 10; then
  die "Escape did not close the menu"
fi
step "ok: Escape closes the menu"

# --- arrows move the selection, Enter fires it ---------------------------
right_click_tab "$settings_idx"
wait_js '!!document.querySelector(".context-menu.measured")' 10
ab press ArrowDown >/dev/null 2>&1
picked=$(jsq '(() => {const b=document.querySelector(".context-menu-item.active"); return b?(b.textContent||"").trim():"";})()')
assert_eq "${picked//\"/}" "Close others" "ArrowDown moves the selection to the next row"
ab press ArrowUp >/dev/null 2>&1
picked=$(jsq '(() => {const b=document.querySelector(".context-menu-item.active"); return b?(b.textContent||"").trim():"";})()')
assert_eq "${picked//\"/}" "Close" "ArrowUp moves it back"
step "ok: the arrows move the active row"

# Enter fires the selected row (Close others takes the strip down to one tab)
ab press Enter >/dev/null 2>&1
if ! wait_js '!document.querySelector(".context-menu")' 10; then
  die "Enter did not close the menu"
fi
if ! wait_js "${MAIN_TABS}.length === 1" 10; then
  die "Close others left $(tab_count) tabs"
fi
left=$(jsq "(() => {const t=${MAIN_TABS}[0]; return t?(t.getAttribute('title')||'').trim():'';})()")
assert_eq "${left//\"/}" "new chat" "Close others kept the tab it was invoked on"
step "ok: Enter fires Close others (the strip is back to the chat tab)"

# --- the locked last tab offers nothing --------------------------- ------------------------------
right_click_tab 0
sleep 0.4   # recipe-lint: allow fixed-sleep - absence check: the menu opens
            # synchronously on contextmenu, so a short settle cannot hide a late one
open_now=$(jsq 'document.querySelector(".context-menu")?1:0')
assert_eq "$open_now" "0" "the locked last tab opens no menu (nothing to fire)"
step "ok: a single tab offers no menu"

# --- Close, on a real tab, through the row -------------------------------
open_settings_tab
settings_idx=$(jsq "[...${MAIN_TABS}].findIndex(t=>(t.getAttribute('title')||'').trim()==='Settings')")
right_click_tab "$settings_idx"
wait_js '!!document.querySelector(".context-menu.measured")' 10
click_row "Close"
if ! wait_js "${MAIN_TABS}.length === 1" 10; then
  die "Close left $(tab_count) tabs"
fi
if ! wait_js '!document.querySelector(".context-menu")' 10; then
  die "the menu stayed open after its row ran"
fi
step "ok: the Close row closes the tab it was opened on"

# --- a press outside closes ----------------------------------------------
open_settings_tab
settings_idx=$(jsq "[...${MAIN_TABS}].findIndex(t=>(t.getAttribute('title')||'').trim()==='Settings')")
right_click_tab "$settings_idx"
wait_js '!!document.querySelector(".context-menu.measured")' 10
ab mouse move 200 300 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
ab mouse up >/dev/null 2>&1
if ! wait_js '!document.querySelector(".context-menu")' 10; then
  die "a press outside the menu did not close it"
fi
step "ok: a press outside closes the menu"

step "done: the shared right-click menu opens, acts and closes on dock tabs"
