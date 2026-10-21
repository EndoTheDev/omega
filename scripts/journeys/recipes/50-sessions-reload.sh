#!/bin/bash
# 50-sessions-reload - the sidebar state journey: the pinned marker and
# the counts survive a reload, and search narrows as you type (the FTS5
# semantics tripwire).
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session sessions | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) sessions-reload $TAG"

open_app
open_journey_row "(journey) sessions-reload $TAG"
step "ok: opened journey session"

# seed one exchange so the row has a message count to render (a fresh
# session renders no meta at all - the count must exist to survive)
send_journey_message "$SID" "meta seed - reply with one word"

# pin it: click the row's status glyph (pin affordance, .stop never opens) -
# polled: the row re-renders on focus switch, don't race the re-render
pinned_now=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  pinned_now=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("(journey) sessions-reload")); if (!r) return 0; const b = r.querySelector("button.row-status"); if (!b) return 0; const al = b.getAttribute("aria-label") || ""; b.click(); return al.includes("Pin") ? 1 : 0;})()' 2>&1)
  [ "$pinned_now" = "1" ] && break
  sleep 1
done
assert_eq "$pinned_now" "1" "row pin affordance available and clicked"

# reload: the pin survives and the row now sits in the PINNED bucket.
open_app
# Poll: the sidebar's session list re-fetches after boot - a one-shot
# query races the render.
in_bucket=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  in_bucket=$(jsq '(()=>{const buckets = Array.from(document.querySelectorAll(".bucket-row")); const b = buckets.find(x => (x.innerText || "").trim().startsWith("PINNED")); if (!b) return 0; let n = b.nextElementSibling; let guard = 0; while (n && guard < 60) { const t = (n.innerText || "").trim(); if (t.startsWith("TODAY") || t.startsWith("YESTERDAY") || t.startsWith("OLDER")) break; if ((n.innerText || "").includes("(journey) sessions-reload")) return 1; n = n.nextElementSibling; guard++; } return 0;})()' 2>&1)
  [ "$in_bucket" = "1" ] && break
  sleep 1
done
assert_eq "$in_bucket" "1" "pin survives reload (journey row inside PINNED bucket)"

# counts render on the row (msgs count straight from GET /sessions) -
# polled: the seeded turn must finish and re-render before the count shows
has_meta=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14; do
  has_meta=$(jsq '(()=>{const rows = Array.from(document.querySelectorAll(".session-list li")); const r = rows.find(x => (x.innerText || "").includes("(journey) sessions-reload")); if (!r) return 0; return (r.innerText || "").match(/msg/)?1:0;})()' 2>&1)
  [ "$has_meta" = "1" ] && break
  sleep 2
done
assert_eq "$has_meta" "1" "row meta (counts) renders"

# search narrows as you type: the unique token filters the list down to
# ours - but narrowing needs a NON-match to lose. A virgin lane (the
# nightly CI lane is one: fresh db) holds only this journey's session,
# and "1 row narrowed to 1 row" is no narrowing - the bucket row keeps
# the li count flat and the tripwire fires on every sparse sidebar. Seed
# a control row the token must NOT match; cleanup deletes it like any
# journey session, and a reload pulls it into the sidebar.
CTRL=$(new_journey_session search-control | tail -1)
label_journey_session "$CTRL" "(journey) search-control"
open_app
rows=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  rows=$(jsq '(()=>{const lis=Array.from(document.querySelectorAll(".session-list li:not(.bucket-row):not(.bucket-empty)")); return lis.filter(li=>li.getClientRects().length>0).length;})()' 2>&1)
  [ "$rows" -ge 2 ] 2>/dev/null && break
  sleep 1
done
[ "$rows" -ge 2 ] 2>/dev/null || die "control row missing from the sidebar (rows=$rows)"
before=$rows
ab type 'input[placeholder="Search sessions"]' "sessions-reload $TAG" >/dev/null
sleep 2
vis=$(jsq '(()=>{const lis=Array.from(document.querySelectorAll(".session-list li:not(.bucket-row):not(.bucket-empty)")); return lis.filter(li=>li.getClientRects().length>0).length;})()' 2>&1)
[ "$vis" -lt "$before" ] 2>/dev/null || die "search did not narrow: rows before=$before after=$vis"
ours=$(jsq '(()=>{const lis=Array.from(document.querySelectorAll(".session-list li:not(.bucket-row):not(.bucket-empty)")); const vis = lis.filter(li => li.getClientRects().length > 0); return vis.some(x => (x.innerText || "").includes("(journey) sessions-reload"))?1:0;})()' 2>&1)
assert_eq "$ours" "1" "search keeps the matching row"
step "ok: search narrows as you type ($before -> $vis rows)"

assert_no_console_errors