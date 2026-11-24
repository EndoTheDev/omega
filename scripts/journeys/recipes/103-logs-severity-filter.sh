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
# WIDENED (card t_57f123ea): the band gained the clear action and the pane grew
# the SOURCE family (.obs-sources / .obs-src, one toggle per derived source).
# Sections 4 and 6 pin the two new halves live: a source toggle really removes
# its rows from the DOM and AND-composes with the severity chip, and clear is a
# display watermark (the ring total holds, the log file still tails).
#
# COMPLETED (card t_971020a3): the fifth source is real. The HTTP channel stamps
# its runs with origin "gateway" (agent/api), the session observer hands the
# origin to the recorder, and every record of THIS recipe's /chat seed carries
# it - so section 4b's toggle now proves a producer end to end instead of
# leaning on the agent chip, and the strip carries five accents.
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

# wait_run_over <session>: the session has no live run. A run's provider retry
# ladder keeps the flag true across every attempt, so this is the ONE reading that
# says "the ring can no longer grow" (the transcript envelope always carries it -
# recipe 76's wait_idle shape). A pane settle that is shorter than the retry gap
# cannot tell a paused ladder from a finished run.
wait_run_over() {
  local i=0
  while [ "$i" -lt 240 ]; do
    case "$(curl -s "${JOURNEY_BASE_URL}/sessions/$1/transcript")" in
      *'"running":false'*) return 0 ;;
    esac
    sleep 0.5
    i=$((i + 1))
  done
  die "session $1 never left running (120s) - the ring cannot go quiet, so the clear cannot be graded"
}

# --- 0: seed the ring with a real run -------------------------------------
# Fire-and-forget into a session THIS recipe names. The run is detached (a
# disconnect never cancels) and its provider retry ladder keeps it live for
# seconds - so section 6 must READ "is it over" off the session, and a session
# id we minted is the handle that question needs (a lane whose provider refuses
# retries the failed stream: the records do not all land at once).
SEED_SID=$(new_journey_session logs103 | tail -1)
[ -n "$SEED_SID" ] || die "could not mint the seed session (POST /sessions answered no id)"
curl -s -N -X POST "${JOURNEY_BASE_URL}/chat" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SEED_SID\",\"messages\":[{\"role\":\"user\",\"content\":\"journey 103 ring seed\"}]}" \
  --max-time 3 >/dev/null 2>&1 || true
step "seeded a detached chat run in session $SEED_SID - the ring records it as it executes"

open_app
ab set viewport 1280 800 >/dev/null 2>&1

# clean dock baseline: the fresh default tree seats the bottom panel
js 'localStorage.clear()' >/dev/null 2>&1
ab reload >/dev/null 2>&1
ab wait --load networkidle >/dev/null 2>&1 || true
wait_booted

# the bottom strip is the Logs tab's home (t_01c53e78: the observability tab
# renamed "Logs", the raw-tail http_channel pane retired)
js 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .dock-tab")).find((t) => (t.querySelector(".dock-tab-title")?.textContent || "").trim() === "Logs")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane").length === 1' 15 \
  || die "the Logs tab mounts no view (the observability capsule's obs-pane is missing)"

# --- 1: the band's chips, and the retired shape staying retired -----------
# Exactly four: session scope, errors-only, raw tail, clear. A fifth band chip is
# the RETURN of the retired three-way severity filter (or a new control nobody
# pinned), so the count is asserted, not just the presence of the ones under test.
chipcount=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .tab-band .obs-chip").length')
assert_eq "$chipcount" "4" "the Logs band carries exactly four chips: session scope, errors-only, raw tail, clear"
scopecount=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-scope").length')
assert_eq "$scopecount" "1" "the session-scope chip still leads the band"
errlbl=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.getAttribute("aria-label") || ""' | tr -d '"')
assert_eq "$errlbl" "toggle errors only" "the severity filter survives as the errors-only chip (t_01c53e78)"
taillbl=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-tail")?.getAttribute("aria-label") || ""' | tr -d '"')
assert_eq "$taillbl" "toggle raw log tail" "the raw tail is the capsule's SECONDARY view behind one chip, not a peer tab"
clearlbl=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-clear")?.getAttribute("aria-label") || ""' | tr -d '"')
assert_eq "$clearlbl" "clear the visible stream" "the clear action names what it empties (the view, never the log file)"
clearpress=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-clear")?.getAttribute("aria-pressed") || "none"' | tr -d '"')
assert_eq "$clearpress" "none" "clear is an ACTION - it carries no pressed state"
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

# --- 4b: the SOURCE family - one toggle per source, multi-select, AND ------
# The source dimension is DERIVED (rows.ts eventSource): the ring has one producer
# family, so a record's source comes from its own provenance - the cron run's session
# label, the run origin a serving-layer run carries (every record of THIS recipe's
# /chat seed is stamped `gateway`), the kanban.* tools, the provider stream, the loop.
# Only a browser witness proves the chips move the render; the node suite pins the
# derivation alone.
srccount=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-sources .obs-src").length')
assert_eq "$srccount" "5" "one toggle per source the stream carries (llm, agent, cron, kanban, gateway)"
srclabels=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-src")).map((c) => c.getAttribute("aria-label")).join(",")' | tr -d '"')
assert_eq "$srclabels" "toggle llm records,toggle agent records,toggle cron records,toggle kanban records,toggle gateway records" \
  "the chips name the closed source vocabulary"
srcpress=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-src")).every((c) => c.getAttribute("aria-pressed") === "true") ? 1 : 0')
assert_eq "$srcpress" "1" "every source starts ON - the tab opens on the unfiltered stream"
srccolors=$(jsq '(() => {
  const chips = Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-pane .obs-src"));
  const vals = chips.map((c) => getComputedStyle(c).getPropertyValue("--src").trim());
  return new Set(vals).size;
})()')
assert_eq "$srccolors" "5" "each source chip resolves its own accent from the existing token ladder (the gateway's is a ladder derivation, not a fifth hue)"
sourced=$(jsq 'Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-row")).every((r) => (r.getAttribute("data-source") || "") !== "") ? 1 : 0')
assert_eq "$sourced" "1" "every row carries its source"

snap
TOT_PRE_SRC=$TOT
ROWS_PRE_SRC=$ROWS
gaterows=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row[data-source=gateway]").length')
[ "$gaterows" -ge 1 ] 2>/dev/null || die "the seeded /chat run painted no gateway rows ($gaterows) - the serving layer's origin never reached the ring"

# gateway OFF: the filtered-out rows must be GONE, not dimmed. This is the live proof
# that the fifth source is real: one producer (the HTTP channel's run origin) paints
# rows end to end, and its chip removes exactly those.
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-src.src-gateway")?.click()' >/dev/null 2>&1
poll_js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-src.src-gateway")?.getAttribute("aria-pressed") === "false"' 5 \
  || die "the gateway source chip never flips aria-pressed=false on click (a dead toggle)"
hidden=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row[data-source=gateway]").length')
assert_eq "$hidden" "0" "with the gateway source OFF no gateway row renders - the filtered rows leave the DOM"
snap
if [ "$TOT_PRE_SRC" -lt 300 ] 2>/dev/null; then
  want=$(( TOT_PRE_SRC - gaterows ))
  assert_eq "$ROWS" "$want" "the render drops exactly the gateway rows ($ROWS of $TOT_PRE_SRC, $gaterows were gateway)"
else
  assert_eq "$ROWS" "300" "the render stays at its 300-row cap over the filtered page"
fi
assert_eq "$TOT" "$TOT_PRE_SRC" "the ring total is unchanged - a source toggle filters the page, it does not fetch"

# AND, not OR: severity still agrees while the source is off
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.click()' >/dev/null 2>&1
poll_js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.getAttribute("aria-pressed") === "true"' 5 \
  || die "the errors chip never flips on with a source off"
androws=$(jsq '(() => {
  const rows = Array.from(document.querySelectorAll(".dock-pane.edge-bottom .obs-row"));
  return rows.every((r) => (r.getAttribute("class") || "").split(/[ \t]+/).includes("error")) && rows.every((r) => r.getAttribute("data-source") !== "gateway") ? 1 : 0;
})()')
assert_eq "$androws" "1" "errors-only AND gateway-off leaves only error rows that are not gateway records"
step "ok: the two filter families compose with AND (gateway off, errors-only on)"

# back to the unfiltered page, both families off
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-errors")?.click()' >/dev/null 2>&1
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-src.src-gateway")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row").length === '"$ROWS_PRE_SRC" 5 \
  || die "the full (up to the cap) page never came back after the chips were toggled home"
snap
assert_eq "$FILT" "0" "the meta line drops the numerator once neither family filters"
step "ok: both families restore the unfiltered stream ($ROWS rows)"

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
# the filtered re-render is the observable: wait for the tail's line count to
# hold still, then compare the lines (card t_fdd12843)
wait_settled 'document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line").length' 10
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

# --- 6: clear empties the VIEW, never the durable stream -------------------
# The clear button is a display watermark: the rows already read go out of view, the
# ring keeps them (the meta total must NOT drop) and the log file is never touched.
#
# The ring must be QUIET first, and quietness is read off the RUN, not guessed from a
# settle window: a record that lands after the clear is NEWER than the watermark and
# paints by design ("new records appear as they arrive"), so the flip's "stays cleared"
# claim only holds once the seed run has left `running`. A settle on the ROW COUNT is
# not enough - a lane whose provider refuses retries the stream, and the gap between
# two attempts held the count still for ~4s (measured on a no-provider lane: 16 records
# over ~6-9s, the final pair landing after the gap) - a window shorter than that gap
# reads the ladder's pause as "finished".
wait_run_over "$SEED_SID"
wait_settled 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row").length' 15
snap
TOT_PRE_CLEAR=$TOT
[ "$TOT_PRE_CLEAR" -ge 1 ] 2>/dev/null || die "the pane reads no events before the clear - nothing to prove (got '$TOT_PRE_CLEAR')"
# the flip grade below is by IDENTITY, not count: tag the pre-clear rows now -
# a tagged row repainting after the flip is a LOST watermark; an untagged row is
# a post-clear arrival (the ring was not quiet - legal, it paints by design).
PRE_SEP=$(printf '\001')
PRE_IDS=$(jsq '[...document.querySelectorAll(".dock-pane.edge-bottom .obs-row")].map(r => r.textContent).join(String.fromCharCode(1))' | tr -d '"')
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-clear")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row").length === 0' 5 \
  || die "the clear button left rows painted (the visible stream is not emptied)"
snap
assert_eq "$ROWS" "0" "clear empties the visible stream"
assert_eq "$TOT" "$TOT_PRE_CLEAR" "the ring is untouched - the cleared records are still read (a watermark, not a delete)"
clearmac=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-meta")?.textContent || ""' | tr -d '"')
case "$clearmac" in
  *"cleared through"*) step "ok: the meta line names the clear watermark ($clearmac)" ;;
  *) die "the meta line lost the clear marker: $clearmac" ;;
esac
CLEARED_WM=$(printf '%s' "$clearmac" | grep -oE 'cleared through [0-9]+' | grep -oE '[0-9]+$')
[ -n "$CLEARED_WM" ] || die "the clear meta names no watermark number: $clearmac"
# the durable half: the tail still reads the file, so nothing was truncated
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-tail")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line").length >= 1' 15 \
  || die "the raw tail lost its lines after a clear - clear touched the log file"
step "ok: clear is display-level (the log file still tails)"
# and a view flip does not resurrect the cleared rows either (the watermark is state,
# not a one-shot DOM sweep)
js 'document.querySelector(".dock-pane.edge-bottom .obs-pane .obs-chip.obs-tail")?.click()' >/dev/null 2>&1
poll_js 'document.querySelectorAll(".dock-pane.edge-bottom .obs-tail-line").length === 0' 10 \
  || die "the stream view did not return after the tail flip"
cleared_after=$(jsq 'document.querySelectorAll(".dock-pane.edge-bottom .obs-row").length')
if [ "${cleared_after//\"/}" != "0" ]; then
  # grade the repaint, per the two candidates this die names: rows are legal
  # ONLY as post-clear arrivals - a PRE-CLEAR identity repainting is a lost
  # watermark, and the meta must still name the SAME cleared-through number.
  flipmac=$(jsq 'document.querySelector(".dock-pane.edge-bottom .obs-meta")?.textContent || ""' | tr -d '\"')
  FLIP_WM=$(printf '%s' "$flipmac" | grep -oE 'cleared through [0-9]+' | grep -oE '[0-9]+$')
  [ -n "$FLIP_WM" ] || die "the flip meta lost the clear watermark - pre: '$clearmac' flip: '$flipmac'"
  assert_eq "$FLIP_WM" "$CLEARED_WM" "the cleared-through watermark survived the view flip"
  NOW_IDS=$(jsq '[...document.querySelectorAll(".dock-pane.edge-bottom .obs-row")].map(r => r.textContent).join(String.fromCharCode(1))' | tr -d '\"')
  repaints=0
  while IFS= read -r row; do
    [ -n "$row" ] || continue
    case "${PRE_SEP}${PRE_IDS}${PRE_SEP}" in
      *"${PRE_SEP}${row}${PRE_SEP}"*) repaints=$((repaints + 1)) ;;
    esac
  done < <(printf '%s' "$NOW_IDS" | tr '\001' '\n')
  [ "$repaints" -eq 0 ] \
    || die "the cleared stream repainted across a view flip (${repaints} pre-clear rows back, was $TOT_PRE_CLEAR before the clear) - meta: $flipmac"
  step "ok: only post-clear arrivals repainted (${cleared_after//\"/} new rows, $TOT_PRE_CLEAR pre-clear, watermark ${CLEARED_WM} intact)"
fi
step "ok: the cleared stream stays cleared across a view flip (the watermark survives)"

# the seed session is this recipe's garbage - delete it so the store does not
# accumulate one journey row per suite run
if [ -n "$SEED_SID" ]; then
  curl -s -X DELETE "${JOURNEY_BASE_URL}/sessions/${SEED_SID}" >/dev/null 2>&1 || true
fi

assert_no_console_errors
