#!/bin/bash
# 22-composer - the composer's SINGLE shape (card t_1750a9e3, ruling
# 2026-11-07 - the mode system died with it; supersedes the two-mode
# pins this recipe used to carry). ONE composer in the pane: the C5
# 'footer-send' arrangement stretched to the pane width (take A of
# sketch/composer-sketch-v3.html) - plus-btn + input on top, chips left
# + send right on a hairline-divided bottom row, full-width floating
# card with the base 12px inset, borders and rounded corners all around
# (NOT edge-to-edge take B). No 720px cap, no mode classes, no settings
# knob.
#
# Orphan law pinned live: a stored omega.composer-mode value (the
# retired setting's key) loads without error and is IGNORED - never
# read, never rewritten, left untouched. The end state removes the
# orphan key so the profile stays virgin.
#
# No model needed: the shape law is pure client DOM + CSS.
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session composer | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) composer $TAG"

# panejs <expr over p>: evaluate expr against the FOCUSED chat pane
# scoped to this run's label (recipe 30's ghost-tab law).
panejs() {
  jsq "(()=>{const panes=[...document.querySelectorAll('.dock-pane')];const p=panes.find(x=>[...x.querySelectorAll('.dock-tab')].some(d=>d.getAttribute('aria-selected')==='true' && (d.querySelector('.dock-tab-title')||{textContent:''}).textContent.includes('(journey) composer')));if(!p)return 'NO_PANE';return ''+($1);})()" 2>&1 | tail -1 | sed -e 's/^"//' -e 's/"$//' -e 's/\\"/"/g'
}

# scoped_wait <expr> <timeout_s>: poll the focused pane; timeout dies.
scoped_wait() {
  local expr="$1" t="$2" i=0 out
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(panejs "$expr")
    if [ "$out" = "1" ] || [ "$out" = "true" ]; then return 0; fi
    sleep 0.5
    i=$((i + 1))
  done
  die "scoped_wait timed out after ${t}s: $expr (last: $out)"
}

# reload_page: an explicit reload, then re-focus the journey row
reload_page() {
  jsq 'location.reload()' >/dev/null 2>&1 || true
  sleep 1.5
  open_journey_row "(journey) composer $TAG"
}

open_app
open_journey_row "(journey) composer $TAG"

# the composer must be reachable in THIS pane before any class assert
scoped_wait '!!p.querySelector(".composer")' 15
step "ok: composer reachable in the journey pane"

# --- 1. the single shape: one composer, no modes, C5 arrangement -------
assert_eq "$(panejs 'p.querySelectorAll(".composer").length')" "1" \
  "exactly ONE composer renders in the pane"
modecls=$(panejs '[...p.querySelector(".composer").classList].filter(c=>c.startsWith("mode-")).join(",")')
assert_eq "$modecls" "" "no mode-* class on the composer (the mode system died)"
mcap=$(panejs 'getComputedStyle(p.querySelector(".composer")).maxWidth')
assert_eq "$mcap" "none" "no width cap (the centered 720px card died)"
mleft=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginLeft')
mright=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginRight')
mbottom=$(panejs 'getComputedStyle(p.querySelector(".composer")).marginBottom')
assert_eq "$mleft" "12px" "the composer insets 12px from the pane's left edge"
assert_eq "$mright" "12px" "the composer insets 12px from the pane's right edge"
assert_eq "$mbottom" "12px" "the composer insets 12px from the pane's floor"
radius=$(panejs '["btl","btr","bbl","bbr"].map(c=>getComputedStyle(p.querySelector(".composer"))[c+"us"]).join(",")')
assert_eq "1" "$(awk -v r="$radius" 'BEGIN{n=split(r,a,",");ok=1;for(i=1;i<=n;i++)if(a[i]=="0px")ok=0;print ok?1:0}')" \
  "rounded corners all around (${radius} - a floating card, never take B edge-to-edge)"
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "column" "the C5 COLUMN card (two rows) - not the one-line bar"
# the two rows: the toolbar (chips + send) sits BELOW the input row
itop=$(panejs 'p.querySelector(".composer .composer-row").offsetTop')
rtop=$(panejs 'p.querySelector(".composer .toolbar").offsetTop')
assert_eq "1" "$(awk "BEGIN{print ($rtop > $itop) ? 1 : 0}")" \
  "rows split: the bottom row (${rtop}px) sits below the input row (${itop}px)"
# the C5 hairline divides the rows
btw=$(panejs 'getComputedStyle(p.querySelector(".composer .toolbar")).borderTopWidth')
assert_eq "$btw" "1px" "the toolbar's top hairline divides the two rows"
wrap=$(panejs 'getComputedStyle(p.querySelector(".composer .toolbar")).flexWrap')
assert_eq "$wrap" "nowrap" "the bottom row's chips never wrap"
# row 2 stance: chips left, send right (never a centered send)
gapr=$(panejs 'p.querySelector(".composer .send").getBoundingClientRect().right - p.querySelector(".composer").getBoundingClientRect().right')
assert_eq "1" "$(awk -v g="$gapr" 'BEGIN{print (g < 0 && g > -20) ? 1 : 0}')" \
  "the send anchors the bottom row's right edge (gap ${gapr}px)"
gapl=$(panejs '(p.querySelector(".composer .workspace-select")||p.querySelector(".composer .model-chip")).getBoundingClientRect().left - p.querySelector(".composer").getBoundingClientRect().left')
assert_eq "1" "$(awk -v g="$gapl" 'BEGIN{print (g > 0 && g < 30) ? 1 : 0}')" \
  "the chips anchor the bottom row's left edge (gap ${gapl}px)"
segdivs=$(panejs 'p.querySelectorAll(".composer .seg-div").length')
assert_eq "$segdivs" "0" "no seg-div divider (the W6 segments died with the full mode)"
ch=$(panejs 'p.querySelector(".composer").clientHeight')
assert_eq "1" "$(awk "BEGIN{print ($ch >= 56 && $ch < 130) ? 1 : 0}")" \
  "the composer is TWO rows tall (${ch}px - the one-line bar would sit under 56px)"
step "ok: the single shape holds (two rows, ${ch}px tall, pane-wide, 12px inset, no modes)"

# --- 2. orphan law: a stored compact value loads ignored, untouched ----
jsq 'localStorage.setItem("omega.composer-mode", "compact")' >/dev/null 2>&1 || true
reload_page
scoped_wait '!!p.querySelector(".composer")' 15
modecls=$(panejs '[...p.querySelector(".composer").classList].filter(c=>c.startsWith("mode-")).join(",")')
assert_eq "$modecls" "" "a stored 'compact' loads with no mode class (ignored orphan)"
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "column" "a stored 'compact' changes nothing (the C5 shape is fixed)"
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "compact" "the stored compact value is UNTOUCHED (never read, never rewritten)"
step "ok: stored compact is an ignored, untouched orphan"

# --- 3. orphan law: a stored full value too ----------------------------
jsq 'localStorage.setItem("omega.composer-mode", "full")' >/dev/null 2>&1 || true
reload_page
scoped_wait '!!p.querySelector(".composer")' 15
mcap=$(panejs 'getComputedStyle(p.querySelector(".composer")).maxWidth')
assert_eq "$mcap" "none" "a stored 'full' lifts no cap - the shape is one pane-wide card"
flexdir=$(panejs 'getComputedStyle(p.querySelector(".composer")).flexDirection')
assert_eq "$flexdir" "column" "a stored 'full' does NOT apply the retired one-row bar"
persisted=$(jsq 'localStorage.getItem("omega.composer-mode") || ""' 2>&1 | tr -d '"')
assert_eq "$persisted" "full" "the stored full value is UNTOUCHED (no migration code exists)"
step "ok: stored full is an ignored, untouched orphan"

# --- 4. clean end state -------------------------------------------------
jsq 'localStorage.removeItem("omega.composer-mode")' >/dev/null 2>&1 || true
assert_no_console_errors

cleanup_journey_sessions
step "GREEN: the single-composer law holds (ONE composer, C5 two-row arrangement pane-wide, 12px inset, no modes, orphan values ignored)"
