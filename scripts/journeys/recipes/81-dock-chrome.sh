#!/bin/bash
# 81-dock-chrome - the dock chrome journey (t_d7fd3ad3, Endo's eyeball
# round 2). Asserts the closed-chrome rulings live in the browser - the
# family the unit gates cannot see (rendered buttons, measured boxes):
#
#   sessions  - the primary sidebar's tab reads "Sessions" (capital S).
#   no-plus   - the pinned shells (both sidebars AND the bottom panel)
#               carry NO '+' on their tab bars; the main pane keeps one.
#   aligned   - the bottom panel's width matches the main panes' box
#               (the default width mode, the VS Code panel read).
#   full      - switching the dock setting to "full" spans the dock area,
#               and switching back to "aligned" re-aligns it.
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

# item 3: the primary sidebar header reads Sessions (capital S)
sess=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].map(e=>(e.textContent||"").trim());
 return t.includes("Sessions")&&!t.includes("sessions")?1:0})()')
assert_eq "$sess" "1" "primary sidebar header reads Sessions"

# items 2+5: the pinned shells carry no '+'; the main pane keeps one
plus=$(jsq '(()=>{const pinned=[...document.querySelectorAll(".dock-pane.pinned")];
 const main=[...document.querySelectorAll(".dock-pane:not(.pinned)")];
 if(pinned.length<3) return -1; /* left, right, bottom expected */
 const stray=pinned.filter(p=>p.querySelector("button.dock-tab-new")).length;
 const kept=main.filter(p=>p.querySelector("button.dock-tab-new")).length;
 return stray===0&&kept>=1?1:0})()')
assert_eq "$plus" "1" "no '+' on the pinned bars; the main pane keeps its '+'"

# item 6 (default): the bottom panel aligns to the main panes' box
aligned=$(jsq '(()=>{const b=document.querySelector(".dock-pane.edge-bottom");
 if(!b) return -1; const r=b.getBoundingClientRect();
 const ms=[...document.querySelectorAll(".dock-pane:not(.pinned)")].map(p=>p.getBoundingClientRect());
 if(!ms.length) return -1;
 const L=Math.min(...ms.map(x=>x.left)), R=Math.max(...ms.map(x=>x.right));
 return Math.abs(r.left-L)<=1&&Math.abs(r.right-R)<=1?1:0})()')
assert_eq "$aligned" "1" "bottom panel aligned to the main panes (default mode)"

# item 6: the dock setting flips full -> aligned. Settings is a pinned
# inspector tab (no menu entry): click it, flip the select, re-measure.
open=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].find(e=>(e.textContent||"").trim()==="Settings");
 if(!t) return 0; t.click(); return 1})()')
assert_eq "$open" "1" "settings tab opens"
wait_text "bottom panel width" 10
full=$(jsq '(()=>{const s=[...document.querySelectorAll("select")].find(e=>(e.textContent||"").includes("full"));
 if(!s) return 0; s.value="full"; s.dispatchEvent(new Event("change",{bubbles:true})); return 1})()')
assert_eq "$full" "1" "width-mode select flipped to full"
i=0; ok=0
while [ "$i" -lt 20 ]; do
  got=$(jsq '(()=>{const b=document.querySelector(".dock-pane.edge-bottom");
   if(!b) return 0; const a=b.parentElement.getBoundingClientRect(); const r=b.getBoundingClientRect();
   return Math.abs(r.left-a.left)<=1&&Math.abs(r.right-a.right)<=1?1:0})()')
  if [ "$got" = "1" ]; then ok=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$ok" = "1" ] || die "full mode never spanned the dock area (last: [$got])"
step "ok: full mode spans the dock area"

back=$(jsq '(()=>{const s=[...document.querySelectorAll("select")].find(e=>(e.textContent||"").includes("full"));
 if(!s) return 0; s.value="aligned"; s.dispatchEvent(new Event("change",{bubbles:true})); return 1})()')
assert_eq "$back" "1" "width-mode select flipped back to aligned"
i=0; ok=0
while [ "$i" -lt 20 ]; do
  got=$(jsq '(()=>{const b=document.querySelector(".dock-pane.edge-bottom");
   if(!b) return 0; const r=b.getBoundingClientRect();
   const ms=[...document.querySelectorAll(".dock-pane:not(.pinned)")].map(p=>p.getBoundingClientRect());
   if(!ms.length) return 0;
   const L=Math.min(...ms.map(x=>x.left)), R=Math.max(...ms.map(x=>x.right));
   return Math.abs(r.left-L)<=1&&Math.abs(r.right-R)<=1?1:0})()')
  if [ "$got" = "1" ]; then ok=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$ok" = "1" ] || die "aligned mode never restored (last: [$got])"
step "ok: aligned mode restored"

step "done: dock chrome holds - Sessions, closed pinned bars, aligned bottom panel"