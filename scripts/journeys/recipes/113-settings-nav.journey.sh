#!/bin/bash
# 113-settings-nav - the settings ONE DOCUMENT (card t_8b464090, which
# SUPERSEDES the master-detail nav of t_af7e1150). One worker owns
# SettingsTab.vue at a time; this recipe is the live witness for the laws
# only a browser can show:
#
#   landing   - nothing is selected and the pane shows EVERY category as a
#               headed section, top to bottom, in nav order (one document,
#               never one category at a time).
#   jump      - a nav click marks the category SELECTED, expands its
#               subsection jump links and SCROLLS the pane to its section;
#               a second click on the same entry DESELECTS: highlight off,
#               branch collapsed, pane back at the top.
#   spy       - scrolling the pane moves the highlight to the section in
#               view (IntersectionObserver), and the highlight lands on the
#               section the user actually scrolled to.
#   search    - a live query filters the DOCUMENT in place, the nav keeps
#               its shape and greys the dead entries; clearing the query
#               brings the whole document back.
#   memory    - the last anchor is remembered per client and a reopen
#               restores the scroll to it (a DESELECT persists as NO
#               anchor, so a cleared selection never comes back).
#   clamp     - card t_a6b794b5: a SELECTED category bounds the pane's
#               scroll to its own range (start and end), a deselect
#               releases it in the same frame, and the boundary follows the
#               section's box when the pane resizes.
#   filter    - card t_cd9952a3: selecting a section FILTERS the document to
#               that section and its subsections (only its rows render),
#               Commonly Used and a deselect restore the all-view, and a
#               search query OVERRIDES the filter (global match) until it
#               clears. A scroll moves the highlight and never filters.
#   url       - card t_cd9952a3 part 3: the in-app URL carries the selected
#               section (?settings=<id> on the current path), and a deep
#               link lands the pane PRE-FILTERED - it outranks the
#               remembered anchor.
#   narrow    - the SAME document; the nav is a drawer that jumps and then
#               closes. There is no second screen and no back control.
#
# No seeding, no model: the lane's own home is enough. Residue-free.
. "${JROOT:?run must export JROOT}/lib.sh"

plain() { tr -d '"'; }
scroll_top() { jsq '(()=>{const b=document.querySelector(".settings-body");return b?Math.round(b.scrollTop):-1})()' 2>&1 | plain; }
# edge_probe <id>: "<edge>|<scrollTop>" in ONE frame - the distance between the
# section's box and the pane's top edge (the observable the jump assertion
# reads), plus the pane's scroll position for a diagnosable red.
edge_probe() {
  jsq "(()=>{const root=document.querySelector('.settings-body');const el=[...document.querySelectorAll('.settings-category')].find(x=>x.getAttribute('data-category')==='$1');if(!root||!el) return '999|-1';return Math.round(Math.abs(el.getBoundingClientRect().top-root.getBoundingClientRect().top))+'|'+Math.round(root.scrollTop)})()" 2>&1 | plain
}
nav_ids() { jsq '(()=>{return [...document.querySelectorAll(".settings-nav-cat")].map(e=>e.getAttribute("data-category")).join(",")})()' 2>&1 | plain; }
doc_ids() { jsq '(()=>{return [...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",")})()' 2>&1 | plain; }
active_id() { jsq '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");return a?a.getAttribute("data-category"):""})()' 2>&1 | plain; }
# card t_cd9952a3: the rows inside ONE category section, and the rows the
# pane renders at all - the two agree exactly while a section filter holds
sec_rows() { jsq "(()=>{const s=document.querySelector('.settings-category[data-category=\"$1\"]');if(!s) return -1;return s.querySelectorAll('.setting-row,.setting-item').length})()" 2>&1 | plain; }
pane_rows() { jsq '(()=>{return document.querySelectorAll(".settings-body .setting-row,.settings-body .setting-item").length})()' 2>&1 | plain; }
# url_section: the section the in-app URL carries (card t_cd9952a3 part 3)
url_section() { jsq '(()=>{return new URL(window.location.href).searchParams.get("settings")||""})()' 2>&1 | plain; }
# sec_geom <id>: "<top>|<height>|<viewport>|<content>" of a category section,
# in the scrollport's own scroll coordinates (top is a DOCUMENT offset).
sec_geom() {
  jsq "(()=>{const root=document.querySelector('.settings-body');const el=document.querySelector('.settings-category[data-category=\"$1\"]');if(!root||!el) return '';const top=Math.round(el.getBoundingClientRect().top-root.getBoundingClientRect().top+root.scrollTop);return [top,Math.round(el.offsetHeight),Math.round(root.clientHeight),Math.round(root.scrollHeight)].join('|')})()" 2>&1 | plain
}
# sec_range <top> <height> <viewport> <content>: the "<min>|<max>" window the
# card's clamp allows. The RECIPE's own arithmetic - the pane's answer is
# never taken as its own witness (the pure maths is pinned in the node suite).
sec_range() {
  local top=$1 h=$2 vp=$3 content=$4 limit min natural max
  limit=$((content - vp)); [ "$limit" -lt 0 ] && limit=0
  min=$top; [ "$min" -lt 0 ] && min=0; [ "$min" -gt "$limit" ] && min=$limit
  natural=$((top + h - vp))
  max=$natural; [ "$max" -gt "$limit" ] && max=$limit; [ "$max" -lt "$min" ] && max=$min
  printf '%s|%s' "$min" "$max"
}
navclick() {
  jsq "(()=>{const b=[...document.querySelectorAll('.settings-nav-cat,.settings-nav-sub')].find(x=>x.getAttribute('data-category')==='$1');if(!b) return 0;b.click();return 1})()" 2>&1 | plain
}
# force_scroll <px>: a REAL user scroll - the wheel releases the observer's
# post-deselect suppression (so the spy follows the hand), then the pane
# jumps to the absolute position asked for. Card t_e7404b84: the CLAMP no
# longer waits for that wheel - any scroll the pane did not issue itself is
# clamped - so this helper would be held at the section's boundary even
# without it. Keep the wheel: it is what a user does, and the spy assertions
# below still need the suppression released.
force_scroll() {
  jsq "(()=>{const root=document.querySelector('.settings-body');if(!root) return 0;root.dispatchEvent(new WheelEvent('wheel',{bubbles:true}));root.scrollTop=$1;return 1})()" 2>&1 | plain
}
# scroll_pane_to <id>: a REAL user scroll - the wheel event releases the
# observer's post-deselect suppression, then the scroll lands the section
# at the pane's top edge
scroll_pane_to() {
  jsq "(()=>{const root=document.querySelector('.settings-body');const el=document.querySelector('.settings-category[data-category=\"$1\"]');if(!root||!el) return 0;root.dispatchEvent(new WheelEvent('wheel',{bubbles:true}));root.scrollTop=root.scrollTop+(el.getBoundingClientRect().top-root.getBoundingClientRect().top);return 1})()" 2>&1 | plain
}

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

# a virgin client: drop the remembered anchor (the pane reads it at setup,
# so the reload is the customer's first-ever open)
js 'localStorage.removeItem("omega.settings-category")' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

GEAR='(()=>{const b=document.querySelector("header.menubar button[aria-label=\"Open settings\"]");if(!b) return false;b.click();return true})()'
if ! poll_js "$GEAR" 15; then
  die "settings menubar gear never rendered"
fi
if ! poll_js '(()=>{return !!document.querySelector(".settings-nav")&&!!document.querySelector(".settings-body")})()' 15; then
  die "the one-document regions never rendered (nav + scrollport)"
fi
step "ok: Settings opened on the one-document pane"

# --- landing: one document, every category, nothing selected -------------
navids=$(nav_ids)
docids=$(doc_ids)
[ -n "$navids" ] || die "the nav rendered no categories"
assert_eq "$docids" "$navids" "every nav category renders as a section, in nav order"
all_ids=$docids

firstdoc=$(jsq '(()=>{const s=document.querySelector(".settings-category");return s?s.getAttribute("data-category"):"none"})()' 2>&1 | plain)
# card t_81d0901b: the MODULES section heads the document now - module
# toggles are configuration, and the retired cockpit tab's reach moved here
assert_eq "$firstdoc" "modules" "the document opens on the MODULES section (nav order)"
rows=$(jsq '(()=>{return document.querySelectorAll(".setting-item,.setting-row").length})()' 2>&1 | plain)
[ "${rows:-0}" -gt 0 ] 2>/dev/null || die "the document rendered no knob rows ($rows)"
step "ok: the one document renders every category ($docids) with $rows rows"

# --- MODULES: the moved module surface (card t_81d0901b) ------------------
# Module toggles are CONFIGURATION, not content: the secondary-pane cockpit
# tab is gone, and this section carries its whole surface - the rows with
# live toggles, the always-on pair as a CHIP (never a disabled switch), and
# the expandable tools/commands/panels detail. The refusal itself lives at
# the seam (PUT /modules/<name> on agent-loop|provider answers
# "module <name> is always on").
# Selection is by getAttribute, never a CSS attr selector: the backslash-free
# probe is a fixed point of every agent-browser unescape pass (t_2454597e).
mods_js='(()=>{const s=[...document.querySelectorAll(".settings-category")].find(x=>x.getAttribute("data-section")==="modules");if(!s) return "missing";return s.querySelectorAll(".extension-row").length+"/"+s.querySelectorAll(".module-toggle").length+"/"+s.querySelectorAll(".module-alwayson").length})()'
mods=$(jsq "$mods_js" 2>&1 | plain)
mrow=${mods%%/*}; rest=${mods#*/}; mtoggles=${rest%%/*}; mchips=${rest##*/}
[ "${mrow:-0}" -gt 0 ] 2>/dev/null || die "the MODULES section rendered no rows ($mods)"
[ "${mtoggles:-0}" -gt 0 ] 2>/dev/null || die "no toggleable module row rendered a switch ($mods)"
[ "${mchips:-0}" -gt 0 ] 2>/dev/null || die "no always-on row rendered its chip ($mods)"
step "ok: MODULES section renders $mrow rows ($mtoggles toggles, $mchips always-on chips)"

# one row's probe: "<chip 0|1>|<pill 0|1>|<row classes>"
row_probe() {
  jsq "(()=>{const s=[...document.querySelectorAll('.settings-category')].find(x=>x.getAttribute('data-section')==='modules');if(!s) return 'missing||';const row=[...s.querySelectorAll('.extension-row')].find(r=>{const n=r.querySelector('.extension-name');return !!n&&n.textContent.trim()==='$1'});if(!row) return 'norow||';return (row.querySelector('.module-alwayson')?1:0)+'|'+(row.querySelector('.module-toggle')?1:0)+'|'+row.className})()" 2>&1 | plain
}

# the load-bearing pair: a VISIBLE row with the chip, and NO switch in it
for pair in agent-loop provider; do
  prow=$(row_probe "$pair")
  prows=${prow%%|*}; prest=${prow#*|}
  assert_eq "$prows" "1" "$pair renders its always-on chip"
  assert_eq "${prest%%|*}" "0" "$pair renders NO toggle switch (the row is a mirror)"
  # the body and the code in ONE capture (a native curl -o under MSYS /tmp
  # writes C:\tmp while cat reads the MSYS path - the empty-body trap)
  refused=$(curl -s -w '\n%{http_code}' -X PUT \
    -H 'Content-Type: application/json' -d '{"enabled":false}' \
    "${JOURNEY_BASE_URL}/modules/$pair")
  code=${refused##*$'\n'}; body=${refused%$'\n'*}
  assert_eq "$code" "400" "the toggle endpoint refuses $pair"
  case "$body" in
    *"module $pair is always on"*) ;;
    *) die "the refusal must name itself ('module $pair is always on'), got: $body" ;;
  esac
  step "ok: the seam refuses $pair (400: $(echo "$body" | tr -d '\n'))"
done

# the expandable detail still lists the module's tools, commands and panels
jsq '(()=>{const s=[...document.querySelectorAll(".settings-category")].find(x=>x.getAttribute("data-section")==="modules");const row=[...s.querySelectorAll(".extension-row")].find(r=>r.querySelector(".extension-expand"));if(!row) return 0;row.querySelector(".extension-expand").click();return 1})()' >/dev/null 2>&1
if ! wait_js '(()=>{const s=[...document.querySelectorAll(".settings-category")].find(x=>x.getAttribute("data-section")==="modules");if(!s) return false;const d=s.querySelector(".extension-detail");if(!d) return false;return ["tools","commands","panels"].every(k=>[...d.querySelectorAll(".detail-label")].some(e=>e.textContent.trim()===k))})()' 10; then
  die "the expanded module row lost its tools/commands/panels detail"
fi
step "ok: an expanded module row lists its tools, commands and panels"

# a removable module toggles OFF and back ON live, the row following the wire
# (the first toggleable row carrying a disable label: the round trip leaves
# the lane as it found it, so the recipe stays residue-free).
# The label may carry the CONSEQUENCE hint t_d92a1f74 shipped ("disable
# notifications - this also disables: cron"): the row is keyed by its NAME,
# so the hint suffix is stripped here - the same read the panel performs,
# never a bare prefix match (a prefix would also accept a longer row name).
tog=$(jsq '(()=>{const s=[...document.querySelectorAll(".settings-category")].find(x=>x.getAttribute("data-section")==="modules");const pill=[...s.querySelectorAll(".module-toggle")].find(p=>/^disable /.test(p.getAttribute("aria-label")||""));if(!pill) return "missing";return (pill.getAttribute("aria-label")||"").replace(/^disable /,"").replace(/ - this also disables:.*$/,"")})()' 2>&1 | plain)
[ "$tog" != "missing" ] || die "no toggleable module row carries a disable label"
click_toggle() {
  jsq "(()=>{const s=[...document.querySelectorAll('.settings-category')].find(x=>x.getAttribute('data-section')==='modules');if(!s) return 0;const row=[...s.querySelectorAll('.extension-row')].find(r=>{const n=r.querySelector('.extension-name');return !!n&&n.textContent.trim()==='$1'});const pill=row&&row.querySelector('.module-toggle');if(!pill) return 0;pill.click();return 1})()" 2>&1 | plain
}
row_off_js="(()=>{const s=[...document.querySelectorAll('.settings-category')].find(x=>x.getAttribute('data-section')==='modules');if(!s) return false;const row=[...s.querySelectorAll('.extension-row')].find(r=>{const n=r.querySelector('.extension-name');return !!n&&n.textContent.trim()==='$tog'});return !!row&&row.className.indexOf('module-off')>=0})()"
row_on_js="(()=>{const s=[...document.querySelectorAll('.settings-category')].find(x=>x.getAttribute('data-section')==='modules');if(!s) return false;const row=[...s.querySelectorAll('.extension-row')].find(r=>{const n=r.querySelector('.extension-name');return !!n&&n.textContent.trim()==='$tog'});return !!row&&row.querySelector('.module-toggle')!==null&&row.className.indexOf('module-off')<0})()"
# ONE click applies (card t_5051d523: no arm step) - the row dims off
assert_eq "$(click_toggle "$tog")" "1" "$tog has a live switch"
if ! poll_js "$row_off_js" 25; then
  die "$tog did not go off (row: $(row_probe "$tog"))"
fi
step "ok: $tog rendered OFF (row dimmed, the switch flipped)"
assert_eq "$(click_toggle "$tog")" "1" "$tog's switch is still there while it is off"
if ! poll_js "$row_on_js" 25; then
  die "$tog did not come back ON (row: $(row_probe "$tog"))"
fi
step "ok: $tog toggles OFF and back ON live (the row state follows the wire)"

active=$(active_id)
assert_eq "$active" "" "a fresh pane has NOTHING selected"
kids=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | plain)
assert_eq "$kids" "0" "nothing selected -> every branch is collapsed (expansion follows selection)"
top=$(scroll_top)
assert_eq "$top" "0" "nothing selected -> the document opens at the TOP"
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "" "a fresh pane carries no anchor"

# --- a click SELECTS + FILTERS + expands (card t_cd9952a3) ---------------
# The jump mechanics are unchanged (cards t_a6b794b5/t_c4b42357/t_720c9cff/
# t_63c699ce): a nav click still marks the entry, still expands its branch,
# still remembers the anchor. What changed is the DOCUMENT: selecting a
# section now narrows the pane to that section and its subsections, so the
# clicked section HEADS the document - the jump has nowhere left to travel
# (scrollTop 0, edge 0). The filter is a view filter: every row of the
# selected section still renders.
target="provider"
assert_eq "$(navclick "$target")" "1" "the provider category is a jump link"
if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return !!a&&a.getAttribute('data-category')==='$target'&&ids==='$target'})()" 15; then
  die "the nav click did not select $target and filter the document to it (active: $(active_id), doc: $(doc_ids))"
fi
step "ok: a nav click marks $target SELECTED and filters the document to it ($(doc_ids))"
# ONLY its rows: the pane's row count equals the section's own
secrows=$(sec_rows "$target")
[ "${secrows:-0}" -gt 0 ] 2>/dev/null || die "the filtered $target section rendered no rows ($secrows)"
prows=$(pane_rows)
assert_eq "$prows" "$secrows" "the pane renders ONLY $target's rows - every other section is filtered out"
# and the section heads the document, so its heading sits on the pane's top
# edge with nothing above it
if ! poll_js "(()=>{const root=document.querySelector('.settings-body');const el=[...document.querySelectorAll('.settings-category')].find(x=>x.getAttribute('data-category')==='$target');if(!root||!el) return false;return Math.abs(el.getBoundingClientRect().top-root.getBoundingClientRect().top)<=8})()" 15; then
  last=$(edge_probe "$target")
  die "the selected section is ${last%%|*} px off the pane's top edge (last edge|scrollTop: $last)"
fi
jumped=$(scroll_top)
assert_eq "$jumped" "0" "a filtered document opens ON its section - the jump rests at the top"
kids=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | plain)
[ "${kids:-0}" -gt 0 ] 2>/dev/null || die "selecting a parent did not expand its subsection jump links"
only=$(jsq '(()=>{const open=[...document.querySelectorAll(".settings-nav-row")].filter(r=>{const c=r.querySelector(".settings-nav-chev");return !!c&&c.getAttribute("aria-expanded")==="true"}).map(r=>r.querySelector(".settings-nav-cat").getAttribute("data-category"));return open.join(",")})()' 2>&1 | plain)
assert_eq "$only" "$target" "exactly ONE branch is open (expansion is derived from the selection)"
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "$target" "the click is remembered as the anchor"
assert_eq "$(url_section)" "$target" "the in-app URL carries the selected section"
assert_eq "$(nav_ids)" "$all_ids" "the NAV keeps its shape - the filter narrows the document, never the list"

# --- the subsections are jump links too ----------------------------------
assert_eq "$(navclick "provider/models")" "1" "a subsection is a jump link"
if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')==='provider/models'})()" 15; then
  die "the subsection click did not mark provider/models (active: $(active_id))"
fi
# card t_a8063683: the section heading is STICKY, so a subsection must land
# BELOW it - a block scrolled to exactly 0 would paint its first rows behind
# the pinned heading. The witness: the block's top is at or under the
# heading's bottom edge, and the heading is pinned at the pane's top.
subok=$(jsq '(()=>{const root=document.querySelector(".settings-body");const el=[...document.querySelectorAll(".settings-block")].find(b=>b.getAttribute("data-category")==="provider/models");if(!root||!el) return "missing";const sec=el.closest(".settings-category");const h=sec&&sec.querySelector(".settings-section-title");if(!h) return "no-heading";const rt=root.getBoundingClientRect().top,bt=el.getBoundingClientRect().top,hb=h.getBoundingClientRect().bottom;return (bt>rt&&bt>=hb-1)?"clear":"occluded:"+Math.round(bt-rt)+"/"+Math.round(hb-rt)})()' 2>&1 | plain)
assert_eq "$subok" "clear" "the subsection lands BELOW the pinned heading - nothing is hidden behind it"
step "ok: a subsection jump lands clear of the pinned heading"

# --- a second click on the selected entry DESELECTS ----------------------
# one click does all three in reverse: highlight off, branch collapsed, top -
# and (card t_cd9952a3) the whole document comes back
assert_eq "$(navclick "provider/models")" "1" "the selected subsection is clickable"
if ! poll_js '(()=>{const b=document.querySelector(".settings-body");const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");const ids=[...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",");return !!b&&b.scrollTop===0&&!a&&document.querySelectorAll(".settings-nav-sub").length===0&&ids==="'"$all_ids"'"})()' 15; then
  die "the second click did not deselect (scrollTop: $(scroll_top), active: $(active_id), doc: $(doc_ids))"
fi
assert_eq "$(doc_ids)" "$all_ids" "a deselect restores the ALL-VIEW document"
assert_eq "$(url_section)" "" "a deselect leaves NO section in the URL"
step "ok: a second click DESELECTS - highlight cleared, branch collapsed, pane back at the top, all-view restored"
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "" "a DESELECT persists as NO anchor"

# --- a FLAT or ONE-CHILD category's jump never hides its first knob -------
# card t_c4b42357: a flat (or one-child) category renders ONE unheaded list,
# so its block repeats the section's own data-section id. The spy/clamp map
# kept the LAST match in DOM order - the inner block - so a jump put the
# block's top at y=0, hidden behind the section's own opaque STICKY heading,
# and the clamp measured the block instead of the section: its minimum moved
# down by the heading's height, so scrolling up to reveal the hidden row
# snapped back. The witness is two MEASURED rects - the category's first knob
# row starts at or below the pinned heading's bottom edge - never a pixel
# constant.
#
# wait_settled: the jump is a SMOOTH scroll, so measure only once the pane's
# scrollTop has stopped moving (two equal samples 200ms apart).
wait_settled() {
  local last=-1 now i=0
  while [ "$i" -lt 75 ]; do
    now=$(scroll_top)
    if [ "$now" = "$last" ]; then return 0; fi
    last=$now
    sleep 0.2
    i=$((i + 1))
  done
  return 1
}
# first_row_clear <id>: "<verdict>|<rowTop>|<headingBottom>" - viewport
# coordinates, so both rects come from the same frame
first_row_clear() {
  jsq "(()=>{const el=document.querySelector('.settings-category[data-category=\"$1\"]');if(!el) return 'missing||';const h=el.querySelector('.settings-section-title');if(!h) return 'no-heading||';const row=el.querySelector('.setting-row,.setting-item');if(!row) return 'no-row||';const rt=row.getBoundingClientRect().top,hb=h.getBoundingClientRect().bottom;return (rt>=hb-1?'clear':'occluded')+'|'+Math.round(rt)+'|'+Math.round(hb)})()" 2>&1 | plain
}
for flatcat in common dock store; do
  assert_eq "$(navclick "$flatcat")" "1" "$flatcat is a jump link"
  if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')==='$flatcat'})()" 15; then
    die "the nav click did not select $flatcat (active: $(active_id))"
  fi
  # card t_cd9952a3: Commonly Used is the curated ALL-VIEW entry, so
  # selecting it restores the whole document; dock and store narrow it to
  # themselves (one section, its own rows, nothing else on screen)
  if [ "$flatcat" = "common" ]; then wantdoc="$all_ids"; else wantdoc="$flatcat"; fi
  if ! poll_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids==='$wantdoc'})()" 15; then
    die "selecting $flatcat did not set the document to [$wantdoc] (now: $(doc_ids))"
  fi
  wait_settled || die "the $flatcat jump never settled (scrollTop: $(scroll_top))"
  measured=$(first_row_clear "$flatcat")
  state=${measured%%|*}; rest=${measured#*|}; rowtop=${rest%%|*}; headbottom=${rest##*|}
  assert_eq "$state" "clear" "$flatcat's first knob row starts BELOW its pinned heading (row top $rowtop, heading bottom $headbottom)"
  step "ok: $flatcat jumps clear of its sticky heading (row top $rowtop >= heading bottom $headbottom, document [$wantdoc])"
done
# leave the pane unselected for the spy block below: ONE click on whatever
# entry currently holds the selection releases it (and the clamp with it)
jsq '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");if(a) a.click();return 1})()' >/dev/null 2>&1
if ! poll_js '(()=>{const b=document.querySelector(".settings-body");const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");return !!b&&b.scrollTop===0&&!a})()' 15; then
  die "the flat-category witness did not leave a clean pane (active: $(active_id), scrollTop: $(scroll_top))"
fi

# --- scroll-spy: the nav follows the pane --------------------------------
spy="server"
assert_eq "$(scroll_pane_to "$spy")" "1" "the pane accepts a user scroll"
if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')==='$spy'})()" 15; then
  die "the scroll-spy did not follow the pane to $spy (active: $(active_id), scrollTop: $(scroll_top))"
fi
step "ok: scrolling moves the highlight to the section in view ($spy)"
# card t_cd9952a3: the SPY's highlight is not a section selection, so a
# scroll must never filter - the whole document stays on screen (only a
# click, and a deep link, own the filter)
spyrows=$(pane_rows)
assert_eq "$spyrows" "$rows" "a scroll never filters the document (the spy only moves the highlight)"
assert_eq "$(doc_ids)" "$all_ids" "the all-view survives the spy's selection"
assert_eq "$(url_section)" "" "and a scroll writes no section into the URL"

# --- search: filter in place, grey the dead entries, OVERRIDE the filter --
# card t_cd9952a3 part 2: a section filter is up while the query runs and
# is IGNORED by it - the search is one global filter across every section -
# and clearing the query gives the filter back.
assert_eq "$(navclick "provider")" "1" "select provider before searching"
if ! poll_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids==='provider'})()" 15; then
  die "the section filter did not come up before the query (doc: $(doc_ids))"
fi
full_ids=$(doc_ids)
full_nav=$(nav_ids)
ab fill '.settings-filterbar input' "model" >/dev/null 2>&1 || die "search input never accepted text"
if ! wait_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids.length>0&&ids!=='${full_ids}'})()" 10; then
  die "the query did not override the section filter (doc: $(doc_ids), filter was $full_ids)"
fi
filtered=$(doc_ids)
case "$filtered" in
  *,*) ;; # more than one section: the query reaches domains the filter hid
  *) die "the query matched only [$filtered] - it must stay GLOBAL across every section" ;;
esac
assert_eq "$(active_id)" "provider" "the selection is untouched by the query - only ignored"
dim=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-cat.dim,.settings-nav-sub.dim").length})()' 2>&1 | plain)
[ "${dim:-0}" -gt 0 ] 2>/dev/null || die "a live query greyed nothing"
common=$(jsq '(()=>{return [...document.querySelectorAll(".settings-nav-cat")].some(e=>e.getAttribute("data-category")==="common")?1:0})()' 2>&1 | plain)
assert_eq "$common" "1" "the curated category STAYS in the nav (greyed or lit, never hidden)"
assert_eq "$(nav_ids)" "$full_nav" "the nav keeps its shape under a query"
step "ok: the query OVERRIDES the section filter ($filtered) and greys $dim nav entries"

ab fill '.settings-filterbar input' "" >/dev/null 2>&1 || die "search input never cleared"
if ! wait_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids==='${full_ids}'})()" 10; then
  die "clearing the query did not restore the section filter (now: $(doc_ids), want $full_ids)"
fi
assert_eq "$(doc_ids)" "$full_ids" "clearing the query restores the SECTION FILTER, not the all-view"
step "ok: clearing the query restores the filter ($full_ids)"
# and a deselect from there gives the whole document back
assert_eq "$(navclick "provider")" "1" "release the section filter"
if ! poll_js '(()=>{const ids=[...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",");return ids==="'"$all_ids"'"})()' 15; then
  die "deselecting did not restore the all-view (doc: $(doc_ids))"
fi
assert_eq "$(url_section)" "" "the released selection left no section in the URL"
step "ok: a deselect after the query restores the whole document"

# --- part 3: the in-app URL carries the section --------------------------
# The URL is the EXPLICIT door: a deep link opens the pane PRE-FILTERED and
# outranks the remembered anchor. The witness plants a DIFFERENT anchor
# first, so only the URL can explain what the pane opens on.
js 'localStorage.setItem("omega.settings-category","compaction")' >/dev/null 2>&1
ab open "${JOURNEY_BASE_URL}/?settings=store" >/dev/null 2>&1 || die "the deep-link URL never opened"
ab wait --load networkidle >/dev/null 2>&1 || true
if ! poll_js "$GEAR" 15; then
  die "settings gear never re-rendered after the deep link"
fi
if ! poll_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return ids==='store'&&!!a&&a.getAttribute('data-category')==='store'})()" 20; then
  die "the deep link did not land pre-filtered (doc: $(doc_ids), active: $(active_id), url: $(url_section))"
fi
assert_eq "$(url_section)" "store" "the URL still carries the section after the load"
assert_eq "$(sec_rows "store")" "$(pane_rows)" "the deep-linked pane renders ONLY that section's rows"
step "ok: a deep link (?settings=store) lands PRE-FILTERED and outranks the remembered anchor"
# clean: no anchor, no section left behind for the steps below
assert_eq "$(navclick "store")" "1" "release the deep link's selection"
if ! poll_js '(()=>{const ids=[...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",");return ids==="'"$all_ids"'"})()' 15; then
  die "the deep-linked selection did not release (doc: $(doc_ids))"
fi
js 'localStorage.removeItem("omega.settings-category")' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
if ! poll_js "$GEAR" 15; then
  die "settings gear never re-rendered after the URL clean-up"
fi
if ! poll_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids==='$all_ids'})()" 20; then
  die "the pane did not reopen on the all-view (doc: $(doc_ids))"
fi
step "ok: with no section in the URL and no anchor, the pane opens on the all-view"

# --- narrow: the SAME document, the nav as a drawer ----------------------
ab set viewport 340 780 >/dev/null 2>&1
if ! wait_js '(()=>{const a=document.querySelector("aside.settings");return !!a&&a.classList.contains("narrow")})()' 15; then
  die "the pane never flipped narrow at 340px"
fi
closed=$(jsq '(()=>{const n=document.querySelector(".settings-nav");const c=document.querySelector(".settings-body");return (n?1:0)+"|"+(c?1:0)})()' 2>&1 | plain)
assert_eq "$closed" "0|1" "narrow shows the DOCUMENT; the nav is not on screen until asked"
assert_eq "$(jsq '(()=>{return document.querySelectorAll(".settings-back").length})()' 2>&1 | plain)" "0" "no back control exists (there is nothing to drill into)"
assert_eq "$(jsq '(()=>{return document.querySelector(".settings-jump")?1:0})()' 2>&1 | plain)" "1" "the drawer toggle rides the filter bar"
js '(()=>{const b=document.querySelector(".settings-jump");if(b) b.click();return 1})()' >/dev/null 2>&1
if ! wait_js '(()=>{const n=document.querySelector(".settings-nav");return !!n&&n.classList.contains("drawer")})()' 10; then
  die "the jump-links drawer never opened"
fi
step "ok: narrow opens the nav as a drawer over the same document"
assert_eq "$(navclick "compaction")" "1" "a drawer entry is a jump link"
if ! poll_js '(()=>{const n=document.querySelector(".settings-nav");const b=document.querySelector(".settings-body");const anchor=localStorage.getItem("omega.settings-category");const ids=[...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",");return !n&&!!b&&anchor==="compaction"&&ids==="compaction"})()' 15; then
  die "a drawer jump did not filter the document, remember the anchor and close the drawer (doc: $(doc_ids), scrollTop: $(scroll_top), anchor: $(jsq '(()=>localStorage.getItem("omega.settings-category")||"")()' 2>&1 | plain))"
fi
step "ok: a drawer jump filters the pane to compaction and closes the drawer"

ab set viewport 1280 800 >/dev/null 2>&1
if ! wait_js '(()=>{const a=document.querySelector("aside.settings");const n=document.querySelector(".settings-nav");return !!a&&!a.classList.contains("narrow")&&!!n})()' 15; then
  die "widening did not restore the always-on nav"
fi

# --- memory: a reopen restores the anchor --------------------------------
# card t_cd9952a3 part 3 rides along here: the selection also wrote the
# section into the URL, so the reopen lands PRE-FILTERED - the URL is the
# explicit door and outranks the remembered anchor (which agrees here).
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "compaction" "the last anchor is remembered in CLIENT storage"
assert_eq "$(url_section)" "compaction" "the URL carries the same section the anchor holds"
before=$(scroll_top)
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
if ! poll_js "$GEAR" 15; then
  die "settings gear never re-rendered after the reopen"
fi
if ! poll_js '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");const ids=[...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",");return !!a&&a.getAttribute("data-category")==="compaction"&&ids==="compaction"})()' 15; then
  die "the reopen did not land pre-filtered on the URL's section (active: $(active_id), doc: $(doc_ids), url: $(url_section))"
fi
step "ok: a reload lands PRE-FILTERED on the URL's section (scrollTop $before -> $(scroll_top))"

# --- clamp: a SELECTED category bounds the pane's scroll ------------------
# card t_a6b794b5 (Endo, 2026-11-11): "when i select a category i should not
# be able to scroll further than inside the category. until i deselected
# again and nothing is selected."
# card t_cd9952a3 later made a selection FILTER the document down to that
# section, so the clamp's two ends now coincide with the document's own
# limit - the arithmetic below is the same one, computed from the section's
# live box, and it must still describe exactly where the pane rests.
# The memory block above left `compaction` restored. A restore does not
# clamp (the remembered id may be a spy highlight), so click it off first -
# that deselect is also the card's own release.
assert_eq "$(navclick "compaction")" "1" "the restored anchor is a real entry to toggle off"
if ! poll_js '(()=>{const b=document.querySelector(".settings-body");const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");const ids=[...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",");return !!b&&b.scrollTop===0&&!a&&ids.length>0})()' 15; then
  die "deselecting the restored anchor did not clear it (active: $(active_id), scrollTop: $(scroll_top))"
fi

# a) NOTHING SELECTED -> the document scrolls end to end (today's behaviour)
assert_eq "$(force_scroll 999999)" "1" "the pane accepts a user scroll with nothing selected"
if ! poll_js '(()=>{const b=document.querySelector(".settings-body");return !!b&&Math.abs(b.scrollTop-(b.scrollHeight-b.clientHeight))<=1})()' 10; then
  die "nothing selected must scroll the whole document (scrollTop: $(scroll_top), end: $(jsq '(()=>{const b=document.querySelector(".settings-body");return Math.round(b.scrollHeight-b.clientHeight)})()' 2>&1 | plain))"
fi
freeend=$(scroll_top)
[ "${freeend:-0}" -gt 200 ] 2>/dev/null || die "the document is too short to prove free scroll (scrollTop $freeend)"
step "ok: nothing selected -> the document scrolls end to end (scrollTop -> $freeend)"

# b) SELECTED -> the filtered document IS the section, and both boundaries
#    hold on its own box
target="provider"
assert_eq "$(navclick "$target")" "1" "$target is a jump link"
if ! poll_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return ids==='$target'&&!!a&&a.getAttribute('data-category')==='$target'})()" 15; then
  die "selecting $target did not filter the document to it (doc: $(doc_ids), active: $(active_id))"
fi
geom=$(sec_geom "$target")
[ -n "$geom" ] || die "the $target section never rendered"
top=${geom%%|*}; rest=${geom#*|}; h=${rest%%|*}; rest=${rest#*|}; vp=${rest%%|*}; content=${rest##*|}
range=$(sec_range "$top" "$h" "$vp" "$content")
lo=${range%%|*}; hi=${range##*|}
[ "${hi:-0}" -gt "${lo:-0}" ] 2>/dev/null || die "the $target section has no travel (geom $geom) - the recipe needs a tall one"
# a filtered section heads the document, so its window starts at the top
assert_eq "$top" "0" "the selected section heads the filtered document"
step "ok: $target is selected and its window is [$lo, $hi] (top $top, height $h, pane $vp, content $content)"

# BELOW the end: ask for the document's bottom, land on the section's bottom
assert_eq "$(force_scroll 999999)" "1" "the pane accepts a scroll while a section is selected"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.abs(b.scrollTop-$hi)<=2})()" 10; then
  die "scrolling past the section's end did not stop at its bottom (scrollTop: $(scroll_top), want $hi, document end $((content - vp)))"
fi
pastend=$(scroll_top)
# card t_cd9952a3: under the filter the document IS the section, so the
# clamp's end is the section's OWN bottom - measured live at 763 here,
# against the document's limit of 767 (the scrollport's 4px of body padding
# is the only document left below the selected section). Both numbers are
# pinned: the pane must rest on the CLAMP's number, and that number must
# still be short of the document's own limit.
assert_eq "$pastend" "$hi" "the pane rests exactly on the clamp's computed END (the section's own bottom)"
[ "${pastend:-0}" -lt "$((content - vp))" ] 2>/dev/null || die "the clamp never bit: the pane reached the document's bottom ($pastend of $((content - vp)))"
step "ok: the selected section stops the pane at its END (scrollTop $pastend == clamp end $hi < document end $((content - vp)))"

# ABOVE the start: ask for the document's top, land on the section's top
assert_eq "$(force_scroll 0)" "1" "the pane still accepts a scroll while a section is selected"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.abs(b.scrollTop-$lo)<=2})()" 10; then
  die "scrolling above the section's start did not stop at its top (scrollTop: $(scroll_top), want $lo)"
fi
assert_eq "$lo" "0" "the section's START is the filtered document's start"
step "ok: the selected section stops the pane at its START (scrollTop $(scroll_top), want $lo)"

# c) DESELECT -> free scroll returns in the same frame. The pane first
#    rests at the section's END, deep inside it, so the spy has moved the
#    highlight onto one of its blocks: a single click on the CATEGORY must
#    still release (the card's promise cannot depend on where the highlight
#    landed).
assert_eq "$(force_scroll "$hi")" "1" "rest at the section's end, deep inside it"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.abs(b.scrollTop-$hi)<=2})()" 10; then
  die "the pane did not rest at the section's end (scrollTop: $(scroll_top), want $hi)"
fi
if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')!=='$target'})()" 15; then
  die "the spy kept the category highlighted at its end - the block release path was not exercised (active: $(active_id))"
fi
spied=$(active_id)
assert_eq "$(navclick "$target")" "1" "the category entry is clickable while one of its blocks is highlighted"
if ! poll_js '(()=>{const b=document.querySelector(".settings-body");const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");return !!b&&b.scrollTop===0&&!a})()' 15; then
  die "ONE click on the category did not release it while the spy sat on $spied (active: $(active_id), scrollTop: $(scroll_top))"
fi
step "ok: one click on the category releases it even with the spy on a block ($spied)"
allend=$(jsq '(()=>{const b=document.querySelector(".settings-body");return b?Math.round(b.scrollHeight-b.clientHeight):-1})()' 2>&1 | plain)
[ "${allend:-0}" -gt "${pastend:-0}" ] 2>/dev/null || die "the all-view did not restore a DEEPER document (all end $allend, filtered end $pastend)"
assert_eq "$(doc_ids)" "$all_ids" "the release restored the ALL-VIEW document"
assert_eq "$(force_scroll "$pastend")" "1" "the pane accepts a scroll after the deselect"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.abs(b.scrollTop-$pastend)<=2})()" 10; then
  die "the clamp did not release: the pane still refuses the section's old end (scrollTop: $(scroll_top), want $pastend)"
fi
step "ok: a deselect releases the clamp - the pane scrolls past the old end again"

# the free scroll let the spy highlight whatever section sits there: clear
# it, so the next click is a SELECT and not a toggle-off
jsq '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");if(a) a.click();return 1})()' >/dev/null 2>&1
if ! poll_js '(()=>{return !document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active")})()' 10; then
  die "the spy's highlight could not be cleared before the resize witness (active: $(active_id))"
fi

# d) the boundary follows the SECTION's box, not just the first measurement:
#    grow the pane while the section holds the scroll and the stale max must
#    pull the pane up (step 3 - a box change with no scroll event)
assert_eq "$(navclick "$target")" "1" "re-selecting $target for the resize witness"
if ! poll_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return ids==='$target'&&!!a&&a.getAttribute('data-category')==='$target'})()" 15; then
  die "re-selecting $target did not filter the document to it (doc: $(doc_ids), active: $(active_id))"
fi
assert_eq "$(force_scroll 999999)" "1" "scroll to the section's end again"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.abs(b.scrollTop-$hi)<=2})()" 10; then
  die "the section's end moved on the re-selection (scrollTop: $(scroll_top), want $hi)"
fi
held=$(scroll_top)
ab set viewport 1280 900 >/dev/null 2>&1
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.round(b.clientHeight)>$vp})()" 10; then
  die "the pane never took the taller viewport (clientHeight: $(jsq '(()=>{const b=document.querySelector(".settings-body");return Math.round(b.clientHeight)})()' 2>&1 | plain), was $vp)"
fi
geom=$(sec_geom "$target")
top=${geom%%|*}; rest=${geom#*|}; h=${rest%%|*}; rest=${rest#*|}; vp2=${rest%%|*}; content=${rest##*|}
range=$(sec_range "$top" "$h" "$vp2" "$content")
lo2=${range%%|*}; hi2=${range##*|}
[ "${hi2:-0}" -lt "${held:-0}" ] 2>/dev/null || die "the taller pane did not move the section's end ($held -> $hi2) - the witness needs a shorter max"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.abs(b.scrollTop-$hi2)<=3})()" 10; then
  die "the clamp kept a STALE max after the pane grew (scrollTop: $(scroll_top), want the new end $hi2, was $held)"
fi
step "ok: the boundary follows the pane (max $held -> $hi2 after the pane grew)"
ab set viewport 1280 800 >/dev/null 2>&1
if ! wait_js "(()=>{const b=document.querySelector('.settings-body');return !!b&&Math.round(b.clientHeight)>=$vp})()" 10; then
  die "the pane never returned to its 800px box"
fi
# leave the pane with NOTHING selected: the sticky block below scrolls freely
jsq '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");if(a) a.click();return 1})()' >/dev/null 2>&1
if ! poll_js '(()=>{return !document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active")})()' 10; then
  die "the clamp witness did not leave a clean, unselected pane (active: $(active_id))"
fi

# --- sticky: the heading pins, the next one pushes it out ----------------
# card t_a8063683 (Endo, 2026-11-11): the document's own section headings are
# the pane's ONLY headings, and they STICK - the effect the deleted mirror
# stripe was imitating. Two witnesses only a browser can give: the heading
# holds at the pane's top edge while its section scrolls under it, and the
# NEXT section's heading takes that edge and pushes the previous one out.
# (Run last: these scrolls unstick the spy, which may move the highlight.)
stuck=$(jsq '(()=>{const root=document.querySelector(".settings-body");const el=document.querySelector(".settings-category");if(!root||!el) return "missing";const h=el.querySelector(".settings-section-title");if(!h) return "no-heading";const style=getComputedStyle(h);root.scrollTop=root.scrollTop+(el.getBoundingClientRect().top-root.getBoundingClientRect().top)+60;const rt=root.getBoundingClientRect().top;const ht=h.getBoundingClientRect().top;const st=el.getBoundingClientRect().top;return (style.position==="sticky"&&Math.abs(ht-rt)<=1&&st<=rt-40)?"pinned":"loose:"+style.position+"@"+Math.round(ht-rt)+"/"+Math.round(st-rt)})()' 2>&1 | plain)
assert_eq "$stuck" "pinned" "scrolling inside a section pins its heading at the pane's top edge"

pushed=$(jsq '(()=>{const root=document.querySelector(".settings-body");const secs=[...document.querySelectorAll(".settings-category")];const rt0=root?root.getBoundingClientRect().top:0;const i=secs.findIndex(s=>{const h=s.querySelector(".settings-section-title");return !!h&&Math.abs(h.getBoundingClientRect().top-rt0)<=1});if(i<0||i+1>=secs.length) return "no-next";const cur=secs[i],next=secs[i+1];root.scrollTop=root.scrollTop+(next.getBoundingClientRect().top-root.getBoundingClientRect().top);const rt=root.getBoundingClientRect().top;const nh=next.querySelector(".settings-section-title"),oh=cur.querySelector(".settings-section-title");const nt=nh?nh.getBoundingClientRect().top:999,ot=oh?oh.getBoundingClientRect().top:999;return (Math.abs(nt-rt)<=1&&ot<rt-1)?"pushed":"bad:"+Math.round(nt-rt)+"/"+Math.round(ot-rt)})()' 2>&1 | plain)
assert_eq "$pushed" "pushed" "the next heading takes the top edge and pushes the previous one out"
step "ok: section headings are sticky and push each other out"

# --- the two-screen model is GONE, not hidden ---------------------------
gone=$(jsq '(()=>{const back=document.querySelectorAll(".settings-back").length;const screens=[...document.querySelectorAll("aside.settings .settings-content")].length;const nav=[...document.querySelectorAll("aside.settings .settings-nav")].length;return back+"|"+screens+"|"+nav})()' 2>&1 | plain)
assert_eq "$gone" "0|1|1" "one document, one nav, no back control"

step "113-settings-nav green"
