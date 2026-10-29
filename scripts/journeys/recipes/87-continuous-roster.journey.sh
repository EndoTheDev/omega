#!/bin/bash
# 87-continuous-roster - the continuous wake surface, MERGED into the
# Agents tab (t_ba3fc50e, ruling 18; card t_c7222095's one-tab merge).
# One pass across the customer surface:
#
#   seed     - POST /continuous creates a throwaway agent (name,
#              directive, wake schedule) through the product's own
#              create flow; the roster REST row answers with the name
#              and the NEXT WAKE state.
#   dock     - the served webui carries ONE Agents tab and NO
#              Continuous chip (deletability law: the fold, not an
#              alias, and no hidden tab).
#   row      - the seeded agent renders in the Agents tab as ONE merged
#              row: its name, the inline wake state ("next wake ..."
#              from the schedule), and the honest empty preview.
#   detail   - expanding the row opens the wake strip (wake/sleep
#              buttons + the wake message input). The agent is NEVER
#              woken - a browser journey does not spend provider turns.
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
printf '%s' "$resp" | grep -q "\"created\": ?\"${CONT_NAME}\"\|\"created\":\"${CONT_NAME}\"\|\"created\": \"${CONT_NAME}\"" || \
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

# the merged dock: ONE Agents chip, and the Continuous chip renders
# NOWHERE (card t_c7222095's deletability law - the fold, not an alias).
# card t_502b8e81: edge shells render ICON-ONLY chips - match by
# aria-label/title, never .dock-tab-title text.
clicked=$(jsq '(() => {
  const tabs=[...document.querySelectorAll(".dock-tab")];
  const chip=(t) => ((t.getAttribute("aria-label")||"")||t.title||t.querySelector(".dock-tab-title")?.textContent||"").trim();
  if (tabs.some(t => chip(t)==="Continuous")) return -2;
  if (tabs.filter(t => chip(t)==="Agents").length !== 1) return -1;
  const t=tabs.find(x => chip(x)==="Agents");
  t.click();
  return 1;
})()' 2>&1 | tail -1)
[ "$clicked" = "1" ] || die "the dock does not present ONE Agents tab and no Continuous chip ($clicked)"
wait_js '!!document.querySelector(".pane.agents")' 10
step "ok: the Agents tab is the one merged surface (no Continuous chip)"

# the merged row renders the seeded agent: name + wake state + preview.
# The rows may carry operator agents anywhere around the seeded row
# (sorted by name - a lane home is never guaranteed jrn-first), so every
# assertion targets THIS journey's row, never the first row.
wait_js "[...document.querySelectorAll('.pane.agents .registry-name')].some(el => el.textContent.trim() === '$CONT_NAME')" 10
step "ok: the Agents list renders the seeded row"

# row_cell <selector>: the seeded row's cell text (the row that carries
# $CONT_NAME, not the first row of the list).
row_cell() {
  jsq '(name => (sel => [...document.querySelectorAll(".pane.agents .registry-row")]
    .find(r => [...r.querySelectorAll(".registry-name")].some(n => n.textContent.trim() === name))
    ?.querySelector(sel)?.textContent?.trim() || "")("'"$1"'"))("'"$CONT_NAME"'")' 2>&1 | tail -1 | tr -d '"'
}
state=$(row_cell ".agents-badge")
case "$state" in
  "next wake"*) state_is_wake=1 ;;
  *) state_is_wake=0 ;;
esac
assert_eq "$state_is_wake" "1" "the merged row shows the wake state (got: [$state])"
preview=$(row_cell ".registry-preview")
assert_eq "$preview" "- no report yet -" "the fresh row shows the honest empty preview"

# --- the wake strip opens on expand (never wakes) ---
jsq '(name => [...document.querySelectorAll(".pane.agents .registry-row")]
  .find(r => [...r.querySelectorAll(".registry-name")].some(n => n.textContent.trim() === name))
  ?.querySelector(".registry-expander")?.click())("'"$CONT_NAME"'")' >/dev/null 2>&1
wait_js 'document.querySelectorAll(".pane.agents .agents-cont-wake, .pane.agents .agents-cont-sleep").length >= 2' 10 >/dev/null 2>&1
wakes=$(jsq 'document.querySelectorAll(".pane.agents .agents-cont-wake, .pane.agents .agents-cont-sleep").length' 2>&1 | tail -1)
assert_eq "$wakes" "2" "the expanded row carries the wake/sleep buttons"
msg=$(jsq '!!document.querySelector(".pane.agents .agents-cont-message")' 2>&1 | tail -1)
assert_eq "$msg" "true" "the wake message input rides the strip"
step "ok: wake strip opened for $CONT_NAME"

assert_no_console_errors
step "GREEN: continuous roster (merged Agents tab) journey"