#!/bin/bash
# 99-cron-crud - the cron tab CRUD journey (card t_3885c975, the
# capsule round: create / edit / remove a cron job from the webui
# without editing crons.yaml by hand).
#
# The editor shipped read-only rows plus the toggle; this round pins the
# customer-level CRUD contract through the REAL tab:
#
#   a  create   - the plus tool opens the form; name + schedule +
#                 prompt save through POST /cron/save; the row renders,
#                 the wire carries the definition (schema-validated
#                 upsert, hot reload - no restart)
#   b  edit     - the row's pencil opens the editor pre-filled; a
#                 schedule change lands on the wire (one upsert, the
#                 whole job travels)
#   c  wire     - GET /cron/jobs rows carry the last-status contract
#                 fields (status/last/status_note absent = never fired)
#   d  remove   - the editor's two-step delete (armed, then confirm)
#                 removes the job; the row dies and the wire answers
#                 the missing name (a no-op delete is loud 404 by law)
#
# Journey-session scoped (the law): the job name is a jrn- prefixed
# throwaway; the cleanup trap POSTs /cron/delete so a red never leaves
# a job firing on the lane. Drive-by-aria-label + text (the
# worker-onboarding law): refs invalidate each render snapshot.

. "${JROOT:?run must export JROOT}/lib.sh"

# JOB: the throwaway job's name (the jrn- prefix keeps the registry
# voice; [a-z0-9-] is the schema's name grammar).
JOB="jrn-crud$(date +%d%b%H%M%S | tr 'A-Z' 'a-z')"

# residue-free: a red that left the job behind must not leave it firing
cleanup() {
  curl -s -X POST "$JOURNEY_BASE_URL/cron/delete" \
    -H 'Content-Type: application/json' \
    -d "{\"name\": \"$JOB\"}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

open_app

# --- open the Cron tab (a pinned right-area dock tab) -------------------
tabclicked=$(jsq '(()=>{const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
 const t=[...document.querySelectorAll(".dock-tab")].find((t)=>name(t).toLowerCase()==="cron");
 if(!t) return 0; t.click(); return 1})()')
assert_eq "$tabclicked" "1" "cron tab clicked"
wait_js '(()=>{const b=[...document.querySelectorAll(".cron-list .band-tool")];return b.some((x)=>x.getAttribute("aria-label")==="add cron job")})()' 15
step "ok: cron tab renders (band + add tool)"

# --- a: create -----------------------------------------------------------
add=$(jsq '(()=>{const b=[...document.querySelectorAll(".cron-list .band-tool")].find((x)=>x.getAttribute("aria-label")==="add cron job");if(!b) return 0;b.click();return 1})()')
assert_eq "$add" "1" "add cron job tool clicked"
wait_js '(()=>document.querySelector(".cron-editor")!==null)' 10

# fill name / schedule / prompt (v-model rides real input events)
setfield() {
  local label="$1" value="$2"
  jsq "(()=>{const lab=[...document.querySelectorAll('.cron-editor-field')].find((l)=>((l.querySelector('span')||{textContent:''}).textContent.trim())==='$label');if(!lab) return 0;const inp=lab.querySelector('input');if(!inp) return 0;const set=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;set.call(inp,'$value');inp.dispatchEvent(new Event('input',{bubbles:true}));return 1})()"
}
assert_eq "$(setfield name "$JOB")" "1" "name field filled"
assert_eq "$(setfield schedule '0 12 * * *')" "1" "schedule field filled"
assert_eq "$(setfield prompt 'journey CRUD probe - say nothing and finish')" "1" "prompt field filled"

jsq '(()=>{const b=[...document.querySelectorAll(".cron-editor-btn")].find((x)=>(x.textContent||"").trim().startsWith("save job"));if(!b) return 0;b.click();return 1})()' >/dev/null
wait_js "(()=>(document.body.innerText||'').includes('$JOB'))" 15
step "ok: saved job row renders ($JOB)"

# the wire carries the definition (hot reload without restart)
wire=$(curl -s "$JOURNEY_BASE_URL/cron/jobs")
printf '%s' "$wire" | grep -q "\"name\":\"$JOB\"" || die "GET /cron/jobs lost the saved job"
printf '%s' "$wire" | grep -q '"schedule":"0 12 \* \* \*"' || die "GET /cron/jobs lost the schedule"
step "ok: the saved job rides the wire"

# --- c: the last-status contract fields ----------------------------------
have_status=$(printf '%s' "$wire" | python3 -c '
import json, sys
rows = json.load(sys.stdin)
row = next((r for r in rows if r["name"] == sys.argv[1]), None)
print(1 if row is not None and "status" not in row else 0)' "$JOB")
assert_eq "$have_status" "1" "a never-fired job carries no status (the '-' truth)"

# --- b: edit --------------------------------------------------------------
pencil=$(jsq "(()=>{const b=[...document.querySelectorAll('.cron-edit-ctrl')].find((x)=>x.getAttribute('aria-label')==='edit cron job $JOB');if(!b) return 0;b.click();return 1})()")
assert_eq "$pencil" "1" "row pencil clicked"
wait_js '(()=>document.querySelector(".cron-editor")!==null)' 10
assert_eq "$(setfield schedule '0 13 * * *')" "1" "schedule re-filled (editor pre-filled the rest)"

jsq '(()=>{const b=[...document.querySelectorAll(".cron-editor-btn")].find((x)=>(x.textContent||"").trim().startsWith("save job"));if(!b) return 0;b.click();return 1})()' >/dev/null
i=0; edited=0
while [ "$i" -lt 20 ]; do
  if curl -s "$JOURNEY_BASE_URL/cron/jobs" | grep -q '"schedule":"0 13 \* \* \*"'; then edited=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$edited" = "1" ] || die "the schedule edit never landed on the wire"
step "ok: the schedule edit landed on the wire"

# --- d: remove (armed, then confirm) --------------------------------------
jsq '(()=>{const b=[...document.querySelectorAll(".cron-editor-btn.danger")].find((x)=>(x.textContent||"").includes("delete job"));if(!b) return 0;b.click();return 1})()' >/dev/null
wait_js '(()=>{const b=[...document.querySelectorAll(".cron-editor-btn.danger")].find((x)=>(x.textContent||"").includes("confirm delete?"));return !!b})()' 10
step "ok: delete armed (two-step)"
jsq '(()=>{const b=[...document.querySelectorAll(".cron-editor-btn.danger")].find((x)=>(x.textContent||"").includes("confirm delete?"));if(!b) return 0;b.click();return 1})()' >/dev/null

i=0; gone=0
while [ "$i" -lt 20 ]; do
  if ! curl -s "$JOURNEY_BASE_URL/cron/jobs" | grep -q "\"name\":\"$JOB\""; then gone=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$gone" = "1" ] || die "the deleted job still rides the wire"
wait_js "(()=>!(document.body.innerText||'').includes('$JOB'))" 15
step "ok: the job is gone from the wire and the row list"

assert_no_console_errors
step "JOURNEY GREEN: cron tab CRUD round-trips from the webui"
