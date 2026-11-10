#!/bin/bash
# 110-settings-scopes - the settings SCOPE TABS (settings tier 3b, card
# t_04eb11d6) as they read under the tier-4 MASTER-DETAIL bones (card
# t_af7e1150: the accordion is gone - a category nav on the left, one
# category's knobs on the right - so this recipe CLICKS nav categories to
# reach a domain's rows instead of scrolling one long accordion).
#
#   strip    - an agent home on the lane materializes the VSCode tab row at
#              the head of the CONTENT pane (tier-4 ruling 11), structurally
#              outside the scrollport; it is the same list the payload's
#              scopes carry (R6 placement, re-seated by tier 4).
#   render   - clicking the agent tab switches the WHOLE pane: the nav
#              carries exactly the four overridable categories (provider,
#              system prompt, description, max turns) - no Commonly Used,
#              no chat client state, no other registry domain (R3).
#   dots     - a knob the agent's own layer names carries the dot AND the
#              reset affordance, and the dot names its base as MAIN; a
#              knob the layer does not name inherits main and carries
#              neither (the seam ruling: differs-from-main, reset =
#              override removal).
#   search   - the filter on an agent tab narrows within those four
#              categories only (R7) and GREYS the categories with no hit
#              (tier-4 ruling 6), while Main keeps its own query.
#   main     - back on Main the pane is untouched: the curated Commonly
#              Used category and the chat category are in the nav.
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
    empty=$(jsq '(()=>{return document.querySelector(".settings-scopes")?1:0})()' 2>&1 | tr -d '"')
    assert_eq "$empty" "0" "R8 empty world: zero scopes renders NO strip at all"
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

# placement (tier-4 ruling 11: the strip is the head of the CONTENT pane -
# inside .settings-content, outside the scrollport, above the content bar)
placement=$(jsq '(()=>{const s=document.querySelector(".settings-scopes"),c=document.querySelector(".settings-content"),b=document.querySelector(".settings-body"),bar=document.querySelector(".settings-content-bar");if(!s||!c||!b||!bar) return "missing";const inside=c.contains(s),outside=!b.contains(s);const before=(s.compareDocumentPosition(bar)&Node.DOCUMENT_POSITION_FOLLOWING)!==0;return inside&&outside&&before?"head":"misplaced"})()' 2>&1 | tr -d '"')
assert_eq "$placement" "head" "the strip heads the CONTENT pane, above its bar, outside the scrollport"

# --- the agent tab: exactly the four overridable categories (R3) ---
clicked=$(jsq "(()=>{const t=[...document.querySelectorAll('.settings-scopes .settings-scope-tab')].find(b=>(b.textContent||'').trim()==='$AGENT');if(!t) return 0;t.click();return 1})()" 2>&1 | tr -d '"')
assert_eq "$clicked" "1" "the agent tab is clickable"

if ! wait_js "(()=>{const g=[...document.querySelectorAll('.settings-nav-cat')].map(e=>(e.getAttribute('data-category')||''));return g.length===4&&g[0]==='provider'})()" 15; then
  die "agent tab never rendered the four scope categories"
fi
cats=$(jsq '(()=>{return [...document.querySelectorAll(".settings-nav-cat .settings-nav-label")].map(e=>(e.textContent||"").trim()).join(",")})()' 2>&1 | tr -d '"')
assert_eq "$cats" "provider,system prompt,description,max turns" "the agent tab's nav carries ONLY the four overridable categories"

# the main-scope surfaces never ride an agent tab: not the curated
# category, not the chat client-state category
leak=$(jsq '(()=>{const t=[...document.querySelectorAll(".settings-nav-cat,.settings-nav-sub")].map(e=>(e.getAttribute("data-category")||"").toLowerCase());return t.some(x=>x==="common"||x==="chat"||x==="dock"||x.indexOf("chat/")===0)?1:0})()' 2>&1 | tr -d '"')
assert_eq "$leak" "0" "no Commonly Used / chat / dock category on an agent tab"

# --- dots: one override set (dot + reset), one inherited knob (neither).
# Master-detail renders ONE category at a time (tier-4 ruling 1), so the
# recipe clicks the category a row lives in - the same rows as before.
navclick() {
  jsq "(()=>{const b=[...document.querySelectorAll('.settings-nav-cat,.settings-nav-sub')].find(x=>x.getAttribute('data-category')==='$1');if(!b) return 0;b.click();return 1})()" 2>&1 | tr -d '"'
}
rows_now() {
  jsq '(()=>{const out=[];for(const r of document.querySelectorAll(".setting-item")){const lab=(r.querySelector(".setting-head > span")?.textContent||"").trim();const d=r.querySelector(".setting-dot");const rs=r.querySelector(".setting-reset");out.push(lab+"="+(d?d.getAttribute("aria-label"):"none")+(rs?"+reset":""))}return out.join(";")})()' 2>&1 | tr -d '"'
}
title_now() {
  jsq '(()=>{return (document.querySelector(".settings-content-title")||{textContent:""}).textContent.trim()})()' 2>&1 | tr -d '"'
}

assert_eq "$(navclick provider)" "1" "the provider category is in the agent nav"
if ! wait_js "(()=>{return !!document.querySelector('.setting-item')})()" 10; then
  die "the provider category never rendered its rows"
fi
rows=$(rows_now)
case ",$rows," in
  *"model name=differs from main+reset"*) step "ok: the overridden model row carries the dot + reset, and the dot names main as its base" ;;
  *) die "the overridden model row lost its dot/reset (rows: $rows)" ;;
esac

assert_eq "$(navclick max_turns)" "1" "the max turns category is clickable"
if ! wait_js "(()=>{const t=((document.querySelector('.settings-content-title')||{}).textContent||'').trim();return t.indexOf('max turns')===0&&!!document.querySelector('.setting-item')})()" 10; then
  die "the max turns category never rendered its row (title: $(title_now))"
fi
rows=$(rows_now)
case ",$rows," in
  *"max turns=none"*) step "ok: the inherited max turns row carries no dot (it IS main's value)" ;;
  *) die "the inherited max turns row lit a dot (rows: $rows)" ;;
esac

assert_eq "$(navclick description)" "1" "the description category is clickable"
if ! wait_js "(()=>{const t=((document.querySelector('.settings-content-title')||{}).textContent||'').trim();return t.indexOf('description')===0&&!!document.querySelector('.setting-item')})()" 10; then
  die "the description category never rendered its row (title: $(title_now))"
fi
rows=$(rows_now)
case ",$rows," in
  *"description=differs from main+reset"*) step "ok: the seeded description override reads as its own row" ;;
  *) die "the seeded description override lost its marker (rows: $rows)" ;;
esac
# every dot on this tab speaks the agent vocabulary
bases=$(jsq '(()=>{const d=[...document.querySelectorAll(".setting-dot")].map(e=>e.getAttribute("aria-label"));return d.every(x=>x==="differs from main")?1:0})()' 2>&1 | tr -d '"')
assert_eq "$bases" "1" "every dot names MAIN as the base on an agent tab"

# --- R4: ONE write path - a pane edit lands through the agents endpoint ---
set_desc=$(jsq '(()=>{const r=[...document.querySelectorAll(".setting-item")].find(x=>((x.querySelector(".setting-head > span")?.textContent||"").trim())==="description");if(!r) return 0;const i=r.querySelector("input");if(!i) return 0;i.focus();i.value="journey edited description";i.dispatchEvent(new Event("input",{bubbles:true}));return 1})()' 2>&1 | tr -d '"')
assert_eq "$set_desc" "1" "the description row accepts an edit"
if ! wait_js '(()=>{const b=document.querySelector(".settings-save");return !!b&&(b.textContent||"").trim()==="save 1 change"})()' 10; then
  die "the save row never counted the edit"
fi
step "ok: the dirty edit surfaces one save (no second write path)"
saved=$(jsq '(()=>{const b=document.querySelector(".settings-save");if(!b) return 0;b.click();return 1})()' 2>&1 | tr -d '"')
assert_eq "$saved" "1" "the save button is clickable"
if ! wait_js '(()=>{return !document.querySelector(".settings-save-row")})()' 20; then
  die "the save row never cleared after the write (refused or stuck)"
fi
step "ok: the save landed and the pane re-read the scope"
onfile=$(cat "$SEED_DIR/config/config.yaml" 2>/dev/null)
case "$onfile" in
  *"journey edited description"*) step "ok: the edit reached the agent's own layer file (the endpoint, not the pane, wrote it)" ;;
  *) die "the edit never reached $SEED_DIR/config/config.yaml: $onfile" ;;
esac
case "$onfile" in
  *"jrn-model-7b"*) step "ok: the untouched provider override survived the write" ;;
  *) die "the write clobbered the neighbouring override: $onfile" ;;
esac
# and the pane still names main as the base - no 409 lock, no error line
locked=$(jsq '(()=>{return document.querySelector(".settings-error")?1:0})()' 2>&1 | tr -d '"')
assert_eq "$locked" "0" "the write did not lock the tab"

# --- R7: the search on an agent tab filters its four categories only,
# and tier 4 GREYS the nav instead of hiding it (ruling 6) ---
ab fill '.settings-filterbar input' "model" >/dev/null 2>&1 || die "search input never accepted text"
if ! wait_js "(()=>{const s=[...document.querySelectorAll('.settings-category')].map(x=>x.getAttribute('data-section'));return s.length===1&&s[0]==='provider'})()" 10; then
  die "agent-tab search did not narrow the content pane to the provider hits"
fi
step "ok: agent-tab search narrows within the four categories (provider only)"
hits=$(jsq '(()=>{return [...document.querySelectorAll(".setting-item .setting-head > span")].map(e=>(e.textContent||"").trim()).filter(Boolean).join(",")})()' 2>&1 | tr -d '"')
case ",$hits," in
  *,model\ name,*) step "ok: the model row is the surviving hit" ;;
  *) die "the search hit list lost the model row (hits: $hits)" ;;
esac
# the nav keeps its four entries and greys the three with no hit
navshape=$(jsq '(()=>{const all=document.querySelectorAll(".settings-nav-cat").length;const dim=document.querySelectorAll(".settings-nav-cat.dim").length;return all+"|"+dim})()' 2>&1 | tr -d '"')
assert_eq "$navshape" "4|3" "the nav keeps its shape (4 entries) and greys the 3 with no hit"

# --- back to Main: per-scope search + the tier-2b pane is untouched ---
clicked=$(jsq "(()=>{const t=[...document.querySelectorAll('.settings-scopes .settings-scope-tab')].find(b=>(b.textContent||'').trim()==='Main');if(!t) return 0;t.click();return 1})()" 2>&1 | tr -d '"')
assert_eq "$clicked" "1" "the Main tab is clickable"
if ! wait_js "(()=>{const g=[...document.querySelectorAll('.settings-nav-cat')].map(e=>(e.getAttribute('data-category')||'').toLowerCase());return g.includes('common')&&g.includes('chat')})()" 15; then
  die "Main never came back to the tier-2b pane (Commonly Used + chat)"
fi
step "ok: Main keeps its curated category and chat state"
mainq=$(jsq '(()=>{const i=document.querySelector(".settings-filterbar input");return i?i.value:""})()' 2>&1 | tr -d '"')
assert_eq "$mainq" "" "per-scope search: Main's query is its own (empty), never the agent tab's"

step "110-settings-scopes green"
