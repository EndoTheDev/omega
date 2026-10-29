#!/bin/bash
# 70-dock-panes-dnd - the dock surface: panes render, the tab strip
# shows the seven fixed inspector tabs, and the session lock holds - a
# synthetic HTML5 drag attempt must not break the shell or relocate the
# pinned sessions singleton. (Positive drag reordering needs two
# panes; it rides the dockdnd arc once pane splitting is journeyed.)
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab wait --load networkidle >/dev/null 2>&1 || true

# panes render: pane elements with ids, tab strips inside them
panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
tabs=$(jsq 'document.querySelectorAll(".dock-tab").length' 2>&1)
[ "$panes" -ge 1 ] 2>/dev/null || die "no dock panes render ($panes)"
step "ok: panes render ($panes panes, $tabs dock tabs)"

# the inspector exposes the registry's fixed tabs (count the misses).
# card t_502b8e81: pinned shells render ICON-ONLY chips - the name rides
# aria-label, never .dock-tab-title text. Logs left the inspector
# (bottom-hosted, 3dbb9c5); the modules registry joined Agents/Modules/
# Insights - the strip IS the registry list the drawer derives from.
missing=$(jsq '["Files","Cron","Skills","Tools","Agents","Continuous","Settings","Kanban","Modules","Insights"].filter(t => ![...document.querySelectorAll(".dock-pane.pinned .dock-tab")].some(b => ((b.getAttribute("aria-label")||"")||b.title).trim() === t)).length' 2>&1)
assert_eq "$missing" "0" "every registry inspector tab renders with its name"
# the chip law itself: no pinned-shell tab may render text-only chrome
titled=$(jsq 'document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom):not(.edge-left) .dock-tab .dock-tab-title").length' 2>&1)
[ "$titled" = "0" ] 2>/dev/null || die "an edge shell renders a text tab where the chip law says icon-only ($titled)"

# the Files tab opens real pane content (click the chip by its aria name;
# the tree body asserts the pane, not the tab's text - icon chips carry
# none)
clicked=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Files");if(!t) return 0;t.click();return 1})()' 2>&1)
assert_eq "$clicked" "1" "Files tab clickable by its name"
sleep 1
files=$(jsq 'document.querySelector(".inspector-files")?1:0' 2>&1)
assert_eq "$files" "1" "Files tab renders the file tree"

# the sessions lock: a synthetic HTML5 drag attempt (dragstart/dragover/
# drop between the sidebar pane's tab and the pane tabbar) must not tear
# the shell: pane count stable, session list alive afterwards
before=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
js '(()=>{const dt = new DataTransfer(); const pane = document.querySelector("[data-pane-id]"); const tab = pane && pane.querySelector(".dock-tab"); if (!tab) return; const target = document.querySelector("[data-pane-id]") && document.querySelector("[data-pane-id]").querySelector(".dock-tab"); tab.dispatchEvent(new DragEvent("dragstart", {dataTransfer: dt, bubbles: true})); if (target) { target.dispatchEvent(new DragEvent("dragover", {dataTransfer: dt, bubbles: true, clientX: 10, clientY: 200})); target.dispatchEvent(new DragEvent("drop", {dataTransfer: dt, bubbles: true, clientX: 10, clientY: 200})); } tab.dispatchEvent(new DragEvent("dragend", {dataTransfer: dt, bubbles: true}));})()' >/dev/null 2>&1 || true
sleep 1
after=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
alive=$(jsq 'document.querySelector(".sessions .session-list") ? 1 : 0' 2>&1)
assert_eq "$alive" "1" "session list alive after drag attempt"
assert_eq "$after" "$before" "pane count stable under drag attempt"
step "ok: sessions lock held under synthetic tab DnD"

# --- card t_aa9dbada: the last tab out CLOSES the pane -------------------
# The real crossing gestures, synthesized like the lock check above:
#   A  a zone-drop of the Files split the MAIN pane around a fresh pane -
#      the fresh pane hosts ONLY Files
#   B  dragging that single tab back into the inspector bar empties the
#      source pane, and the pane CLOSES with it (the manual remove
#      button's result, automatic) - no empty pane, no remove button left.
before=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
split=$(jsq '(() => {
  const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
  const tab=[...document.querySelectorAll(".dock-tab")]
    .find(t=>name(t)==="Files");
  if (!tab) return "no-files-tab";
  const dt=new DataTransfer();
  tab.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));
  const main=[...document.querySelectorAll(".dock-pane")].find(p=>p.querySelector(".composer"));
  if (!main) return "no-main";
  const r=main.getBoundingClientRect();
  const cx=r.left+r.width*0.15, cy=r.top+r.height*0.6;
  main.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:cx,clientY:cy}));
  main.dispatchEvent(new DragEvent("drop",{dataTransfer:dt,bubbles:true,clientX:cx,clientY:cy}));
  tab.dispatchEvent(new DragEvent("dragend",{dataTransfer:dt,bubbles:true}));
  return "split";
})()' 2>&1)
assert_eq "$split" "\"split\"" "a zone-drop splits the main pane around Files"
sleep 1
mid=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
[ "$mid" = "$((before+1))" ] || die "the zone split did not mint a pane (before $before, mid $mid)"
fresh=$(jsq '[...document.querySelectorAll("[data-pane-id]")].filter(p=>!p.classList.contains("pinned")&&!p.querySelector(".composer")&&p.querySelectorAll(".dock-tab").length===1&&((p.querySelector(".dock-tab-title")?.textContent||"").trim()==="Files")).length' 2>&1)
assert_eq "$fresh" "1" "the fresh pane hosts exactly one tab: Files"
trip=$(jsq '(() => {
  const pane=[...document.querySelectorAll("[data-pane-id]")]
    .find(p=>!p.classList.contains("pinned")&&!p.querySelector(".composer")&&p.querySelectorAll(".dock-tab").length===1&&((p.querySelector(".dock-tab-title")?.textContent||"").trim()==="Files"));
  if (!pane) return "no-fresh-pane";
  const tab=pane.querySelector(".dock-tab");
  const dt=new DataTransfer();
  tab.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));
  const bar=[...document.querySelectorAll(".dock-pane.pinned")]
    .find(p=>!p.classList.contains("edge-bottom")&&!p.querySelector(".session-list"))
    ?.querySelector(".dock-tabs .dock-tab");
  if (!bar) return "no-inspector-tab";
  const r=bar.getBoundingClientRect();
  const cx=r.left+r.width*0.5, cy=Math.max(r.top+10, r.bottom-2);
  pane.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:cx,clientY:cy}));
  bar.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:cx,clientY:cy}));
  bar.dispatchEvent(new DragEvent("drop",{dataTransfer:dt,bubbles:true,clientX:cx,clientY:cy}));
  tab.dispatchEvent(new DragEvent("dragend",{dataTransfer:dt,bubbles:true}));
  return "moved";
})()' 2>&1)
assert_eq "$trip" "\"moved\"" "the return trip lands Files in the inspector bar"
sleep 1
after=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
assert_eq "$after" "$before" "the emptied source pane closed (back to $before panes)"
empty=$(jsq '[...document.querySelectorAll("[data-pane-id]")].filter(p=>!p.classList.contains("pinned")&&p.querySelectorAll(".dock-tab").length===0).length' 2>&1)
assert_eq "$empty" "0" "no empty main pane lingers"

assert_no_console_errors