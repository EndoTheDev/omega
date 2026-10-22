#!/bin/bash
# 79-insights-open - the insights tab journey (t_e1e61680; home moved
# t_d7fd3ad3). Catches the invisible-to-gates family live on the lane:
# the '+' new-tab menu once iterated a never-defined identifier and
# rendered EMPTY - insights shipped unreachable in the UI with every
# gate green. A journey that drives the tab through the real UI would
# have gone red.
#
# card t_d7fd3ad3 moved the home: insights is the SECONDARY sidebar's
# default-visible tab now (label "Insights", capital I). Customer-visible
# invariants, asserted here:
#   default  - a fresh boot's inspector strip already carries insights
#              (exact case) - no menu open needed to see the tab.
#   render   - clicking the tab mounts the usage dashboard (OVERVIEW).
#   numbers  - the overview carries real data (a session count > 0, a
#              token figure, the period header).
#   ranges   - the period switches exist and the header follows a click.
#   menu     - the MAIN pane's '+' menu still lists insights and FOCUSES
#              the sidebar instance (no second insights tab appears).
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

panes=$(jsq 'document.querySelectorAll("[data-pane-id]").length')
[ "$panes" -ge 3 ] 2>/dev/null || die "default dock tree did not render ($panes panes)"
step "ok: dock tree renders ($panes panes)"

# self-sufficient data seed. The old "seeded model run" step drove a real
# chat turn (completion_tokens > 0) and red on the nightly's no-provider
# lane (t_7adf2e7f: "seeded model run completed ... got [0] want [1]")
# instead of testing anything insights-related. The overview's numbers
# are text-estimated (store EstimateTokens), so a persisted user turn
# seeds them WITHOUT a model: the nightly lane keeps no journey data,
# fresh db -> sessions 0 -> the overview renders its empty state and the
# numbers assert is unreachable. Seed this recipe's own throwaway
# session (jrn- prefix, deleted by the suite's cleanup trap).
seed=$(new_journey_session insights | tail -1)
send_journey_message "$seed" "insights overview seed - one persisted user turn"
step "ok: seeded jrn session for the overview numbers"

# the default home: the inspector strip names insights before any click
# (exact case - capital I, the registry voice)
default=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].map(e=>(e.textContent||"").trim());
 return t.includes("Insights")?1:0})()')
assert_eq "$default" "1" "insights tab default-visible (capital I) in the inspector strip"

# click it: the dashboard body carries the OVERVIEW block
clicked=$(jsq '(()=>{const t=[...document.querySelectorAll(".dock-tab-title")].find(e=>(e.textContent||"").trim()==="Insights");
 if(!t) return 0; t.click(); return 1})()')
assert_eq "$clicked" "1" "insights tab clicked"
wait_js '(()=>{return (document.body.innerText||"").includes("OVERVIEW")})()' 15
step "ok: insights tab renders the dashboard"

# real numbers: a positive session count and a token figure in the
# overview (catches an empty-payload render)
nums=$(jsq '(()=>{const el=[...document.querySelectorAll(".dock-content,.pane")].map(e=>e.innerText).find(t=>t.includes("OVERVIEW"))||"";
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

# the MAIN pane's '+' menu coexists (t_d7fd3ad3): it lists insights and
# clicking it FOCUSES the sidebar instance - no second insights tab
before=$(jsq 'document.querySelectorAll(".dock-tab-title").length')
mainJS='(()=>{const ps=[...document.querySelectorAll(".dock-pane")];
 const hit=ps.find(p=>!p.classList.contains("pinned")&&[...p.querySelectorAll(".dock-tab-title")].some(t=>/chat/i.test(t.textContent||"")));
 if(!hit) return 0; const plus=hit.querySelector("button.dock-tab-new");
 if(!plus) return 0; plus.click(); return 1})()'
opened=$(jsq "$mainJS")
assert_eq "$opened" "1" "main pane '+' menu opens"
listed=$(jsq '(()=>{const m=document.querySelector(".dock-menu");
 return m&&[...m.querySelectorAll("button")].some(b=>(b.textContent||"").trim()==="Insights")?1:0})()')
assert_eq "$listed" "1" "'+' menu lists insights (capital I)"
clicked=$(jsq '(()=>{const m=document.querySelector(".dock-menu");
 const b=[...m.querySelectorAll("button")].find(b=>(b.textContent||"").trim()==="Insights");
 if(!b) return 0; b.click(); return 1})()')
assert_eq "$clicked" "1" "insights menu item clicked"
# no duplicate: the singleton focused the sidebar instance, so the tab
# count is unchanged
after=$(jsq 'document.querySelectorAll(".dock-tab-title").length')
[ "$before" = "$after" ] || die "menu open minted a duplicate insights tab ($before -> $after tabs)"
step "ok: '+' menu focuses the sidebar instance (no duplicate tab)"

step "done: insights opens through the UI with real data"