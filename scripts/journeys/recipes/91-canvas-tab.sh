#!/bin/bash
# 91-canvas-tab - the canvas host tab journey (arc 3c, t_516d7494).
# The generic panel renderer is shell-owned LAYOUT: the tab strip carries
# the canvas host tab, its content comes from the canvas store (GET
# /ui/canvas snapshot at boot), never a hardcoded list (Q5a law).
# RE-ANCHORED after arcs 4b+5 (2026-10-24): modules mount panels now, so
# the EMPTY-state pin is obsolete - and it false-passed on a dead lane
# (a failed snapshot fetch also renders the empty paragraph, so the old
# assertion was green while the lane was broken, proven live). The
# journey now pins the MOUNT CONTRACT: the DOM renders exactly what the
# snapshot carries - one .canvas-panel section per snapshot panel, the
# empty paragraph only when the snapshot is []. Works on a demoduled
# build (zero panels) and on the full build (4 panels) alike. The wire
# contracts (snapshot 200, action-pipe 404 sentinels, delta frames) are
# pinned by modules/http_channel/server_canvas_test.go; what only a
# browser catches is the mount: a canvas tab that fails to render, a
# store ReferenceError in setup, a pane that never opens. Catches the
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

# the pane settled on ONE state: panel sections or the empty paragraph
# (a dead mount or a setup ReferenceError renders neither)
wait_js '(()=>{const p=document.querySelectorAll(".canvas-panel").length; const e=((document.body.innerText)||"").includes("no panels mounted"); return p>0||e})()' 15
step "ok: canvas pane rendered its state"

# the mount contract: DOM panel count == snapshot panel count, and the
# empty paragraph shows only when the snapshot is empty. The fetch is
# fired into a window var and polled - eval may not await promises.
jsq 'fetch("/ui/canvas").then(r=>r.json()).then(a=>{window.__snapN=a.length}).catch(()=>{window.__snapN=-1}); 1' >/dev/null 2>&1
wait_js 'window.__snapN!==undefined' 10
snap=$(jsq 'window.__snapN')
dom=$(jsq 'document.querySelectorAll(".canvas-panel").length')
[ "$snap" -ge 0 ] 2>/dev/null || die "snapshot fetch failed in the journey (snap=$snap) - lane trouble, not a product verdict"
assert_eq "$dom" "$snap" "canvas DOM matches the snapshot (dom=$dom snap=$snap)"
empty=$(jsq 'document.querySelectorAll(".canvas-empty").length')
if [ "$snap" -gt 0 ]; then
  assert_eq "$empty" "0" "empty paragraph hidden while panels render"
else
  assert_eq "$empty" "1" "empty paragraph renders on an empty snapshot"
fi
step "ok: canvas mount contract holds (dom=$dom snap=$snap)"

step "done: canvas host tab mounts and renders the snapshot state"