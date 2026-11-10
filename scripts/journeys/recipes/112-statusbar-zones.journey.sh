#!/bin/bash
# 112-statusbar-zones - the status bar's two zones (card t_8fd38f88).
# Endo's ruling (2026-11-10, settings-tier2 round 1/2): the bar is VSCode's
# arrangement - LEFT is work context (the workdir selector, which left the
# composer), RIGHT is live metrics + the build. No model segment, no
# session label. The CLICKABLE SET IS CLOSED: workdir, health, pending.
# Narrow screens collapse to the health dot + the workdir.
#
#   zones    - two children: .zone.left hosts the picker (.ws-btn), .zone.right
#              hosts the metrics; the composer renders NO .ws-btn.
#   closed   - the bar's only <button>s are health + (when live) pending;
#              every metric segment stays a span.
#   picker   - clicking the bar's chip opens the SAME .ws-pop directory
#              browser (the component's class law survived the move).
#   health   - the dot opens .health-pop: server reachable, provider,
#              database, uptime and last error rows. provider/database/
#              last error read "not reported": /health carries status,
#              uptime and version only (the honest gap, never a guess).
#   narrow   - at 700px: every .hide-sm segment is gone, the health DOT and
#              the workdir chip are still there.
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
  jsq '(()=>{const bar=document.querySelector(".statusbar");if(!bar)return 0;const zones=[...bar.querySelectorAll(":scope > .zone")];if(zones.length!==2)return 0;const left=bar.querySelector(".zone.left"),right=bar.querySelector(".zone.right");if(!left||!right)return 0;if(!left.querySelector(".ws-btn"))return 0;if(left.children.length!==1)return 0;if(!right.querySelector(".seg.seg-btn.health"))return 0;if(!right.querySelector(".seg[title^=\"server build\"]"))return 0;if(document.querySelector(".composer .ws-btn"))return 0;if(document.querySelector(".composer .ws-pop"))return 0;return 1})()'
}
v=$(probe_zones)
[ "$v" = "1" ] || die "the two-zone shape is off (left=workdir, right=metrics+build, composer chip gone): $(jsq '(()=>{const bar=document.querySelector(".statusbar");const z=[...bar.querySelectorAll(":scope > .zone")].map(x=>x.className);return "zones="+z.join("|")+" composerChip="+!!document.querySelector(".composer .ws-btn")})()')"
step "ok: two zones - workdir left, metrics + build right, no composer chip"

# --- 2. the clickable set is closed -----------------------------------------
probe_buttons() {
  jsq '(()=>{const bar=document.querySelector(".statusbar");const btns=[...bar.querySelectorAll("button")].filter(b=>!b.classList.contains("ws-btn"));const classes=btns.map(b=>b.className);for(const c of classes){if(!/(^|\s)seg-btn(\s|$)/.test(c))return 0}for(const c of classes){if(!/health|pending/.test(c))return 0}if(classes.length>2)return 0;if(!bar.querySelector(".seg.seg-btn.health"))return 0;return 1})()'
}
v=$(probe_buttons)
[ "$v" = "1" ] || die "the clickable set is not closed: $(jsq '(()=>{return [...document.querySelector(".statusbar").querySelectorAll("button")].map(b=>b.className).join("|")})()')"
step "ok: only health (and a live pending) are buttons - every metric stays a read"

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
probe_health() {
  jsq '(()=>{const p=document.querySelector(".health-pop");if(!p)return 0;const keys=[...p.querySelectorAll(".hp-key")].map(k=>k.textContent.trim());for(const k of ["server","provider","database","uptime","last error"]){if(!keys.includes(k))return 0}const val=k=>{const row=[...p.querySelectorAll(".hp-row")].find(r=>r.querySelector(".hp-key").textContent.trim()===k);return row?row.querySelector(".hp-val").textContent.trim():""};const up=val("uptime");if(!/^[0-9]+(s|m|h)/.test(up))return 0;for(const k of ["provider","database","last error"]){if(val(k)!=="not reported")return 0}const note=(p.querySelector(".hp-note")||{}).textContent||"";if(!note.includes("/health reports"))return 0;return 1})()'
}
v=$(probe_health)
[ "$v" = "1" ] || die "the health popover's facts are wrong: $(jsq '(()=>{return [...document.querySelectorAll(".health-pop .hp-row")].map(r=>r.querySelector(".hp-key").textContent+"="+r.querySelector(".hp-val").textContent.trim()).join(" | ")})()')"
step "ok: health popover - reachable server, live uptime, and the unsourced facts say not reported"
assert_eq "$(jsq 'document.querySelector(".statusbar .seg.seg-btn.health").getAttribute("aria-expanded")' | tr -d '"')" "true" "the health control reports its open state"
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!document.querySelector(".health-pop")' 10 || die "the health popover never closed"

# --- 5. narrow: health dot + workdir only ----------------------------------
ab set viewport 700 800 >/dev/null 2>&1
sleep 1
probe_narrow() {
  jsq '(()=>{const bar=document.querySelector(".statusbar");if(!bar)return 0;const hidden=e=>getComputedStyle(e).display==="none";const shown=[...bar.querySelectorAll(".hide-sm")];if(!shown.length)return 0;for(const e of shown){if(!hidden(e))return 0}const dot=bar.querySelector(".seg.seg-btn.health .dot");if(!dot||hidden(dot))return 0;const ws=bar.querySelector(".ws-btn");if(!ws||hidden(ws))return 0;return 1})()'
}
v=$(probe_narrow)
[ "$v" = "1" ] || die "narrow 700px did not collapse to health dot + workdir: $(jsq '(()=>{const bar=document.querySelector(".statusbar");return [...bar.querySelectorAll(".seg")].map(e=>e.className+"["+getComputedStyle(e).display+"]").join(" | ")})()')"
step "ok: 700px - health dot and workdir stay, every metric hides"
ab set viewport 1280 800 >/dev/null 2>&1 || true

assert_no_console_errors
step "112-statusbar-zones: green"
