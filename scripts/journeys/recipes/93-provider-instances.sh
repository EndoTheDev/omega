#!/bin/bash
# 93-provider-instances - the named provider instances journey (card
# t_7dcd4280): the settings pane's providers list editor renders the
# instance rows, an added row survives save + pane reload (the config
# round trip), the row leaves again through remove + save - and a
# session pinned to a removed instance name strands LOUD: its next
# send answers 400 "not configured", never a silent fallback to the
# default. No model needed: both stranded sends die at instance
# resolution, before any provider I/O.
#
# Wire truth: GET /settings (the provider.instances knob value) is
# the config truth, GET /sessions (the row's provider field) is the
# pin truth. The pane is presentation truth.
#
# Lane note: the strand section pins a NEVER-configured instance name
# - it works on every lane (with or without a pre-existing providers:
# section), because the resolver answers not-found for any name the
# config does not carry. The settings edits are restored by the EXIT
# trap; the stranded seed session is deleted by the lib cleanup trap
# (overridden here to chain both).
. "${JROOT:?run must export JROOT}/lib.sh"

SIDTAG=$(date +%H%M%S)
ITAG=$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')
PROV_NAME="jrn-inst-$ITAG"     # the instance the editor adds + removes
GHOST_NAME="jrn-ghost-$ITAG"   # the never-configured name the strand pins

# state this recipe owns on the lane: the original provider.instances
# list (restored by the trap) and the seed session (deleted by the trap).
INST_BEFORE_FILE="$JOURNEY_WORKTMP/omega-93-inst-before.json"

cleanup() {
  restore_providers || true
  cleanup_journey_sessions || true
}
trap cleanup EXIT

# restore_providers: PUT the original provider.instances list back.
# Empty snapshot = the lane carried none: an empty list replaces the
# map with nothing (the same wire the pane's save fires).
restore_providers() {
  [ -f "$INST_BEFORE_FILE" ] || return 0
  curl -s -X PUT "${JOURNEY_BASE_URL}/settings" \
    -H 'Content-Type: application/json' \
    -d "{\"values\": {\"provider.instances\": $(cat "$INST_BEFORE_FILE")}}" >/dev/null
  step "cleanup: provider.instances restored"
}

# deq: ab eval prints JS strings JSON-quoted - unquote before a shell
# comparator (the house dequote, cf 30-retry's sed, 83's deq).
deq() { sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'; }

# read_instances: the provider.instances knob value (JSON). python -
# stdout reconfigure(newline="\n") because windows translates \n to
# \r\n and the shell comparator must not eat a trailing \r; unknown
# pythons that lack reconfigure keep their plain print.
read_instances() {
  curl -s "$JOURNEY_BASE_URL/settings" | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
doc = json.load(sys.stdin)
out = []
for g in doc.get("groups", []):
    for k in g.get("knobs", []):
        if k.get("key") == "provider.instances":
            out = k.get("value") or []
print(json.dumps(out))
'
}

# inst_count: rows on the wire for the given name
inst_has() {
  read_instances | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
names = [r.get("name", "") for r in json.load(sys.stdin)]
print("has" if sys.argv[1] in names else "no")
' "$1"
}

# row_field: one field of the /sessions row (the 83 shape: provider is
# the pin the wire wrote).
row_field() {
  curl -s "$JOURNEY_BASE_URL/sessions" | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
rows = json.load(sys.stdin)
for r in rows:
    if r["id"] == sys.argv[1]:
        print(r.get(sys.argv[2], "") or "")
        break
' "$1" "$2"
}

# fill_row <arilabel> <text>: fill the LAST row's input carrying the
# aria-label. Vue rides the input event; value alone does nothing.
fill_row() {
  jsq "(()=>{const rows=[...document.querySelectorAll('.providers-editor .provider-row')];const r=rows[rows.length-1];if(!r) return false;const i=[...r.querySelectorAll('input')].find(x=>(x.getAttribute('aria-label')||'')===\"$1\");if(!i) return false;i.value=\"$2\";i.dispatchEvent(new Event('input',{bubbles:true}));return true;})()" >/dev/null 2>&1
}

open_settings() {
  open_app
  # the settings doc-tab chip: find by name (60's probe, poll, never
  # the immediate query), fallback toggles the edge the chip lives on
  CHIP='(()=>{const t=[...document.querySelectorAll(".dock-pane.pinned .dock-tab")].find(b=>((b.getAttribute("aria-label")||"")||b.title).trim()==="Settings");if(!t) return false;t.click();return true})()'
  if ! poll_js "$CHIP" 4; then
    jsq '(()=>{const b=document.querySelector("button[aria-label=\"Toggle right sidebar\"]"); if(!b) return false; b.click(); return true})()' >/dev/null 2>&1 || true
    if ! poll_js "$CHIP" 10; then
      die "Settings dock-tab chip never rendered"
    fi
  fi
  wait_js '!!document.querySelector(".providers-editor .provider-add")' 15 \
    || die "providers editor never rendered after opening"
}

# --- 0. snapshot the lane's instances (the restore anchor) ----------
open_settings
INST_BEFORE=$(read_instances)
printf '%s' "$INST_BEFORE" > "$INST_BEFORE_FILE"
step "ok: provider.instances snapshot taken"
step "ok: provider instances editor renders"
assert_eq "$(jsq '!!document.querySelector(".providers-editor .provider-add")' 2>&1 | tail -1 | deq)" "true" \
  "providers editor renders with its add control"

# a lane may carry pre-existing rows; the editor must render each one
# (the pane is presentation truth for whatever the config carries)
have_rows=$(jsq 'document.querySelectorAll(".providers-editor .provider-row").length' 2>&1 | tail -1 | deq)
wire_rows=$(read_instances | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
print(len(json.load(sys.stdin)))
')
assert_eq "$have_rows" "$wire_rows" "editor row count matches the wire provider.instances"

# --- 1. add a row, save, reload: the instance persists ---------------
jsq '(()=>{const b=document.querySelector(".providers-editor .provider-add"); if(!b) return false; b.click(); return true})()' >/dev/null 2>&1
if ! wait_js '(()=>{const rows=[...document.querySelectorAll(".providers-editor .provider-row")]; return rows.length > '"$wire_rows"' && rows[rows.length-1].querySelector(".provider-remove") !== null;})()' 10 \
  ; then
  die "add instance never appended a row"
fi
step "ok: add instance appended a row"

# fill the NEW row (last): name is the instance id (the id law:
# lowercase alnum/-/_, no 'default'); type stays the row default
# (openai); the model + key are required - a masked key on a NEW
# instance is a save error, so the fresh row carries its own.
fill_row "instance $((wire_rows + 1)) name" "$PROV_NAME"
fill_row "instance $((wire_rows + 1)) model" "jrn-model"
fill_row "instance $((wire_rows + 1)) api key" "jrn-key-$ITAG"

if ! wait_js '!!document.querySelector(".settings-save")' 8; then
  die "save row never appeared after editing the new instance (nothing marked dirty)"
fi
jsq 'document.querySelector(".settings-save").click()' >/dev/null 2>&1
# the save row unmounts when the diff lands (the refresh payload clears
# dirty); a save ERROR keeps it - poll both ways and fail on the error
if ! poll_js '!document.querySelector(".settings-save")' 20; then
  err=$(jsq '(document.querySelector(".settings-error")||{}).textContent||""' 2>&1 | tail -1 | deq)
  die "instance save never cleared the save row (error: $err)"
fi
step "ok: new instance $PROV_NAME saved"

assert_eq "$(inst_has "$PROV_NAME")" "has" "saved instance is on the wire (provider.instances)"

# pane reload: the editor re-renders the saved row (config round trip)
open_settings
# pane reload + poll: the editor re-renders the saved row (config round
# trip); an immediate query would race the reload's fetch (the 60 law)
if ! poll_js '(()=>{return !!document.querySelector(".providers-editor .provider-row[data-instance=\"'$PROV_NAME'\"]")})()' 10; then
  die "the added instance never re-rendered after a pane reload (did not persist)"
fi
step "ok: the added instance re-renders after a pane reload (persists)"
# the key must ride MASKED on the reload - the plain never touches the pane
masked=$(jsq '(document.querySelector(".providers-editor .provider-row[data-instance=\"'$PROV_NAME'\"] input[type=password]")||{}).value||""' 2>&1 | tail -1 | deq)
stars=$(printf '%s' "$masked" | sed 's/\*//g')
[ -n "$masked" ] && [ -z "$stars" ] && step "ok: saved key renders masked" \
  || die "saved key renders EMPTY (masked round trip lost) or PLAIN ($masked)"

# --- 2. remove the row, save: the instance leaves --------------------
jsq '(()=>{const r=document.querySelector(".providers-editor .provider-row[data-instance=\"'$PROV_NAME'\"] .provider-remove"); if(!r) return false; r.click(); return true})()' >/dev/null 2>&1
wait_js '!document.querySelector(".providers-editor .provider-row[data-instance=\"'$PROV_NAME'\"]")' 8 \
  || die "remove never dropped the row"
if ! wait_js '!!document.querySelector(".settings-save")' 8; then
  die "save row never appeared after removing the instance"
fi
jsq 'document.querySelector(".settings-save").click()' >/dev/null 2>&1
if ! poll_js '!document.querySelector(".settings-save")' 20; then
  err=$(jsq '(document.querySelector(".settings-error")||{}).textContent||""' 2>&1 | tail -1 | deq)
  die "remove save never cleared the save row (error: $err)"
fi
step "ok: instance $PROV_NAME removed + saved"

assert_eq "$(inst_has "$PROV_NAME")" "no" "removed instance is off the wire (provider.instances)"

# --- 3. the strand: pin a session to a name the config never had -----
# (the part-1 law exercised on the wire: a send naming an unknown
# instance writes the pin (wire wins and sticks) and fails LOUDLY;
# every later send without the field lifts the dead pin off the row
# and fails LOUDLY again - the default never picks the work up.)
JRN_ID=$(new_journey_session "prov-strand-$SIDTAG" | tail -1)
label_journey_session "$JRN_ID" "(journey) prov-strand $SIDTAG"

body_file="$JOURNEY_WORKTMP/omega-93-strand-body"
code1=$(curl -s -o "$body_file" -w '%{http_code}' -X POST "$JOURNEY_BASE_URL/chat" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\": \"$JRN_ID\", \"messages\": [{\"role\": \"user\", \"content\": \"jrn strand 1\"}], \"provider\": \"$GHOST_NAME\"}")
assert_eq "$code1" "400" "send naming an unknown instance fails loudly (400)"
case "$(cat "$body_file")" in
  *"not configured"*) step "ok: 400 carries the not-configured message" ;;
  *) die "400 body does not name the loud failure: $(cat "$body_file")" ;;
esac

pin=$(row_field "$JRN_ID" provider)
assert_eq "$pin" "$GHOST_NAME" "the wire name stuck on the session pin"

# NO provider field now: the session's dead pin must surface as the
# same loud 400 - never a silent fallback to the default instance.
code2=$(curl -s -o "$body_file" -w '%{http_code}' -X POST "$JOURNEY_BASE_URL/chat" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\": \"$JRN_ID\", \"messages\": [{\"role\": \"user\", \"content\": \"jrn strand 2\"}]}")
assert_eq "$code2" "400" "a send after removal strands on the session pin (400, not a silent default)"
case "$(cat "$body_file")" in
  *"not configured"*) step "ok: stranded send 400s with the not-configured message" ;;
  *) die "stranded send body does not name the loud failure: $(cat "$body_file")" ;;
esac

assert_no_console_errors
step "journey green"
