#!/bin/bash
# 120-capsule-row-menus - the right-click arc's step 5b: the secondary-capsule interiors.
#
# Every surface in the right inspector offered its verbs only through a control you had to
# find first. This recipe drives the menus this step adds through the REAL input pipeline -
# a right-button press on the real row, then a real click on the menu's own row:
#
#   skills        a row menus Open / Disable / Delete. Disable writes GET /disabled/skills,
#                 the menu then reads Enable, and Delete arms the tab editor's ONE confirm
#                 (POST /skills/delete) which removes the seed.
#   tools         a row menus Disable (PUT /disabled/tools) and the menu reads Enable again
#                 once the row is off.
#   mcp           a seeded server row menus Open / Disable / Delete. Delete unfolds the
#                 row detail's one confirm and POST /mcp/delete empties the entry.
#   files         a file row menus New / Open / Copy / Copy path / Add file to chat. Open
#                 mints the viewer tab, New opens the tab's own create form, and Copy's own
#                 answer (copied, or refused where the page cannot reach a clipboard) lands
#                 on the tab's status line - a refusal never reads as "copied".
#   notifications a record row menus Clear all (DELETE /notifications) and the feed empties.
#
# Escape closes any of them; the pure item lists are pinned in webui/test/capsulemenus.test.ts.
#
# Residue-free: the trap deletes the seeded mcp entry, the skill dir, the probe file, the
# board task (whose done move IS the notification seed) and clears the feed.
. "${JROOT:?run must export JROOT}/lib.sh"
# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep leaves at the first match, so a needle that IS present
# reads as missing - and the negated form reads it as absent, which is a false
# green that hides a defect. A herestring has no writer, so grep's early exit
# costs nothing. This file scans its own live lines; comments may name the
# shape, so they are skipped.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] \
	|| { printf '%s: pipes a writer into grep -q (SIGPIPE under pipefail): %s\n' "${_PIN_SELF##*/}" "$_pin_hits" >&2; exit 1; }


if [ -z "${OMEGA_HOME:-}" ]; then
  [ -n "${OMEGA_BIN:-}" ] || die "set OMEGA_HOME or OMEGA_BIN to the serving lane home"
  OMEGA_HOME="$(dirname "$OMEGA_BIN")"
fi
SKILL="jrn-menu$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"
SKILL_DIR="$OMEGA_HOME/skills/$SKILL"
MCP="jrn-mcp-$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"
PROBE="jrn-menu-probe-$(date +%d%b%H%M%S).txt"
JRN_TASK="jrn-$(date +%s)-menu"
TASK_ID=""

menu_rows() {
  local r
  r=$(jsq '[...document.querySelectorAll(".context-menu-item .context-menu-label")].map(e=>e.textContent.trim()).join("|")')
  printf '%s' "${r//\"/}"
}

right_click_at() {
  ab mouse move "$1" "$2" >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real pointer
               # pipeline coalesces a move and a press that land in one task
  ab mouse down right >/dev/null 2>&1
  ab mouse up right >/dev/null 2>&1
}

# right_click_js <expr returning "x y">
right_click_js() {
  local box
  box=$(jsq "$1")
  box=${box//\"/}
  [ -n "$box" ] || die "the right-click target never rendered: $1"
  right_click_at $box
}

click_row() {
  local label=$1 box
  box=$(jsq "(() => {const b=[...document.querySelectorAll('.context-menu-item')].find(x=>((x.querySelector('.context-menu-label')||{}).textContent||'').trim()==='$label'); if(!b) return ''; const r=b.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()")
  box=${box//\"/}
  [ -n "$box" ] || die "no menu row named $label"
  ab mouse move $box >/dev/null 2>&1
  sleep 0.15   # recipe-lint: allow fixed-sleep - move and press pacing
  ab mouse down >/dev/null 2>&1
  ab mouse up >/dev/null 2>&1
}

# menu_wait: every menu assertion waits for the measured box, never a bare frame
menu_wait() {
  wait_js "(() => {const m=document.querySelector('.context-menu.measured'); return m && m.innerText.includes('$1')})()" 10 \
    || die "the row menu never opened with '$1'"
}

esc_closes() {
  ab press Escape >/dev/null 2>&1
  wait_js '(() => !document.querySelector(".context-menu"))()' 10 || die "Escape did not close the menu"
}

# chip_click <aria-label>: the sidebar strips are icon-only, so the accessible name
# carries the tab's own title (the dockable.json override).
chip_click() {
  local clicked
  clicked=$(jsq "(() => {const t=[...document.querySelectorAll('.dock-tabbar .dock-tab')].find((e)=>((e.getAttribute('aria-label')||e.textContent||'').trim())==='$1'); if(!t) return 0; t.click(); return 1})()")
  assert_eq "${clicked//\"/}" "1" "the $1 chip is on the strip"
}

disabled_names() {
  curl -s "$JOURNEY_BASE_URL/disabled" | python3 -c '
import json, sys
print(" ".join((json.load(sys.stdin) or {}).get(sys.argv[1], [])))' "$1" 2>/dev/null
}

# fold_has_name <kind> <name>: WHOLE-TOKEN match - the wire prints a space-joined,
# server-sorted list, so a bare `*"$NAME"` glob would anchor on the line's end.
fold_has_name() {
  case " $(disabled_names "$1") " in
    *" $2 "*) return 0 ;;
  esac
  return 1
}

cleanup() {
  curl -s -X POST "$JOURNEY_BASE_URL/mcp/delete" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$MCP\"}" >/dev/null 2>&1 || true
  curl -s -X PUT "$JOURNEY_BASE_URL/disabled/skills" -H 'Content-Type: application/json' \
    -d "{\"name\": \"$SKILL\", \"enabled\": true}" >/dev/null 2>&1 || true
  curl -s -X PUT "$JOURNEY_BASE_URL/disabled/tools" -H 'Content-Type: application/json' \
    -d "{\"name\": \"$TOOL\", \"enabled\": true}" >/dev/null 2>&1 || true
  curl -s -X DELETE "$JOURNEY_BASE_URL/notifications" >/dev/null 2>&1 || true
  [ -n "$TASK_ID" ] && curl -s -X DELETE "$JOURNEY_BASE_URL/tasks/$TASK_ID" >/dev/null 2>&1
  rm -rf "$SKILL_DIR" 2>/dev/null || true
  # the probe file lands under whatever root the server resolved for the session, which
  # only the browse route can name (the files capsule has no delete route).
  probe_root=$(curl -s "$JOURNEY_BASE_URL/files/browse?path=" | python3 -c \
    'import json,sys; print((json.load(sys.stdin) or {}).get("root") or "")' 2>/dev/null)
  [ -n "$probe_root" ] && rm -f "$probe_root/$PROBE" 2>/dev/null
  return 0
}
TOOL=""
trap 'cleanup; cleanup_journey_residue' EXIT

# --- seed: one skill, one mcp entry, one notification record ---------------
mkdir -p "$SKILL_DIR" || die "cannot mkdir $SKILL_DIR (OMEGA_HOME not writable?)"
printf -- '---\nname: %s\ndescription: journey seed - the row menu probe\n---\nbody of %s\n' \
  "$SKILL" "$SKILL" > "$SKILL_DIR/SKILL.md" || die "seed write failed"

saved=$(curl -s -X POST "$JOURNEY_BASE_URL/mcp/save" -H 'Content-Type: application/json' \
  -d "{\"name\":\"$MCP\",\"command\":\"jrn-nonexistent-binary\"}")
grep -q "\"name\":\"$MCP\"" <<<"$saved" || die "POST /mcp/save did not land $MCP"

created=$(curl -s -X POST "$JOURNEY_BASE_URL/tasks" -H 'Content-Type: application/json' \
  -d "{\"title\":\"$JRN_TASK\"}")
TASK_ID=$(printf '%s' "$created" | grep -oE '"id": ?"t_[a-f0-9]+"' | head -1 | sed 's/.*"\(t_[a-f0-9]*\)".*/\1/')
[ -n "$TASK_ID" ] || die "task create failed: $created"
grep -Eq '"status": ?"done"' <<<"$(curl -s -X PATCH "$JOURNEY_BASE_URL/tasks/$TASK_ID" -H 'Content-Type: application/json'  -d '{"status":"done"}')" || die "the done move (the notification seed) failed"

JRN_ID=$(new_journey_session menu)
label_journey_session "$JRN_ID" "jrn-rowmenu-$(date +%d%b%H%M%S)"
wrote=$(curl -s -X POST "$JOURNEY_BASE_URL/files/write" -H 'Content-Type: application/json' \
  -d "{\"path\":\"$PROBE\",\"content\":\"row menu probe\\n\",\"session\":\"$JRN_ID\"}")
grep -q "\"$PROBE\"" <<<"$wrote" || die "POST /files/write did not land $PROBE: $wrote"
step "seed: skill $SKILL, mcp $MCP, one notification record, session $JRN_ID"

open_app
ab set viewport 1440 900 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
wait_booted

# the right sidebar (the inspector) must be open before its chips are clickable
side=$(jsq 'document.querySelector(".dock-pane.kind-files")?1:0' 2>/dev/null | tail -1)
if [ "$side" != "1" ]; then
  js 'document.querySelector("[aria-label=\"Toggle right sidebar\"]")?.click()'
  wait_js '(()=>{return document.querySelector(".dock-pane.kind-files")?true:false})()' 5 \
    || die "the right sidebar never opened"
fi

# --- 1. skills -------------------------------------------------------------
chip_click "Skills"
wait_js "(() => [...document.querySelectorAll('.skills-list .registry-row')].some((r)=>r.innerText.includes('$SKILL')))()" 15 \
  || die "the seeded skill's row never rendered"
skill_row="(() => {const row=[...document.querySelectorAll('.skills-list .registry-row')].find((r)=>r.innerText.includes('$SKILL')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$skill_row"
menu_wait "Open"
assert_eq "$(menu_rows)" "Open|Disable|Delete" "a skills row menus Open, Disable and Delete"
step "ok: the skills row's right-click opens the shared menu"
esc_closes
step "ok: Escape closes it"

right_click_js "$skill_row"
menu_wait "Disable"
click_row "Disable"
i=0
while [ "$i" -lt 40 ]; do
  fold_has_name skills "$SKILL" && break
  sleep 0.5
  i=$((i + 1))
done
fold_has_name skills "$SKILL" || die "Disable did not write GET /disabled/skills"
step "ok: Disable runs the pill's own PUT /disabled/skills"

wait_js "(() => [...document.querySelectorAll('.skills-list .registry-row')].some((r)=>r.innerText.includes('$SKILL') && r.className.includes('registry-off')))()" 15 \
  || die "the disabled skill's row did not render its off state"
right_click_js "$skill_row"
menu_wait "Enable"
assert_eq "$(menu_rows)" "Open|Enable|Delete" "a disabled skill row menus Enable - the verb follows the state"
click_row "Enable"
i=0
while [ "$i" -lt 40 ]; do
  fold_has_name skills "$SKILL" || break
  sleep 0.5
  i=$((i + 1))
done
if fold_has_name skills "$SKILL"; then die "Enable left the skill in the fold"; fi
step "ok: Enable clears the fold"

right_click_js "$skill_row"
menu_wait "Delete"
click_row "Delete"
if ! wait_js '(() => {const b=document.querySelector(".skill-editor-btn.danger"); return !!b && b.textContent.includes("confirm delete?")})()' 15; then
  die "the Delete row did not arm the editor's one confirm"
fi
jsq '(() => {const b=[...document.querySelectorAll(".skill-editor-btn.danger")].find((x)=>x.textContent.includes("confirm delete?")); if(!b) return 0; b.click(); return 1})()' >/dev/null
i=0
gone=0
while [ "$i" -lt 40 ]; do
  if ! grep -q "\"$SKILL\"" <<<"$(curl -s "$JOURNEY_BASE_URL/skills")"; then gone=1; break; fi
  sleep 0.5
  i=$((i + 1))
done
[ "$gone" = "1" ] || die "the deleted skill still rides GET /skills"
step "ok: Delete arms the existing confirm and removes the skill"

# --- 2. tools --------------------------------------------------------------
chip_click "Tools"
wait_js '(() => document.querySelectorAll(".tools-list .registry-row").length > 0)()' 15 \
  || die "the tools tab never rendered rows"
TOOL=$(jsq '(() => {const r=document.querySelector(".tools-list .registry-row .reg-row-name"); return r ? r.textContent.trim() : ""})()')
TOOL=${TOOL//\"/}
[ -n "$TOOL" ] || die "no tool row to menu"
tool_row="(() => {const row=[...document.querySelectorAll('.tools-list .registry-row')].find((r)=>((r.querySelector('.reg-row-name')||{}).textContent||'').trim()==='$TOOL'); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$tool_row"
menu_wait "Disable"
assert_eq "$(menu_rows)" "Disable" "a tool row menus its one verb - Disable"
click_row "Disable"
wait_js "(() => [...document.querySelectorAll('.tools-list .registry-row')].some((r)=>((r.querySelector('.reg-row-name')||{}).textContent||'').trim()==='$TOOL' && r.className.includes('registry-off')))()" 15 \
  || die "the Disable row did not turn $TOOL off"
step "ok: Disable runs PUT /disabled/tools"

right_click_js "$tool_row"
menu_wait "Enable"
assert_eq "$(menu_rows)" "Enable" "the disabled tool row menus Enable"
click_row "Enable"
wait_js "(() => ![...document.querySelectorAll('.tools-list .registry-row')].some((r)=>((r.querySelector('.reg-row-name')||{}).textContent||'').trim()==='$TOOL' && r.className.includes('registry-off')))()" 15 \
  || die "the Enable row did not turn $TOOL back on"
step "ok: Enable clears it again"

# --- 3. mcp ----------------------------------------------------------------
chip_click "MCP"
wait_js "(() => [...document.querySelectorAll('.mcp-row')].some((r)=>r.innerText.includes('$MCP')))()" 15 \
  || die "the seeded mcp server's row never rendered"
mcp_row="(() => {const row=[...document.querySelectorAll('.mcp-row')].find((r)=>r.innerText.includes('$MCP')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$mcp_row"
menu_wait "Open"
assert_eq "$(menu_rows)" "Open|Disable|Delete" "an MCP server row menus Open, Disable and Delete"
step "ok: the MCP row's right-click opens the shared menu"

right_click_js "$mcp_row"
menu_wait "Open"
click_row "Open"
wait_js '(() => !!document.querySelector(".mcp-detail"))()' 10 || die "Open unfolded no row detail"
step "ok: Open unfolds the row detail"

right_click_js "$mcp_row"
menu_wait "Delete"
click_row "Delete"
if ! wait_js '(() => !!document.querySelector(".mcp-detail .mcp-confirm-go"))()' 10; then
  die "the Delete row armed no confirm in the detail"
fi
jsq '(() => {const b=document.querySelector(".mcp-detail .mcp-confirm-go"); if(!b) return 0; b.click(); return 1})()' >/dev/null
i=0
gone=0
while [ "$i" -lt 40 ]; do
  if ! grep -q "\"$MCP\"" <<<"$(curl -s "$JOURNEY_BASE_URL/mcp/servers")"; then gone=1; break; fi
  sleep 0.5
  i=$((i + 1))
done
[ "$gone" = "1" ] || die "the deleted mcp entry still rides GET /mcp/servers"
step "ok: Delete arms the detail's one confirm and removes the entry"

# --- 4. the file tree ------------------------------------------------------
open_journey_row "jrn-rowmenu-"
chip_click "Files"
wait_js "(() => {const t=document.querySelector('.files-tree'); return !!t && (t.innerText||'').includes('$PROBE')})()" 20 \
  || die "the probe file's tree row never rendered"
file_row="(() => {const row=[...document.querySelectorAll('.files-tree .tree-row')].find((r)=>((r.querySelector('.name')||{}).textContent||'').includes('$PROBE')); if(!row) return ''; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+' '+Math.round(r.y+r.height/2);})()"
right_click_js "$file_row"
menu_wait "New"
assert_eq "$(menu_rows)" "New|Open|Copy|Copy path|Add file to chat" \
  "a file row menus the five tree verbs, the chat lane included"
step "ok: the file row's right-click opens the shared menu"
esc_closes

right_click_js "$file_row"
menu_wait "New"
click_row "New"
if ! wait_js '(() => !!document.querySelector(".files-add"))()' 10; then
  die "the New row opened no create form"
fi
step "ok: New opens the tab's own create form"

# Copy's notice is the copy's OWN answer: a page without a clipboard lane refuses, and
# the refusal is what the status line must say (never a silent "copied").
right_click_js "$file_row"
menu_wait "Copy"
click_row "Copy"
if ! wait_js '(() => {const p=document.querySelector(".files-tool-error"); return !!p && /copied|copy refused/.test(p.textContent)})()' 10; then
  die "the Copy row left no answer on the tab's status line"
fi
copy_line=$(jsq '(() => {const p=document.querySelector(".files-tool-error"); return p ? p.textContent.trim() : ""})()')
copy_line=${copy_line//\"/}
step "ok: Copy answers through the tab: $copy_line"

# Add file to chat rides the composer's own attach lane: the chip lands on the focused
# chat tab's strip (the same chip a picked file renders).
right_click_js "$file_row"
menu_wait "Add file to chat"
click_row "Add file to chat"
if ! wait_js "(() => [...document.querySelectorAll('.attach-file .attach-file-name')].some((e)=>(e.textContent||'').trim()==='$PROBE'))()" 15; then
  die "Add file to chat parked no attachment chip for $PROBE"
fi
step "ok: Add file to chat parks the composer's attachment chip"

# Open LAST: it mints a viewer tab, and focusing that tab moves the focused session away
# from the chat the file lanes read (the tree re-roots on nothing).
right_click_js "$file_row"
menu_wait "Open"
click_row "Open"
if ! wait_js "(() => {const t=[...document.querySelectorAll('.dock-tab')].find((x)=>(x.innerText||'').includes('$PROBE')); return !!t})()" 15; then
  die "the Open row minted no viewer tab for $PROBE"
fi
step "ok: Open mints the viewer tab"

# --- 5. notifications ------------------------------------------------------
chip_click "Notifications"
wait_js '(() => document.querySelectorAll(".notif-list .notif-row").length > 0)()' 15 \
  || die "the notification feed rendered no record row"
right_click_js '(() => {const row=document.querySelector(".notif-list .notif-row"); if(!row) return ""; const r=row.getBoundingClientRect(); return Math.round(r.x+r.width/2)+" "+Math.round(r.y+r.height/2);})()'
menu_wait "Clear all"
assert_eq "$(menu_rows)" "Clear all" "a notification row menus Clear all, one row, no per-entry clear"
step "ok: the notification row's right-click opens the shared menu"
click_row "Clear all"
i=0
cleared=0
while [ "$i" -lt 40 ]; do
  left=$(curl -s "$JOURNEY_BASE_URL/notifications" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("notifications") or []))' 2>/dev/null)
  if [ "$left" = "0" ]; then cleared=1; break; fi
  sleep 0.5
  i=$((i + 1))
done
[ "$cleared" = "1" ] || die "Clear all left $left records on the feed"
step "ok: Clear all empties the feed through DELETE /notifications"

step "done: five capsule interiors menu their existing verbs, act, and Escape closes them"
