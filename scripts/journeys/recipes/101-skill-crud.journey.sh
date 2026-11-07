#!/bin/bash
# 101-skill-crud - the skills tab CRUD journey (card t_0a108f16, the
# capsule round: create / edit / remove a skill from the webui without
# touching a skills folder by hand).
#
# The tab shipped read-only rows plus the toggle; this round pins the
# customer-level CRUD contract through the REAL tab:
#
#   a  create   - the plus tool opens the form; name + description +
#                 body save through POST /skills/save; the row renders
#                 and GET /skills/content carries the composed
#                 frontmatter + body (no restart)
#   b  edit     - the row's pencil opens the editor pre-filled from
#                 the served content; a description change lands on
#                 the wire, the body under the fence preserved
#   c  remove   - the editor's two-step delete (armed, then confirm)
#                 removes the skill; the row dies, the listing answers
#                 the missing name, and a second delete of the gone
#                 name is the loud 404 (a no-op delete is never silent)
#
# Journey-session scoped (the law): the skill name is a jrn- prefixed
# throwaway; the cleanup trap POSTs /skills/delete so a red never
# leaves a folder behind. Drive-by-aria-label + text (the
# worker-onboarding law): refs invalidate each render snapshot.
# The skills tab is NOT provider-gated - this recipe runs on any lane.

. "${JROOT:?run must export JROOT}/lib.sh"

# SKILL: the throwaway skill's name (the jrn- prefix keeps the journey
# voice; a single folder component is the name law).
SKILL="jrn-crud$(date +%d%b%H%M%S | tr 'A-Z' 'a-z' | tr -cd 'a-z0-9-')"

# residue-free: a red that left the folder behind must not linger
cleanup() {
  curl -s -X POST "$JOURNEY_BASE_URL/skills/delete" \
    -H 'Content-Type: application/json' \
    -d "{\"name\": \"$SKILL\"}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

open_app

# --- open the Skills tab (the registry fallback's Title Case chip) -----
open_tab() {
  local label="$1"
  jsq "(()=>{const t=[...document.querySelectorAll('.dock-tabbar .dock-tab')].find(e=>{const n=((e.getAttribute('aria-label')||e.textContent||'').trim());return n==='$label'});t&&t.click();return t?1:0})()" >/dev/null
}
open_tab Skills
# a fresh lane home lists an EMPTY inventory - the rows ul is gated on
# rows.length; the tab's presence is the render verdict (the seed comes
# from the journey itself, through the real create flow below).
wait_js '(()=>{return document.querySelector(".skills-list")?true:false})()' 10 \
  || die "the skills tab never rendered"
wait_js '(()=>{const b=[...document.querySelectorAll(".skills-list .band-tool")].find((x)=>x.getAttribute("aria-label")==="add skill");return b?true:false})()' 10 \
  || die "the add-skill band tool never rendered"
step "ok: skills tab renders (rows + add tool)"

# fill via the v-model law: real input events (the setfield helper)
setfield() {
  local label="$1" value="$2"
  jsq "(()=>{const lab=[...document.querySelectorAll('.skill-editor-field')].find((l)=>((l.querySelector('span')||{textContent:''}).textContent.trim())==='$label');if(!lab) return 0;const inp=lab.querySelector('input')||lab.querySelector('textarea');if(!inp) return 0;const inp2=inp;const proto=inp2.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;const set=Object.getOwnPropertyDescriptor(proto,'value').set;set.call(inp2,'$value');inp2.dispatchEvent(new Event('input',{bubbles:true}));return 1})()"
}

# --- a: create ------------------------------------------------------------
add=$(jsq '(()=>{const b=[...document.querySelectorAll(".skills-list .band-tool")].find((x)=>x.getAttribute("aria-label")==="add skill");if(!b) return 0;b.click();return 1})()' 2>&1 | tail -1)
assert_eq "$add" "1" "add skill tool clicked"
# ponytail: bare-arrow evals return {} on this agent-browser CLI
# (proven live 2026-11-07) - every wait uses the block-return shape.
wait_js '(()=>{return !!document.querySelector(".skill-editor")})()' 10

assert_eq "$(setfield name "$SKILL")" "1" "name field filled"
assert_eq "$(setfield description 'journey CRUD probe skill')" "1" "description field filled"
assert_eq "$(setfield body 'first body line')" "1" "body field filled"
jsq '(()=>{const b=[...document.querySelectorAll(".skill-editor-btn")].find((x)=>(x.textContent||"").trim().startsWith("save skill"));if(!b) return 0;b.click();return 1})()' >/dev/null
wait_js "(()=>{return [...document.querySelectorAll('.skills-list .reg-row-name')].some((r)=>r.textContent.trim()==='$SKILL')})()" 15 \
  || die "the saved skill's row never rendered"
step "ok: saved skill row renders ($SKILL)"

wire=$(curl -s "$JOURNEY_BASE_URL/skills")
printf '%s' "$wire" | grep -q "\"name\":\"$SKILL\"" || die "GET /skills lost the saved skill"
content=$(curl -s "$JOURNEY_BASE_URL/skills/content?name=$SKILL")
printf '%s' "$content" | grep -q "journey CRUD probe skill" || die "GET /skills/content lost the description"
printf '%s' "$content" | grep -q "first body line" || die "GET /skills/content lost the body"
step "ok: the saved skill rides the wire (frontmatter composed, no restart)"

# --- b: edit ---------------------------------------------------------------
pencil=$(jsq "(()=>{const b=[...document.querySelectorAll('.skills-list .skill-edit-ctrl')].find((x)=>x.getAttribute('aria-label')==='edit skill $SKILL');if(!b) return 0;b.click();return 1})()" 2>&1 | tail -1)
assert_eq "$pencil" "1" "row pencil clicked"
# the editor prefills from GET /skills/content: wait the body back
wait_js "(()=>{const t=document.querySelector('.skill-editor textarea');return t!==null && t.value.includes('first body line')})()" 15 \
  || die "the editor never pre-filled the draft"
assert_eq "$(setfield description 'journey CRUD probe skill EDITED')" "1" "description re-filled (the rest pre-filled)"

jsq '(()=>{const b=[...document.querySelectorAll(".skill-editor-btn")].find((x)=>(x.textContent||"").trim().startsWith("save skill"));if(!b) return 0;b.click();return 1})()' >/dev/null
i=0; edited=0
while [ "$i" -lt 20 ]; do
  if curl -s "$JOURNEY_BASE_URL/skills" | grep -q "journey CRUD probe skill EDITED"; then edited=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$edited" = "1" ] || die "the description edit never landed on the wire"
step "ok: the description edit landed on the wire"
content=$(curl -s "$JOURNEY_BASE_URL/skills/content?name=$SKILL")
printf '%s' "$content" | grep -q "first body line" \
  || die "the body did not survive the description edit"

# --- c: remove (armed, then confirm) ---------------------------------------
# the edit-save closes the editor - the delete button lives IN the
# editor, so re-open it from the row pencil first.
pencil=$(jsq "(()=>{const b=[...document.querySelectorAll('.skills-list .skill-edit-ctrl')].find((x)=>x.getAttribute('aria-label')==='edit skill $SKILL');if(!b) return 0;b.click();return 1})()" 2>&1 | tail -1)
assert_eq "$pencil" "1" "row pencil clicked (editor re-opened for delete)"
wait_js '(()=>{return !!document.querySelector(".skill-editor")})()' 10
jsq '(()=>{const b=[...document.querySelectorAll(".skill-editor-btn.danger")].find((x)=>(x.textContent||"").includes("delete skill"));if(!b) return 0;b.click();return 1})()' >/dev/null
wait_js '(()=>{const b=[...document.querySelectorAll(".skill-editor-btn.danger")].find((x)=>(x.textContent||"").includes("confirm delete?"));return !!b})()' 10
step "ok: delete armed (two-step)"
jsq '(()=>{const b=[...document.querySelectorAll(".skill-editor-btn.danger")].find((x)=>(x.textContent||"").includes("confirm delete?"));if(!b) return 0;b.click();return 1})()' >/dev/null

i=0; gone=0
while [ "$i" -lt 20 ]; do
  if ! curl -s "$JOURNEY_BASE_URL/skills" | grep -q "\"name\":\"$SKILL\""; then gone=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$gone" = "1" ] || die "the deleted skill still rides the wire"
# scoped to the skills list: other tabs may echo the journey name in
# their own body text, so a whole-body wait can never go true.
wait_js "(()=>{const c=document.querySelector('.skills-list'); return c===null || !(c.innerText||'').includes('$SKILL')})()" 15
step "ok: the skill is gone from the wire and the row list"

# the loud no-op delete: the second removal of the gone name is 404
code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$JOURNEY_BASE_URL/skills/delete" \
  -H 'Content-Type: application/json' \
  -d "{\"name\": \"$SKILL\"}")
[ "$code" = "404" ] || die "deleting the gone skill = $code, want 404 (the loud no-op)"
step "ok: a no-op delete is loud 404"

assert_no_console_errors
step "JOURNEY GREEN: skills tab CRUD round-trips from the webui"
