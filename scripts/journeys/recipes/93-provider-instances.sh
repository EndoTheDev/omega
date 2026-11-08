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
# THE api_key DOOR (354ef12f; the Nov 8 nightly red was this recipe
# exercising the refused shape as a green): config.yaml is the only
# door for an instance api_key. Over the API a row may be added,
# renamed, retyped or removed - but a plain-text key (on a new row or
# a stored one) and a clear of a stored key are REFUSED with the door
# error. The journey therefore:
#   - adds its instance WITHOUT an api_key (empty is the one legal
#     keyless shape on a new row - a real key only ever enters
#     through config.yaml), and
#   - PINS the refusal: a plain-key PUT on a new row, a plain-key PUT
#     on the stored row, a clear of a keyed lane row when the lane
#     carries one (the nightly CI lane is zero-provider - that face
#     stays pinned by cmd/omega settings_test.go
#     TestApplyProviderInstancesGate), and the same refusal surfaced
#     IN THE PANE. A future door-removal turns this journey red
#     instead of silently reopening the API.
#
# Wire truth: GET /settings (the provider.instances knob value) is
# the config truth, GET /sessions (the row's provider field) is the
# pin truth. The pane is presentation truth.
#
# Selector law: every data-instance probe compares through
# getAttribute in a BACKSLASH-FREE expression - the old
# [data-instance=\"name\"] shape is a CSS value with literal quote
# characters under agent-browser 0.26.0 (the winbtw shim passes the
# eval raw) while CI's 0.41.x unescapes once, so the quoted-attr
# form reds on one CLI and greens on the other (the CLI split,
# proven live on this recipe). A backslash-free expr is a fixed
# point of any unescape pass - it agrees on every CLI.
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
PLAIN_KEY="jrn-key-$ITAG"      # the plain key every door pin tries to plant

# state this recipe owns on the lane: the original provider.instances
# list (restored by the trap) and the seed session (deleted by the trap).
INST_BEFORE_FILE="$JOURNEY_WORKTMP/omega-93-inst-before.json"
PIN_BODY="$JOURNEY_WORKTMP/omega-93-pin-body"

cleanup() {
  restore_providers || true
  cleanup_journey_sessions || true
}
trap cleanup EXIT

# restore_providers: PUT the original provider.instances list back.
# Empty snapshot = the lane carried none: an empty list replaces the
# map with nothing (the same wire the pane's save fires). Keyed lane
# rows ride masked in the snapshot - the masked round trip keeps
# their secrets (the door allows it).
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

# keyed_row: the name of the first wire row carrying a MASKED key (""
# when the lane has none - the nightly CI lane is zero-provider).
keyed_row() {
  read_instances | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
name = ""
for r in json.load(sys.stdin):
    if r.get("api_key") == "****":
        name = r.get("name", "")
        break
print(name)
'
}

# pin_new_list <name> <key>: the wire list + one NEW row carrying a
# plain key - the exact shape this journey used to save as green
# before the door landed (the Nov 8 nightly red).
pin_new_list() {
  read_instances | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
rows = json.load(sys.stdin)
rows.append({"name": sys.argv[1], "type": "openai", "host": "", "api_key": sys.argv[2]})
print(json.dumps(rows))
' "$1" "$2"
}

# pin_key_list <name> <key>: the wire list with the named row's
# api_key replaced - a plain plant on a stored row, or "" = a clear
# attempt on a stored key.
pin_key_list() {
  read_instances | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
rows = json.load(sys.stdin)
hit = False
for r in rows:
    if r.get("name") == sys.argv[1]:
        r["api_key"] = sys.argv[2]
        hit = True
if not hit:
    sys.exit(1)
print(json.dumps(rows))
' "$1" "$2"
}

# put_instances <json>: PUT the list over the settings API; prints the
# HTTP status, the body lands in $PIN_BODY (a refused apply answers
# 400 + the raw error text).
put_instances() {
  curl -s -o "$PIN_BODY" -w '%{http_code}' -X PUT "$JOURNEY_BASE_URL/settings" \
    -H 'Content-Type: application/json' \
    -d "{\"values\": {\"provider.instances\": $1}}"
}

# door_refused <label> <json>: the PUT must answer 400 naming the
# config.yaml door. A 200 here means the door came off its hinge -
# the hardening silently reopened the API (the pin this journey
# exists for).
door_refused() {
  assert_eq "$(put_instances "$2")" "400" "$1: the settings PUT refuses (400)"
  case "$(cat "$PIN_BODY")" in
    *"config.yaml is the only door"*)
      step "ok: $1 answers the config.yaml door error" ;;
    *) die "$1 does not name the door: $(cat "$PIN_BODY")" ;;
  esac
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

# row_present_js <name>: a boolean probe - the named row is rendered.
row_present_js() {
  printf '%s' '(()=>{return [...document.querySelectorAll(".providers-editor .provider-row")].some(x=>x.getAttribute("data-instance")==="'$1'")})()'
}

# row_key_js <name>: the named row's api-key input value ("" when the
# row or its input is missing).
row_key_js() {
  printf '%s' '(()=>{const r=[...document.querySelectorAll(".providers-editor .provider-row")].find(x=>x.getAttribute("data-instance")==="'$1'");return r?(r.querySelector("input[type=password]")||{}).value||"":"";})()'
}

# fill_row_key <name> <text>: set the NAMED row's api-key input (the
# input-event law: value alone does nothing).
fill_row_key() {
  jsq '(()=>{const r=[...document.querySelectorAll(".providers-editor .provider-row")].find(x=>x.getAttribute("data-instance")==="'$1'");if(!r) return false;const i=r.querySelector("input[type=password]");if(!i) return false;i.value="'$2'";i.dispatchEvent(new Event("input",{bubbles:true}));return true;})()' >/dev/null 2>&1
}

# remove_row <name>: click the named row's remove control.
remove_row() {
  jsq '(()=>{const r=[...document.querySelectorAll(".providers-editor .provider-row")].find(x=>x.getAttribute("data-instance")==="'$1'");if(!r) return false;const b=r.querySelector(".provider-remove");if(!b) return false;b.click();return true;})()' >/dev/null 2>&1
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
# (openai) and there is no model column (t_1914a8c9 - an instance is
# a named endpoint). NO api_key: the door (354ef12f) refuses a plain
# key on a new row - empty is the one legal keyless shape over the
# API, a real key only ever enters through config.yaml.
fill_row "instance $((wire_rows + 1)) name" "$PROV_NAME"

if ! wait_js '!!document.querySelector(".settings-save")' 8; then
  die "save row never appeared after naming the new instance (nothing marked dirty)"
fi
jsq 'document.querySelector(".settings-save").click()' >/dev/null 2>&1
# the save row unmounts when the diff lands (the refresh payload clears
# dirty); a save ERROR keeps it - poll both ways and fail on the error
if ! poll_js '!document.querySelector(".settings-save")' 20; then
  err=$(jsq '(document.querySelector(".settings-error")||{}).textContent||""' 2>&1 | tail -1 | deq)
  die "instance save never cleared the save row (error: $err)"
fi
step "ok: new instance $PROV_NAME saved (keyless - the door's one legal add shape)"

assert_eq "$(inst_has "$PROV_NAME")" "has" "saved instance is on the wire (provider.instances)"

# pane reload: the editor re-renders the saved row (config round trip)
open_settings
# pane reload + poll: the editor re-renders the saved row (config round
# trip); an immediate query would race the reload's fetch (the 60 law)
if ! poll_js "$(row_present_js "$PROV_NAME")" 10; then
  die "the added instance never re-rendered after a pane reload (did not persist)"
fi
step "ok: the added instance re-renders after a pane reload (persists)"
# the stored row carries NO key: the key input must render EMPTY
# (maskSecret keeps an empty key empty - a mask would imply a key
# exists), so the plain never touches the pane
keyval=$(jsq "$(row_key_js "$PROV_NAME")" 2>&1 | tail -1 | deq)
assert_eq "$keyval" "" "the keyless instance renders an empty key input (no mask without a stored key)"

# --- 1b. the door pins: an instance api_key never crosses the API -----
# (354ef12f: config.yaml is the only door. Each pin PUTs the wire list
# with one door-refused mutation; a green 200 here means the door came
# off its hinge and the journey reds - that is the point.)
WIRE_AT_PINS=$(read_instances)

door_refused "a plain api_key on a NEW row" \
  "$(pin_new_list "jrn-plant-$ITAG" "$PLAIN_KEY")"
door_refused "a plain api_key on the stored row $PROV_NAME" \
  "$(pin_key_list "$PROV_NAME" "$PLAIN_KEY")"

KEYED=$(keyed_row)
if [ -n "$KEYED" ]; then
  door_refused "clearing the stored key on $KEYED" "$(pin_key_list "$KEYED" "")"
else
  step "SKIP: the lane carries no keyed instance - the clear face stays pinned by cmd/omega settings_test.go (TestApplyProviderInstancesGate)"
fi

# every refused PUT mutated nothing: the wire is byte-identical
assert_eq "$(read_instances)" "$WIRE_AT_PINS" "the refused door PUTs changed nothing (wire unchanged)"

# --- 1c. the door pin in the pane -------------------------------------
# The refusal must be VISIBLE where the operator edits (the Nov 8
# nightly red surfaced exactly here): a plain key typed into the
# saved row's key input dies on save with the door error, the dirty
# save row stays (the config never moved), and a pane reload shows
# the key input EMPTY again - the refused write left no residue.
if ! poll_js "$(row_present_js "$PROV_NAME")" 8; then
  die "the saved instance row never rendered (pane pin)"
fi
fill_row_key "$PROV_NAME" "$PLAIN_KEY"
if ! wait_js '!!document.querySelector(".settings-save")' 8; then
  die "the plain-key edit never marked the pane dirty (save row missing)"
fi
jsq 'document.querySelector(".settings-save").click()' >/dev/null 2>&1
door_in_pane='(()=>{const e=document.querySelector(".settings-error");return !!e && (e.textContent||"").indexOf("config.yaml is the only door") !== -1;})()'
if ! poll_js "$door_in_pane" 15; then
  err=$(jsq '(document.querySelector(".settings-error")||{}).textContent||""' 2>&1 | tail -1 | deq)
  die "pane save with a plain key was not refused with the door error (error: $err)"
fi
step "ok: pane save with a plain api_key surfaces the door error"
# the refusal keeps the dirty row - the config never moved
assert_eq "$(jsq '!!document.querySelector(".settings-save")' 2>&1 | tail -1 | deq)" "true" \
  "the refused pane save keeps the dirty save row"

# reload: the refused write left no residue - the row persists, its
# key input empty again
open_settings
if ! poll_js "$(row_present_js "$PROV_NAME")" 10; then
  die "the instance row never re-rendered after the refused save (did it persist?)"
fi
residue=$(jsq "$(row_key_js "$PROV_NAME")" 2>&1 | tail -1 | deq)
assert_eq "$residue" "" "the refused plain key left no residue (key input empty after reload)"

# --- 2. remove the row, save: the instance leaves --------------------
remove_row "$PROV_NAME"
if ! wait_js "!($(row_present_js "$PROV_NAME"))" 8; then
  die "remove never dropped the row"
fi
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
