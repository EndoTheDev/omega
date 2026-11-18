#!/bin/bash
# 100-sidebar-stack-only - canvas C2, THE SIDEBAR SHAPE LAW (t_78e6ca62):
# a sidebar is TABS + a STACK. It stacks panes (a column split) and NEVER
# puts two columns of content side by side - the shell's two columns ARE the
# two sidebars, so the split-sidebar gesture is DELETED, not refused: the
# sidebar body has no left/right half at all.
# Beats (the left sessions sidebar is both source and target):
#   1. hover its TOP quarter -> a `top` zone lights and NO left/right zone
#      exists anywhere in that sidebar (the deleted gesture's entry point);
#   2. release on its LEFT quarter -> the column drop is a FOLD, not a split
#      (no pane minted, no row split added, no local split tree);
#   3. release on its TOP quarter -> the sidebar STACKS: one column split
#      inside the sidebar, the dragged tab in the fresh leaf;
#   4. the fresh leaf's strip is the SIDEBAR strip (icon-only chips, no
#      split-actions menu) - the zone identity, so a text strip can never be
#      crushed into a narrow column again.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
# deterministic stage: a prior run's persisted layout (this chrome profile is
# shared across suite invocations) must not leak a grown sidebar into the beats
jsq 'localStorage.removeItem("omega.dock-layout"); 1' >/dev/null 2>&1 || true
ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || true
ab wait --load networkidle >/dev/null 2>&1 || true
ab console --clear >/dev/null 2>&1 || true
ab wait --load networkidle >/dev/null 2>&1 || true

# the stage: the primary sidebar (sessions list) and a chip that may travel
# into a fresh pane - the LOCKED sessions tab never lands in one, so the
# recipe drags the agents chip (falling back to the last chip)
left_exists=$(jsq 'document.querySelector(".dock-pane.pinned:not(.edge-bottom)") ? 1 : 0' 2>&1)
assert_eq "$left_exists" "1" "the primary sidebar renders"
chip_exists=$(jsq '[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom) .dock-tab")].length > 0 ? 1 : 0' 2>&1)
assert_eq "$chip_exists" "1" "the sidebar carries a tab chip to drag"

panesBefore=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
colsBefore=$(jsq 'document.querySelectorAll(".dock-split.column").length' 2>&1)
rowsBefore=$(jsq 'document.querySelectorAll(".dock-split.row").length' 2>&1)
step "ok: stage set ($panesBefore panes, $colsBefore column splits, $rowsBefore row splits)"

# BEAT 1 - the sidebar offers its vertical lane only: the TOP quarter lights a
# `top` zone, and no left/right zone can be produced inside that sidebar
js '(()=>{
  const chips=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom) .dock-tab")];
  const src=chips.find(t => ((t.getAttribute("aria-label")||t.title||"").trim()) === "Agents") || chips[chips.length-1];
  const dst=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");
  if (!src||!dst) return "no stage";
  const dt=new DataTransfer();
  src.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));
  const r=dst.getBoundingClientRect();
  const bar=dst.querySelector(".dock-tabbar");
  const top=bar?bar.getBoundingClientRect().bottom:r.top;
  window.__jrn_c2_dt=dt;
  dst.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.5,clientY:top+(r.bottom-top)*0.08}));
  return "ok";
})()' >/dev/null 2>&1 || true
wait_js '!!document.querySelector(".dock-pane.pinned:not(.edge-bottom) .dock-drop-overlay[data-zone=top]")' 10
topZone=$(jsq 'document.querySelector(".dock-pane.pinned:not(.edge-bottom) .dock-drop-overlay[data-zone=top]") ? 1 : 0' 2>&1)
assert_eq "$topZone" "1" "the sidebar's top quarter lights the stack zone"

# the deleted gesture's entry point: no horizontal half exists in a sidebar -
# hovering its LEFT quarter (the same x recipe 71 splits a main pane with)
# must never light a left/right zone inside that sidebar
js '(()=>{
  const dt=window.__jrn_c2_dt;
  const dst=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");
  if (!dt||!dst) return "no gesture";
  const r=dst.getBoundingClientRect();
  const bar=dst.querySelector(".dock-tabbar");
  const top=bar?bar.getBoundingClientRect().bottom:r.top;
  dst.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.08,clientY:top+(r.bottom-top)*0.5}));
  return "ok";
})()' >/dev/null 2>&1 || true
sideZone=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");if(!s)return "no sidebar";return s.querySelector(".dock-drop-overlay[data-zone=left], .dock-drop-overlay[data-zone=right]") ? 1 : 0})()' 2>&1)
assert_eq "$sideZone" "0" "a sidebar body has no left/right half (the split gesture is gone)"

# BEAT 2 - release on the LEFT quarter: a FOLD (the same-pane center read). A
# sidebar has no column split to enter, so nothing is minted and nothing grows.
out2=$(js '(()=>{
  const dt=window.__jrn_c2_dt;
  const dst=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");
  if (!dt||!dst) return "no gesture";
  const r=dst.getBoundingClientRect();
  const bar=dst.querySelector(".dock-tabbar");
  const top=bar?bar.getBoundingClientRect().bottom:r.top;
  dst.dispatchEvent(new DragEvent("drop",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.08,clientY:top+(r.bottom-top)*0.5}));
  return "ok";
})()' 2>&1)
step "left-quarter release: $out2"
wait_js '!document.querySelector(".dock-drop-overlay")' 10
panesAfter2=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
assert_eq "$panesAfter2" "$panesBefore" "the left-quarter drop minted no pane"
rowsAfter2=$(jsq 'document.querySelectorAll(".dock-split.row").length' 2>&1)
assert_eq "$rowsAfter2" "$rowsBefore" "no row split appeared beside the sidebar"
grownAfter2=$(jsq 'document.querySelector(".dock-pane.pinned:not(.edge-bottom) > .dock-split") ? 1 : 0' 2>&1)
assert_eq "$grownAfter2" "0" "the sidebar grew nothing"

# BEAT 3 - release on the TOP quarter: the sidebar STACKS (its kept shape)
out3=$(js '(()=>{
  const dt=new DataTransfer();
  const chips=[...document.querySelectorAll(".dock-pane.pinned:not(.edge-bottom) .dock-tab")];
  const src=chips.find(t => ((t.getAttribute("aria-label")||t.title||"").trim()) === "Agents") || chips[chips.length-1];
  const dst=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");
  if (!src||!dst) return "no gesture";
  src.dispatchEvent(new DragEvent("dragstart",{dataTransfer:dt,bubbles:true}));
  const r=dst.getBoundingClientRect();
  const bar=dst.querySelector(".dock-tabbar");
  const top=bar?bar.getBoundingClientRect().bottom:r.top;
  dst.dispatchEvent(new DragEvent("dragover",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.5,clientY:top+(r.bottom-top)*0.2}));
  dst.dispatchEvent(new DragEvent("drop",{dataTransfer:dt,bubbles:true,clientX:r.left+r.width*0.5,clientY:top+(r.bottom-top)*0.2}));
  src.dispatchEvent(new DragEvent("dragend",{dataTransfer:dt,bubbles:true}));
  return "ok";
})()' 2>&1)
step "stack release: $out3"
wait_js '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");return !!(s && s.querySelector(":scope > .dock-split"))})()' 10
panesAfter3=$(jsq 'document.querySelectorAll("[data-pane-id]").length' 2>&1)
assert_ne "$panesAfter3" "$panesBefore" "the sidebar stacked (panes minted)"
stackCol=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");return s && s.querySelector(":scope > .dock-split.column") ? 1 : 0})()' 2>&1)
assert_eq "$stackCol" "1" "the sidebar's local tree is a COLUMN (a stack, never two columns)"
sideRows=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");return s ? s.querySelectorAll(".dock-split.row").length : -1})()' 2>&1)
assert_eq "$sideRows" "0" "no row split lives inside the sidebar"
leafCount=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");return s ? s.querySelectorAll("[data-pane-id]").length : -1})()' 2>&1)
assert_eq "$leafCount" "2" "the stack is two leaves"

# BEAT 4 - the stack leaves carry the SIDEBAR strip: icon-only chips, and no
# split-actions menu and no '+' (the shape law's entry points are gone with
# the gesture) - a text strip in a narrow pane was the bleed class
leafIcon=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");if(!s)return 0;const tabs=[...s.querySelectorAll(".dock-tab")];return tabs.length>0 && tabs.every(t=>t.classList.contains("ic-only")) ? 1 : 0})()' 2>&1)
assert_eq "$leafIcon" "1" "the sidebar's stack leaves render icon-only chips"
leafMenu=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");if(!s)return -1;return s.querySelectorAll(".dock-tab-btn.actions").length})()' 2>&1)
assert_eq "$leafMenu" "0" "no split-actions menu inside a sidebar"
leafNew=$(jsq '(()=>{const s=document.querySelector(".dock-pane.pinned:not(.edge-bottom)");if(!s)return -1;return s.querySelectorAll(".dock-tab-new").length})()' 2>&1)
assert_eq "$leafNew" "0" "no '+' inside a sidebar"

assert_no_console_errors
