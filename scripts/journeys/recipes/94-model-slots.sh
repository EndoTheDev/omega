#!/bin/bash
# 94-model-slots - the per-subsystem model slots journey (card
# t_fc0b2f2d): provider settings carries exactly four model slots
# (compaction, summarizer, skills curator, approval decision), the
# per-section model knobs are gone from the schema, each slot
# round-trips a value over PUT /settings (a value survives the
# re-serve), a saved slot lands as a top-level key in config.yaml,
# and the three moved-away keys answer a loud 400 on the wire.
#
# No model needed: settings PUT is config I/O, never a provider call -
# this runs on every lane, dead provider or none. Wire truth only (no
# browser steps: the pickers' slot behavior is pinned by the webui
# node tests, modelcatalog.test.ts); the values this recipe writes are
# restored by the EXIT trap.
. "${JROOT:?run must export JROOT}/lib.sh"

SIDTAG=$(date +%H%M%S)
SLOT_COMP="jrn-inst-$SIDTAG:jrn-slot-comp"   # instance:model grammar
SLOT_SUMM="jrn-model-summarizer-$SIDTAG"     # a bare model
SLOT_SKILLS="default:jrn-slot-skills"        # the reserved default
SLOT_APPR="jrn-model-approval-$SIDTAG"       # a bare model

SLOTS_BEFORE_FILE="$JOURNEY_WORKTMP/omega-94-slots-before.json"

# deq: ab eval prints JS strings JSON-quoted; the house dequote (93).
deq() { sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'; }

# read_slots: the four slot knobs' wire values, keyed by knob key (JSON).
read_slots() {
  curl -s "$JOURNEY_BASE_URL/settings" | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
doc = json.load(sys.stdin)
out = {}
for g in doc.get("groups", []):
    for k in g.get("knobs", []):
        if str(k.get("key", "")).startswith("provider.") and k.get("slot") is True:
            out[k["key"]] = k.get("value", "")
print(json.dumps(out))
'
}

# knob_attr <key> <attr> <expect>: asserts the schema carries the knob
# with the given attribute; empties when the knob is absent.
knob_attr() {
  curl -s "$JOURNEY_BASE_URL/settings" | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
doc = json.load(sys.stdin)
key, attr = sys.argv[1], sys.argv[2]
for g in doc.get("groups", []):
    for k in g.get("knobs", []):
        if k.get("key") == key:
            print(json.dumps(k.get(attr)))
            break
    else:
        continue
    break
else:
    print("")
' "$1" "$2"
}

# schema_absent <key>: the moved-away knob is GONE from the schema.
schema_absent() {
  n=$(curl -s "$JOURNEY_BASE_URL/settings" | python3 -c '
import sys, json
doc = json.load(sys.stdin)
print(sum(1 for g in doc.get("groups", []) for k in g.get("knobs", []) if k.get("key") == sys.argv[1]))
' "$1")
  [ "$n" = "0" ] || die "moved-away knob still on the schema wire: $1"
}

cleanup() {
  [ -f "$SLOTS_BEFORE_FILE" ] || return 0
  vals=$(python3 - "$SLOTS_BEFORE_FILE" <<'PYEOF'
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
d = json.load(open(sys.argv[1]))
names = ("provider.compaction_model", "provider.summarizer_model",
         "provider.skills_model", "provider.approvals_model")
print(json.dumps({k: d.get(k, "") for k in names}))
PYEOF
)
  curl -s -X PUT "$JOURNEY_BASE_URL/settings" \
    -H 'Content-Type: application/json' \
    -d "{\"values\": $vals}" >/dev/null
  step "cleanup: the four slot values restored"
}
trap cleanup EXIT

# --- 0. schema truth: four slot knobs, none of the moved-away ones ---
COMP=$(knob_attr "provider.compaction_model" "slot")
SUMM=$(knob_attr "provider.summarizer_model" "type")
assert_eq "$SUMM" '"model"' "summarizer slot rides the model type"
assert_eq "$COMP" "true" "compaction slot is a slot knob (instances on the picker)"
[ -n "$(knob_attr "provider.skills_model" "type")" ] || die "skills_model slot missing from the schema"
[ -n "$(knob_attr "provider.approvals_model" "type")" ] || die "approvals_model slot missing from the schema"
step "ok: the four per-subsystem slots ride the schema"

schema_absent "sessions.summarizer_model"
schema_absent "skills.curator_model"
schema_absent "approvals.model"
step "ok: the per-section model knobs are off the schema"

# --- 1. the wire round trip: PUT the four slots, re-GET, assert -----
BEFORE=$(read_slots)
printf '%s' "$BEFORE" > "$SLOTS_BEFORE_FILE"

body_file="$JOURNEY_WORKTMP/omega-94-put-body"
code=$(curl -s -o "$body_file" -w '%{http_code}' -X PUT "$JOURNEY_BASE_URL/settings" \
  -H 'Content-Type: application/json' \
  -d "{\"values\": {\"provider.compaction_model\": \"$SLOT_COMP\", \"provider.summarizer_model\": \"$SLOT_SUMM\", \"provider.skills_model\": \"$SLOT_SKILLS\", \"provider.approvals_model\": \"$SLOT_APPR\"}}")
assert_eq "$code" "200" "PUT of the four slots is accepted"
step "ok: the four slot values PUT clean"

AFTER=$(read_slots)
for pair in "provider.compaction_model:$SLOT_COMP" "provider.summarizer_model:$SLOT_SUMM" "provider.skills_model:$SLOT_SKILLS" "provider.approvals_model:$SLOT_APPR"; do
  k=${pair%%:*}
  want=${pair#*:}
  got=$(printf '%s' "$AFTER" | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
print(json.load(sys.stdin).get(sys.argv[1], ""))
' "$k")
  assert_eq "$got" "$want" "slot $k round-tripped the value"
done
step "ok: all four slots survive the re-serve (the grammar stays intact)"


# --- 2. yaml persistence: the saved slot lands in config.yaml -------
if [ -n "${OMEGA_HOME:-}" ]; then
  :
elif [ -n "${OMEGA_BIN:-}" ]; then
  OMEGA_HOME="$(dirname "$OMEGA_BIN")"
else
  OMEGA_HOME=""
fi
if [ -n "$OMEGA_HOME" ]; then
  CONFIG="$OMEGA_HOME/config/config.yaml"
  CONFIG_CYG=$(cygpath -m "$CONFIG" 2>/dev/null || printf '%s' "$CONFIG")
  [ -f "$CONFIG_CYG" ] || CONFIG="$OMEGA_HOME/config.yaml" CONFIG_CYG="$(cygpath -m "$CONFIG" 2>/dev/null || printf '%s' "$CONFIG")"
  if [ -f "$CONFIG_CYG" ]; then
    for pair in "compaction_model:$SLOT_COMP" "summarizer_model:$SLOT_SUMM" "skills_model:$SLOT_SKILLS" "approvals_model:$SLOT_APPR"; do
      k=${pair%%:*}
      want=${pair#*:}
      yaml_val=$(python3 - "$CONFIG_CYG" "$k" <<'PYEOF'
import sys
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
# top-level scan: the key starts at column 0 with the exact name; the
# value may be quoted, strip one pair of matching quotes.
key = sys.argv[2]
for line in open(sys.argv[1], encoding="utf-8", errors="replace"):
    ls = line.rstrip("\n\r")
    if ls.startswith(key + ":"):
        v = ls[len(key) + 1:].strip()
        if len(v) >= 2 and v[0] == v[-1] and v[0] in "\"'":
            v = v[1:-1]
        print(v)
        break
PYEOF
)
      assert_eq "$yaml_val" "$want" "slot $k persisted as top-level yaml (config.yaml)"
    done
    step "ok: the four slots persisted into config.yaml"
  else
    die "cannot locate the lane config.yaml from OMEGA_HOME=$OMEGA_HOME"
  fi
else
  step "SKIP: no OMEGA_HOME/OMEGA_BIN exported - yaml persistence not asserted"
fi

# --- 3. the moved-away keys answer a loud 400, never a silent save ---
for dead in "sessions.summarizer_model" "skills.curator_model" "approvals.model"; do
  code=$(curl -s -o "$body_file" -w '%{http_code}' -X PUT "$JOURNEY_BASE_URL/settings" \
    -H 'Content-Type: application/json' \
    -d "{\"values\": {\"$dead\": \"jrn-slot-probe-$SIDTAG\"}}")
  assert_eq "$code" "400" "moved-away key $dead is a loud 400 on PUT"
  case "$(cat "$body_file")" in
    *"unknown setting"*) step "ok: $dead answers unknown-setting (no silent accept)" ;;
    *) die "$dead 400 body does not name the loud failure: $(cat "$body_file")" ;;
  esac
done

step "journey green"
