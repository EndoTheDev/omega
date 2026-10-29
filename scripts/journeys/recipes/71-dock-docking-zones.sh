#!/bin/bash
# 71-dock-docking-zones - the VS Code-style landing prediction (card
# t_0e833eae): a tab dragged over ANOTHER pane's body lights its landing
# zone - the four half-zones split the target around the dragged tab,
# center groups it - with a ghost of the dragged tab where it would land.
# Escape cancels the gesture with the layout untouched; release on left
# splits the target sideways, the dragged tab takes the left half.
# Beat here: the files tab (seated in the pinned inspector shell) drags
# over the main chat pane's body. Synthetic HTML5 drags (real
# DataTransfer) act out each beat like journey 70 does.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
# deterministic stage: a prior run's persisted layout (this chrome profile
# is shared across suite invocations) must not leak panes/tabs into the
# beats - the docking beats read a DEFAULT shell
jsq 'localStorage.removeItem("omega.dock-layout"); 1' >/dev/null 2>&1 || true
ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || true
ab wait --load networkidle >/dev/null 2>&1 || true
# the gate judges THIS page session: drop the pre-reset load's console
# history (the throwaway first open served the polluted layout)
ab console --clear >/dev/null 2>&1 || true
ab wait --load networkidle >/dev/null 2>&1 || true

# the stage: the pinned inspector's files tab and the main chat pane
# card t_502b8e81: pinned shells render ICON-ONLY chips - the name rides
# aria-label (no .dock-tab-title text to compare).
chipName71='((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim()'
src_exists=$(jsq "[...document.querySelectorAll(\".dock-pane.pinned .dock-tab\")].some(t => ($chipName71) === \"Files\") ? 1 : 0" 2>&1)
assert_eq "$src_exists" "1" "the files tab sits in the pinned inspector shell"
dst_exists=$(jsq 'document.querySelector(".dock-pane.kind-chat") ? 1 : 0' 2>&1)
assert_eq "$dst_exists" "1" "the main chat pane renders"

before=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
rowsBefore=$(jsq 'document.querySelectorAll(".dock-split.row").length' 2>&1)
step "ok: stage set ($before panes, $rowsBefore row splits)"

# BEAT 1 - zones light on hover + the ghost preview: drag files over the
# chat pane's body, pointer in the left quarter -> the left half lights
js '(()=>{
  const src=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(t => (t.getAttribute("aria-label")||"").trim() === "Files");
  const dst=document.querySelector(".dock-pane.kind-chat");
  if (!src||!dst) return "no panes";
  const dt=new DataTransfer();
  src.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));
  const r=dst.getBoundingClientRect();
  const bar=dst.querySelector(".dock-tabbar");
  const top=bar?bar.getBoundingClientRect().bottom:r.top;
  window.__jrn_dt=dt;
  dst.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.15,clientY:top+(r.bottom-top)*0.5}));
  return "ok";
})()' >/dev/null 2>&1 || true
sleep 1
zone=$(jsq 'document.querySelector(".dock-pane .dock-drop-overlay[data-zone=left]") ? 1 : 0' 2>&1)
assert_eq "$zone" "1" "left zone lights on body hover"
ghost=$(jsq '(()=>{const g=document.querySelector(".dock-drop-ghost");return g&&g.textContent.includes("Files")?1:0})()' 2>&1)
assert_eq "$ghost" "1" "the ghost previews the dragged tab"

# BEAT 2 - Escape cancels cleanly: overlay gone, layout untouched
js 'document.dispatchEvent(new KeyboardEvent("keydown",{key:"Escape",bubbles:true}));
    const dt=window.__jrn_dt;
    const src=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(t => (t.getAttribute("aria-label")||"").trim() === "Files");
    if (src&&dt) src.dispatchEvent(new DragEvent("dragend",{dataTransfer:dt,bubbles:true}));' >/dev/null 2>&1 || true
sleep 1
cleared=$(jsq 'document.querySelector(".dock-drop-overlay") ? 0 : 1' 2>&1)
assert_eq "$cleared" "1" "Escape clears the prediction"
after=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
assert_eq "$after" "$before" "Escape leaves the layout untouched"
rowsAfter=$(jsq 'document.querySelectorAll(".dock-split.row").length' 2>&1)
assert_eq "$rowsAfter" "$rowsBefore" "Escape mints no split"

# BEAT 3 - release on left: the chat pane splits sideways, files takes
# the left half
out=$(js '(()=>{
  const dt=new DataTransfer();
  const src=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(t => (t.getAttribute("aria-label")||"").trim() === "Files");
  const dst=document.querySelector(".dock-pane.kind-chat");
  if (!src||!dst) return "no gesture";
  src.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));
  const r=dst.getBoundingClientRect();
  const bar=dst.querySelector(".dock-tabbar");
  const top=bar?bar.getBoundingClientRect().bottom:r.top;
  dst.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.15,clientY:top+(r.bottom-top)*0.5}));
  dst.dispatchEvent(new DragEvent("drop",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.15,clientY:top+(r.bottom-top)*0.5}));
  src.dispatchEvent(new DragEvent("dragend",{dataTransfer:dt,bubbles:true}));
  return "ok";
})()' 2>&1)
step "drag beat: $out"
sleep 1
after2=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
assert_eq "$after2" "$((before+1))" "the target split around the drop"
rows2=$(jsq 'document.querySelectorAll(".dock-split.row").length' 2>&1)
assert_eq "$rows2" "$((rowsBefore+1))" "the zone drop minted one row split"

# the landing: an UNPINNED files pane exists, and it sits beside the chat
# pane in that fresh row - the dragged tab took the left half
landing=$(jsq '(()=>{const fp=[...document.querySelectorAll(".dock-pane.kind-files")].find(p=>!p.classList.contains("pinned"));const cp=document.querySelector(".dock-pane.kind-chat");if(!fp||!cp) return 0;return fp.parentElement===cp.parentElement&&fp.nextElementSibling?.classList.contains("dock-divider")?1:0})()' 2>&1)
assert_eq "$landing" "1" "files landed left of the chat pane (row sibling)"

assert_no_console_errors