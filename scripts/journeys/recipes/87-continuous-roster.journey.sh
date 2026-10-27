#!/bin/bash
# 87-continuous-roster - the continuous roster browser journey
# (t_ba3fc50e, ruling 18). One pass across the customer surface:
#
#   seed     - POST /continuous creates a throwaway agent (name,
#              directive, wake schedule) through the product's own
#              create flow; the roster REST row answers with the name
#              and the NEXT WAKE state.
#   picker   - the sidebar new-session type picker opens and its
#             Continuous entry opens the roster pane (t_55a571ff
#             placeholder wired by t_72ecca67).
#   roster   - the seeded row renders in the served webui: its name,
#              and its wake state ("next wake ..." from the schedule).
#   detail   - clicking the row opens the detail strip (canonical chat
#              + the wake/sleep buttons). The agent is NEVER woken - a
#              browser journey does not spend provider turns.
#
# Residue-free: the seeded name carries the jrn- prefix and the EXIT
# trap removes its home folder - ruling 2 makes folder presence the
# registration, so removing the folder IS the uninstall (the canonical
# store, crons.yaml and the schedule all live inside it).
. "${JROOT:?run must export JROOT}/lib.sh"

runtag=$(date +%d%H%M%S)
printf '%s' "$runtag" > "${JRN_TAGFILE:-/tmp/omega-jrn-runtag}"
CONT_NAME="jrn-${runtag}-roster"
JRN_ID="$CONT_NAME" # cleanup_journey_sessions' fast path is a no-op; the trap below owns the home

# seeded_dir: capture the agent home's absolute dir from the roster row
# (the lane's OMEGA_HOME is the caller's business) once into globals.
clean_seeded_agent() {
  if [ -n "${SEED_DIR:-}" ]; then
    case "$SEED_DIR" in
      */agents/jrn-*) rm -rf "$SEED_DIR" ;;
      *) : ;; # a malformed dir is never rm'd blind
    esac
  fi
}
trap 'clean_seeded_agent; cleanup_journey_sessions' EXIT

# --- seed: the product's create flow ---
resp=$(curl -s -X POST "${JOURNEY_BASE_URL}/continuous" \
       -H 'Content-Type: application/json' \
       -d "{\"name\": \"${CONT_NAME}\", \"directive\": \"(journey) roster seed agent\", \"schedule\": \"17 8 * * *\"}")
printf '%s' "$resp" | grep -q "\"created\": ?\"${CONT_NAME}\"\|\"created\":\"${CONT_NAME}\"" || \
  die "POST /continuous failed: $resp"
step "ok: created $CONT_NAME"

# --- the roster REST row answers with name + wake state ---
roster=$(curl -s "${JOURNEY_BASE_URL}/continuous/roster")
row=$(printf '%s' "$roster" | python3 -c "
import json,sys
rows=json.load(sys.stdin)
print(json.dumps([r for r in rows if r.get('name')=='$CONT_NAME']))
" 2>/dev/null)
printf '%s' "$row" | grep -q "$CONT_NAME" || die "roster row for $CONT_NAME missing: $roster"
next=$(printf '%s' "$row" | grep -oE '"next_wake": ?"[^"]*"' | head -1)
case "$next" in
  *'T'*) ;; # an ISO timestamp means the schedule produced a next wake
  *) die "roster row lost the next wake: $row" ;;
esac
SEED_DIR=$(printf '%s' "$row" | python3 -c "
import json,sys
(rows,) = json.load(sys.stdin)
print(rows.get('dir') or '')" 2>/dev/null)
[ -n "$SEED_DIR" ] || die "roster row lost the dir: $row"
step "ok: roster row answers (dir $SEED_DIR)"

# --- the served webui ---
open_app

# the picker: the caret opens the type menu, the Continuous entry is wired
jsq 'document.querySelector(".new-caret")?.click()' >/dev/null 2>&1
clicked=$(jsq '(() => {
  const b=[...document.querySelectorAll(".new-menu-item")].find(x => x.textContent.trim()==="Continuous");
  if (!b) return 0;
  if (b.disabled) return -1;
  b.click();
  return 1;
})()' 2>&1 | tail -1)
[ "$clicked" = "1" ] || die "the picker's Continuous entry never clicked ($clicked)"
wait_js '!!document.querySelector(".pane.continuous")' 10
step "ok: picker Continuous entry opened the roster pane"

# the roster renders the seeded row: name + wake state
wait_js "document.querySelector('.pane.continuous .continuous-row-name')?.textContent?.trim() === '$CONT_NAME'" 10
state=$(jsq 'document.querySelector(".pane.continuous .continuous-row-status")?.textContent?.trim() || ""' 2>&1 | tail -1 | tr -d '"')
case "$state" in
  "next wake"*) state_is_wake=1 ;;
  *) state_is_wake=0 ;;
esac
assert_eq "$state_is_wake" "1" "row shows the wake state (got: [$state])"
preview=$(jsq 'document.querySelector(".pane.continuous .continuous-row-preview")?.textContent?.trim() || ""' 2>&1 | tail -1 | tr -d '"')
assert_eq "$preview" "- no report yet -" "fresh agent row shows the honest empty preview"

# --- the detail strip opens on click (never wakes) ---
jsq 'document.querySelector(".pane.continuous .continuous-row")?.click()' >/dev/null 2>&1
wait_js "document.querySelector('.continuous-detail-title')?.textContent?.trim() === '$CONT_NAME'" 10
jsq '!!document.querySelector(".continuous-wake")' >/dev/null 2>&1
wakes=$(jsq 'document.querySelectorAll(".continuous-wake, .continuous-sleep").length' 2>&1 | tail -1)
assert_eq "$wakes" "2" "detail strip carries the wake/sleep buttons"
step "ok: detail strip opened for $CONT_NAME"

assert_no_console_errors
step "GREEN: continuous roster journey"