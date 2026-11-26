#!/bin/bash
# 112-statusbar-zones - the status bar's two zones (card t_8fd38f88).
# Endo's ruling (2026-11-10, settings-tier2 round 1/2): the bar is VSCode's
# arrangement - LEFT is work context (the workdir selector, which left the
# composer), RIGHT is live metrics + the build. No model segment, no
# session label. The CLICKABLE SET IS CLOSED: workdir, health, pending.
# Narrow screens collapse to the health dot + the workdir.
#
#   zones    - two children: .zone.left leads with the HEALTH segment, then
#              the picker (.ws-btn); .zone.right hosts the metrics with the
#              version LAST (card t_5656d52f); the composer renders NO .ws-btn.
#   closed   - the bar's only <button>s are health + (when live) pending;
#              every metric segment stays a span. Buttons INSIDE the open
#              panel are panel content, never bar segments.
#   picker   - clicking the bar's chip opens the SAME .ws-pop directory
#              browser (the component's class law survived the move).
#   conns    - the health segment IS the connection control (card t_3cd02fe3):
#              the dot plus the connected instance's name, and the panel leads
#              with the connection list - the current row marked, the saved
#              rows above the facts, no add form, no remove, no reconnect.
#   health   - the dot opens .health-pop: server reachable, provider,
#              database, uptime and last error rows. provider/database/
#              last error are OPTIONAL by wire law (card t_0b52537a, after
#              701ce273 sourced them): each row renders a real value when
#              /health carries the fact and "not reported" when it does
#              not - the probe accepts either, and the "no source" note
#              must render exactly when at least one row is unsourced.
#   narrow   - at 700px: every .hide-sm segment is gone, the health DOT and
#              the workdir chip are still there. At 340px the dot is alone
#              and the open panel is a bar-wide sheet inside the viewport.
#
# No model needed: the bar is chrome, the picker is a filesystem listing and
# the popover reads the health frame the socket already pushes.
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
wait_js '!!document.querySelector(".statusbar .zone.left .ws-btn")' 20 \
  || die "the status bar's left zone never rendered the workdir picker"
step "ok: the bar's left zone hosts the workdir picker"

# --- 1. two zones, and the composer chip is gone -----------------------------
probe_zones() {
  jsq '(()=>{const bar=document.querySelector(".statusbar");if(!bar)return 0;const zones=[...bar.querySelectorAll(":scope > .zone")];if(zones.length!==2)return 0;const left=bar.querySelector(".zone.left"),right=bar.querySelector(".zone.right");if(!left||!right)return 0;if(!left.querySelector(".ws-btn"))return 0;if(left.children.length!==2)return 0;if(!left.children[0].classList.contains("health"))return 0;if(right.querySelector(".seg.seg-btn.health"))return 0;if(!right.querySelector(".seg[title^=\"server build\"]"))return 0;if(document.querySelector(".composer .ws-btn"))return 0;if(document.querySelector(".composer .ws-pop"))return 0;return 1})()'
}
v=$(probe_zones)
[ "$v" = "1" ] || die "the two-zone shape is off (left=workdir, right=metrics+build, composer chip gone): $(jsq '(()=>{const bar=document.querySelector(".statusbar");const z=[...bar.querySelectorAll(":scope > .zone")].map(x=>x.className);return "zones="+z.join("|")+" composerChip="+!!document.querySelector(".composer .ws-btn")})()')"
step "ok: two zones - workdir left, metrics + build right, no composer chip"

# --- 2. the clickable set is closed -----------------------------------------
probe_buttons() {
  jsq '(()=>{const bar=document.querySelector(".statusbar");const btns=[...bar.querySelectorAll("button")].filter(b=>!b.classList.contains("ws-btn")&&!b.closest(".health-pop"));const classes=btns.map(b=>b.className);for(const c of classes){if(!/(^|\s)seg-btn(\s|$)/.test(c))return 0}for(const c of classes){if(!/health|pending/.test(c))return 0}if(classes.length>2)return 0;if(!bar.querySelector(".seg.seg-btn.health"))return 0;return 1})()'
}
v=$(probe_buttons)
[ "$v" = "1" ] || die "the clickable set is not closed: $(jsq '(()=>{return [...document.querySelector(".statusbar").querySelectorAll("button")].map(b=>b.className).join("|")})()')"
step "ok: only health (and a live pending) are buttons - every metric stays a read"

# card t_5656d52f: the version segment is the LAST thing in the right zone
lastseg=$(jsq '(()=>{const r=document.querySelector(".statusbar .zone.right");if(!r)return 0;const segs=[...r.querySelectorAll(".seg")];const last=segs[segs.length-1];if(!last)return 0;const t=last.getAttribute("title")||"";return t.indexOf("server build")>=0?1:0})()' 2>&1 | tr -d '"')
assert_eq "$lastseg" "1" "the version segment is the RIGHTMOST segment on the bar"
step "ok: version sits at the far right, after the message count"

# card t_be30543a: the version segment carries the SHORT VERSION alone - the
# uptime it used to repeat ("· up 8m 20s") now lives ONLY in the health
# popover (step 4), so the bar must render no uptime string at all.
vseg=$(jsq '(()=>{const r=document.querySelector(".statusbar .zone.right");if(!r)return "no-zone";const segs=[...r.querySelectorAll(".seg")];const last=segs[segs.length-1];if(!last)return "no-seg";const t=(last.textContent||"").replace(/\s+/g," ").trim();if(/\bup\b/.test(t))return "uptime-in-bar:"+t;return t?("ok:"+t):"empty"})()' 2>&1 | tail -1 | tr -d '"')
case "$vseg" in
  ok:*) step "ok: the version segment reads the short version alone [$vseg]" ;;
  *) die "the bar's version segment should read the short version alone - uptime lives in the health popover now: $vseg" ;;
esac

# --- 3. the picker survived the move ---------------------------------------
jsq 'document.querySelector(".statusbar .ws-btn").click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".statusbar .ws-pop .ws-dirs")' 10 \
  || die "the bar's workdir chip opened no .ws-pop directory browser"
step "ok: the bar's picker opens the same directory popover"
jsq 'document.querySelector(".ws-pop .ws-close")?.click()' >/dev/null 2>&1

# --- 4. the health popover: the four facts, honestly sourced ---------------
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".health-pop")' 10 \
  || die "the health dot opened no detail popover"
# card t_5656d52f: the trigger leads the bar, so the popover anchors LEFT -
# it must render FULLY inside the viewport from that new position
if ! wait_js '(()=>{const p=document.querySelector(".health-pop");if(!p)return false;const r=p.getBoundingClientRect();const vw=document.documentElement.clientWidth;const vh=document.documentElement.clientHeight;return r.left>=0&&r.top>=0&&r.right<=vw&&r.bottom<=vh})()' 10; then
  die "the health popover does not render fully inside the pane from the left edge ($(jsq '(()=>{const p=document.querySelector(".health-pop");if(!p)return "missing";const r=p.getBoundingClientRect();return "left="+Math.round(r.left)+" right="+Math.round(r.right)+" top="+Math.round(r.top)+" bottom="+Math.round(r.bottom)+" vw="+document.documentElement.clientWidth+" vh="+document.documentElement.clientHeight})()' 2>&1 | tr -d '\"'))"
fi
step "ok: the health popover renders fully inside the pane (left-anchored)"

# --- 4b. the connection list rides the same panel (card t_3cd02fe3) ---------
# The bar's control names the connected instance and the panel LEADS with the
# connection list: the current row marked aria-current, the saved rows below it,
# and no write surface (settings owns writes) and no reconnect control
# (switching and the retry loop are the recovery - nothing new).
# The lane's list is user config (desktop.instances), so the expected row count
# comes from the same GET /instances the panel reads. The current origin's own
# row is the marked one and is never doubled in the switch list.
counts=$(curl -s "${JOURNEY_BASE_URL}/instances" \
  | python3 -c 'import json,sys
base = sys.argv[1].rstrip("/")
rows = (json.load(sys.stdin) or {}).get("instances") or []
print(len(rows), len([r for r in rows if (r.get("url") or "").rstrip("/") != base]))' "${JOURNEY_BASE_URL}" 2>/dev/null)
[ -n "$counts" ] || die "the lane answered no /instances body - the panel's list has no source"
saved_other=${counts#* }
probe_connections() {
  jsq "(()=>{const p=document.querySelector('.health-pop');if(!p)return 'no-panel';const head=p.querySelector('.hp-head');if(!head||head.textContent.trim().toLowerCase()!=='connections')return 'no-heading';const cur=p.querySelector('.hp-crow[aria-current=\"true\"]');if(!cur)return 'unmarked-current';const cname=((cur.querySelector('.hp-cname')||{}).textContent||'').trim();if(!cname)return 'current-unnamed';const bar=document.querySelector('.statusbar .seg.seg-btn.health');const label=(bar&&bar.getAttribute('aria-label'))||'';if(label!=='Connection: '+cname)return 'label['+label+']name['+cname+']';const rows=[...p.querySelectorAll('.hp-switch')];if(rows.length!==$saved_other)return 'rows['+rows.length+'/'+'$saved_other'+']';for(const r of rows){const n=((r.querySelector('.hp-cname')||{}).textContent||'').trim();const u=((r.querySelector('.hp-curl')||{}).textContent||'').trim();if(!n||!u)return 'row-incomplete['+(r.textContent||'').replace(/\\s+/g,' ')+']'}const bad=[...p.querySelectorAll('*')].filter(e=>/reconnect/i.test(((e.getAttribute('aria-label')||'')+(e.getAttribute('title')||'')+(e.tagName==='BUTTON'?(e.textContent||''):''))));if(bad.length)return 'reconnect-control';return 'ok'})()"
}
v=$(probe_connections | tr -d '"')
assert_eq "$v" "ok" "the panel leads with the connection list - current row marked, saved rows switchable, no reconnect"
step "ok: the panel lists the connections above the facts - current instance marked and named on the bar, $saved_other saved row(s) switchable, no add/remove/reconnect control"

# card t_0b52537a: provider / database / last error are OPTIONAL facts on the
# /health body (701ce273) - absent means "not reported", present means a
# rendered value (a dead provider still renders "not reachable"; a sourced
# but silent logger renders "none"). The probe is runtime-honest: it takes
# EITHER shape per row, and pins the note to the unsourced count - the
# "no source" footnote renders exactly when at least one row is unsourced.
probe_health() {
  jsq '(()=>{const p=document.querySelector(".health-pop");if(!p)return 0;const keys=[...p.querySelectorAll(".hp-key")].map(k=>k.textContent.trim());for(const k of ["server","provider","database","uptime","last error"]){if(!keys.includes(k))return 0}const val=k=>{const row=[...p.querySelectorAll(".hp-row")].find(r=>r.querySelector(".hp-key").textContent.trim()===k);return row?row.querySelector(".hp-val").textContent.trim():""};if(!/^(reachable|not reachable)$/.test(val("server")))return 0;if(!/^[0-9]+(s|m|h)/.test(val("uptime")))return 0;let unsourced=0;for(const k of ["provider","database","last error"]){const fv=val(k);if(fv==="not reported"){unsourced=unsourced+1;continue}if(!fv)return 0}const note=(p.querySelector(".hp-note")||{}).textContent||"";const t=note.trim();if((unsourced>0)!==(t.length>0))return 0;if(unsourced>0){if(t.indexOf("/health")<0)return 0;const lead=unsourced===1?"one fact has":unsourced+" facts have";if(t.indexOf(lead)!==0)return 0}return 1})()'
}
v=$(probe_health)
[ "$v" = "1" ] || die "the health popover's facts are wrong: $(jsq '(()=>{const rows=[...document.querySelectorAll(".health-pop .hp-row")].map(r=>r.querySelector(".hp-key").textContent+"="+r.querySelector(".hp-val").textContent.trim()).join(" | ");const n=document.querySelector(".health-pop .hp-note");return rows+" || note="+(n?n.textContent.trim():"<none>")})()')"
step "ok: health popover - reachable server, live uptime, each optional fact sourced or honestly not reported, note matches the gaps"
assert_eq "$(jsq 'document.querySelector(".statusbar .seg.seg-btn.health").getAttribute("aria-expanded")' | tr -d '"')" "true" "the health control reports its open state"
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!document.querySelector(".health-pop")' 10 || die "the health popover never closed"

# --- 5. narrow: health dot + workdir only ----------------------------------
ab set viewport 700 800 >/dev/null 2>&1
wait_layout   # the width cross re-lays out the bar: settle before probing
probe_narrow() {
  jsq '(()=>{const bar=document.querySelector(".statusbar");if(!bar)return 0;const hidden=e=>getComputedStyle(e).display==="none";const shown=[...bar.querySelectorAll(".hide-sm")];if(!shown.length)return 0;for(const e of shown){if(!hidden(e))return 0}const dot=bar.querySelector(".seg.seg-btn.health .dot");if(!dot||hidden(dot))return 0;const ws=bar.querySelector(".ws-btn");if(!ws||hidden(ws))return 0;return 1})()'
}
v=$(probe_narrow)
[ "$v" = "1" ] || die "narrow 700px did not collapse to health dot + workdir: $(jsq '(()=>{const bar=document.querySelector(".statusbar");return [...bar.querySelectorAll(".seg")].map(e=>e.className+"["+getComputedStyle(e).display+"]").join(" | ")})()')"
step "ok: 700px - health dot and workdir stay, every metric hides"

# --- 5b. 340px: the dot alone, the sheet inside the viewport (t_3cd02fe3) ---
# The panel is closed here (step 4 closed it). At the phone width the bar keeps
# the dot alone (the name is a .hide-sm) and the open panel must sit fully
# inside the viewport with no horizontal overflow - the geometry the menubar
# pill could not hold.
ab set viewport 340 720 >/dev/null 2>&1
wait_layout
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".health-pop")' 10 \
  || die "the connection panel did not open at 340px"
probe_340() {
  jsq '(()=>{const p=document.querySelector(".health-pop");if(!p)return "no-panel";const r=p.getBoundingClientRect();const vw=document.documentElement.clientWidth;if(r.left<0||r.right>vw)return "panel-outside:"+Math.round(r.left)+".."+Math.round(r.right)+"/"+vw;if(document.documentElement.scrollWidth>vw+1)return "hscroll:"+document.documentElement.scrollWidth+"/"+vw;const bar=document.querySelector(".statusbar");const dot=bar.querySelector(".seg.seg-btn.health .dot");if(!dot||getComputedStyle(dot).display==="none")return "dot-hidden";const name=bar.querySelector(".seg.seg-btn.health .hide-sm");if(name&&getComputedStyle(name).display!=="none")return "name-visible";return "ok"})()'
}
v=$(probe_340 | tr -d '"')
assert_eq "$v" "ok" "the 340px bar keeps the dot alone and the open sheet inside the viewport"
step "ok: 340px - the dot alone carries the name and the open sheet stays inside the viewport"
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!document.querySelector(".health-pop")' 10 || die "the panel never closed at 340px"
ab set viewport 1280 800 >/dev/null 2>&1 || true

assert_no_console_errors
step "112-statusbar-zones: green"
