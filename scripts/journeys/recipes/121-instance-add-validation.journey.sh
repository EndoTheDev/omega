#!/bin/bash
# 121-instance-add-validation - an added connection proves the address first
# (card t_bf698d65).
#
# The add was a draft append: any typed address landed on the list and the
# file sorted it out later, so a row could name a dead port, a web page or a
# typo. Now the add ACTION asks the serve to probe the address (shape,
# liveness, omega-ness) and appends nothing on a refusal - and a refusal that
# is not a shape carries the add-anyway hatch, which stamps the row
# `unverified`.
#
# Walked here, on a private lane, with the pane as the witness:
#   syntax     - a foreign scheme answers the validator's own sentence, adds
#                no row, and offers NO hatch (a shape the file refuses could
#                never be saved).
#   hatch      - a dead port answers "unreachable at that address" and offers
#                the hatch; the hatch row renders its marker, saves, and
#                reaches the wire carrying `unverified: true`.
#   not-omega  - the lane's own origin plus a path that is not omega (the SPA
#                index) is refused as "not an omega instance" - the same
#                200-HTML shape an older serve answers where the probe route
#                would be.
#   pass       - `localhost:PORT` (a NAME, the shape a tailnet short name
#                takes) probes ok: the row lands, verified, and persists.
#   panel      - the status-bar connection panel reads the SAME row field, so
#                the hatch row's marker renders there too.
#
# The lane's saved list is user config (config.yaml desktop.instances): this
# recipe snapshots it and PUTs it back in the EXIT trap (the 93 shape).
. "${JROOT:?run must export JROOT}/lib.sh"

TAG=$(date +%H%M%S)
BAD_NAME="jrn-bad-$TAG"          # a foreign scheme: refused, no row
HATCH_NAME="jrn-hatch-$TAG"      # a dead port: refused, then added anyway
FOREIGN_NAME="jrn-page-$TAG"     # the SPA index: refused as not omega
NAME_NAME="jrn-name-$TAG"        # a NAME that answers: probes ok, lands

BEFORE_FILE="$JOURNEY_WORKTMP/omega-121-instances-before.json"

cleanup() {
  restore_instances || true
}
trap cleanup EXIT

# the lane's port, for the `localhost:PORT` name probe
LANE_PORT=$(printf '%s' "$JOURNEY_BASE_URL" | sed -e 's#^[a-zA-Z]*://##' -e 's#/.*$##' -e 's#^.*:##')

# deq: ab eval prints JS strings JSON-quoted - unquote before a shell comparator
deq() { sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'; }

read_wire() { curl -s "${JOURNEY_BASE_URL}/instances"; }

snapshot_instances() {
  read_wire | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
rows = (json.load(sys.stdin) or {}).get("instances") or []
print(json.dumps(rows))
'
}

restore_instances() {
  [ -f "$BEFORE_FILE" ] || return 0
  curl -s -X PUT "${JOURNEY_BASE_URL}/instances" \
    -H 'Content-Type: application/json' \
    -d "{\"instances\": $(cat "$BEFORE_FILE")}" >/dev/null
  step "cleanup: the lane's saved connections are restored"
}

# wire_state <name>: absent | present:verified | present:unverified
wire_state() {
  read_wire | python3 -c '
import sys, json
try:
    sys.stdout.reconfigure(newline="\n")
except Exception:
    pass
name = sys.argv[1]
rows = (json.load(sys.stdin) or {}).get("instances") or []
for r in rows:
    if r.get("name") == name:
        print("present:unverified" if r.get("unverified") else "present:verified")
        break
else:
    print("absent")
' "$1"
}

# row_present_js <name>: a boolean probe (getAttribute, never a quoted attr selector -
# the CLI split the 93 header records)
row_present_js() {
  printf '%s' "(()=>{return [...document.querySelectorAll('.instances-editor .instance-row')].some(x=>x.getAttribute('data-instance')==='$1')})()"
}

# row_marked_js <name>: the named row renders the unverified marker
row_marked_js() {
  printf '%s' "(()=>{const r=[...document.querySelectorAll('.instances-editor .instance-row')].find(x=>x.getAttribute('data-instance')==='$1');return !!r && !!r.querySelector('.instance-unverified')})()"
}

row_count() { jsq 'document.querySelectorAll(".instances-editor .instance-row").length' 2>&1 | tail -1 | deq; }

fill_draft() { # fill_draft <aria-label> <text>
  jsq "(()=>{const i=[...document.querySelectorAll('.instances-editor .instance-add input')].find(x=>(x.getAttribute('aria-label')||'')==='$1');if(!i)return false;i.value='$2';i.dispatchEvent(new Event('input',{bubbles:true}));return true;})()" >/dev/null 2>&1
}

add_click() {
  jsq '(()=>{const b=document.querySelector(".instances-editor .instance-add-btn");if(!b)return false;b.click();return true})()' >/dev/null 2>&1
}

refusal_text() {
  jsq '(document.querySelector(".instance-probe-error .instance-probe-reason")||{}).textContent||""' 2>&1 | tail -1 | deq
}

has_hatch() { jsq '!!document.querySelector(".instances-editor .instance-anyway")' 2>&1 | tail -1 | deq; }

open_settings_desktop() {
  open_app
  GEAR='(()=>{const b=document.querySelector("header.menubar button[aria-label=\"Open settings\"]");if(!b) return false;b.click();return true})()'
  poll_js "$GEAR" 10 || die "the settings gear never rendered"
  wait_js '!!document.querySelector(".settings-nav button[data-category=\"desktop\"]")' 10 \
    || die "the desktop category never rendered in the settings nav"
  jsq '(()=>{const b=document.querySelector(".settings-nav button[data-category=\"desktop\"]");if(!b) return false;b.click();return true})()' >/dev/null 2>&1
  wait_js '!!document.querySelector(".instances-editor .instance-add-btn")' 15 \
    || die "the connections editor never rendered"
}

# --- 0. snapshot the lane's list (the restore anchor) ----------------------
open_settings_desktop
step "ok: the connections editor renders under the desktop category"
snapshot_instances > "$BEFORE_FILE"
ROWS_BEFORE=$(row_count)
step "ok: the lane's saved connections snapshotted ($ROWS_BEFORE row(s))"

# --- 1. syntax: the validator's own sentence, no row, no hatch -------------
fill_draft "new connection name" "$BAD_NAME"
fill_draft "new connection address" "ftp://nope.test"
add_click
if ! poll_js '!!document.querySelector(".instance-probe-error")' 20; then
  die "a foreign scheme produced no refusal line (the probe route answered nothing)"
fi
reason=$(refusal_text)
case "$reason" in
  *"scheme must be http or https"*) step "ok: the bad shape answers the validator's own sentence [$reason]" ;;
  *) die "the syntax refusal does not carry the validator's words: [$reason]" ;;
esac
assert_eq "$(row_count)" "$ROWS_BEFORE" "a refused shape adds no row"
assert_eq "$(has_hatch)" "false" "a shape the file refuses offers no hatch (it could never be saved)"

# --- 2. a dead port: the reason, the hatch, the marker, the wire ----------
fill_draft "new connection name" "$HATCH_NAME"
fill_draft "new connection address" "127.0.0.1:1"
add_click
if ! poll_js '!!document.querySelector(".instances-editor .instance-anyway")' 25; then
  die "a dead port offered no hatch (reason: [$(refusal_text)])"
fi
reason=$(refusal_text)
case "$reason" in
  *"unreachable at that address"*) step "ok: the dead port answers 'unreachable at that address' [$reason]" ;;
  *) die "the dead port's refusal is wrong: [$reason]" ;;
esac
assert_eq "$(row_count)" "$ROWS_BEFORE" "an unreachable address adds no row before the hatch"

jsq '(()=>{const b=document.querySelector(".instances-editor .instance-anyway");if(!b)return false;b.click();return true})()' >/dev/null 2>&1
if ! poll_js "$(row_marked_js "$HATCH_NAME")" 10; then
  die "the hatch appended no row carrying its unverified marker"
fi
step "ok: the hatch lands the row, stamped unverified"

# the pane is dirty: saving is what puts the row on the wire
if ! wait_js '!!document.querySelector(".settings-save-row")' 10; then
  die "the hatch row never marked the pane dirty (no save row)"
fi
jsq '(()=>{const b=document.querySelector(".settings-save-row .settings-save");if(!b)return 0;b.click();return 1})()' >/dev/null 2>&1
if ! poll_js '!document.querySelector(".settings-save-row")' 25; then
  err=$(jsq '(document.querySelector(".settings-error")||{}).textContent||""' 2>&1 | tail -1 | deq)
  die "the hatch row never saved (error: [$err])"
fi
assert_eq "$(wire_state "$HATCH_NAME")" "present:unverified" "the hatch row persists with its flag on the wire"

# --- 3. the SPA index is not omega ---------------------------------------
fill_draft "new connection name" "$FOREIGN_NAME"
fill_draft "new connection address" "${JOURNEY_BASE_URL}/nope"
add_click
if ! poll_js "(()=>{const r=(document.querySelector('.instance-probe-error .instance-probe-reason')||{}).textContent||'';return r.indexOf('not an omega instance')>=0})()" 25; then
  die "a page that is not omega was not refused as such (reason: [$(refusal_text)])"
fi
step "ok: the lane's own origin with a non-omega path answers 'not an omega instance' [$(refusal_text)]"
assert_eq "$(row_count)" "$((ROWS_BEFORE + 1))" "a not-omega refusal adds no row (the hatch row is the only add)"

# --- 4. a NAME that answers: localhost:PORT probes ok --------------------
fill_draft "new connection name" "$NAME_NAME"
fill_draft "new connection address" "localhost:${LANE_PORT}"
add_click
if ! poll_js "$(row_present_js "$NAME_NAME")" 25; then
  die "a passing probe appended no row (reason: [$(refusal_text)])"
fi
assert_eq "$(row_count)" "$((ROWS_BEFORE + 2))" "the passing probe appends exactly one row"
assert_eq "$(jsq "$(row_marked_js "$NAME_NAME")" 2>&1 | tail -1 | deq)" "false" \
  "a verified row carries no marker"

if ! wait_js '!!document.querySelector(".settings-save-row")' 10; then
  die "the verified row never marked the pane dirty"
fi
jsq '(()=>{const b=document.querySelector(".settings-save-row .settings-save");if(!b)return 0;b.click();return 1})()' >/dev/null 2>&1
if ! poll_js '!document.querySelector(".settings-save-row")' 25; then
  err=$(jsq '(document.querySelector(".settings-error")||{}).textContent||""' 2>&1 | tail -1 | deq)
  die "the verified row never saved (error: [$err])"
fi
assert_eq "$(wire_state "$NAME_NAME")" "present:verified" "the NAME row persists as a verified row"

# --- 5. the panel reads the same field -----------------------------------
jsq '(()=>{const b=document.querySelector(".statusbar .seg.seg-btn.health");if(!b)return false;b.click();return true})()' >/dev/null 2>&1
wait_js '!!document.querySelector(".health-pop")' 10 || die "the connection panel never opened"
if ! poll_js '(()=>{const m=[...document.querySelectorAll(".health-pop .hp-unverified")];return m.length>0 && m[0].closest(".hp-crow")!==null})()' 15; then
  die "the status-bar panel renders no unverified marker for the hatch row"
fi
panel_title=$(jsq '(document.querySelector(".health-pop .hp-unverified")||{}).getAttribute("title")||""' 2>&1 | tail -1 | deq)
case "$panel_title" in
  *"added anyway"*) step "ok: the panel's marker names why the row is unproven [$panel_title]" ;;
  *) die "the panel's marker carries no reason: [$panel_title]" ;;
esac
step "ok: the status-bar panel and the pane read one flag"

assert_no_console_errors
step "121-instance-add-validation: green"
