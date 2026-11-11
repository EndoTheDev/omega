#!/bin/bash
# 111-socket-push - the push channel's webui half (card t_896cf5a8).
#
# Two customer-visible shapes, both only the browser can witness:
#
#   1. NOTHING POLLS. The shell used to fetch /health every 30s,
#      /sessions every 2s, /ui/canvas every 15s and
#      /notifications+/verdicts every 15s and 5s. Instrument fetch on
#      the live page and hold still for 17s: a single polling fetch is
#      a red. (Longer than every deleted cadence, so the 15s pair is
#      inside the window.) The websocket itself is not a fetch, so the
#      push traffic cannot mask a poller.
#
#   2. DROP -> RECONNECT -> REFETCH. The server buffers and replays
#      NOTHING: with the tab offline a session row appears on the
#      server (nothing can reach the page), and once the network comes
#      back the RECONNECT's catch-up refetches /sessions and the row
#      shows up. The fetch log is the witness that the catch-up did it -
#      no poller exists to do it (shape 1 proves that on the same page).
#
# No model needed: the "new row" is a REST session create, not a run.
. "${JROOT:?run must export JROOT}/lib.sh"
trap cleanup_journey_residue EXIT

open_app
step "ok: app open"

# --- 1: nothing polls beside the socket ----------------------------------
# install the recorder AFTER boot (the boot fetches are legitimate: the
# dot, the sidebar, the canvas snapshot all read once before the socket
# carries the truth). From here on, every fetch is a poller or a user
# action - and this recipe performs neither.
rec=$(jsq '(()=>{window.__fetchLog=[];const of=window.fetch.bind(window);window.fetch=(...a)=>{try{const u=a[0]&&a[0].url?a[0].url:String(a[0]);window.__fetchLog.push(u)}catch{};return of(...a)};return 1})()' 2>&1 | tail -1 | tr -d '"\r')
assert_eq "$rec" "1" "fetch recorder installed on the live page"

step "holding still 17s (every deleted cadence is inside this window)"
sleep 17
polled=$(jsq '(()=>{const re=/\/(sessions|health|ui\/canvas|notifications|verdicts)(\?|$)/;return (window.__fetchLog||[]).filter(u=>re.test(u)).length})()' 2>&1 | tail -1 | tr -d '"\r')
assert_eq "$polled" "0" "no polling fetch in 17s (health/sessions/canvas/notifications/verdicts)"

# the socket is actually alive: the health topic pushes every 5s, so the
# health popover's uptime row climbs without a single fetch. Uptime is the
# server's, so a still page that shows a moving number is a pushed fact.
# card t_be30543a: the bar's version segment no longer repeats the uptime -
# the popover row is the ONLY place it renders, so the probe opens the
# popover and reads it there. The click is local state (the popover renders
# the already-pushed health frame), not a fetch: the poller assertion above
# still covers the whole window.
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!!document.querySelector(".health-pop .hp-row")' 10 \
  || die "the health popover never opened (the pushed-frame probe has nothing to read)"
read_up() {
  jsq '(()=>{const p=document.querySelector(".health-pop");if(!p)return "";const row=[...p.querySelectorAll(".hp-row")].find(r=>(r.querySelector(".hp-key").textContent||"").trim()==="uptime");return row?(row.querySelector(".hp-val").textContent||"").trim():""})()' 2>&1 | tail -1 | tr -d '"'
}
up1=$(read_up)
[ -n "$up1" ] || die "the health popover renders no uptime row (the pushed-frame probe has nothing to read)"
# the row's voice drops the seconds above the hour mark ("2m 05s" below 1h,
# "1h 02m" above it), so a fixed 6s window can only see a change 1 minute in
# 10 on a lane that has been up past the hour - poll instead, bounded by the
# slowest honest change (the next minute boundary). Reading the DOM is not a
# fetch: the poller assertion above still covers the window.
up2="$up1"
_i=0
while [ "$up2" = "$up1" ] && [ "$_i" -lt 70 ]; do
  sleep 1
  up2=$(read_up)
  _i=$((_i + 1))
done
assert_ne "$up1" "$up2" "the health popover's uptime advanced from PUSHED frames only"
jsq 'document.querySelector(".statusbar .seg.seg-btn.health").click()' >/dev/null 2>&1
wait_js '!document.querySelector(".health-pop")' 10 \
  || die "the health popover never closed after the uptime probe"
step "ok: pushed health keeps the popover uptime live with zero fetches"

# --- 2: drop -> reconnect -> refetched pane -------------------------------
mark=$(jsq '(()=>{window.__fetchMark=(window.__fetchLog||[]).length;return window.__fetchMark})()' 2>&1 | tail -1 | tr -d '"\r')
case "$mark" in ''|*[!0-9]*) die "fetch log mark is garbage: $mark" ;; esac

ab set offline on >/dev/null 2>&1 || die "cannot put the tab offline"
online=$(jsq 'navigator.onLine' 2>&1 | tail -1 | tr -d '"\r')
assert_eq "$online" "false" "the tab is offline (the socket is cut)"

# a half-open socket never fires `close`: the client's quiet-wire check
# cuts it once 15s have passed with no frame (the health topic beats
# every 5s), and the backoff ladder starts. That budget is a deadline
# from the LAST FRAME, so the cut lands inside this hold: 18s offline
# plus the seed's 3s sleep puts ~21s of dark wire behind the cut, which
# is the margin the journey needs (card t_71f28526 pinned the deadline -
# a full-budget re-arm stretched the discovery window to 30s and the
# socket outlived the outage, reviving with the network instead of
# reconnecting, so the catch-up never ran).
step "holding offline 18s (the quiet-wire budget)"
sleep 18

# a NEW session row appears on the server while nothing can reach the page
SID=$(new_journey_session socket-push | tail -1)
TAG=$(journey_tag)
LABEL="(journey) socket-push $TAG"
label_journey_session "$SID" "$LABEL"
step "seeded $SID offline"

# nothing pushed, nothing replayed: the row cannot be on the page
sleep 3
present=$(jsq "(()=>[...document.querySelectorAll('.session-list li')].some(x=>(x.innerText||'').includes('socket-push $TAG'))?1:0)()" 2>&1 | tail -1 | tr -d '"\r')
assert_eq "$present" "0" "the new row is NOT on the page while the socket is down (no replay, no poller)"

# back online: the ladder reconnects and CATCHES UP through the GET
# routes - the pane refetches and the row appears, no reload
ab set offline off >/dev/null 2>&1 || die "cannot put the tab back online"
poll_js "(()=>[...document.querySelectorAll('.session-list li')].some(x=>(x.innerText||'').includes('socket-push $TAG')))()" 45 \
  || die "the row never appeared after reconnect - the catch-up refetch is dead"
step "ok: the row appeared on the RECONNECT's catch-up (no reload)"

# the catch-up's own receipt: the fetch log grew by the reconnect reads
# (/health is the first thing a reconnect does), and the sessions list
# rode along. A page that never reconnected could not have them.
caught=$(jsq "(()=>{const log=(window.__fetchLog||[]).slice(Number(window.__fetchMark)||0);return log.some(u=>/\\/health/.test(u))&&log.some(u=>/\\/sessions/.test(u))?1:0})()" 2>&1 | tail -1 | tr -d '"\r')
assert_eq "$caught" "1" "the reconnect ran GET /health then refetched /sessions"

assert_no_console_errors
step "green: no pollers, drop -> reconnect -> refetched pane"
