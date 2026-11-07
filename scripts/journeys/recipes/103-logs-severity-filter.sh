#!/bin/bash
# 103-logs-severity-filter - the logs tab severity filter journey (card
# t_221dc498). The bottom panel's Logs tab gains three toggle chips
# (info/warning/error) classifying the raw tail on the in-band markers
# (harness/logging: Errorf -> "ERROR: ", Warnf -> "WARN: " after the
# log.LstdFlags timestamp). The node suite pins the pure classifier and
# the SFC wiring; this journey is the only witness the CHIPS work live -
# a dead toggle (click flips nothing, aria-pressed never moves) passes
# every pure test. The seeds are honest: the journey appends real
# marker-shaped lines to $OMEGA_HOME/omega.log (the exact file the
# /logs tail reads), never mocks the fetch.
#
# No model needed: pure shell UI over the tail. Runs against
# JOURNEY_BASE_URL; needs OMEGA_HOME (the lane home whose omega.log the
# serve tails).
. "${JROOT:?run must export JROOT}/lib.sh"

open_app
ab set viewport 1280 800 >/dev/null 2>&1

# clean dock baseline: the fresh default tree seats the bottom panel
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1

# --- 1: the chips render, all on by default -------------------------------
# (ab eval JSON-quotes STRING results - the t_3aed367e quote law - so
# every string probe strips the quotes before assert_eq)
chips=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).map(c=>c.getAttribute("aria-label")).join(",")' | tr -d '"')
assert_eq "$chips" "toggle info lines,toggle warning lines,toggle error lines" \
  "the Logs band carries the three severity chips in order (info, warning, error)"
pressed=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).every(c=>c.getAttribute("aria-pressed")==="true")?1:0')
assert_eq "$pressed" "1" "every chip starts aria-pressed=true (the full stream)"

# --- 2: seed warn/error lines into the tailed log --------------------------
# The lane home's omega.log is the file GET /logs tails; appending two
# marker-shaped lines makes the filter observable without mocking
# anything. Timestamps match the log.LstdFlags shape so the classifier
# reads them like any logger-written line.
LOG="${OMEGA_HOME:?run must export OMEGA_HOME (the lane home)}/omega.log"
[ -f "$LOG" ] || die "lane log missing: $LOG (logging disabled on this lane?)"
NOW=$(date '+%Y/%m/%d %H:%M:%S')
printf '%s WARN: journey seeded warning line\n' "$NOW" >> "$LOG"
printf '%s ERROR: journey seeded error line\n' "$NOW" >> "$LOG"
step "seeded one WARN and one ERROR line into $LOG"

# the 2.5s poll picks the tail up; both seeded lines must classify.
# (poll_js passes only on the literal "true" - boolean expression, not
# a 1/0 ternary.)
poll_js '(()=>{const lines=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).map(l=>l.textContent);return lines.some(t=>t.includes("journey seeded warning line"))&&lines.some(t=>t.includes("journey seeded error line"))})()' 12 \
  || die "the seeded warn/error lines never showed in the tail (poll or classifier broken)"
step "ok: seeded warn + error lines render in the unfiltered tail"

TOTAL=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length')

# --- 3: toggle info off - only warn/error lines remain ---------------------
# unmarked(line) = the line's text, with the LstdFlags timestamp
# stripped, carries NO "ERROR: "/"WARN: " marker - the shape the info
# filter must hide. (Exact counts are brittle: the tail keeps growing
# with lane activity, so the assertions are shape-based.)
UNMARKED_JS='(()=>{const ts=/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2} /;const out=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).filter(l=>{let t=(l.textContent||"").replace(ts,"");return !(t.startsWith("ERROR: ")||t.startsWith("WARN: ")||t.startsWith("WARNING: "))}).length;return out})()'
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle info lines").click()' >/dev/null 2>&1
sleep 0.4
ipressed=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle info lines").getAttribute("aria-pressed")' | tr -d '"')
assert_eq "$ipressed" "false" "the info chip flips aria-pressed=false on click"
unmarked=$(jsq "$UNMARKED_JS")
assert_eq "$unmarked" "0" "info off: zero unmarked (info-class) lines are visible (got $unmarked)"
warnvis=$(jsq '(()=>{const lines=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).map(l=>l.textContent);return lines.some(t=>t.includes("journey seeded warning line"))?1:0})()')
assert_eq "$warnvis" "1" "info off: the seeded WARNING line stays visible"
errvis=$(jsq '(()=>{const lines=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).map(l=>l.textContent);return lines.some(t=>t.includes("journey seeded error line"))?1:0})()')
assert_eq "$errvis" "1" "info off: the seeded ERROR line stays visible"

# the meta line carries filtered of total
META_FILTERED=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length')
meta=$(jsq "document.querySelector(\".dock-pane.edge-bottom .logs-meta\")?.textContent ?? \"\"" | tr -d '"')
case "$meta" in
  *"$META_FILTERED of "*) step "ok: meta reads filtered of total ($meta)" ;;
  *) die "meta line lost the filtered/total counts: $meta" ;;
esac

# --- 4: toggle warning off too - only error lines remain ------------------
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle warning lines").click()' >/dev/null 2>&1
sleep 0.4
warnvis=$(jsq '(()=>{const lines=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).map(l=>l.textContent);return lines.some(t=>t.includes("journey seeded warning line"))?1:0})()')
assert_eq "$warnvis" "0" "warning off: the seeded WARNING line is gone"
errvis=$(jsq '(()=>{const lines=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).map(l=>l.textContent);return lines.some(t=>t.includes("journey seeded error line"))?1:0})()')
assert_eq "$errvis" "1" "warning off: the seeded ERROR line stays visible"
onlyerr=$(jsq '(()=>{const ts=/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2} /;return Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).every(l=>{let t=(l.textContent||"").replace(ts,"");return t.startsWith("ERROR: ")})?1:0})()')
assert_eq "$onlyerr" "1" "warning off too: every visible line is ERROR-class"

# --- 5: all off - the explicit empty-filter state -------------------------
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle error lines").click()' >/dev/null 2>&1
sleep 0.4
empty=$(jsq '(()=>{const el=document.querySelector(".dock-pane.edge-bottom");return el&&(el.textContent||"").includes("no lines match the severity filter")?1:0})()')
assert_eq "$empty" "1" "all chips off: the explicit no-match state shows (not a blank body)"
nolines=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length')
assert_eq "$nolines" "0" "all chips off: zero log lines render"

# --- 6: toggle info back on - the info lines return with no refetch --------
# (the poll never stopped: the filter is view-only over the kept tail -
# a refetch-based filter would blank the tab mid-toggle. Info back on
# returns the INFO lines only - warning/error chips are still off.)
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle info lines").click()' >/dev/null 2>&1
sleep 0.4
back=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length')
[ "$back" -ge 1 ] 2>/dev/null || die "info back on: no info lines returned (a chip toggle must not need a refetch)"
bannervis=$(jsq '(()=>{const lines=Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-line")).map(l=>l.textContent);return lines.some(t=>t.includes("serving 1 channel"))?1:0})()')
assert_eq "$bannervis" "1" "info back on: the info-class boot banner is visible again"
step "ok: info back on restored the info lines with no refetch"

# --- clean state: every chip back on, the full stream ----------------------
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle warning lines").click()' >/dev/null 2>&1
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).find(c=>c.getAttribute("aria-label")==="toggle error lines").click()' >/dev/null 2>&1
sleep 0.4
pressed=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .logs-sev-chip")).every(c=>c.getAttribute("aria-pressed")==="true")?1:0')
assert_eq "$pressed" "1" "final: every chip is back on (clean state)"
full=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .logs-line").length')
[ "$full" -ge "$TOTAL" ] 2>/dev/null || die "final: the full stream did not return ($full < $TOTAL)"
step "ok: full stream back ($full lines >= $TOTAL at seed time; the tail keeps growing)"

assert_no_console_errors
