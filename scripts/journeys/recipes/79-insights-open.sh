#!/bin/bash
# 79-insights-open - the insights tab journey (t_e1e61680 + the '+'
# menu repair). Catches the invisible-to-gates family live on the
# lane: the '+' new-tab menu iterated a never-defined identifier
# from t_d6b657a7 until 2026-10-22 and rendered EMPTY - insights
# shipped unreachable in the UI with every gate green. A journey
# that opens the tab through the real UI would have gone red.
#
# Customer-visible invariants, asserted here:
#   menu    - the '+' menu of the MAIN pane lists the inventory
#             (insights included): the refill source is alive.
#   tab     - clicking insights opens a main tab whose content
#             renders the usage dashboard (OVERVIEW present).
#   numbers - the overview carries real data (a session count
#             > 0, a token figure, the period header).
#   ranges   - the period switches exist (7 days / 30 days /
#             90 days / all time) and the header follows a click.
. "${JROOT:?run must export JROOT}/lib.sh"

# seed: the overview asserts REAL numbers (a session count > 0, a token
# figure) - on a swept lane nothing qualifies and the assertions are
# unfalsifiable reds. The journey runs its own throwaway session (one
# real model exchange) so the numbers exist regardless of lane state.
JRN_ID=$(new_journey_session insights | tail -1)
label_journey_session "$JRN_ID" "(journey) insights-seed $(journey_tag)"
send_journey_message "$JRN_ID" "insights seed - reply with exactly: seedok"
seeded=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  seeded=$(curl -s "$JOURNEY_BASE_URL/sessions" | python3 -c "
import sys, json
rows = json.load(sys.stdin)
print(1 if any(r['id'] == '$JRN_ID' and r.get('completion_tokens', 0) > 0 for r in rows) else 0)
")
  [ "$seeded" = "1" ] && break
  sleep 2
done
assert_eq "$seeded" "1" "seeded model run completed (completion_tokens > 0)"

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# the MAIN pane: the pane hosting the chat/board/sessions tabs. Its
# '+' button opens the new-tab menu.
mainJS='(()=>{const ps=[...document.querySelectorAll(".dock-pane")];
 const hit=ps.find(p=>[...p.querySelectorAll(".dock-tab-title")].some(t=>/chat/i.test(t.textContent||"")));
 if(!hit) return 0; const plus=hit.querySelector("button.dock-tab-new");
 if(!plus) return 0; plus.click(); return 1})()'
opened=$(jsq "$mainJS")
assert_eq "$opened" "1" "main pane '+' menu opens"

# the menu lists insights - the empty-menu regression dies here
listed=$(jsq '(()=>{const m=document.querySelector(".dock-menu");
 return m&&[...m.querySelectorAll("button")].some(b=>(b.textContent||"").trim()==="insights")?1:0})()')
assert_eq "$listed" "1" "'+' menu lists insights"

# click insights: the singleton opens in the main pane
clicked=$(jsq '(()=>{const m=document.querySelector(".dock-menu");
 const b=[...m.querySelectorAll("button")].find(b=>(b.textContent||"").trim()==="insights");
 if(!b) return 0; b.click(); return 1})()')
assert_eq "$clicked" "1" "insights menu item clicked"

# the tab renders: an insights tab title exists and the dashboard
# body carries the overview block (body-wide: the first .dock-content
# in DOM order is the sidebar pane, not the main pane). The heading is
# the component's lowercase h3 - matched case-insensitively, since the
# journey once pinned an uppercase OVERVIEW that no build ever shipped.
wait_js '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].some(e=>/insights/i.test(e.textContent||""));
 return t&&/overview/i.test((document.body.innerText||""))})()' 15
step "ok: insights tab renders the dashboard"

# real numbers: a positive session count and a token figure in the
# overview (catches an empty-payload render). The pane is found by its
# overview H3 HEADING, never by body-wide text matching: a session
# titled "Intro and Repo Overview" in the sidebar pane matches
# /overview/i first (DOM order) and the regex reads the sidebar's
# rows instead of the dashboard.
nums=$(jsq '(()=>{const p=[...document.querySelectorAll(".dock-content,.pane")].find(e=>[...e.querySelectorAll("h3")].some(h=>/^overview$/i.test((h.textContent||"").trim())));
 const el=p?p.innerText:"";
 const s=/sessions\s*\n?\s*(\d+)/.exec(el); const k=/tokens \(est\.\)\s*\n?\s*([\d,]+)/.exec(el);
 return s&&k&&Number(s[1])>0&&Number(k[1].replace(/,/g,""))>0?1:0})()')
assert_eq "$nums" "1" "overview carries real data"

# the period switches: a select with the four windows; the header
# follows a 7-days switch (v-model rides the change event)
hdr=$(jsq '(()=>{const s=[...document.querySelectorAll("select")].find(e=>(e.textContent||"").includes("all time"));
 if(!s) return 0; s.value="7"; s.dispatchEvent(new Event("change",{bubbles:true})); return 1})()')
assert_eq "$hdr" "1" "period select switches present"
# the header follows the switch - poll 1/0 (wait_js wants a literal
# `true` from ab eval; the 1/0 shape matches every assert above)
i=0; ok=0; got=""
while [ "$i" -lt 30 ]; do
  got=$(jsq '(()=>{return ((document.body.innerText)||"").includes("Last 7 days")?1:0})()')
  if [ "$got" = "1" ]; then ok=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$ok" = "1" ] || die "7-days header never rendered (last: [$got])"
step "ok: period header follows the 7-days switch"

step "done: insights opens through the UI with real data"