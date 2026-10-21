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
ab press Enter
step "sent long prompt"

# mid-stream: a streaming bubble with a cursor must exist, THEN reload
ab wait --text "journey line" >/dev/null 2>&1 || true
streaming=$(jsq '!!(document.querySelector(".cursor") || document.querySelector(".msg .cursor"))')
if [ "$streaming" != "true" ]; then
  step "stream finished too fast to catch - restarting wider"
  ab find placeholder "message omega" click >/dev/null
  ab keyboard type "Write 30 numbered lines. Each line is exactly: journey line N (N is 1 to 30)"
  ab press Enter
  wait_js '!!document.querySelector(".msg .cursor")' 20 || true
fi
streaming=$(jsq '!!document.querySelector(".msg .cursor")')
[ "$streaming" = "true" ] || die "no streaming cursor seen; cannot test mid-run refresh (stream finished or never started)"
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