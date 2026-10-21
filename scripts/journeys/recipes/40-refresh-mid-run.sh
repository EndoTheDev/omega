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
ab keyboard type "Write 15 numbered lines. Each line is exactly: journey line N (N is 1 to 15)"
sleep 1
ab press Enter
ab press Enter
step "sent long prompt"

# mid-stream: wait for a streaming cursor with a real poll - TTFT is
# seconds, so a one-shot check right after send always lost the race.
# (The old `ab wait --text "journey line"` matched the USER bubble -
# the prompt itself contains the token - and a one-shot cursor check
# 0.3s after send always read false, so the fallback fired on a
# cancel-faced composer and the wider send never left the draft.)
if poll_js '!!document.querySelector(".msg .cursor")' 15; then
  :
else
  # no cursor inside 15s: the run finished ultra fast (or returned
  # empty). Wait for the composer to go idle BEFORE resending - Enter
  # on a cancel-faced composer sends nothing.
  poll_js '!document.querySelector("button.send.cancel, .composer .cancel")' 90 \
    || die "first run never finished (composer still cancel-faced after 90s)"
  step "stream finished too fast to catch - restarting wider"
  ab find placeholder "message omega" click >/dev/null
  ab keyboard type "Write 30 numbered lines. Each line is exactly: journey line N (N is 1 to 30)"
  sleep 1
  ab press Enter
  ab press Enter
  poll_js '!!document.querySelector(".msg .cursor")' 15 \
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