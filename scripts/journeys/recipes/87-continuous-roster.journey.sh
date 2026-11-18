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
#              row: its name, the subtitle status (dot + word - the
#              t_2d8deb8c vocabulary, "next wake ..." on the dim line)
#              from the schedule). The under-row preview is dead
#              (t_2d8deb8c ruling 3).
#   detail   - expanding the row opens the wake strip (wake/sleep
#              buttons + the wake message input). The agent is NEVER
#              woken - a browser journey does not spend provider turns.
#   chats    - card t_5df01d0f: TWO seeded agents, one click each. Each
#              agent home carries its own permanent chat, and clicking a
#              row must open THAT agent's chat: the URL follows the
#              clicked agent's own session id, a second tab is minted
#              (no dedupe onto the first agent's), and the accent row
#              follows the open agent. Before the fix both chats carried
#              the bare id "canonical" and the second row opened the
#              first agent's conversation.
#
# Residue-free: the seeded name carries the jrn- prefix and the EXIT
# trap removes its home folder - ruling 2 makes folder presence the
# registration, so removing the folder IS the uninstall (the canonical
# store, crons.yaml and the schedule all live inside it). The trap
# rides cleanup_journey_residue (card t_a99ffcd7), not just the
# captured dir: a die before SEED_DIR lands still reaps the home (the
# prefix glob), and the cron runner's GC sweeps the wake entry out of
# cronstate.json once the home is gone.
. "${JROOT:?run must export JROOT}/lib.sh"

runtag=$(date +%d%H%M%S)
printf '%s' "$runtag" > "${JRN_TAGFILE:-$JOURNEY_WORKTMP/omega-jrn-runtag}"
CONT_NAME="jrn-${runtag}-roster"
JRN_ID="$CONT_NAME" # cleanup_journey_residue's fast path is a no-op; the trap below owns the home

# seeded_dir: capture the agent home's absolute dir from the roster row
# (the lane's OMEGA_HOME is the caller's business) once into globals.
clean_seeded_agent() {
  # card t_5df01d0f: the homes go through the PRODUCT's delete verb
  # first. Serving an agent's chat opens that home's store through the
  # daemon's routing handle, and on windows a held handle makes the data
  # files unremovable ("Device or resource busy") - the delete verb
  # releases it before removing the directory. The rm stays as the
  # fallback for a lane whose /agents route never answered.
  for name in "${CONT_NAME:-}" "${CONT_NAME2:-}"; do
    [ -n "$name" ] || continue
    curl -s -X DELETE "${JOURNEY_BASE_URL}/agents/${name}" >/dev/null 2>&1 || true
  done
  for d in "${SEED_DIR:-}" "${SEED_DIR2:-}"; do
    [ -n "$d" ] || continue
    case "$d" in
      */agents/jrn-*) [ -d "$d" ] && rm -rf "$d" ;;
      *) : ;; # a malformed dir is never rm'd blind
    esac
  done
}
trap 'clean_seeded_agent; cleanup_journey_residue' EXIT

# created_ok <response body> <agent name>: the create flow answered with
# THIS agent's name (the wire's {"created": "<name>"} row).
created_ok() {
  printf '%s' "$1" | python3 -c '
import json,sys
try:
    row = json.load(sys.stdin)
except Exception:
    sys.exit(1)
sys.exit(0 if str(row.get("created") or "") == sys.argv[1] else 1)' "$2" 2>/dev/null
}

# --- seed: the product's create flow ---
resp=$(curl -s -X POST "${JOURNEY_BASE_URL}/continuous" \
       -H 'Content-Type: application/json' \
       -d "{\"name\": \"${CONT_NAME}\", \"directive\": \"(journey) roster seed agent\", \"schedule\": \"17 8 * * *\"}")
created_ok "$resp" "$CONT_NAME" || die "POST /continuous failed: $resp"
step "ok: created $CONT_NAME"

# card t_5df01d0f: a SECOND agent home. Two agent chats are two chats
# only if their session IDs differ, and the id is what the roster row
# opens, the store router resolves and the shell's dedupe keys on - so
# the pair IS the fixture for the collision below. The name differs in
# its first 8 characters (the chat tab title's id prefix) so the two
# chips are told apart by the same text an operator reads.
CONT_NAME2="jrn-b-${runtag}"
resp2=$(curl -s -X POST "${JOURNEY_BASE_URL}/continuous" \
        -H 'Content-Type: application/json' \
        -d "{\"name\": \"${CONT_NAME2}\", \"directive\": \"(journey) second roster agent\", \"schedule\": \"23 9 * * *\"}")
created_ok "$resp2" "$CONT_NAME2" || die "POST /continuous (second agent) failed: $resp2"
step "ok: created $CONT_NAME2"

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
# the second home's dir rides the same roster payload (its row answered by
# the same create flow); captured so the EXIT trap can reap it too
SEED_DIR2=$(printf '%s' "$roster" | python3 -c '
import json,sys
rows = json.load(sys.stdin)
print(next((r.get("dir") or "" for r in rows if r.get("name") == sys.argv[1]), ""))' "$CONT_NAME2" 2>/dev/null)
[ -n "$SEED_DIR2" ] || die "roster lost the second agent's dir: $roster"
step "ok: roster row answers (dirs $SEED_DIR + $SEED_DIR2)"

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
wait_js "[...document.querySelectorAll('.pane.agents .reg-row-name')].some(el => el.textContent.trim() === '$CONT_NAME')" 10
step "ok: the Agents list renders the seeded row"

# row_cell <selector>: the seeded row's cell text (the row that carries
# $CONT_NAME, not the first row of the list).
row_cell() {
  jsq '(name => (sel => [...document.querySelectorAll(".pane.agents .registry-row")]
    .find(r => [...r.querySelectorAll(".reg-row-name")].some(n => n.textContent.trim() === name))
    ?.querySelector(sel)?.textContent?.trim() || "")("'"$1"'"))("'"$CONT_NAME"'")' 2>&1 | tail -1 | tr -d '"'
}
# the t_2d8deb8c vocabulary: the wake state rides the row law's
# SUBTITLE line as the dot + word (green = active, dim = sleeping) -
# the inline badge and the under-row preview died with ruling 3.
state=$(row_cell ".reg-row-status")
case "$state" in
  *"next wake"*) state_is_wake=1 ;;
  *) state_is_wake=0 ;;
esac
assert_eq "$state_is_wake" "1" "the merged row shows the wake state (got: [$state])"
dead=$(row_cell ".registry-preview")
if [ -n "$dead" ]; then die "the under-row preview line is dead (ruling 3) - rendered [$dead]"; fi
deadbadge=$(row_cell ".agents-badge")
if [ -n "$deadbadge" ]; then die "the inline badge is dead (ruling 3) - rendered [$deadbadge]"; fi

# --- card t_db378a0d: the raised chip and the seams it crosses -----------
# HIS fork ruling: the hairlines ABOVE and BELOW a raised agents row fade,
# so the chip never crosses a seam. CSS :hover cannot be faked from eval,
# so the REAL pointer drives it (ab hover), and the law is read back as
# computed styles: rest quiet -> raised with both seams clear -> quiet
# again. The default agent always renders, so two rows are guaranteed.
nrows=$(jsq 'document.querySelectorAll(".pane.agents .registry-row").length' 2>&1 | tail -1)
[ "${nrows:-0}" -ge 2 ] 2>/dev/null || die "the Agents tab rendered $nrows rows - the hover seam law needs two"
RAISE_READ='(()=>{
  const rows=[...document.querySelectorAll(".pane.agents .registry-row")];
  const r=rows[1], prev=rows[0];
  if(!r||!prev) return "no-second-row";
  const clear=(el)=>getComputedStyle(el).backgroundColor==="rgba(0, 0, 0, 0)";
  const drawn=(el)=>getComputedStyle(el).borderBottomColor!=="rgba(0, 0, 0, 0)";
  const seam=(el)=>getComputedStyle(el).borderBottomColor==="rgba(0, 0, 0, 0)";
  const raised=!clear(r), own=seam(r), above=seam(prev);
  if(clear(r)&&drawn(prev)) return "rest";
  if(raised&&own&&above) return "raised";
  return "raised="+raised+" own="+own+" above="+above+" bg="+getComputedStyle(r).backgroundColor+" seam="+getComputedStyle(prev).borderBottomColor;
})()'
rest=$(jsq "$RAISE_READ" 2>&1 | tail -1 | tr -d '"')
assert_eq "$rest" "rest" "at rest the agents rows carry no raise and the seam above them is drawn"
ab hover ".pane.agents .registry-row:nth-child(2)" >/dev/null 2>&1
sleep 0.5
hovered=$(jsq "$RAISE_READ" 2>&1 | tail -1 | tr -d '"')
assert_eq "$hovered" "raised" "hover raises the row as one chip and fades BOTH its seams (own + the preceding row's)"
ab hover ".dock-pane.edge-bottom .dock-tab" >/dev/null 2>&1 || ab hover "body" >/dev/null 2>&1
sleep 0.5
back=$(jsq "$RAISE_READ" 2>&1 | tail -1 | tr -d '"')
assert_eq "$back" "rest" "the pointer leaving restores the quiet list exactly"
step "ok: the agents chip raises, both seams fade, and rest comes back"

# --- the wake strip opens on expand (never wakes) ---
jsq '(name => [...document.querySelectorAll(".pane.agents .registry-row")]
  .find(r => [...r.querySelectorAll(".reg-row-name")].some(n => n.textContent.trim() === name))
  ?.querySelector(".registry-expander")?.click())("'"$CONT_NAME"'")' >/dev/null 2>&1
wait_js 'document.querySelectorAll(".pane.agents .agents-cont-wake, .pane.agents .agents-cont-sleep").length >= 2' 10 >/dev/null 2>&1
wakes=$(jsq 'document.querySelectorAll(".pane.agents .agents-cont-wake, .pane.agents .agents-cont-sleep").length' 2>&1 | tail -1)
assert_eq "$wakes" "2" "the expanded row carries the wake/sleep buttons"
msg=$(jsq '!!document.querySelector(".pane.agents .agents-cont-message")' 2>&1 | tail -1)
assert_eq "$msg" "true" "the wake message input rides the strip"
step "ok: wake strip opened for $CONT_NAME"

# --- card t_5df01d0f: each agent's row opens THAT agent's chat ----------
# Two agent homes carry one permanent chat each. Before the fix both
# chats carried the BARE id "canonical": the roster row handed that id to
# the shell, the shell's opener deduped the second click onto the first
# agent's tab (one session, one tab), the store router answered the first
# home that held the id, and the accent's match (canonical_session_id ===
# focused id) always found the first agent. The id is agent-qualified at
# the source now, so this pass proves the whole operator path: click A,
# then B - the URL, the tab census and the accent all follow B, and A's
# chat stays open beside it.
#
# The row's id is the agent's CANONICAL chat, never the most-recent one
# (card t_10ac04f0, R2): a click's target must not move when a worker
# run lands, so last_session_id left the wire as a rule - an absent
# canonical means the click mints one, and the retired field must never
# come back (the negative assertion below pins that).
agents=$(curl -s "${JOURNEY_BASE_URL}/agents")
chat_ref() {
  printf '%s' "$agents" | python3 -c '
import json,sys
rows = json.load(sys.stdin)
print(next((r.get("canonical_session_id") or "" for r in rows if r.get("name") == sys.argv[1]), ""))' "$1" 2>/dev/null
}
ID_A=$(chat_ref "$CONT_NAME")
ID_B=$(chat_ref "$CONT_NAME2")
[ -n "$ID_A" ] && [ -n "$ID_B" ] || die "a roster row lost its canonical_session_id (A=[$ID_A] B=[$ID_B]): $agents"
case "$ID_A" in "$CONT_NAME"-*) ;; *) die "A's canonical chat id [$ID_A] is not agent-qualified" ;; esac
case "$ID_B" in "$CONT_NAME2"-*) ;; *) die "B's canonical chat id [$ID_B] is not agent-qualified" ;; esac
assert_ne "$ID_A" "$ID_B" "two agents' permanent chats are two session ids"

# THE R2 negative law: the most-recent bind is retired. A row that carried
# last_session_id again would point every click at a moving target (the
# newest session, usually the human's own conversation), so assert the
# FIELD's absence on every row - a reintroduction REDs the nightly here.
# "-" is the parsed-and-clean answer; an unparsed payload is a loud fail,
# never a silent pass.
stale=$(printf '%s' "$agents" | python3 -c '
import json,sys
rows = json.load(sys.stdin)
bad = [str(r.get("name")) for r in rows if "last_session_id" in r]
print(",".join(bad) if bad else "-")' 2>/dev/null)
assert_eq "${stale:-unparsed}" "-" "no roster row carries the retired last_session_id field (the most-recent bind stays dead)"
step "ok: both agent chats are openable, their ids are agent-qualified, and last_session_id is gone"

# active_row: the roster row wearing the accent ("the open agent's row"),
# "" when none does.
active_row() {
  jsq '(() => { const r = document.querySelector(".pane.agents .registry-row.reg-row-active .reg-row-name"); return r ? r.textContent.trim() : ""; })()' 2>&1 | tail -1 | tr -d '"'
}
dock_tabs() { jsq 'document.querySelectorAll(".dock-pane .dock-tab").length' 2>&1 | tail -1; }
# open_agent_row <name>: the row's own open affordance (the name button)
open_agent_row() {
  jsq '(name => {
    const row = [...document.querySelectorAll(".pane.agents .registry-row")]
      .find(r => [...r.querySelectorAll(".reg-row-name")].some(n => n.textContent.trim() === name));
    const b = row?.querySelector("button.registry-open");
    if (!b) return "no-open";
    b.click();
    return "ok";
  })("'"$1"'")' 2>&1 | tail -1 | tr -d '"'
}

tabs0=$(dock_tabs)
got=$(open_agent_row "$CONT_NAME")
assert_eq "$got" "ok" "A's roster row offers the chat open affordance"
wait_js "location.pathname === \"/chat/$ID_A\"" 15
tabsA=$(dock_tabs)
assert_eq "$tabsA" "$((tabs0 + 1))" "A's row opens ONE chat tab bound to A's session"
assert_eq "$(active_row)" "$CONT_NAME" "A's row wears the accent once its chat is open"
step "ok: A ($CONT_NAME) opened its own chat ($ID_A)"

got=$(open_agent_row "$CONT_NAME2")
assert_eq "$got" "ok" "B's roster row offers the chat open affordance"
# THE acceptance law: B's click must move the shell to B's chat, not
# focus A's tab (the pre-fix dedupe) - and it must MINT a tab.
wait_js "location.pathname === \"/chat/$ID_B\"" 15
tabsB=$(dock_tabs)
assert_eq "$tabsB" "$((tabsA + 1))" "B's row mints its OWN chat tab (no dedupe onto A's)"
assert_eq "$(active_row)" "$CONT_NAME2" "the accent follows the open agent - B's row, not A's"
step "ok: B ($CONT_NAME2) opened its own chat ($ID_B)"

# Both agent chats are open, one chip each: the tab title falls back to
# the session id's prefix for a session the sidebar list does not carry,
# and the two names differ in their first eight characters by design.
prefix_a=$(printf '%s' "$ID_A" | cut -c1-8)
prefix_b=$(printf '%s' "$ID_B" | cut -c1-8)
both=$(jsq '(() => {
  const titles = [...document.querySelectorAll(".dock-pane .dock-tab")].map(x =>
    ((x.getAttribute("aria-label") || "") || x.title || x.querySelector(".dock-tab-title")?.textContent || x.textContent || "").trim());
  return (titles.some(s => s.startsWith("'"$prefix_a"'")) && titles.some(s => s.startsWith("'"$prefix_b"'"))) ? 1 : 0;
})()' 2>&1 | tail -1)
assert_eq "$both" "1" "both agent chats stay open, one dock chip each ($prefix_a + $prefix_b)"

assert_no_console_errors
step "GREEN: continuous roster (merged Agents tab) journey"