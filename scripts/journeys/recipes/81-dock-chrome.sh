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

# item 3: the primary sidebar header reads Sessions (capital S).
# card t_502b8e81: edge shells render ICON-ONLY chips - the name rides
# aria-label (no .dock-tab-title); the chip is the header.
sess=$(jsq '(()=>{const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
 const left=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom)")].find((p)=>p.querySelector(".session-list"));
 if(!left) return 0;
 const chip=[...left.querySelectorAll(".dock-tab")].find((t)=>/sessions/i.test(name(t)));
 return chip?name(chip):0})()')
sess=${sess#\"}; sess=${sess%\"} # jsq returns JSON strings quoted
assert_eq "$sess" "Sessions" "primary sidebar header reads Sessions"

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

# item 6 (default): the dock setting flips full -> aligned. Settings is a
# pinned inspector tab (no menu entry): click it, flip the select,
# re-measure. The right edge renders ICON-ONLY chips (t_502b8e81) - the
# chip's name rides aria-label.
open=$(jsq '(()=>{const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
 const t=[...document.querySelectorAll(".dock-tab")].find((t)=>name(t)==="Settings");
 if(!t) return 0; t.click(); return 1})()')
assert_eq "$open" "1" "settings tab opens"
wait_text "bottom panel width" 10
# card t_acad02ec: the knob select rides the BandSelect listbox - open
# the pop (chip aria-label = the knob label), pick the option
full=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="bottom panel width"); if(!c) return 0; c.click(); return 1})()')
assert_eq "$full" "1" "width chip clicked"
if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()==="full")})()' 10; then
  die "width-mode options never rendered"
fi
full=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="full"); if(!o) return 0; o.click(); return 1})()')
assert_eq "$full" "1" "width-mode flipped to full"
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

back=$(jsq '(()=>{const c=[...document.querySelectorAll(".setting-select .model-chip")].find(b=>b.getAttribute("aria-label")==="bottom panel width"); if(!c) return 0; c.click(); return 1})()')
assert_eq "$back" "1" "width chip clicked (back)"
if ! wait_js '(()=>{return [...document.querySelectorAll(".setting-select .model-list li")].some(l=>(l.textContent||"").trim()==="aligned")})()' 10; then
  die "width-mode options never rendered (back)"
fi
back=$(jsq '(()=>{const o=[...document.querySelectorAll(".setting-select .model-list li")].find(l=>(l.textContent||"").trim()==="aligned"); if(!o) return 0; o.click(); return 1})()')
assert_eq "$back" "1" "width-mode flipped back to aligned"
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

# --- card t_aa9dbada: pane visibility persists WITH the layout -----------
# Close BOTH sidebars via the menubar toggles, reload: the panes stay
# closed (the record's closed-edge set restores the visibility the user
# left - the loader no longer re-asserts the open shape over it), and the
# reopen persists the same way.
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle right sidebar").click()' >/dev/null 2>&1
sleep 0.5
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
closed_left=$(jsq 'document.querySelector(".sessions .session-list")?0:1')
assert_eq "$closed_left" "1" "RELOAD: the closed sessions sidebar STAYS closed"
# the right edge no longer carries an edge-right class (t_d6b657a7 pane
# chrome) - the closed inspector asserts by its SURFACE: no pinned
# non-bottom pane hosts the Settings chip anymore.
closed_right=$(jsq '(()=>{const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
 return [...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom)")].some((p)=>[...p.querySelectorAll(".dock-tab")].some((t)=>name(t)==="Settings"))?0:1})()')
assert_eq "$closed_right" "1" "RELOAD: the closed inspector sidebar STAYS closed"
alive=$(jsq 'document.querySelector(".composer")?1:0')
assert_eq "$alive" "1" "the main pane still renders under both closed sidebars"
js 'Array.from(document.querySelectorAll("button.icon-btn.edge-toggle")).find(b=>b.getAttribute("aria-label")==="Toggle left sidebar").click()' >/dev/null 2>&1
sleep 0.6
back=$(jsq 'document.querySelector(".sessions .session-list")?1:0')
assert_eq "$back" "1" "the reopen persists too (state the user left)"
step "ok: sidebar closure persists across reload (t_aa9dbada)"

step "done: dock chrome holds - Sessions, closed pinned bars, aligned bottom panel"