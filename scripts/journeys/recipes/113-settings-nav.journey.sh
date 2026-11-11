#!/bin/bash
# 113-settings-nav - the settings MASTER-DETAIL nav (card t_af7e1150,
# settings tier 4). One worker owns SettingsTab.vue at a time; this recipe
# is the live witness for the four rulings that only a browser can show:
#
#   landing   - a fresh client opens on Commonly Used (ruling 4/13: the
#               virtual top category is FIRST, and the remembered id is
#               client storage - cleared here, so the default is provable).
#   two levels- the nav renders parents AND their children, and every
#               parent carries its own disclosure chevron (card t_5656d52f
#               SUPERSEDES ruling 5): the chevron collapses/expands, the
#               label still SELECTS, a child is indented under its domain,
#               and the content pane follows the selection - the whole
#               category (heading + count, ruling 9) or one child.
#   search    - a live query is CROSS-category in the content pane, while
#               the nav KEEPS ITS SHAPE and greys the categories with no
#               hit (ruling 6) - it never hides an entry, and the curated
#               category stays in the list even though search mode skips
#               its section (no knobs listed twice).
#   modified  - the modified-only switch above the list narrows it to the
#               knobs that differ from their base (ruling 8) and restoring
#               it brings the full list back.
#   narrow    - a narrow pane is TWO SCREENS of the same two components
#               (ruling 12): the nav alone, then the content full width
#               with a back control - never an accordion, never a dropdown.
#   memory    - the last selected category is remembered PER CLIENT and a
#               reopen lands on it (ruling 13).
#   gone      - the accordion is deleted, not hidden (step 10): no group
#               head, no collapse class anywhere in the pane (card
#               t_5656d52f re-introduced ONLY the nav's own disclosure -
#               the accordion's group machinery stays dead).
#
# No seeding, no model: the lane's own home is enough. Residue-free.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

# a virgin client: drop the remembered category (the pane reads it at
# setup, so the reload is the customer's first-ever open)
js 'localStorage.removeItem("omega.settings-category")' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true

GEAR='(()=>{const b=document.querySelector("header.menubar button[aria-label=\"Open settings\"]");if(!b) return false;b.click();return true})()'
if ! poll_js "$GEAR" 15; then
  die "settings menubar gear never rendered"
fi
if ! poll_js '(()=>{return !!document.querySelector(".settings-nav")&&!!document.querySelector(".settings-content")})()' 15; then
  die "the master-detail regions never rendered (nav + content)"
fi
step "ok: Settings opened on the two regions"

title_now() {
  jsq '(()=>{return (document.querySelector(".settings-content-title")||{textContent:""}).textContent.trim().replace(/\s+/g," ")})()' 2>&1 | tr -d '"'
}
navclick() {
  jsq "(()=>{const b=[...document.querySelectorAll('.settings-nav-cat,.settings-nav-sub')].find(x=>x.getAttribute('data-category')==='$1');if(!b) return 0;b.click();return 1})()" 2>&1 | tr -d '"'
}

# --- landing: Commonly Used, first in the nav, its curated rows below ---
landing=$(title_now)
case "$landing" in
  "Commonly Used"*) step "ok: a fresh client lands on Commonly Used ($landing)" ;;
  *) die "a fresh client did not land on Commonly Used (title: $landing)" ;;
esac
first=$(jsq '(()=>{const b=document.querySelector(".settings-nav-cat");return b?b.getAttribute("data-category"):"none"})()' 2>&1 | tr -d '"')
assert_eq "$first" "common" "Commonly Used is the FIRST nav category (ruling 4)"
rows=$(jsq '(()=>{return document.querySelectorAll(".setting-item,.setting-row").length})()' 2>&1 | tr -d '"')
[ "${rows:-0}" -gt 0 ] 2>/dev/null || die "the curated category rendered no rows ($rows)"
step "ok: the curated category renders its $rows rows"

# --- two levels, each parent carrying its OWN disclosure chevron ----------
# card t_5656d52f SUPERSEDES ruling 5: children still render at rest (the
# nav opens EXPANDED - nothing looks missing), but a parent can be
# collapsed, and the chevron is a SEPARATE hit target from the label (a
# parent must stay selectable without being expanded).
levels=$(jsq '(()=>{const parents=document.querySelectorAll(".settings-nav-cat").length;const kids=document.querySelectorAll(".settings-nav-sub").length;return parents+"/"+kids})()' 2>&1 | tr -d '"')
case "$levels" in
  */0) die "the nav rendered no child level (parents/children: $levels)" ;;
  *) step "ok: the nav opens with both levels showing (parents/children: $levels)" ;;
esac
indent=$(jsq '(()=>{const k=document.querySelector(".settings-nav-sub");if(!k) return 0;const p=document.querySelector(".settings-nav-cat");if(!p) return 0;const ki=parseFloat(getComputedStyle(k).paddingLeft);const pi=parseFloat(getComputedStyle(p).paddingLeft);return ki>pi?1:0})()' 2>&1 | tr -d '"')
assert_eq "$indent" "1" "a child row is INDENTED under its parent"
chevrons=$(jsq '(()=>{return document.querySelectorAll(".settings-nav .settings-nav-chev").length})()' 2>&1 | tr -d '"')
expanders=$(jsq '(()=>{return document.querySelectorAll(".settings-nav [aria-expanded]").length})()' 2>&1 | tr -d '"')
[ "${chevrons:-0}" -gt 0 ] 2>/dev/null || die "no parent carries a disclosure chevron"
assert_eq "$expanders" "$chevrons" "every chevron reports its own open state"
opened=$(jsq '(()=>{const c=document.querySelector(".settings-nav-chev");return c?c.getAttribute("aria-expanded"):"none"})()' 2>&1 | tr -d '"')
assert_eq "$opened" "true" "the nav opens EXPANDED (today's behaviour at rest)"
kids_before=$(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | tr -d '"')
jsq '(()=>{const row=[...document.querySelectorAll(".settings-nav-row")].find(r=>{const b=r.querySelector(".settings-nav-cat");return b&&b.getAttribute("data-category")==="provider"});const c=row?row.querySelector(".settings-nav-chev"):null;if(!c)return 0;c.click();return 1})()' >/dev/null 2>&1 \
  || die "the provider category carries no chevron to click"
if ! wait_js "(()=>{const row=[...document.querySelectorAll('.settings-nav-row')].find(r=>{const b=r.querySelector('.settings-nav-cat');return b&&b.getAttribute('data-category')==='provider'});const c=row?row.querySelector('.settings-nav-chev'):null;const kids=document.querySelectorAll('.settings-nav-sub').length;return !!c&&c.getAttribute('aria-expanded')==='false'&&kids<${kids_before}})()" 10; then
  die "collapsing the provider category did not hide its children (children before: $kids_before)"
fi
step "ok: the chevron collapses a parent ($kids_before children -> $(jsq '(()=>{return document.querySelectorAll(".settings-nav-sub").length})()' 2>&1 | tr -d '"'))"
# the LABEL of that collapsed parent still selects it (two hit targets)
assert_eq "$(navclick provider)" "1" "a collapsed parent's label is still a click target"
if ! wait_js "(()=>{const t=((document.querySelector('.settings-content-title')||{}).textContent||'').trim();return t.indexOf('provider')===0&&!!document.querySelector('.settings-category')})()" 10; then
  die "clicking the LABEL of a collapsed parent did not select it (title: $(title_now))"
fi
step "ok: the label selects a collapsed category without expanding it"
# re-open, so the rest of the recipe sees the expanded nav
jsq '(()=>{const row=[...document.querySelectorAll(".settings-nav-row")].find(r=>{const b=r.querySelector(".settings-nav-cat");return b&&b.getAttribute("data-category")==="provider"});const c=row?row.querySelector(".settings-nav-chev"):null;if(c&&c.getAttribute("aria-expanded")==="false") c.click();return 1})()' >/dev/null 2>&1
wait_js "(()=>{return document.querySelectorAll('.settings-nav-sub').length>=${kids_before}})()" 10 \
  || die "the chevron never re-opened the parent"

# --- the parent shows the whole category; the child shows its slice ---
assert_eq "$(navclick provider)" "1" "the provider category is clickable"
if ! wait_js "(()=>{const t=((document.querySelector('.settings-content-title')||{}).textContent||'').trim();return t.indexOf('provider')===0&&!!document.querySelector('.settings-category')})()" 10; then
  die "the provider category never rendered (title: $(title_now))"
fi
heads=$(jsq '(()=>{return [...document.querySelectorAll(".settings-subgroup-title")].map(e=>(e.textContent||"").trim()).join(",")})()' 2>&1 | tr -d '"')
case "$heads" in
  connection*models*|models*connection*) step "ok: the parent view heads its children ($heads)" ;;
  *) die "the parent view does not name its children (heads: $heads)" ;;
esac
# ruling 9: the heading carries the count the nav shows for that category
counts=$(jsq '(()=>{const nav=document.querySelector(".settings-nav-cat[data-category=\"provider\"]");const navn=(nav?nav.querySelector(".settings-nav-count").textContent:"").trim();const head=(document.querySelector(".settings-knob-count")||{textContent:""}).textContent.trim();return navn+"|"+head})()' 2>&1 | tr -d '"')
navn=${counts%%|*}; headn=${counts##*|}
assert_eq "$headn" "$navn" "the heading's knob count matches the nav's count for the category"

assert_eq "$(navclick provider/models)" "1" "a provider child is clickable"
if ! wait_js "(()=>{const t=((document.querySelector('.settings-content-title')||{}).textContent||'').trim();return t.indexOf('provider')===0&&!!document.querySelector('.setting-item')})()" 10; then
  die "the provider/models child never rendered its rows"
fi
child=$(jsq '(()=>{const nav=document.querySelector(".settings-nav-sub[data-category=\"provider/models\"]");const navn=(nav?nav.querySelector(".settings-nav-count").textContent:"").trim();const head=(document.querySelector(".settings-knob-count")||{textContent:""}).textContent.trim();const rows=document.querySelectorAll(".setting-item,.setting-row").length;return navn+"|"+head+"|"+rows})()' 2>&1 | tr -d '"')
cn=${child%%|*}; rest=${child#*|}; ch=${rest%%|*}; crows=${rest##*|}
assert_eq "$ch" "$cn" "the child view's count is the child's own, not the category's"
assert_eq "$crows" "$cn" "the child view renders exactly that many rows"
step "ok: a child selection is one domain slice ($cn rows), not the whole category"

# --- search: cross-category content, greyed nav, shape preserved ---
ab fill '.settings-filterbar input' "model" >/dev/null 2>&1 || die "search input never accepted text"
if ! wait_js "(()=>{const s=document.querySelectorAll('.settings-category').length;return s>1})()" 10; then
  die "the query did not render cross-category hits in the content pane (ruling 6)"
fi
search=$(jsq '(()=>{const s=document.querySelectorAll(".settings-category").length;const nav=document.querySelectorAll(".settings-nav-cat,.settings-nav-sub").length;const dim=document.querySelectorAll(".settings-nav-cat.dim,.settings-nav-sub.dim").length;const common=document.querySelector(".settings-nav-cat[data-category=\"common\"]")?1:0;return s+"|"+nav+"|"+dim+"|"+common})()' 2>&1 | tr -d '"')
secs=${search%%|*}; rest=${search#*|}; navtotal=${rest%%|*}; rest=${rest#*|}; dimmed=${rest%%|*}; common=${rest##*|}
[ "${dimmed:-0}" -gt 0 ] 2>/dev/null || die "a live query greyed nothing (nav: $navtotal entries, $dimmed dimmed)"
assert_eq "$common" "1" "the curated category STAYS in the nav while searching (greyed or lit, never hidden)"
[ "${navtotal:-0}" -gt 4 ] 2>/dev/null || die "the nav lost its shape under a query ($navtotal entries)"
step "ok: search shows $secs category sections while the nav keeps $navtotal entries ($dimmed greyed)"
dupe=$(jsq '(()=>{const ids=[...document.querySelectorAll(".settings-category")].map(s=>s.getAttribute("data-section"));return ids.includes("common")?1:0})()' 2>&1 | tr -d '"')
assert_eq "$dupe" "0" "search mode skips the curated section (a hit renders once, under its domain)"

# clearing the query restores the selected category's own list
ab fill '.settings-filterbar input' "" >/dev/null 2>&1 || die "search input never cleared"
if ! wait_js "(()=>{return document.querySelectorAll('.settings-category').length===1})()" 10; then
  die "clearing the query did not return to the selected category"
fi
step "ok: clearing the query returns to the one selected category"

# --- modified-only (ruling 8) ---
before=$(jsq '(()=>{return document.querySelectorAll(".setting-item,.setting-row").length})()' 2>&1 | tr -d '"')
js '(()=>{const b=document.querySelector(".settings-modified");if(b) b.click();return 1})()' >/dev/null 2>&1
if ! wait_js '(()=>{const b=document.querySelector(".settings-modified");return !!b&&b.getAttribute("aria-pressed")==="true"&&b.classList.contains("on")})()' 10; then
  die "the modified-only switch never turned on"
fi
after=$(jsq '(()=>{return document.querySelectorAll(".setting-item,.setting-row").length})()' 2>&1 | tr -d '"')
[ "${after:-0}" -le "${before:-0}" ] 2>/dev/null || die "modified-only GREW the list ($before -> $after)"
step "ok: modified-only narrows the list ($before -> $after rows)"
js '(()=>{const b=document.querySelector(".settings-modified");if(b) b.click();return 1})()' >/dev/null 2>&1
if ! wait_js "(()=>{return document.querySelectorAll('.setting-item,.setting-row').length===$before})()" 10; then
  die "turning modified-only off did not restore the full list (expected $before rows)"
fi
step "ok: switching it off restores the full list ($before rows)"

# --- narrow: drill-down, two screens, one renderer (ruling 12) ---
# 340px: the pane spans the window at this width (378px on a 380 viewport
# is still WIDE - the threshold is 360), so 340 is the honest narrow host.
ab set viewport 340 780 >/dev/null 2>&1
if ! wait_js '(()=>{const a=document.querySelector("aside.settings");return !!a&&a.classList.contains("narrow")})()' 15; then
  die "the pane never flipped narrow at 340px"
fi
screens=$(jsq '(()=>{const n=document.querySelector(".settings-nav");const c=document.querySelector(".settings-content");return (n?1:0)+"|"+(c?1:0)})()' 2>&1 | tr -d '"')
assert_eq "$screens" "1|0" "a narrow pane shows the NAV screen alone (no content beside it)"
step "ok: narrow = the nav screen, full width"

assert_eq "$(navclick provider)" "1" "a category tap drills into its content"
if ! wait_js '(()=>{const n=document.querySelector(".settings-nav");const c=document.querySelector(".settings-content");return !n&&!!c})()' 10; then
  die "a narrow category tap did not open the content screen"
fi
back=$(jsq '(()=>{const b=document.querySelector(".settings-back");return b?(b.getAttribute("aria-label")||"")+"|"+(b.textContent||"").trim():"MISSING"})()' 2>&1 | tr -d '"')
assert_eq "$back" "back to categories|categories" "the content screen carries a labelled back control"
step "ok: the drilled content screen shows the back control"

js '(()=>{const b=document.querySelector(".settings-back");if(b) b.click();return 1})()' >/dev/null 2>&1
if ! wait_js '(()=>{const n=document.querySelector(".settings-nav");const c=document.querySelector(".settings-content");return !!n&&!c})()' 10; then
  die "the back control did not return to the nav screen"
fi
step "ok: the back control returns to the nav screen"

# --- memory: a reopen lands on the remembered category (ruling 13) ---
stored=$(jsq '(()=>{return localStorage.getItem("omega.settings-category")||""})()' 2>&1 | tr -d '"')
assert_eq "$stored" "provider" "the selection is remembered in CLIENT storage"
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
if ! poll_js "$GEAR" 15; then
  die "settings gear never re-rendered after the reopen"
fi
if ! poll_js '(()=>{return !!document.querySelector(".settings-nav")})()' 15; then
  die "the nav never re-rendered after the reopen"
fi
active=$(jsq '(()=>{const a=document.querySelector(".settings-nav-cat.active,.settings-nav-sub.active");return a?a.getAttribute("data-category"):"none"})()' 2>&1 | tr -d '"')
assert_eq "$active" "provider" "a reopen lands on the remembered category"
step "ok: the remembered category survives a reload"

# --- the accordion is GONE, not hidden (step 10) ---
# card t_5656d52f: the ONLY disclosure left is the nav's own chevron; the
# accordion's group head, expanders and wrappers stay dead
gone=$(jsq '(()=>{const head=document.querySelectorAll(".settings-group-head").length;const exp=[...document.querySelectorAll("aside.settings [aria-expanded]")].filter(e=>!e.classList.contains("settings-nav-chev")).length;const old=document.querySelectorAll(".settings-group").length;return head+"|"+exp+"|"+old})()' 2>&1 | tr -d '"')
assert_eq "$gone" "0|0|0" "no group head, no expander outside the nav, no collapse wrapper survives in the pane"

# widening puts both regions back on screen (one renderer, two widths)
ab set viewport 1280 800 >/dev/null 2>&1
if ! wait_js '(()=>{const a=document.querySelector("aside.settings");const n=document.querySelector(".settings-nav");const c=document.querySelector(".settings-content");return !!a&&!a.classList.contains("narrow")&&!!n&&!!c})()' 15; then
  die "widening the pane did not restore the master-detail split"
fi
step "ok: widening restores nav + content side by side"

step "113-settings-nav green"
