#!/bin/bash
# 78-bottom-panel - the bottom panel journey (t_67fc1905).
# The horizontal pane pinned under the main panel, logs home:
#   1  fresh load: the bottom shell renders (edge-bottom pane, 200 default
#      rung) with the logs tab live and polling - and sits UNDER the main
#      panel (geometry, not just presence)
#   2  divider drag resizes its height on the +200 snap ladder (200 -> 400)
#   3  reload persists the resized tree
#   4  close/reopen: the menubar toggle round-trips the panel; closure
#      SURVIVES a reload (BOTTOM_CLOSED_KEY), the reopen restores the
#      remembered height
#   5  mobile (<768px): the bottom cell zero-collapses (state survives),
#      the desktop toggle drops out, and the logs view stays reachable in
#      the inspector drawer (the dual-host union)
#
# No model needed: pure shell UI. Runs against JOURNEY_BASE_URL.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1

# clean baseline: the fresh default tree (column root = main row + bottom)
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1

# --- 1: presence, default height, logs tab live -------------------------
bottom=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0')
assert_eq "$bottom" "1" "bottom panel renders (edge-bottom pane)"

height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom").getBoundingClientRect().height)')
assert_eq "$height" "200" "fresh bottom panel height = the default rung 200"

tab=$(jsq '(() => {
  const pane = document.querySelector(".dock-pane.edge-bottom");
  if (!pane) return "";
  return Array.from(pane.querySelectorAll(".dock-tab")).map((t) => t.textContent.trim()).join(",");
})()' 2>/dev/null | tr -d '"')
assert_eq "$tab" "Logs" "logs is the bottom panel's default tab"

# the logs body streams: it polls /logs every 2.5s - give one tick plus a
# margin for the poll's first requests to fill the tail
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length >= 1' 12 \
  || die "logs tab shows no lines after polling (the tail read)"
lines=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length')
step "ok: logs tab live in the bottom panel ($lines lines)"

# under-ness: the bottom cell's top edge sits BELOW the main chat pane's
# top edge and above the statusbar - a horizontal pane under the main one
under=$(jsq '(() => {
  const b = document.querySelector(".dock-pane.edge-bottom");
  const chat = document.querySelector(".dock-pane .composer");
  if (!b || !chat) return 0;
  return b.getBoundingClientRect().top > chat.getBoundingClientRect().top ? 1 : 0;
})()')
assert_eq "$under" "1" "bottom panel renders under the main panel"

# --- 2: divider drag (+200 ladder: 200 -> 400) --------------------------
# the bottom divider is the column root's LAST divider in the DOM
divx=$(jsq '(() => {
  const ds = document.querySelectorAll(".dock-divider");
  return Math.round(ds[ds.length - 1].getBoundingClientRect().x) + 2;
})()')
divy=$(jsq '(() => {
  const ds = document.querySelectorAll(".dock-divider");
  return Math.round(ds[ds.length - 1].getBoundingClientRect().y) + 3;
})()')
[ -n "$divx" ] && [ -n "$divy" ] || die "no bottom divider found"
ab mouse move "$divx" "$divy" >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
d=30; while [ "$d" -le 200 ]; do ab mouse move "$divx" $((divy + d)) >/dev/null 2>&1; sleep 0.03; d=$((d + 30)); done
ab mouse move "$divx" $((divy + 200)) >/dev/null 2>&1
ab mouse up >/dev/null 2>&1
sleep 0.4
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom").getBoundingClientRect().height)')
assert_eq "$height" "400" "divider drag resizes the bottom panel; 400 is the +200 ladder rung (snap zone 385..415)"

# --- 3: the resized tree survives a reload ------------------------------
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom")?.getBoundingClientRect().height ?? 0)')
assert_eq "$height" "400" "reload restores the bottom panel at the resized height"
tab=$(jsq '(() => {
  const pane = document.querySelector(".dock-pane.edge-bottom");
  return pane ? (pane.querySelector(".dock-tab")?.textContent.trim() ?? "") : "";
})()' 2>/dev/null | tr -d '"')
assert_eq "$tab" "Logs" "reload keeps logs the bottom panel's active tab"

# --- 4: close/reopen round-trip (closure survives reload) ---------------
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b => b.getAttribute("aria-label") === "Toggle bottom panel").click()' >/dev/null 2>&1
sleep 0.4
bottom=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0')
assert_eq "$bottom" "0" "the menubar toggle closes the bottom panel"
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
bottom=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0')
assert_eq "$bottom" "0" "the closed state survives a reload (BOTTOM_CLOSED_KEY)"
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b => b.getAttribute("aria-label") === "Toggle bottom panel").click()' >/dev/null 2>&1
sleep 0.6
bottom=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0')
assert_eq "$bottom" "1" "the menubar toggle reopens the bottom panel"
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom").getBoundingClientRect().height)')
assert_eq "$height" "400" "reopen restores the remembered height (400)"

# --- 5: mobile - the cell zero-collapses, state survives ----------------
ab set viewport 420 840 >/dev/null 2>&1
sleep 1.2
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom")?.getBoundingClientRect().height ?? -1)')
assert_eq "$height" "0" "mobile: the bottom cell zero-collapses (state survives in the tree)"
toggle=$(jsq '(() => {
  const b = Array.from(document.querySelectorAll("button.icon-btn.edge-toggle"))
    .find((b) => b.getAttribute("aria-label") === "Toggle bottom panel");
  return b ? getComputedStyle(b).display === "none" : true;
})()')
assert_eq "$toggle" "true" "mobile: the desktop bottom toggle is hidden"
ab set viewport 1280 800 >/dev/null 2>&1
sleep 1.2
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom")?.getBoundingClientRect().height ?? -1)')
assert_eq "$height" "400" "desktop again: the panel is back at its height (state survived mobile)"

# and the logs view stays reachable on mobile - the inspector drawer
# unions the right-domain set (logs' default home moved to the bottom)
ab set viewport 420 840 >/dev/null 2>&1
sleep 1
js 'Array.from(document.querySelectorAll("button.icon-btn.drawer-btn")).find(b => b.getAttribute("aria-label") === "Open the inspector drawer").click()' >/dev/null 2>&1
sleep 0.7
js 'Array.from(document.querySelectorAll(".mobile-drawer.right .mobile-drawer-tab")).find(t => t.textContent.trim() === "Logs")?.click()' >/dev/null 2>&1
sleep 3
logs=$(jsq 'document.querySelector(".mobile-drawer.right.open .logs-list")?1:0')
assert_eq "$logs" "1" "mobile: logs stays reachable in the inspector drawer (dual-host union)"

# clean exit: back at desktop, drawer closed, bottom panel still there
js 'document.querySelector(".scrim")?.click()' >/dev/null 2>&1
ab set viewport 1280 800 >/dev/null 2>&1
sleep 0.8
bottom=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0')
assert_eq "$bottom" "1" "final: the bottom panel is present at desktop"

# console check via lib.sh's shared detector; JOURNEY_DEV_LANE (a
# recipe-local shell var, unset again after) filters ONLY the nuxt DEV
# server's vite HMR websocket noise: this verify lane serves the dev
# bundle through the worktree proxy, which cannot ride the websocket
# upgrade. The embedded lane (CI, :8098) asserts genuine silence.
JOURNEY_DEV_LANE=1
assert_no_console_errors
# unset: run.sh SOURCES its recipes in one process - without this, the
# dev filter would leak into any recipe running after this one
unset JOURNEY_DEV_LANE