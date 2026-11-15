#!/bin/bash
# 75-divider-resize - the primary sidebar divider drags and the sidebar
# resizes, snapping at the +320 ladder (320/640/960/...). Regression guard
# for the v0.9.0 drag rework that crossed the cumulative/per-event delta
# contract (card t_38c0e960): the divider looked dead - ~10px of movement
# for a full drag, every move resetting the pane to baseline plus one
# increment. Also re-asserts the tab-DnD sessions lock still holds
# alongside the divider gesture (both share the pointer surface).
#
# The baseline is MEASURED, never pinned (card t_b7447ae3). A fresh boot
# sizes the edge panes from EDGE_DEFAULT_PX in webui/app/composables/dock.ts,
# and that default moved (240 -> 264, t_d4443ab5 "wider sidebars"): a pinned
# "start at 240, drag +90 into the 320 zone" became 264+90=354, so the
# journey redded on arithmetic and stopped guarding anything. Every drag and
# every expected width below derives from the width the page actually booted
# with.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1

# clean baseline: the default dock tree (sidebar, main, inspector)
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# jsq hands numbers back unquoted, but strip quotes defensively: a CLI that
# stringifies must not break the integer compares below.
sidebar_px() {
  jsq '(()=>{const p=document.querySelector("[data-pane-id]");return p?Math.round(p.getBoundingClientRect().width):-1})()' 2>/dev/null | tail -1 | tr -d '"'
}
abs() { if [ "$1" -lt 0 ]; then echo $(( -$1 )); else echo "$1"; fi; }

base=$(sidebar_px)
[ "$base" -ge 128 ] 2>/dev/null || die "fresh-boot sidebar did not measure (got [$base])"
step "baseline: fresh-boot sidebar ${base}px"

# the rung the snap drag aims at: the first rung of the +320 ladder above the
# baseline's attract zone (SNAP_STEP 320 / SNAP_ZONE 15 in
# webui/app/composables/panes.ts - each rung attracts within +/-15px)
rung=$(( (base / 320 + 1) * 320 ))
# land a few px PAST the rung on the drag's 10px step grid: inside the zone,
# so the release must read the exact rung and not the pointer width
snap_drag=$(( (rung - base + 4) / 10 * 10 ))
snap_px=$(( base + snap_drag ))
[ "$(abs $(( snap_px - rung )))" -le 15 ] \
  || die "derived snap drag misses the ${rung} attract zone (${base}+${snap_drag}=${snap_px})"
step "snap probe: +${snap_drag} -> ${snap_px}px (zone $((rung - 15))..$((rung + 15)), rung ${rung})"

# the primary divider sits between the left sidebar and the main pane
divx=$(jsq '(()=>{const r=document.querySelector(".dock-divider").getBoundingClientRect();return Math.round(r.x)+2})()')
[ -n "$divx" ] || die "no dock-divider found"
ab mouse move "$divx" 400 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
x=10; while [ "$x" -le "$snap_drag" ]; do ab mouse move "$((divx+x))" 400 >/dev/null 2>&1; sleep 0.04; x=$((x+10)); done
ab mouse up >/dev/null 2>&1
sleep 0.3
sidebar=$(sidebar_px)
assert_eq "$sidebar" "$rung" "primary divider drag snaps the sidebar to the ${rung} rung"

# the divider is alive (not just the magnet): a free drag outside any snap
# zone tracks the pointer - the dead-divider shape moved ~10px per drag
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
base=$(sidebar_px)
[ "$base" -ge 128 ] 2>/dev/null || die "sidebar did not measure after the reload (got [$base])"
divx=$(jsq '(()=>{const r=document.querySelector(".dock-divider").getBoundingClientRect();return Math.round(r.x)+2})()')
[ -n "$divx" ] || die "no dock-divider found after the reload"

# 100px past the aimed rung: clear of that zone and well short of the next
free_px=$(( rung + 100 ))
off=$(( free_px % 320 )); [ "$off" -gt 160 ] && off=$(( 320 - off ))
[ "$off" -gt 15 ] || die "derived free-drag landing ${free_px} sits in a snap zone (offset ${off})"
free_drag=$(( (free_px - base) / 10 * 10 ))
free_px=$(( base + free_drag ))
off=$(( free_px % 320 )); [ "$off" -gt 160 ] && off=$(( 320 - off ))
[ "$off" -gt 15 ] || die "derived free-drag landing ${free_px} sits in a snap zone (offset ${off})"
step "free probe: +${free_drag} -> ${free_px}px (no rung within 15)"

ab mouse move "$divx" 400 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
x=10; while [ "$x" -le "$free_drag" ]; do ab mouse move "$((divx+x))" 400 >/dev/null 2>&1; sleep 0.03; x=$((x+10)); done
ab mouse up >/dev/null 2>&1
sleep 0.3
sidebar=$(sidebar_px)
assert_eq "$sidebar" "$free_px" "primary divider tracks the pointer outside snap zones"

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
