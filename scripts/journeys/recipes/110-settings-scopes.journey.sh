#!/bin/bash
# 110-settings-scopes - the settings SCOPE TABS (settings tier 3b, card
# t_04eb11d6): the agent half of the settings payload has a face.
#
#   strip    - an agent home on the lane materializes the VSCode tab row
#              at the head of the pane, [Main | <agent>], structurally
#              above the filter bar and OUTSIDE the scrollport; it is the
#              same list the payload's scopes carry (R6 placement).
#   render   - clicking the agent tab switches the WHOLE pane: exactly the
#              four overridable groups (provider, system prompt,
#              description, max turns) - no Commonly Used, no chat client
#              state, no other registry group (R3).
#   dots     - a knob the agent's own layer names carries the dot AND the
#              reset affordance, and the dot names its base as MAIN; a
#              knob the layer does not name inherits main and carries
#              neither (the seam ruling: differs-from-main, reset =
#              override removal).
#   search   - the filter on an agent tab narrows within those four groups
#              only (R7), while Main keeps its own query (per-scope search:
#              switching back finds Main unfiltered).
#   main     - back on Main the tier-2b pane is untouched: the curated
#              Commonly Used group and the chat group are there.
#
# Residue-free: the seeded home carries the jrn- prefix, so the suite's
# own reaper (cleanup_journey_residue, card t_a99ffcd7) owns it; the EXIT
# trap also removes it directly in case this recipe dies mid-way.
. "${JROOT:?run must export JROOT}/lib.sh"

runtag=$(date +%d%H%M%S)
AGENT="jrn-${runtag}-scope"

# the lane home: OMEGA_HOME wins, else derived from OMEGA_BIN - the same
# contract as 60-settings-themes and 92-registry-toggles. Both unset dies
# loudly rather than seeding into a guess.
if [ -z "${OMEGA_HOME:-}" ]; then
  [ -n "${OMEGA_BIN:-}" ] || die "set OMEGA_HOME or OMEGA_BIN to the serving lane home"
  OMEGA_HOME="$(dirname "$OMEGA_BIN")"
fi
SEED_DIR="$OMEGA_HOME/agents/$AGENT"

cleanup_seed() {
  case "${SEED_DIR:-}" in
    */agents/jrn-*) rm -rf "$SEED_DIR" ;;
    *) : ;;
  esac
}
trap 'cleanup_seed; cleanup_journey_residue' EXIT

# --- seed: one agent home whose layer names exactly two of the four
# knobs. Folder presence IS the registration (the roster scan); the layer
# file is the override. max_turns and system_prompt stay absent, so they
# answer the inherited main value with overridden=false - the receipt's
# "inherited knob (no dot)".
#
# ORDER MATTERS: the pane is opened BEFORE the seed so a lane that carries
# no agent home can also prove R8 live (zero scopes = no strip at all).
# The wire is the guard, not an assumption - a lane with real agents skips
# the empty-world step loudly instead of asserting a lie.
pre_scopes=$(curl -s "$JOURNEY_BASE_URL/settings" | grep -o '"scopes":\[[^]]*\]' | head -1)
step "pre-seed wire: ${pre_scopes:-<none>}"

open_app

# --- open Settings from the menubar gear (no inspector chip exists) ---
GEAR_POLL='(()=>{const b=document.querySelector("header.menubar button[aria-label=\"Open settings\"]");if(!b) return false;b.click();return true})()'
if ! poll_js "$GEAR_POLL" 15; then
  die "settings menubar gear never rendered"
fi
step "ok: Settings opens from the menubar gear"

case "${pre_scopes:-}" in
  '"scopes":[]')
    if ! poll_js '(()=>{return !!document.querySelector(".settings-filterbar input")})()' 15; then
      die "settings pane never rendered its filter bar"
    fi
    empty=$(jsq '(()=>{return document.querySelector(".settings-scopes")?"strip":"none"})()' 2>&1 | tr -d '"')
    assert_eq "$empty" "none" "R8 empty world: zero scopes renders NO strip at all"
    SEEDED_LATE=1
    ;;
  *)
    step "note: lane already carries scopes - the R8 empty-world step is skipped (wire is not [])"
    SEEDED_LATE=0
    ;;
esac

mkdir -p "$SEED_DIR/config"
cat > "$SEED_DIR/config/config.yaml" <<YAML
# (journey) settings scope seed - card t_04eb11d6
description: journey scope helper
provider:
  model_name: jrn-model-7b
YAML
[ -f "$SEED_DIR/config/config.yaml" ] || die "seed layer file never landed"
step "ok: seeded agent home $SEED_DIR"

# the payload is the tab strip's one source: prove the wire answers the
# scope before blaming the pane for it
wire=$(curl -s "$JOURNEY_BASE_URL/settings")
printf '%s' "$wire" | grep -q "\"name\":\"$AGENT\"" || die "GET /settings carries no scope $AGENT: $(printf '%s' "$wire" | head -c 400)"
step "ok: GET /settings carries the seeded scope"

# the pane's dots derive from THIS payload: a scope that reports an error
# (a layer the host could not read) or answers different flags would make
# the dot assertions below blame the wrong layer. Fail here, naming which.
ovr=$(printf '%s' "$wire" | AGENT="$AGENT" python3 -c '
import json, os, sys
name = os.environ["AGENT"]
scopes = json.load(sys.stdin).get("scopes") or []
s = next((x for x in scopes if x.get("name") == name), None)
if s is None:
    print("missing"); raise SystemExit
if s.get("error"):
    print("error: " + str(s["error"])); raise SystemExit
knobs = {k.get("key"): k for g in (s.get("groups") or []) for k in (g.get("knobs") or [])}
want = {"provider.model_name": True, "description": True, "max_turns": False, "system_prompt": False}
got = {k: bool(knobs.get(k, {}).get("overridden")) for k in want}
print("flags-ok" if got == want else "flags-bad: " + json.dumps(got) + " values: " + json.dumps({k: knobs.get(k, {}).get("value") for k in want}))
')
assert_eq "$ovr" "flags-ok" "the wire marks exactly the seeded knobs overridden (provider.model_name + description)"

if [ "${SEEDED_LATE:-0}" = "1" ]; then
  # the SPA fetched its payload before the home existed: a reload re-mounts
  # and re-reads, which is exactly the customer's path after a new agent
  ab reload >/dev/null 2>&1 || die "reload failed"
  if ! poll_js "$GEAR_POLL" 15; then
    die "settings menubar gear never re-rendered after reload"
  fi
fi

# --- strip: the tab row, Main first, the seeded agent second ---
if ! poll_js "(()=>{return !!document.querySelector('.settings-scopes .settings-scope-tab')})()" 15; then
  die "scope strip never materialized for a payload that carries a scope (R6/R8)"
fi
tabs=$(jsq '(()=>{return [...document.querySelectorAll(".settings-scopes .settings-scope-tab")].map(b=>(b.textContent||"").trim()).join(",")})()' 2>&1 | tr -d '"')
assert_eq "$tabs" "Main,$AGENT" "the strip reads [Main | $AGENT]"

# placement: the strip is the HEAD of the pane - above the filter bar, and
# both sit outside the scrollport (structurally pinned, not scrolled away)
placement=$(jsq '(()=>{const s=document.querySelector(".settings-scopes"),f=document.querySelector(".settings-filterbar"),b=document.querySelector(".settings-body");if(!s||!f||!b) return "missing";const before=(s.compareDocumentPosition(f)&Node.DOCUMENT_POSITION_FOLLOWING)!==0;const outside=!b.contains(s)&&!b.contains(f);return before&&outside?"head":"misplaced"})()' 2>&1 | tr -d '"')
assert_eq "$placement" "head" "the strip heads the pane, above the filter, outside the scrollport"

# --- the agent tab: exactly the four overridable groups (R3) ---
clicked=$(jsq "(()=>{const t=[...document.querySelectorAll('.settings-scopes .settings-scope-tab')].find(b=>(b.textContent||'').trim()==='$AGENT');if(!t) return 0;t.click();return 1})()" 2>&1 | tr -d '"')
assert_eq "$clicked" "1" "the agent tab is clickable"

if ! wait_js "(()=>{const g=[...document.querySelectorAll('.settings-group-head .settings-group-title')].map(e=>(e.textContent||'').trim());return g.length===4&&g[0]==='provider'})()" 15; then
  die "agent tab never rendered the four scope groups"
fi
groups=$(jsq '(()=>{return [...document.querySelectorAll(".settings-group-head .settings-group-title")].map(e=>(e.textContent||"").trim()).join(",")})()' 2>&1 | tr -d '"')
assert_eq "$groups" "provider,system prompt,description,max turns" "the agent tab renders ONLY the four overridable groups"

# the main-scope surfaces never ride an agent tab: not the curated group,
# not the chat client-state group
leak=$(jsq '(()=>{const t=[...document.querySelectorAll(".settings-group-head .settings-group-title")].map(e=>(e.textContent||"").trim().toLowerCase());return t.some(x=>x==="commonly used"||x==="chat"||x==="dock")?"leak":"clean"})()' 2>&1 | tr -d '"')
assert_eq "$leak" "clean" "no Commonly Used / chat / dock group on an agent tab"

# --- dots: one override set (dot + reset), one inherited knob (neither) ---
rows=$(jsq '(()=>{const out=[];for(const r of document.querySelectorAll(".setting-item")){const lab=(r.querySelector(".setting-head span")?.textContent||"").trim();const d=r.querySelector(".setting-dot");const rs=r.querySelector(".setting-reset");out.push(lab+"="+(d?d.getAttribute("aria-label"):"none")+(rs?"+reset":""))}return out.join(";")})()' 2>&1 | tr -d '"')
case ",$rows," in
  *"model name=differs from main+reset"*) step "ok: the overridden model row carries the dot + reset, and the dot names main as its base" ;;
  *) die "the overridden model row lost its dot/reset (rows: $rows)" ;;
esac
case ",$rows," in
  *"max turns=none"*) step "ok: the inherited max turns row carries no dot (it IS main's value)" ;;
  *) die "the inherited max turns row lit a dot (rows: $rows)" ;;
esac
case ",$rows," in
  *"description=differs from main+reset"*) step "ok: the seeded description override reads as its own row" ;;
  *) die "the seeded description override lost its marker (rows: $rows)" ;;
esac
# every dot on this tab speaks the agent vocabulary
bases=$(jsq '(()=>{const d=[...document.querySelectorAll(".setting-dot")].map(e=>e.getAttribute("aria-label"));return d.every(x=>x==="differs from main")?"main-base":"wrong-base"})()' 2>&1 | tr -d '"')
assert_eq "$bases" "main-base" "every dot names MAIN as the base on an agent tab"

# --- R7: the search on an agent tab filters its four groups only ---
ab fill '.settings-filterbar input' "model" >/dev/null 2>&1 || die "search input never accepted text"
if ! wait_js "(()=>{const g=[...document.querySelectorAll('.settings-group-head .settings-group-title')];return g.length===1&&(g[0].textContent||'').trim()==='provider'})()" 10; then
  die "agent-tab search did not narrow to the provider group alone"
fi
step "ok: agent-tab search narrows within the four groups (provider only)"
hits=$(jsq '(()=>{return [...document.querySelectorAll(".setting-item .setting-head span")].map(e=>(e.textContent||"").trim()).join(",")})()' 2>&1 | tr -d '"')
case ",$hits," in
  *,model\ name,*) step "ok: the model row is the surviving hit" ;;
  *) die "the search hit list lost the model row (hits: $hits)" ;;
esac

# --- back to Main: per-scope search + the tier-2b pane is untouched ---
clicked=$(jsq "(()=>{const t=[...document.querySelectorAll('.settings-scopes .settings-scope-tab')].find(b=>(b.textContent||'').trim()==='Main');if(!t) return 0;t.click();return 1})()" 2>&1 | tr -d '"')
assert_eq "$clicked" "1" "the Main tab is clickable"
if ! wait_js "(()=>{const g=[...document.querySelectorAll('.settings-group-head .settings-group-title')].map(e=>(e.textContent||'').trim().toLowerCase());return g.includes('commonly used')&&g.includes('chat')})()" 15; then
  die "Main never came back to the tier-2b pane (Commonly Used + chat)"
fi
step "ok: Main keeps its curated group and chat state"
mainq=$(jsq '(()=>{const i=document.querySelector(".settings-filterbar input");return i?i.value:""})()' 2>&1 | tr -d '"')
assert_eq "$mainq" "" "per-scope search: Main's query is its own (empty), never the agent tab's"

step "110-settings-scopes green"
