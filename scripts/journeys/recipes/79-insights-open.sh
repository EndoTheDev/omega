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
#   ranges  - the period switches exist and the header follows a click.
#   plus    - the MAIN pane's '+' MINTS a fresh chat session directly
#             (Endo amendment 2026-10-26, t_a582486d: the inventory
#             dropdown is OUT of the '+'; the empty-pane watermark
#             keeps the dockable inventory).
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
# (exact case - capital I, the registry voice). card t_502b8e81: the
# right edge renders ICON-ONLY chips - the name rides aria-label.
default=$(jsq '(()=>{const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
 return [...document.querySelectorAll(".dock-tab")].some((t)=>name(t)==="Insights")?1:0})()')
assert_eq "$default" "1" "insights tab default-visible (capital I) in the inspector strip"

# click it: the dashboard body carries the OVERVIEW block
clicked=$(jsq '(()=>{const name=(t)=>((t.getAttribute("aria-label")||"")||t.title||((t.querySelector(".dock-tab-title")||{textContent:""}).textContent)).trim();
 const t=[...document.querySelectorAll(".dock-tab")].find((t)=>name(t)==="Insights");
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

# the MAIN pane's '+' mints a session (Endo amendment 2026-10-26,
# t_a582486d): no dropdown, no menu. openChatTab(null) FOCUSES an
# existing draft chat tab before minting (app.vue: no duplicate
# new-chat tab), so the mint path needs a draft-free dock: close the
# boot draft tab, then open the seeded jrn session in the pane, then
# the '+' must mint a FRESH draft tab. Catches the '+' going dead
# (the invisible-to-gates family this journey exists for).
# 1) close the boot draft tab (title "new chat", unsaved)
draftclosed=$(jsq '(()=>{const ps=[...document.querySelectorAll(".dock-pane")];const hit=ps.find(p=>!p.classList.contains("pinned")&&[...p.querySelectorAll(".dock-tab-title")].some(t=>/^new chat$/i.test((t.textContent||"").trim())));if(!hit) return 0;const t=[...hit.querySelectorAll(".dock-tab-title")].findIndex(x=>/^new chat$/i.test((x.textContent||"").trim()));if(t<0) return 0;const x=hit.querySelectorAll(".dock-tab-close")[t];if(!x) return 0;x.click();return 1})()')
assert_eq "$draftclosed" "1" "boot draft chat tab closed"
draftgone=0; guard=0
while [ "$guard" -lt 20 ]; do
  left=$(jsq '(()=>{const ps=[...document.querySelectorAll(".dock-pane")];const hit=ps.find(p=>!p.classList.contains("pinned"));return hit?[...hit.querySelectorAll(".dock-tab-title")].filter(t=>/^new chat$/i.test((t.textContent||"").trim())).length:0})()')
  [ "$left" = "0" ] && draftgone=1 && break
  sleep 0.5; guard=$((guard + 1))
done
[ "$draftgone" = "1" ] || die "the draft chat tab never closed"
# 2) open the seeded jrn session in the (now empty) main pane. The
# open proof is the SESSION CONTENT in the pane, not the tab title:
# an untitled session keeps the fallback title ("chat") and titling
# needs a model call - the nightly's no-provider lane has none
# (t_da6f294f). The session list lives in a PINNED shell, so seed text
# inside a NON-pinned pane means the chat tab itself opened.
seedrow=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  seedrow=$(jsq '(()=>{const rows=Array.from(document.querySelectorAll(".session-list li"));const r=rows.find(x=>/insights/i.test(x.innerText||""));if(!r) return 0;if(r.classList.contains("bucket-row")) return 0;r.click();return 1})()' 2>&1)
  [ "$seedrow" = "1" ] && break
  sleep 1
done
assert_eq "$seedrow" "1" "seeded jrn session row clicked (opens in the pane)"
seedopen=0; guard=0
while [ "$guard" -lt 20 ]; do
  inpane=$(jsq '(()=>{const ps=[...document.querySelectorAll(".dock-pane")];const hit=ps.find(p=>!p.classList.contains("pinned")&&(p.innerText||"").includes("insights overview seed"));return hit?1:0})()')
  [ "$inpane" = "1" ] && seedopen=1 && break
  sleep 0.5; guard=$((guard + 1))
done
[ "$seedopen" = "1" ] || die "the seeded session never opened in the pane"
before=$(jsq 'document.querySelectorAll(".dock-tab-title").length')
mainJS='(()=>{const ps=[...document.querySelectorAll(".dock-pane")];
  const hit=ps.find(p=>!p.classList.contains("pinned")&&p.querySelector("button.dock-tab-new"));
  if(!hit) return 0; const plus=hit.querySelector("button.dock-tab-new");
  if(!plus) return 0; plus.click(); return 1})()'
opened=$(jsq "$mainJS")
assert_eq "$opened" "1" "main pane '+' clicked"
i=0; grew=0; got=""
while [ "$i" -lt 30 ]; do
  got=$(jsq 'document.querySelectorAll(".dock-tab-title").length')
  if [ "$got" -gt "$before" ]; then grew=1; break; fi
  sleep 0.5; i=$((i + 1))
done
[ "$grew" = "1" ] || die "'+' minted no new tab (tab count stuck at $before, last [$got])"
step "ok: '+' minted a fresh tab ($before -> $got tabs)"
# no menu survives: the dock-menu popup must not render on the click
menu=$(jsq '(()=>{const m=document.querySelector(".dock-menu");
  return m?1:0})()')
assert_eq "$menu" "0" "the '+' opens no inventory menu (Endo amendment)"

step "done: insights opens through the UI with real data"