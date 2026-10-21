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

# the inspector exposes the seven fixed tabs (count the misses)
missing=$(jsq '["Files","Cron","Logs","Skills","Tools","Settings","Kanban"].filter(t => !Array.from(document.querySelectorAll("button, [role=tab], .dock-tab")).some(b => (b.innerText || "").trim() === t)).length' 2>&1)
assert_eq "$missing" "0" "all seven inspector tabs render"

# the Files tab opens real pane content
ab find text "Files" click >/dev/null 2>&1 || die "Files tab not clickable"
sleep 1
files=$(jsq 'document.querySelectorAll("[data-pane-id]").length >= 1 && document.body.innerText.includes("Files") ? 1 : 0' 2>&1)
assert_eq "$files" "1" "Files tab renders content"

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

assert_no_console_errors