#!/bin/bash
# 100-kanban-crud - the board tab as a standalone CRUD surface (round
# t_f81e845e): the kanban tab must manage cards WITHOUT the REST API or
# the CLI. In a browser, driving the real UI:
#
#   a. CREATE - the + in any column opens the full form (title, body,
#      assignee, priority); the card lands in that column wearing its
#      priority chip and a body-preview line;
#   b. EDIT - the drawer edits the title (pencil), the body (click),
#      and the priority (the listbox); every save round-trips to the
#      server (GET /tasks and GET /tasks/{id} carry the change);
#   c. COMMENT - the comment form appends and the event trail grows a
#      comment row; the events list renders created/edited/comment;
#   d. DELETE - the armed two-click discard (edit-body form) removes
#      the card: gone from the DOM, gone from the server (404).
#
# The board is PINNED (j100-crud, recipe 78/98's shape); this run's card
# carries the run tag in the title and DELETEs out at exit (the
# TaskDeleter capability is new; the archive fallback keeps older lanes
# working).
. "${JROOT:?run must export JROOT}/lib.sh"

BTAG="j100"
OMEGA_BIN="${OMEGA_BIN:-$JROOT/../../bin/omega}"
BOARD="$BTAG-crud"
TAG="$(date +%s%N | cut -c1-13)"
A=""

ensure_board() {
  if "$OMEGA_BIN" kanban board create "$1" >/dev/null 2>&1; then
    return 0
  fi
  if "$OMEGA_BIN" kanban board list 2>/dev/null | sed 's/^[* ] \+//' | awk '{print $1}' | grep -qx "$1"; then
    return 0
  fi
  die "board ensure $1"
}

# delete_cards <ids...>: exit cleanup - DELETE rides the new route; a
# lane serving an older embed (or a store without the capability)
# falls back to archive, the old recipe's cleanup.
delete_cards() {
  local id
  for id in "$@"; do
    [ -n "$id" ] || continue
    if ! curl -s -X DELETE "${JOURNEY_BASE_URL}/tasks/${id}?board=${BOARD}" >/dev/null 2>&1; then
      curl -s -X PATCH "${JOURNEY_BASE_URL}/tasks/${id}?board=${BOARD}" \
        -H 'Content-Type: application/json' -d '{"status":"archived"}' >/dev/null || true
    fi
  done
}

# col_ids <status>: the server's order for that column.
col_ids() {
  curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}&status=$1" \
    | tr -d '\r' | grep -oE '"id":"t_[0-9a-f]+"' | grep -oE 't_[0-9a-f]+' \
    | tr '\n' ' ' | sed 's/ *$//'
}

# task_by_title <title>: the id whose title matches (GET /tasks scan).
task_by_title() {
  curl -s "${JOURNEY_BASE_URL}/tasks?board=${BOARD}" \
    | tr -d '\r' | grep -oE '\{"id":"t_[0-9a-f]+","title":"[^"]*"[^}]*\}' \
    | grep -F "\"$1\"" | grep -oE 't_[0-9a-f]+' | head -1
}

# js_click <selector>: one DOM click on the first match (selectors use
# UNQUOTED attribute values - the printf lands inside a JS double
# quoted string).
js_click() {
  js "$(printf '(()=>{const e=document.querySelector("%s");if(!e) return 0;e.click();return 1})()' "$1")" >/dev/null
}

# js_click_card <id>: click the card with the id (the click trails a
# pointerup WITHOUT a moved drag, so the drawer opens).
js_click_card() {
  js "$(printf '(()=>{const e=[...document.querySelectorAll(".card")].find(x=>x.dataset.id==="%s");if(!e) return 0;e.dispatchEvent(new MouseEvent("click",{bubbles:true}));return 1})()' "$1")" >/dev/null
}

# pick_listbox <scopeSel> <label>: open a BandSelect pop inside scope and
# pick the option whose text matches exactly.
pick_listbox() {
  js "$(printf '(()=>{const s=document.querySelector("%s");if(!s) return 0;const c=s.querySelector(".model-chip");if(!c) return 0;if(c.getAttribute("aria-expanded")!=="true")c.click();return 1})()' "$1")" >/dev/null
  js "$(printf '(()=>{const s=document.querySelector("%s");const o=[...s.querySelectorAll(".model-list li")].find(l=>(l.textContent||"").trim()==="%s");if(!o) return 0;o.click();return 1})()' "$1" "$2")" >/dev/null
}

switch_to_board() {
  # recipe 98's two-mount law (card t_2bfd52ac): poll, reload, poll
  # again, die NAMING the registry answer.
  if ! poll_js "(()=>{return !!document.querySelector('.board-switch-select .model-chip')})()" 15; then
    ab reload >/dev/null 2>&1 || true
    ab wait --load networkidle >/dev/null 2>&1 || true
    poll_js "(()=>{return !!document.querySelector('.board-switch-select .model-chip')})()" 10 ||
      die "board switcher chip never rendered on two mounts (GET /boards: $(curl -s "${JOURNEY_BASE_URL}/boards" | head -c 200))"
  fi
  js '(()=>{const c=document.querySelector(".board-switch-select .model-chip");if(c.getAttribute("aria-expanded")!=="true")c.click();return 1})()' >/dev/null
  wait_js "(()=>{return [...document.querySelectorAll('.board-switch-select .model-list li')].some(l=>(l.textContent||'').trim()==='$BOARD')})()" 15
  js "$(printf '(()=>{const o=[...document.querySelectorAll(".board-switch-select .model-list li")].find(l=>(l.textContent||"").trim()==="%s");if(!o) return 0;o.click();return 1})()' "$BOARD")" >/dev/null
  wait_js "(()=>{return document.body.textContent.includes('$BTAG-crud')})()" 15
}

step "pinning board: $BOARD"
ensure_board "$BOARD"
trap 'delete_cards "$A"' EXIT

# ---- boot ----------------------------------------------------------------
open_app
ab open "${JOURNEY_BASE_URL}/board" >/dev/null 2>&1 || die "cannot open /board"
ab wait --load networkidle >/dev/null 2>&1 || true
switch_to_board
step "ok: pinned board $BOARD live"

# ---- a: CREATE through the full form --------------------------------------
TITLE="$BTAG-crud $TAG alpha"
BODY="the full spec body for $TAG"
ASSIG="cody"
COL='.board-col[data-status=todo]'
js_click "$COL .col-create-btn"
wait_js '(()=>{return !!document.querySelector(".board-col[data-status=todo] .col-create-title")})()' 8
ab type "$COL .col-create-title" "$TITLE" >/dev/null
ab fill "$COL .col-create-body" "$BODY" >/dev/null
ab fill "$COL .col-create-assignee" "$ASSIG" >/dev/null
pick_listbox "$COL .col-create-prio" "P2"
js_click "$COL .col-create-actions .col-add-btn"
wait_text "$TITLE" 10

A=$(task_by_title "$TITLE")
[ -n "$A" ] || die "created card never reached GET /tasks (title: $TITLE)"
[ "$(col_ids todo | awk '{print $1}')" = "$A" ] || die "created card did not land at the top of todo (ids: $(col_ids todo))"

# the card wears the P2 chip and the body preview
wait_js "$(printf '(()=>{const e=[...document.querySelectorAll(".card")].find(x=>x.dataset.id==="%s");if(!e||!e.querySelector(".card-prio"))return false;return (e.querySelector(".card-prio").textContent||"").trim()==="P2"&&((e.querySelector(".card-preview")||{}).textContent||"").includes("full spec body")})()' "$A")" 8
step "ok: create form made a todo card with P2 chip + body preview"

# ---- b: open the drawer, EDIT title, body, priority ------------------------
js_click_card "$A"
wait_js '(()=>{return !!document.querySelector(".drawer")})()' 8
# the event trail renders the create event (kind text is CSS-uppercased:
# match the DOM text, not the rendered case)
wait_js '(()=>{return [...document.querySelectorAll(".event-row .event-kind")].some(x=>(x.textContent||"").trim()==="created")})()' 8
step "ok: drawer opens with the event trail"

NEW_TITLE="$TITLE renamed"

# title edit: pencil -> input (the draft pre-fills the current title,
# so fill the FULL new title) -> save
js_click '.drawer-edit'
wait_js '(()=>{return !!document.querySelector(".drawer-edit-form .drawer-edit-input")})()' 8
ab fill '.drawer-edit-form .drawer-edit-input' "$NEW_TITLE" >/dev/null
js_click '.drawer-edit-form .drawer-save'
wait_text "$NEW_TITLE" 10
[ -n "$(task_by_title "$NEW_TITLE")" ] || die "title save did not round-trip (GET /tasks)"
step "ok: title edit round-trips to the server"

# body edit: click the pre -> textarea -> save
EDITBODY="$BODY edited body"
js_click '.drawer-body'
wait_js '(()=>{return !!document.querySelector(".drawer-body-input")})()' 8
ab fill '.drawer-body-input' "$EDITBODY" >/dev/null
js_click '.drawer-body-actions .drawer-save'
wait_js '(()=>{const t=[...document.querySelectorAll(".card .card-preview")].some(x=>(x.textContent||"").includes("edited body"));return t})()' 10
curl -s "${JOURNEY_BASE_URL}/tasks/${A}?board=${BOARD}" | grep -q "edited body" || die "body save did not round-trip"
step "ok: body edit round-trips to the server"

# priority edit: the drawer's listbox -> P3, chip + server agree
pick_listbox '.drawer-priority' "P3"
wait_js "$(printf '(()=>{const c=[...document.querySelectorAll(".card")].find(x=>x.dataset.id==="%s");if(!c)return false;return ((c.querySelector(".card-prio")||{}).textContent||"").trim()==="P3"})()' "$A")" 10
curl -s "${JOURNEY_BASE_URL}/tasks/${A}?board=${BOARD}" | grep -q '"priority":3' || die "priority save did not round-trip"
step "ok: priority edit round-trips (P3 on the server)"

# ---- c: COMMENT + the event trail ------------------------------------------
ab fill '.comment-input' "the $TAG round-trip note" >/dev/null
js_click '.comment-send'
wait_text "the $TAG round-trip note" 10
curl -s "${JOURNEY_BASE_URL}/tasks/${A}?board=${BOARD}" | grep -q "round-trip note" || die "comment did not reach GET /tasks/{id}"
# events: created + edited + comment kinds all render in the trail
wait_js '(()=>{const k=[...document.querySelectorAll(".event-row .event-kind")].map(x=>(x.textContent||"").trim());return k.includes("created")&&k.includes("edited")&&k.includes("comment")})()' 10
step "ok: comment posted, event trail renders created/edited/comment"

# ---- d: DELETE - armed two-click, card gone everywhere ----------------------
# the discard lives in the body-edit form
js_click '.drawer-body'
wait_js '(()=>{return !!document.querySelector(".drawer-discard")})()' 8
js_click '.drawer-discard'
wait_js '(()=>{const d=document.querySelector(".drawer-discard");return !!d&&(d.textContent||"").includes("confirm delete")})()' 5
js_click '.drawer-discard'
wait_js '(()=>{return !document.querySelector(".drawer")})()' 8
wait_js "$(printf '(()=>{return ![...document.querySelectorAll(".card")].some(x=>x.dataset.id==="%s")})()' "$A")" 8
sleep 1
code=$(curl -s -o /dev/null -w '%{http_code}' "${JOURNEY_BASE_URL}/tasks/${A}?board=${BOARD}")
[ "$code" = "404" ] || die "deleted card still answers GET $code (task $A)"
step "ok: armed delete removes the card (drawer closes, server 404s)"

assert_no_console_errors
A=""   # the delete already cleaned this run's card
step "100-kanban-crud GREEN"
