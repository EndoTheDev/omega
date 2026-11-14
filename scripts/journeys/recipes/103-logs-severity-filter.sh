#!/bin/bash
# 103-logs-severity-filter - the Logs tab's severity filter journey.
#
# RE-PINNED (card t_a9239e91). The original recipe (card t_221dc498) pinned
# three toggle chips (info/warning/error) that classified the RAW LOG TAIL on
# its in-band markers. That whole surface rode out with
# api/http_channel/ui/LogsList.vue + ui/severity.ts in t_01c53e78 (commit
# c006f9ac, "one logs pane - the raw tail retires, Logs is the stream"), so
# every `.dock-pane.edge-bottom .logs-line` / `.logs-sev-chip` / `.logs-meta`
# selector resolved nothing and this recipe could not pass. It was masked
# behind 78's red in the 2026-11-13 nightly.
#
# The FEATURE did not vanish, it narrowed: the surviving severity filter is
# the capsule's `errors` chip (harness/observability/ui/ObservabilityTab.vue,
# .obs-chip.obs-errors) filtering the STRUCTURED run-event stream on the
# server-derived level (harness/observability/ring.go LevelInfo/Warn/Error;
# rows.ts isErrorEvent/visibleEvents/errorCount). It is a binary filter over
# derived levels, not a three-way classifier over raw text, and it is the
# only severity control the product still ships. Nothing pinned it live -
# recipes 77 and 78 assert .obs-pane and .obs-tail-line only - so this recipe
# keeps its original JOB on the selectors that exist: the node suite pins the
# pure half, and a chip that flips nothing, never moves aria-pressed and never
# shrinks the page passes every pure test. Only a browser witness catches it.
#
# The retired shape is pinned as retired, twice: the old classes must resolve
# to ZERO elements, and the raw tail must stay UNFILTERED - the tail is the
# process's own words verbatim (rows.ts parseLogsPayload: no reformatting, no
# filtering), so an errors-only toggle must not remove an "ERROR: " marked
# line from it. A severity classifier growing back onto the tail reds here.
#
# No model needed. The ring is seeded by a REAL run: POST /chat is fire-and-
# forget (runs are detached from the request - a disconnect never cancels),
# and agent_start/turn_start are observed within milliseconds, before the
# provider is ever called. On the nightly's no-provider lane the run also
# fails fast, which lands error-level rows and gives the filter real rows to
# hide; on a lane with a live model the same seed lands info rows and the
# assertions take their zero-error branch. Both branches are strict.
#
# Runs against JOURNEY_BASE_URL. Needs no OMEGA_HOME: the tail's file path is
# read from the capsule's own GET /observability/logs answer, so the seed
# always writes the file the route actually tails.
. "${JROOT:?run must export JROOT}/lib.sh"

# One atomic read of the pane's whole verdict: the meta counts, the row census
# and the chip state come from the SAME rendered frame, so a ring that grows
# mid-assertion can never make two reads disagree. (No backslashes and no
# middot in this expression - eval passes it raw on every agent-browser
# version, the t_2454597e selector law.)
snapshot() {
  jsq '(() => {
    const p = document.querySelector(".dock-pane.edge-bottom");
    if (!p) return "nopane";
    const m = (p.querySelector(".obs-meta") || {}).textContent || "";
    const e = (m.match(/([0-9]+) errors/) || [])[1] || "-1";
    const ofm = m.match(/([0-9]+) of ([0-9]+) events/);
    const flat = (m.match(/([0-9]+) events/) || [])[1] || "-1";
    const sh = ofm ? ofm[1] : flat;
    const tot = ofm ? ofm[2] : flat;
    const f = ofm ? 1 : 0;
    const c = Array.from(p.querySelectorAll(".obs-chip")).find((x) => (x.getAttribute("aria-label") || "") === "toggle errors only");
    return [tot, sh, e, f, p.querySelectorAll(".obs-row").length, p.querySelectorAll(".obs-row.error").length, c ? c.getAttribute("aria-pressed") : "none"].join("|");
  })()' | tr -d '"'
}
snap() { IFS='|' read -r TOT SHOWN ERRS FILT ROWS ERROWS PRESS <<< "$(snapshot)"; }

# --- 0: seed the ring with a real run -------------------------------------
# Fire-and-forget: the SSE session frame is the first frame, so a 3s capture
# gets the id for cleanup while the detached run keeps feeding the ring.
seed_out=$(curl -s -N -X POST "${JOURNEY_BASE_URL}/chat" \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"journey 103 ring seed"}]}' \
  --max-time 3 2>/dev/null || true)
SEED_SID=$(printf '%s' "$seed_out" | sed -n 's/^data: {"id":"\([A-Za-z0-9]*\)"}.*/\1/p' | head -1)
step "seeded a detached chat run (session ${SEED_SID:-unknown}) - the ring records it as it executes"

open_app
ab set viewport 1280 800 >/dev/null 2>&1

# clean dock baseline: the fresh default tree seats the bottom panel
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
sleep 1

# the bottom strip is the Logs tab's home (t_01c53e78: the observability tab
# renamed "Logs", the raw-tail http_channel pane retired)
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .dock-tab")).find((t) => (t.querySelector(".dock-tab-title")?.textContent || "").trim() === "Logs")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane").length === 1' 15 \
  || die "the Logs tab mounts no view (the observability capsule's obs-pane is missing)"

# --- 1: the band's chips, and the retired shape staying retired -----------
# Exactly three: session scope, errors-only, raw tail. A fourth chip is the
# RETURN of the retired three-way severity filter, so the count is asserted,
# not just the presence of the two under test.
chipcount=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-chip").length')
assert_eq "$chipcount" "3" "the Logs band carries exactly three chips: session scope, errors-only, raw tail"
scopecount=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-scope").length')
assert_eq "$scopecount" "1" "the session-scope chip still leads the band"
errlbl=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.getAttribute("aria-label") || ""' | tr -d '"')
assert_eq "$errlbl" "toggle errors only" "the severity filter survives as the errors-only chip (t_01c53e78)"
taillbl=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-tail")?.getAttribute("aria-label") || ""' | tr -d '"')
assert_eq "$taillbl" "toggle raw log tail" "the raw tail is the capsule's SECONDARY view behind one chip, not a peer tab"
# the retired selectors must resolve to nothing: a hit here means the raw-tail
# pane came back, and every assertion below would be pinning the wrong surface
retired=$(jsq 'document.querySelectorAll(".logs-line, .logs-sev-chip, .logs-meta, .logs-view").length')
assert_eq "$retired" "0" "the retired raw-tail Logs pane selectors resolve to zero elements (t_01c53e78, c006f9ac)"

# errors-only starts OFF (aria-pressed=false, the `.off` dim): the tab opens on
# the full stream, the opposite default from the retired severity chips.
snap
assert_eq "$PRESS" "false" "the errors-only chip starts aria-pressed=false (the full stream)"
assert_eq "$FILT" "0" "the meta line carries no 'of' numerator before any filtering"

# --- 2: the seed reached the pane through the bounded refresh -------------
# The logs push topic retired with the pane (t_1423218e): the tab re-reads on
# a bounded REFRESH_MS=5000 while mounted. The seed must paint with NO reload.
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row").length >= 1' 15 \
  || die "the seeded run never reached the ring view (no .obs-row painted; GET ${JOURNEY_BASE_URL}/observability/events is empty?)"
snap
[ "$TOT" -ge 1 ] 2>/dev/null || die "the pane reports no events total (meta gave '$TOT') - the seed did not reach the ring"
[ "$ERRS" -ge 0 ] 2>/dev/null || die "the meta line lost its error count (got '$ERRS') - want '<n> events / <e> errors / <b>/<cap> buffered'"
# anti-vacuity: agent_start is always an info record, so a healthy ring always
# holds at least one non-error row for the filter to hide.
[ "$ERRS" -lt "$TOT" ] 2>/dev/null || die "every ring record reads as an error ($ERRS of $TOT) - the derived level is broken and the filter would be untestable"
TOT_OFF=$TOT
want=$(( TOT_OFF < 300 ? TOT_OFF : 300 ))
assert_eq "$ROWS" "$want" "the full stream paints every record (up to the 300-row render cap): $ROWS of $TOT_OFF"
assert_eq "$ERROWS" "$ERRS" "the error-classed rows in the full stream match the meta's error count"
step "ok: ring seeded with $TOT_OFF events, $ERRS of them error-level"

# --- 3: errors-only ON - the strict filter --------------------------------
# A dead toggle passes every pure test: it must move aria-pressed, drop the
# `.off` dim, and shrink the page to exactly the error rows.
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.click()' >/dev/null 2>&1
poll_js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.getAttribute("aria-pressed") === "true"' 5 \
  || die "the errors-only chip never flips aria-pressed=true on click (a dead toggle)"
snap
assert_eq "$PRESS" "true" "the errors-only chip reads aria-pressed=true when on"
assert_eq "$FILT" "1" "the meta line carries the filtered numerator while the filter is on"
assert_eq "$SHOWN" "$ERRS" "the filtered numerator equals the error count in the same frame (got shown=$SHOWN, errors=$ERRS)"
assert_eq "$ROWS" "$ERRS" "errors-only paints exactly the error rows (got $ROWS, want $ERRS)"
assert_eq "$ERROWS" "$ROWS" "every row painted under errors-only carries the error class (got $ERROWS of $ROWS)"
[ "$TOT" -ge "$TOT_OFF" ] 2>/dev/null || die "the ring total went backwards ($TOT < $TOT_OFF) - the tab is reading a different source"
allerr=$(jsq '(() => {
  const rows = Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-row"));
  return rows.every((r) => (r.getAttribute("class") || "").split(/[ \t]+/).includes("error")) ? 1 : 0;
})()')
assert_eq "$allerr" "1" "errors-only leaves no non-error row in the pane (level class check, not a count)"
if [ "$ERRS" -gt 0 ] 2>/dev/null; then
  step "ok: errors-only kept $ERRS error rows out of $TOT (the seeded run failed, so the filter had real rows to hide)"
else
  step "ok: errors-only emptied the pane over an error-free ring (0 errors on this lane - the live-model branch)"
fi

# --- 4: errors-only OFF - the full stream returns, no refetch -------------
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.click()' >/dev/null 2>&1
poll_js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.getAttribute("aria-pressed") === "false"' 5 \
  || die "the errors-only chip never flips back to aria-pressed=false"
snap
assert_eq "$FILT" "0" "the meta line drops the 'of' numerator once the filter is off"
assert_eq "$SHOWN" "$TOT" "the unfiltered meta reads its own total as the shown count"
assert_eq "$ROWS" "$(( TOT < 300 ? TOT : 300 ))" "the full stream returns (up to the render cap) with no refetch"
step "ok: errors-only round-trips (full stream back, $ROWS rows)"

# --- 5: the raw tail is VERBATIM and never severity-filtered --------------
# The retired feature's boundary, pinned: the tail renders the process's own
# lines, so an "ERROR: " marked line must be visible with errors-only ON AND
# OFF. If a severity classifier ever grows back onto the tail, this reds.
TAIL_PATH=$(curl -s "${JOURNEY_BASE_URL}/observability/logs?lines=1" | sed -n 's/.*"path": *"\([^"]*\)".*/\1/p')
[ -n "$TAIL_PATH" ] || die "the observability capsule reports no log file (GET /observability/logs carries no path; logging disabled on this lane?)"
[ -f "$TAIL_PATH" ] || die "the capsule's log path does not exist on disk: $TAIL_PATH"
# the tag is unique per run: the tail is a live file that keeps every line a
# previous suite run seeded, so an unqualified substring would count the
# residue and a strict count would rot on the second run.
NOW=$(date '+%Y/%m/%d %H:%M:%S')
TAG="journey103-$(date +%s)-$$"
MARKED="ERROR: $TAG marked error line"
PLAIN="$TAG INFO line"
printf '%s %s\n' "$NOW" "$PLAIN" >> "$TAIL_PATH"
printf '%s %s\n' "$NOW" "$MARKED" >> "$TAIL_PATH"
step "seeded one unmarked and one ERROR-marked line ($TAG) into $TAIL_PATH"

js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-tail")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line").length >= 1' 15 \
  || die "the tail chip mounts no tail lines (/observability/logs painted nothing)"
# the bounded 5s refresh must carry the seed - NO reload, NO re-click
poll_js "(() => {
  const lines = Array.from(document.querySelectorAll('.dock-pane.edge-bottom .obs-tail-line')).map((l) => l.textContent);
  return lines.some((t) => t.includes('${PLAIN}')) && lines.some((t) => t.includes('${MARKED}'));
})()" 20 || die "the seeded lines never reached the tail (the tab's bounded refresh is dead - no reload happened, so only the 5s re-read could carry them)"
# verbatim: the line the pane paints is byte-for-byte the line we appended,
# LstdFlags timestamp prefix included (no reformatting, no classification)
verbatim=$(jsq "(() => {
  const l = Array.from(document.querySelectorAll('.dock-pane.edge-bottom .obs-tail-line')).find((x) => x.textContent.includes('${MARKED}'));
  if (!l) return 0;
  return l.textContent.trim() === '${NOW} ${MARKED}' ? 1 : 0;
})()")
assert_eq "$verbatim" "1" "the tail renders the seeded line verbatim, timestamp and ERROR: marker intact"
tailmeta=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-meta")?.textContent || ""' | tr -d '"')
case "$tailmeta" in
  *"refreshed every 5s"*) step "ok: the tail meta names its bounded refresh ($tailmeta)" ;;
  *) die "the tail meta lost its refresh marker: $tailmeta" ;;
esac
# snapshot the tail's lines, then prove the toggle REMOVES nothing. Growth is
# allowed (the file keeps taking lane traffic and the 5s re-read picks it up);
# a removal is the retired classifier coming back.
js 'window.__tailBefore = Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line")).map((l) => l.textContent)' >/dev/null 2>&1

# errors-only ON with the tail up: the raw tail must NOT change
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.click()' >/dev/null 2>&1
poll_js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.getAttribute("aria-pressed") === "true"' 5 \
  || die "the errors-only chip never flips on while the tail is up"
sleep 0.6
tailkept=$(jsq '(() => {
  const now = Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line")).map((l) => l.textContent);
  const before = window.__tailBefore || [];
  return before.length > 0 && before.every((t) => now.includes(t)) ? 1 : 0;
})()')
assert_eq "$tailkept" "1" "errors-only removes no raw tail line - the tail is never severity-filtered (the retired feature stays retired)"
tailmarked=$(jsq "Array.from(document.querySelectorAll('.dock-pane.edge-bottom .obs-tail-line')).filter((l) => l.textContent.includes('${MARKED}')).length")
assert_eq "$tailmarked" "1" "the ERROR-marked tail line survives the errors-only toggle (a classifier on the tail would have judged it, not the chip)"
tailinfo=$(jsq "Array.from(document.querySelectorAll('.dock-pane.edge-bottom .obs-tail-line')).filter((l) => l.textContent.includes('${PLAIN}')).length")
assert_eq "$tailinfo" "1" "the unmarked tail line survives too - nothing on the tail is classified"
step "ok: raw tail verbatim and unfiltered ($(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line").length') lines under errors-only)"

# --- clean state: chips off, stream view back ----------------------------
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.click()' >/dev/null 2>&1
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-tail")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row").length >= 1' 10 \
  || die "closing the tail did not return the stream view"
snap
assert_eq "$PRESS" "false" "final: every chip is back in its default state"
assert_eq "$FILT" "0" "final: the stream view is unfiltered"
step "ok: clean state (stream view, full page)"

# the seed session is this recipe's garbage - delete it so the store does not
# accumulate one journey row per suite run
if [ -n "$SEED_SID" ]; then
  curl -s -X DELETE "${JOURNEY_BASE_URL}/sessions/${SEED_SID}" >/dev/null 2>&1 || true
fi

assert_no_console_errors
