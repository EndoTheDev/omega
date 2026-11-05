#!/bin/bash
# 97-live-tail-follow - the tail-follow journey (card t_bef3f45e):
# while a response streams, the chat view follows the tail. The customer
# scrolls up mid-stream: follow must STOP, the jump-to-live pill must
# show, and the growing content must NOT yank the view back to the live
# tail. Clicking the pill returns to live. The node tests pin the pure
# laws only (resumesFollow, pillVisible); this journey is the only
# witness that the ResizeObserver seam is wired - a dead seam passes
# every pure test with the follow broken.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

# num_jsq <js>: jsq with the eval-channel noise stripped - ab eval may
# quote string results and the MSYS pipe carries a trailing CR; the
# probes below need bare integers.
num_jsq() { jsq "$1" 2>&1 | tail -1 | tr -d '"\r'; }

SID=$(new_journey_session tailfollow | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) tail-follow $TAG"

open_app
open_journey_row "(journey) tail-follow $TAG"
ab find placeholder "message omega" click >/dev/null
ab keyboard type "Write 60 numbered lines. Each line is exactly: follow tail test line N (N goes from 1 to 60)."
sleep 1
ab press Enter
ab press Enter
step "sent long prompt"

# streaming must be live before asserting scroll behavior
poll_js '!!document.querySelector(".msg .cursor")' 15 \
  || die "no streaming cursor seen (stream finished or never started)"
step "streaming confirmed"

# 1) the pinned view rides the streaming tail. Content grows between
#    transcript deltas (150ms-debounced paint, thinking growth), so a
#    sample that stays near the bottom while scrollHeight grows proves
#    the RO/content follow, not a one-shot send scroll.
bottom_count=0
grew=0
prev_h=$(num_jsq 'document.querySelector(".messages")?.scrollHeight ?? 0')
case "$prev_h" in ''|*[!0-9]*) die "height probe garbage: $prev_h" ;; esac
for i in 1 2 3 4 5 6; do
  sleep 1
  gap=$(num_jsq '(()=>{const el=document.querySelector(".messages");return el?Math.round(el.scrollHeight-el.clientHeight-el.scrollTop):-1})()')
  h=$(num_jsq 'document.querySelector(".messages")?.scrollHeight ?? -1')
  case "$gap$h" in ''|*[!0-9-]*) die "scroll probe garbage: gap=$gap h=$h" ;; esac
  [ "$gap" = "-1" ] || [ "$h" = "-1" ] && die "no .messages scroll container while streaming"
  [ "$h" -gt "$prev_h" ] && grew=$((grew + 1))
  prev_h=$h
  [ "$gap" -le 40 ] && bottom_count=$((bottom_count + 1))
done
[ "$grew" -ge 2 ] || die "content never grew - stream too short to test the follow"
[ "$bottom_count" -ge 4 ] \
  || die "pinned view did not ride the streaming tail ($bottom_count/6 samples within 40px)"
step "ok: pinned view rode the growing tail ($bottom_count/6 samples)"

# 2) scroll up mid-stream: follow stops, the pill shows, and the growing
#    tail must not yank the view back (the shrink-clamp/resumesFollow law
#    runs both directions - an unpinned view stays unpinned)
jsq 'document.querySelector(".messages").scrollTop = 0' >/dev/null
sleep 1
poll_js '!!document.querySelector(".jump-live")' 10 \
  || die "jump-to-live pill never showed after scrolling up"
sleep 2
yank=$(num_jsq 'document.querySelector(".messages")?.scrollTop ?? -1')
case "$yank" in ''|*[!0-9-]*) die "yank probe garbage: $yank" ;; esac
[ "$yank" -gt 200 ] && die "view yanked back to the tail after scroll-up (scrollTop=$yank)"
step "ok: follow stopped, pill shown, no yank (scrollTop=$yank)"

# 3) the pill click returns the view to the live tail
jsq 'document.querySelector(".jump-live").click()' >/dev/null
poll_js '(()=>{const el=document.querySelector(".messages");return el?(el.scrollHeight-el.clientHeight-el.scrollTop)<40:false})()' 10 \
  || die "pill click did not return the view to the live tail"
step "ok: pill click returned to live"

# 4) the run finishes and the console stays silent
wait_js '!document.querySelector(".msg .cursor")' 200 \
  || step "warn: cursor still live after 200s - tail behavior already proven"
assert_no_console_errors
