#!/bin/bash
# 92-registry-toggles - the skills/tools/agents toggle round-trip journey
# (card t_11f65e89, journey-upkeep follow-up to t_372b9e9a's v0.14.1
# registry-browser).
#
# The registry-browser shipped without a journey recipe; this pins the
# customer-level contract on the skills tab and the /disabled wire:
#
#   a  fold persistence  - a flipped toggle lands in config.yaml's
#                          `disabled:` fold (the lane home the serve
#                          process owns; same key boot reads, Q7's one
#                          write path) and in GET /disabled
#   b  persisted render  - after a page reload the row STILL shows the
#                          off state: the fold on disk drives the render,
#                          not a memory of the click
#   c  refusal           - an unknown-name PUT is refused (404, plain
#                          "refused" text) and persists NOTHING; the
#                          default agent (main home) disable is refused
#                          in the BROWSER with a visible error row
#                          (409 -> .registry-error) instead of a silent
#                          no-op
#   d  re-enable         - the fold entry is gone: config.yaml, the
#                          wire, and the render all agree it is on again
#
# Lane-home scoped by law (the journey suite law): the seed skill is
# written into $OMEGA_HOME/skills/<name>/SKILL.md - OMEGA_HOME is
# REQUIRED from the caller (the lane-local suite and the nightly
# runner both export it); the recipe has no default. ListSkills
# re-scans per listing, so the
# seed shows up in GET /skills without a restart. Residue-free: the
# cleanup trap deletes the seed dir and, if a red left the fold dirty,
# PUTs the re-enable (an honest no-op when absent).
#
# Drive-by-aria-label (worker-onboarding law): the refs invalidate each
# snapshot; the toggles are clicked by their aria-label state machine
# ("disable X" -> "confirm apply to X?" -> "enable X").
. "${JROOT:?run must export JROOT}/lib.sh"

SKILL="jrn-tgl$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"
GHOST="jrn-ghost-${SKILL#jrn-tgl}"
OMEGA_HOME="${OMEGA_HOME:?set OMEGA_HOME to the serving lane home}"
CONFIG="$OMEGA_HOME/config/config.yaml"
SKILL_DIR="$OMEGA_HOME/skills/$SKILL"
AGENTS_PANE='document.querySelector(".pane.agents")'

# disabled_names <kind>: the folded names on the wire, JSON array of
# strings (python - only stdlib).
disabled_names() {
  curl -s "$JOURNEY_BASE_URL/disabled" | python3 -c '
import json, sys
print(" ".join((json.load(sys.stdin) or {}).get(sys.argv[1], [])))' "$1" 2>/dev/null
}

# fold_has <name>: non-zero unless config.yaml's `disabled:` fold
# carries the name. Go-yaml writes the flat key list under the fold; a
# bare grep of the whole file would match an unrelated top-level use of
# the name, so the scan stays inside the block (its end = the next
# top-level key).
fold_has() {
  # a missing CONFIG is "fold has no entries" (nothing written yet),
  # not an awk error - fresh runner lanes have no config.yaml until
  # the first toggle write
  [ -f "$CONFIG" ] || return 1
  if awk -v want="$1" '
    fold && found { next }
    /^disabled:/ { fold = 1; next }
    fold && /^[A-Za-z_]/ { fold = 0 }
    fold && $0 ~ "^[[:space:]]*- " want "[[:space:]]*$" { found = 1 }
    END { exit (found ? 0 : 1) }
  ' "$CONFIG"; then
    return 0
  fi
  # Go-yaml node round-trips keep the document's original style: a
  # config.yaml that sits as a bare {} (the all-on rewrite after a
  # re-enable) re-serializes the fold INLINE - {disabled: {skills:
  # [name]}} - and the block scan above cannot read it. Read the flow
  # shape too, scoped to the disabled: stretch of its line.
  python3 - "$CONFIG" "$1" <<'PY'
import re, sys
text = open(sys.argv[1]).read()
want = re.escape(sys.argv[2])
for line in text.splitlines():
    i = line.find("disabled:")
    if i < 0:
        continue
    rest = line[i + len("disabled:"):]
    if re.search(r'[\[{,]\s*"?' + want + r'"?\s*[,}\]]', rest):
        sys.exit(0)
    sys.exit(1)
sys.exit(1)
PY
}

# registry_click <kind-selector> <aria-exact> <aria-starts>: click the
# pane's registry toggle matching the aria state machine once. Prints
# 1 when clicked, 0 when not found.
registry_click() {
  jsq "(()=>{const root=document.querySelectorAll('$1 input.registry-toggle');
    const el=[...root].find(b=>{const l=b.getAttribute('aria-label')||'';
    return l==='$2' || l.startsWith('$3')});
    if(!el){return 0}el.click();return 1})()" 2>&1 | tail -1
}

cleanup() {
  # the fold must never keep a journey name: re-enable (no-op if the
  # fold never learned this name) - the wire answers the fresh lists
  curl -s -X PUT "$JOURNEY_BASE_URL/disabled/skills" \
    -H 'Content-Type: application/json' \
    -d "{\"name\": \"$SKILL\", \"enabled\": true}" >/dev/null 2>&1 || true
  rm -rf "$SKILL_DIR" 2>/dev/null || true
}
trap cleanup EXIT

# A missing config/config.yaml is a LEGAL state before the first
# registry fold write (bootstrapHome seeds only config.yaml.example;
# the server materializes config/config.yaml on the first toggle).
[ -d "$OMEGA_HOME" ] || die "OMEGA_HOME does not exist: $OMEGA_HOME (set OMEGA_HOME to the serving lane's home)"

# --- seed: one known skill on the lane home --------------------------------
mkdir -p "$SKILL_DIR" || die "cannot mkdir $SKILL_DIR (OMEGA_HOME not writable?)"
printf -- '---\nname: %s\ndescription: journey seed - registry toggle round-trip\n---\nbody of %s\n' \
  "$SKILL" "$SKILL" > "$SKILL_DIR/SKILL.md" || die "seed write failed"
step "seeded: $SKILL_DIR"

# ListSkills scans live, but give the lane a couple of seconds to the
# next listing; die on never
i=0
while [ "$i" -lt 40 ]; do
  curl -s "$JOURNEY_BASE_URL/skills" | grep -q "\"$SKILL\"" && break
  sleep 0.5
  i=$((i + 1))
done
curl -s "$JOURNEY_BASE_URL/skills" | grep -q "\"$SKILL\"" || \
  die "seeded skill never showed up in GET /skills"
step "ok: seeded skill in the inventory"

# --- boot: the SPA sees the skills tab as the customer opens it ------------
open_app
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1

# the right sidebar (inspector) is open before the tab chip is clicked
side=$(jsq 'document.querySelector(".dock-pane.kind-files")?1:0' 2>/dev/null | tail -1)
if [ "$side" != "1" ]; then
  js 'document.querySelector("[aria-label=\"Toggle right sidebar\"]")?.click()'
  wait_js '(()=>{return document.querySelector(".dock-pane.kind-files")?true:false})()' 5 \
    || die "the right sidebar never opened"
fi

# Tab chips: the sidebar shells are ICON-ONLY strips (t_502b8e81) -
# no .dock-tab-title text, the accessible name rides aria-label. Tab
# matching is by aria-label (text fallback kept for main/bottom chips),
# and the labels are the runtime titles: module dockable.json titles
# override the registry fallback (Skills / Tools / Agents, Title
# Case - 86-dock-round5's rename law voice).
open_tab() {
  local label="$1"
  js "(()=>{const t=[...document.querySelectorAll('.dock-tabbar .dock-tab')].find(e=>{const n=((e.getAttribute('aria-label')||e.textContent||'').trim());return n==='$label'});t&&t.click();return t?1:0})()" >/dev/null
}

open_tab Skills
wait_js '(()=>{return document.querySelector(".skills-list .registry-rows")?true:false})()' 10 \
  || die "the skills tab never rendered rows"
step "ok: skills tab rendered"

# --- the toggle round-trip: off --------------------------------------------
wait_js "(()=>{return [...document.querySelectorAll('.skills-list .registry-row')].some(r=>r.querySelector('.registry-name').textContent.trim()==='$SKILL')})()" 10 \
  || die "the seeded skill's row never rendered"
step "ok: seeded row renders"

# the armed first click: "disable X" -> "confirm apply to X?"
clicked=$(registry_click ".skills-list" "disable $SKILL" "__arm__")
assert_eq "$clicked" "1" "arm click found the toggle"
wait_js "(()=>{return [...document.querySelectorAll('.skills-list input.registry-toggle')].some(b=>(b.getAttribute('aria-label')||'')==='confirm apply to $SKILL?')})()" 5 \
  || die "the toggle never armed (no confirm state)"
clicked=$(registry_click ".skills-list" "__apply__" "confirm apply to $SKILL?")
assert_eq "$clicked" "1" "confirm click applied the toggle"
wait_js "(()=>{return [...document.querySelectorAll('.skills-list input.registry-toggle')].some(b=>(b.getAttribute('aria-label')||'')==='enable $SKILL')})()" 10 \
  || die "the toggle never landed (row never showed the off state)"

# (a) the fold persists: the wire answers it AND config.yaml owns it
i=0
while [ "$i" -lt 20 ]; do
  assert_contained=$(disabled_names skills)
  case "$assert_contained" in *"$SKILL"*) break ;; esac
  sleep 0.5
  i=$((i + 1))
done
case "$(disabled_names skills)" in
  *"$SKILL") ;;
  *) die "GET /disabled lost the disabled skill" ;;
esac
fold_has "$SKILL" || die "config.yaml disabled: fold does not persist $SKILL"
step "ok: (a) fold persisted on the wire and in config.yaml"

# (b) the persisted fold drives the render: reload, re-open, row off
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1
open_tab skills
wait_js "(()=>{const r=[...document.querySelectorAll('.skills-list .registry-row')].find(x=>x.querySelector('.registry-name').textContent.trim()==='$SKILL'); const b=r&&r.querySelector('input.registry-toggle'); return !!r&&r.classList.contains('registry-off')&&!!b&&b.getAttribute('aria-label')==='enable $SKILL'&&!b.checked})()" 10 \
  || die "after reload the seeded row does not render the persisted off state"
step "ok: (b) reload renders the persisted off state"

# --- (c) refusal: unknown name on the wire (all three registries) ----------
# The tabs only list registry names, so an unknown name has no row to
# click: the refusal surface is the API probe this card mirrors from the
# live check on :8098. NOTHING may persist from it.
for ns in skills tools agents; do
  body=$(curl -s -X PUT "$JOURNEY_BASE_URL/disabled/$ns" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"$GHOST\",\"enabled\":false}")
  printf '%s' "$body" | grep -q "toggle refused" || \
    die "$ns unknown-name toggle did not answer a visible refusal text: $body"
  code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$JOURNEY_BASE_URL/disabled/$ns" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"$GHOST\",\"enabled\":false}")
  assert_eq "$code" "404" "$ns unknown-name toggle refused 404"
  case "$(disabled_names "$ns")" in
    *"$GHOST"*) die "$ns fold persisted the ghost name (invisible forever)" ;;
  esac
  if fold_has "$GHOST"; then die "config.yaml fold persisted the ghost name"; fi
done
step "ok: (c) unknown-name toggles refused 404, nothing persisted"

# (c, visible half) the browser-visible refusal: the default agent is
# the main home - no roster to leave, no wake to refuse. The Agents tab
# answers TWO clicks with a refused PUT and the visible error row.
open_tab Agents
wait_js '(()=>{return document.querySelector(".pane.agents .registry-rows")?true:false})()' 10 \
  || die "the Agents tab never rendered rows"
clicked=$(registry_click ".pane.agents" "disable default" "__arm__")
assert_eq "$clicked" "1" "agents default row found"
wait_js "(()=>{return [...document.querySelectorAll('.pane.agents input.registry-toggle')].some(b=>(b.getAttribute('aria-label')||'')==='confirm apply to default?')})()" 5 \
  || die "the agents toggle never armed"
clicked=$(registry_click ".pane.agents" "__apply__" "confirm apply to default?")
assert_eq "$clicked" "1" "agents confirm click applied"
wait_js '(()=>{const r=document.querySelector(".pane.agents .registry-error"); return r && r.textContent.includes("main home")})()' 10 \
  || die "the default-agent disable was not refused with a visible error"
case "$(disabled_names agents)" in *"default"*) die "the default agent landed in the fold (refusal bypassed)" ;; esac
step "ok: (c) browser-visible refusal on the main-home disable"

# --- (d) re-enable clears the fold -----------------------------------------
open_tab Skills
wait_js "(()=>{return [...document.querySelectorAll('.skills-list input.registry-toggle')].some(b=>(b.getAttribute('aria-label')||'')==='enable $SKILL')})()" 10 \
  || die "the skills tab lost the seeded row before the re-enable"
clicked=$(registry_click ".skills-list" "enable $SKILL" "__arm__")
assert_eq "$clicked" "1" "re-enable arm click"
wait_js "(()=>{return [...document.querySelectorAll('.skills-list input.registry-toggle')].some(b=>(b.getAttribute('aria-label')||'')==='confirm apply to $SKILL?')})()" 5 \
  || die "the re-enable never armed"
clicked=$(registry_click ".skills-list" "__apply__" "confirm apply to $SKILL?")
assert_eq "$clicked" "1" "re-enable confirm click"
wait_js "(()=>{return [...document.querySelectorAll('.skills-list input.registry-toggle')].some(b=>(b.getAttribute('aria-label')||'')==='disable $SKILL')})()" 10 \
  || die "the re-enable never landed (row never back on)"
case "$(disabled_names skills)" in
  *"$SKILL"*) die "the fold kept the disabled skill after the re-enable" ;;
esac
fold_has "$SKILL" && die "config.yaml fold kept the disabled skill after the re-enable"
step "ok: (d) re-enable cleared the fold (wire + config.yaml)"

assert_no_console_errors
step "92-registry-toggles GREEN"