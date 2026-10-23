#!/bin/bash
# 91-canvas-tab - the canvas host tab journey (arc 3c, t_516d7494).
# The generic panel renderer is shell-owned LAYOUT: the tab strip carries
# the canvas host tab, its content comes from the canvas store (GET
# /ui/canvas snapshot at boot), never a hardcoded list (Q5a law). No
# module mounts panels until arc 4b, so this journey pins the EMPTY
# canvas state - the state every fresh boot renders. The wire contracts
# (snapshot 200 [], action-pipe 404 sentinels, delta frames) are pinned
# by modules/http_channel/server_canvas_test.go; what only a browser
# catches is the mount: a canvas tab that fails to render, a store
# ReferenceError in setup, a pane that never opens. Catches the
# invisible-to-gates family (the insights tab once shipped unreachable
# with every gate green).
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# the registry voice: the tab strip carries Canvas (capital C)
tab=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].map(e=>(e.textContent||"").trim()); return t.includes("Canvas")?1:0})()')
assert_eq "$tab" "1" "canvas tab present (capital C)"

# click the tab: the singleton focuses its pane
clicked=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].find(e=>(e.textContent||"").trim()==="Canvas"); if(!t) return 0; t.click(); return 1})()')
assert_eq "$clicked" "1" "canvas tab clicked"

# the empty canvas state renders - the store's zero-panel view after the
# boot snapshot (a dead mount or a setup ReferenceError leaves it absent)
wait_js '(()=>{return ((document.body.innerText)||"").includes("no panels mounted")})()' 15
step "ok: canvas empty state renders (no panels mounted)"

# no panel descriptors leaked into the DOM: the host pane renders the
# empty paragraph only, zero .canvas-panel sections
n=$(jsq 'document.querySelectorAll(".canvas-panel").length')
assert_eq "$n" "0" "no phantom panel sections render"

step "done: canvas host tab mounts and renders the empty canvas state"