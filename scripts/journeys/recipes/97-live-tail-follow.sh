#!/bin/bash
# 97-live-tail-follow - the tail-follow journey (card t_bef3f45e):
# while a response streams, the chat view follows the tail. The customer
# scrolls up mid-stream: follow must STOP, the jump-to-live pill must
# show, and the growing content must NOT yank the view back to the live
# tail. Clicking the pill returns to live. The node tests pin the pure
# laws only (resumesFollow, pillVisible); this journey is the only
# witness that the ResizeObserver seam is wired - a dead seam passes
# every pure test with the follow broken.
#
# The whole observation runs INSIDE the page (a 100ms recorder installed
# before the send, which also performs the reader's scroll-up the moment
# the pane has grown twice). A shell round trip costs two `ab eval` calls
# plus a sleep - on a fast lane (the dev lane's model streams a 60-line
# answer's text in ~1.5s) the old six fixed 1s windows graded a settled
# pane, and a shell-gated scroll-up landed after the stream had ended.
# The page keeps the trace and the moment; the shell only reads counters
# and asserts on them.
JOURNEY_NEEDS_MODEL=1
. "${JROOT:?run must export JROOT}/lib.sh"

num_jsq() { jsq "$1" 2>&1 | tail -1 | tr -d '\r' | tr -d '"'; }

SID=$(new_journey_session tailfollow | tail -1)
TAG=$(journey_tag)
label_journey_session "$SID" "(journey) tail-follow $TAG"

open_app
open_journey_row "(journey) tail-follow $TAG"
ab find placeholder "message omega" click >/dev/null
ab keyboard type "Reply in this chat with 240 numbered lines. Each line is exactly: follow tail test line N (N goes from 1 to 240). Do not use any tools - no files, no shell; the answer must be the chat message itself."
wait_js '(()=>{return [...document.querySelectorAll("textarea,[contenteditable=true]")].some(e=>(e.value||e.textContent||"").includes("numbered lines"))})()' 10 \
  || die "the typed prompt never landed in the composer draft"

# the recorder: samples the scrollport, the streaming cursor and growth
# every 100ms, and performs the reader's scroll-up itself the moment the
# pane has grown twice (the follow has something to grade by then, and
# the stream still has content ahead of it). Installed BEFORE the send - a
# recorder started after the first chunk misses exactly the part that is
# hard to catch.
jsq '(()=>{window.__cursorSeen=false;window.__tail=[];window.__grew=0;window.__firstGrowth=-1;window.__hmax=-1;
  window.__scrollUpAt=-1;
  const t0=performance.now();
  const tick=()=>{const m=document.querySelector(".messages");
    if(document.querySelector(".msg .cursor"))window.__cursorSeen=true;
    const h=m?m.scrollHeight:-1;
    if(h>0&&window.__hmax>=0&&h>window.__hmax){window.__grew++;if(window.__firstGrowth<0)window.__firstGrowth=window.__tail.length;}
    if(h>window.__hmax)window.__hmax=h;
    window.__tail.push({t:Math.round(performance.now()-t0),h:h,
      top:m?Math.round(m.scrollTop):-1,
      gap:m?Math.round(m.scrollHeight-m.clientHeight-m.scrollTop):-1,
      busy:!!document.querySelector("button.send.cancel, .composer .cancel")});};
  window.__tail.push({t:0,h:0,top:-1,gap:-1,busy:false});
  window.__scrollWatch=setInterval(()=>{if(window.__scrollUpAt>=0)return;
    if(window.__grew<2)return;if((window.__tail.length-window.__firstGrowth)<10)return;
    const m=document.querySelector(".messages");
    if(!m)return;window.__scrollUpAt=window.__tail.length;m.scrollTop=0;},100);
  tick();window.__tailTimer=setInterval(tick,100);return true})()' >/dev/null 2>&1
poll_js '!!window.__tailTimer' 5 \
  || die "the live recorder never installed - nothing below can be graded"
ab press Enter
ab press Enter
step "sent long prompt"

# the reader's scroll-up fires from the page the instant the pane has grown
# twice; wait for that moment (a lost premise - no growth at all - dies with
# its own numbers, never with a phantom follow verdict)
poll_js 'window.__scrollUpAt>=0' 90 \
  || die "the pane never grew twice while live (grew=$(num_jsq 'window.__grew') samples=$(num_jsq 'window.__tail.length')) - nothing streamed to follow"
step "ok: live growth reached the scroll-up (grew=$(num_jsq 'window.__grew'))"

# 2) the scroll-up is mid-stream: the pill shows, and the growing tail must
#    not yank the view back (the shrink-clamp/resumesFollow law runs both
#    directions - an unpinned view stays unpinned)
poll_js '!!document.querySelector(".jump-live")' 15 \
  || die "jump-to-live pill never showed after scrolling up"
# the no-yank claim is an ABSENCE: watch it sample by sample instead of
# pausing two seconds and reading once (card t_fdd12843)
watch_absent '(()=>{const m=document.querySelector(".messages");return !!m&&m.scrollTop>200})()' 2 \
  "the live tail yanking the view back after a scroll-up" \
  '(()=>{const m=document.querySelector(".messages");return "scrollTop="+(m?Math.round(m.scrollTop):"missing")})()'
yank=$(num_jsq 'document.querySelector(".messages")?.scrollTop ?? -1')
case "$yank" in ''|*[!0-9-]*) die "yank probe garbage: $yank" ;; esac
[ "$yank" -gt 200 ] && die "view yanked back to the tail after scroll-up (scrollTop=$yank)"
step "ok: follow stopped, pill shown, no yank (scrollTop=$yank)"

# grade the run from the trace: the pinned follow BEFORE the scroll-up (the
# law's positive half) and real growth AFTER it (else the no-yank window
# graded a settled pane)
num=$(jsq '(()=>{const t=window.__tail;const k=window.__scrollUpAt;
  const live=t.slice(0,k).filter(s=>s.busy&&s.h>0);
  if(!live.length)return "none";
  let grew=0;for(let i=1;i<live.length;i++)if(live[i].h>live[i-1].h)grew++;
  const base=live[0].h;const grown=live.filter(s=>s.h>base);
  const pinned=grown.filter(s=>Math.abs(s.gap)<=40).length;
  const after=t.slice(k).filter(s=>s.busy&&s.h>0);
  let grewAfter=0;for(let i=1;i<after.length;i++)if(after[i].h>after[i-1].h)grewAfter++;
  return [grew,live.length,grown.length,pinned,grewAfter,after.length].join(",")})()' 2>&1 | tail -1 | tr -d '\r' | tr -d '"')
case "$num" in
  none|"") die "no live sample captured before the scroll-up - the run never streamed with a .messages scrollport" ;;
esac
GROW=$(printf '%s' "$num" | cut -d, -f1)
LIVE=$(printf '%s' "$num" | cut -d, -f2)
GROWN=$(printf '%s' "$num" | cut -d, -f3)
PINNED=$(printf '%s' "$num" | cut -d, -f4)
GREWAFTER=$(printf '%s' "$num" | cut -d, -f5)
AFTER=$(printf '%s' "$num" | cut -d, -f6)
case "$GROW$LIVE$GROWN$PINNED$GREWAFTER$AFTER" in ''|*[!0-9]*) die "trace probe garbage: $num" ;; esac
step "ok: live trace grew=$GROW live=$LIVE grown=$GROWN pinned=$PINNED grewAfter=$GREWAFTER liveAfter=$AFTER"
[ "$GROW" -ge 2 ] || die "content never grew across $LIVE live samples - the pane never painted the stream"
[ "$GROWN" -ge 6 ] || die "only $GROWN samples after the first growth - the stream was too short to grade the follow"
[ $((PINNED * 4)) -ge $((GROWN * 3)) ] \
  || die "pinned view did not ride the streaming tail (pinned=$PINNED of grown=$GROWN within 40px)"
[ "$GREWAFTER" -ge 1 ] \
  || die "the tail stopped growing at the scroll-up (liveAfter=$AFTER) - nothing could have yanked the view, the absence proves nothing"
step "ok: pinned view rode the growing tail ($PINNED/$GROWN within 40px) and the tail kept growing after the scroll-up ($GREWAFTER)"

# 3) the pill click returns the view to the live tail
jsq 'document.querySelector(".jump-live").click()' >/dev/null
poll_js '(()=>{const el=document.querySelector(".messages");return el?(el.scrollHeight-el.clientHeight-el.scrollTop)<40:false})()' 10 \
  || die "pill click did not return the view to the live tail"
step "ok: pill click returned to live"

# 4) the run finishes and the console stays silent
wait_js '!document.querySelector(".msg .cursor")' 200 \
  || step "warn: cursor still live after 200s - tail behavior already proven"
jsq 'clearInterval(window.__tailTimer),clearInterval(window.__scrollWatch),true' >/dev/null 2>&1
assert_no_console_errors
