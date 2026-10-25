#!/bin/bash
# 77-inspector-narrow - the module-UI narrow recipe journey (t_a2fdc630).
#
# The recipe (ruling, 2026-10-21): every inspector view renders a
# desktop layout AND a narrow adaptation, measured against its OWN
# container - not the viewport. The kanban Board is the reference
# implementation; the sweep retrofitted cron, logs, skills, tools, the
# files tree, and settings rides card t_49d0b308's own adaptation.
# Logs left the inspector (bottom-hosted only, 3dbb9c5) - the logs
# checks moved out of this journey with it.
#
# Customer-visible invariants, asserted here per inspector tab:
#   narrow  -> the panel root carries the `narrow` class AND the class's
#              geometry actually applied (computed style, not class trust)
#   wide    -> the same root drops the class (a drag between hosts shows
#              ONE adaptation, never both at once)
#   flip    -> returning the pane narrow re-applies the adaptation (the
#              observer is live, not a mount-time snapshot)
#
# Readability rides the geometry asserts: stacked columns / condensed
# chrome / hidden size column are the narrow views themselves.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

# clean baseline: localStorage.clear() + reload restores the default dock
# tree (sidebar 240, main, inspector 240) - the inspector loads NARROW
# (240 < 360), which is exactly the state this journey asserts first.
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# the inspector pane: the right edge shell - the pinned pane whose tabbar
# hosts the kanban/settings tabs. JS finds it by tab set, not position
# arithmetic, so menu-bar tweaks upstream cannot mis-route this journey.
inspJS='(()=>{const ps=[...document.querySelectorAll(".dock-pane")];
 const pinned=ps.filter(p=>p.classList.contains("pinned")||p.querySelector(".dock-tabbar"));
 const hit=pinned.find(p=>[...p.querySelectorAll(".dock-tab-title")].some(t=>/kanban|settings/i.test(t.textContent||"")));
 return hit?1:0})()'
found=$(jsq "$inspJS")
assert_eq "$found" "1" "inspector pane found by its tab set"

# TABS: title-regex, panel-root selector, narrow computed-style check.
# The style checks pin the STACKED/CONDENSED shapes of the recipe's
# scoped narrow rules - if a refactor drops the rule while keeping the
# class, this goes red.
tab_narrow() {
  # $1 title-regex  $2 root selector  $3 style-expr on a descendant
  local out code
  out=$(jsq "(() => {
    const bar=[...document.querySelectorAll('.dock-pane')]
      .find(p=>[...p.querySelectorAll('.dock-tab-title')].some(t=>/$1/i.test(t.textContent||'')));
    if(!bar) return 0;
    const tab=[...bar.querySelectorAll('.dock-tab')].find(t=>/$1/i.test(t.querySelector('.dock-tab-title')?.textContent||''));
    if(!tab) return 0;
    tab.click(); return 2;
  })()")
  [ "$out" = "2" ] 2>/dev/null || die "tab /$1/ not found in the dock"
  sleep 2.5 # mount + mount-measure + one rAF delivery (+ nuxt dev first compile)
  code=$(jsq "(() => {
    const root=document.querySelector('$2');
    if(!root) return 0;
    if(!root.classList.contains('narrow')) return 1;
    try { if(!($3)) return 3; } catch (e) { return 4; }
    return 4;
  })()")
  case "$code" in
    4) step "ok: $1 narrow geometry applies";;
    3) die "$1: narrow class present but its geometry did NOT apply";;
    1) die "$1: panel root lost the narrow class at the default 240px inspector";;
    *) die "$1: panel root '$2' did not render";;
  esac
}

# --- phase 1: every inspector tab renders its narrow adaptation ---------
# style checks pick data-independent elements where the private lane has
# no rows (empty cron list, log tail): the condensed root padding is the
# narrow rule; a missing descendant (no data) cannot fail the tab.
tab_narrow 'cron'    '.cron-list'   'getComputedStyle(document.querySelector(".cron-list")).paddingLeft==="8px"'
tab_narrow 'skills'  '.skills-list' 'getComputedStyle(document.querySelector(".skills-list")).paddingLeft==="8px"'
tab_narrow 'tools'   '.tools-list'  'getComputedStyle(document.querySelector(".tools-list")).paddingLeft==="8px"'
tab_narrow 'files'   '.inspector-panel' 'getComputedStyle(document.querySelector(".files-tree .size")).display==="none"'
tab_narrow 'settings' 'aside.settings'  'getComputedStyle(document.querySelector(".settings-note")).fontSize==="10.5px"'
tab_narrow 'kanban'  '.board'       'getComputedStyle(document.querySelector(".board-scroll")).flexDirection==="column"'
step "ok: every inspector tab renders its measured narrow adaptation"

# --- phase 2: widen the inspector past the threshold - classes drop -----
# the divider is the inspector's LEFT border: dragging it RIGHT shrinks the
# pane to the collapse floor (the pane closes - observed on the live lane),
# dragging LEFT grows it. -160px: 240 -> 400 (>360, no 320-rung in the
# land zone). Steps negative and clipped to stay inside the viewport.
dragJS='(()=>{const ps=[...document.querySelectorAll(".dock-pane")];
 const right=ps.filter(p=>p.classList.contains("pinned")||p.querySelector(".dock-tabbar"));
 const insp=right.find(p=>[...p.querySelectorAll(".dock-tab-title")].some(t=>/kanban|settings/i.test(t.textContent||"")));
 if(!insp) return -1;
 const r=insp.getBoundingClientRect();
 const divs=[...document.querySelectorAll(".dock-divider")];
 const div=divs.find(d=>Math.abs(d.getBoundingClientRect().x+2-r.x)<10);
 return div?Math.round(div.getBoundingClientRect().x)+2:-1})()'
divx=$(jsq "$dragJS")
[ "$divx" != "-1" ] || die "no divider found left of the inspector pane"
ab mouse move "$divx" 400 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
x=-10; while [ "$x" -ge -160 ]; do ab mouse move "$((divx+x))" 400 >/dev/null 2>&1; sleep 0.03; x=$((x-10)); done
ab mouse up >/dev/null 2>&1
sleep 0.6

# the drag may re-key the pane tree; re-find the inspector by its tab set
wide=$(jsq '(()=>{const ps=[...document.querySelectorAll(".dock-pane")];
 const insp=ps.find(p=>[...p.querySelectorAll(".dock-tab-title")].some(t=>/kanban|settings/i.test(t.textContent||"")));
 return insp?Math.round(insp.getBoundingClientRect().width):-1})()')
[ "$wide" -ge 380 ] 2>/dev/null || die "inspector did not widen (width $wide)"
step "ok: inspector widens to ${wide}px"
sleep 1 # settle: the drag re-keys the pane; let the tabbar re-render

# click through every tab: only the active tab keeps an outlet mounted.
wide_drop() {
  out=$(jsq "(() => {
    const panes=[...document.querySelectorAll('.dock-pane')];
    const bar=panes.find(p=>[...p.querySelectorAll('.dock-tab-title')].some(t=>/$1/i.test(t.textContent||'')));
    if(!bar) return 0;
    const tab=[...bar.querySelectorAll('.dock-tab')].find(t=>/$1/i.test(t.querySelector('.dock-tab-title')?.textContent||''));
    if(!tab) return 0;
    try { tab.click(); } catch (e) { return 'ERR:'+e.message; }
    return 'clicked';
  })()")
  out=${out//\"/} # jsq returns JSON quotes around string results
  [ "$out" = "clicked" ] 2>/dev/null || die "tab /$1/ wide-pass click failed (out=[${out:-EMPTY}])"
  sleep 2.5 # mount at the wide width (+ nuxt dev first compile)
  out=$(jsq "(() => {
    const root=document.querySelector('$2');
    return root&&!root.classList.contains('narrow')?4:(root?3:0);
  })()")
  [ "$out" = "4" ] 2>/dev/null || die "$1: narrow class did not drop at ${wide}px"
}

wide_drop 'files'   '.inspector-panel'
wide_drop 'cron'    '.cron-list'
wide_drop 'skills'  '.skills-list'
wide_drop 'tools'   '.tools-list'
wide_drop 'settings' 'aside.settings'
wide_drop 'kanban'  '.board'
step "ok: every widened inspector tab drops the narrow class"

# --- phase 3: the flip is live - narrow again re-applies ----------------
ab mouse move "$((divx-160))" 400 >/dev/null 2>&1
ab mouse down >/dev/null 2>&1
x=-150; while [ "$x" -le 0 ]; do ab mouse move "$((divx+x))" 400 >/dev/null 2>&1; sleep 0.03; x=$((x+10)); done
ab mouse up >/dev/null 2>&1
sleep 0.6
tab_narrow 'cron' '.cron-list' 'getComputedStyle(document.querySelector(".cron-list")).paddingLeft==="8px"'
step "ok: dragging back narrow re-applies the adaptation (live observer)"