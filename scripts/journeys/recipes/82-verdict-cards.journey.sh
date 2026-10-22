#!/bin/bash
# 82-verdict-cards - the request_verdict REST surface journey
# (t_2ecf9c51). REST-only on purpose: issuing a card is the agent's
# blocked tool call, which needs a live model - that path is covered by
# the Go unit gates (hub block/expiry/race, transcript round-trip). This
# recipe pins the routes the webui depends on:
#
#   seam    - GET /verdicts answers the pending-list shape on THIS lane
#             (a foreign lane without the seam is a 501/404).
#   answer  - POST /sessions/{id}/verdicts rejects a ghost card with
#             404 (the route is wired and reaches the hub's "no pending
#             verdict card"; a missing route is a 404 from the mux, and
#             a 404 body from the hub names the card - we discriminate).
#   hygiene - POST without a session (/verdicts) is 400, DELETE is 405,
#             a malformed body is 400.
#
# Residue-free: creates nothing, answers nothing.
. "${JROOT:?run must export JROOT}/lib.sh"

# --- seam mounted: the bell's pending list answers with the shape ---
payload=$(curl -s "${JOURNEY_BASE_URL}/verdicts")
printf '%s' "$payload" | grep -qF '"cards"' || \
  die "GET /verdicts answered non-verdict payload (wrong lane or seam unmounted): $payload"
printf '%s' "$payload" | grep -qF '"count"' || \
  die "GET /verdicts missing count: $payload"
step "ok: verdicts seam mounted, /verdicts answered with the pending list"

# --- per-session listing answers the same shape ---
payload=$(curl -s "${JOURNEY_BASE_URL}/sessions/jrn-ghost/verdicts")
printf '%s' "$payload" | grep -qF '"cards"' || \
  die "GET /sessions/{id}/verdicts answered wrong shape: $payload"
step "ok: per-session pending listing wired"

# --- answer surface: a ghost card is a hub-level 404, not a mux 404 ---
code=$(curl -s -o "${EVID}/82-ghost-body" -w '%{http_code}' \
  -X POST "${JOURNEY_BASE_URL}/sessions/jrn-ghost/verdicts" \
  -H 'Content-Type: application/json' \
  -d '{"card_id":"jrn-ghost-card","option_index":1}')
[ "$code" = "404" ] || die "ghost-card answer status = $code, want 404: $(cat "${EVID}/82-ghost-body")"
grep -q "no pending verdict card" "${EVID}/82-ghost-body" || \
  die "ghost-card 404 body is not the hub's 'no pending verdict card' (route unwired?): $(cat "${EVID}/82-ghost-body")"
step "ok: ghost-card answer is 404 with the hub reason"

# --- hygiene: no-session POST 400, DELETE 405, malformed body 400 ---
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${JOURNEY_BASE_URL}/verdicts" \
  -H 'Content-Type: application/json' -d '{"option_index":1}')
[ "$code" = "400" ] || die "POST /verdicts (no session) = $code, want 400"

code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "${JOURNEY_BASE_URL}/verdicts")
[ "$code" = "405" ] || die "DELETE /verdicts = $code, want 405"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${JOURNEY_BASE_URL}/sessions/jrn-ghost/verdicts" \
  -H 'Content-Type: application/json' -d '{not json')
[ "$code" = "400" ] || die "malformed body = $code, want 400"
step "ok: route hygiene (400 no-session, 405 DELETE, 400 malformed)"
