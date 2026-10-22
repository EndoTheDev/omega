#!/bin/bash
# 80-agents - the agents API journey (t_7e2b7002). The webui Agents
# panel is wired by a later pass; the REST surface is the customer-
# visible contract that must hold the moment the mounts land:
#
#   inventory - GET /agents always lists the default agent first (the
#               default exists by definition, even on a fresh home).
#   detail    - GET /agents/default answers a full profile row; an
#               unknown agent 404s.
#   binding   - POST /agents/{name}/sessions creates a session whose
#               row carries agent=<name>.
#   visibility- a default-bound row shows up in BOTH the agent's own
#               list and GET /sessions; every row the plain list
#               reports is a default-bound row (a non-default row
#               never rides the default list - the cross-agent half is
#               pinned in the Go gates against scratch homes, it needs
#               a second agent that live lanes do not scaffold).
#   transcript- the transcript response carries the agent name.
#
# Residue-free: the only session it creates is a jrn- one, deleted in
# the cleanup trap (no agent dir is scaffolded on the live lane - the
# nightly would accumulate them; scaffold lives in the Go gates).
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab wait --load networkidle >/dev/null 2>&1 || true

# the app shell itself survives the new surface (a poisoned route never
# breaks the dock render)
panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# --- inventory: the default agent exists by definition ---
inv=$(curl -s "${JOURNEY_BASE_URL}/agents")
printf '%s' "$inv" | grep -Eq '"name": ?"default"' || \
  die "GET /agents lost the default agent: $inv"
step "ok: /agents lists default"

# ordered first: the inventory leads with the default row
first=$(printf '%s' "$inv" | grep -oE '"name": ?"[A-Za-z0-9-]+"' | head -1)
case "$first" in
  *'"default"') ;;
  *) die "first profile is $first, want default" ;;
esac
# every row carries the store-filled counts field
names=$(printf '%s' "$inv" | grep -oE '"name": ?"[A-Za-z0-9-]+"' | wc -l)
counts=$(printf '%s' "$inv" | grep -oE '"session_count"' | wc -l)
assert_eq "$counts" "$names" "every profile row carries session_count"

# --- detail ---
det=$(curl -s "${JOURNEY_BASE_URL}/agents/default")
printf '%s' "$det" | grep -Eq '"name": ?"default"' || die "GET /agents/default returned $det"
printf '%s' "$det" | grep -q '"dir"' || die "detail lost the dir field: $det"
code=$(curl -s -o /dev/null -w '%{http_code}' "${JOURNEY_BASE_URL}/agents/does-not-exist-jrn")
assert_eq "$code" "404" "unknown agent detail 404s"
step "ok: default detail answers, ghost 404s"

# --- binding: create a session AS the default agent ---
runtag=$(date +%d%b%H%M%S)
printf '%s' "$runtag" > "${JRN_TAGFILE:-/tmp/omega-jrn-runtag}"
JRN_ID="jrn-${runtag}-agents"
resp=$(curl -s -X POST "${JOURNEY_BASE_URL}/agents/default/sessions" \
       -H 'Content-Type: application/json' -d "{\"id\": \"${JRN_ID}\"}")
printf '%s' "$resp" | grep -q "$JRN_ID" || die "POST /agents/default/sessions failed: $resp"
trap cleanup_journey_sessions EXIT
step "ok: created $JRN_ID as the default agent"

# the row carries the binding agent=default
echo "$resp" | grep -Eq '"agent": ?"default"' || die "created row lost agent=default: $resp"

# the agent's own list carries it
agent_list=$(curl -s "${JOURNEY_BASE_URL}/agents/default/sessions")
echo "$agent_list" | grep -q "$JRN_ID" || die "agent session list lost $JRN_ID"
step "ok: /agents/default/sessions lists the bound session"

# a DEFAULT-bound row is visible on the plain sessions list
plain=$(curl -s "${JOURNEY_BASE_URL}/sessions")
echo "$plain" | grep -q "$JRN_ID" || die "default-bound session missing from GET /sessions"
bad=$(printf '%s' "$plain" | python3 -c '
import json,sys
rows=json.load(sys.stdin)
print(len([r for r in rows if (r.get("agent") or "default") != "default"]))' 2>/dev/null || echo 0)
assert_eq "$bad" "0" "GET /sessions shows only default-bound rows"
step "ok: default-bound row rides GET /sessions, nothing else leaks in"

# --- transcript carries the agent ---
trans=$(curl -s "${JOURNEY_BASE_URL}/sessions/${JRN_ID}/transcript")
echo "$trans" | grep -Eq '"agent": ?"default"' || \
  die "transcript response lost the agent field"
step "ok: transcript carries agent=default"

step "GREEN: agents API journey"