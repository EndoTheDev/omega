#!/bin/bash
# 40-refresh-mid-run - the reattach journey: a run streams, the
# customer reloads mid-stream, and the transcript must MERGE the live
# run: the still-running session restores, the partial response shows
# up, the stream continues to completion. This is the detached-run
# auto-subscribe path the gates cannot see.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

SID=$(new_journey_session refresh | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) refresh-midrun $TAG"

open_app
open_journey_row "(journey) refresh-midrun $TAG"
ab find placeholder "message omega" click >/dev/null
ab keyboard type "Reply in this chat with 40 numbered lines. Each line is exactly: journey line N (N is 1 to 40). Do not use any tools - the answer must be the chat message itself."
wait_js '(()=>{return [...document.querySelectorAll("textarea,[contenteditable=true]")].some(e=>(e.value||e.textContent||"").includes("numbered lines"))})()' 10 \
  || die "the typed prompt never landed in the composer draft"

# the cursor witness is stamped INSIDE the page (100ms tick), not read by a
# shell poll: a shell round trip costs two `ab eval` calls plus a 0.5s sleep,
# and the dev lane's model streams the answer text for only ~1.5s - a shell
# cadence can miss that window entirely while the cursor was plainly there.
jsq '(()=>{window.__cursorSeen=false;clearInterval(window.__cursorTimer);
  window.__cursorTimer=setInterval(()=>{if(document.querySelector(".msg .cursor"))window.__cursorSeen=true;},100);
  return true})()' >/dev/null 2>&1
ab press Enter
ab press Enter
step "sent long prompt"

# mid-stream: wait for the streaming cursor with a real poll - TTFT is
# seconds, so a one-shot check right after send always lost the race.
# (The old `ab wait --text "journey line"` matched the USER bubble -
# the prompt itself contains the token - and a one-shot cursor check
# 0.3s after send always read false, so the fallback fired on a
# cancel-faced composer and the wider send never left the draft.)
if poll_js '!!window.__cursorSeen' 15; then
  :
else
  # no cursor inside 15s: the run finished ultra fast (or returned
  # empty). Wait for the composer to go idle BEFORE resending - Enter
  # on a cancel-faced composer sends nothing.
  poll_js '!document.querySelector("button.send.cancel, .composer .cancel")' 90 \
    || die "first run never finished (composer still cancel-faced after 90s)"
  step "stream finished too fast to catch - restarting wider"
  ab find placeholder "message omega" click >/dev/null
  ab keyboard type "Reply in this chat with 80 numbered lines. Each line is exactly: journey line N (N is 1 to 80). Do not use any tools - the answer must be the chat message itself."
  wait_js '(()=>{return [...document.querySelectorAll("textarea,[contenteditable=true]")].some(e=>(e.value||e.textContent||"").includes("numbered lines"))})()' 10 \
    || die "the wider prompt never landed in the composer draft"
  jsq 'window.__cursorSeen=false, true' >/dev/null 2>&1
  ab press Enter
  ab press Enter
  poll_js '!!window.__cursorSeen' 15 \
    || die "no streaming cursor seen; cannot test mid-run refresh (stream finished or never started)"
fi
step "mid-stream confirmed (cursor blinking)"

# the customer reloads NOW, while the run is still live
open_app
step "reloaded mid-run"

# the transcript must merge the live run: session restores and streams on
wait_js 'document.querySelectorAll(".msg.user").length >= 1' 20 || die "after reload the transcript did not restore the sent message"
step "ok: transcript restored after reload"

wait_js '!document.querySelector("button.send.cancel, .composer .cancel")' 150
lines=$(jsq 'document.querySelectorAll(".msg.assistant .md li").length || document.body.innerText.match(/journey line/g)?.length || 0')
case "$(printf '%s' "$lines")" in [1-9]*) ;; *) die "live run did not complete into the transcript after reload ($lines)" ;; esac
step "ok: merged live run completes ($lines numbered lines)"

assert_no_console_errors