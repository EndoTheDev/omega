#!/bin/bash
# 78-bottom-panel - the bottom panel journey (t_67fc1905).
# The horizontal pane pinned under the main panel, logs home:
#   1  fresh load: the bottom shell renders (edge-bottom pane, 200 default
#      rung) with the logs tab live and polling - and sits UNDER the main
#      panel (geometry, not just presence)
#   2  divider drag grows the bottom panel to the 50% cap (400 rung unreachable
#      below half the content box - v0.14.4 card t_e539ba29 law, merge e36e555)
#   3  reload persists the resized tree
#   4  close/reopen: the menubar toggle round-trips the panel; closure
#      SURVIVES a reload (the record's closed-edge set, card t_aa9dbada),
#      the reopen restores the remembered height
#   5  mobile (<768px): the bottom cell zero-collapses (state survives),
#      the desktop toggle STAYS VISIBLE at every breakpoint (the parity
#      contract, 16ac10a - the toggle drives dock.toggleEdge directly
#      below 768px too), and the logs view stays reachable on mobile
#      the bottom-only way (logs is bottom-hosted, 3dbb9c5)
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

# --- 2: divider drag (grows to the 50% cap; the 400 rung sits below it) ---
# t_be305778: the shell anchors to the viewport bottom - drag UP GROWS it
# (the divider is its top edge); the drag-down variant was the inverted
# gesture this card fixed.
# t_e539ba29 (v0.14.4 law): BOTTOM_MAX_FRACTION 0.65 -> 0.5, so at this
# journey's fixed 1280x800 viewport the cap is 364 (half of the 728px
# content box) and the +200 rung at 400 is unreachable - keep the 364 in
# sync if header/statusbar chrome heights change.
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
d=30; while [ "$d" -le 200 ]; do ab mouse move "$divx" $((divy - d)) >/dev/null 2>&1; sleep 0.03; d=$((d + 30)); done
ab mouse move "$divx" $((divy - 200)) >/dev/null 2>&1
ab mouse up >/dev/null 2>&1
sleep 0.4
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom").getBoundingClientRect().height)')
assert_eq "$height" "364" "divider drag grows the bottom panel to the 50% cap (364 at 1280x800; the 400 rung is unreachable below it)"

# --- 3: the resized tree survives a reload ------------------------------
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom")?.getBoundingClientRect().height ?? 0)')
assert_eq "$height" "364" "reload restores the bottom panel at the capped height"
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
assert_eq "$bottom" "0" "the closed state survives a reload (closed-edge set)"
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b => b.getAttribute("aria-label") === "Toggle bottom panel").click()' >/dev/null 2>&1
sleep 0.6
bottom=$(jsq 'document.querySelector(".dock-pane.edge-bottom")?1:0')
assert_eq "$bottom" "1" "the menubar toggle reopens the bottom panel"
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom").getBoundingClientRect().height)')
assert_eq "$height" "364" "reopen restores the remembered height (364, the cap)"

# --- 5: mobile - the cell zero-collapses, state survives ----------------
ab set viewport 420 840 >/dev/null 2>&1
sleep 1.2
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom")?.getBoundingClientRect().height ?? -1)')
assert_eq "$height" "0" "mobile: the bottom cell zero-collapses (state survives in the tree)"
# 16ac10a parity contract: the bottom toggle stays visible at every
# breakpoint - card t_4bf1d5b7: below 768px it drives the mobile BOTTOM
# DRAWER (the zero-collapse cell can never render the tree pane, so the
# desktop reveal would be a dead click: Endo's report 2026-10-29)
toggle=$(jsq '(() => {
  const b = Array.from(document.querySelectorAll("button.icon-btn.edge-toggle"))
    .find((b) => b.getAttribute("aria-label") === "Toggle bottom panel");
  return b && getComputedStyle(b).display !== "none" ? true : false;
})()')
assert_eq "$toggle" "true" "mobile: the bottom-panel toggle is visible under 768px (parity contract, 16ac10a)"
ab set viewport 1280 800 >/dev/null 2>&1
sleep 1.2
height=$(jsq 'Math.round(document.querySelector(".dock-pane.edge-bottom")?.getBoundingClientRect().height ?? -1)')
assert_eq "$height" "364" "desktop again: the panel is back at the capped height (state survived mobile)"

# logs stays reachable on mobile the bottom-only way (Endo's ruling,
# 3dbb9c5: logs is bottom-hosted only) - card t_4bf1d5b7: the path is
# the menubar BOTTOM toggle -> the bottom drawer (side="bottom"), the
# same fixed-drawer + scrim rule the inspector drawer rides. The drawer
# seats the logs view and streams the tail; the tree is never touched.
ab set viewport 420 840 >/dev/null 2>&1
sleep 1
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b => b.getAttribute("aria-label") === "Toggle bottom panel")?.click()' >/dev/null 2>&1
sleep 0.7
bopen=$(jsq 'document.querySelector(".mobile-drawer.bottom.open")?1:0')
bscrim=$(jsq 'document.querySelector(".scrim")?1:0')
assert_eq "$bopen" "1" "mobile: the bottom toggle opens the bottom drawer (4bf1d5b7)"
assert_eq "$bscrim" "1" "mobile: the bottom drawer renders over the scrim"
poll_js 'document.querySelectorAll(".mobile-drawer.bottom.open .logs-line").length >= 1' 12 \
  || die "mobile: the bottom drawer seats no logs tail (logs unreachable on mobile)"
step "ok: mobile bottom drawer seats the live logs tail"
# the right drawer never seats logs (3dbb9c5 stands) - chip names now
# ride aria-label (icon-only chips, card t_4bf1d5b7)
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b => b.getAttribute("aria-label") === "Toggle right sidebar")?.click()' >/dev/null 2>&1
sleep 0.7
drawerlogs=$(jsq 'Array.from(document.querySelectorAll(".mobile-drawer.right.open .mobile-drawer-tab")).filter(t => (t.getAttribute("aria-label")||"").trim() === "Logs").length')
assert_eq "$drawerlogs" "0" "mobile: the right drawer holds no Logs tab (logs is bottom-only, 3dbb9c5)"

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