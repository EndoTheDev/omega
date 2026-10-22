#!/bin/bash
# 82-verdict-cards - the verdict card journey (t_2ecf9c51, v1.1
# t_42d4dc09). Two halves:
#
# REST (always runs, no model needed):
#   seam    - GET /verdicts answers the pending-list shape on THIS lane
#             (a foreign lane without the seam is a 501/404).
#   answer  - POST /sessions/{id}/verdicts rejects a ghost card with
#             404 (the route is wired and reaches the hub's "no pending
#             verdict card"; a missing route is a 404 from the mux, and
#             a 404 body from the hub names the card - we discriminate).
#   hygiene - POST without a session (/verdicts) is 400, DELETE is 405,
#             a malformed body is 400.
#   served  - the SERVED bundle (not the source) carries the v1.1 list
#             layout markers: stacked option rows with descriptions,
#             the multi confirm, and the typed-answer row.
#
# Browser (model-driven, self-skips on a modelless lane):
#   layout  - a real card from a real run renders its options top to
#             bottom (strictly increasing row tops), each row carries
#             its description, the multi confirm and the typed-answer
#             row are present; clicking two rows + confirm lands
#             "answered: <first>, <second>" on the card.
#
# Residue-free: the browser half runs in its own throwaway session
# (jrn- prefix) and lib.sh deletes it on exit.
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

# --- served content, never source strings: the SPA bundle carries the
# v1.1 list layout (stacked rows + descriptions + confirm + typed row).
# A stale bundle without the new renderer is a red serve, not a green one.
index=$(curl -s "${JOURNEY_BASE_URL}/")
bundle_hit_list=
bundle_hit_row=
for asset in $(printf '%s' "$index" | grep -oE '/_nuxt/[A-Za-z0-9._-]+\.js' | sort -u | head -15); do
  body=$(curl -s "${JOURNEY_BASE_URL}${asset}")
  printf '%s' "$body" | grep -qF 'verdict-option-row' && bundle_hit_row=1
  printf '%s' "$body" | grep -qF 'type your own answer' && bundle_hit_list=1
done
[ "$bundle_hit_row" = "1" ] || die "served bundle does not carry the v1.1 option rows (stale static/?): no .verdict-option-row anywhere in /_nuxt"
[ "$bundle_hit_list" = "1" ] || die "served bundle does not carry the typed-answer row (stale static?)"
step "ok: served SPA carries the list layout (option rows + typed-answer row)"

# --- browser: a REAL card, rendered and answered. Needs the lane's
# model; a modelless lane skips this half without going red (the REST
# half above already judged everything the bundle can carry).
if ! curl -s "${JOURNEY_BASE_URL}/models" | grep -q '"models":\[[^]]'; then
  step "skipped: lane has no reachable model - the card render half needs a live run"
  exit 0
fi

SID=$(new_journey_session verdictlayout | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) verdict layout $TAG"

open_app
open_journey_row "(journey) verdict layout $TAG"

# drive a real run: demand the verdict tool with fixed options, so the
# layout assertions below are stable regardless of the model's wording
ab find placeholder "message omega" click >/dev/null
ab keyboard type "Call the verdict tool now with action request, title \"J82 layout probe\", options EXACTLY [{label:\"Ship\",description:\"merge the branch now\"},{label:\"Hold\",description:\"wait for the next window\"},{label:\"Scrap\",description:\"abandon the attempt\"},{label:\"Park\"},{label:\"Fold\"}], multi=true, allow_text=true. Do not add, reorder, or reword options. Issue the card and stop."
ab press Enter
ab press Enter
step "sent the card instruction"

# the card renders as an interactive card, not a tool line: our title
# token pins OUR card (the throwaway session has no other cards)
wait_js '(([...document.querySelectorAll(".verdict-title")].some(t => (t.innerText||"").includes("J82 layout probe")))) ? 1 : 0' 150
step "ok: verdict card rendered"

# list layout geometry: the rows stack top to bottom - every row's top
# is strictly below the previous row's, all sharing one column.
geo=$(jsq '(()=>{
  const rows=[...document.querySelectorAll(".verdict-card")].filter(c=>c.innerText.includes("J82 layout probe"))[0];
  if(!rows) return "no-card";
  const opts=[...rows.querySelectorAll(".verdict-option-row")];
  if(opts.length<3) return "rows:"+opts.length;
  for(let i=1;i<opts.length;i++){
    if(opts[i].getBoundingClientRect().top<=opts[i-1].getBoundingClientRect().top) return "not-stacked@"+i;
    if(Math.abs(opts[i].getBoundingClientRect().left-opts[i-1].getBoundingClientRect().left)>2) return "not-one-column@"+i;
  }
  return "stacked:"+opts.length;
})()' 2>&1)
case "$geo" in stacked:*) step "ok: options stack top to bottom, one per row ($geo)";;
  *) die "list layout geometry failed: $geo";;
esac

# each described row shows its description; the confirm and the
# typed-answer row are there
desc=$(jsq '(()=>{
  const card=[...document.querySelectorAll(".verdict-card")].filter(c=>c.innerText.includes("J82 layout probe"))[0];
  if(!card) return "no-card";
  const descs=[...card.querySelectorAll(".verdict-desc")].map(d=>(d.innerText||"").trim());
  const need=["merge the branch now","wait for the next window"];
  if(!need.every(w=>descs.some(d=>d.includes(w)))) return "descs:"+JSON.stringify(descs);
  if(!card.querySelector(".verdict-confirm")) return "no-confirm";
  if(!(card.querySelector(".verdict-free-label")||{innerText:""}).innerText.includes("type your own answer")) return "no-typed-row";
  return "ok";
})()' 2>&1)
assert_eq "$desc" "ok" "described rows + multi confirm + typed-answer row render"

# act like the customer: toggle two rows, confirm, the card settles
# with every picked label on the outcome line
clicked=$(jsq '(()=>{
  const card=[...document.querySelectorAll(".verdict-card")].filter(c=>c.innerText.includes("J82 layout probe"))[0];
  if(!card) return "no-card";
  const opts=[...card.querySelectorAll(".verdict-option-row")];
  opts[0].click(); opts[1].click();
  return opts[0].getAttribute("aria-pressed")+","+opts[1].getAttribute("aria-pressed");
})()' 2>&1)
[ "$clicked" = "true,true" ] || die "row toggles did not register (aria: $clicked)"
card_confirm=$(jsq '(()=>{
  const card=[...document.querySelectorAll(".verdict-card")].filter(c=>c.innerText.includes("J82 layout probe"))[0];
  card.querySelector(".verdict-confirm").click(); return "sent";
})()' 2>&1)
[ "$card_confirm" = "sent" ] || die "confirm click failed: $card_confirm"
step "ok: two rows toggled, confirm sent"

wait_js '(([...document.querySelectorAll(".verdict-card")].some(c => c.classList.contains("settled") && /answered:\s*Ship,\s*Hold/.test(c.innerText)))) ? 1 : 0' 90
step "ok: card settled with the multi answer (answered: Ship, Hold)"

wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 90
assert_no_console_errors