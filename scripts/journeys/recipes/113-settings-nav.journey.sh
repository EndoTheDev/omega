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
#   narrow    - the SAME document; the nav is a drawer that jumps and then
#               closes. There is no second screen and no back control.
#
# No seeding, no model: the lane's own home is enough. Residue-free.
. "${JROOT:?run must export JROOT}/lib.sh"

plain() { tr -d '"'; }
scroll_top() { jsq '(()=>{const b=document.querySelector(".settings-body");return b?Math.round(b.scrollTop):-1})()' 2>&1 | plain; }
nav_ids() { jsq '(()=>{return [...document.querySelectorAll(".settings-nav-cat")].map(e=>e.getAttribute("data-category")).join(",")})()' 2>&1 | plain; }
doc_ids() { jsq '(()=>{return [...document.querySelectorAll(".settings-category")].map(e=>e.getAttribute("data-category")).join(",")})()' 2>&1 | plain; }
active_id() { jsq '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");return a?a.getAttribute("data-category"):""})()' 2>&1 | plain; }
navclick() {
  jsq "(()=>{const b=[...document.querySelectorAll('.settings-nav-cat,.settings-nav-sub')].find(x=>x.getAttribute('data-category')==='$1');if(!b) return 0;b.click();return 1})()" 2>&1 | plain
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
firstdoc=$(jsq '(()=>{const s=document.querySelector(".settings-category");return s?s.getAttribute("data-category"):"none"})()' 2>&1 | plain)
assert_eq "$firstdoc" "common" "the document opens on the curated category (nav order)"
rows=$(jsq '(()=>{return document.querySelectorAll(".setting-item,.setting-row").length})()' 2>&1 | plain)
[ "${rows:-0}" -gt 0 ] 2>/dev/null || die "the document rendered no knob rows ($rows)"
step "ok: the one document renders every category ($docids) with $rows rows"

active=$(active_id)
assert_eq "$active" "" "a fresh pane has NOTHING selected"
kids=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | plain)
assert_eq "$kids" "0" "nothing selected -> every branch is collapsed (expansion follows selection)"
top=$(scroll_top)
assert_eq "$top" "0" "nothing selected -> the document opens at the TOP"
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "" "a fresh pane carries no anchor"

# --- a click JUMPS: select + expand + scroll ------------------------------
target="provider"
assert_eq "$(navclick "$target")" "1" "the provider category is a jump link"
if ! poll_js "(()=>{const b=document.querySelector('.settings-body');if(!b||b.scrollTop<50) return false;const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')==='$target'})()" 15; then
  die "the nav click did not scroll the pane to $target and mark it (scrollTop: $(scroll_top), active: $(active_id))"
fi
jumped=$(scroll_top)
step "ok: a nav click SCROLLS the pane (scrollTop 0 -> $jumped) and selects $target"
kids=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | plain)
[ "${kids:-0}" -gt 0 ] 2>/dev/null || die "selecting a parent did not expand its subsection jump links"
only=$(jsq '(()=>{const open=[...document.querySelectorAll(".settings-nav-row")].filter(r=>{const c=r.querySelector(".settings-nav-chev");return !!c&&c.getAttribute("aria-expanded")==="true"}).map(r=>r.querySelector(".settings-nav-cat").getAttribute("data-category"));return open.join(",")})()' 2>&1 | plain)
assert_eq "$only" "$target" "exactly ONE branch is open (expansion is derived from the selection)"
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "$target" "the click is remembered as the anchor"
# the section's heading sits at the pane's top edge (the jump really landed)
edge=$(jsq "(()=>{const root=document.querySelector('.settings-body');const el=document.querySelector('.settings-category[data-category=\"$target\"]');if(!root||!el) return 999;return Math.round(Math.abs(el.getBoundingClientRect().top-root.getBoundingClientRect().top))})()" 2>&1 | plain)
[ "${edge:-999}" -le 8 ] 2>/dev/null || die "the jump landed $edge px off the pane's top edge"
step "ok: the jumped-to section heads the pane (${edge}px)"

# --- the subsections are jump links too ----------------------------------
assert_eq "$(navclick "provider/models")" "1" "a subsection is a jump link"
if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')==='provider/models'})()" 15; then
  die "the subsection click did not mark provider/models (active: $(active_id))"
fi
subedge=$(jsq '(()=>{const root=document.querySelector(".settings-body");const el=[...document.querySelectorAll(".settings-block")].find(b=>b.getAttribute("data-category")==="provider/models");if(!root||!el) return 999;return Math.round(Math.abs(el.getBoundingClientRect().top-root.getBoundingClientRect().top))})()' 2>&1 | plain)
[ "${subedge:-999}" -le 8 ] 2>/dev/null || die "the subsection jump landed $subedge px off the top edge"
step "ok: a subsection jump lands its block at the top (${subedge}px)"

# --- a second click on the selected entry DESELECTS ----------------------
# one click does all three in reverse: highlight off, branch collapsed, top
assert_eq "$(navclick "provider/models")" "1" "the selected subsection is clickable"
if ! poll_js '(()=>{const b=document.querySelector(".settings-body");const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");return !!b&&b.scrollTop===0&&!a&&document.querySelectorAll(".settings-nav-sub").length===0})()' 15; then
  die "the second click did not deselect (scrollTop: $(scroll_top), active: $(active_id), children: $(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | plain))"
fi
step "ok: a second click DESELECTS - highlight cleared, branch collapsed, pane back at the top"
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "" "a DESELECT persists as NO anchor"

# --- scroll-spy: the nav follows the pane --------------------------------
spy="server"
assert_eq "$(scroll_pane_to "$spy")" "1" "the pane accepts a user scroll"
if ! poll_js "(()=>{const a=document.querySelector('.settings-nav-cat.active,.settings-nav-sub.active');return !!a&&a.getAttribute('data-category')==='$spy'})()" 15; then
  die "the scroll-spy did not follow the pane to $spy (active: $(active_id), scrollTop: $(scroll_top))"
fi
step "ok: scrolling moves the highlight to the section in view ($spy)"
spyrows=$(jsq '(()=>{return document.querySelectorAll(".setting-item,.setting-row").length})()' 2>&1 | plain)
assert_eq "$spyrows" "$rows" "selection never filters the document"

# --- search: filter in place, grey the dead entries ----------------------
full_ids=$(doc_ids)
full_nav=$(nav_ids)
ab fill '.settings-filterbar input' "model" >/dev/null 2>&1 || die "search input never accepted text"
if ! wait_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids.length>0&&ids!=='${full_ids}'})()" 10; then
  die "the query did not filter the document in place ($(doc_ids) of $full_ids)"
fi
filtered=$(doc_ids)
dim=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-cat.dim,.settings-nav-sub.dim").length})()' 2>&1 | plain)
[ "${dim:-0}" -gt 0 ] 2>/dev/null || die "a live query greyed nothing"
common=$(jsq '(()=>{return document.querySelector(".settings-nav-cat[data-category=\"common\"]")?1:0})()' 2>&1 | plain)
assert_eq "$common" "1" "the curated category STAYS in the nav (greyed or lit, never hidden)"
assert_eq "$(nav_ids)" "$full_nav" "the nav keeps its shape under a query"
step "ok: the query filters the document ($filtered) and greys $dim nav entries"

ab fill '.settings-filterbar input' "" >/dev/null 2>&1 || die "search input never cleared"
if ! wait_js "(()=>{const ids=[...document.querySelectorAll('.settings-category')].map(e=>e.getAttribute('data-category')).join(',');return ids==='${full_ids}'})()" 10; then
  die "clearing the query did not restore the full document (now: $(doc_ids))"
fi
assert_eq "$(doc_ids)" "$full_ids" "clearing the query brings every category back"
step "ok: clearing the query restores the whole document"

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
if ! poll_js '(()=>{const n=document.querySelector(".settings-nav");const b=document.querySelector(".settings-body");const anchor=localStorage.getItem("omega.settings-category");return !n&&!!b&&b.scrollTop>50&&anchor==="compaction"})()' 15; then
  die "a drawer jump did not scroll the pane and close the drawer (scrollTop: $(scroll_top), anchor: $(jsq '(()=>localStorage.getItem("omega.settings-category")||"")()' 2>&1 | plain))"
fi
step "ok: a drawer jump scrolls the pane and closes the drawer"

ab set viewport 1280 800 >/dev/null 2>&1
if ! wait_js '(()=>{const a=document.querySelector("aside.settings");const n=document.querySelector(".settings-nav");return !!a&&!a.classList.contains("narrow")&&!!n})()' 15; then
  die "widening did not restore the always-on nav"
fi

# --- memory: a reopen restores the anchor --------------------------------
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | plain)
assert_eq "$stored" "compaction" "the last anchor is remembered in CLIENT storage"
before=$(scroll_top)
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
if ! poll_js "$GEAR" 15; then
  die "settings gear never re-rendered after the reopen"
fi
if ! poll_js '(()=>{return !!document.querySelector(".settings-body")})()' 15; then
  die "the document never re-rendered after the reopen"
fi
if ! poll_js '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");const b=document.querySelector(".settings-body");return !!a&&a.getAttribute("data-category")==="compaction"&&!!b&&b.scrollTop>50})()' 15; then
  die "the reopen did not restore the anchor (active: $(active_id), scrollTop: $(scroll_top))"
fi
step "ok: the remembered anchor survives a reload (scrollTop $before -> $(scroll_top))"

# --- the two-screen model is GONE, not hidden ---------------------------
gone=$(jsq '(()=>{const back=document.querySelectorAll(".settings-back").length;const screens=[...document.querySelectorAll("aside.settings .settings-content")].length;const nav=[...document.querySelectorAll("aside.settings .settings-nav")].length;return back+"|"+screens+"|"+nav})()' 2>&1 | plain)
assert_eq "$gone" "0|1|1" "one document, one nav, no back control"

step "113-settings-nav green"
