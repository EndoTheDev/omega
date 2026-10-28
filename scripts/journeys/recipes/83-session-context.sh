#!/bin/bash
# 83-session-context - the pinned context journey (t_9de5b009): the
# model picker and the workspace chip WRITE the session row, and a
# reload restores both on reopen - "sticks" is restore, not just an
# optimistic chip. Wire truth is the /sessions row (model + workspace
# fields); presentation truth is the composer chips after a fresh SPA
# load.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

# fresh_open: same stale-editing-state trap as 77-session-branch - a
# long-lived chrome keeps SPA state, about:blank forces a real remount.
fresh_open() { ab open "about:blank" >/dev/null 2>&1; open_app; }

# deq: ab eval prints JS strings JSON-quoted ("beam") - a value compared
# against row_field (plain) or interpolated into a JS string comparator
# must be unquoted first (the house dequote, cf 30-retry's sed). Strip
# the outer quote pair, unescape the inner ones.
deq() { sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'; }

# row_model / row_workspace: wire truth from the /sessions row.
row_field() {
  curl -s "$JOURNEY_BASE_URL/sessions" | python3 -c "
import sys, json
rows = json.load(sys.stdin)
for r in rows:
    if r['id'] == '$1':
        print(r.get('$2', '') or '')
        break
"
}

JRN_ID=$(new_journey_session ctx-pin | tail -1)
TAG=$(journey_tag)
LABEL="(journey) ctx-pin $TAG"
label_journey_session "$JRN_ID" "$LABEL"

fresh_open
open_journey_row "$LABEL"

# --- the model picker writes the row --------------------------------
# the chip waits for the models fetch to seed it (boot fires /models
# without blocking; the chip fills within ~a second on a healthy lane)
wait_js '!!(document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? "")' 10 \
  || die "model chip did not render"
default=$(jsq 'document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? ""' 2>&1 | tail -1 | deq)
[ -n "$default" ] || die "model chip rendered empty"

jsq 'document.querySelector(".model-select:not(.thinking-select) .model-chip")?.click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".model-list li")' 10
# pick the first option that differs from the chip's current model
# target is read INSIDE js (cur comes from the DOM) - it exits as a
# plain model name once deq'd, safe to interpolate into the li matcher
target=$(jsq '(()=>{const cur = document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? ""; const opts = Array.from(document.querySelectorAll(".model-list li")); const other = opts.find(li => (li.textContent || "").trim() && (li.textContent || "").trim() !== cur.trim()); return other ? (other.textContent || "").trim() : "";})()' 2>&1 | tail -1 | deq)
if [ -z "$target" ]; then
  # one-model lane: the picker cannot move anything - the pin halves
  # below still prove the restore path over a wire-written pin
  step "lane serves one model - picker change skipped"
  curl -s -X PATCH "$JOURNEY_BASE_URL/sessions/$JRN_ID" \
    -H 'Content-Type: application/json' -d "{\"model\": \"$default\"}" >/dev/null
else
  jsq "(()=>{const li = Array.from(document.querySelectorAll(\".model-list li\")).find(li => (li.textContent || \"\").trim() === \"$target\"); if (!li) return 0; li.click(); return 1;})()" >/dev/null 2>&1
  pinned=0
  for i in 1 2 3 4 5 6 7 8 9 10; do
    pinned=$(row_field "$JRN_ID" model)
    [ "$pinned" = "$target" ] && break
    sleep 1
  done
  assert_eq "$pinned" "$target" "picker change wrote the session row (model)"
fi

# --- reload: the pin restores ---------------------------------------
fresh_open
open_journey_row "$LABEL"
restored=$(jsq 'document.querySelector(".model-select:not(.thinking-select) .model-chip .chip-name")?.textContent ?? ""' 2>&1 | tail -1 | deq)
assert_eq "$restored" "$target" "pinned model restored on reopen"

# --- the workspace chip writes the row ------------------------------
jsq 'document.querySelector(".ws-btn")?.click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".ws-pop .ws-dirs .ws-dir")' 10
# browse: "/" -> home (a non-hidden dir that exists on every linux
# box), then apply the listed path - the crumb names current.path
jsq '(()=>{const b = Array.from(document.querySelectorAll(".ws-pop .ws-dir")).find(x => (x.textContent || "").includes("home")); if (!b) return 0; b.click(); return 1;})()' >/dev/null 2>&1
wait_js '(()=>{const c = document.querySelector(".ws-pop .ws-crumb"); return !!c && c.getAttribute("title") === "/home";})()' 10
jsq 'document.querySelector(".ws-pop .ws-apply")?.click()' >/dev/null 2>&1
ws=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  ws=$(row_field "$JRN_ID" workspace)
  [ -n "$ws" ] && break
  sleep 1
done
assert_eq "$ws" "/home" "workspace chip wrote the session row"

# --- reload: the folder restores ------------------------------------
fresh_open
open_journey_row "$LABEL"
ws_set=$(jsq 'document.querySelector(".ws-btn .ws-set")?.textContent ?? ""' 2>&1 | tail -1 | deq)
assert_eq "$ws_set" "home" "pinned workspace restored on reopen"

# --- clear: the chip drops the pin ----------------------------------
jsq 'document.querySelector(".ws-btn")?.click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".ws-pop .ws-clear")' 10
jsq 'document.querySelector(".ws-pop .ws-clear")?.click()' >/dev/null 2>&1
cleared=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  cleared=$(row_field "$JRN_ID" workspace)
  [ -z "$cleared" ] && break
  sleep 1
done
assert_eq "$cleared" "" "workspace clear removed the pin"
unset_chip=$(jsq '!!document.querySelector(".ws-btn .ws-unset")' 2>&1 | tail -1)
assert_eq "$unset_chip" "true" "chip renders the unset state after clear"

assert_no_console_errors
step "journey green"