#!/bin/bash
# 75-divider-resize - the primary sidebar divider drags and the sidebar
# resizes, snapping at the +320 ladder (320/640/960/...). Regression guard
# for the v0.9.0 drag rework that crossed the cumulative/per-event delta
# contract (card t_38c0e960): the divider looked dead - ~10px of movement
# for a full drag, every move resetting the pane to baseline plus one
# increment. Also re-asserts the tab-DnD sessions lock still holds
# alongside the divider gesture (both share the pointer surface).
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1

# clean baseline: the default dock tree (sidebar 240, main, inspector 240)
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# the primary divider sits between the left sidebar and the main pane. A
# +90px drag lands the sidebar at 330 - inside the 320 rung's 15px attract
# zone (305..335) - so it must SNAP to 320.
divx=$(jsq '(()=>{const r=document.querySelector(".dock-divider").getBoundingClientRect();return Math.round(r.x)+2})()')
[ -n "$divx" ] || die "no dock-divider found"
ab mouse move "$divx" 400 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
x=10; while [ "$x" -le 90 ]; do ab mouse move "$((divx+x))" 400 >/dev/null 2>&1; sleep 0.04; x=$((x+10)); done
ab mouse up >/dev/null 2>&1
sleep 0.3
sidebar=$(jsq '(()=>{const p=document.querySelector("[data-pane-id]");return Math.round(p.getBoundingClientRect().width)})()')
assert_eq "$sidebar" "320" "primary divider drag snaps the sidebar to 320"

# the divider is alive (not just the magnet): a free drag outside any snap
# zone tracks the pointer - 240 -> 440 (no rung within 15px)
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
divx=$(jsq '(()=>{const r=document.querySelector(".dock-divider").getBoundingClientRect();return Math.round(r.x)+2})()')
ab mouse move "$divx" 400 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
x=10; while [ "$x" -le 200 ]; do ab mouse move "$((divx+x))" 400 >/dev/null 2>&1; sleep 0.03; x=$((x+10)); done
ab mouse up >/dev/null 2>&1
sleep 0.3
sidebar=$(jsq '(()=>{const p=document.querySelector("[data-pane-id]");return Math.round(p.getBoundingClientRect().width)})()')
assert_eq "$sidebar" "440" "primary divider tracks the pointer outside snap zones"

# the tab-DnD sessions lock still holds under a synthetic drag (the
# divider and tab gestures share the pointer surface - card t_38c0e960)
before=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
js '(()=>{const dt=new DataTransfer();const pane=document.querySelector("[data-pane-id]");const tab=pane&&pane.querySelector(".dock-tab");if(!tab)return;const target=pane.querySelector(".dock-tab");tab.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));if(target){target.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:10,clientY:200}));target.dispatchEvent(new DragEvent("drop",{dataTransfer:dt,bubbles:true,clientX:10,clientY:200}));}tab.dispatchEvent(new DragEvent("dragend",{dataTransfer:dt,bubbles:true}));})()' >/dev/null 2>&1 || true
sleep 1
after=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
alive=$(jsq 'document.querySelector(".sessions .session-list")?1:0')
assert_eq "$alive" "1" "session list alive after tab DnD attempt"
assert_eq "$after" "$before" "pane count stable under tab DnD attempt"
step "ok: tab DnD sessions lock holds alongside divider drag"

assert_no_console_errors